// 沙箱渲染块的**换肤通道**（宿主 → 块内，单向）。
//
// 为什么必须由宿主推：块是 `sandbox="allow-scripts"` 且**没有** `allow-same-origin` 的
// iframe，是 opaque origin——宿主读不到它的 DOM，它也读不到宿主的 CSS 变量与
// `<html data-theme>`。所以"块跟着皮肤与明暗走"这件事只有一条路：宿主把 token 读出来、
// 发进去；块内的接收端在 html-embed.js 的桥脚本里（协议由发送端定义，见那边的
// `EMBED_HOST_TAG` / `EMBED_THEME_MESSAGE` 与 `applyTheme`）。
//
// 三个判据：
//   1) **按名字映射，不搬值**：本文件只声明"宿主哪个变量 → 沙箱哪个变量"（两张表见下），
//      颜色事实仍然只有一份，在 styles.css / themes/*.css 里。宿主换肤 = 换供源，这里不
//      新增第二套色值。
//   2) **语义色不进皮肤**：`--sx-warn/danger/ok/info` 只映射宿主的 `--status-*` 一族
//      （它们在宿主里就是 skin-independent 的），因此"警告"永远是那个警告色，不会被
//      品牌色染成主信号。品牌相关的只有 base 一套（surface/text/border/accent/...）。
//   3) **发的是变量，不是代码**：载荷只允许 `--sx-*` 自定义属性，块内接收端还会再校验
//      一遍（名字白名单 + 值里不许出现 `;{}<>`、`url(`、`expression(`、`@import`）。
//      所以这条通道**不授予任何能力**：它改不了 DOM、读不到数据、发不出请求。
//
// 什么时候发：帧 `load` 时补发一次（新块永远先拿到当前主题），以及每次换肤/切深浅后
// 广播一次（app.js 的 themeController `onApplied`）。帧不重载、srcdoc 不重建，块内的
// 交互状态因此不丢。

import {
  EMBED_DEFAULT_TOKENS,
  EMBED_HOST_TAG,
  EMBED_THEME_MESSAGE,
  EMBED_THEME_TOKENS_DARK,
  EMBED_THEME_TOKENS_LIGHT
} from "./html-embed.js";

// EMBED_THEME_VARS：**沙箱变量 → 宿主变量**。左边是块内写的那一套（`--sx-*`），右边是
// styles.css / 皮肤包里的既有 token。皮肤只会改 `--accent*` / `--on-accent` / 渐变，
// 其余中性面在深浅两套基座里——所以换皮肤、切深浅都会经由这张表流进块内。
export const EMBED_THEME_VARS = Object.freeze({
  "--sx-bg": "--bg",
  "--sx-panel": "--panel",
  "--sx-surface": "--surface",
  "--sx-surface-2": "--surface-2",
  "--sx-border": "--border",
  "--sx-border-strong": "--border-strong",
  "--sx-text": "--text",
  "--sx-text-strong": "--text-strong",
  "--sx-text-soft": "--text-soft",
  "--sx-muted": "--muted",
  "--sx-faint": "--faint",
  "--sx-accent": "--accent",
  "--sx-on-accent": "--on-accent",
  "--sx-code-bg": "--code-bg",
  "--sx-font-ui": "--font-ui",
  "--sx-font-mono": "--font-mono",
  "--sx-radius-sm": "--r-sm",
  "--sx-radius": "--r-md",
  "--sx-radius-lg": "--r-lg",
  "--sx-duration-fast": "--duration-fast",
  "--sx-duration-normal": "--duration-normal",
  "--sx-ease-out": "--ease-out",
  "--sx-ease-in": "--ease-in"
});

// EMBED_SEMANTIC_VARS：语义色单独一张表，故意只取 `--status-*` / `--tint-*` /
// `--border-*` 这些**只随明暗、不随皮肤**的 token。它们与 EMBED_THEME_VARS 必须不相交
// （embed-theme.test.mjs 钉住），否则"警告色跟着品牌走"就会悄悄回来。
export const EMBED_SEMANTIC_VARS = Object.freeze({
  "--sx-warn": "--status-running",
  "--sx-ok": "--status-done",
  "--sx-danger": "--status-failed",
  "--sx-info": "--status-info",
  "--sx-tint-warn": "--tint-running",
  "--sx-tint-ok": "--tint-done",
  "--sx-tint-danger": "--tint-failed",
  "--sx-tint-info": "--tint-info",
  "--sx-border-warn": "--border-running",
  "--sx-border-ok": "--border-done",
  "--sx-border-danger": "--border-failed",
  "--sx-border-info": "--border-info"
});

export const EMBED_THEME_SOURCES = Object.freeze({
  ...EMBED_THEME_VARS,
  ...EMBED_SEMANTIC_VARS
});

