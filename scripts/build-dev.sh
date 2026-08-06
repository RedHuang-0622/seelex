#!/usr/bin/env bash
# Build dev binaries into dist/ after each commit (post-commit hook entry).
#   - CLI:  dist/seelex-dev.exe            (TUI, DefaultFrontend=tui)
#   - GUI:  dist/seelex-gui-dev/seelex-gui.exe (desktop, DefaultFrontend=gui)
# Skip with: SKIP_BUILD=1 (e.g. quick commits) or SKIP_BUILD_GUI=1 (CLI only).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
VERSION="${SEELEX_DEV_VERSION:-dev}"

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
