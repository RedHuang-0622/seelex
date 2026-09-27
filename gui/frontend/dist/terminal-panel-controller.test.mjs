// 下栏终端面板的控制器级契约测试：用最小假 DOM + 假 xterm + 假 Bridge 驱动
// createTerminalPanel 的真实分支（不开窗、不起真进程），断言的是模块对外的
// 可观察行为：调了哪个 Bridge 方法、写了哪些字节、标签怎么变、布局落没落盘。
//
// 说明：标签条是 innerHTML 渲染（与仓库其它视图一致），因此标签断言落在 HTML
// 字符串上；切标签/关标签这类交互经控制器 API（closeTerminal）与事件路由覆盖。

import assert from "node:assert/strict";
import test from "node:test";

import {
  TERMINAL_SCROLLBACK_DEFAULT,
  TERMINAL_SCROLLBACK_KEY,
  TERMINAL_STATE_KEY,
  createTerminalPanel,
  encodeBase64Bytes
} from "./terminal-panel.js";

// ── 最小假 DOM ──────────────────────────────────────────────────────────

class FakeClassList {
  constructor() {
    this.names = new Set();
  }
  add(...names) {
    for (const name of names) this.names.add(name);
  }
  remove(...names) {
    for (const name of names) this.names.delete(name);
  }
  toggle(name, force) {
    const on = force === undefined ? !this.names.has(name) : Boolean(force);
    if (on) this.names.add(name);
    else this.names.delete(name);
    return on;
  }
  contains(name) {
    return this.names.has(name);
  }
  get value() {
    return [...this.names].join(" ");
  }
}

class FakeElement {
  constructor(tag = "div") {
    this.tagName = tag.toUpperCase();
    this.classList = new FakeClassList();
    this.attributes = new Map();
    this.dataset = {};
    this.style = { values: new Map(), setProperty: (name, value) => this.style.values.set(name, value) };
    this.children = [];
    this.parent = null;
    this.listeners = new Map();
    this.hidden = false;
    this.innerHTML = "";
    this.textContent = "";
    this.focused = false;
    this.rect = { bottom: 900, top: 0, left: 0, width: 800, height: 300 };
  }
  get className() {
    return this.classList.value;
  }
  set className(value) {
    this.classList = new FakeClassList();
    for (const name of String(value).split(/\s+/).filter(Boolean)) this.classList.add(name);
  }
  appendChild(child) {
    child.parent = this;
    this.children.push(child);
    return child;
  }
  remove() {
    if (!this.parent) return;
    this.parent.children = this.parent.children.filter(item => item !== this);
    this.parent = null;
  }
  setAttribute(name, value) {
    this.attributes.set(name, String(value));
  }
  getAttribute(name) {
    return this.attributes.has(name) ? this.attributes.get(name) : null;
  }
  addEventListener(type, handler) {
    if (!this.listeners.has(type)) this.listeners.set(type, []);
    this.listeners.get(type).push(handler);
  }
  dispatch(type, event = {}) {
    for (const handler of this.listeners.get(type) || []) handler({ target: this, ...event });
  }
  getBoundingClientRect() {
    return this.rect;
  }
  focus() {
    this.focused = true;
  }
}

function installFakeGlobals(viewportHeight = 900, tokens = {}) {
  const document = {
    documentElement: new FakeElement("html"),
    body: new FakeElement("body"),
    createElement: tag => new FakeElement(tag)
  };
  const window = {
    innerHeight: viewportHeight,
    listeners: new Map(),
    addEventListener(type, handler) {
      if (!this.listeners.has(type)) this.listeners.set(type, []);
      this.listeners.get(type).push(handler);
    },
    removeEventListener(type, handler) {
      if (!this.listeners.has(type)) return;
      this.listeners.set(type, this.listeners.get(type).filter(item => item !== handler));
    },
    dispatch(type, event = {}) {
      for (const handler of this.listeners.get(type) || []) handler({ preventDefault() {}, ...event });
    }
  };
  globalThis.document = document;
  globalThis.window = window;
  // tokens 是"当前皮肤"的语义 token 表：测试改它就是模拟换肤后 token 变化。
  globalThis.getComputedStyle = () => ({ getPropertyValue: name => tokens[name] ?? "" });
  return { document, window, tokens };
}

