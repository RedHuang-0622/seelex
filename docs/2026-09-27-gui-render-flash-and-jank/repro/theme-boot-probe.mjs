// 复现「启动主题闪白（FOUC）」：用真实 gui/frontend/dist/index.html + 真实 app.js/theme.js，
// 在 headless Chromium（与 WebView2 同引擎族）里量「首帧主题」与「主题落地时刻」的先后。
//
// 判据（机械可判）：
//   themeAt  =  <html data-theme> 首次从「首帧值」变成别的值的时刻（theme.js putMode 落地）
//   firstPaint / fcp = 浏览器首次绘制 / 首次内容绘制的时刻
//   themeAt > firstPaint   →  至少有一帧是按旧主题（浅色）画出来的 = 用户可见闪白
//   闪白时长 = themeAt - firstPaint
//
// 对照组：stored mode = dark（应闪）vs light（不应闪）。
//
// 用法：
//   node docs/2026-09-27-gui-render-flash-and-jank/repro/theme-boot-probe.mjs
import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { execFileSync } from "node:child_process";
import path from "node:path";

const ROOT = path.resolve("gui/frontend/dist");
const CHROME_CANDIDATES = [
  "C:/Program Files/Google/Chrome/Application/chrome.exe",
  "C:/Program Files (x86)/Google/Chrome/Application/chrome.exe",
  "/usr/bin/google-chrome",
  "/usr/bin/chromium"
];

const MIME = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".svg": "image/svg+xml",
  ".png": "image/png",
  ".woff2": "font/woff2",
  ".map": "application/json"
};

// PROBE 注入在 <head> 的第一行：必须先于样式表与 deferred module 执行。
const PROBE = `<script>
(() => {
  const probe = { t0: 0, firstPaint: null, fcp: null, themeAt: null, skinLinkAt: null,
                  storedMode: null, storedSkin: null, settledTheme: null, settledBg: null };
  window.__flashProbe = probe;
  probe.t0 = performance.now();
  try {
    probe.storedMode = localStorage.getItem("seelex.mode");
    probe.storedSkin = localStorage.getItem("seelex.skin");
  } catch (e) { probe.storageError = String(e); }
  const attrs = new MutationObserver(() => {
    const value = document.documentElement.getAttribute("data-theme");
    if (probe.themeAt === null && value && value !== (probe.initialTheme || null)) probe.themeAt = performance.now();
  });
  probe.initialTheme = document.documentElement.getAttribute("data-theme");
  attrs.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
  new MutationObserver(() => {
    if (probe.skinLinkAt === null && document.getElementById("seelex-skin")) probe.skinLinkAt = performance.now();
  }).observe(document.head, { childList: true, subtree: true });
  function paints() {
    const list = performance.getEntriesByType("paint") || [];
    probe.firstPaint = Number((list.find(p => p.name === "first-paint") || {}).startTime || 0) || null;
    probe.fcp = Number((list.find(p => p.name === "first-contentful-paint") || {}).startTime || 0) || null;
  }
  paints();
  setTimeout(paints, 0);
  window.addEventListener("load", () => setTimeout(() => {
    paints();
    probe.settledTheme = document.documentElement.getAttribute("data-theme");
    probe.settledBg = getComputedStyle(document.documentElement).getPropertyValue("--bg").trim();
    const out = document.createElement("pre");
    out.id = "flash-probe-out";
    out.style.cssText = "position:fixed;left:0;bottom:0;z-index:2147483647;font:10px monospace";
    out.textContent = JSON.stringify(probe);
    document.body.appendChild(out);
  }, 1500));
})();
</script>`;

function injectProbe(html) {
  const at = html.indexOf("<head>");
  if (at < 0) throw new Error("index.html 没有 <head>");
  return html.slice(0, at + "<head>".length) + PROBE + html.slice(at + "<head>".length);
}

function seedPage(mode) {
  return `<!doctype html><meta charset="utf-8"><script>
    try { localStorage.setItem("seelex.mode", ${JSON.stringify(mode)}); localStorage.setItem("seelex.skin", "qoder"); }
    catch (e) {}
    location.replace("/index.html");
  </script>`;
}

const server = createServer(async (req, res) => {
  const url = new URL(req.url, "http://127.0.0.1");
  try {
    if (url.pathname === "/__seed-dark" || url.pathname === "/__seed-light") {
      const mode = url.pathname.endsWith("dark") ? "dark" : "light";
      res.writeHead(200, { "content-type": MIME[".html"] });
      res.end(seedPage(mode));
      return;
    }
    const rel = url.pathname === "/" ? "index.html" : decodeURIComponent(url.pathname).replace(/^\/+/, "");
    if (rel.includes("..")) { res.writeHead(403).end(); return; }
    let body = await readFile(path.join(ROOT, rel));
    if (rel === "index.html") body = Buffer.from(injectProbe(body.toString("utf8")), "utf8");
    res.writeHead(200, { "content-type": MIME[path.extname(rel)] || "application/octet-stream" });
    res.end(body);
  } catch {
    res.writeHead(404, { "content-type": "text/plain" }).end("not found");
  }
});

function chromePath() {
  for (const candidate of CHROME_CANDIDATES) {
    try {
      execFileSync(candidate, ["--version"], { stdio: "ignore" });
      return candidate;
    } catch { /* try next */ }
  }
  throw new Error("找不到 chrome/chromium");
}

function probe(mode) {
  const chrome = chromePath();
  const out = execFileSync(chrome, [
    "--headless=new", "--disable-gpu", "--no-first-run", "--disable-extensions",
    "--window-size=1440,900", "--virtual-time-budget=6000", "--dump-dom",
    `http://127.0.0.1:${port}/__seed-${mode}`
  ], { encoding: "utf8", maxBuffer: 512 * 1024 * 1024, stdio: ["ignore", "pipe", "ignore"] });
  const match = out.match(/<pre id="flash-probe-out"[^>]*>([\s\S]*?)<\/pre>/);
  if (!match) throw new Error(`探针没落地（dump 长度 ${out.length}）mode=${mode}`);
  return JSON.parse(match[1].replaceAll("&quot;", '"').replaceAll("&amp;", "&").replaceAll("&lt;", "<").replaceAll("&gt;", ">"));
}

let port = 0;
await new Promise(resolve => server.listen(0, "127.0.0.1", () => { port = server.address().port; resolve(); }));

try {
  const rows = [];
  for (const mode of ["dark", "light"]) {
    const p = probe(mode);
    rows.push({
      storedMode: mode,
      firstFrameTheme: p.initialTheme,
      settledTheme: p.settledTheme,
      firstPaintMs: p.firstPaint === null ? null : Number(p.firstPaint.toFixed(0)),
      fcpMs: p.fcp === null ? null : Number(p.fcp.toFixed(0)),
      themeAppliedAtMs: p.themeAt === null ? null : Number(p.themeAt.toFixed(0)),
      skinLinkAtMs: p.skinLinkAt === null ? null : Number(p.skinLinkAt.toFixed(0)),
      flashMs: p.themeAt === null || p.firstPaint === null ? 0 : Number((p.themeAt - p.firstPaint).toFixed(0)),
      settledBg: p.settledBg
    });
  }
  console.log(JSON.stringify(rows, null, 2));
} finally {
  server.close();
}
