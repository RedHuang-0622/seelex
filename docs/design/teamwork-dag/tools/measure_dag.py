#!/usr/bin/env python
# -*- coding: utf-8 -*-
"""teamwork-dag 静态稿自证工具（无窗口路径）。

做三件事，全部只读文件系统 + 调用 headless Chrome：
  1) --dump-dom 拿渲染后的 DOM（含页面自证写下的 #dag-probe JSON）
  2) --screenshot 出 light / dark 两张 PNG
  3) 用 PIL 量测 PNG：尺寸、非空白像素比例、四种状态色是否真的出现在画面里

用法：  python docs/design/teamwork-dag/tools/measure_dag.py
产物：  docs/design/teamwork-dag/evidence/{dom-light.html,shot-light.png,shot-dark.png,readings.json}
"""
import json, os, re, subprocess, sys, tempfile

try:
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
except Exception:
    pass

CHROME = r"C:\Program Files\Google\Chrome\Application\chrome.exe"
ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))      # docs/design/teamwork-dag
OUT = os.path.join(ROOT, "evidence")
INDEX = os.path.join(ROOT, "index.html")
WIN = "1440,1200"
PROFILE = os.path.join(tempfile.gettempdir(), "seelex-dag-chrome-profile")   # 临时 profile，别落在 evidence/

# 期望色：逐字取自 gui/frontend/dist/styles.css 的浅色基座
EXPECT_LIGHT = {
    "running": (178, 106, 0),      # --status-running #b26a00
    "done":    (31, 156, 99),      # --status-done    #1f9c63
    "failed":  (214, 69, 69),      # --status-failed  #d64545
    "review":  (46, 107, 230),     # --status-info    #2e6be6
    "pending": (154, 160, 168),    # --status-idle    #9aa0a8
}
BG_LIGHT = (247, 246, 242)         # --bg
# 期望色：深色基座（:root[data-theme="dark"]）
EXPECT_DARK = {
    "running": (224, 164, 92),     # #e0a45c
    "done":    (76, 195, 138),     # #4cc38a
    "failed":  (240, 113, 106),    # #f0716a
    "review":  (107, 166, 255),    # #6ba6ff
    "pending": (140, 140, 147),    # #8c8c93
}
BG_DARK = (24, 24, 24)             # --bg


def run(args):
    p = subprocess.run(args, capture_output=True, timeout=180)
    return p.stdout, p.stderr


def chrome_dom(url):
    out, err = run([CHROME, "--headless=new", "--disable-gpu", "--no-first-run",
                    "--disable-extensions", "--virtual-time-budget=3000",
                    "--user-data-dir=" + PROFILE,
                    "--window-size=" + WIN,
                    "--dump-dom", url])
    if not out:
        raise RuntimeError("dump-dom 无输出: " + err.decode("utf-8", "replace")[-800:])
    return out.decode("utf-8", "replace")


def chrome_shot(png, url):
    out, err = run([CHROME, "--headless=new", "--disable-gpu", "--no-first-run",
                    "--disable-extensions", "--virtual-time-budget=3000",
                    "--user-data-dir=" + PROFILE,
                    "--window-size=" + WIN,
                    "--screenshot=" + png, url])
    if not os.path.exists(png):
        raise RuntimeError("screenshot 未生成: " + err.decode("utf-8", "replace")[-800:])
    return png


def probe_from_dom(dom):
    m = re.search(r'<pre id="dag-probe"[^>]*>(.*?)</pre>', dom, re.S)
    if not m:
        raise RuntimeError("DOM 里找不到 #dag-probe（页面自证 JSON）")
    return json.loads(m.group(1))


