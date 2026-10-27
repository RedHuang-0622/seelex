#!/usr/bin/env python3
"""Replay or verify the audited M7 external-consumer patch sequence."""

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


def sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def git(worktree: Path, *args: str, env: dict[str, str] | None = None) -> str:
    return subprocess.check_output(
        ["git", *args], cwd=worktree, text=True, env=env
    ).strip()


def write_atomic(path: Path, data: bytes) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(dir=path.parent, delete=False) as handle:
        handle.write(data)
        temporary = Path(handle.name)
    os.replace(temporary, path)


def checked_files(worktree: Path, manifest: dict) -> list[Path]:
    paths: list[Path] = []
    for entry in manifest["files"]:
        path = worktree / entry["path"]
        paths.append(path)
    return paths


def patch_paths(worktree: Path, manifest: dict) -> list[Path]:
    paths: list[Path] = []
    for batch in manifest["batches"]:
        path = worktree / batch["patch"]
        if not path.is_file() or sha256(path) != batch["patch_sha256"]:
            raise SystemExit(f"patch hash mismatch: {batch['patch']}")
        paths.append(path)
    return paths


def dry_run(worktree: Path, manifest: dict, patches: list[Path]) -> None:
    with tempfile.NamedTemporaryFile(delete=False) as handle:
        index_path = Path(handle.name)
    index_path.unlink()
    env = os.environ.copy()
    env["GIT_INDEX_FILE"] = str(index_path)
    try:
        git(worktree, "read-tree", manifest["execution"]["checkpoint_commit"], env=env)
        for patch in patches:
            subprocess.run(
                ["git", "apply", "--cached", "--check", str(patch)],
                cwd=worktree,
                check=True,
                env=env,
            )
            subprocess.run(
                ["git", "apply", "--cached", str(patch)],
                cwd=worktree,
                check=True,
                env=env,
            )
            print(f"dry-run patch OK: {patch.relative_to(worktree)}")
    finally:
        index_path.unlink(missing_ok=True)


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
    if Path(git(worktree, "rev-parse", "--show-toplevel")).resolve() != worktree:
        raise SystemExit("worktree root mismatch")
    branch = git(worktree, "branch", "--show-current")
    if branch != args.branch or not branch.startswith("codex/store-ddd-"):
        raise SystemExit(f"branch mismatch: {branch!r}")

    manifest_path = worktree / args.manifest
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    if manifest["execution"]["checkpoint_commit"] != args.checkpoint:
        raise SystemExit("manifest checkpoint mismatch")

    if args.rollback:
        journal = json.loads(Path(args.rollback).read_text(encoding="utf-8"))
        for entry in journal["files"]:
            path = worktree / entry["path"]
            if entry["existed"]:
                write_atomic(path, bytes.fromhex(entry["backup_hex"]))
                if sha256(path) != entry["before_sha256"]:
                    raise SystemExit(f"rollback hash mismatch: {entry['path']}")
            else:
                path.unlink(missing_ok=True)
        print(f"rolled back {len(journal['files'])} files")
        return 0

    if git(worktree, "rev-parse", "HEAD") != args.checkpoint:
        raise SystemExit("HEAD does not match checkpoint")
    files = checked_files(worktree, manifest)
    patches = patch_paths(worktree, manifest)
    if args.dry_run:
        dry_run(worktree, manifest, patches)
        print("dry-run: no files written")
        return 0

    backups = {path: path.read_bytes() if path.is_file() else None for path in files}
    journal = {
        "worktree": str(worktree),
        "branch": branch,
        "checkpoint": args.checkpoint,
        "created_at": datetime.now(timezone.utc).isoformat(),
        "files": [
            {
                "path": path.relative_to(worktree).as_posix(),
                "existed": backups[path] is not None,
                "before_sha256": sha256(path) if backups[path] is not None else "",
                "backup_hex": backups[path].hex() if backups[path] is not None else "",
            }
            for path in files
        ],
    }
    journal_path = Path(tempfile.gettempdir()) / f"seelex-store-ddd-M7-{os.getpid()}.json"
    journal_path.write_text(json.dumps(journal, indent=2), encoding="utf-8")

    try:
        for patch in patches:
            subprocess.run(
                ["git", "apply", "--whitespace=nowarn", str(patch)],
                cwd=worktree,
                check=True,
            )
        go_files = [str(path) for path in files if path.suffix == ".go"]
        if go_files:
            subprocess.run(["gofmt", "-w", *go_files], cwd=worktree, check=True)
    except Exception as exc:
        for path, data in backups.items():
            if data is None:
                path.unlink(missing_ok=True)
            else:
                write_atomic(path, data)
        print(f"apply failed and was rolled back: {exc}", file=sys.stderr)
        print(f"journal: {journal_path}", file=sys.stderr)
        return 1

    print(f"applied {len(patches)} patches across {len(files)} files")
    print(f"journal: {journal_path}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
