// fit-probe.mjs —— **行高自适应（wi-gantt-fit）** 的无窗口自证工具。
//
// 干什么：用**产品的真渲染件**（gui/frontend/dist/team-board-view.js）在真实浏览器里出图，
// 把「改前 / 改后」两版在**同一份夹具、同一档宽度**下各量一遍：
//   · 逐行 clientHeight/scrollHeight（行高是不是被内容撑起来的）
//   · 名字元素 clientWidth/scrollWidth（名字有没有被截/被挤成伪文字）
//   · 条内回声字 clientWidth/scrollWidth 或 display:none
//   · 里程碑框 clientHeight vs 「框头 + 汇总条 + Σ行 + 下内距」（框是不是跟着行长）
//   · 依赖边每段线的实测位置 vs **从 DOM 反读**的锚点中心（边是不是按实测几何画）
//   · 实测布局跑两遍是否逐字相同（幂等）
//   · 窄栏 370px（产品右栏真实宽度）与宽栏 1180px 两档
//
// 怎么跑（在仓库根目录）：
//   node docs/design/teamwork-gantt/tools/fit-probe.mjs --out docs/design/teamwork-gantt/evidence --before-ref HEAD
// 改前那一版是从 `--before-ref` 这个 git 引用里**原样取出来**的（连它的 import 链一起），
// 所以"改前"永远是事实对照，不是我口头引用的数。
import { execFileSync, spawn } from "node:child_process";
import { createServer } from "node:http";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { extname, join, normalize } from "node:path";

// chrome 要**异步**跑：静态源就起在本进程里，用 execSync 会把事件循环钉住、页面永远取不到
// （第一版就是这么卡住的）。
function runChrome(command) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, { shell: true, stdio: "ignore" });
    child.on("error", reject);
    child.on("exit", code => resolve(code));
  });
}

const args = process.argv.slice(2);
const argOf = (name, fallback) => {
  const index = args.indexOf(name);
  return index >= 0 && args[index + 1] ? args[index + 1] : fallback;
};
const ROOT = process.cwd().replace(/\\/g, "/");
const OUT_ARG = argOf("--out", "docs/design/teamwork-gantt/evidence").replace(/\\/g, "/");
// chrome 的 --screenshot 要**绝对路径**（相对路径它会写到自己那边去），所以这里统一换算。
const OUT = /^[A-Za-z]:\//.test(OUT_ARG) ? OUT_ARG : ROOT + "/" + OUT_ARG;
const TMP = ROOT + "/" + argOf("--tmp", "_tmp/gantt-fit").replace(/\\/g, "/");
const TMP_URL = TMP.slice(ROOT.length); // 静态源挂在仓库根上，探针页的 URL 前缀就是这个
const BEFORE_REF = argOf("--before-ref", "HEAD");
const PORT = Number(argOf("--port", "8793"));
const CHROME = argOf("--chrome", "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe");
const WIDTHS = [370, 1180];

mkdirSync(OUT, { recursive: true });
mkdirSync(TMP + "/before", { recursive: true });

// ── 夹具：三个里程碑屏障串行（m-design → m-impl → m-verify），名字有长有短 ───────────────
const PLAN = {
  team_id: "gantt-fit",
  version: 3,
  members: [
    { role: "artist", role_session_id: "s-fit-artist", worktree: "seelex/artist-fit", status: "idle" },
    { role: "frontend", role_session_id: "s-fit-frontend", worktree: "seelex/frontend-fit", status: "running" },
  ],
  milestones: [
    { id: "m-design", name: "设计", status: "done", content: "静态稿经 leader 逐字节复核" },
    { id: "m-impl", name: "实现", depends_on: ["m-design"], status: "running" },
    { id: "m-verify", name: "独立验证", depends_on: ["m-impl"], status: "pending" },
  ],
  work_items: [
    { id: "wi-spec", milestone: "m-design", role: "artist", name: "规格：一页对比表", status: "done", session_id: "s-fit-a1", worktree: "seelex/artist-fit", goal: "把三档宽度的读数压成一页" },
    { id: "wi-draw", milestone: "m-design", role: "artist", name: "出图：teamwork 里程碑/工作项 DAG 静态 UI 稿", status: "done", depends_on: ["wi-spec"], session_id: "s-fit-a2", worktree: "seelex/artist-fit", goal: "静态稿要能在 370px 右栏读得完" },
    { id: "wi-impl", milestone: "m-impl", role: "frontend", name: "实现：行高由内容撑", status: "running", depends_on: ["wi-draw"], session_id: "s-fit-f1", worktree: "seelex/frontend-fit", goal: "字撑行、框随行长、边按实测几何" },
    { id: "wi-geo", milestone: "m-impl", role: "frontend", name: "几何：边按实测位置画", status: "pending", depends_on: ["wi-impl"], session_id: "s-fit-f2", worktree: "seelex/frontend-fit", goal: "layouts pass 幂等" },
    { id: "wi-verify", milestone: "m-verify", role: "artist", name: "复核", status: "pending", depends_on: ["wi-impl"], session_id: "s-fit-a3", worktree: "seelex/artist-fit", goal: "独立复跑两档读数" },
  ],
};