def nearest(img, cx, cy, expect, rx=5, ry=4):
    """在 (cx,cy) 邻域里找与期望色最近的像素，返回 (x, y, rgb, 距离) 或 None（窗口落在画外）。"""
    W, H = img.size
    x0, x1 = max(0, int(cx) - rx), min(W - 1, int(cx) + rx)
    y0, y1 = max(0, int(cy) - ry), min(H - 1, int(cy) + ry)
    if x1 < x0 or y1 < y0 or int(cy) < 0 or int(cy) >= H or int(cx) < 0 or int(cx) >= W:
        return None
    best = None
    for y in range(y0, y1 + 1):
        for x in range(x0, x1 + 1):
            p = img.getpixel((x, y))[:3]
            d = sum((p[i] - expect[i]) ** 2 for i in range(3))
            if best is None or d < best[3]:
                best = (x, y, p, d)
    return best


def measure(png, probe, expect_map, bg):
    from PIL import Image
    img = Image.open(png).convert("RGB")
    W, H = img.size
    px = img.load()
    non_bg = 0
    step = 1
    for y in range(0, H, step):
        for x in range(0, W, step):
            p = px[x, y]
            if abs(p[0]-bg[0]) + abs(p[1]-bg[1]) + abs(p[2]-bg[2]) > 12:
                non_bg += 1
    total = W * H
    report = {"png": os.path.relpath(png, ROOT).replace("\\", "/"), "size": [W, H],
              "bg": list(bg), "non_blank_ratio": round(non_bg / float(total), 4),
              "swatch": [], "row_border": []}

    # (a) 图例色例：纯色块中心，必须逐字命中令牌值
    for st, r in (probe.get("legendSwap", {}).get("status") or {}).items():
        exp = expect_map.get(st)
        if not exp:
            continue
        cx, cy = r["l"] + r["w"] / 2.0, r["t"] + r["h"] / 2.0
        hit = nearest(img, cx, cy, exp, rx=2, ry=2)
        if hit is None:
            report["swatch"].append({"status": st, "expect": list(exp), "out_of_frame": True})
            continue
        x, y, got, d = hit
        report["swatch"].append({"status": st, "expect": list(exp), "px": [x, y], "rgb": list(got),
                                 "dist2": d, "exact": list(got) == list(exp)})

    # (b) 行线框：卡片上边框中点附近取最接近期望色的像素 + 节点填充色（pending 用虚线，靠节点兜底）
    for b in probe.get("boards") or []:
        if b is None:
            continue
        sc = b["scroll"]["rect"]
        for row in b["rows"]:
            exp = expect_map.get(row["eff"])
            if not exp:
                continue
            c = row["card"]
            locked = bool(row.get("locked"))
            exp_eff = list(exp)
            if locked:      # 锁定框整体 opacity .55 → 观测值 = 55% 状态色 + 45% 底色
                exp_eff = [int(round(0.55 * exp[i] + 0.45 * bg[i])) for i in range(3)]
            in_view = (sc["t"] <= c["t"] <= sc["t"] + b["scroll"]["clientH"])
            rec = {"board": b["id"], "item": row["item"], "eff": row["eff"], "locked": locked,
                   "expect": list(exp), "expect_observed": exp_eff,
                   "in_view": in_view, "card": [c["l"], c["t"], c["w"], c["h"]]}
            if in_view:
                hit = nearest(img, c["l"] + c["w"] / 2.0, c["t"], exp_eff, rx=4, ry=3)
                if hit:
                    rec["border_px"], rec["border_rgb"], rec["border_d2"] = [hit[0], hit[1]], list(hit[2]), hit[3]
                n = row["node"]
                hit2 = nearest(img, n["l"] + n["w"] / 2.0, n["t"] + n["h"] / 2.0, exp_eff, rx=2, ry=2)
                if hit2:
                    rec["node_px"], rec["node_rgb"], rec["node_d2"] = [hit2[0], hit2[1]], list(hit2[2]), hit2[3]
                rec["hit"] = (rec.get("border_d2", 1e9) <= 900) or (rec.get("node_d2", 1e9) <= 900)
            report["row_border"].append(rec)
    return report