// ── 假 xterm / 假 fit / 假 Bridge ────────────────────────────────────────

function createFakeTerminal() {
  const terminal = {
    cols: 80,
    rows: 24,
    options: { disableStdin: false },
    writes: [],
    dataHandlers: [],
    resizeHandlers: [],
    addons: [],
    disposed: false,
    focused: false,
    open(host) {
      this.host = host;
    },
    loadAddon(addon) {
      this.addons.push(addon);
    },
    onData(handler) {
      this.dataHandlers.push(handler);
    },
    onResize(handler) {
      this.resizeHandlers.push(handler);
    },
    write(chunk) {
      this.writes.push(chunk);
    },
    focus() {
      this.focused = true;
    },
    dispose() {
      this.disposed = true;
    },
    emitData(data) {
      for (const handler of this.dataHandlers) handler(data);
    },
    emitResize(cols, rows) {
      this.cols = cols;
      this.rows = rows;
      for (const handler of this.resizeHandlers) handler({ cols, rows });
    }
  };
  return terminal;
}

function createFakeFit(terminal) {
  return {
    fits: 0,
    // 假 fit：把终端尺寸设成固定值，让 Open 的 cols/rows 有确定断言点。
    fit() {
      this.fits++;
      terminal.emitResize(100, 30);
    }
  };
}

function createHarness({ viewportHeight = 900, deferredOpen = false, tokens = {}, scrollbackStore = null } = {}) {
  installFakeGlobals(viewportHeight, tokens);
  const host = new FakeElement("section");
  host.rect = { bottom: 900, top: 600, left: 0, width: 800, height: 300 };
  const body = new FakeElement("div");
  const tabsHost = new FakeElement("div");
  const resizeHandle = new FakeElement("div");
  const collapseButton = new FakeElement("button");
  const newButton = new FakeElement("button");
  const closeButton = new FakeElement("button");
  const hideButton = new FakeElement("button");
  const toggleButton = new FakeElement("button");

  const calls = [];
  const pendingOpen = [];
  let openCount = 0;
  const invoke = (method, ...args) => {
    calls.push({ method, args });
    if (method === "TerminalOpen") {
      openCount++;
      const info = { id: `term-${openCount}`, title: "powershell", shell: "pwsh", dir: "/w", running: true };
      if (deferredOpen) return new Promise(resolve => pendingOpen.push(() => resolve(info)));
      return Promise.resolve(info);
    }
    return Promise.resolve(true);
  };

  const store = new Map();
  // 预置落盘设置（测"构造时读回设置"这条路；缺省就是没写过 → 默认值）。
  if (scrollbackStore !== null) store.set(TERMINAL_SCROLLBACK_KEY, String(scrollbackStore));
  const storage = {
    getItem: key => (store.has(key) ? store.get(key) : null),
    setItem: (key, value) => store.set(key, String(value))
  };

  const terminals = [];
  const panel = createTerminalPanel({
    host,
    body,
    tabsHost,
    resizeHandle,
    collapseButton,
    newButton,
    closeButton,
    hideButton,
    toggleButton,
    invoke,
    onError: error => calls.push({ method: "onError", args: [String(error?.message || error)] }),
    storage,
    viewport: () => globalThis.window.innerHeight,
    createTerminal: () => {
      const terminal = createFakeTerminal();
      // 与 defaultTerminalFactory 同口径：配色只在创建那一刻从 token 取一次
      // （xterm 不会因为 CSS 变量变了就自己回溯），所以这里也快照一次。
      terminal.options.theme = {
        background: tokens["--code-bg"] ?? "#10161b",
        foreground: tokens["--text"] ?? "#e9e4d8",
        cursor: tokens["--accent"] ?? "#d9a657"
      };
      terminals.push(terminal);
      return terminal;
    },
    // fit 在 createTerminal 之后调用：注入的假 fit 绑定最近一台终端。
    createFit: () => createFakeFit(terminals.at(-1))
  });

  return {
    panel,
    host,
    body,
    tabsHost,
    resizeHandle,
    collapseButton,
    newButton,
    closeButton,
    hideButton,
    toggleButton,
    calls,
    storage,
    store,
    tokens,
    terminals,
    pendingOpen,
    invokeCalls: method => calls.filter(call => call.method === method),
    settleOpen: async () => {
      while (pendingOpen.length) pendingOpen.shift()();
      await Promise.resolve();
      await Promise.resolve();
    }
  };
}

