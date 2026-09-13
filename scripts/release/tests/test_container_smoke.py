"""Regression coverage for deployment-host and authenticated landing checks."""
from __future__ import annotations

from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import importlib.util
import os
from pathlib import Path
import re
import subprocess
import tempfile
import threading
import unittest
from unittest.mock import patch
from urllib.parse import parse_qs


ROOT = Path(__file__).resolve().parents[3]
SPEC = importlib.util.spec_from_file_location("docker_browser_smoke", ROOT / "scripts/docker_browser_smoke.py")
assert SPEC is not None and SPEC.loader is not None
BROWSER_SMOKE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BROWSER_SMOKE)
SWAP_ENV = "LAKE_PASS_SMOKE_SWAP_LIMIT_SUPPORTED"
SWAP_HEADER = "Filename\tType\tSize\tUsed\tPriority\n"


class SwapLimitTests(unittest.TestCase):
    def test_supported_accounting_requires_the_effective_limit(self) -> None:
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, {SWAP_ENV: "true"}):
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
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, {SWAP_ENV: ""}):
            root = Path(directory)
            (root / "swaps").write_text(SWAP_HEADER)
            with self.assertRaises(FileNotFoundError):
                BROWSER_SMOKE.check_swap_limit(root / "missing-limit", 0, root / "swaps")

    def test_unsupported_accounting_requires_verified_empty_host_swap(self) -> None:
        with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, {SWAP_ENV: "false"}):
            root = Path(directory)
            swaps = root / "swaps"
            swaps.write_text(SWAP_HEADER)
            BROWSER_SMOKE.check_swap_limit(root / "missing-limit", 0, swaps)
            for contents in ("", "unrecognized\n", SWAP_HEADER + "/swapfile file 1024 0 -2\n"):
                swaps.write_text(contents)
                with self.subTest(contents=contents), self.assertRaises(AssertionError):
                    BROWSER_SMOKE.check_swap_limit(root / "missing-limit", 0, swaps)
            swaps.unlink()
            with self.assertRaises(FileNotFoundError):
                BROWSER_SMOKE.check_swap_limit(root / "missing-limit", 0, swaps)


class AuthenticatedLandingTests(unittest.TestCase):
    def run_landing(self, root_status=303, destination="/lakes", final_status=200,
                    identity=True, cache="no-store", csp="default-src 'none'"):
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
                    self.wfile.write(b'<a aria-label="Account settings for ci-admin">ci-admin</a>')

            def log_message(self, *_args):
                pass

        script = (ROOT / "scripts/docker_smoke.sh").read_text()
        functions = []
        for name in ("fail", "header_value", "fetch_authenticated_landing"):
            match = re.search(rf"(?ms)^{name}\(\) \{{\n.*?^\}}", script)
            assert match is not None
            functions.append(match.group())
        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.01})
        thread.start()
        try:
            with tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                cookies = root / "cookies"
                cookies.touch()
                environment = {**os.environ, "base_url": f"http://127.0.0.1:{server.server_port}",
                               "admin_username": "ci-admin", "SMOKE_FIXTURE": str(root)}
                command = "set -Eeuo pipefail\n" + "\n".join(functions) + '\nfetch_authenticated_landing "$SMOKE_FIXTURE/cookies" "$SMOKE_FIXTURE/page" "$SMOKE_FIXTURE/headers"\n'
                result = subprocess.run(["bash", "-c", command], env=environment,
                                        capture_output=True, text=True, timeout=10)
                return result, paths
        finally:
            server.shutdown()
            thread.join()
            server.server_close()

    def test_home_and_expected_lakes_redirect_keep_authenticated_checks(self):
        for status, paths in ((200, ["/"]), (303, ["/", "/lakes"])):
            with self.subTest(status=status):
                result, requested = self.run_landing(root_status=status)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(requested, paths)

    def test_unexpected_redirect_is_never_followed(self):
        for destination in ("/login", "https://example.test/"):
            with self.subTest(destination=destination):
                result, paths = self.run_landing(destination=destination)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(paths, ["/"])

    def test_final_page_must_be_authenticated_and_hardened(self):
        for options in ({"final_status": 401}, {"identity": False}, {"cache": "public"}, {"csp": ""}):
            with self.subTest(options=options):
                result, paths = self.run_landing(**options)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(paths, ["/", "/lakes"])


