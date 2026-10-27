#!/usr/bin/env python3
"""Apply the M1 external-consumer replacements from its reviewed manifest."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import subprocess
import sys
import tempfile
from datetime import datetime, timezone
from pathlib import Path


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def git(worktree: Path, *args: str) -> str:
    return subprocess.check_output(["git", *args], cwd=worktree, text=True).strip()


def write_atomic(path: Path, data: bytes) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(dir=path.parent, delete=False) as handle:
        handle.write(data)
        temporary = Path(handle.name)
    os.replace(temporary, path)


def apply_replacements(data: bytes, operations: list[dict]) -> bytes:
    text = data.decode("utf-8")
    for operation in operations:
        if operation["kind"] != "replace":
            raise ValueError(f"unsupported operation: {operation['kind']}")
        before = operation["from"]
        after = operation["to"]
        count = text.count(before)
        if count != 1:
            raise ValueError(f"expected exactly one match, got {count}: {before}")
        text = text.replace(before, after, 1)
    return text.encode("utf-8")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--manifest", required=True)
    parser.add_argument("--checkpoint", required=True)
    parser.add_argument("--worktree", required=True)
    parser.add_argument("--branch", required=True)
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--dry-run", action="store_true")
    mode.add_argument("--apply", action="store_true")
    mode.add_argument("--rollback")
    args = parser.parse_args()

    worktree = Path(args.worktree).resolve()
    actual_root = Path(git(worktree, "rev-parse", "--show-toplevel")).resolve()
    if actual_root != worktree:
        raise SystemExit(f"worktree mismatch: {actual_root} != {worktree}")
    branch = git(worktree, "branch", "--show-current")
    if branch != args.branch or not branch.startswith("codex/store-ddd-"):
        raise SystemExit(f"branch mismatch: {branch!r} != {args.branch!r}")

    manifest_path = worktree / args.manifest
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))

    if args.rollback:
        journal = json.loads(Path(args.rollback).read_text(encoding="utf-8"))
        for entry in journal["files"]:
            path = worktree / entry["path"]
            write_atomic(path, bytes.fromhex(entry["backup_hex"]))
            if sha256(path.read_bytes()) != entry["before_sha256"]:
                raise SystemExit(f"rollback hash mismatch: {entry['path']}")
        print(f"rolled back {len(journal['files'])} files")
        return 0

    if git(worktree, "rev-parse", "HEAD") != args.checkpoint:
        raise SystemExit("HEAD does not match checkpoint")

    planned: list[tuple[Path, bytes, bytes, str, str]] = []
    for entry in manifest["files"]:
        path = worktree / entry["path"]
        before = path.read_bytes()
        before_hash = sha256(before)
        if before_hash != entry["sha256"]:
            raise SystemExit(f"manifest is stale: {entry['path']} sha256={before_hash}")
        after = apply_replacements(before, entry["operations"])
        planned.append((path, before, after, before_hash, sha256(after)))

    for path, _, after, _, _ in planned:
        print(f"{path.relative_to(worktree)}: {len(after)} bytes")
    if args.dry_run:
        print("dry-run: no files written")
        return 0

    journal = {
        "worktree": str(worktree),
        "branch": branch,
        "checkpoint": args.checkpoint,
        "created_at": datetime.now(timezone.utc).isoformat(),
        "files": [
            {
                "path": str(path.relative_to(worktree)),
                "before_sha256": before_hash,
                "after_sha256": after_hash,
                "backup_hex": before.hex(),
            }
            for path, before, _, before_hash, after_hash in planned
        ],
    }
    journal_path = Path(tempfile.gettempdir()) / f"seelex-store-ddd-M1-{os.getpid()}.json"
    journal_path.write_text(json.dumps(journal, indent=2), encoding="utf-8")

    written: list[Path] = []
    try:
        for path, _, after, _, _ in planned:
            write_atomic(path, after)
            written.append(path)
        subprocess.run(
            ["gofmt", "-w", *[str(path) for path in written]],
            cwd=worktree,
            check=True,
        )
    except Exception as exc:
        for path, before, _, before_hash, _ in planned:
            write_atomic(path, before)
            if sha256(path.read_bytes()) != before_hash:
                print(f"restore hash mismatch: {path}", file=sys.stderr)
        print(f"apply failed and was rolled back: {exc}", file=sys.stderr)
        print(f"journal: {journal_path}", file=sys.stderr)
        return 1

    print(f"applied {len(written)} files")
    print(f"journal: {journal_path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
