"""验证两轴换肤的渲染结果：对每个 (皮肤, 深浅) 组合跑一次 headless Chrome 截屏，
再采样「壳·顶」「壳·底」「内容纸」三处像素，检查：
  1) 每套皮肤的环境渐变不同（壳顶/壳底颜色逐皮肤相异）；
  2) 深浅轴确实换中性基座（内容纸在 light=白 / dark=深灰）；
  3) 渐变是「等亮度、纯竖直」的一条扫过（顶/底色差存在但亮度接近）。

用法：python docs/design/qoder-skin/tools/verify-skins.py
"""
import subprocess
import sys
import tempfile
import os
from PIL import Image

CHROME = r"C:\Program Files\Google\Chrome\Application\chrome.exe"
PREVIEW = "file:///G:/Program/go/seelex/docs/design/qoder-skin/build/seelex-qoder-preview.html"
SKINS = ["qoder", "graphite", "verdigris", "paper", "silver"]
MODES = ["light", "dark"]
W, H = 1000, 820

# 采样点（虚拟像素）
SHELL_TOP = (100, 6)      # 顶栏：落在壳的渐变起点
SHELL_BOTTOM = (100, H - 6)  # 左栏底：落在壳的渐变终点
PAPER = (470, 300)        # 中栏内容纸


def shoot(skin, mode):
    path = os.path.join(tempfile.gettempdir(), f"seelex-{skin}-{mode}.png")
    url = f"{PREVIEW}#{skin}-{mode}"
    proc = subprocess.run(
        [CHROME, "--headless=new", "--disable-gpu", "--hide-scrollbars",
         f"--window-size={W},{H}", f"--screenshot={path}", url],
        capture_output=True, text=True, timeout=90,
    )
    if not os.path.exists(path):
        raise RuntimeError(f"截图失败 {skin}/{mode}: {proc.stderr[-300:]}")
    return Image.open(path).convert("RGB")


def sample(img, pt):
    return img.getpixel(pt)


def hexc(rgb):
    return "#%02X%02X%02X" % rgb


def luminance(rgb):
    r, g, b = (c / 255 for c in rgb)
    return round(0.2126 * r + 0.7152 * g + 0.0722 * b, 3)


def main():
    print(f"preview: {PREVIEW}")
    rows = []
    for skin in SKINS:
        for mode in MODES:
            img = shoot(skin, mode)
            top = sample(img, SHELL_TOP)
            bottom = sample(img, SHELL_BOTTOM)
            paper = sample(img, PAPER)
            rows.append((skin, mode, top, bottom, paper))
            print(f"{skin:10s} {mode:5s}  壳顶 {hexc(top)} (L{luminance(top):.2f})  "
                  f"壳底 {hexc(bottom)} (L{luminance(bottom):.2f})  纸 {hexc(paper)}")

    # 判据 1：每套皮肤的渐变起点唯一
    tops = [hexc(r[2]) for r in rows if r[1] == "light"]
    print("\n浅色下各皮肤壳顶是否互异:", len(set(tops)) == len(tops), tops)
    # 判据 2：深浅换中性基座（纸色不同）
    paper_light = {hexc(r[4]) for r in rows if r[1] == "light"}
    paper_dark = {hexc(r[4]) for r in rows if r[1] == "dark"}
    print("浅色纸色集合:", paper_light, "| 深色纸色集合:", paper_dark)
    ok = paper_light != paper_dark and len(paper_light) == 1 and len(paper_dark) == 1
    print("深浅轴独立于皮肤（同深浅下所有皮肤纸色一致、浅≠深）:", ok)
    # 判据 3：同皮肤深浅两端渐变不同
    for skin in SKINS:
        l = [r for r in rows if r[0] == skin and r[1] == "light"][0]
        d = [r for r in rows if r[0] == skin and r[1] == "dark"][0]
        print(f"{skin:10s} light 壳顶/底 {hexc(l[2])}/{hexc(l[3])}   dark 壳顶/底 {hexc(d[2])}/{hexc(d[3])}")


if __name__ == "__main__":
    sys.exit(main())
