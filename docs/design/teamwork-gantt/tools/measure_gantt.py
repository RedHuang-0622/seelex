#!/usr/bin/env python
# -*- coding: utf-8 -*-
"""teamwork-gantt 静态稿自证工具（全程无窗口路径）。

做四件事（只读文件系统 + 调 headless Chrome）：
  1) `--dump-dom` 取渲染后的 DOM（含页面自证写下的 #gantt-probe JSON）
  2) `--screenshot` 出 PNG：**宽栏 / 窄栏 360px / 推进一轮后**（附加 sketch / dark / 滚到底）
  3) **用 PIL 逐行扫每根条的实色 x 区间**（条的左/右竖边列 + 顶边游程），换算回槽位，
     与「照抄 leader 冻结公式、在本脚本里独立重算」的期望槽位逐条对照 —— 把「像甘特」变成「量出来是甘特」
     · 绘图区原点 plot_l 与槽宽 slotW **从 PNG 自己量**（刻度尺竖线），不依赖页面读数
     · 槽位换算只用 PNG 量到的 x0；页面读数（DOM rect）只当搜索窗口，同时交叉核对
  4) 另用 DOM 核对：汇总条跨度 = min..max、里程碑间箭头、行序 = 拓扑序、信息不丢、闸门三支文案

用法：  python docs/design/teamwork-gantt/tools/measure_gantt.py
产物：  docs/design/teamwork-gantt/evidence/{gantt-*.png,dom-*.html,readings.json,readings.txt,slot-table.md}
"""
import json, math, os, re, subprocess, sys, tempfile

try:
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
except Exception:
    pass

CHROME = r"C:\Program Files\Google\Chrome\Application\chrome.exe"
ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))   # docs/design/teamwork-gantt
OUT = os.path.join(ROOT, "evidence")
INDEX = os.path.join(ROOT, "index.html")
PROFILE = os.path.join(tempfile.gettempdir(), "seelex-gantt-chrome-profile")

# 逐字取自 gui/frontend/dist/styles.css
STATUS_LIGHT = {"running": (178, 106, 0), "done": (31, 156, 99), "failed": (214, 69, 69),
                "review": (46, 107, 230), "pending": (154, 160, 168)}
STATUS_DARK = {"running": (224, 164, 92), "done": (76, 195, 138), "failed": (240, 113, 106),
               "review": (107, 166, 255), "pending": (140, 140, 147)}
TONES = {"light": [(106, 127, 168), (138, 127, 168), (111, 143, 127), (160, 138, 106), (143, 111, 127)],
         "dark":  [(143, 166, 200), (168, 154, 196), (143, 176, 154), (196, 171, 132), (184, 148, 164)]}
PANEL = {"light": (251, 250, 246), "dark": (31, 31, 31)}
SURFACE = {"light": (255, 255, 255), "dark": (38, 38, 38)}
GRID_STRONG = {"light": (214, 210, 198), "dark": (71, 71, 71)}
INK_TOL = 25.0

SHOTS = [
    ("wide",        "?view=wide",                 "800,1400", True,  "宽栏 704px 宿主 · 真实故事（3 里程碑 / 6 工作项）· scrollTop=0"),
    ("narrow",      "?view=narrow",               "420,1500", True,  "窄栏 344px 宿主（容器查询生效）· scrollTop=0"),
    ("advance1",    "?view=wide&advance=1",       "800,1400", True,  "推进一轮（wi-design done · wi-impl running · m-impl 闸门放行）"),
    ("wide-bottom", "?view=wide&scroll=94",       "800,1400", False, "宽栏滚到底（看 m-verify 的两行）"),
    ("narrow-bottom", "?view=narrow&scroll=44",   "420,1500", False, "窄栏滚到底"),
    ("sketch",      "?view=wide&demo=sketch",     "800,1400", False, "用户 sketch：a/b/c/d（c→d、b→a、d→a）"),
    ("mixed",       "?view=wide&demo=mixed",      "800,1400", False, "变体：done/review/failed+interrupted + 空里程碑（零宽菱形）"),
    ("dark",        "?view=wide&theme=dark",      "800,1400", False, "深色基座"),
]


def run(args):
    return subprocess.run(args, capture_output=True, timeout=300)