// ── 改前那一版：从 git 引用里取出来（含 import 链），落到 <tmp>/before/ ────────────────
function pullBefore() {
  const seen = new Set();
  const pull = name => {
    if (seen.has(name)) return;
    seen.add(name);
    const body = execFileSync("git", ["show", `${BEFORE_REF}:gui/frontend/dist/${name}`], { maxBuffer: 1e8 });
    writeFileSync(`${TMP}/before/${name}`, body);
    for (const hit of body.toString("utf8").matchAll(/from\s+"\.\/([\w.-]+\.js)"/g)) pull(hit[1]);
  };
  pull("team-board-view.js");
  return [...seen];
}

// ── 探针页：内联夹具 + 真渲染件 + 页内读数 ─────────────────────────────────────────
function page(variant) {
  const before = variant === "before";
  const renderer = before ? "./before/team-board-view.js" : "../../gui/frontend/dist/team-board-view.js";
  const styles = "../../gui/frontend/dist/styles.css";
  const layoutCall = before ? "/* 改前那版没有实测布局这一步：边的 y 是渲染时写好的 calc 算式 */" : "layoutTeamGanttEdges(document);";
  const layoutImport = before ? "" : ", layoutTeamGanttEdges";
  return `<!doctype html>
<html lang="zh" data-theme="light"><head><meta charset="utf-8">
<title>gantt fit probe (${variant})</title>
<link rel="stylesheet" href="${styles}">
<style id="board-css"></style>
<style>
 body { margin: 0; padding: 10px; background: var(--panel-solid, #fff); font: 13px/1.45 "Segoe UI", system-ui, sans-serif; }
 #host { width: 370px; box-sizing: content-box; border: 1px dashed #bbb; background: var(--panel-solid, #fff); }
 #probe { white-space: pre-wrap; font: 11px/1.35 Consolas, monospace; color: #222; }
</style></head>
<body>
<div id="host"></div>
<script type="application/json" id="plan">${JSON.stringify(PLAN)}</script>
<script type="module">
import { TEAM_BOARD_CSS, renderTeamBoard${layoutImport} } from "${renderer}";

document.getElementById("board-css").textContent = TEAM_BOARD_CSS;
const trap = (message, where) => {
  document.getElementById("probe")?.remove();
  const pre = document.createElement("pre");
  pre.id = "probe";
  pre.textContent = "探针报错：" + message + " @ " + where;
  document.body.appendChild(pre);
};
window.addEventListener("error", event => trap(String(event.message), event.filename + ":" + event.lineno));
window.addEventListener("unhandledrejection", event => trap(String(event.reason), "unhandledrejection"));

const plan = JSON.parse(document.getElementById("plan").textContent);
const host = document.getElementById("host");
const width = Number(new URLSearchParams(location.search).get("w") || 370);
host.style.width = width + "px";
const round = value => Math.round(value * 100) / 100;
const out = { variant: "${variant}", width, rows: [], frames: [], checks: {} };

// 产品调用姿势：app.js 就是这么用的（renderTeamBoard → innerHTML），实测由渲染件自己排。
host.innerHTML = renderTeamBoard({ plan, jobs: [], events: [], maxMembers: 6 });
await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));

// **产品自己**排的那次实测做没做（在显式再跑之前先读）：这一条是"调用方不用改"的证据。
{
  const layer = host.querySelector(".team-dag-edges");
  const seg = host.querySelector(".team-dag-edge i");
  out.checks.productPass = {
    laidOut: layer ? layer.getAttribute("data-laid-out") : null,
    visibility: layer ? getComputedStyle(layer).visibility : null,
    firstSegTop: seg ? seg.getAttribute("style") : null,
  };
}

const board = host.querySelector(".team-board");
const scroll = host.querySelector(".team-dag-scroll");
const content = host.querySelector(".team-dag-content");
const cssVar = name => getComputedStyle(board).getPropertyValue(name).trim();
const slotW = getComputedStyle(scroll).getPropertyValue("--team-dag-slot-w").trim();
const contentRect = content.getBoundingClientRect();
const relTop = el => round(el.getBoundingClientRect().top - contentRect.top);
const center = el => { const rect = el.getBoundingClientRect(); return round(rect.top - contentRect.top + rect.height / 2); };

out.geom = {
  boardWidth: round(board.getBoundingClientRect().width),
  labelW: cssVar("--team-dag-label-w"), slotW, rowH: cssVar("--team-dag-row-h"), barH: cssVar("--team-dag-bar-h"),
  queryApplied: slotW !== "64px",
};

for (const row of host.querySelectorAll(".team-dag-row")) {
  const name = row.querySelector(".team-dag-name");
  const barName = row.querySelector(".team-dag-bar-name");
  const bar = row.querySelector(".team-dag-bar");
  const barNameStyle = getComputedStyle(barName);
  const nameBox = name ? name.getBoundingClientRect() : { height: 0 };
  const lineH = name ? Number.parseFloat(getComputedStyle(name).lineHeight) || 16 : 16;
  const barW = round(bar.getBoundingClientRect().width);
  const barInner = barW - 7 - 4; // 条内回声的可用宽（left:7px / right:4px）
  out.rows.push({
    id: row.dataset.itemId,
    rowH: row.clientHeight, rowScrollH: row.scrollHeight,
    nameText: (name?.textContent || "").trim(),
    nameClientW: name ? name.clientWidth : null,
    nameScrollW: name ? name.scrollWidth : null,
    nameLines: Math.max(1, Math.round(nameBox.height / lineH)),
    nameTruncated: name ? name.scrollWidth > name.clientWidth + 1 : null,
    barNameDisplay: barNameStyle.display,
    barNameClientW: barName ? barName.clientWidth : null,
    barNameScrollW: barName ? barName.scrollWidth : null,
    // 条内回声的判据：要么整段不画（窄栏），要么**可见宽 ≥ 它的可用宽 60%**（半截字不合格）。
    barNameBoxRatio: barName ? round(barName.clientWidth / Math.max(1, barInner)) : null,
    barW,
    barLeft: round(bar.getBoundingClientRect().left - row.querySelector(".team-dag-plot").getBoundingClientRect().left),
  });
}

const headH = Number.parseFloat(cssVar("--team-dag-head-h"));
const sumH = Number.parseFloat(cssVar("--team-dag-sum-h"));
for (const frame of host.querySelectorAll(".team-dag-frame")) {
  const rowsH = [...frame.querySelectorAll(".team-dag-row")].reduce((sum, row) => sum + row.clientHeight, 0);
  out.frames.push({
    ms: frame.dataset.milestoneId, clientH: frame.clientHeight, rows: frame.querySelectorAll(".team-dag-row").length, rowsH,
    wantClientH: headH + sumH + rowsH + 4, delta: round(frame.clientHeight - (headH + sumH + rowsH + 4)),
  });
}

// 实测锚点表：**从 DOM 反读**（行 / 汇总条那一行的中心），不看渲染件的任何中间量。
const anchors = new Map();
for (const row of host.querySelectorAll(".team-dag-row")) anchors.set("row:" + row.dataset.itemId, center(row));
for (const sum of host.querySelectorAll(".team-dag-ms-sum")) anchors.set("sum:" + sum.dataset.ms, center(sum));
const attrNum = (el, name) => (el.getAttribute(name) === null ? 0 : Number(el.getAttribute(name)));

let worst = 0, mismatches = 0, segCount = 0;
for (const edge of host.querySelectorAll(".team-dag-edge")) {
  for (const seg of edge.querySelectorAll("i")) {
    segCount += 1;
    const anchor = anchors.get(seg.getAttribute("data-y"));
    if (anchor === undefined || anchor === null) continue;
    const wantTop = round(anchor + attrNum(seg, "data-y-dy"));
    const delta = round(relTop(seg) - wantTop);
    worst = Math.max(worst, Math.abs(delta));
    if (Math.abs(delta) > 0.51) mismatches += 1;
    const anchor2 = anchors.get(seg.getAttribute("data-y2"));
    if (anchor2 !== undefined && anchor2 !== null) {
      const wantH = Math.max(0, round(anchor2 + attrNum(seg, "data-y2-dy") - wantTop));
      const dH = round(seg.getBoundingClientRect().height - wantH);
      worst = Math.max(worst, Math.abs(dH));
      if (Math.abs(dH) > 0.51) mismatches += 1;
    }
  }
}
out.checks.edges = { segCount, worstDelta: worst, mismatches };

const snapshot = () => [...host.querySelectorAll(".team-dag-edge i")].map(seg => seg.getAttribute("style")).join("|");
const first = snapshot();
${layoutCall}
const second = snapshot();
${layoutCall}
out.checks.idempotent = first === second && second === snapshot();

const squeezed = [];
for (const el of host.querySelectorAll(".team-dag-name, .team-dag-bar-name, .team-dag-id, .team-dag-ms-content")) {
  if (getComputedStyle(el).display === "none" || !el.textContent.trim()) continue;
  const avail = Math.min(el.scrollWidth, el.clientWidth + 1);
  if (el.clientWidth < 0.4 * avail) squeezed.push({ cls: el.className, clientW: el.clientWidth, scrollW: el.scrollWidth });
}
out.checks.squeezed = squeezed;
out.checks.scroll = { clientH: scroll.clientHeight, scrollH: scroll.scrollHeight, maxH: getComputedStyle(scroll).maxHeight };
out.checks.edgeLayer = {
  laidOut: host.querySelector(".team-dag-edges").getAttribute("data-laid-out"),
  visibility: getComputedStyle(host.querySelector(".team-dag-edges")).visibility,
};

document.getElementById("probe")?.remove();
const pre = document.createElement("pre");
pre.id = "probe";
pre.textContent = JSON.stringify(out, null, 1);
document.body.appendChild(pre);
</script>
</body></html>`;
}

