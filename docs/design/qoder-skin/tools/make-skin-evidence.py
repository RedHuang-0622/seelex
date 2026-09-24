"""把 verify-skins.py 产出的 10 张渲染图整理成证据：
  · refs/replica-seelex-light.png / -dark.png —— 默认皮肤（qoder）× 深浅的复刻页截图；
  · refs/skins-gradient-matrix.png —— 5 皮肤 × 2 深浅 的渐变色对照矩阵。
图来自 headless Chrome 对 docs/design/qoder-skin/build/seelex-qoder-preview.html 的渲染。
用法：python docs/design/qoder-skin/tools/make-skin-evidence.py
"""
import os
import shutil
import tempfile
from PIL import Image, ImageDraw

SKINS = ["qoder", "graphite", "verdigris", "paper", "silver"]
MODES = ["light", "dark"]
HERE = os.path.dirname(os.path.abspath(__file__))
REFS = os.path.normpath(os.path.join(HERE, "..", "refs"))
TMP = tempfile.gettempdir()

thumbs = {}
for skin in SKINS:
    for mode in MODES:
        src = os.path.join(TMP, f"seelex-{skin}-{mode}.png")
        if not os.path.exists(src):
            raise SystemExit(f"缺少渲染图 {src}，先跑 tools/verify-skins.py")
        img = Image.open(src).convert("RGB")
        thumbs[(skin, mode)] = img

# 默认皮肤 × 深浅 → 复刻页截图（与 README 的 refs 表对应）
for mode, name in (("light", "replica-seelex-light.png"), ("dark", "replica-seelex-dark.png")):
    thumbs[("qoder", mode)].save(os.path.join(REFS, name))

# 5 × 2 对照矩阵
TW, TH = 500, 410
GAP, LABEL_H = 8, 0
W = TW * len(MODES) + GAP * (len(MODES) + 1)
H = TH * len(SKINS) + GAP * (len(SKINS) + 1)
sheet = Image.new("RGB", (W, H), (30, 30, 30))
for row, skin in enumerate(SKINS):
    for col, mode in enumerate(MODES):
        t = thumbs[(skin, mode)].resize((TW, TH))
        x = GAP + col * (TW + GAP)
        y = GAP + row * (TH + GAP)
        sheet.paste(t, (x, y))
sheet.save(os.path.join(REFS, "skins-gradient-matrix.png"))
print("rows = skins (qoder/graphite/verdigris/paper/silver), cols = light|dark")
print("wrote", os.path.join(REFS, "skins-gradient-matrix.png"))
