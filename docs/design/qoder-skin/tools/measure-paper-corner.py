"""量「内容纸」的圆角：给定纸上的一点（左上角）与右/下边界，逐行扫出纸色起点，
得到角的剖面（= 半径），用于把实机与自家渲染放在同一把尺子上比。

这与 `measure-skin.py` 的 `radius` 子命令同族（同一套"逐行找第一个纸色像素"的量法），
差别只是这里只需要一个角：圆角是"圆弧里露出壳"，所以判据是
**首行的偏移 = 半径**，且偏移逐行递减到 0，纸的左边界不再往里缩（不缩进）。

用法：
    python docs/design/qoder-skin/tools/measure-paper-corner.py <png> <left> <top> <right> <bottom>

例（Qoder 实机，纸的左上角在 (316,56)）：
    python docs/design/qoder-skin/tools/measure-paper-corner.py docs/design/qoder-skin/refs/qoder-light-clean.png 316 56 1913 1076
    # 顶左 y=56 偏移 7、y=63 归 0 → 半径 ≈7px

例（本产品真前端 1600×1000 渲染，中栏纸在 (274,52)-(1293,999)）：
    python docs/design/qoder-skin/tools/measure-paper-corner.py tmp/front-after.png 274 52 1293 999
    # 顶左 y=52 偏移 7、y=59 归 0 → 半径 8px（= --r-md）
"""
import sys
from PIL import Image


def is_paper(px, tol=6):
    r, g, b = px[:3]
    return abs(r - 255) <= tol and abs(g - 255) <= tol and abs(b - 255) <= tol


def first_paper_x(img, left, y, span=40):
    for x in range(max(0, left - 16), left + span):
        if is_paper(img.getpixel((x, y))):
            return x
    return None


def main():
    path, left, top, right, bottom = sys.argv[1], *map(int, sys.argv[2:6])
    img = Image.open(path).convert("RGB")
    w, h = img.size
    top = max(0, min(top, h - 1))
    bottom = max(0, min(bottom, h - 1))
    right = max(0, min(right, w - 1))
    print(f"{path}  size={img.size}  paper box=({left},{top})-({right},{bottom})")

    print("--- 左上角：逐行第一个纸色 x（首行偏移 = 半径） ---")
    for y in range(max(0, top - 2), min(h, top + 12)):
        x = first_paper_x(img, left, y)
        print(f"  y={y:5d}  first_paper_x={x}  offset={(x - left) if x is not None else None}")

    print("--- 左下角：逐行第一个纸色 x ---")
    for y in range(max(0, bottom - 12), min(h, bottom + 2)):
        x = first_paper_x(img, left, y)
        print(f"  y={y:5d}  first_paper_x={x}  offset={(x - left) if x is not None else None}")

    print("--- 右上角：逐行最后一个纸色 x ---")
    for y in range(max(0, top - 2), min(h, top + 12)):
        last = None
        for x in range(min(img.size[0] - 1, right + 16), max(0, right - 40), -1):
            if is_paper(img.getpixel((x, y))):
                last = x
                break
        print(f"  y={y:5d}  last_paper_x={last}  offset={(right - last) if last is not None else None}")


if __name__ == "__main__":
    main()
