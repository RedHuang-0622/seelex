#!/usr/bin/env python3
"""Apply or roll back the audited M10 facade migration patch."""

from __future__ import annotations

import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time


def fail(message: str) -> "NoReturn":
    print(f"M10 migration: {message}", file=sys.stderr)
    raise SystemExit(2)


def run_git(worktree: Path, *args: str, check: bool = True) -> subprocess.CompletedProcess[str]:
    result = subprocess.run(
        ["git", *args],
        cwd=worktree,
        text=True,
        capture_output=True,
        check=False,
    )
    if check and result.returncode != 0:
        fail(f"git {' '.join(args)} failed:\n{result.stdout}{result.stderr}")
    return result


def sha256(path: Path) -> str | None:
    if not path.exists():
        return None
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def load_manifest(path: Path) -> dict:
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        fail(f"read manifest: {exc}")
    if data.get("schema_version") != 1 or data.get("milestone") != "M10":
        fail("manifest must be schema_version=1 milestone=M10")
    return data


def validate_environment(worktree: Path, branch: str, manifest: dict) -> None:
    if not worktree.is_dir():
        fail(f"worktree does not exist: {worktree}")
    root = Path(run_git(worktree, "rev-parse", "--show-toplevel").stdout.strip()).resolve()
    if root != worktree.resolve():
        fail(f"worktree mismatch: got {root}, want {worktree.resolve()}")
    current_branch = run_git(worktree, "branch", "--show-current").stdout.strip()
    if current_branch != branch or not current_branch.startswith("codex/store-ddd-"):
        fail(f"branch mismatch: got {current_branch!r}, want {branch!r}")
    expected_branch = manifest["execution"]["worktree_branch"]
    if not expected_branch.startswith("codex/store-ddd-"):
        fail(f"manifest branch is not a store-ddd branch: {expected_branch!r}")


def validate_targets(worktree: Path, manifest: dict) -> list[dict]:
    files = manifest.get("files", [])
    if not files:
        fail("manifest files must not be empty")
    for entry in files:
        relative = entry["path"]
        if Path(relative).is_absolute() or ".." in Path(relative).parts:
            fail(f"unsafe manifest path: {relative}")
        if relative.startswith(("store/.seelex/", ".seelex/", "config/", "dist/")):
            fail(f"manifest path targets user data: {relative}")
        path = worktree / relative
        if entry.get("pre_sha256") is not None:
            actual = sha256(path)
            if actual != entry["pre_sha256"]:
                fail(f"stale manifest for {relative}: pre_sha256 {actual} != {entry['pre_sha256']}")
        elif path.exists():
            fail(f"new-file target already exists: {relative}")
    return files


def backup_targets(worktree: Path, files: list[dict], journal_dir: Path) -> dict[str, str | None]:
    backups: dict[str, str | None] = {}
    for entry in files:
        relative = entry["path"]
        source = worktree / relative
        if not source.exists():
            backups[relative] = None
            continue
        backup = journal_dir / relative
        backup.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(source, backup)
        backups[relative] = str(backup)
    return backups


def restore_targets(worktree: Path, files: list[dict], backups: dict[str, str | None]) -> None:
    for entry in files:
        relative = entry["path"]
        target = worktree / relative
        backup = backups.get(relative)
        if backup is None:
            if target.exists():
                target.unlink()
            continue
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(backup, target)


def verify_post_hashes(worktree: Path, files: list[dict]) -> None:
    for entry in files:
        relative = entry["path"]
        actual = sha256(worktree / relative)
        if actual != entry["post_sha256"]:
            fail(f"post-migration hash mismatch for {relative}: {actual} != {entry['post_sha256']}")