const TOKEN_NAME = /^--sx-[a-z0-9-]{1,32}$/;
const TOKEN_VALUE_BAD = /[;{}<>\\]|url\s*\(|expression\s*\(|@import/i;
const TOKEN_VALUE_MAX = 256;

// sanitizeTokenValue 只做一件事：把一个 CSS 变量值收敛成"能安全写进 setProperty"
// 的形态。非法值直接丢（返回空串）而不是清洗——半个 `url(javascript:` 拼出来的东西
// 比没有更糟。
export function sanitizeTokenValue(raw) {
  const value = String(raw ?? "").trim();
  if (!value || value.length > TOKEN_VALUE_MAX) return "";
  if (TOKEN_VALUE_BAD.test(value)) return "";
  return value;
}

// readEmbedTheme 从宿主页面读当前外观：`mode` 取 `<html data-theme>`（styles.css 的
// 浅色基座是 `:root` 本身，所以没有该属性时按浅色算），token 逐个走 getComputedStyle。
// 读不到（非浏览器 / 变量未定义）时回落到与沙箱同一份缺省（EMBED_DEFAULT_TOKENS 的
// 明暗对应版本），保证任何情况下推出去的都是一套完整 token，不会留下半套。
export function readEmbedTheme(win = typeof globalThis === "undefined" ? null : globalThis.window) {
  const doc = win && win.document;
  const root = doc && doc.documentElement;
  const mode = root && root.dataset && root.dataset.theme === "dark" ? "dark" : "light";
  const base = mode === "dark" ? EMBED_THEME_TOKENS_DARK : EMBED_THEME_TOKENS_LIGHT;
  const computed = root && typeof win.getComputedStyle === "function" ? win.getComputedStyle(root) : null;
  const tokens = {};
  for (const [sandboxVar, sourceVar] of Object.entries(EMBED_THEME_SOURCES)) {
    const raw = computed && typeof computed.getPropertyValue === "function"
      ? computed.getPropertyValue(sourceVar)
      : "";
    tokens[sandboxVar] = sanitizeTokenValue(raw) || base[sandboxVar] || EMBED_DEFAULT_TOKENS[sandboxVar] || "";
  }
  return { mode, tokens };
}

// normalizeEmbedTheme 把任意输入收敛成"只含已知 --sx-* 变量 + 合法 mode"的一份主题。
// 它守的是**出口**：宁可少发一个变量（块内用缺省），也不发一个不认识的名字或一个
// 能改变声明的值。
export function normalizeEmbedTheme(raw) {
  const source = raw && typeof raw === "object" ? raw : {};
  const mode = source.mode === "dark" ? "dark" : "light";
  const fallback = mode === "dark" ? EMBED_THEME_TOKENS_DARK : EMBED_THEME_TOKENS_LIGHT;
  const tokens = {};
  for (const name of Object.keys(EMBED_THEME_SOURCES)) {
    const value = sanitizeTokenValue(source.tokens && source.tokens[name]);
    tokens[name] = value || fallback[name] || EMBED_DEFAULT_TOKENS[name] || "";
  }
  return { mode, tokens };
}

// embedThemeMessage 是发进帧里的那个对象（结构由 html-embed.js 的接收端认）。
export function embedThemeMessage(theme) {
  const normalized = normalizeEmbedTheme(theme);
  return {
    source: EMBED_HOST_TAG,
    type: EMBED_THEME_MESSAGE,
    mode: normalized.mode,
    tokens: normalized.tokens
  };
}

// createEmbedThemeRelay 是宿主侧接线：`start()` 在 document 上挂一个**捕获阶段**的
// load 监听（iframe 的 load 不冒泡，捕获能收到），每枚帧加载完就补发一次当前主题；
// `broadcast()` 在换肤/切深浅后把新主题发给页面上所有帧。
//
// 帧的选择器默认就是 html-embed.js 渲染出来的 `.html-embed-frame`。**文件预览**用的
// 是同一个沙箱文档但没有桥脚本（`renderSandboxFrame`），它收不到这条消息，靠沙箱 CSS
// 里的 `prefers-color-scheme` 兜底——预览一份磁盘 HTML 不该有会话级通道，这是刻意的。
export function createEmbedThemeRelay(options = {}) {
  const doc = options.document || (typeof globalThis === "undefined" ? null : globalThis.document);
  const win = options.window || (typeof globalThis === "undefined" ? null : globalThis.window);
  const frameSelector = String(options.frameSelector || ".html-embed-frame");
  let started = false;

  function message() {
    return embedThemeMessage(readEmbedTheme(win));
  }

  function sendTo(frame) {
    if (!frame || typeof frame !== "object") return false;
    const target = frame.contentWindow;
    if (!target || typeof target.postMessage !== "function") return false;
    try {
      // 目标是 opaque origin（没有 allow-same-origin），targetOrigin 只能是 "*"；
      // 载荷本身没有能力，且块内接收端会校验来源是父窗口。
      target.postMessage(message(), "*");
      return true;
    } catch (error) {
      return false;
    }
  }

  function broadcast() {
    if (!doc || typeof doc.querySelectorAll !== "function") return 0;
    let sent = 0;
    for (const frame of doc.querySelectorAll(frameSelector)) if (sendTo(frame)) sent += 1;
    return sent;
  }

  function onLoad(event) {
    const target = event && event.target;
    if (!target || typeof target.matches !== "function") return;
    if (!target.matches(frameSelector)) return;
    sendTo(target);
  }

  function start() {
    if (started || !doc || typeof doc.addEventListener !== "function") return false;
    doc.addEventListener("load", onLoad, true);
    started = true;
    return true;
  }

  return {
    start,
    broadcast,
    sendTo,
    message,
    started: () => started
  };
}
