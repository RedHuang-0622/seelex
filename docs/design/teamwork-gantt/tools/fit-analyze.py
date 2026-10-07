# fit-analyze.py —— 无窗口看图（PIL）：把 fit-probe.mjs 出的 PNG 掰成读数。
#
# 读三件事（都不需要人眼看图）：
#   1) 语义色是否真的落在画面上（状态色 = 条描边 / chip 文字；一行一条描边）；
#   2) 依赖边（--team-dag-edge）的像素落在哪几档 y、最左到哪儿 —— 最左必须 ≥
#      页面边距 + labelW：线不许画进"任务名列"；
#   3) 行高是不是真的由内容撑：同一张图里，名字长的行所占的墨迹带比名字短的行高。
#
# 用法（仓库根目录）：
#   python docs/design/teamwork-gantt/tools/fit-analyze.py \
#     docs/design/teamwork-gantt/evidence/fit-narrow-370.png \
#     docs/design/teamwork-gantt/evidence/fit-pixels.json
from PIL import Image
import json
import sys

png = sys.argv[1] if len(sys.argv) > 1 else "docs/design/teamwork-gantt/evidence/fit-narrow-370.png"
out_path = sys.argv[2] if len(sys.argv) > 2 else "docs/design/teamwork-gantt/evidence/fit-pixels.json"

img = Image.open(png).convert("RGB")
width, height = img.size
px = img.load()


def near(color, target, tol):
    return all(abs(color[i] - target[i]) <= tol for i in range(3))


def collect(target, tol):
    return [(x, y) for y in range(height) for x in range(width) if near(px[x, y], target, tol)]


STATUS = {
    "done(绿=完成)": (31, 156, 99),
    "running(黄=正在做)": (178, 106, 0),
    "review(蓝=正在 review)": (46, 107, 230),
    "failed(红=中断待恢复)": (214, 69, 69),
    "pending/idle(灰)": (154, 160, 168),
}
EDGE = (63, 147, 208)  # --team-dag-edge（浅色主题）


def bands(ys, gap=2):
    out = []
    for y in sorted(ys):
        if out and y - out[-1][-1] <= gap:
            out[-1].append(y)
        else:
            out.append([y])
    return out


result = {"png": png, "size": [width, height], "status": {}, "edge": {}}
for name, target in STATUS.items():
    points = collect(target, 2)
    result["status"][name] = {
        "pixels": len(points),
        "y": [min(p[1] for p in points), max(p[1] for p in points)] if points else None,
    }

# 边：严格容差 1，避免把别的蓝灰色（下划线/描边）算进来。
edge_points = collect(EDGE, 1)
edge_ys = [p[1] for p in edge_points]
result["edge"] = {
    "pixels": len(edge_points),
    "x": [min(p[0] for p in edge_points), max(p[0] for p in edge_points)] if edge_points else None,
    "y": [min(edge_ys), max(edge_ys)] if edge_ys else None,
    "y_bands(段)": [f"{b[0]}–{b[-1]}" for b in bands(edge_ys)],
}
result["读法"] = "edge.x[0] ≥ 页面边距(11) + labelW(118) = 129：线不许画进任务名列；y_bands 越多 = 边越是真的落在各行自己的高度上"

payload = json.dumps(result, ensure_ascii=False, indent=1) + "\n"
with open(out_path, "w", encoding="utf-8", newline="\n") as handle:
    handle.write(payload)
# 本目录的惯例：同名 .txt 与 .json 逐字相同（给不看 json 的人一眼看到），所以这里一并写。
if out_path.endswith(".json"):
    with open(out_path[:-5] + ".txt", "w", encoding="utf-8", newline="\n") as handle:
        handle.write(payload)
print(payload, end="")