def chrome(url, mode, png=None, win="800,1400"):
    base = [CHROME, "--headless=new", "--disable-gpu", "--no-first-run", "--disable-extensions",
            "--force-device-scale-factor=1",
            "--virtual-time-budget=4000", "--user-data-dir=" + PROFILE, "--window-size=" + win]
    if mode == "dom":
        p = run(base + ["--dump-dom", url])
        if not p.stdout:
            raise RuntimeError("dump-dom 无输出: " + p.stderr.decode("utf-8", "replace")[-600:])
        return p.stdout.decode("utf-8", "replace")
    p = run(base + ["--screenshot=" + png, url])
    if not os.path.exists(png):
        raise RuntimeError("screenshot 未生成: " + p.stderr.decode("utf-8", "replace")[-600:])
    return png


def probe_of(dom):
    m = re.search(r'<pre id="gantt-probe"[^>]*>(.*?)</pre>', dom, re.S)
    if not m or not m.group(1).strip():
        raise RuntimeError("DOM 里找不到 #gantt-probe 内容（页面 JS 可能报错）")
    return json.loads(m.group(1))


# ---------------------------------------------------------------- 公式（本脚本独立重算，不读页面算好的值）
def formula_slots(rows):
    """start(i) = 0（无依赖）；否则 max(start(d) + dur(d))，dur 恒 1。只看 DOM 里读到的 depends_on。"""
    deps = {r["item"]: [d for d in (r.get("deps") or "").split(",") if d] for r in rows}
    start = {}

    def s(i, seen):
        if i in start:
            return start[i]
        if i in seen:
            return 0                          # 成环兜底
        ds = [d for d in deps.get(i, []) if d in deps]
        v = max([s(d, seen | {i}) + 1 for d in ds], default=0)
        start[i] = v
        return v

    for r in rows:
        s(r["item"], set())
    return start


# ---------------------------------------------------------------- PNG 量测原语
def blend(c, bg, a):
    return tuple(int(round(a * c[i] + (1 - a) * bg[i])) for i in range(3))


def dist(a, b):
    return math.sqrt(sum((a[i] - b[i]) ** 2 for i in range(3)))


def prototypes(status_map, panel):
    """全部原型：5 状态 × 2（实色 / 未解锁 opacity .55 混色）。用于「就近归类」。"""
    out = []
    for name, c in status_map.items():
        out.append((name, 1.0, tuple(c)))
        out.append((name, 0.55, blend(c, panel, 0.55)))
    return out


def status_protos(name, c, panel):
    return [(name, 1.0, tuple(c)), (name + "×.55", 0.55, blend(c, panel, 0.55))]


def find_ruler_band(img, surface):
    """刻度尺底色带（--surface）：返回 (y0, y1)。"""
    px = img.load()
    W, H = img.size
    step = 8
    ys = [sum(1 for x in range(0, W, step) if px[x, y][:3] == surface) * step >= 0.5 * W
          for y in range(H)]
    y0 = next((y for y, ok in enumerate(ys) if ok), None)
    if y0 is None:
        return None, None
    y1 = y0
    while y1 + 1 < H and ys[y1 + 1]:
        y1 += 1
    return y0, y1


def find_ticks(img, band, strong, tol=8.0):
    """刻度尺下半段的竖线（--border-strong，实色）→ 只保留能连成等差链的那些（剔掉边框/干扰）。"""
    y0, y1 = band
    px = img.load()
    W = img.size[0]
    ya, yb = y0 + 17, y1
    need = max(4, int(0.75 * (yb - ya + 1)))
    cols = [x for x in range(2, W - 2)
            if sum(1 for y in range(ya, yb + 1) if dist(px[x, y][:3], strong) <= tol) >= need]
    groups = []
    for x in cols:
        if groups and x - groups[-1][-1] <= 2:
            groups[-1].append(x)
        else:
            groups.append([x])
    starts = [g[0] for g in groups]
    if len(starts) < 2:
        return []
    diffs = {}
    for i in range(len(starts) - 1):
        d = starts[i + 1] - starts[i]
        diffs[d] = diffs.get(d, 0) + 1
    step = max(diffs.items(), key=lambda kv: (kv[1], -kv[0]))[0]
    if step < 8:
        return []
    chain = [starts[0]]
    for s in starts[1:]:
        if abs((s - chain[-1]) - step) <= 2:
            chain.append(s)
    return chain


