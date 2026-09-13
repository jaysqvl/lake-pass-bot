#!/usr/bin/env python3
from __future__ import annotations

import json
import re
from pathlib import Path
import tomllib


ROOT = Path(__file__).resolve().parent.parent
PACKAGE_NAME = "lake-pass-actions"


def fail(message: str) -> None:
    raise SystemExit(f"release metadata: {message}")


def read_json(path: str) -> object:
    return json.loads((ROOT / path).read_text(encoding="utf-8"))


def main() -> int:
    manifest = read_json(".release-please-manifest.json")
    config = read_json("release-please-config.json")
    pyproject_text = (ROOT / "actions/pyproject.toml").read_text(encoding="utf-8")
    lock_text = (ROOT / "actions/uv.lock").read_text(encoding="utf-8")

    if not isinstance(manifest, dict) or not isinstance(config, dict):
        fail("manifest and configuration must be JSON objects")

    manifest_version = manifest.get(".")
    project = tomllib.loads(pyproject_text).get("project", {})
    project_version = project.get("version")
    packages = tomllib.loads(lock_text).get("package", [])
    locked_matches = [
        package for package in packages if package.get("name") == PACKAGE_NAME
    ]
    if len(locked_matches) != 1:
        fail(f"actions/uv.lock must contain exactly one {PACKAGE_NAME} package")
    lock_version = locked_matches[0].get("version")

    if not (
        isinstance(manifest_version, str)
        and manifest_version == project_version == lock_version
    ):
        fail("manifest, Python project, and lockfile versions do not match")

    packages_config = config.get("packages")
    root_config = (
        packages_config.get(".") if isinstance(packages_config, dict) else None
    )
    if (
        not isinstance(root_config, dict)
        or root_config.get("package-name") != "lake-pass-bot"
        or root_config.get("component") != "lake-pass-bot"
    ):
        fail("Release Please package and component must use lake-pass-bot")
    extra_files = (
        root_config.get("extra-files") if isinstance(root_config, dict) else None
    )
    if not isinstance(extra_files, list):
        fail("Release Please has no root extra-files list")

    expected_entries = (
        {
            "type": "toml",
            "path": "actions/pyproject.toml",
            "jsonpath": "$.project.version",
        },
        {"type": "generic", "path": "actions/uv.lock"},
    )
    for expected in expected_entries:
        if expected not in extra_files:
            fail(f"Release Please does not update {expected['path']}")

    # TOML discards comments. Inspect this one Release Please annotation as text,
    # then parse its prefix to establish which package owns the marked version.
    markers = list(
        re.finditer(
            r'(?m)^version\s*=\s*"[^"]+" # x-release-please-version\s*$',
            lock_text,
        )
    )
    if len(markers) != 1:
        fail("actions/uv.lock lacks the package version update marker")
    marked_packages = tomllib.loads(lock_text[: markers[0].end()]).get("package", [])
    if not marked_packages or marked_packages[-1].get("name") != PACKAGE_NAME:
        fail("actions/uv.lock version update marker belongs to another package")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
