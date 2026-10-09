"""Exercise the container's JSON API and embedded frontend; Docker lifecycle stays in docker_smoke.sh."""

from __future__ import annotations

import argparse
from http.cookiejar import MozillaCookieJar
from http.cookies import SimpleCookie
import json
import os
import re
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


def decode_page(body: str, resource: str | None = None) -> dict:
    page = json.loads(body)
    assert isinstance(page.get("Data"), dict), "API data is missing"
    assert isinstance(page.get("Build"), dict), "API build identity is missing"
    if resource:
        assert page.get("Page") == resource, "unexpected API resource"
    return page


def csrf(data: dict) -> str:
    token = data.get("CSRFToken")
    assert isinstance(token, str) and token, "API CSRF token is empty"
    return token


def validate_page_version(body: str, version: str, revision: str) -> None:
    build = decode_page(body)["Build"]
    assert build.get("Version") == version, "API version differs from the image build"
    assert build.get("Revision") == revision, "API revision differs from the image build"
    if version == "dev":
        assert build.get("Label") == "Development build", "development image is not identified"
    else:
        assert build.get("Label") == f"v{version}", "release version is missing"
        assert build.get("ReleaseURL") == f"{REPOSITORY}/releases/tag/lake-pass-bot-v{version}", "release link is missing"
    if revision:
        assert build.get("ShortRevision") == revision[:7], "build revision is missing"
        assert build.get("CommitURL") == f"{REPOSITORY}/commit/{revision}", "commit link is missing"


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
        api: bool = True,
    ):
        headers = {"Accept": "application/json"} if api else {}
        if host:
            headers["Host"] = host
        if fields is not None:
            headers["Origin"] = self.base_url
        data = None if fields is None else urlencode(fields).encode()
        request = Request(self.base_url + ("/api" if api else "") + path, data=data, headers=headers)
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

    def page(self, path: str, artifact: str, resource: str) -> dict:
        status, headers, body = self.request(path, artifact)
        assert status == 200, f"{path} returned HTTP {status}"
        assert headers.get("Content-Type", "").startswith("application/json"), "API did not return JSON"
        return decode_page(body, resource)["Data"]

    def frontend(self) -> None:
        status, headers, body = self.request("/jobs/123", "frontend", authenticated=False, api=False)
        assert status == 200 and 'id="root"' in body, "frontend history fallback is missing"
        assert headers.get("Content-Security-Policy"), "frontend omitted Content Security Policy"
        assert "CSRFToken" not in body, "frontend shell contains private context"
        assets = re.findall(r'(?:src|href)="(/assets/[^"?#]+\.(?:js|css))"', body)
        assert any(asset.endswith(".js") for asset in assets), "frontend script is missing"
        assert any(asset.endswith(".css") for asset in assets), "frontend stylesheet is missing"
        for asset in assets:
            status, headers, _ = self.request(asset, "frontend-" + asset.rsplit("/", 1)[1], authenticated=False, api=False)
            assert status == 200, "embedded frontend asset is missing"
            assert "text/html" not in headers.get("Content-Type", ""), "asset returned the HTML fallback"
            assert headers.get("Cache-Control") == "public, max-age=31536000, immutable", "hashed asset cache policy is missing"

    def landing(self) -> None:
        status, headers, body = self.request("/", "landing")
        if status == 303:
            assert headers.get("Location") == "/api/lakes", (
                "Home returned an unexpected redirect"
            )
            status, headers, body = self.request("/lakes", "landing-lakes")
        assert status == 200, f"authenticated landing page returned HTTP {status}"
        data = decode_page(body)["Data"]
        assert data.get("Authenticated") is True and data.get("Username") == self.username, "landing API omitted administrator identity"
        assert headers.get("Cache-Control") == "no-store", (
            "authenticated API was cacheable"
        )
        assert headers.get("Content-Security-Policy"), (
            "authenticated API omitted Content Security Policy"
        )
        validate_page_version(body, self.version, self.revision)

    def setup(self, token: str, password: str) -> None:
        status, _, page = self.request("/setup", "setup")
        assert status == 200, f"setup returned HTTP {status}"
        validate_page_version(page, self.version, self.revision)
        data = decode_page(page, "setup")["Data"]
        status, headers, _ = self.request(
            "/setup",
            "setup-save",
            {
                "csrf_token": csrf(data),
                "setup_token": token,
                "username": self.username,
                "password": password,
                "password_confirm": password,
            },
        )
        assert status == 303, f"first-run setup returned HTTP {status}"
        assert headers.get("Location") == "/api/?ok=setup", (
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
        data = self.page("/login", "login", "login")
        status, headers, _ = self.request(
            "/login",
            "login-save",
            {
                "csrf_token": csrf(data),
                "username": self.username,
                "password": password,
            },
        )
        assert status == 303, f"login returned HTTP {status}"
        assert headers.get("Location") == "/api/", "login returned an unexpected redirect"
        self.landing()

    def setup_complete(self) -> None:
        status, headers, _ = self.request("/setup", "setup-complete")
        assert status == 303 and headers.get("Location") == "/api/login", (
            "setup completion was not retained"
        )

    def network_form(self, enabled: bool) -> dict:
        data = self.page("/settings/network", "network", "network_settings")
        csrf(data)
        assert not data.get("ManagedReason"), "hostname settings are unexpectedly locked"
        assert data.get("HostCheckEnabled") is enabled, "hostname toggle differs from expected saved state"
        assert isinstance(data.get("AllowedHosts"), str), "allowed hostnames are not available"
        return data

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
        data = self.network_form(not enabled)
        self.hostname_boundary(not enabled)
        fields = {
            "csrf_token": csrf(data),
            "allowed_hosts": f"{urlsplit(self.base_url).netloc},{ALLOWED_HOST}",
        }
        if enabled:
            fields["host_check_enabled"] = "on"
        status, headers, _ = self.request("/settings/network", "network-save", fields)
        assert status == 303, f"saving Network settings returned HTTP {status}"
        assert headers.get("Location") == "/api/settings/network?ok=updated", (
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
    client.frontend()
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