class NetworkSettingsSmokeTests(unittest.TestCase):
    def run_network_settings(self, *, initially_enabled=False, locked=False,
                             apply_changes=True, reject_unlisted=True, save_status=303):
        state = {"enabled": initially_enabled}
        updates = []
        allowed_host = "lake-pass-smoke.example:8080"
        unlisted_host = "unlisted-lake-pass.example:8080"

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                if self.path == "/login":
                    rejected = state["enabled"] and reject_unlisted and self.headers.get("Host") == unlisted_host
                    self.send_response(400 if rejected else 200)
                    self.end_headers()
                    return
                if self.path != "/settings/network" or self.headers.get("Cookie") != "smoke_session=admin":
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
                form = parse_qs(self.rfile.read(int(self.headers["Content-Length"])).decode())
                if (self.path != "/settings/network" or self.headers.get("Cookie") != "smoke_session=admin"
                        or self.headers.get("Origin") != f"http://127.0.0.1:{self.server.server_port}"
                        or form.get("csrf_token") != ["network-token"]):
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

        script = (ROOT / "scripts/docker_smoke.sh").read_text()
        functions = []
        for name in ("fail", "header_value", "network_settings_csrf", "save_network_settings", "validate_hostname_boundary"):
            match = re.search(rf"(?ms)^{name}\(\) \{{\n.*?^\}}", script)
            assert match is not None
            functions.append(match.group())
        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.01})
        thread.start()
        try:
            with tempfile.TemporaryDirectory() as directory:
                environment = {**os.environ, "base_url": f"http://127.0.0.1:{server.server_port}",
                               "workspace": directory, "container": "fixture",
                               "network_allowed_host": allowed_host, "network_unlisted_host": unlisted_host}
                command = "set -Eeuo pipefail\n" + "\n".join(functions) + """
# Execute only the parser portion of docker exec against this HTTP fixture.
docker() { shift 4; python3 "$@"; }
cookies="smoke_session=admin"
csrf="$(network_settings_csrf "$cookies" false)"
validate_hostname_boundary false
save_network_settings "$cookies" true "$csrf"
csrf="$(network_settings_csrf "$cookies" true)"
validate_hostname_boundary true
save_network_settings "$cookies" false "$csrf"
network_settings_csrf "$cookies" false >/dev/null
validate_hostname_boundary false
"""
                result = subprocess.run(["bash", "-c", command], env=environment,
                                        capture_output=True, text=True, timeout=10)
                return result, updates
        finally:
            server.shutdown()
            thread.join()
            server.server_close()

    def test_toggle_round_trip_uses_authenticated_form_csrf_and_exact_hostnames(self):
        result, updates = self.run_network_settings()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(updates), 2)
        self.assertEqual(updates[0]["host_check_enabled"], ["on"])
        self.assertNotIn("host_check_enabled", updates[1])
        for form in updates:
            self.assertRegex(form["allowed_hosts"][0], r"^127\.0\.0\.1:\d+,lake-pass-smoke\.example:8080$")

    def test_smoke_rejects_wrong_defaults_locked_controls_unsaved_changes_and_missing_boundary(self):
        for options in ({"initially_enabled": True}, {"locked": True}, {"apply_changes": False},
                        {"reject_unlisted": False}, {"save_status": 200}):
            with self.subTest(options=options):
                result, _ = self.run_network_settings(**options)
                self.assertNotEqual(result.returncode, 0, result.stdout)


if __name__ == "__main__":
    unittest.main()
