#!/usr/bin/env python3
"""Verify the README install snippets point at the tag being released.

Both READMEs carry literal ``VERSION=vX.Y.Z`` install examples. Those are
maintained by hand, so tagging a new release without editing them silently
ships a README that installs the previous version. Run this before packaging.

Usage::

    python3 scripts/check_release_version.py v0.5.3
"""
from __future__ import annotations

import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
FILES = ("README.md", "README.en.md")
TAG_RE = re.compile(r"v\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.\-]+)?")
SNIPPET_RE = re.compile(r"VERSION=(" + TAG_RE.pattern + r")")


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: check_release_version.py <tag>", file=sys.stderr)
        return 2
    tag = sys.argv[1]
    if not TAG_RE.fullmatch(tag):
        print(f"invalid tag: {tag!r} (expected vX.Y.Z with optional suffix)", file=sys.stderr)
        return 2

    problems: list[str] = []
    for name in FILES:
        path = ROOT / name
        if not path.exists():
            problems.append(f"{name}: file missing")
            continue
        versions = SNIPPET_RE.findall(path.read_text(encoding="utf-8"))
        if not versions:
            problems.append(f"{name}: no VERSION=vX.Y.Z snippet found")
            continue
        print(f"{name}: {len(versions)} snippet(s) -> {', '.join(versions)}")
        stale = sorted({v for v in versions if v != tag})
        if stale:
            problems.append(f"{name}: expected {tag}, found {', '.join(stale)}")

    if problems:
        print("\nREADME version check failed:", file=sys.stderr)
        for problem in problems:
            print(f"  - {problem}", file=sys.stderr)
        print("\nUpdate the install snippets in both READMEs before tagging.", file=sys.stderr)
        return 1
    print(f"OK: all README install snippets point at {tag}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
