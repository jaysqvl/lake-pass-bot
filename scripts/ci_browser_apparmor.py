"""Generate an exact-binary, temporary Chromium userns profile for Linux CI.

The workflow loads this profile only around integration tests. It never changes
the host-wide namespace restriction or disables Chromium's own sandbox.
"""

import argparse
import json
import os
import platform
import re
from pathlib import Path


def pinned_paths(chromium_executable, browsers):
    """Bind both Linux/x64 executables to the installed locked SDK metadata."""
    revisions = {}
    for name in ("chromium", "chromium-headless-shell"):
        records = [browser for browser in browsers if browser.get("name") == name]
        if len(records) != 1 or not re.fullmatch(r"[0-9]+", records[0].get("revision", "")):
            raise ValueError("Expected one numeric locked revision for " + name)
        revisions[name] = records[0]["revision"]

    chromium = Path(chromium_executable)
    if not chromium.is_absolute():
        raise ValueError("Chromium executable must be absolute")
    cache = chromium.parents[2]
    expected = cache / ("chromium-" + revisions["chromium"]) / "chrome-linux64/chrome"
    if chromium != expected:
        raise ValueError("Unexpected pinned Linux/x64 Chromium layout")
    shell = (cache / ("chromium_headless_shell-" + revisions["chromium-headless-shell"])
             / "chrome-headless-shell-linux64/chrome-headless-shell")
    for executable in (chromium, shell):
        # Reject aliases: the kernel attachment must name this exact installed
        # binary, not a symlink to some other executable or cache revision.
        if executable.resolve(strict=True) != executable:
            raise ValueError("Browser executable must not traverse a symlink")
        if not executable.is_file() or not os.access(executable, os.X_OK):
            raise ValueError("Pinned browser executable is missing or not executable")
    return (chromium, shell)


def render_profile(name, executables):
    if not re.fullmatch(r"lake-pass-ci-[0-9]+-[0-9]+", name):
        raise ValueError("Profile name must identify one GitHub run and attempt")
    if len(executables) != 2 or len(set(executables)) != 2:
        raise ValueError("Expected exactly two distinct pinned browser executables")
    lines = ["abi <abi/4.0>,", "include <tunables/global>", ""]
    for index, executable in enumerate(executables):
        path = str(executable)
        # No AppArmor globbing, variables, quoting escapes or policy injection.
        if (not re.fullmatch(r"/[A-Za-z0-9_./-]+", path) or path.startswith("//")
                or str(Path(path)) != path or ".." in Path(path).parts):
            raise ValueError("Browser path must be a literal canonical attachment")
        lines.extend([
            f'profile {name}-{index} "{path}" flags=(unconfined) {{',
            "  userns,",
            "}",
            "",
        ])
    return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--name", required=True)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    if platform.system() != "Linux" or platform.machine() != "x86_64":
        raise SystemExit("This profile is only for the Linux/x64 integration runner")
    import playwright
    from playwright.sync_api import sync_playwright

    metadata = Path(playwright.__file__).parent / "driver/package/browsers.json"
    browsers = json.loads(metadata.read_text())["browsers"]
    with sync_playwright() as runtime:
        executables = pinned_paths(runtime.chromium.executable_path, browsers)
    profile = render_profile(args.name, executables)
    # The caller uses a fresh run-specific file, never an existing file/symlink.
    fd = os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w") as output:
        output.write(profile)
    for executable in executables:
        print("CI-only sandbox attachment:", executable)


if __name__ == "__main__":
    main()
