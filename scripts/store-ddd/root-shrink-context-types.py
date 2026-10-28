#!/usr/bin/env python3
"""Move seelectx production type/clause identities off the root store facade."""

from __future__ import annotations

import argparse
import pathlib
import re

ROOT = pathlib.Path(__file__).resolve().parents[2]
BASE = ROOT / "backend/engine/context/seelectx"


def transform(text: str) -> str:
    text = text.replace("store.Event", "protocol.TranscriptEvent")
    text = text.replace("store.CompleteEventUnits", "protocol.CompleteEventUnits")
    text = text.replace("store.CompactFrame", "storecontext.CompactFrame")
    text = text.replace("store.EvidenceRef", "storecontext.EvidenceRef")
    return text


def ensure_imports(text: str) -> str:
    wanted = []
    if "protocol." in text and '"github.com/RedHuang-0622/seelex/contract/protocol"' not in text:
        wanted.append('protocol "github.com/RedHuang-0622/seelex/contract/protocol"')
    if "storecontext." in text and '"github.com/RedHuang-0622/seelex/store/context"' not in text:
        wanted.append('storecontext "github.com/RedHuang-0622/seelex/store/context"')
    for imported in wanted:
        text = text.replace(")\n", f"\t{imported}\n)\n", 1)
    return text


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()
    changed = []
    for path in sorted(BASE.rglob("*.go")):
        if path.name.endswith("_test.go"):
            continue
        before = path.read_text(encoding="utf-8")
        after = ensure_imports(transform(before))
        if after == before:
            continue
        changed.append(path.relative_to(ROOT).as_posix())
        if not args.dry_run:
            path.write_text(after, encoding="utf-8", newline="\n")
    print("\n".join(changed))


if __name__ == "__main__":
    main()
