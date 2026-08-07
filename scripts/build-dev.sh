#!/usr/bin/env bash
# Build dev binaries into dist/ after each commit (post-commit hook entry).
#   - CLI:  dist/seelex-dev.exe            (TUI, DefaultFrontend=tui)
#   - GUI:  dist/seelex-gui-dev/seelex-gui.exe (desktop, DefaultFrontend=gui)
# Skip with: SKIP_BUILD=1 (e.g. quick commits) or SKIP_BUILD_GUI=1 (CLI only).
# Worktree skip: core.hooksPath 跨 worktree 共享，子代理在独立 worktree 里
# commit 也会触发本钩子；非主 worktree 一律跳过（避免为子代理现场重建
# 32MB+35MB 二进制拖垮收尾/超时）。
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

mkdir -p dist

echo "[build-dev] CLI -> dist/seelex-dev.exe"
go build -trimpath -ldflags "-s -w -X main.Version=$VERSION" -o dist/seelex-dev.exe .

if [[ "${SKIP_BUILD_GUI:-0}" != "1" ]]; then
  echo "[build-dev] GUI -> dist/seelex-gui-dev/seelex-gui.exe"
  mkdir -p dist/seelex-gui-dev
  go build -tags "gui,desktop,production" -trimpath \
    -ldflags "-s -w -H windowsgui -X main.Version=$VERSION -X main.DefaultFrontend=gui" \
    -o dist/seelex-gui-dev/seelex-gui.exe .
fi

echo "[build-dev] done: dist/seelex-dev.exe + dist/seelex-gui-dev/seelex-gui.exe"