// ── 断言 ────────────────────────────────────────────────────────────────

test("panel opens, creates a terminal and reports it to the Bridge", async () => {
  const harness = createHarness();
  assert.equal(harness.host.classList.contains("hidden"), true);

  await harness.panel.newTerminal();

  const openCalls = harness.invokeCalls("TerminalOpen");
  assert.equal(openCalls.length, 1);
  assert.equal(harness.panel.sessionCount(), 1);
  assert.equal(harness.panel.activeId(), "term-1");
  assert.equal(harness.host.classList.contains("hidden"), false);
  assert.equal(harness.toggleButton.getAttribute("aria-pressed"), "true");
  // 标签条按后端返回的 title 渲染
  assert.match(harness.tabsHost.innerHTML, /data-terminal-tab="term-1"/);
  assert.match(harness.tabsHost.innerHTML, /powershell/);
  // 终端宿主挂进 body 且被标记为当前
  assert.equal(harness.body.children.length, 1);
  assert.equal(harness.body.children[0].classList.contains("is-active"), true);
  // 高度落盘
  assert.ok(harness.store.get(TERMINAL_STATE_KEY).includes("height"));
});

test("terminal input is encoded to base64 and output is written back as bytes", async () => {
  const harness = createHarness();
  await harness.panel.newTerminal();
  const terminal = harness.terminals[0];

  terminal.emitData("ls\r");
  const writes = harness.invokeCalls("TerminalWrite");
  assert.equal(writes.length, 1);
  assert.deepEqual(writes[0].args, ["term-1", encodeBase64Bytes("ls\r")]);

  harness.panel.handleEvent({ id: "term-1", kind: "output", data: encodeBase64Bytes("hello") });
  assert.equal(new TextDecoder().decode(terminal.writes.at(-1)), "hello");
});

test("output that arrives before TerminalOpen resolves is replayed, not dropped", async () => {
  const harness = createHarness({ deferredOpen: true });
  const pending = harness.panel.newTerminal();
  // Open 还没返回，shell 的首屏输出已经到了：按 id 暂存
  harness.panel.handleEvent({ id: "term-1", kind: "output", data: encodeBase64Bytes("banner") });
  await harness.settleOpen();
  await pending;
  assert.equal(new TextDecoder().decode(harness.terminals[0].writes[0]), "banner");
});

test("exit event seals the terminal and marks the tab", async () => {
  const harness = createHarness();
  await harness.panel.newTerminal();
  const terminal = harness.terminals[0];

  harness.panel.handleEvent({ id: "term-1", kind: "exit", exit_code: 0 });
  assert.equal(terminal.options.disableStdin, true);
  assert.match(harness.tabsHost.innerHTML, /is-exited/);
  // 退出后再输入不再外发
  const before = harness.invokeCalls("TerminalWrite").length;
  terminal.emitData("x");
  assert.equal(harness.invokeCalls("TerminalWrite").length, before);
});

test("multi-open keeps creation order and closing picks the neighbour tab", async () => {
  const harness = createHarness();
  await harness.panel.newTerminal();
  await harness.panel.newTerminal();
  assert.deepEqual(harness.panel.sessionIDs(), ["term-1", "term-2"]);
  assert.equal(harness.panel.activeId(), "term-2");
  // 同名 shell 的第二枚标签带编号
  assert.match(harness.tabsHost.innerHTML, /powershell 2/);

  await harness.panel.closeTerminal("term-2");
  assert.deepEqual(harness.panel.sessionIDs(), ["term-1"]);
  assert.equal(harness.panel.activeId(), "term-1");
  assert.equal(harness.invokeCalls("TerminalClose")[0].args[0], "term-2");
  assert.equal(harness.terminals[1].disposed, true);
  assert.equal(harness.body.children.length, 1);

  // 关掉最后一个 → 空态提示，面板仍开着（不自动隐藏）
  await harness.panel.closeTerminal("term-1");
  assert.equal(harness.panel.sessionIDs().length, 0);
  assert.equal(harness.panel.activeId(), "");
  assert.match(harness.tabsHost.innerHTML, /终端已全部关闭/);
  assert.equal(harness.host.classList.contains("hidden"), false);
});

