// 换肤通道的离线判据（node --test，无浏览器、无第三方依赖）。
//
// 三块：
//   1) **映射表**：宿主 token → `--sx-*` 的对应关系本身是契约——沙箱变量必须都有缺省值，
//      两张表必须互不相交，语义色只许来自宿主那族 skin-independent 的 `--status-*`；
//   2) **读取与清洗**：readEmbedTheme 读不到就回落、非法值直接丢（不是清洗）；
//   3) **继电器**：帧 load 补发、换肤广播、消息形状、幂等的 start()。
//
// 安全口径：这条通道只写 `--sx-*` 自定义属性，不承载任何能力。所以这里同时钉住
// "值里不许出现能改变声明的字符"，以及沙箱接收端确实在做同样的校验。

import test from "node:test";
import assert from "node:assert/strict";

import {
  EMBED_DEFAULT_TOKENS,
  EMBED_HOST_TAG,
  EMBED_THEME_MESSAGE,
  EMBED_THEME_TOKENS_DARK,
  EMBED_THEME_TOKENS_LIGHT,
  buildEmbedBridgeScript
} from "./html-embed.js";
import {
  EMBED_SEMANTIC_VARS,
  EMBED_THEME_SOURCES,
  EMBED_THEME_VARS,
  createEmbedThemeRelay,
  embedThemeMessage,
  normalizeEmbedTheme,
  readEmbedTheme,
  sanitizeTokenValue
} from "./embed-theme.js";

// ── 1. 映射表 ────────────────────────────────────────────
test("换肤映射：沙箱变量全覆盖、两张表互不相交", () => {
  const themeNames = Object.keys(EMBED_THEME_VARS);
  const semanticNames = Object.keys(EMBED_SEMANTIC_VARS);
  // 主题色与语义色不能有交集：交集意味着某个语义色会被品牌色染掉。
  const overlap = themeNames.filter(name => semanticNames.includes(name));
  assert.deepEqual(overlap, [], `主题表与语义表重叠：${overlap.join(",")}`);
  // 沙箱里出现的每个 --sx-* 都必须有缺省值，否则推一个空值过去会打回浏览器缺省色。
  for (const name of [...themeNames, ...semanticNames]) {
    assert.match(name, /^--sx-[a-z0-9-]+$/, `${name} 必须是 --sx-* 命名`);
    assert.ok(EMBED_DEFAULT_TOKENS[name], `${name} 在沙箱缺省里没有值`);
  }
  // 反过来：沙箱缺省里的每个变量都要有人负责推（否则换肤后它会一直停留在缺省）。
  const pushed = new Set([...themeNames, ...semanticNames]);
  // 字体与圆角也走宿主 token；只检查"颜色类"必须有来源，动效/字体允许留在缺省。
  for (const name of Object.keys(EMBED_DEFAULT_TOKENS)) {
    if (!name.includes("font")) assert.ok(pushed.has(name), `${name} 没有宿主来源`);
  }
  assert.equal(Object.keys(EMBED_THEME_SOURCES).length, themeNames.length + semanticNames.length);
});

test("换肤映射：语义色只来自宿主的 status/tint/border 三族", () => {
  for (const [sandboxVar, hostVar] of Object.entries(EMBED_SEMANTIC_VARS)) {
    assert.match(
      hostVar,
      /^--(status|tint|border)-/,
      `${sandboxVar} 的来源 ${hostVar} 不是 status/tint/border 族：语义色不许跟着皮肤走`,
    );
  }
  // 品牌相关的必须有来源，否则"跟随皮肤"这件事就是空的。
  for (const name of ["--sx-accent", "--sx-surface", "--sx-text", "--sx-border"]) {
    assert.ok(name in EMBED_THEME_VARS, `${name} 必须由宿主主题表供给`);
  }
  // 明暗两套缺省的变量集合必须一致，避免某一模式下某个变量空着。
  assert.deepEqual(Object.keys(EMBED_THEME_TOKENS_DARK).sort(), Object.keys(EMBED_THEME_TOKENS_LIGHT).sort());
});

