#!/usr/bin/env python3
"""Qoder 皮肤 · 取色与几何量测工具（验收用）

用法：
  python docs/design/qoder-skin/tools/measure-skin.py gradient  <png> [x] [y0] [y1]
  python docs/design/qoder-skin/tools/measure-skin.py radius    <png> <x0> <y0> <x1> <y1>
  python docs/design/qoder-skin/tools/measure-skin.py edges     <png> <y> <x0> <x1>

为什么要有它：
  「好看」在评审里没法争论，但「渐变顶端是 #EBEBC6」可以。这个脚本把
  视觉口径变成可复现的数字——设计语言确认稿里的每一个数值都出自它。

依赖：Pillow（`pip install pillow`）。无 numpy 依赖。
"""

import sys

try:
    from PIL import Image, ImageStat
except ImportError:  # pragma: no cover
    sys.exit("需要 Pillow：pip install pillow")


def hexof(rgb):
    return "#%02X%02X%02X" % tuple(int(round(v)) for v in rgb)


def hsl(rgb):
    r, g, b = [v / 255.0 for v in rgb]
    mx, mn = max(r, g, b), min(r, g, b)
    l = (mx + mn) / 2
    if mx == mn:
        return 0.0, 0.0, l
    d = mx - mn
    s = d / (2 - mx - mn) if l > 0.5 else d / (mx + mn)
    if mx == r:
        h = ((g - b) / d) % 6
    elif mx == g:
        h = (b - r) / d + 2
    else:
        h = (r - g) / d + 4
    return h * 60, s, l


def patch_mean(im, cx, cy, r=4):
    w, h = im.size
    box = (max(0, cx - r), max(0, cy - r), min(w, cx + r), min(h, cy + r))
    return tuple(ImageStat.Stat(im.crop(box)).mean)


def cmd_gradient(argv):
    """沿一条干净的竖线取渐变控制点，并做线性拟合。

    判据（见 DESIGN-LANGUAGE.md）：同一 y 上横向取三点色差 ≤2；
    拟合直线的 R² 应接近 1（说明是单条线性渐变，不是多层叠加）。
    """
    path = argv[0]
    x = int(argv[1]) if len(argv) > 1 else 270
    y0 = int(argv[2]) if len(argv) > 2 else 4
    y1 = int(argv[3]) if len(argv) > 3 else None
    im = Image.open(path).convert("RGB")
    w, h = im.size
    y1 = y1 if y1 is not None else h - 26

    pts = []
    for y in range(y0, y1, 50):
        c = patch_mean(im, x, y)
        pts.append((y, c))

    print("size %dx%d  column x=%d" % (w, h, x))
    for y, c in pts:
        hh, ss, ll = hsl(c)
        print("  y=%4d  %s  %s   H=%5.0f° S=%4.1f%% L=%4.1f%%" % (y, hexof(c), tuple(round(v) for v in c), hh, ss * 100, ll * 100))

    # 线性拟合 R/G/B 三通道
    ys = [p[0] for p in pts]
    n = len(ys)
    my = sum(ys) / n
    print("\n线性拟合（斜率/px，截距=第 0 行颜色）：")
    ends = []
    for i in range(3):
        vs = [p[1][i] for p in pts]
        mv = sum(vs) / n
        num = sum((ys[k] - my) * (vs[k] - mv) for k in range(n))
        den = sum((ys[k] - my) ** 2 for k in range(n))
        k = num / den
        b = mv - k * my
        ss_res = sum((vs[j] - (k * ys[j] + b)) ** 2 for j in range(n))
        ss_tot = sum((v - mv) ** 2 for v in vs)
        r2 = 1 - ss_res / ss_tot if ss_tot else 1
        print("   ch%d: slope=%+.5f  y0=%7.2f  R²=%.4f" % (i, k, b, r2))
        ends.append((b, k))

    top = [ends[i][0] for i in range(3)]
    bot = [ends[i][0] + ends[i][1] * (h - 1) for i in range(3)]
    print("\n  顶端(y=0)   %s   %s" % (hexof(top), hsl(top)))
    print("  底端(y=%d) %s   %s" % (h - 1, hexof(bot), hsl(bot)))
    print("  中点       %s" % hexof([(top[i] + bot[i]) / 2 for i in range(3)]))
    print("\n横向不变性检查（同 y、不同 x）：")
    for y in (120, 600, 1000):
        cs = [patch_mean(im, xx, y, 3) for xx in (8, 120, 240)]
        spread = max(max(c[i] for c in cs) - min(c[i] for c in cs) for i in range(3))
        print("   y=%4d %s  最大通道差=%.1f  %s" % (y, [hexof(c) for c in cs], spread, "OK" if spread <= 2 else "偏大"))


