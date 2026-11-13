#!/usr/bin/env python3
"""Verify every hand-maintained version reference matches the tag being released.

The two READMEs carry literal ``VERSION=vX.Y.Z`` install examples, and
``docs/项目说明.md`` states the current release plus the newest release note.
All are maintained by hand, so tagging a new release without editing them
silently ships docs that install or describe the previous version. Run this
before packaging.

Usage::

    python3 scripts/check_release_version.py v0.6.5
"""
from __future__ import annotations

import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
FILES = ("README.md", "README.en.md")
PROJECT_DOC = "docs/项目说明.md"
TAG_RE = re.compile(r"v\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.\-]+)?")
SNIPPET_RE = re.compile(r"VERSION=(" + TAG_RE.pattern + r")")
CURRENT_RELEASE_RE = re.compile(r"当前发布 (" + TAG_RE.pattern + r")")
LATEST_NOTE_RE = re.compile(r"最新为 `docs/releases/(" + TAG_RE.pattern + r")\.md`")


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

    # The project overview used to stay at v0.5.3 for several releases because
    # it was not part of this check. Require both its stated current version
    # and its "latest release note" pointer to follow the tag, and confirm that
    # the referenced note actually exists.
    doc = ROOT / PROJECT_DOC
    if not doc.exists():
        problems.append(f"{PROJECT_DOC}: file missing")
    else:
        text = doc.read_text(encoding="utf-8")
        stated = CURRENT_RELEASE_RE.findall(text)
        if stated != [tag]:
            problems.append(f"{PROJECT_DOC}: '当前发布' = {stated or ['missing']}, expected [{tag}]")
        else:
            print(f"{PROJECT_DOC}: 当前发布 -> {tag}")
        notes = LATEST_NOTE_RE.findall(text)
        if notes != [tag]:
            problems.append(f"{PROJECT_DOC}: '最新为 docs/releases/...md' = {notes or ['missing']}, expected [{tag}]")
        else:
            print(f"{PROJECT_DOC}: 最新版本说明 -> docs/releases/{tag}.md")
        note_path = ROOT / "docs" / "releases" / f"{tag}.md"
        if not note_path.exists():
            problems.append(f"docs/releases/{tag}.md: release notes missing")

    if problems:
        print("\nRelease version check failed:", file=sys.stderr)
        for problem in problems:
            print(f"  - {problem}", file=sys.stderr)
        print("\nUpdate the install snippets, project overview and release notes before tagging.", file=sys.stderr)
        return 1
    print(f"OK: all version references point at {tag}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