// ── 起一个静态源（探针页要 import 产品渲染件，file:// 下模块脚本被 CORS 挡）──────────
const TYPES = { ".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8" };
const server = createServer(async (request, response) => {
  try {
    const path = decodeURIComponent(new URL(request.url, "http://x").pathname);
    const file = join(ROOT, normalize(path).replace(/^[/\\]+/, ""));
    const body = readFileSync(file);
    response.writeHead(200, { "content-type": TYPES[extname(file)] || "application/octet-stream", "cache-control": "no-store" });
    response.end(body);
  } catch (error) {
    response.writeHead(404, { "content-type": "text/plain" });
    response.end("not found");
  }
});
await new Promise(resolve => server.listen(PORT, "127.0.0.1", resolve));

const chromeTail = (width, tail) =>
  `"${CHROME}" --headless=new --disable-gpu --no-first-run --no-default-browser-check ` +
  `--user-data-dir="${TMP}/chrome-profile" --virtual-time-budget=4000 --window-size=${width + 120},1500 ${tail}`;

async function readVariant(variant, width) {
  const file = `${TMP}/probe-${variant}.html`;
  await runChrome(`${chromeTail(width, `--dump-dom "http://127.0.0.1:${PORT}${TMP_URL}/probe-${variant}.html?w=${width}" > "${file}.dom"`)} 2> "${TMP}/chrome.err"`);
  const dom = readFileSync(`${file}.dom`, "utf8");
  const hit = dom.match(/<pre id="probe">([\s\S]*?)<\/pre>/);
  if (!hit) throw new Error(`${variant} · ${width}px 探针没出读数：` + (dom.match(/探针报错[^<]*/) || ["（也没有报错）"])[0]);
  return JSON.parse(hit[1].replace(/&quot;/g, '"').replace(/&amp;/g, "&").replace(/&lt;/g, "<").replace(/&gt;/g, ">"));
}

