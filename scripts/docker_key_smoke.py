"""Exercise retained encrypted credentials through the local app's JSON API."""
from __future__ import annotations

import argparse
from http.cookiejar import CookieJar
import json
import os
import sqlite3
from urllib.parse import urlencode
from urllib.request import HTTPCookieProcessor, HTTPRedirectHandler, Request, build_opener


class NoRedirects(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("create", "retained"))
    parser.add_argument("--base-url", default="http://127.0.0.1:8080")
    parser.add_argument("--database", default="/appdata/lake-pass-bot.db")
    args = parser.parse_args()
    base = args.base_url.rstrip("/")
    client = build_opener(HTTPCookieProcessor(CookieJar()), NoRedirects())

    def request(path, fields=None):
        assert path.startswith("/") and not path.startswith("//") and "\\" not in path, "unexpected API navigation"
        headers = {"Accept": "application/json", "X-Lake-Pass-Navigation": "manual"}
        if fields is not None:
            headers["Origin"] = base
        payload = None if fields is None else urlencode(fields).encode()
        with client.open(Request(base + "/api" + path, data=payload, headers=headers), timeout=10) as response:
            assert response.status == 200, "API request was rejected"
            assert response.headers.get("Content-Type", "").startswith("application/json"), "API did not return JSON"
            assert response.headers.get("Cache-Control") == "no-store", "credential API was cacheable"
            return json.load(response)

    def page(path):
        for _ in range(8):
            result = request(path)
            if "Redirect" not in result:
                assert isinstance(result.get("Data"), dict), "API page data is missing"
                return result["Data"]
            path = result["Redirect"]
        raise AssertionError("API returned too many redirects")

    def submit(path, fields, destination):
        token = page(path).get("CSRFToken")
        assert isinstance(token, str) and token, "API CSRF token is missing"
        result = request(path, dict(fields, csrf_token=token))
        assert result.get("Redirect") == destination, "API mutation was not confirmed"
        return page(destination)

    dashboard = submit("/login", {"username": "ci-admin", "password": os.environ["CI_ADMIN_PASSWORD"]}, "/")
    assert dashboard.get("Authenticated") is True and dashboard.get("Username") == "ci-admin", "API login identity differs"
    if args.action == "create":
        submit("/sources/new", {
            "name": "Synthetic key relocation", "provider": "twilio",
            "twilio_account_sid": "AC" + "1" * 32,
            "twilio_auth_token": "synthetic-key-relocation-secret",
            "twilio_to_number": "+15550100123",
        }, "/sources?ok=created")
    # Read-only synthetic DB inspection establishes there really is encrypted
    # state. No provider health/pairing/booking request is ever sent.
    with sqlite3.connect("file:" + args.database + "?mode=ro", uri=True) as database:
        rows = database.execute("SELECT id, config_ciphertext FROM otp_sources").fetchall()
    assert len(rows) == 1 and rows[0][1]
    assert "synthetic-key-relocation-secret" not in str(rows[0][1])
    source_id = rows[0][0]
    # The edit path decrypts the stored config and retains its write-only token.
    # Supplying no credential prevents a replacement from masking a wrong key.
    submit(f"/sources/{source_id}", {
        "name": "Synthetic key relocation verified", "provider": "twilio",
    }, "/sources?ok=updated")
    print("Encrypted source retained and updated without replacing its credential.")


if __name__ == "__main__":
    main()