def cmd_radius(argv):
    """量一个圆角矩形控件的圆角半径。

    做法：以 (x0,y0) 角落像素为"背景色"，找出这个矩形控件的上边线起点与左边线位置，
    两者之差 ≈ 半径。比"看着像 16px"可靠。浅底描边控件与深色实心控件都适用
    （只要给出的 (x0,y0) 落在控件之外的背景上）。
    """
    path, x0, y0, x1, y1 = argv[0], *[int(v) for v in argv[1:5]]
    im = Image.open(path).convert("RGB")
    px = im.load()
    bg = px[x0, y0]

    def is_ink(x, y):
        c = px[x, y]
        return max(abs(c[i] - bg[i]) for i in range(3)) > 6

    # 圆角的两种独立估计，互相校验：
    #   ① 纵向：从"顶行"到"墨段首次达到满宽"的行距 ≈ 半径
    #      （圆角矩形在 dy=r 处才恢复满宽，所以这段距离就是半径）
    #   ② 横向：顶行的墨段左端 − 竖直中段的左边线 ≈ 半径
    # 抗锯齿会让顶行早 1px 出现，两种估计允许 ±2px 的差。
    def row_span(y):
        xs = [x for x in range(x0, x1) if is_ink(x, y)]
        return (min(xs), max(xs)) if xs else None

    rows = [(y, row_span(y)) for y in range(y0, min(y0 + 80, y1))]
    rows = [(y, s) for y, s in rows if s]
    if not rows:
        sys.exit("没找到控件，放宽坐标范围（或把 (x0,y0) 放到控件外的背景上）")
    full = max(s[1] - s[0] + 1 for _, s in rows)
    # 顶行 = 第一行宽度 ≥ 满宽一半的行。用这个门槛可以把同一框里其它小元素
    # （比如 composer 上方那颗"回到底部"圆钮）排除掉。
    top_row = next(((y, s) for y, s in rows if s[1] - s[0] + 1 >= full * 0.5), None)
    if top_row is None:
        print("⚠ 没有任何行达到满宽的一半——目标可能是个胶囊/圆形，下面的半径估计不可用")
        top_row = rows[0]
    top_y, top_span = top_row

    # 元素左右边线取"满宽行"（一定是竖直中段）的墨段端点
    full_row = max(rows, key=lambda t: t[1][1] - t[1][0])
    L, R = full_row[1]
    probe_y = full_row[0]

    print("背景色 %s（取自 (%d,%d)）" % (hexof(bg), x0, y0))
    print("顶行 y=%d（该行墨段 x=%d..%d）" % (top_y, top_span[0], top_span[1]))
    print("满宽行 y=%d（x=%d..%d，宽 %d）" % (probe_y, L, R, full))
    radius = top_span[0] - L
    print("→ 圆角半径 ≈ %d px   （= 顶行墨段左端 − 满宽行左端）" % radius)
    print("→ 控件尺寸 ≈ %d × %d" % (R - L + 1, y1 - top_y))
    hh = y1 - top_y
    print("→ 半径/半高 = %.2f  %s" % (radius / (hh / 2), "偏圆（接近圆/胶囊）" if radius > hh / 2 else "克制的圆角"))
    print("→ 角剖面交叉校验（dy → 最左墨像素相对左端的偏移）：")
    prof = []
    for y, s in rows:
        if y < top_y - 2 or y > top_y + 24:
            continue
        prof.append("%d:%d" % (y - top_y, s[0] - L))
    print("   " + "  ".join(prof))


def cmd_edges(argv):
    """扫一行的颜色断点，用来找面板边界与发丝线。"""
    path, y = argv[0], int(argv[1])
    x0 = int(argv[2]) if len(argv) > 2 else 0
    x1 = int(argv[3]) if len(argv) > 3 else None
    im = Image.open(path).convert("RGB")
    w, h = im.size
    x1 = x1 if x1 is not None else w
    px = im.load()
    prev = px[x0, y]
    print("y=%d 断点：" % y)
    for x in range(x0 + 1, x1):
        c = px[x, y]
        if max(abs(c[i] - prev[i]) for i in range(3)) > 3:
            print("   x=%4d  %s -> %s" % (x, hexof(prev), hexof(c)))
        prev = c


def main():
    if len(sys.argv) < 3:
        print(__doc__)
        return
    cmd, rest = sys.argv[1], sys.argv[2:]
    {"gradient": cmd_gradient, "radius": cmd_radius, "edges": cmd_edges}.get(cmd, lambda _: print(__doc__))(rest)


if __name__ == "__main__":
    main()
