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
#
# 除二进制外还同步**包内配置**（见下方 sync_package_config）：dev GUI 包自带
# 一份 config/，不同步就会冻结在最后一次 build-gui.ps1 的那天。
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

# ── 包内配置同步 ────────────────────────────────────────────────────────────
# 为什么要有这一步：GUI/CLI 都按 **CWD 相对路径** 读 config/seelex.yaml（main.go
# 的 firstExisting("config/seelex.yaml", "seelex.yaml")），而 dev 包自带一份
# config/。只重建 exe、不同步配置，包内那份就冻结在"上一次跑 build-gui.ps1 的
# 那天"：仓库里改的 limits 与权限规则在运行中的 GUI 上看起来"完全没生效"。
# 2026-09-29 的折叠厚摘要开关就是这么被吞掉的——仓库 config/seelex.yaml 17:23
# 改成 enabled: true，包内那份还是 09-26 的 7067 字节旧档，连这个键都没有，
# 于是折叠照旧走本地确定性摘要（排查记录见
# docs/devlog/2026-09-29-dev-package-config-drift.md）。
#
# 只同步这两个文件。accounts.yaml 是本地凭据（构建时由 -LocalConfigPath 指定，
# 包内那份可能已被用户在界面上改过），脚本一律不碰。
sync_package_config() {
  local dest="$ROOT/dist/seelex-gui-dev/config" name src
  mkdir -p "$dest"
  for name in seelex.yaml seele.yaml; do
    src="$ROOT/config/$name"
    if [[ ! -f "$src" ]]; then
      continue
    fi
    if [[ -f "$dest/$name" ]] && cmp -s "$src" "$dest/$name"; then
      continue
    fi
    cp -f "$src" "$dest/$name"
    echo "[build-dev] config: $name -> dist/seelex-gui-dev/config/"
  done
}
sync_package_config

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
