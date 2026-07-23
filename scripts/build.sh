#!/usr/bin/env bash
# Seelex 构建与打包脚本
# 使用: ./scripts/build.sh [版本号]
set -euo pipefail

VERSION="${1:-dev}"
ARCHIVE_VERSION="${VERSION#v}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST="$ROOT/dist"

# 颜色
GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[0;33m'
RED='\033[0;31m'
NC='\033[0m'

# ─── 目标平台 ────────────────────────────────────────
declare -A TARGETS
TARGETS=(
    ["windows/amd64"]=".exe"
    ["linux/amd64"]=""
    ["darwin/amd64"]=""
    ["darwin/arm64"]=""
)

# ─── 清理 ────────────────────────────────────────────
if [ -d "$DIST" ]; then
    echo -e "${YELLOW}[clean] 清理 $DIST${NC}"
    rm -rf "$DIST"
fi

echo -e "${CYAN}[build] 版本: $VERSION${NC}"

# ─── 构建每个目标 ────────────────────────────────────
for platform in "${!TARGETS[@]}"; do
    os="${platform%/*}"
    arch="${platform#*/}"
    ext="${TARGETS[$platform]}"
    name="seelex${ext}"
    outdir="$DIST/${os}-${arch}"
    binpath="$outdir/$name"

    echo -e "${GREEN}[build] GOOS=$os GOARCH=$arch -> $binpath${NC}"

    mkdir -p "$outdir"

    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
        go build -trimpath -ldflags="-s -w -X main.Version=$VERSION" -o "$binpath" .

    # ─── 复制运行时文件 ───────────────────────────────
    echo -e "  [copy] 运行时文件 -> $outdir"
    mkdir -p "$outdir/config"
    cp "$ROOT/config/accounts.example.yaml" "$outdir/config/"
    cp -r "$ROOT/plugins" "$outdir/"
    cp "$ROOT/seele.yaml" "$outdir/"
    cp "$ROOT/LICENSE" "$ROOT/CHANGELOG.md" "$ROOT/README.md" "$outdir/"

    size=$(du -h "$binpath" | cut -f1)
    echo -e "${GREEN}[ok]   $os/$arch 完成 ($size)${NC}"
done

# ─── 归档 ────────────────────────────────────────────
echo ""
echo -e "${CYAN}[pack] 生成 .tar.gz 归档...${NC}"

for platform in "${!TARGETS[@]}"; do
    os="${platform%/*}"
    arch="${platform#*/}"
    src="$DIST/${os}-${arch}"
    dirname="seelex-v${ARCHIVE_VERSION}-${os}-${arch}"
    archive="$DIST/${dirname}.tar.gz"

    echo -e "  [tar] $archive"

    cp -r "$src" "$DIST/$dirname"
    tar -czf "$archive" -C "$DIST" "$dirname"
    rm -rf "$DIST/$dirname"
done

# ─── 摘要 ────────────────────────────────────────────
echo ""
echo -e "${CYAN}=== 打包完成 ===${NC}"
echo -e "输出目录: $DIST"
find "$DIST" -type f -exec ls -lh {} \; | awk '{print "  " $NF " (" $5 ")"}'