test("collapse / expand / hide drive layout state and persistence", async () => {
  const harness = createHarness();
  await harness.panel.newTerminal();

  harness.panel.toggleCollapse();
  assert.equal(harness.panel.state().collapsed, true);
  assert.equal(harness.host.classList.contains("is-collapsed"), true);
  assert.equal(harness.collapseButton.getAttribute("aria-expanded"), "false");

  harness.panel.toggleCollapse();
  assert.equal(harness.panel.state().collapsed, false);
  assert.equal(harness.collapseButton.getAttribute("aria-expanded"), "true");

  // Ctrl+` 语义：展开 → 收起（仍可见）；再按 → 展开
  harness.panel.toggle();
  assert.equal(harness.panel.state().collapsed, true);
  harness.panel.toggle();
  assert.equal(harness.panel.state().collapsed, false);

  harness.panel.hide();
  assert.equal(harness.panel.state().open, false);
  assert.equal(harness.host.classList.contains("hidden"), true);
  assert.equal(harness.toggleButton.getAttribute("aria-pressed"), "false");
});

test("dragging the top edge resizes the panel inside the clamp", async () => {
  const harness = createHarness({ viewportHeight: 900 });
  await harness.panel.newTerminal();
  harness.resizeHandle.dispatch("pointerdown", { button: 0, clientY: 600, preventDefault() {} });

  // 向上拖：bottom(900) - 300 = 600 → 合法
  globalThis.window.dispatch("pointermove", { clientY: 300 });
  assert.equal(harness.panel.state().height, 600);
  // 拖到视口顶部之外：被 72% 上限钳住
  globalThis.window.dispatch("pointermove", { clientY: -200 });
  assert.equal(harness.panel.state().height, 648);
  globalThis.window.dispatch("pointerup", {});
  assert.ok(harness.store.get(TERMINAL_STATE_KEY).includes("648"));
});

test("resize handle keyboard nudges persist and clamp", async () => {
  const harness = createHarness();
  await harness.panel.newTerminal();
  const start = harness.panel.state().height;
  harness.resizeHandle.dispatch("keydown", { key: "ArrowUp", preventDefault() {} });
  assert.equal(harness.panel.state().height, start + 24);
  harness.resizeHandle.dispatch("keydown", { key: "ArrowDown", preventDefault() {} });
  assert.equal(harness.panel.state().height, start);
  harness.resizeHandle.dispatch("keydown", { key: "Enter", preventDefault() {} });
  assert.equal(harness.panel.state().height, start);
});

test("restored layout from storage is honoured and clamped", async () => {
  installFakeGlobals(900);
  const store = new Map([[TERMINAL_STATE_KEY, JSON.stringify({ open: true, collapsed: false, height: 99_999 })]]);
  const panel = createTerminalPanel({
    host: new FakeElement("section"),
    body: new FakeElement("div"),
    tabsHost: new FakeElement("div"),
    resizeHandle: new FakeElement("div"),
    collapseButton: new FakeElement("button"),
    invoke: () => Promise.resolve({ id: "t", title: "cmd", running: true }),
    storage: { getItem: key => (store.has(key) ? store.get(key) : null), setItem: (key, value) => store.set(key, value) },
    viewport: () => 900
  });
  assert.equal(panel.state().open, true);
  assert.equal(panel.state().height, 648);
});

