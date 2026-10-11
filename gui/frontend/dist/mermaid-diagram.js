// ── mermaid 图表渲染（本地 vendor，懒加载）──────────────────
// markdown 里语言标记为 `mermaid` 的代码块由 markdown.js 渲染成
// `.mermaid-diagram[data-mermaid-diagram]` 容器：容器里先放一份**源码代码块**
// （`pre.mermaid-source > code.language-mermaid`），再放一枚空的画布
// （`.mermaid-canvas[data-mermaid-canvas][hidden]`）。本模块负责把源码渲染成图：
//
//   - 库体走既有懒加载通道 vendor-loader.js: ensureVendorScript（与 PDF.js /
//     docx-preview / xterm 同一条路），**不在 index.html 尾部同步加载**——mermaid
//     是几百 KB 的重型 vendor，不能为它让每次启动都付 parse/compile；
//   - 离线：资产是本地 `vendor/mermaid/mermaid.min.js`（UMD、单文件、无 CDN、
//     无动态 chunk 请求），随 embed 打包进二进制；
//   - 安全：mermaid 产出的 SVG **必须经 DOMPurify 白名单放行**后才写入画布
//     （markdown 走净化，图表不为它破例）。净化器缺席或净化失败一律不放行；
//   - 降级：库未就绪 / 超时 / 渲染抛错 / SVG 未过净化，一律保留源码代码块并标
//     `data-mermaid-state="failed"`——**绝不白屏**；超时与重试由 ensureVendorScript
//     负责（失败不缓存，下次进入还能再试）。
//
// 幂等：已处理的容器（有 `data-mermaid-state`）不重复渲染；流式场景下内容变化会
// 换掉整个容器元素（见 conversation-view.js 的 keyed reconcile），新容器自然重试。

import { ensureVendorScript } from "./vendor-loader.js";

const MERMAID_LIB_SRC = new URL("./vendor/mermaid/mermaid.min.js", import.meta.url).toString();
const DIAGRAM_SELECTOR = "[data-mermaid-diagram]";
const SOURCE_SELECTOR = "pre.mermaid-source";
const SOURCE_CODE_SELECTOR = ".mermaid-source code";
const CANVAS_SELECTOR = "[data-mermaid-canvas]";

let initialized = false;
let diagramSeq = 0;

function mermaidAPI() {
  return globalThis.mermaid || (typeof window !== "undefined" ? window.mermaid : null) || null;
}

function purifyAPI() {
  return globalThis.DOMPurify || (typeof window !== "undefined" ? window.DOMPurify : null) || null;
}

async function ensureMermaid() {
  const ready = mermaidAPI();
  if (ready) {
    initialize(ready);
    return ready;
  }
  await ensureVendorScript(MERMAID_LIB_SRC, () => Boolean(mermaidAPI()));
  const api = mermaidAPI();
  if (!api || typeof api.render !== "function") throw new Error("mermaid 未就绪（vendor/mermaid 缺失？）");
  initialize(api);
  return api;
}

function initialize(api) {
  if (initialized || typeof api.initialize !== "function") return;
  const root = typeof document !== "undefined" ? document.documentElement : null;
  const prefersDark = Boolean(root && root.dataset && root.dataset.theme === "dark");
  api.initialize({
    startOnLoad: false,
    // 图表里的文本只可能是文件内容，strict 关掉 HTML 标签与点击事件。
    securityLevel: "strict",
    theme: prefersDark ? "dark" : "default"
  });
  initialized = true;
}

// sanitizeSVG 用 DOMPurify 的 SVG 白名单放行 mermaid 的结构：`svg` / `svgFilters`
// 两档 profile 覆盖形状、渐变、滤镜与 marker；mermaid 的节点标签默认走
// `<foreignObject>` 内嵌 HTML（flowchart 的 htmlLabels），所以 `html` profile 也要
// 放行——否则标签会被整片剥掉，只剩空框。`style` 元素与属性在内联 CSS 净化下放行
// （mermaid 把配色与布局都写在 `<style>` 里，禁掉就成了黑白空图）。脚本、外链嵌入
// 与事件属性一律拒（DOMPurify 默认属性白名单本身就不含 on*）。净化器缺席返回空串
// ⇒ 调用方降级，绝不放未净化内容。
function sanitizeSVG(svg) {
  const purify = purifyAPI();
  if (!purify || typeof purify.sanitize !== "function") return "";
  return purify.sanitize(String(svg), {
    USE_PROFILES: { svg: true, svgFilters: true, html: true },
    ADD_TAGS: ["style", "foreignObject"],
    FORBID_TAGS: ["script", "iframe", "object", "embed", "form", "input", "button"],
    FORBID_ATTR: ["onload", "onerror", "onclick", "onmouseover"]
  });
}

function readSource(block) {
  const code = typeof block.querySelector === "function" ? block.querySelector(SOURCE_CODE_SELECTOR) : null;
  return code ? String(code.textContent || "") : "";
}

function markState(block, state) {
  if (block && block.dataset) block.dataset.mermaidState = state;
}

function applySVG(block, svg) {
  const canvas = typeof block.querySelector === "function" ? block.querySelector(CANVAS_SELECTOR) : null;
  if (!canvas) throw new Error("mermaid 容器缺少画布节点");
  canvas.innerHTML = svg;
  canvas.hidden = false;
  const source = typeof block.querySelector === "function" ? block.querySelector(SOURCE_SELECTOR) : null;
  if (source) source.hidden = true;
}

async function renderOne(block) {
  const source = readSource(block);
  if (!source.trim()) {
    markState(block, "failed");
    return false;
  }
  const api = await ensureMermaid();
  const id = `seelex-mermaid-${++diagramSeq}`;
  const result = await api.render(id, source);
  const svg = typeof result === "string" ? result : result && result.svg;
  if (!svg) throw new Error("mermaid 未产出 SVG");
  const clean = sanitizeSVG(svg);
  if (!clean) throw new Error("mermaid SVG 未通过净化（DOMPurify 缺席或净化失败）");
  applySVG(block, clean);
  markState(block, "ready");
  return true;
}

// hydrateMermaid 把 root 内所有未处理的 mermaid 容器渲染成图，返回成功条数。
// 它**不抛错**：任何失败都落成容器的 `data-mermaid-state="failed"`，源码代码块
// 继续可读（调用方可以是流式渲染路径，不需要为它加 try/catch）。
export async function hydrateMermaid(root) {
  if (!root || typeof root.querySelectorAll !== "function") return 0;
  let blocks;
  try {
    blocks = [...root.querySelectorAll(DIAGRAM_SELECTOR)];
  } catch {
    return 0;
  }
  let rendered = 0;
  for (const block of blocks) {
    const state = block && block.dataset ? block.dataset.mermaidState : "";
    if (state) continue; // 已成功或已失败：不重试（换容器时自然重来）
    try {
      if (await renderOne(block)) rendered += 1;
    } catch {
      markState(block, "failed");
    }
  }
  return rendered;
}

export const MERMAID_VENDOR_SRC = MERMAID_LIB_SRC;