// ── 2. 读取与清洗 ────────────────────────────────────────
function fakeWindow({ theme, vars = {} } = {}) {
  return {
    document: { documentElement: { dataset: theme ? { theme } : {}, style: {} } },
    getComputedStyle: () => ({ getPropertyValue: name => vars[name] ?? "" })
  };
}

test("readEmbedTheme：按 <html data-theme> 取模式，按映射表取 token", () => {
  const dark = readEmbedTheme(fakeWindow({ theme: "dark", vars: { "--accent": "#abc123", "--surface": "#101010" } }));
  assert.equal(dark.mode, "dark");
  assert.equal(dark.tokens["--sx-accent"], "#abc123");
  assert.equal(dark.tokens["--sx-surface"], "#101010");
  // 宿主没给的变量回落到该模式的缺省，不会推空值
  assert.equal(dark.tokens["--sx-muted"], EMBED_THEME_TOKENS_DARK["--sx-muted"]);

  // 没有 data-theme：styles.css 的浅色基座就是 :root 本身，按浅色算
  const light = readEmbedTheme(fakeWindow({ vars: { "--bg": "#fafafa" } }));
  assert.equal(light.mode, "light");
  assert.equal(light.tokens["--sx-bg"], "#fafafa");
  assert.equal(light.tokens["--sx-warn"], EMBED_THEME_TOKENS_LIGHT["--sx-warn"]);

  // 非浏览器 / 空窗口：一套完整 token，不是 undefined
  const none = readEmbedTheme(null);
  assert.equal(none.mode, "light");
  assert.equal(Object.keys(none.tokens).length, Object.keys(EMBED_DEFAULT_TOKENS).length);
});

test("sanitizeTokenValue：能改变声明的值直接丢，正常色值/字体/缓动照收", () => {
  assert.equal(sanitizeTokenValue("#1f2328"), "#1f2328");
  assert.equal(sanitizeTokenValue("rgba(46,107,230,.10)"), "rgba(46,107,230,.10)");
  assert.equal(sanitizeTokenValue("cubic-bezier(.16,1,.3,1)"), "cubic-bezier(.16,1,.3,1)");
  assert.equal(sanitizeTokenValue("'Segoe UI Variable',system-ui,sans-serif"), "'Segoe UI Variable',system-ui,sans-serif");
  for (const hostile of [
    "red;}body{display:none",
    "#fff; background-image:url(https://evil/x.png)",
    "url(data:image/png;base64,AAA)",
    "expression(alert(1))",
    "@import 'x.css'",
    "a\\62 c",
    "",
    "x".repeat(300)
  ]) {
    assert.equal(sanitizeTokenValue(hostile), "", `必须丢弃：${hostile.slice(0, 24)}`);
  }
});

test("readEmbedTheme：宿主里的恶意值不会原样推给沙箱", () => {
  const theme = readEmbedTheme(fakeWindow({ theme: "dark", vars: { "--accent": "red;}body{display:none" } }));
  assert.equal(theme.tokens["--sx-accent"], EMBED_THEME_TOKENS_DARK["--sx-accent"]);
});

test("normalizeEmbedTheme：只留已知变量，缺的按模式补齐", () => {
  const normalized = normalizeEmbedTheme({ mode: "light", tokens: { "--sx-accent": "#000", "--evil": "1" } });
  assert.equal(normalized.mode, "light");
  assert.equal(normalized.tokens["--sx-accent"], "#000");
  assert.equal("--evil" in normalized.tokens, false, "未知变量必须被丢掉");
  assert.equal(normalized.tokens["--sx-surface"], EMBED_THEME_TOKENS_LIGHT["--sx-surface"]);
  // 未知 mode 归一到浅色（宿主只会是这两档）
  assert.equal(normalizeEmbedTheme({ mode: "sepia" }).mode, "light");
});

// ── 3. 继电器 ────────────────────────────────────────────
function fakeFrame(sent) {
  return {
    matches: selector => selector === ".html-embed-frame",
    contentWindow: { postMessage: (message, origin) => sent.push({ message, origin }) }
  };
}

function fakeDocument(frames) {
  const listeners = [];
  return {
    frames,
    listeners,
    querySelectorAll: selector => (selector === ".html-embed-frame" ? frames : []),
    addEventListener: (type, handler, capture) => listeners.push({ type, handler, capture })
  };
}