async function shoot(variant, width, file) {
  await runChrome(`${chromeTail(width, `--screenshot="${file}" "http://127.0.0.1:${PORT}${TMP_URL}/probe-${variant}.html?w=${width}"`)} 2> "${TMP}/chrome.err"`);
  return file;
}

// ── 跑 ────────────────────────────────────────────────────────────────────────
const beforeFiles = pullBefore();
writeFileSync(`${TMP}/probe-after.html`, page("after"), "utf8");
writeFileSync(`${TMP}/probe-before.html`, page("before"), "utf8");

const readings = { fixture: PLAN, widths: WIDTHS, beforeRef: BEFORE_REF, beforeFiles, after: {}, before: {} };
for (const width of WIDTHS) {
  readings.after[width] = await readVariant("after", width);
  readings.before[width] = await readVariant("before", width);
}
await shoot("after", 370, `${OUT}/fit-narrow-370.png`);
await shoot("before", 370, `${OUT}/fit-narrow-370-before.png`);
writeFileSync(`${OUT}/fit-readings.json`, JSON.stringify(readings, null, 2), "utf8");

function table(label, data) {
  const lines = [`── ${label}（宿主 ${data.width}px · 容器查询 ${data.geom.queryApplied ? "生效" : "未生效"} · labelW=${data.geom.labelW} slotW=${data.geom.slotW} rowH=${data.geom.rowH}）`];
  for (const row of data.rows) {
    lines.push(`  ${row.id.padEnd(9)} 行高 ${String(row.rowH).padStart(3)}（scroll ${String(row.rowScrollH).padStart(3)}）· 名字 ${String(row.nameClientW).padStart(3)}/${String(row.nameScrollW).padStart(4)}（折 ${row.nameLines} 行${row.nameTruncated ? "，**被截**" : "，读得完"}）· 条内回声 ${row.barNameDisplay === "none" ? "display:none（不画）" : row.barNameClientW + "/" + row.barNameScrollW + "，占可用宽 " + Math.round(row.barNameBoxRatio * 100) + "%"} · 条宽 ${row.barW}px @${row.barLeft} · 「${row.nameText}」`);
  }
  for (const frame of data.frames) {
    lines.push(`  框 ${frame.ms.padEnd(10)} clientH ${frame.clientH} vs 头+汇总+Σ行+4 = ${frame.wantClientH}（Δ${frame.delta}）· ${frame.rows} 行 Σ${frame.rowsH}`);
  }
  lines.push(`  依赖边：${data.checks.edges.segCount} 段 · 与实测锚点的最大偏差 ${data.checks.edges.worstDelta}px · 超 0.5px 的段 ${data.checks.edges.mismatches} · data-laid-out=${data.checks.edgeLayer.laidOut} · visibility=${data.checks.edgeLayer.visibility} · 幂等=${data.checks.idempotent}`);
  lines.push(`  产品自己那次实测：${JSON.stringify(data.checks.productPass)}`);
  lines.push(`  挤成伪文字：${data.checks.squeezed.length ? JSON.stringify(data.checks.squeezed) : "无"} · 滚动 ${data.checks.scroll.clientH}/${data.checks.scroll.scrollH}（max-height ${data.checks.scroll.maxH}）`);
  return lines.join("\n");
}

const report = [];
report.push(table("改后 · 窄栏 370px（产品右栏真实宽度）", readings.after["370"]));
report.push(table("改前 · 窄栏 370px（" + BEFORE_REF + " 那一版）", readings.before["370"]));
report.push(table("改后 · 宽栏 1180px", readings.after["1180"]));
report.push(table("改前 · 宽栏 1180px（" + BEFORE_REF + " 那一版）", readings.before["1180"]));
writeFileSync(`${OUT}/fit-readings.txt`, report.join("\n\n") + "\n", "utf8");
console.log(report.join("\n\n"));

server.close();
