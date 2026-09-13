"""Release metadata checks read TOML values and retain the updater annotation."""

from __future__ import annotations

import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from scripts import check_release_config


class ReleaseConfigTests(unittest.TestCase):
    def check_metadata(
        self,
        *,
        project_version="0.6.2",
        lock_version="0.6.2",
        marker=True,
        marker_package="lake-pass-actions",
        duplicate=False,
    ):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "actions").mkdir()
            (root / ".release-please-manifest.json").write_text(
                json.dumps({".": "0.6.2"})
            )
            (root / "release-please-config.json").write_text(
                json.dumps(
                    {
                        "packages": {
                            ".": {
                                "package-name": "lake-pass-bot",
                                "component": "lake-pass-bot",
                                "extra-files": [
                                    {
                                        "type": "toml",
                                        "path": "actions/pyproject.toml",
                                        "jsonpath": "$.project.version",
                                    },
                                    {"type": "generic", "path": "actions/uv.lock"},
                                ],
                            }
                        },
                    }
                )
            )
            # Whitespace, comments and single quotes are valid TOML, not changes
            # to the release contract. The old regular expressions rejected them.
            (root / "actions/pyproject.toml").write_text(
                f"[project] # worker metadata\nversion='{project_version}'\nname='lake-pass-actions'\n"
            )
            blocks = []
            for package in ("unrelated-dependency", "lake-pass-actions"):
                annotation = (
                    " # x-release-please-version"
                    if marker and package == marker_package
                    else ""
                )
                blocks.append(
                    f'[[package]]\nname = "{package}"\nversion = "{lock_version}"{annotation}\n'
                )
            if duplicate:
                blocks.append(
                    f'[[package]]\nname = "lake-pass-actions"\nversion = "{lock_version}"\n'
                )
            (root / "actions/uv.lock").write_text("\n".join(blocks))
            with patch.object(check_release_config, "ROOT", root):
                return check_release_config.main()

    def test_toml_formatting_does_not_change_the_release_contract(self):
        self.assertEqual(self.check_metadata(), 0)

    def test_mismatched_project_and_lock_versions_are_rejected(self):
        for options in ({"project_version": "0.6.1"}, {"lock_version": "0.6.1"}):
            with (
                self.subTest(options=options),
                self.assertRaisesRegex(SystemExit, "versions do not match"),
            ):
                self.check_metadata(**options)

    def test_marker_must_belong_to_the_application_package(self):
        for options in ({"marker": False}, {"marker_package": "unrelated-dependency"}):
            with (
                self.subTest(options=options),
                self.assertRaisesRegex(SystemExit, "version update marker"),
            ):
                self.check_metadata(**options)

    def test_duplicate_application_packages_are_rejected(self):
        with self.assertRaisesRegex(SystemExit, "exactly one"):
            self.check_metadata(duplicate=True)


if __name__ == "__main__":
    unittest.main()
