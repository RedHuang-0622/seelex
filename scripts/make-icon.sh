#!/usr/bin/env bash
# ============================================================================
# Seelex 图标资源：从品牌 logo 生成 Windows 图标（.ico）与链接用的 .syso。
#
#   gui/icons/seelex-logo.png   ← 品牌源图（唯一事实，改图标只改这里）
#   gui/icons/seelex.ico        ← 多尺寸 ICO（16/24/32/48/64/128/256），入库
#   gui/frontend/dist/assets/seelex-logo.png ← 前端品牌位用的小图（128，入库）
#   rsrc_windows_amd64.syso     ← windres 产物，仓库根目录，Go 在 windows/amd64
#                                 构建时自动链接（该目标会写进 exe 的图标资源，
#                                 快捷方式 / 任务栏 / 资源管理器都取它）
#
# 用法：
#   scripts/make-icon.sh              重新生成 .ico、前端小图与 .syso
#   scripts/make-icon.sh --syso-only  只重建 .syso（.ico 已入库时用，构建脚本走这条）
#
# 依赖与降级：
#   - .ico 需要 Python + Pillow（只在本机改品牌图时才需要；.ico 已入库，构建不需要）；
#   - .syso 需要 windres（MinGW binutils）。没有 windres 时**跳过并说明**，不让构建失败
#     ——图标是表现层资源，缺它不该挡住任何平台的构建。
# ============================================================================
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

SRC="gui/icons/seelex-logo.png"
ICO="gui/icons/seelex.ico"
RC="gui/icons/icon.rc"
WEB="gui/frontend/dist/assets/seelex-logo.png"
SYSO="rsrc_windows_amd64.syso"

MODE="${1:-}"

if [[ "$MODE" != "--syso-only" ]]; then
  if [[ ! -f "$SRC" ]]; then
    echo "[icon] missing $SRC (品牌源图入库后才能重建图标)" >&2
    exit 1
  fi
  if ! command -v python >/dev/null 2>&1; then
    echo "[icon] python not found: 跳过 .ico 重建（已入库的 $ICO 继续用）" >&2
  else
    python - "$SRC" "$ICO" "$WEB" <<'PY'
import sys
from PIL import Image

src_path, ico_path, web_path = sys.argv[1], sys.argv[2], sys.argv[3]
image = Image.open(src_path).convert("RGBA")

# 源图四周有透明留白（品牌 canvas 比图形本身大一圈）：先按 alpha 裁到图形，再补 4%
# 边距并摆到正方形画布上。不裁的话，26px 的品牌位会显示成一个缩小的白块，
# 图标也会在任务栏/资源管理器里显得比同类应用小一圈。
bbox = image.getchannel("A").getbbox()
if bbox:
    image = image.crop(bbox)
side = max(image.size)
pad = max(1, int(side * 0.04))
canvas = Image.new("RGBA", (side + 2 * pad, side + 2 * pad), (0, 0, 0, 0))
canvas.paste(image, ((canvas.width - image.width) // 2, (canvas.height - image.height) // 2))
image = canvas

# 资源管理器/任务栏/标题栏会各取一档：小尺寸给 16/24/32/48，高 DPI 给 64/128/256。
image.save(ico_path, sizes=[(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)])
# 前端品牌位显示 26px：128 给 HiDPI 留 4x 余量，体积只有几 KB。
image.resize((128, 128), Image.LANCZOS).save(web_path)
print(f"[icon] wrote {ico_path} + {web_path} (trimmed from {src_path})")
PY
  fi
fi

if ! command -v windres >/dev/null 2>&1; then
  echo "[icon] windres not found: 跳过 $SYSO（exe 将没有自定义图标，其余功能不受影响）"
  exit 0
fi

windres "$RC" -O coff -o "$SYSO" --include-dir "$(dirname "$RC")"
echo "[icon] wrote $SYSO"
