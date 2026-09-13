"""Regression coverage for deployment-host and authenticated landing checks."""

from __future__ import annotations

from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import os
from pathlib import Path
import tempfile
import threading
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs

from scripts import docker_browser_smoke as BROWSER_SMOKE
from scripts import docker_http_smoke as HTTP_SMOKE

SWAP_ENV = "LAKE_PASS_SMOKE_SWAP_LIMIT_SUPPORTED"
SWAP_HEADER = "Filename\tType\tSize\tUsed\tPriority\n"
BUILD_FOOTER = '<footer id="build-info" data-version="dev" data-revision="">Development build</footer>'


@contextmanager
def http_fixture(handler):
    server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
    thread = threading.Thread(
        target=server.serve_forever, kwargs={"poll_interval": 0.01}
    )
    thread.start()
    try:
        with tempfile.TemporaryDirectory() as directory:
            yield f"http://127.0.0.1:{server.server_port}", Path(directory)
    finally:
        server.shutdown()
        thread.join(timeout=5)
        server.server_close()
        assert not thread.is_alive(), "HTTP fixture did not stop"


class SwapLimitTests(unittest.TestCase):
    def test_supported_accounting_requires_the_effective_limit(self) -> None:
        with (
            tempfile.TemporaryDirectory() as directory,
            patch.dict(os.environ, {SWAP_ENV: "true"}),
        ):
            limit = Path(directory) / "limit"
            for expected in (0, 4 << 30):
                limit.write_text(str(expected))
                BROWSER_SMOKE.check_swap_limit(limit, expected)
                limit.write_text(str(expected + 1))
                with self.assertRaises(AssertionError):
                    BROWSER_SMOKE.check_swap_limit(limit, expected)
            limit.unlink()
            with self.assertRaises(FileNotFoundError):
                BROWSER_SMOKE.check_swap_limit(limit, 0)

    def test_missing_limit_needs_explicit_unsupported_capability(self) -> None:
        with (
            tempfile.TemporaryDirectory() as directory,
            patch.dict(os.environ, {SWAP_ENV: ""}),
        ):
            root = Path(directory)
            (root / "swaps").write_text(SWAP_HEADER)
            with self.assertRaises(FileNotFoundError):
                BROWSER_SMOKE.check_swap_limit(
                    root / "missing-limit", 0, root / "swaps"
                )

    def test_unsupported_accounting_requires_verified_empty_host_swap(self) -> None:
        with (
            tempfile.TemporaryDirectory() as directory,
            patch.dict(os.environ, {SWAP_ENV: "false"}),
        ):
            root = Path(directory)
            swaps = root / "swaps"
            swaps.write_text(SWAP_HEADER)
            BROWSER_SMOKE.check_swap_limit(root / "missing-limit", 0, swaps)
            for contents in (
                "",
                "unrecognized\n",
                SWAP_HEADER + "/swapfile file 1024 0 -2\n",
            ):
                swaps.write_text(contents)
                with self.subTest(contents=contents), self.assertRaises(AssertionError):
                    BROWSER_SMOKE.check_swap_limit(root / "missing-limit", 0, swaps)
            swaps.unlink()
            with self.assertRaises(FileNotFoundError):
                BROWSER_SMOKE.check_swap_limit(root / "missing-limit", 0, swaps)


class AuthenticatedLandingTests(unittest.TestCase):
    def run_landing(
        self,
        root_status=303,
        destination="/lakes",
        final_status=200,
        identity=True,
        cache="no-store",
        csp="default-src 'none'",
        footer=BUILD_FOOTER,
    ):
        paths = []

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                paths.append(self.path)
                redirect = self.path == "/" and root_status == 303
                self.send_response(303 if redirect else final_status)
                if redirect:
                    self.send_header("Location", destination)
                else:
                    self.send_header("Cache-Control", cache)
                    self.send_header("Content-Security-Policy", csp)
                self.end_headers()
                if not redirect and identity:
                    self.wfile.write(
                        b'<a aria-label="Account settings for ci-admin">ci-admin</a>'
                    )
                if not redirect:
                    self.wfile.write(footer.encode())

            def log_message(self, *_args):
                pass

        with http_fixture(Handler) as (base_url, workspace):
            client = HTTP_SMOKE.SmokeClient(base_url, workspace, "landing")
            try:
                client.landing()
            except AssertionError as error:
                return error, paths
        return None, paths

    def test_home_and_expected_lakes_redirect_keep_authenticated_checks(self):
        for status, paths in ((200, ["/"]), (303, ["/", "/lakes"])):
            with self.subTest(status=status):
                error, requested = self.run_landing(root_status=status)
                self.assertIsNone(error)
                self.assertEqual(requested, paths)

    def test_unexpected_redirect_is_never_followed(self):
        for destination in ("/login", "https://example.test/"):
            with self.subTest(destination=destination):
                error, paths = self.run_landing(destination=destination)
                self.assertIsNotNone(error)
                self.assertEqual(paths, ["/"])

    def test_final_page_must_be_authenticated_and_hardened(self):
        for options in (
            {"final_status": 401},
            {"identity": False},
            {"cache": "public"},
            {"csp": ""},
            {"footer": ""},
        ):
            with self.subTest(options=options):
                error, paths = self.run_landing(**options)
                self.assertIsNotNone(error)
                self.assertEqual(paths, ["/", "/lakes"])


