"""Literal attachment and pinned-browser controls; no kernel policy changes."""

import tempfile
import unittest
from pathlib import Path

from ci_browser_apparmor import pinned_paths, render_profile


class BrowserProfileTests(unittest.TestCase):
    def setUp(self):
        self.folder = tempfile.TemporaryDirectory()
        self.addCleanup(self.folder.cleanup)
        self.cache = Path(self.folder.name).resolve()
        self.chromium = self.cache / "chromium-1234/chrome-linux64/chrome"
        self.shell = (self.cache / "chromium_headless_shell-1234"
                      / "chrome-headless-shell-linux64/chrome-headless-shell")
        for executable in (self.chromium, self.shell):
            executable.parent.mkdir(parents=True)
            executable.touch()
            executable.chmod(0o700)
        self.metadata = [
            {"name": "chromium", "revision": "1234"},
            {"name": "chromium-headless-shell", "revision": "1234"},
        ]

    def test_only_the_two_locked_executables_are_attached(self):
        old = self.cache / "chromium-1111/chrome-linux64/chrome"
        old.parent.mkdir(parents=True)
        old.touch()
        paths = pinned_paths(str(self.chromium), self.metadata)
        self.assertEqual(paths, (self.chromium, self.shell))
        profile = render_profile("lake-pass-ci-123-2", paths)
        self.assertEqual(profile.count("  userns,"), 2)
        self.assertEqual(profile.count("flags=(unconfined)"), 2)
        self.assertIn(f'"{self.chromium}"', profile)
        self.assertIn(f'"{self.shell}"', profile)
        self.assertNotIn(str(old), profile)
        self.assertNotIn("*", profile)

    def test_wrong_or_missing_revision_is_rejected(self):
        for metadata in ([], self.metadata + [self.metadata[0]],
                         [{**record, "revision": "*"} for record in self.metadata],
                         [{**record, "revision": "1111"} for record in self.metadata]):
            with self.subTest(metadata=metadata), self.assertRaises(ValueError):
                pinned_paths(str(self.chromium), metadata)

    def test_missing_browser_does_not_fall_back_to_host_chrome(self):
        self.shell.unlink()
        with self.assertRaises(FileNotFoundError):
            pinned_paths(str(self.chromium), self.metadata)

    def test_nonexecutable_or_symlinked_browser_is_rejected(self):
        self.shell.chmod(0o600)
        with self.assertRaises(ValueError):
            pinned_paths(str(self.chromium), self.metadata)
        self.shell.unlink()
        self.shell.symlink_to(self.chromium)
        with self.assertRaises(ValueError):
            pinned_paths(str(self.chromium), self.metadata)

    def test_policy_metacharacters_and_noncanonical_paths_are_rejected(self):
        for path in ("relative/chrome", "/tmp/*/chrome", "/tmp/?/chrome",
                     "/tmp/{a,b}/chrome", "/tmp/@{HOME}/chrome", '/tmp/"chrome',
                     "/tmp/chrome\nuserns,", "/tmp/../chrome", "/tmp//chrome",
                     "/tmp/a b/chrome", "/tmp/\\chrome", "/tmp/[a]/chrome", "//tmp/chrome"):
            with self.subTest(path=path), self.assertRaises(ValueError):
                render_profile("lake-pass-ci-123-2", (path, "/tmp/shell"))

    def test_profile_identity_and_attachment_count_fail_closed(self):
        for name in ("chrome", "lake-pass-ci-*-2", "lake-pass-ci-123-2\nuserns,"):
            with self.subTest(name=name), self.assertRaises(ValueError):
                render_profile(name, (self.chromium, self.shell))
        for paths in ((), (self.chromium,), (self.chromium, self.chromium)):
            with self.subTest(paths=paths), self.assertRaises(ValueError):
                render_profile("lake-pass-ci-123-2", paths)

    def test_workflow_scopes_policy_to_integration_and_always_removes_it(self):
        workflow = (Path(__file__).resolve().parents[1] / ".github/workflows/ci.yml").read_text()
        before, integration = workflow.split("\n  integration:\n", 1)
        integration, after = integration.split("\n  docker:\n", 1)
        self.assertNotIn("sudo apparmor_parser", before + after)
        self.assertIn("PLAYWRIGHT_BROWSERS_PATH: ${{ runner.temp }}/", integration)
        self.assertLess(integration.index("--add --skip-cache"),
                        integration.index("- name: Browser integration"))
        cleanup = integration.split("- name: Remove the CI-only browser profile\n", 1)[1]
        self.assertIn("if: always()", cleanup)
        self.assertIn('sudo apparmor_parser --remove --skip-cache "$profile"', cleanup)
        self.assertIn('sudo rm -- "$profile"', cleanup)
        self.assertEqual(integration.count("kernel.apparmor_restrict_unprivileged_userns)\" = 1"), 3)
        self.assertNotIn("--no-sandbox", workflow)


if __name__ == "__main__":
    unittest.main()
