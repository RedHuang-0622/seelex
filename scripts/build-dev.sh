#!/usr/bin/env bash
# ============================================================================
# Build dev binaries after each commit (post-commit hook entry).
# Canonical output partitions (see scripts/build-layout.ps1 /
# .claude/build-convention.md):
#   CLI -> dist/dev/seelex.exe                (P4 local quick builds)
#   GUI -> dist/seelex-gui-dev/seelex-gui.exe (P2 dev GUI baseline)
# Skip with: SKIP_BUILD=1 (e.g. quick commits) or SKIP_BUILD_GUI=1 (CLI only).
# Linked worktrees (subagent forks) are skipped: they share core.hooksPath and
# must not rebuild two 30MB+ binaries on every commit.
# ============================================================================
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
VERSION="${SEELEX_DEV_VERSION:-dev}"

if [[ -d .git ]] && command -v git >/dev/null 2>&1; then
  MAIN_ROOT="$(git worktree list --porcelain 2>/dev/null | awk '/^worktree /{print $2; exit}')"
  if [[ -n "$MAIN_ROOT" && "$(git rev-parse --show-toplevel 2>/dev/null)" != "$MAIN_ROOT" ]]; then
    echo "[build-dev] skipping: commit is inside a linked worktree ($(git rev-parse --show-toplevel))"
    exit 0
  fi
fi

mkdir -p dist/dev

echo "[build-dev] CLI -> dist/dev/seelex.exe"
go build -trimpath -ldflags "-s -w -X github.com/RedHuang-0622/seelex/internal/buildinfo.Version=$VERSION" -o dist/dev/seelex.exe .

if [[ "${SKIP_BUILD_GUI:-0}" != "1" ]]; then
  echo "[build-dev] GUI -> dist/seelex-gui-dev/seelex-gui.exe"
  mkdir -p dist/seelex-gui-dev
  go build -tags "gui,desktop,production" -trimpath \
    -ldflags "-s -w -H windowsgui -X github.com/RedHuang-0622/seelex/internal/buildinfo.Version=$VERSION -X github.com/RedHuang-0622/seelex/internal/buildinfo.DefaultFrontend=gui" \
    -o dist/seelex-gui-dev/seelex-gui.exe .
fi

echo "[build-dev] done: dist/dev/seelex.exe + dist/seelex-gui-dev/seelex-gui.exe"
