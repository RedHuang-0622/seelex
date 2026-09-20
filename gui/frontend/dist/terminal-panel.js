// 下栏终端面板（VS Code 式）：纯函数（状态归一化 / 标签生命周期 / 事件解码）
// 与 DOM 控制器分离，纯函数可离线单测（terminal-panel.test.mjs）。
//
// 事实边界：
//   - 终端的**权威状态在后端**（gui/terminal）：会话 ID、shell、cwd、尺寸、
//     退出码都来自 Bridge；前端只持有布局（是否展开/收起、面板高度、当前标签）
//     与 xterm 实例，不伪造进程状态。
//   - 终端不属于会话：切换会话、历史分页、plan/task 事件都不触碰它（终端是
//     用户的本地 shell，与 Snapshot/revision 无关）。
//   - 输出走独立事件名 seelex:terminal（不进 seelex:event 的 seq 水位），因此
//     这里也不实现 gap/resync：事件按 id 追加到对应 xterm，缺失由重开终端兜底。

import { flashResizePill, hideResizePill, showResizePill } from "./resize-pill.js";

export const TERMINAL_STATE_KEY = "seelex.terminal.v1";
export const TERMINAL_MIN_HEIGHT = 120;
export const TERMINAL_DEFAULT_HEIGHT = 280;
// 面板高度上限按视口比例给：终端再高也要留出对话区（VS Code 同为比例封顶）。
export const TERMINAL_MAX_RATIO = 0.72;
// 后端 gui/terminal 的默认值（仅用于首次 Open 的尺寸回退）。
export const TERMINAL_DEFAULT_COLS = 80;
export const TERMINAL_DEFAULT_ROWS = 24;

// ── 纯函数 ───────────────────────────────────────────────────────────────

// clampTerminalHeight 把面板高度钳到 [最小高度, 视口比例上限]。
export function clampTerminalHeight(value, viewportHeight) {
  const viewport = Number.isFinite(viewportHeight) && viewportHeight > 0 ? viewportHeight : 900;
  const ceiling = Math.max(TERMINAL_MIN_HEIGHT, Math.round(viewport * TERMINAL_MAX_RATIO));
  const height = Number.isFinite(value) ? Math.round(value) : TERMINAL_DEFAULT_HEIGHT;
  return Math.min(ceiling, Math.max(TERMINAL_MIN_HEIGHT, height));
}

// normalizeTerminalState 归一化持久化布局：字段缺失/类型不对/越界都收敛到
// 合法值（localStorage 是外部输入，不能相信）。
export function normalizeTerminalState(raw, viewportHeight) {
  const source = raw && typeof raw === "object" ? raw : {};
  return {
    open: source.open === true,
    collapsed: source.collapsed === true,
    height: clampTerminalHeight(Number(source.height), viewportHeight)
  };
}

// terminalTabItems 产出渲染用的标签模型：同名终端按出现顺序编号（第 2 个
// "powershell" → "powershell 2"，与 VS Code 一致），退出态单独标记。
export function terminalTabItems(tabs, activeId) {
  const seen = new Map();
  return (Array.isArray(tabs) ? tabs : []).map(tab => {
    const base = String(tab.title || tab.id || "terminal").trim() || "terminal";
    const count = (seen.get(base) || 0) + 1;
    seen.set(base, count);
    return {
      id: tab.id,
      title: base,
      label: count === 1 ? base : `${base} ${count}`,
      active: tab.id === activeId,
      exited: tab.running === false
    };
  });
}

// nextActiveTerminal 关闭标签后的落点：关掉当前标签时激活它右边一个；关的是
// 最右一个则退回左边一个；关掉的不是当前标签时当前选择不变。
export function nextActiveTerminal(tabs, activeId, closedId) {
  const list = Array.isArray(tabs) ? tabs : [];
  const remaining = list.filter(tab => tab.id !== closedId);
  if (!remaining.length) return "";
  if (closedId !== activeId && remaining.some(tab => tab.id === activeId)) return activeId;
  const closedIndex = list.findIndex(tab => tab.id === closedId);
  if (closedIndex < 0) return remaining[0].id;
  return (remaining[closedIndex] || remaining[remaining.length - 1]).id;
}