def main():
    os.makedirs(OUT, exist_ok=True)
    base_url = "file:///" + INDEX.replace("\\", "/")
    dom = chrome_dom(base_url)
    dom_path = os.path.join(OUT, "dom-light.html")
    with open(dom_path, "w", encoding="utf-8") as f:
        f.write(dom)
    probe = probe_from_dom(dom)

    # 「推进一轮」后的状态：?advance=1（第一轮：running→done / pending→running / 闸门放行）
    adv_dom = chrome_dom(base_url + "?advance=1")
    with open(os.path.join(OUT, "dom-light-round1.html"), "w", encoding="utf-8") as f:
        f.write(adv_dom)
    adv = probe_from_dom(adv_dom)

    light_png = os.path.join(OUT, "shot-light.png")
    dark_png = os.path.join(OUT, "shot-dark.png")
    scr_png = os.path.join(OUT, "shot-light-scrolled.png")
    chrome_shot(light_png, base_url)
    chrome_shot(dark_png, base_url + "?theme=dark")
    chrome_shot(scr_png, base_url + "?scroll=240")     # 仅作图证：滚动后 sticky 框头

    readings = {
        "probe": {k: probe.get(k) for k in ("dpr", "innerW", "innerH", "docW", "docH", "theme", "round",
                                            "idempotent", "hashA", "hashB", "scrollKeep", "scrollBefore",
                                            "scrollAfter", "stickyStuck", "stickyDelta", "roleIndex", "viewports")},
        "round1": {"round": adv.get("round"),
                   "main360": {"frames": [{"ms": f["ms"], "locked": f["locked"], "items": f["items"]}
                                          for f in adv["boards"][0]["frames"]],
                               "gates": adv["boards"][0]["gates"],
                               "rows": [{"item": r["item"], "status": r["status"], "eff": r["eff"]}
                                        for r in sorted(adv["boards"][0]["rows"], key=lambda r: r["order"])]}},
        "dom": [],
        "png_light": measure(light_png, probe, EXPECT_LIGHT, BG_LIGHT),
        "png_dark": measure(dark_png, probe, EXPECT_DARK, BG_DARK),
    }

    # DOM 读数：框 / 框内条数 / 行序=拓扑序（打 item id + depth）+ 依赖边几何对照
    for b in probe["boards"]:
        if b is None:
            continue
        rows = sorted(b["rows"], key=lambda r: r["order"])
        pos = {r["item"]: i for i, r in enumerate(rows)}
        violations = []
        for i, r in enumerate(rows):
            for dep in [d.strip() for d in r["deps"].split(",") if d.strip() and d.strip() != "—"]:
                if dep in pos and pos[dep] >= i:
                    violations.append("%s 排在依赖 %s 之前" % (dep, r["item"]))
        # 边的几何对照：路径里的起点/终点必须等于两端节点在内容坐标系里的锚点
        content = b["contentRect"]
        node = {r["item"]: r["node"] for r in b["rows"]}
        geo = []
        for e in b.get("edgesDetail") or []:
            nums = [float(x) for x in re.findall(r"-?\d+(?:\.\d+)?", e["d"])]
            xs, ys, xd, yd = nums[0], nums[1], nums[-1], nums[-2]
            def anchor(it):
                n = node[it]
                return (n["l"] - content["l"] + n["w"], n["t"] - content["t"] + n["h"] / 2.0)
            sx, sy = anchor(e["src"]); dx, dy = anchor(e["dst"])
            geo.append({"src": e["src"], "dst": e["dst"],
                        "path_start": [xs, ys], "node_start": [round(sx, 2), round(sy, 2)],
                        "path_end": [xd, yd], "node_end": [round(dx, 2), round(dy, 2)],
                        "ok": abs(xs-sx) <= 1 and abs(ys-sy) <= 1.5 and abs(xd-dx) <= 1 and abs(yd-dy) <= 1.5})
        readings["dom"].append({
            "board": b["id"], "widthPx": b["widthPx"],
            "scroll": b["scroll"], "contentH": b["contentH"], "svg": b["svg"],
            "edge_geometry": geo,
            "edge_geometry_ok": sum(1 for g in geo if g["ok"]),
            "frames": [{"ms": f["ms"], "locked": f["locked"], "layer": f["layer"], "items": f["items"],
                        "rowIds": f["rowIds"]} for f in b["frames"]],
            "gates": b["gates"],
            "rows": [{"order": r["order"], "item": r["item"], "role": r["role"], "status": r["status"],
                      "eff": r["eff"], "depth": r["depth"], "deps": r["deps"],
                      "interrupted": r["interrupted"]} for r in rows],
            "topo_violations": violations,
            "frame_items_total": sum(f["items"] for f in b["frames"]),
        })

    rp = os.path.join(OUT, "readings.json")
    with open(rp, "w", encoding="utf-8") as f:
        json.dump(readings, f, ensure_ascii=False, indent=1)

    # ---- 控制台报告 ----
    print("== probe ==")
    print(json.dumps(readings["probe"], ensure_ascii=False))
    print("\n== round 1（点一次「推进一轮」）==")
    m = readings["round1"]["main360"]
    for f in m["frames"]:
        print("  frame %-9s locked=%-5s items=%d" % (f["ms"], f["locked"], f["items"]))
    for g in m["gates"]:
        print("  gate  %-24s open=%s" % (g["gate"], g["open"]))
    print("  rows:", ", ".join("%s=%s" % (r["item"], r["status"]) for r in m["rows"]))
    for b in readings["dom"]:
        print("\n== board %s  width=%spx  scroll=%s/%s  contentH=%s  edges=%s(arrowheads %s) svgH=%s ==" %
              (b["board"], b["widthPx"], b["scroll"]["clientH"], b["scroll"]["scrollH"], b["contentH"],
               b["svg"]["edges"], b["svg"]["heads"], b["svg"]["h"]))
        for f in b["frames"]:
            print("  frame %-9s locked=%-5s L%s items=%d  %s" %
                  (f["ms"], f["locked"], f["layer"], f["items"], ",".join(f["rowIds"])))
        for g in b["gates"]:
            print("  gate  %-24s open=%s" % (g["gate"], g["open"]))
        for r in b["rows"]:
            print("    #%d %-15s %-8s %-7s eff=%-7s depth=%d deps=%s" %
                  (r["order"], r["item"], r["role"], r["status"], r["eff"], r["depth"], r["deps"]))
        print("  topo_violations =", b["topo_violations"] or "none")
        print("  edge_geometry ok=%d/%d" % (b["edge_geometry_ok"], len(b["edge_geometry"])))
        for g in b["edge_geometry"]:
            print("    %-15s <- %-15s start %s vs node %s | end %s vs node %s -> ok=%s" %
                  (g["dst"], g["src"], g["path_start"], g["node_start"], g["path_end"], g["node_end"], g["ok"]))
    for key in ("png_light", "png_dark"):
        m = readings[key]
        print("\n== %s %s size=%s non_blank=%.3f ==" % (key, m["png"], m["size"], m["non_blank_ratio"]))
        for s in m["swatch"]:
            print("  swatch %-8s expect=%s px=%s rgb=%s exact=%s" % (s["status"], s["expect"], s["px"], s["rgb"], s["exact"]))
        for s in m["row_border"]:
            if not s["in_view"]:
                print("  row %-18s %-15s %-7s (被滚动裁掉，不采样)" % (s["board"], s["item"], s["eff"]))
                continue
            print("  row %-18s %-15s %-7s locked=%-5s expect=%s obs=%s border=%s/%s d2=%s node=%s/%s d2=%s hit=%s" %
                  (s["board"], s["item"], s["eff"], s["locked"], s["expect"], s["expect_observed"],
                   s.get("border_px"), s.get("border_rgb"),
                   s.get("border_d2"), s.get("node_px"), s.get("node_rgb"), s.get("node_d2"), s.get("hit")))
    print("\nreadings ->", rp)


if __name__ == "__main__":
    main()