test("relay.broadcast：把当前主题发给页面上每一枚沙箱帧", () => {
  const sent = [];
  const frames = [fakeFrame(sent), fakeFrame(sent)];
  const doc = fakeDocument(frames);
  const win = fakeWindow({ theme: "dark", vars: { "--accent": "#123456" } });
  const relay = createEmbedThemeRelay({ document: doc, window: win });

  assert.equal(relay.broadcast(), 2);
  assert.equal(sent.length, 2);
  const { message, origin } = sent[0];
  // opaque origin 的帧只能收 "*"
  assert.equal(origin, "*");
  assert.equal(message.source, EMBED_HOST_TAG);
  assert.equal(message.type, EMBED_THEME_MESSAGE);
  assert.equal(message.mode, "dark");
  assert.equal(message.tokens["--sx-accent"], "#123456");
  assert.equal(message.tokens["--sx-text"], EMBED_THEME_TOKENS_DARK["--sx-text"]);
});

test("relay.start：帧 load 时补发一次；非帧目标忽略；重复 start 幂等", () => {
  const sent = [];
  const frame = fakeFrame(sent);
  const doc = fakeDocument([frame]);
  const relay = createEmbedThemeRelay({ document: doc, window: fakeWindow({ theme: "dark" }) });

  assert.equal(relay.start(), true);
  assert.equal(relay.start(), false, "重复 start 不改监听");
  assert.equal(doc.listeners.length, 1);
  assert.equal(doc.listeners[0].type, "load");
  // iframe 的 load 不冒泡：必须用捕获阶段才收得到
  assert.equal(doc.listeners[0].capture, true);

  const onLoad = doc.listeners[0].handler;
  onLoad({ target: { matches: () => false } });
  assert.equal(sent.length, 0, "不是沙箱帧的 load 不发消息");
  onLoad({ target: frame });
  assert.equal(sent.length, 1);
  onLoad({ target: null });
  assert.equal(sent.length, 1, "畸形事件不炸也不发");
});

test("relay.sendTo：没有 contentWindow 的帧安静跳过（不抛）", () => {
  const sent = [];
  const doc = fakeDocument([{ }]);
  const relay = createEmbedThemeRelay({ document: doc, window: fakeWindow({ theme: "dark" }) });
  assert.equal(relay.broadcast(), 0);
  assert.equal(relay.sendTo(null), false);
  assert.equal(relay.sendTo({}), false);
  // 抛错的 contentWindow 也不会把广播整条打断
  assert.equal(relay.sendTo({ contentWindow: { postMessage() { throw new Error("boom"); } } }), false);
  assert.deepEqual(sent, []);
});

test("embedThemeMessage：形状固定，token 已归一", () => {
  const message = embedThemeMessage({ mode: "light", tokens: { "--sx-info": "#2e6be6" } });
  assert.equal(message.source, EMBED_HOST_TAG);
  assert.equal(message.type, EMBED_THEME_MESSAGE);
  assert.equal(message.mode, "light");
  assert.equal(message.tokens["--sx-info"], "#2e6be6");
  assert.equal(message.tokens["--sx-accent"], EMBED_THEME_TOKENS_LIGHT["--sx-accent"]);
});

// ── 4. 接收端（沙箱里的桥脚本）─────────���──────────────────
test("桥脚本里有换肤接收端：只认父窗口、只写 --sx-*、二次校验值", () => {
  const script = buildEmbedBridgeScript();
  assert.match(script, /addEventListener\("message"/);
  assert.match(script, /event\.source !== parent/, "只认父窗口发来的消息");
  assert.match(script, /data\.source !== HOST_TAG \|\| data\.type !== THEME_MESSAGE/);
  assert.match(script, /\^--sx-\[a-z0-9-\]\{1,32\}\$/, "变量名走白名单");
  assert.match(script, /setProperty\(name, value\)/);
  assert.match(script, /url\\s\*\\\(/, "值里不许出现 url(");
  // 这条通道不承载能力：不出现 eval / Function / innerHTML / srcdoc 改写
  assert.doesNotMatch(script, /\beval\s*\(|new Function|\.innerHTML\s*=|\.srcdoc\s*=/);
});