class AuthenticationSmokeTests(unittest.TestCase):
    def run_authentication(
        self, *, cookie_attributes="HttpOnly; SameSite=Strict", redirect="/?ok=setup"
    ):
        configured = False
        submissions = []

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                if self.path == "/setup" and configured:
                    self.send_response(303)
                    self.send_header("Location", "/login")
                    self.end_headers()
                    return
                if self.path in {"/setup", "/login"}:
                    self.send_response(200)
                    self.send_header("Set-Cookie", "preauth_csrf=form-token; Path=/")
                    self.end_headers()
                    page = (
                        f'<form method="post" action="{self.path}">'
                        '<input name="csrf_token" value="form-token"></form>'
                        + BUILD_FOOTER
                    )
                    self.wfile.write(page.encode())
                    return
                if "lake_pass_session=session-cookie" not in self.headers.get(
                    "Cookie", ""
                ):
                    self.send_error(401)
                    return
                self.send_response(200)
                self.send_header("Cache-Control", "no-store")
                self.send_header("Content-Security-Policy", "default-src 'none'")
                self.end_headers()
                self.wfile.write(
                    ("Account settings for ci-admin" + BUILD_FOOTER).encode()
                )

            def do_POST(self):
                nonlocal configured
                fields = parse_qs(
                    self.rfile.read(int(self.headers["Content-Length"])).decode()
                )
                expected = {
                    "csrf_token": ["form-token"],
                    "username": ["ci-admin"],
                    "password": ["synthetic-admin-password"],
                }
                if self.path == "/setup":
                    expected.update(
                        setup_token=["synthetic-setup-token"],
                        password_confirm=["synthetic-admin-password"],
                    )
                if (
                    self.path not in {"/setup", "/login"}
                    or fields != expected
                    or "preauth_csrf=form-token" not in self.headers.get("Cookie", "")
                    or self.headers.get("Origin")
                    != f"http://127.0.0.1:{self.server.server_port}"
                ):
                    self.send_error(403)
                    return
                configured = True
                submissions.append(self.path)
                self.send_response(303)
                self.send_header("Location", redirect if self.path == "/setup" else "/")
                self.send_header(
                    "Set-Cookie",
                    f"lake_pass_session=session-cookie; Path=/; {cookie_attributes}",
                )
                self.send_header(
                    "Set-Cookie",
                    f"lake_pass_csrf=csrf-cookie; Path=/; {cookie_attributes}",
                )
                self.end_headers()

            def log_message(self, *_args):
                pass

        with http_fixture(Handler) as (base_url, workspace):
            client = HTTP_SMOKE.SmokeClient(base_url, workspace, "setup")
            client.setup("synthetic-setup-token", "synthetic-admin-password")
            HTTP_SMOKE.SmokeClient(base_url, workspace, "setup").landing()
            HTTP_SMOKE.SmokeClient(base_url, workspace, "login").login(
                "synthetic-admin-password"
            )
            HTTP_SMOKE.SmokeClient(base_url, workspace, "anonymous").setup_complete()
            for path in workspace.iterdir():
                self.assertNotIn(
                    "synthetic-admin-password", path.read_text(), path.name
                )
                self.assertNotIn("synthetic-setup-token", path.read_text(), path.name)
        return submissions

    def test_setup_login_and_reopened_session_use_real_csrf_cookies(self):
        self.assertEqual(self.run_authentication(), ["/setup", "/login"])

    def test_setup_requires_hardened_cookies_and_exact_redirect(self):
        for options in (
            {"cookie_attributes": "SameSite=Strict"},
            {"cookie_attributes": "HttpOnly"},
            {"redirect": "https://example.test/"},
        ):
            with self.subTest(options=options), self.assertRaises(AssertionError):
                self.run_authentication(**options)


