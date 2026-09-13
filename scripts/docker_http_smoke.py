"""Exercise the container's HTTP forms; Docker lifecycle stays in docker_smoke.sh."""

from __future__ import annotations

import argparse
from html.parser import HTMLParser
from http.cookiejar import MozillaCookieJar
from http.cookies import SimpleCookie
import os
from pathlib import Path
from urllib.error import HTTPError
from urllib.parse import urlencode, urlsplit
from urllib.request import (
    HTTPCookieProcessor,
    HTTPRedirectHandler,
    Request,
    build_opener,
)


ALLOWED_HOST = "lake-pass-smoke.example:8080"
UNLISTED_HOST = "unlisted-lake-pass.example:8080"
REPOSITORY = "https://github.com/jaysqvl/lake-pass-bot"


class FormParser(HTMLParser):
    def __init__(self, action: str):
        super().__init__()
        self.action = action
        self.in_form = False
        self.forms = []
        self.fields = {}

    def handle_starttag(self, tag, attrs):
        fields = dict(attrs)
        if tag == "form":
            self.in_form = fields.get("action") == self.action
            if self.in_form:
                self.forms.append(fields)
        if self.in_form and tag in {"input", "textarea"} and fields.get("name"):
            self.fields.setdefault(fields["name"], []).append(fields)

    def handle_endtag(self, tag):
        if tag == "form":
            self.in_form = False

    def field(self, name: str) -> dict:
        matches = self.fields.get(name, [])
        assert len(matches) == 1, f"expected exactly one {name} field in {self.action}"
        return matches[0]

    def csrf(self) -> str:
        assert len(self.forms) == 1, f"expected exactly one {self.action} form"
        assert self.forms[0].get("method", "").lower() == "post", (
            "form must submit with POST"
        )
        token = self.field("csrf_token").get("value")
        assert token, "form CSRF token is empty"
        return token


class BuildInfoParser(HTMLParser):
    def __init__(self):
        super().__init__()
        self.footers = []
        self.links = []
        self.text = []
        self.in_footer = False

    def handle_starttag(self, tag, attrs):
        fields = dict(attrs)
        if tag == "footer" and fields.get("id") == "build-info":
            self.footers.append(fields)
            self.in_footer = True
        if self.in_footer and tag == "a":
            self.links.append(fields.get("href"))

    def handle_endtag(self, tag):
        if tag == "footer":
            self.in_footer = False

    def handle_data(self, data):
        if self.in_footer:
            self.text.append(data)


def validate_page_version(html: str, version: str, revision: str) -> None:
    parser = BuildInfoParser()
    parser.feed(html)
    assert len(parser.footers) == 1, "expected exactly one application version footer"
    footer = parser.footers[0]
    assert footer.get("data-version") == version, (
        "page version differs from the image build"
    )
    assert footer.get("data-revision") == revision, (
        "page revision differs from the image build"
    )
    text = " ".join(" ".join(parser.text).split())
    if version == "dev":
        assert "Development build" in text, (
            "development image is not clearly identified"
        )
    else:
        assert f"v{version}" in text, "release version is missing"
        assert f"{REPOSITORY}/releases/tag/lake-pass-bot-v{version}" in parser.links, (
            "release link is missing"
        )
    if revision:
        assert f"Build {revision[:7]}" in text, "build revision is missing"
        assert f"{REPOSITORY}/commit/{revision}" in parser.links, (
            "commit link is missing"
        )