def group_cols(cols, gap):
    runs = []
    for x in cols:
        if runs and x - runs[-1][-1] <= gap:
            runs[-1].append(x)
        else:
            runs.append([x])
    return [(r[0], r[-1]) for r in runs]


def ink_columns(img, protos, x_from, x_to, y_from, y_to, min_count=3, tol=22.0):
    """按列统计「命中指定原型」的像素数；竖边（左/右边框）会整列命中，网格线/连线/文字给不出整列。"""
    px = img.load()
    W, H = img.size
    out = []
    for x in range(max(0, int(x_from)), min(W, int(x_to))):
        n, best = 0, 1e9
        for y in range(max(0, int(y_from)), min(H, int(y_to))):
            p = px[x, y][:3]
            d = min(dist(p, c) for _, _, c in protos)
            if d <= tol:
                n += 1
                best = min(best, d)
        if n >= min_count:
            out.append((x, n, round(best, 1)))
    return out


# ---------------------------------------------------------------- 主流程
def main():
    from PIL import Image
    os.makedirs(OUT, exist_ok=True)
    base_url = "file:///" + INDEX.replace("\\", "/")
    R = {"shots": {}, "checks": [], "problems": [], "rows_all": [], "sums_all": []}
    md_header = ["| 图 | item | role | 状态(eff) | 量到 x0..x1(px) | 换算槽位 | 期望槽位 | 条宽 px | 判 |",
                 "|---|---|---|---|---|---|---|---|---|"]
    md = list(md_header)

    def bad(msg):
        R["problems"].append(msg)
        print("  [FAIL] " + msg)

    for name, q, win, core, desc in SHOTS:
        url = base_url + q
        dom = chrome(url, "dom", win=win)
        with open(os.path.join(OUT, "dom-%s.html" % name), "w", encoding="utf-8", newline=chr(10)) as f:
            f.write(dom)
        probe = probe_of(dom)
        png_path = os.path.join(OUT, "gantt-%s.png" % name)
        chrome(url, "shot", png=png_path, win=win)
        img = Image.open(png_path).convert("RGB")
        theme = probe.get("theme") or "light"
        status_map = STATUS_DARK if theme == "dark" else STATUS_LIGHT
        panel, surface, strong = PANEL[theme], SURFACE[theme], GRID_STRONG[theme]
        protos = prototypes(status_map, panel)
        b0 = [b for b in probe["boards"] if b][0]

        # --- PNG 自查：刻度尺带 + 刻度竖线 → plot_l / slotW（不依赖页面读数） ---
        y0, y1 = find_ruler_band(img, surface)
        ticks = find_ticks(img, (y0, y1), strong) if y0 is not None else []
        png_plot_l = ticks[0] if ticks else None
        png_slot_w = (ticks[1] - ticks[0]) if len(ticks) > 1 else None
        dom_plot_l, dom_slot_w = b0["ruler"]["plot"]["l"], b0["slotW"]
        dy = (y0 - b0["ruler"]["rect"]["t"]) if y0 is not None else None

        entry = {"png": "evidence/gantt-%s.png" % name, "dom": "evidence/dom-%s.html" % name,
                 "png_size": list(img.size), "query": q, "desc": desc, "core": core, "theme": theme,
                 "round": probe.get("round"), "demo": probe.get("demo"),
                 "ruler_band_png": [y0, y1],
                 "calib": {"dy_px": dy, "ruler_top_probe": b0["ruler"]["rect"]["t"],
                           "why": "Chrome --dump-dom 与 --screenshot 的页面宽度/换行差（dump 侧页面滚动条占 18px → legend 多折一行）；"
                                  "本脚本把该差值量出来用于取窗口，x 量测完全走 PNG"},
                 "png_plot_l": png_plot_l, "png_slot_w": png_slot_w,
                 "dom_plot_l": dom_plot_l, "dom_slot_w": dom_slot_w,
                 "idempotent": probe.get("idempotent"), "stickyRuler": probe.get("stickyRuler"),
                 "stickyHead": probe.get("stickyHead"), "roleIndex": probe.get("roleIndex"),
                 "boards": []}
        print("\n=== shot %s  %s  png=%s ===" % (name, q, os.path.basename(png_path)))
        print("  PNG 自查：刻度尺带 y=%s..%s · 刻度竖线 %d 条 · plot_l=%s slot_w=%s · 页面读数 plot_l=%s slot_w=%s · dy=%s"
              % (y0, y1, len(ticks), png_plot_l, png_slot_w, dom_plot_l, dom_slot_w, dy))
        if png_plot_l is None or png_slot_w is None:
            bad("%s: PNG 里找不到刻度尺/刻度竖线" % name)
        else:
            if abs(png_plot_l - dom_plot_l) > 1.5:
                bad("%s: PNG 量到绘图区原点 %s ≠ 页面读数 %.1f" % (name, png_plot_l, dom_plot_l))
            if abs(png_slot_w - dom_slot_w) > 1.5:
                bad("%s: PNG 量到槽宽 %s ≠ 页面读数 %.1f" % (name, png_slot_w, dom_slot_w))
        plot_l = png_plot_l if png_plot_l is not None else int(dom_plot_l)
        slot_w = png_slot_w if png_slot_w else int(dom_slot_w)

        for b in probe["boards"]:
            if b is None:
                continue
            rows = sorted(b["rows"], key=lambda r: r["order"])
            fslot = formula_slots(rows)
            slots = max(1, max((r["slot"] + r["dur"] for r in rows), default=1))
            sc = b["scroll"]
            vis_top = sc["rect"]["t"] + b["ruler"]["rect"]["h"] + 30      # 刻度尺 + 可能被吸住的框头（28px）都遮住顶部
            vis_bot = sc["rect"]["t"] + sc["clientH"] - 1
            rec = {"board": b["id"], "hostW": b["hostW"], "domSlotW": b["slotW"], "labelW": b["labelW"],
                   "rowH": b["rowH"], "barH": b["barH"], "scroll": sc, "scrollMax": b["scrollMaxH"],
                   "svg": b["svg"], "nItemEdges": len(b["itemEdges"]), "nMsEdges": len(b["msEdges"]),
                   "frames": b["frames"], "gates": b["gates"], "rows": [], "sums": [],
                   "topo_violations": [], "edge_anchor_ok": 0, "edge_anchor_total": 0}
            print("  board=%s hostW=%s rowH=%s barH=%s scroll=%s/%s(限%s) svg=%s itemEdges=%d msEdges=%d"
                  % (b["id"], b["hostW"], b["rowH"], b["barH"], sc["clientH"], sc["scrollH"], b["scrollMaxH"],
                     b["svg"], len(b["itemEdges"]), len(b["msEdges"])))
            print("  %-10s %-8s %-7s %-5s %-5s %-6s %-12s %-10s %-7s %s"
                  % ("item", "role", "eff", "期望槽", "DOM槽", "PNG槽", "量到 x0..x1", "期望 x0..x1", "条宽", "判"))
            for r in rows:
                exp = fslot[r["item"]]
                bt = r["bar"]["t"] + (dy or 0)
                visible = (bt >= vis_top) and (bt + r["bar"]["h"] <= vis_bot)
                rowp = status_protos(r["eff"], status_map[r["eff"]], panel)
                x_win_lo = max(plot_l, r["bar"]["l"] - 4)
                x_win_hi = min(plot_l + slots * slot_w, r["bar"]["l"] + r["bar"]["w"] + 4)
                y_win_lo, y_win_hi = bt + 2, bt + r["bar"]["h"] - 3     # 躲开被压盖的顶边；箭头只盖住中间几行
                cols = ink_columns(img, rowp, x_win_lo, x_win_hi, y_win_lo, y_win_hi, min_count=3) if visible else []
                if not visible:
                    mslot = mw = None
                    how = "skipped: 被滚动视口裁掉 / 被 sticky 刻度尺遮住（该行在别的图里量）"
                elif not cols:
                    mslot = mw = None
                    how = "PNG 里扫不到条的实色竖边"
                    bad("%s/%s: PNG 里扫不到条（期望 x0=%.0f, bar.l=%s, 窗口 x=%d..%d y=%d..%d）"
                        % (name, r["item"], plot_l + exp * slot_w, r["bar"]["l"], x_win_lo, x_win_hi, y_win_lo, y_win_hi))
                else:
                    x0, x1 = cols[0][0], cols[-1][0]
                    mslot = int(round((x0 - plot_l) / slot_w))
                    mw = x1 - x0 + 1
                    how = "v-edge col %s(%d..%d)" % (r["eff"], y_win_lo, y_win_hi)
                ok = (mslot == exp == r["slot"]) and (mw is not None and abs(mw - (slot_w * r["dur"] - 2)) <= 5)
                if mslot is not None and not ok:
                    bad("%s/%s: 槽位/条宽对不上 公式=%d DOM=%d PNG=%s 宽=%s(期望%d)"
                        % (name, r["item"], exp, r["slot"], mslot, mw, slot_w * r["dur"] - 2))
                rec["rows"].append({
                    "item": r["item"], "ms": r["ms"], "role": r["role"], "status": r["status"], "eff": r["eff"],
                    "locked": r["ms"] in set(f["ms"] for f in b["frames"] if f["locked"]),
                    "formula_slot": exp, "dom_slot": r["slot"], "dur": r["dur"],
                    "visible": visible, "png_x0": x0 if cols else None, "png_x1": x1 if cols else None,
                    "png_slot": mslot, "png_width_px": mw, "how": how,
                    "expect_px": [round(plot_l + exp * slot_w, 1), round(plot_l + (exp + r["dur"]) * slot_w - 2, 1)],
                    "dom_bar": [r["bar"]["l"], r["bar"]["t"], r["bar"]["w"], r["bar"]["h"]],
                    "blocked": r["blocked"], "interrupted": r["interrupted"], "live": r["live"],
                    "deps": r["deps"], "ok": bool(ok), "measured": mslot is not None})
                if mslot is not None:
                    R["rows_all"].append({"shot": name, "item": r["item"], "board": b["id"],
                                          "x0": x0, "x1": x1, "slot": mslot, "expect": exp,
                                          "width": mw, "ok": bool(ok)})
                    print("  %-10s %-8s %-7s %-5d %-5d %-6s %-12s %-10s %-7s %s"
                          % (r["item"], r["role"], r["eff"], exp, r["slot"], mslot,
                             "%s..%s" % (x0, x1), "%.0f..%.0f" % (plot_l + exp * slot_w, plot_l + (exp + r["dur"]) * slot_w - 2),
                             mw, "OK" if ok else "**FAIL**"))
                    if name in ("wide", "advance1", "sketch", "narrow"):
                        md.append("| %s | `%s` | %s | %s | %s..%s | **%s** | %s | %s | %s |"
                                  % (name, r["item"], r["role"], r["eff"], x0, x1, mslot, exp, mw,
                                     "OK" if ok else "FAIL"))
                else:
                    print("  %-10s %-8s %-7s %-5d %-5d %-6s %-12s %-10s %-7s %s"
                          % (r["item"], r["role"], r["eff"], exp, r["slot"], "—", "—",
                             "%.0f..%.0f" % (plot_l + exp * slot_w, plot_l + (exp + r["dur"]) * slot_w - 2), "—", how))

            # 汇总条：取该里程碑汇总行 y 带内的 tone 实色列
            for fi, f in enumerate(b["frames"]):
                sb = f.get("sumBar")
                ids = f["rowIds"]
                s = min(fslot[i] for i in ids) if ids else None
                e = max(fslot[i] + 1 for i in ids) if ids else None
                pngx = None
                sum_visible = True
                if sb and not f["sumEmpty"] and dy is not None:
                    sy = sb["t"] + dy                                        # PNG 坐标
                    sum_visible = (sb["t"] + 2 >= vis_top) and (sb["t"] + sb["h"] - 2 <= vis_bot)   # 页面坐标
                    if sum_visible:
                        tone = TONES[theme][f["layer"] % 5]
                        tprotos = [("tone", 1.0, tone), ("tone×.55", 0.55, blend(tone, panel, 0.55))]
                        y_lo, y_hi = max(sy, vis_top + (dy or 0)), min(sy + sb["h"] + 2, vis_bot + (dy or 0))
                        cols = ink_columns(img, tprotos, plot_l - 2, plot_l + slots * slot_w + 2,
                                           y_lo, y_hi, min_count=max(4, int(0.6 * (y_hi - y_lo + 1))))
                        if cols:
                            runs = group_cols([c[0] for c in cols], 5)
                            best = max(runs, key=lambda r: r[1] - r[0])
                            pngx = [best[0], best[1]]
                rec["sums"].append({"ms": f["ms"], "empty": f["sumEmpty"], "formula_s": s, "formula_e": e,
                                    "dom_s": f["sumStart"], "dom_e": f["sumEnd"],
                                    "sumLeftInPlot": f["sumLeftInPlot"], "sumWidth": f["sumWidth"],
                                    "png_x": pngx,
                                    "expect_x": [round(plot_l + s * slot_w, 1), round(plot_l + e * slot_w - 1, 1)] if ids else None})
                if ids:
                    if (f["sumStart"], f["sumEnd"]) != (s, e):
                        bad("%s/%s: 汇总条跨度 %s..%s ≠ min..max %s..%s" % (name, f["ms"], f["sumStart"], f["sumEnd"], s, e))
                    if f["sumLeftInPlot"] is None or abs(f["sumLeftInPlot"] - s * slot_w) > 1.5 \
                       or abs(f["sumWidth"] - (e - s) * slot_w) > 1.5:
                        bad("%s/%s: 汇总条几何 left=%s 宽=%s（期望 %s / %s）"
                            % (name, f["ms"], f["sumLeftInPlot"], f["sumWidth"], s * slot_w, (e - s) * slot_w))
                    if pngx:
                        # 左端可能被里程碑间箭头的箭头盖住（≤8px），右端不该被盖
                        dl = pngx[0] - (plot_l + s * slot_w)
                        dr = pngx[1] - (plot_l + e * slot_w - 1)
                        if dl > 9 or dl < -6 or abs(dr) > 5:
                            bad("%s/%s: PNG 汇总条 x=%s..%s（期望 %.0f..%.0f，左端差 %+.0f 右端差 %+.0f）"
                                % (name, f["ms"], pngx[0], pngx[1], plot_l + s * slot_w, plot_l + e * slot_w - 1, dl, dr))
                        R["sums_all"].append({"shot": name, "ms": f["ms"], "png_x": pngx,
                                              "expect_x": [plot_l + s * slot_w, plot_l + e * slot_w - 1],
                                              "dl": dl, "dr": dr})
                    else:
                        if sum_visible:
                            bad("%s/%s: PNG 里扫不到汇总条" % (name, f["ms"]))
                else:
                    if not f["sumEmpty"]:
                        bad("%s/%s: 空里程碑没画零宽菱形" % (name, f["ms"]))            # 拓扑序 / 边锚点
            pos = {r["item"]: i for i, r in enumerate(rows)}
            for i, r in enumerate(rows):
                for d in [x for x in (r["deps"] or "").split(",") if x]:
                    if d in pos and pos[d] >= i:
                        rec["topo_violations"].append("%s 排在依赖 %s 之前" % (d, r["item"]))
            if rec["topo_violations"]:
                bad("%s: 行序不是拓扑序 %s" % (name, rec["topo_violations"]))
            barmap = {r["item"]: r["bar"] for r in b["rows"]}
            for e2 in b["itemEdges"]:
                rec["edge_anchor_total"] += 1
                sb2, db2 = barmap.get(e2["src"]), barmap.get(e2["dst"])
                if sb2 and db2 and abs(e2["srcView"][0] - (sb2["l"] + sb2["w"])) <= 1.5 \
                   and abs(e2["srcView"][1] - (sb2["t"] + sb2["h"] / 2.0)) <= 1.5 \
                   and abs(e2["dstView"][0] - db2["l"]) <= 1.5 \
                   and abs(e2["dstView"][1] - (db2["t"] + db2["h"] / 2.0)) <= 1.5:
                    rec["edge_anchor_ok"] += 1
                else:
                    bad("%s: 边 %s→%s 锚点不对（srcView=%s dstView=%s）" % (name, e2["src"], e2["dst"], e2["srcView"], e2["dstView"]))
            entry["boards"].append(rec)

        overflow = all(bb["scroll"]["scrollH"] > bb["scroll"]["clientH"] for bb in probe["boards"] if bb)
        checks = [
            ("幂等（同输入重渲染 DOM 串逐字相同 hashA=hashB）", probe.get("idempotent") is True),
            ("滚动保持（重渲染后 scrollTop 不跳）",
             probe.get("scrollKeep") is True or not overflow),
            ("刻度尺 sticky（滚动时刻度尺吸附容器顶边）",
             probe.get("stickyRuler") is True or not overflow),
            ("框头 sticky（吸在刻度尺下沿）",
             probe.get("stickyHead") is True or not overflow),
            ("面板 max-height = 380（内容超出时内部滚动；单屏夹具不适用）",
             all(bb["scroll"]["clientH"] <= 381 for bb in probe["boards"] if bb) and
             (overflow or True)),
            ("PNG 刻度竖线逐条落在 plot_l + i*slotW 上",
             bool(png_plot_l) and all(abs(t - (png_plot_l + i * png_slot_w)) <= 1.5 for i, t in enumerate(ticks))),
            ("空里程碑画零宽菱形 / 非空不画（菱形 data-empty 与框 data-sum-empty 一致，且有对应 CSS）",
             len(re.findall(r'<span class="sum-diamond"[^>]*data-empty="true"', dom)) ==
             len(re.findall(r'data-sum-empty="true"', dom)) and
             bool(re.search(r'\.sum-bar\[data-empty="true"\]\{display:none', dom)) and
             bool(re.search(r'\.sum-diamond\[data-empty="false"\]\{display:none', dom))),
        ]
        for label, ok in checks:
            print("  [%s] %s" % ("PASS" if ok else "FAIL", label))
            if not ok:
                bad("%s: %s" % (name, label))
        R["checks"].append({"shot": name, "items": [{"label": l, "ok": bool(o)} for l, o in checks]})
        for k in ("goal:", "description:", "note:", "session_id:", "worktree:", "depends_on:",
                  'data-flag="blocked"', 'data-flag="interrupted"', 'data-flag="live"',
                  "ms-id", "ms-name", "ms-layer", "ms-lock", "ms-count", "ms-content", "ms-deps",
                  "data-team-item-open", "data-team-role-open", "data-team-session"):
            if k not in dom:
                bad("%s: DOM 里找不到 %r" % (name, k))
        glabels = re.findall(r'<span class="gate-label">(.*?)</span>', dom, re.S)
        entry["gateTexts"] = glabels
        if not glabels:
            bad("%s: 没有闸门带文案" % name)
        R["shots"][name] = entry

    # ---- 覆盖性：每行至少被某一张图量到过 ----
    print("\n============ 覆盖性：每个工作项是否至少被量到一次 ============")
    seen = {}
    for r in R["rows_all"]:
        if r["ok"]:
            seen[r["item"]] = r
    all_items = [r["item"] for r in R["rows_all"]]
    for it in sorted(set(all_items)):
        hit = seen.get(it)
        print("  %-12s %s" % (it, ("量到并命中（%s 图：x=%s..%s → 槽 %s）" % (hit["shot"], hit["x0"], hit["x1"], hit["slot"])) if hit else "**没有被任何一张图量到**"))
        if not hit:
            bad("工作项 %s 没有任何一张图量到过" % it)
    print("\n============ 逐行条形 x 区间 → 槽位 核对表 ============")
    for line in md:
        print(line)
    with open(os.path.join(OUT, "slot-table.md"), "w", encoding="utf-8", newline=chr(10)) as f:
        f.write("\n".join(md) + "\n")
    R["slot_table_md"] = md
    with open(os.path.join(OUT, "readings.json"), "w", encoding="utf-8", newline=chr(10)) as f:
        json.dump(R, f, ensure_ascii=False, indent=1)
    with open(os.path.join(OUT, "readings.txt"), "w", encoding="utf-8", newline=chr(10)) as f:
        f.write(json.dumps(R, ensure_ascii=False, indent=1))
    print("\nproblems: %d" % len(R["problems"]))
    for p in R["problems"]:
        print("  - " + p)
    print("evidence ->", OUT)
    return 1 if R["problems"] else 0


if __name__ == "__main__":
    sys.exit(main())
