#!/usr/bin/env python3
"""Generate the reviewed M10 external-dependency manifest from the current diff."""

from __future__ import annotations

import hashlib
import json
from pathlib import Path
import subprocess
import argparse


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def git(*args: str) -> subprocess.CompletedProcess[bytes]:
    return subprocess.run(["git", *args], check=True, capture_output=True)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--pre-worktree", required=True)
    args = parser.parse_args()
    root = Path(git("rev-parse", "--show-toplevel").stdout.decode().strip())
    pre_root = Path(args.pre_worktree).resolve()
    paths = [
        path.decode()
        for path in git(
            "diff",
            "--name-only",
            "--",
            ".",
            ":(exclude)e2e/store_ddd_m10_facade_gate_test.go",
            ":(exclude)scripts/store-ddd/**",
            ":(exclude)docs/2026-10-27-store-ddd-split/impact/**",
        ).stdout.splitlines()
        if path
    ]
    head = git("rev-parse", "HEAD").stdout.decode().strip()
    entries = []
    for relative in paths:
        exists = subprocess.run(
            ["git", "cat-file", "-e", f"HEAD:{relative}"],
            cwd=root,
            check=False,
            capture_output=True,
        ).returncode == 0
        pre_file = pre_root / relative
        pre = sha256(pre_file.read_bytes()) if pre_file.is_file() else None
        if exists and pre is None:
            raise SystemExit(f"pre-worktree is missing checked-out file {relative}")
        target = root / relative
        post = sha256(target.read_bytes()) if target.is_file() else None
        directory = str(Path(relative).parent).replace("\\", "/")
        package = "github.com/RedHuang-0622/seelex"
        if directory != ".":
            package += "/" + directory
        entries.append(
            {
                "path": relative,
                "package": package,
                "side": "test" if relative.endswith("_test.go") else "production",
                "sha256": pre,
                "pre_sha256": pre,
                "post_sha256": post,
                "depends_on": [],
                "depended_on_by": [],
                "operations": [{"kind": "apply_reviewed_patch"}],
                "verify_packages": [package],
            }
        )
    manifest = {
        "schema_version": 1,
        "milestone": "M10",
        "execution": {
            "worktree_branch": "codex/store-ddd-M8-to-M11",
            "base_commit": head,
            "checkpoint_commit": "PENDING_CHECKPOINT",
        },
        "checkpoint": {"commit": "PENDING_CHECKPOINT", "created_at": ""},
        "providers": [
            {
                "package": "github.com/RedHuang-0622/seelex/store",
                "symbols": [
                    "BoardsFor",
                    "BoardsForSession",
                    "TeamworkFor",
                    "ToolRecordFor",
                    "MediaStoreOf",
                ],
                "change": "removed runtime capability discovery; typed Stores bundle injected by composition root",
            }
        ],
        "files": entries,
        "summary": {
            "production_files": sum(entry["side"] == "production" for entry in entries),
            "test_files": sum(entry["side"] == "test" for entry in entries),
            "packages": len({entry["package"] for entry in entries}),
            "direct_edges": 0,
            "reverse_edges": 0,
        },
    }
    path = root / "docs/2026-10-27-store-ddd-split/impact/M10-external-deps.json"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