// decodeBase64Bytes 把后端事件的 base64 负载还原成字节（xterm.write(Uint8Array)
// 自带 UTF-8 解码，避免在 JSON 字符串里做有损的字节→字符转换）。
export function decodeBase64Bytes(data) {
  if (typeof data !== "string" || data === "") return new Uint8Array(0);
  const binary = atob(data);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index++) bytes[index] = binary.charCodeAt(index);
  return bytes;
}

// encodeBase64Bytes 是 decodeBase64Bytes 的逆（用户输入走同一条通道）。
export function encodeBase64Bytes(text) {
  const bytes = new TextEncoder().encode(String(text ?? ""));
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary);
}

// terminalEventOf 归一化一条 seelex:terminal 负载：只有带 id 的事件才可用。
export function terminalEventOf(event) {
  const id = typeof event?.id === "string" ? event.id : "";
  if (!id) return null;
  if (event.kind === "exit") {
    return {
      id,
      kind: "exit",
      bytes: new Uint8Array(0),
      exitCode: Number.isFinite(event.exit_code) ? event.exit_code : null
    };
  }
  return { id, kind: "output", bytes: decodeBase64Bytes(event.data), exitCode: undefined };
}

// ── DOM 控制器 ──────────────────────────────────────────────────────────

// createTerminalPanel 绑定一次下栏终端面板；所有后端调用经 options.invoke
// （app.js 的 Bridge 包装），事件经 bindRuntime 挂到 Wails runtime 上。
export function createTerminalPanel(options) {
  const host = options.host;
  const body = options.body;
  const tabsHost = options.tabsHost;
  const resizeHandle = options.resizeHandle;
  const collapseButton = options.collapseButton;
  const newButton = options.newButton;
  const closeButton = options.closeButton;
  const hideButton = options.hideButton;
  const toggleButton = options.toggleButton;
  const invoke = options.invoke;
  const onError = options.onError || (() => {});
  const storage = options.storage || safeStorage();
  const readViewport = options.viewport || (() => window.innerHeight || 900);
  const createTerminal = options.createTerminal || defaultTerminalFactory;
  const createFit = options.createFit || defaultFitFactory;

  let state = normalizeTerminalState(readState(), readViewport());
  // sessions: 后端终端 ID → { id, title, term, fit, view, running, exited }
  const sessions = new Map();
  // pending: 早到的输出（TerminalOpen 返回前 shell 就已写出的字节）按 id 暂存，
  // 会话登记时按序补投，不丢首屏。
  const pending = new Map();
  let activeId = "";
  let observer = null;

  function readState() {
    try {
      const raw = storage.getItem(TERMINAL_STATE_KEY);
      return raw ? JSON.parse(raw) : null;
    } catch {
      return null;
    }
  }

  function persist() {
    try {
      storage.setItem(TERMINAL_STATE_KEY, JSON.stringify(state));
    } catch {
      /* 无存储环境忽略：本次布局不记忆，功能不受影响 */
    }
  }

  function sessionList() {
    return [...sessions.values()];
  }

  function isVisible(session) {
    return Boolean(session) && state.open && !state.collapsed && session.id === activeId;
  }

  function applyLayout() {
    if (host) {
      host.classList.toggle("hidden", !state.open);
      host.classList.toggle("is-collapsed", state.collapsed);
    }
    document.documentElement.style.setProperty("--terminal-h", `${state.height}px`);
    if (collapseButton) {
      const label = state.collapsed ? "展开终端" : "收起终端";
      collapseButton.setAttribute("title", label);
      collapseButton.setAttribute("aria-label", label);
      collapseButton.setAttribute("aria-expanded", state.collapsed ? "false" : "true");
    }
    if (toggleButton) {
      toggleButton.setAttribute("aria-pressed", state.open ? "true" : "false");
      toggleButton.classList.toggle("is-on", state.open);
    }
    applyVisibility();
  }

  // applyVisibility 只让当前会话可见：xterm 在隐藏容器里量不到尺寸，隐藏的
  // 终端不 fit，切回来时再 fit（避免 cols/rows 被写成 0）。
  function applyVisibility() {
    for (const session of sessions.values()) {
      session.view.classList.toggle("is-active", isVisible(session));
    }
  }

  function renderTabs() {
    if (!tabsHost) return;
    const items = terminalTabItems(sessionList(), activeId);
    if (!items.length) {
      tabsHost.innerHTML = `<span class="terminal-empty-hint">终端已全部关闭</span>`;
      return;
    }
    tabsHost.innerHTML = items.map(item => `
      <span class="terminal-tab${item.active ? " is-active" : ""}${item.exited ? " is-exited" : ""}" role="tab" aria-selected="${item.active}" data-terminal-tab="${escapeAttribute(item.id)}" title="${escapeAttribute(item.label)}">
        <span class="terminal-tab-label">${escapeText(item.label)}</span>
        <button class="terminal-tab-close" type="button" title="关闭 ${escapeAttribute(item.label)}" aria-label="关闭 ${escapeAttribute(item.label)}" data-terminal-close="${escapeAttribute(item.id)}">✕</button>
      </span>
    `).join("");
  }

  function refit() {
    const session = sessions.get(activeId);
    if (!isVisible(session)) return;
    try {
      session.fit?.fit();
    } catch {
      /* 容器尚未布局（宽度 0）时 fit 会抛错：下一次 ResizeObserver 会重试 */
    }
  }

  function focusActive() {
    const session = sessions.get(activeId);
    if (session && !session.exited) session.term.focus();
  }

  function setActive(id, { focus = true } = {}) {
    if (id && !sessions.has(id)) return;
    activeId = id || "";
    renderTabs();
    applyVisibility();
    refit();
    if (focus) focusActive();
  }

  function newTerminal() {
    if (!state.open) state.open = true;
    state.collapsed = false;
    persist();
    applyLayout();
    let term = null;
    let view = null;
    try {
      term = createTerminal();
      const fit = createFit();
      if (fit) term.loadAddon(fit);
      view = document.createElement("div");
      view.className = "terminal-view";
      body.appendChild(view);
      term.open(view);
      fit?.fit();
      return startSession(term, fit, view);
    } catch (error) {
      if (term) cleanupView(term, view);
      onError(error);
      return Promise.resolve(null);
    }
  }

  async function startSession(term, fit, view) {
    let info = null;
    try {
      info = await invoke("TerminalOpen", {
        cols: term.cols || TERMINAL_DEFAULT_COLS,
        rows: term.rows || TERMINAL_DEFAULT_ROWS
      });
    } catch (error) {
      cleanupView(term, view);
      onError(error);
      return null;
    }
    if (!info || !info.id) {
      cleanupView(term, view);
      onError(new Error("终端启动失败：后端未返回会话 ID"));
      return null;
    }

    const session = {
      id: info.id,
      title: info.title || "terminal",
      shell: info.shell || "",
      dir: info.dir || "",
      running: info.running !== false,
      exited: false,
      exitCode: null,
      term,
      fit,
      view
    };
    sessions.set(session.id, session);

    // 输入 → 后端（onData 的字符串按 UTF-8 编码后 base64 走与输出同一条通道）。
    term.onData(data => {
      if (session.exited) return;
      invoke("TerminalWrite", session.id, encodeBase64Bytes(data)).catch(onError);
    });
    // xterm 自报尺寸 → 后端 PTY（fit 后触发，尺寸事实唯一来自前端像素）。
    term.onResize(({ cols, rows }) => {
      invoke("TerminalResize", session.id, cols, rows).catch(() => {});
    });

    setActive(session.id);
    if (pending.has(session.id)) {
      for (const chunk of pending.get(session.id)) term.write(chunk);
      pending.delete(session.id);
    }
    return session;
  }

  function cleanupView(term, view) {
    try {
      term.dispose();
    } catch {
      /* 已销毁 */
    }
    view?.remove();
  }

  async function closeTerminal(id) {
    const session = sessions.get(id);
    if (!session) return null;
    const nextID = nextActiveTerminal(sessionList(), activeId, id);
    sessions.delete(id);
    pending.delete(id);
    cleanupView(session.term, session.view);
    setActive(nextID, { focus: true });
    try {
      await invoke("TerminalClose", id);
    } catch (error) {
      // 会话可能已因 shell 退出被回收：只对活着的终端报错，其余静默收敛。
      if (!session.exited && !/unknown session/.test(String(error?.message || error))) onError(error);
    }
    return nextID;
  }

  async function closeActive() {
    if (activeId) await closeTerminal(activeId);
  }

  // handleEvent 是 seelex:terminal 的消费者：按 id 追加输出，退出则封输入并
  // 在标签上留态（不自动关标签——用户可能还要看最后几行）。
  function handleEvent(event) {
    const payload = terminalEventOf(event);
    if (!payload) return;
    const session = sessions.get(payload.id);
    if (!session) {
      if (payload.kind === "output") {
        const queue = pending.get(payload.id) || [];
        queue.push(payload.bytes);
        pending.set(payload.id, queue);
      }
      return;
    }
    if (payload.kind === "output") {
      session.term.write(payload.bytes);
      return;
    }
    session.running = false;
    session.exited = true;
    session.exitCode = payload.exitCode;
    session.term.options.disableStdin = true;
    const code = payload.exitCode === null ? "" : `（退出码 ${payload.exitCode}）`;
    session.term.write(`\r\n\x1b[2m[进程已结束${code}]\x1b[0m\r\n`);
    renderTabs();
  }

  function bindRuntime(runtime) {
    if (!runtime || typeof runtime.EventsOn !== "function") return false;
    runtime.EventsOn("seelex:terminal", handleEvent);
    return true;
  }

  function open() {
    state.open = true;
    state.collapsed = false;
    persist();
    applyLayout();
    if (!sessions.size) {
      newTerminal();
      return;
    }
    refit();
    focusActive();
  }

  function hide() {
    state.open = false;
    persist();
    applyLayout();
  }

  // toggleOpen 是快捷键/工具栏按钮的语义：已展开 → 收起（面板头仍可见，可再
  // 次点击展开）；已收起或隐藏 → 展开。刻意不同于 hide（完全隐藏）。
  function toggleOpen() {
    if (state.open && !state.collapsed) {
      state.collapsed = true;
      persist();
      applyLayout();
      return;
    }
    open();
  }

  function toggleCollapse() {
    if (!state.open) {
      open();
      return;
    }
    state.collapsed = !state.collapsed;
    persist();
    applyLayout();
    if (!state.collapsed) {
      refit();
      focusActive();
    }
  }

  // beginResize：面板顶边拖动改高度（向上拉 = 变高）。拖拽时点亮分隔条、
  // 全局切 row-resize 光标，并在指针旁贴实时高度读条；松手落盘。
  function beginResize(event) {
    if (event.button !== 0) return;
    event.preventDefault();
    resizeHandle.classList.add("is-dragging");
    document.body.classList.add("is-resizing-row");
    document.body.style.cursor = "row-resize";
    document.body.style.userSelect = "none";
    if (state.collapsed) {
      state.collapsed = false;
      applyLayout();
    }
    const bottom = host.getBoundingClientRect().bottom;
    const move = moveEvent => {
      state.height = clampTerminalHeight(bottom - moveEvent.clientY, readViewport());
      document.documentElement.style.setProperty("--terminal-h", `${state.height}px`);
      showResizePill(`${state.height} px`, moveEvent.clientX, moveEvent.clientY);
      refit();
    };
    const up = () => {
      resizeHandle.classList.remove("is-dragging");
      document.body.classList.remove("is-resizing-row");
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
      hideResizePill();
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      persist();
      refit();
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  }

  function nudgeHeight(delta) {
    state.height = clampTerminalHeight(state.height + delta, readViewport());
    persist();
    applyLayout();
    refit();
    const rect = resizeHandle?.getBoundingClientRect?.();
    if (rect && Number.isFinite(rect.left)) {
      flashResizePill(`${state.height} px`, rect.left + rect.width / 2, rect.top + 3);
    }
  }

  // refreshTheme 换肤回流：xterm 的配色只在创建那一刻从语义 token 取一次
  // （见 terminalTheme），皮肤 <link> 换了不会自动回溯——浅色皮肤下终端会
  // 留在深色底。换肤后由 theme.js 的 onApplied 调这里，把最新 token 套回
  // 全部活着的终端；返回刷新台数，便于调用方与测试观察。
  function refreshTheme() {
    const theme = terminalTheme();
    let refreshed = 0;
    for (const session of sessions.values()) {
      if (!session?.term?.options) continue;
      session.term.options.theme = theme;
      refreshed++;
    }
    return refreshed;
  }

  function setup() {
    applyLayout();
    renderTabs();
    resizeHandle?.addEventListener("pointerdown", beginResize);
    resizeHandle?.addEventListener("keydown", event => {
      if (event.key !== "ArrowUp" && event.key !== "ArrowDown") return;
      event.preventDefault();
      nudgeHeight(event.key === "ArrowUp" ? 24 : -24);
    });
    collapseButton?.addEventListener("click", toggleCollapse);
    newButton?.addEventListener("click", () => { newTerminal(); });
    closeButton?.addEventListener("click", () => { closeActive(); });
    hideButton?.addEventListener("click", hide);
    toggleButton?.addEventListener("click", toggleOpen);
    tabsHost?.addEventListener("click", event => {
      const closeTarget = event.target.closest("[data-terminal-close]");
      if (closeTarget) {
        event.stopPropagation();
        closeTerminal(closeTarget.getAttribute("data-terminal-close"));
        return;
      }
      const tab = event.target.closest("[data-terminal-tab]");
      if (tab) setActive(tab.getAttribute("data-terminal-tab"));
    });
    window.addEventListener("resize", () => {
      // 视口变矮时重钳高度（否则面板会顶掉整个对话区）。
      const clamped = clampTerminalHeight(state.height, readViewport());
      if (clamped !== state.height) {
        state.height = clamped;
        document.documentElement.style.setProperty("--terminal-h", `${state.height}px`);
      }
      refit();
    });
    if (typeof ResizeObserver === "function" && body) {
      observer = new ResizeObserver(() => refit());
      observer.observe(body);
    }
  }

  setup();

  return {
    // app.js 的动作面
    bindRuntime,
    handleEvent,
    open,
    hide,
    toggle: toggleOpen,
    toggleCollapse,
    newTerminal,
    closeTerminal,
    closeActive,
    refit,
    // 换肤回流面（theme.js 换肤后调；见 refreshTheme 注释）
    refreshTheme,
    // 只读视图（测试/调试）
    state: () => ({ ...state }),
    activeId: () => activeId,
    sessionIDs: () => [...sessions.keys()],
    sessionCount: () => sessions.size
  };
}

function safeStorage() {
  try {
    return window.localStorage;
  } catch {
    return { getItem: () => null, setItem: () => {} };
  }
}

function defaultTerminalFactory() {
  const Terminal = window.Terminal;
  if (typeof Terminal !== "function") {
    throw new Error("终端组件未加载（vendor/xterm/xterm.js）");
  }
  return new Terminal({
    cursorBlink: true,
    fontSize: 12,
    lineHeight: 1.25,
    fontFamily: cssToken("--font-mono", "ui-monospace, Consolas, monospace"),
    scrollback: 5000,
    theme: terminalTheme(),
    allowProposedApi: true
  });
}

function defaultFitFactory() {
  const Fit = window.FitAddon?.FitAddon;
  if (typeof Fit !== "function") {
    throw new Error("终端自适应插件未加载（vendor/xterm/addon-fit.js）");
  }
  return new Fit();
}

// cssToken 读取 :root 的语义 token（终端配色不写死色值：换肤即换终端配色）。
function cssToken(name, fallback) {
  try {
    const value = getComputedStyle(document.documentElement).getPropertyValue(name);
    return value && value.trim() ? value.trim() : fallback;
  } catch {
    return fallback;
  }
}

function terminalTheme() {
  const background = cssToken("--code-bg", "#10161b");
  return {
    background,
    foreground: cssToken("--text", "#e9e4d8"),
    cursor: cssToken("--accent", "#d9a657"),
    cursorAccent: background,
    selectionBackground: cssToken("--surface-3", "#36434f"),
    black: cssToken("--bg", "#141b21"),
    red: cssToken("--status-failed", "#d4695f"),
    green: cssToken("--status-done", "#6fb58e"),
    yellow: cssToken("--accent", "#d9a657"),
    blue: cssToken("--status-info", "#7ba3c7"),
    magenta: cssToken("--fork-lane-3", "#b98ac7"),
    cyan: cssToken("--fork-lane-1", "#7ba3c7"),
    white: cssToken("--text-soft", "#c6c4ba"),
    brightBlack: cssToken("--faint", "#77828a"),
    brightRed: cssToken("--status-failed", "#d4695f"),
    brightGreen: cssToken("--status-done", "#6fb58e"),
    brightYellow: cssToken("--accent-strong", "#e8bc72"),
    brightBlue: cssToken("--status-info", "#7ba3c7"),
    brightMagenta: cssToken("--fork-lane-3", "#b98ac7"),
    brightCyan: cssToken("--status-info", "#7ba3c7"),
    brightWhite: cssToken("--text-bright", "#f0ecdf")
  };
}

function escapeText(value) {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

function escapeAttribute(value) {
  return escapeText(value).replace(/"/g, "&quot;");
}