class NetworkSettingsSmokeTests(unittest.TestCase):
    def run_network_settings(
        self,
        *,
        initially_enabled=False,
        locked=False,
        apply_changes=True,
        reject_unlisted=True,
        save_status=303,
    ):
        state = {"enabled": initially_enabled}
        updates = []
        allowed_host = "lake-pass-smoke.example:8080"
        unlisted_host = "unlisted-lake-pass.example:8080"

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                if self.path == "/fixture-session":
                    self.send_response(200)
                    self.send_header(
                        "Set-Cookie",
                        "smoke_session=admin; Path=/; HttpOnly; SameSite=Strict",
                    )
                    self.end_headers()
                    return
                if self.path == "/login":
                    if self.headers.get("Cookie"):
                        self.send_response(303)
                        self.send_header("Location", "/")
                        self.end_headers()
                        return
                    rejected = (
                        state["enabled"]
                        and reject_unlisted
                        and self.headers.get("Host") == unlisted_host
                    )
                    self.send_response(400 if rejected else 200)
                    self.end_headers()
                    return
                if (
                    self.path != "/settings/network"
                    or self.headers.get("Cookie") != "smoke_session=admin"
                ):
                    self.send_error(401)
                    return
                checked = " checked" if state["enabled"] else ""
                disabled = " disabled" if locked else ""
                page = (
                    '<form action="/logout" method="post"><input name="csrf_token" value="logout-token"></form>'
                    '<form action="/settings/network" method="post">'
                    '<input name="csrf_token" value="network-token">'
                    f'<input name="host_check_enabled" type="checkbox" role="switch"{checked}{disabled}>'
                    f'<textarea name="allowed_hosts"{disabled}>{allowed_host}</textarea></form>'
                )
                self.send_response(200)
                self.end_headers()
                self.wfile.write(page.encode())

            def do_POST(self):
                form = parse_qs(
                    self.rfile.read(int(self.headers["Content-Length"])).decode()
                )
                if (
                    self.path != "/settings/network"
                    or self.headers.get("Cookie") != "smoke_session=admin"
                    or self.headers.get("Origin")
                    != f"http://127.0.0.1:{self.server.server_port}"
                    or form.get("csrf_token") != ["network-token"]
                ):
                    self.send_error(403)
                    return
                updates.append(form)
                if apply_changes:
                    state["enabled"] = form.get("host_check_enabled") == ["on"]
                self.send_response(save_status)
                self.send_header("Location", "/settings/network?ok=updated")
                self.end_headers()

            def log_message(self, *_args):
                pass

        with http_fixture(Handler) as (base_url, workspace):
            client = HTTP_SMOKE.SmokeClient(base_url, workspace, "network")
            client.request("/fixture-session", "session")
            try:
                client.change_network_settings(True)
                # Reopen the saved cookie jar as the shell does after container restart.
                client = HTTP_SMOKE.SmokeClient(base_url, workspace, "network")
                client.change_network_settings(False)
            except AssertionError as error:
                return error, updates
        return None, updates

    def test_toggle_round_trip_uses_authenticated_form_csrf_and_exact_hostnames(self):
        error, updates = self.run_network_settings()
        self.assertIsNone(error)
        self.assertEqual(len(updates), 2)
        self.assertEqual(updates[0]["host_check_enabled"], ["on"])
        self.assertNotIn("host_check_enabled", updates[1])
        for form in updates:
            self.assertRegex(
                form["allowed_hosts"][0],
                r"^127\.0\.0\.1:\d+,lake-pass-smoke\.example:8080$",
            )

    def test_smoke_rejects_wrong_defaults_locked_controls_unsaved_changes_and_missing_boundary(
        self,
    ):
        for options in (
            {"initially_enabled": True},
            {"locked": True},
            {"apply_changes": False},
            {"reject_unlisted": False},
            {"save_status": 200},
        ):
            with self.subTest(options=options):
                error, _ = self.run_network_settings(**options)
                self.assertIsNotNone(error)


if __name__ == "__main__":
    unittest.main()