def materialize_patch(path: Path) -> tuple[Path, Path | None]:
    if path.suffix != ".b64":
        return path, None
    try:
        payload = base64.b64decode(path.read_bytes(), validate=True)
    except (OSError, ValueError) as exc:
        fail(f"decode patch: {exc}")
    handle = tempfile.NamedTemporaryFile(prefix="seelex-store-ddd-M10-", suffix=".patch", delete=False)
    handle.write(payload)
    handle.close()
    return Path(handle.name), Path(handle.name)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--manifest", required=True)
    parser.add_argument("--checkpoint", required=True)
    parser.add_argument("--worktree", required=True)
    parser.add_argument("--branch", required=True)
    parser.add_argument("--patch", required=True)
    action = parser.add_mutually_exclusive_group(required=True)
    action.add_argument("--dry-run", action="store_true")
    action.add_argument("--apply", action="store_true")
    action.add_argument("--rollback")
    args = parser.parse_args()

    worktree = Path(args.worktree).resolve()
    manifest_path = Path(args.manifest).resolve()
    patch_path = Path(args.patch).resolve()
    manifest = load_manifest(manifest_path)
    validate_environment(worktree, args.branch, manifest)

    if args.rollback:
        journal = Path(args.rollback).resolve()
        try:
            data = json.loads(journal.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as exc:
            fail(f"read rollback journal: {exc}")
        files = data["files"]
        restore_targets(worktree, files, data["backups"])
        for entry in files:
            if sha256(worktree / entry["path"]) != entry["pre_sha256"]:
                fail(f"rollback hash mismatch for {entry['path']}")
        print(json.dumps({"status": "rolled-back", "journal": str(journal)}, ensure_ascii=False))
        return

    head = run_git(worktree, "rev-parse", "HEAD").stdout.strip()
    if head != args.checkpoint:
        fail(f"checkpoint mismatch: HEAD {head} != {args.checkpoint}")
    files = validate_targets(worktree, manifest)
    patch_path, temporary_patch = materialize_patch(patch_path)
    check = run_git(worktree, "apply", "--check", "--whitespace=nowarn", str(patch_path), check=False)
    if check.returncode != 0:
        fail(f"patch does not apply cleanly:\n{check.stdout}{check.stderr}")
    if args.dry_run:
        print(json.dumps({
            "status": "dry-run-ok",
            "checkpoint": head,
            "patch": str(patch_path),
            "targets": [entry["path"] for entry in files],
        }, ensure_ascii=False, indent=2))
        if temporary_patch is not None:
            temporary_patch.unlink(missing_ok=True)
        return

    journal_dir = Path(tempfile.mkdtemp(prefix="seelex-store-ddd-M10-"))
    backups = backup_targets(worktree, files, journal_dir / "backup")
    journal_path = journal_dir / "journal.json"
    journal = {
        "worktree": str(worktree),
        "branch": args.branch,
        "checkpoint": head,
        "created_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "files": files,
        "backups": backups,
        "status": "applying",
    }
    journal_path.write_text(json.dumps(journal, ensure_ascii=False, indent=2), encoding="utf-8")

    applied = run_git(worktree, "apply", "--whitespace=nowarn", str(patch_path), check=False)
    if applied.returncode != 0:
        restore_targets(worktree, files, backups)
        journal["status"] = "failed-restored"
        journal["error"] = applied.stdout + applied.stderr
        journal_path.write_text(json.dumps(journal, ensure_ascii=False, indent=2), encoding="utf-8")
        if temporary_patch is not None:
            temporary_patch.unlink(missing_ok=True)
        fail(f"apply failed; files restored. journal={journal_path}\n{applied.stdout}{applied.stderr}")
    try:
        verify_post_hashes(worktree, files)
    except SystemExit as exc:
        restore_targets(worktree, files, backups)
        journal["status"] = "failed-restored"
        journal["error"] = str(exc)
        journal_path.write_text(json.dumps(journal, ensure_ascii=False, indent=2), encoding="utf-8")
        if temporary_patch is not None:
            temporary_patch.unlink(missing_ok=True)
        raise

    journal["status"] = "applied"
    journal_path.write_text(json.dumps(journal, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps({
        "status": "applied",
        "journal": str(journal_path),
        "checkpoint": head,
        "targets": len(files),
    }, ensure_ascii=False, indent=2))
    if temporary_patch is not None:
        temporary_patch.unlink(missing_ok=True)


if __name__ == "__main__":
    main()
