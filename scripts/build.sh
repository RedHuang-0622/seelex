#!/usr/bin/env bash
# ============================================================================
# Seelex cross-platform CLI build and package script (POSIX).
# Usage: ./scripts/build.sh [version] [--clean-dev]
# ----------------------------------------------------------------------------
# Canonical layout (single source of truth: scripts/build-layout.ps1, mirror
# table in .claude/build-convention.md):
#   binaries + runtime -> dist/<os>-<arch>/
#   archives + sha256  -> dist/archive/
# The dev GUI baseline partition dist/seelex-gui-dev/ (user data) is NEVER
# cleaned unless --clean-dev is passed explicitly.
# ============================================================================
set -euo pipefail

VERSION="${1:-dev}"
CLEAN_DEV="${2:-}"
ARCHIVE_VERSION="${VERSION#v}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
DIST="$ROOT/dist"
ARCHIVE="$DIST/archive"

# colors
GREEN='\033[0;32m'; CYAN='\033[0;36m'; YELLOW='\033[0;33m'
RED='\033[0;31m'; NC='\033[0m'

declare -A TARGETS
TARGETS=(
    ["windows/amd64"]=".exe"
    ["linux/amd64"]=""
    ["darwin/amd64"]=""
    ["darwin/arm64"]=""
)

# ---- clean (derived partitions only; dev GUI baseline preserved) ----------
if [ -d "$DIST" ]; then
    for platform in "${!TARGETS[@]}"; do
        os="${platform%/*}"; arch="${platform#*/}"
        rm -rf "$DIST/${os}-${arch}"
    done
    rm -rf "$ARCHIVE"
    if [ -d "$DIST/seelex-gui-dev" ]; then
        if [ "$CLEAN_DEV" = "--clean-dev" ]; then
            echo -e "${YELLOW}[clean] removing $DIST/seelex-gui-dev (explicit --clean-dev)${NC}"
            rm -rf "$DIST/seelex-gui-dev"
        else
            echo -e "${YELLOW}[clean] keeping $DIST/seelex-gui-dev (dev GUI baseline contains user data; use --clean-dev to remove)${NC}"
        fi
    fi
fi

echo -e "${CYAN}[build] version: $VERSION${NC}"

# ---- build each platform tree ---------------------------------------------
for platform in "${!TARGETS[@]}"; do
    os="${platform%/*}"; arch="${platform#*/}"
    ext="${TARGETS[$platform]}"
    outdir="$DIST/${os}-${arch}"
    binpath="$outdir/seelex${ext}"

    echo -e "${GREEN}[build] GOOS=$os GOARCH=$arch -> $binpath${NC}"
    mkdir -p "$outdir"

    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
        go build -trimpath -ldflags="-s -w -X github.com/RedHuang-0622/seelex/internal/buildinfo.Version=$VERSION" -o "$binpath" .

    # runtime files: example config + permission/runtime yaml live under config/
    mkdir -p "$outdir/config"
    cp "$ROOT/config/accounts.example.yaml" "$outdir/config/"
    cp "$ROOT/config/README.md" "$outdir/config/"
    cp "$ROOT/config/seele.yaml" "$ROOT/config/seelex.yaml" "$outdir/config/"
    cp -r "$ROOT/plugins" "$outdir/"
    cp "$ROOT/LICENSE" "$ROOT/CHANGELOG.md" "$ROOT/README.md" "$outdir/"
    [ ! -f "$ROOT/README_EN.md" ] || cp "$ROOT/README_EN.md" "$outdir/"

    echo -e "${GREEN}[ok]   $os/$arch done${NC}"
done

# ---- archive into dist/archive ---------------------------------------------
echo ""
echo -e "${CYAN}[pack] generating archives into $ARCHIVE${NC}"
mkdir -p "$ARCHIVE"

for platform in "${!TARGETS[@]}"; do
    os="${platform%/*}"; arch="${platform#*/}"
    src="$DIST/${os}-${arch}"
    dirname="seelex-v${ARCHIVE_VERSION}-${os}-${arch}"
    archive="$ARCHIVE/${dirname}.tar.gz"

    echo -e "${YELLOW}[tar] $archive${NC}"
    cp -r "$src" "$ARCHIVE/$dirname"
    tar -czf "$archive" -C "$ARCHIVE" "$dirname"
    rm -rf "$ARCHIVE/$dirname"
    if command -v sha256sum >/dev/null 2>&1; then
        (cd "$ARCHIVE" && sha256sum "${dirname}.tar.gz" > "${dirname}.tar.gz.sha256")
    fi
done

echo -e "${CYAN}=== build complete ===${NC}"
echo "platform trees: $DIST/<os>-<arch>/"
echo "archives:       $ARCHIVE"
