#!/usr/bin/env python3
"""Fail the build when a user-visible Chinese string has no English translation.

Viya uses Chinese literals as i18n keys: ``t("保存成功")`` looks the key up in
``web/src/locales/en.json`` when the UI language is English, and the backend
surfaces ``errors.New("保存成功")`` through ``t(body.error)``. A missing key
means English users see raw Chinese.

Only strings that can actually reach ``t()`` are checked:

  * frontend ``t("...")`` / ``t('...')`` and ``tr`...``` static literals;
  * backend ``errors.New("...")``, ``fmt.Errorf("...")`` and
    ``{"error": "..."}`` literals.

Runtime templates (``${...}``) and Go format strings (``%s`` / ``%w``) are
skipped: their final value cannot be matched statically. Orphan keys in
``en.json`` are reported but never fail the build, because many of them come
from dynamic concatenation, server-side formatted messages or older copy.

Usage::

    python3 scripts/check_i18n.py          # exits 1 on missing translations
    python3 scripts/check_i18n.py --verbose # also list orphan keys
"""
from __future__ import annotations

import json
import pathlib
import re
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
FRONTEND = ROOT / "web" / "src"
BACKEND = ROOT / "cmd" / "viya"
CATALOG = FRONTEND / "locales" / "en.json"

HAN = re.compile(r"[\u4e00-\u9fff]")
T_CALL = re.compile(r"(?<![A-Za-z0-9_])t\(\s*([\"'])([^\"'\n]*?)\1")
TR_START = re.compile(r"(?<![A-Za-z0-9_])tr`")
GO_ERRORS = (
    re.compile(r"errors\.New\(\s*\"([^\"\n]*)\"\s*\)"),
    re.compile(r"fmt\.Errorf\(\s*\"([^\"\n]*)\""),
    re.compile(r"\"error\"\s*:\s*\"([^\"\n]*)\""),
)


def extract_tr_keys(text: str) -> list[str]:
    """Extract `tr` tagged templates, replacing `${...}` slots with `{0}`, `{1}`, ..."""
    keys: list[str] = []
    n = len(text)
    for match in TR_START.finditer(text):
        i = match.end()
        out: list[str] = []
        slot = 0
        closed = False
        while i < n:
            ch = text[i]
            if ch == "\\":
                if i + 1 < n:
                    out.append(text[i : i + 2])
                    i += 2
                    continue
                break
            if ch == "`":
                closed = True
                break
            if ch == "\n":
                break
            if ch == "$" and i + 1 < n and text[i + 1] == "{":
                out.append(f"{{{slot}}}")
                slot += 1
                i += 2
                depth = 1
                in_quote: str | None = None
                while i < n and depth > 0:
                    c = text[i]
                    if c == "\\":
                        i += 2
                        continue
                    if in_quote:
                        if c == in_quote:
                            in_quote = None
                        i += 1
                        continue
                    if c in ("'", '"', "`"):
                        in_quote = c
                    elif c == "{":
                        depth += 1
                    elif c == "}":
                        depth -= 1
                    i += 1
                continue
            out.append(ch)
            i += 1
        if closed:
            keys.append("".join(out))
    return keys


def frontend_keys() -> dict[str, str]:
    found: dict[str, str] = {}
    paths = sorted(FRONTEND.rglob("*.vue")) + sorted(FRONTEND.rglob("*.ts"))
    for path in paths:
        text = path.read_text(encoding="utf-8", errors="replace")
        rel = str(path.relative_to(ROOT)).replace("\\", "/")
        for match in T_CALL.finditer(text):
            key = match.group(2)
            if not key or not HAN.search(key) or "\n" in key or "`" in key:
                continue
            found.setdefault(key, rel)
        for key in extract_tr_keys(text):
            if not key or not HAN.search(key):
                continue
            found.setdefault(key, rel)
    return found


def backend_keys() -> dict[str, str]:
    found: dict[str, str] = {}
    for path in sorted(BACKEND.rglob("*.go")):
        if path.name.endswith("_test.go"):
            continue
        text = path.read_text(encoding="utf-8", errors="replace")
        rel = str(path.relative_to(ROOT)).replace("\\", "/")
        for pattern in GO_ERRORS:
            for match in pattern.finditer(text):
                key = match.group(1)
                if not key or not HAN.search(key):
                    continue
                # Format strings produce a different runtime value; internal
                # HTML fragments are not passed through t().
                if "%" in key or "${" in key or "<" in key:
                    continue
                found.setdefault(key, rel)
    return found


def main() -> int:
    if not CATALOG.exists():
        print(f"catalog not found: {CATALOG}", file=sys.stderr)
        return 2
    catalog = json.loads(CATALOG.read_text(encoding="utf-8"))
    if not isinstance(catalog, dict):
        print("en.json must be a flat object", file=sys.stderr)
        return 2

    front, back = frontend_keys(), backend_keys()
    used: dict[str, str] = {}
    for key, origin in list(front.items()) + list(back.items()):
        used.setdefault(key, origin)

    missing = sorted(key for key in used if key not in catalog)
    orphans = sorted(key for key in catalog if key not in used)
    empty = sorted(key for key, value in catalog.items() if isinstance(value, str) and not value.strip())

    print(f"frontend literals: {len(front)} | backend error strings: {len(back)}")
    print(f"checked keys: {len(used)} | catalog entries: {len(catalog)}")
    print(f"missing translations: {len(missing)} | orphan entries: {len(orphans)} | empty values: {len(empty)}")

    if missing:
        print("\nMissing English translations (add these to web/src/locales/en.json):")
        for key in missing[:80]:
            print(f"  [{used[key]}] {key!r}")
        if len(missing) > 80:
            print(f"  ... and {len(missing) - 80} more")
        print("\nTip: keys are the Chinese literals themselves; keep punctuation identical.")
    if empty:
        print("\nEmpty translations:")
        for key in empty[:20]:
            print(f"  {key!r}")

    if "--verbose" in sys.argv and orphans:
        print(f"\nOrphan entries (reported only, never a failure), first 30 of {len(orphans)}:")
        for key in orphans[:30]:
            print(f"  {key!r}")

    if missing or empty:
        return 1
    print("\nOK: every user-visible Chinese string has an English translation.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
