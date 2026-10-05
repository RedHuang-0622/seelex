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

# ── 包内插件同步 ────────────────────────────────────────────────────────────
# 与 config/ 同一条口径（2026-10-03 补齐）：运行中的 dev GUI 按 **CWD 相对路径**
# 读 plugins/，只重建 exe、不同步 plugins/，包内那份就冻结在"上一次跑
# build-gui.ps1 的那天"——仓库里改的 skill / plugin.md 口径在运行中的 app 上
# 看起来"完全没生效"。
#
# 实例（2026-10-03）：teamwork 的 SKILL.md 改成「里程碑 + Work Item」口径之后，
# `$teamwork` 召回的仍是阶段制纪律——包内副本停在 21:07，注入的是旧文本。
# 与 2026-09-29 那次 config 漂移是同一个坑，只是换了个目录。
#
# 整树镜像（rm -rf + cp -r，与 build-linux-gui.sh 同口径）：plugins/ 是**构建
# 产物**，仓库才是唯一事实；只 cp 不删会让仓库里已删掉的插件在包里阴魂不散。
# 只在整树确有差异时才动（diff -rq 兜底：没有 diff 就老实镜像）。
sync_package_plugins() {
  local src="$ROOT/plugins" dest="$ROOT/dist/seelex-gui-dev/plugins"
  if [[ ! -d "$src" ]]; then
    return
  fi
  # 判据只看"仓库里有的，包里是不是一致的"：包内**多出来**的目录不算差异
  # （那可能是本机自加、不入库的插件，见下面的"只覆盖不删除"）。
  #
  # 2026-10-05 修：这里原来是
  #   ! diff -rq "$src" "$dest" | grep -qv "Only in $dest"
  # 本脚本第 15 行是 `set -o pipefail`，管道退出码取**最右非零成员**，于是这句取反
  # 读到的是 diff 的 1（"有差异"），恒为真 ⇒ 每次都 early return，同步**从未真正执行过**。
  # 后果（证据链）：运行树 dist/seelex-gui-dev/plugins/ 里的文件带着仓库侧源文件的 mtime
  # （bash `cp -r` 不保 mtime，实测；PowerShell `Copy-Item` 保），说明那份载荷是别的
  # 途径写进去的、此后一直冻结；仓库 10-05 02:23 的提交（94f7ae4，正是把 curated.yaml
  # 加进仓库的那次）跑过本脚本（dist/dev 与 dist/seelex-gui-dev 两个 exe 的 mtime 可证），
  # 但包内那份 curated.yaml 至今缺席，启动期因此报"精选目录读不到"。
  # 现在改成不依赖管道退出码的逐文件比对（有差异才覆盖，语义与原来一致）。
  local stale=1 file rel
  if [[ -d "$dest" ]]; then
    stale=0
    while IFS= read -r -d '' file; do
      rel="${file#"$src"/}"
      if [[ ! -f "$dest/$rel" ]] || ! cmp -s "$file" "$dest/$rel"; then
        stale=1
        break
      fi
    done < <(find "$src" -type f -print0)
  fi
  if [[ "$stale" == "0" ]]; then
    return
  fi
  # **只覆盖、不删除**（2026-10-03 修正）：plugins/ 里除了入库的插件，还可能放
  # 着**本机自加、不入库**的试验插件（它们不在 git 里，仓库也就无从声明它们）。
  # 整树镜像（rm -rf）会把这类插件一并抹掉——那是替使用者销毁现场。
  # 代价是仓库里已删除的插件可能在包里残留（包是构建产物，残留可接受）。
  mkdir -p "$dest"
  cp -r "$src/." "$dest/"
  echo "[build-dev] plugins: plugins/ -> dist/seelex-gui-dev/plugins/（只覆盖，不删包内自加目录）"
}
sync_package_plugins

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