class NoRedirects(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        # Inspect each response before following the one expected Lakes redirect.
        return None


class SmokeClient:
    def __init__(
        self,
        base_url: str,
        workspace: Path,
        label: str,
        username: str = "ci-admin",
        version: str = "dev",
        revision: str = "",
    ):
        self.base_url = base_url.rstrip("/")
        self.workspace = workspace
        self.label = label
        self.username = username
        self.version = version
        self.revision = revision
        self.cookies = MozillaCookieJar(str(workspace / f"{label}-cookies"))
        if Path(self.cookies.filename).exists():
            self.cookies.load(ignore_discard=True)
        self.opener = build_opener(HTTPCookieProcessor(self.cookies), NoRedirects())

    def request(
        self,
        path: str,
        artifact: str,
        fields: dict | None = None,
        host: str | None = None,
        authenticated: bool = True,
    ):
        headers = {}
        if host:
            headers["Host"] = host
        if fields is not None:
            headers["Origin"] = self.base_url
        data = None if fields is None else urlencode(fields).encode()
        request = Request(self.base_url + path, data=data, headers=headers)
        opener = self.opener if authenticated else build_opener(NoRedirects())
        try:
            response = opener.open(request, timeout=10)
        except HTTPError as error:
            response = error
        with response:
            body = response.read().decode()
            status, response_headers = response.status, response.headers
        # Store responses only. Passwords and the setup token must never enter artifacts.
        (self.workspace / f"{self.label}-{artifact}-page").write_text(body)
        (self.workspace / f"{self.label}-{artifact}-headers").write_text(
            str(response_headers)
        )
        self.cookies.save(ignore_discard=True)
        return status, response_headers, body

    def form(self, path: str, artifact: str) -> FormParser:
        status, _, body = self.request(path, artifact)
        assert status == 200, f"{path} returned HTTP {status}"
        parser = FormParser(path)
        parser.feed(body)
        return parser

    def landing(self) -> None:
        status, headers, body = self.request("/", "landing")
        if status == 303:
            assert headers.get("Location") == "/lakes", (
                "Home returned an unexpected redirect"
            )
            status, headers, body = self.request("/lakes", "landing-lakes")
        assert status == 200, f"authenticated landing page returned HTTP {status}"
        assert f"Account settings for {self.username}" in body, (
            "landing page omitted administrator identity"
        )
        assert headers.get("Cache-Control") == "no-store", (
            "authenticated HTML was cacheable"
        )
        assert headers.get("Content-Security-Policy"), (
            "authenticated HTML omitted Content Security Policy"
        )
        validate_page_version(body, self.version, self.revision)

    def setup(self, token: str, password: str) -> None:
        status, _, page = self.request("/setup", "setup")
        assert status == 200, f"setup returned HTTP {status}"
        validate_page_version(page, self.version, self.revision)
        form = FormParser("/setup")
        form.feed(page)
        status, headers, _ = self.request(
            "/setup",
            "setup-save",
            {
                "csrf_token": form.csrf(),
                "setup_token": token,
                "username": self.username,
                "password": password,
                "password_confirm": password,
            },
        )
        assert status == 303, f"first-run setup returned HTTP {status}"
        assert headers.get("Location") == "/?ok=setup", (
            "setup returned an unexpected redirect"
        )
        cookies = SimpleCookie()
        for header in headers.get_all("Set-Cookie", []):
            cookies.load(header)
        for name in ("lake_pass_session", "lake_pass_csrf"):
            assert name in cookies and cookies[name].value, f"setup omitted {name}"
            assert cookies[name]["httponly"], f"{name} omitted HttpOnly"
            assert cookies[name]["samesite"] == "Strict", (
                f"{name} omitted SameSite=Strict"
            )
        self.landing()

    def login(self, password: str) -> None:
        form = self.form("/login", "login")
        status, headers, _ = self.request(
            "/login",
            "login-save",
            {
                "csrf_token": form.csrf(),
                "username": self.username,
                "password": password,
            },
        )
        assert status == 303, f"login returned HTTP {status}"
        assert headers.get("Location") == "/", "login returned an unexpected redirect"
        self.landing()

    def setup_complete(self) -> None:
        status, headers, _ = self.request("/setup", "setup-complete")
        assert status == 303 and headers.get("Location") == "/login", (
            "setup completion was not retained"
        )

    def network_form(self, enabled: bool) -> FormParser:
        form = self.form("/settings/network", "network")
        form.csrf()
        toggle = form.field("host_check_enabled")
        assert toggle.get("type") == "checkbox" and toggle.get("role") == "switch", (
            "hostname control is not an accessible toggle"
        )
        assert "disabled" not in toggle, "hostname toggle is unexpectedly locked"
        assert ("checked" in toggle) == enabled, (
            "hostname toggle differs from expected saved state"
        )
        assert "disabled" not in form.field("allowed_hosts"), (
            "allowed hostnames are not editable"
        )
        return form

    def hostname_boundary(self, enabled: bool) -> None:
        for host, expected in (
            (ALLOWED_HOST, 200),
            (UNLISTED_HOST, 400 if enabled else 200),
        ):
            status, _, _ = self.request(
                "/login", f"host-{host.split(':')[0]}", host=host, authenticated=False
            )
            assert status == expected, (
                f"{host} returned HTTP {status} with hostname checks {enabled}"
            )

    def change_network_settings(self, enabled: bool) -> None:
        # Each half of the round trip starts by verifying the previous state.
        # Docker recreation between the two calls tests actual persistence.
        form = self.network_form(not enabled)
        self.hostname_boundary(not enabled)
        fields = {
            "csrf_token": form.csrf(),
            "allowed_hosts": f"{urlsplit(self.base_url).netloc},{ALLOWED_HOST}",
        }
        if enabled:
            fields["host_check_enabled"] = "on"
        status, headers, _ = self.request("/settings/network", "network-save", fields)
        assert status == 303, f"saving Network settings returned HTTP {status}"
        assert headers.get("Location") == "/settings/network?ok=updated", (
            "unexpected Network save redirect"
        )
        self.network_form(enabled)
        self.hostname_boundary(enabled)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", required=True)
    parser.add_argument("--workspace", required=True, type=Path)
    parser.add_argument("--label", required=True)
    parser.add_argument("--username", default="ci-admin")
    parser.add_argument("--version", default="dev")
    parser.add_argument("--revision", default="")
    parser.add_argument(
        "action",
        choices=(
            "setup",
            "login",
            "landing",
            "setup-complete",
            "network-enable",
            "network-disable",
        ),
    )
    args = parser.parse_args()
    client = SmokeClient(
        args.base_url,
        args.workspace,
        args.label,
        args.username,
        args.version,
        args.revision,
    )
    if args.action == "setup":
        client.setup(
            os.environ["LAKE_PASS_SMOKE_SETUP_TOKEN"],
            os.environ["LAKE_PASS_SMOKE_ADMIN_PASSWORD"],
        )
    elif args.action == "login":
        client.login(os.environ["LAKE_PASS_SMOKE_ADMIN_PASSWORD"])
    elif args.action == "landing":
        client.landing()
    elif args.action == "setup-complete":
        client.setup_complete()
    else:
        client.change_network_settings(enabled=args.action == "network-enable")


if __name__ == "__main__":
    main()