test("换肤后 refreshTheme 把最新 token 套回已开终端（否则终端留在旧皮肤底色）", async () => {
  // 当前皮肤：graphite（深色）——终端创建时快照的配色也是深色
  const harness = createHarness({
    tokens: { "--code-bg": "#10161b", "--text": "#e9e4d8", "--accent": "#d9a657" }
  });
  await harness.panel.newTerminal();
  await harness.panel.newTerminal();
  assert.equal(harness.panel.sessionCount(), 2);
  assert.equal(harness.terminals[0].options.theme.background, "#10161b");

  // 换成银白冷钢：皮肤 <link> 换了、token 变了，但 xterm 配色是创建那刻取的
  harness.tokens["--code-bg"] = "#e8eaed";
  harness.tokens["--text"] = "#2b3138";
  harness.tokens["--accent"] = "#5c6673";
  assert.equal(
    harness.terminals[0].options.theme.background,
    "#10161b",
    "换肤不会改 xterm 自带配色，必须由 refreshTheme 显式回流"
  );

  // 回流：已开的两台终端都拿到新皮肤配色
  assert.equal(harness.panel.refreshTheme(), 2);
  for (const terminal of harness.terminals) {
    assert.equal(terminal.options.theme.background, "#e8eaed");
    assert.equal(terminal.options.theme.foreground, "#2b3138");
    assert.equal(terminal.options.theme.cursor, "#5c6673");
  }

  // 已关闭的终端不再参与回流
  await harness.panel.closeTerminal("term-2");
  assert.equal(harness.panel.refreshTheme(), 1);
});

test("bindRuntime subscribes to the dedicated terminal event", () => {
  const harness = createHarness();
  const subscribed = [];
  const bound = harness.panel.bindRuntime({ EventsOn: (name, handler) => subscribed.push({ name, handler }) });
  assert.equal(bound, true);
  assert.equal(subscribed.length, 1);
  assert.equal(subscribed[0].name, "seelex:terminal");
  assert.equal(harness.panel.bindRuntime(null), false);
});

test("a failing TerminalOpen reports the error and does not leak a view", async () => {
  installFakeGlobals();
  const body = new FakeElement("div");
  const errors = [];
  const panel = createTerminalPanel({
    host: new FakeElement("section"),
    body,
    tabsHost: new FakeElement("div"),
    resizeHandle: new FakeElement("div"),
    collapseButton: new FakeElement("button"),
    invoke: () => Promise.reject(new Error("terminal: no usable shell found on this host")),
    onError: error => errors.push(String(error.message)),
    storage: { getItem: () => null, setItem: () => {} },
    createTerminal: () => createFakeTerminal(),
    createFit: () => ({ fit() {} }),
    viewport: () => 900
  });
  await panel.newTerminal();
  assert.equal(panel.sessionCount(), 0);
  assert.equal(body.children.length, 0);
  assert.match(errors[0], /no usable shell/);
});

// ── 回滚行数：设置项（默认 2000）────────────────────────────────────────

test("回滚行数：默认 2000，开标签即生效，改动立刻作用到已打开的标签", async () => {
  const harness = createHarness();
  assert.equal(harness.panel.scrollback(), TERMINAL_SCROLLBACK_DEFAULT);
  await harness.panel.newTerminal();
  assert.equal(harness.terminals[0].options.scrollback, TERMINAL_SCROLLBACK_DEFAULT, "开终端即按设置给值");

  assert.equal(harness.panel.setScrollback(5000), 5000);
  assert.equal(harness.terminals[0].options.scrollback, 5000, "已打开的标签就地生效（不重开标签）");
  assert.equal(harness.store.get(TERMINAL_SCROLLBACK_KEY), "5000", "设置落盘");

  await harness.panel.newTerminal();
  assert.equal(harness.terminals[1].options.scrollback, 5000, "后开的标签用新值");

  // 非法值（下拉框被改成怪东西 / 手改 localStorage）回落默认，而不是把垃圾塞给 xterm
  assert.equal(harness.panel.setScrollback("不是数字"), TERMINAL_SCROLLBACK_DEFAULT);
  assert.equal(harness.terminals[0].options.scrollback, TERMINAL_SCROLLBACK_DEFAULT);
  assert.equal(harness.store.get(TERMINAL_SCROLLBACK_KEY), String(TERMINAL_SCROLLBACK_DEFAULT));
});

test("回滚行数：落盘值在下次构造时生效（不是每次都用默认）", async () => {
  const harness = createHarness({ scrollbackStore: 5000 });
  assert.equal(harness.panel.scrollback(), 5000);
  await harness.panel.newTerminal();
  assert.equal(harness.terminals[0].options.scrollback, 5000);
});
