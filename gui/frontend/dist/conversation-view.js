import { createConversationWheel } from "./conversation-wheel.js";
import { createEmbedFrameBuffer } from "./embed-buffering.js";
import { markEntering } from "./motion.js";

const BOTTOM_THRESHOLD = 72;

// shouldAutoLoadOlder 判定"顶部 sentinel 的自动触发"是否真的应当翻更早一页。
//
// 抽成纯函数是为了让这条纪律可测（view 本身要真 DOM + 轮轴）：它是"看不到最新消息"
// 那类问题的唯一判据——**用户停在尾部时一律不自动翻页**。
export function shouldAutoLoadOlder(state) {
  if (!state?.hasLoader) return false;
  if (state.followsTail) return false;
  if (!state.canLoadMore || state.loadingOlder) return false;
  return Boolean(state.sentinelArmed);
}

export function createConversationView(container, options = {}) {
  const htmlByKey = new Map();
  let payloads = new Map();
  let followsTail = true;
  let canLoadMore = false;
  let loadingOlder = false;
  let sentinelArmed = true;
  const sentinel = document.createElement("div");
  sentinel.className = "conversation-sentinel";
  sentinel.dataset.conversationSentinel = "top";
  sentinel.setAttribute("aria-hidden", "true");
  container.prepend(sentinel);
  // 右侧「会话内全量用户输入索引」：索引本身由宿主（app.js）经
  // invoke("SessionInputIndex", sessionID) 拉回后写入（inputIndex(...)）；
  // 点击到尚未加载的早期输入时，轮轴经 locateInput 让宿主先回读那一页。
  const wheel = createConversationWheel(container, { locateInput: options.locateInput });

  // 沙箱渲染块的帧缓冲（embed-buffering.js）。这里只做接线：换行前 adopt、换行后
  // attach、行被移出时 release。**缓冲算法一概不写进 reconcile** —— 闪屏的根因是
  // "行 html 一变就整行重建、iframe 跟着重建"，判据（领养/挂载/延迟/卸载）在
  // embed-buffering.js 里是纯函数，可离线单测。
  const embedBuffer = options.embedBuffer || createEmbedFrameBuffer({
    mountRoot: options.embedMountRoot || null,
    doubleBuffer: options.doubleBuffer === true
  });

  // loadOlder 翻更早的一页。
  //
  // **只在用户真的离开尾部时**才自动翻：可见窗口是"从 history_offset 起的连续一段"
  // （后端 history_window），翻更早页会把窗口整体后退，最新的那一段随之离开 DOM。
  // 用户停在尾部时他看的就是最新内容，此时任何自动触发（IntersectionObserver 在容器
  // display:none 后重新显示、布局变化把 sentinel 带进 rootMargin、侧栏压缩改变可用
  // 高度）都会把最新消息静默移出窗口——这正是"看不到最新消息"最容易复现的一条。
  // 显式的「加载更早」按钮走宿主命令（invoke LoadMoreHistory），不受这里限制。
  async function loadOlder() {
    if (!shouldAutoLoadOlder({
      canLoadMore,
      followsTail,
      loadingOlder,
      sentinelArmed,
      hasLoader: typeof options.loadMore === "function"
    })) return;
    sentinelArmed = false;
    loadingOlder = true;
    try { await options.loadMore(); }
    finally { loadingOlder = false; }
  }

  if (typeof IntersectionObserver === "function") {
    const observer = new IntersectionObserver(entries => {
      const visible = entries.some(entry => entry.isIntersecting);
      if (!visible) sentinelArmed = true;
      else void loadOlder();
    }, { root: container, rootMargin: "160px 0px 0px" });
    observer.observe(sentinel);
  }
  container.addEventListener("scroll", () => {
    followsTail = isNearBottom(container);
    wheel.updateViewport();
    if (container.scrollTop > 240) sentinelArmed = true;
    else if (typeof IntersectionObserver !== "function") void loadOlder();
  }, { passive: true });
  container.addEventListener("click", event => handleAction(event, () => payloads, options));

  return {
    render(model, options = {}) {
      const before = scrollState(container, followsTail);
      const anchor = captureScrollAnchor(container);
      payloads = model.payloads;
      canLoadMore = Boolean(options.hasMoreHistory);
      // 流式标记：会话还在跑的时候一帧都不许新建（见 embed-buffering.js 的
      // shouldDeferMount）。chat-view.js 传的是 chat.running。
      embedBuffer.setStreaming(Boolean(options.streaming));
      reconcile(container, model.items, htmlByKey, payloads, { streaming: Boolean(options.streaming), embedBuffer });
      restoreScroll(container, before, options.scrollMode || "auto", anchor);
      // 几何来自 DOM：加载更早历史/增量新消息/换行后线表自动重建。
      wheel.refresh();
      followsTail = isNearBottom(container);
    },
    payload(key) { return payloads.get(key) || ""; },
    // inputIndex 写入全量用户输入索引（后端 SessionInputIndex 载荷；null =
    // 退回「窗口内用户行」临时模式）。返回本次刻度表。
    inputIndex(payload) { return wheel.setIndex(payload); },
    // wheelRounds 返回当前刻度表（宿主定位编排/诊断用）。
    wheelRounds() { return wheel.rounds(); }
  };
}

async function handleAction(event, getPayloads, options) {
  const copyButton = event.target.closest("[data-copy]");
  if (copyButton) {
    const value = getPayloads().get(copyButton.dataset.copy) || "";
    try {
      await options.copyText(value);
      options.notify("已复制");
    } catch (error) { options.notify(error); }
    return;
  }
  const expandButton = event.target.closest("[data-expand]");
  if (expandButton) {
    const panel = expandButton.closest(".io-panel");
    const key = expandButton.dataset.expand;
    const value = getPayloads().get(key) || "";
    const pre = panel?.querySelector("pre");
    const label = expandButton.querySelector("span");
    if (panel?.classList.contains("expanded")) {
      panel.classList.remove("expanded");
      pre?.replaceChildren(document.createTextNode(expandButton.dataset.original || ""));
      if (label) label.textContent = `+${expandButton.dataset.hiddenChars || "0"} chars`;
      expandButton.title = "展开完整内容";
      return;
    }
    expandButton.dataset.original = pre?.textContent || "";
    expandButton.dataset.hiddenChars = (label?.textContent || "").replace(/\D/g, "");
    pre?.replaceChildren(document.createTextNode(value));
    panel?.classList.add("expanded");
    if (label) label.textContent = "收回";
    expandButton.title = "收回完整内容";
    return;
  }
  // 快照截断输出的"加载完整输出"：按 result_ref 从后端分页拉全文
  // （复用 read_tool_result 通道），拉回后替换预览并展开；再点收回。
  const loadButton = event.target.closest("[data-load-ref]");
  if (loadButton) {
    const panel = loadButton.closest(".io-panel");
    const ref = loadButton.dataset.loadRef;
    const details = panel?.querySelector("details.io-collapse");
    const pre = panel?.querySelector("pre");
    if (details?.open && panel?.classList.contains("expanded")) {
      details.open = false;
      panel.classList.remove("expanded");
      const label = loadButton.querySelector("span");
      if (label) label.textContent = "加载完整输出";
      loadButton.title = "加载完整输出";
      return;
    }
    if (!panel || panel.classList.contains("loading-full")) return;
    panel.classList.add("loading-full");
    const label = loadButton.querySelector("span");
    if (label) label.textContent = "加载中…";
    loadButton.disabled = true;
    try {
      const page = await options.loadResultRef(ref, 0, options.resultPageLimit || 12000);
      const text = page?.content || "";
      if (pre) pre.replaceChildren(document.createTextNode(text));
      if (details) details.open = true;
      panel.classList.add("expanded");
      panel.dataset.fullRef = ref;
      if (label) label.textContent = page?.has_more ? "加载更多" : "收回完整输出";
      loadButton.title = "收回完整输出";
      loadButton.dataset.fullLoaded = "1";
      if (page?.has_more && page.next_offset > 0 && typeof options.loadResultRef === "function") {
        loadButton.dataset.nextOffset = String(page.next_offset);
      }
    } catch (error) {
      if (label) label.textContent = "加载失败";
      options.notify?.(error);
    } finally {
      panel.classList.remove("loading-full");
      loadButton.disabled = false;
    }
    return;
  }
}

function reconcile(container, items, htmlByKey, payloads, buffers = {}) {
  const embedBuffer = buffers.embedBuffer;
  const streaming = Boolean(buffers.streaming);
  const existing = new Map([...container.children]
    .filter(node => node.dataset.conversationKey)
    .map(node => [node.dataset.conversationKey, node]));
  const desired = new Set();
  const sentinel = container.querySelector("[data-conversation-sentinel]");
  let cursor = sentinel ? sentinel.nextElementSibling : container.firstElementChild;
  for (const item of items) {
    desired.add(item.key);
    let node = existing.get(item.key);
    let fresh = false;
    if (!node || htmlByKey.get(item.key) !== item.html) {
      const replacement = elementFromHTML(item.html);
      if (node) {
        const wasCursor = node === cursor;
        const uiState = captureUIState(node);
        // 换行**之前**：让缓冲区把旧行里内容没变的沙箱帧领养进新行（srcdoc 一致就
        // 原样搬过去，不重新加载），内容变了的帧继续占着可视位等新帧 load 完。
        embedBuffer?.adopt(node, replacement, { streaming });
        node.replaceWith(replacement);
        restoreUIState(replacement, uiState, payloads);
        if (wasCursor) cursor = replacement;
      } else {
        // 新出现的条目（新消息 / 一次新的工具过程）入场一次：keyed reconcile
        // 只在**首次插入**时挂类，后续流式替换不重播（否则工具面板会一直闪）。
        markEntering(replacement, "is-entering-row");
        embedBuffer?.adopt(null, replacement, { streaming });
      }
      node = replacement;
      fresh = true;
      htmlByKey.set(item.key, item.html);
    }
    if (node !== cursor) container.insertBefore(node, cursor);
    // 换行**之后**（行进文档）才发起离屏挂载：流式期间只留同高占位。
    if (fresh) embedBuffer?.attach(node, { streaming });
    cursor = node.nextElementSibling;
  }
  while (cursor) {
    const next = cursor.nextElementSibling;
    htmlByKey.delete(cursor.dataset.conversationKey);
    // 行被移出对话流：撤在途挂载、摘监听、清 pending 帧（不留在离屏容器里）。
    embedBuffer?.release(cursor);
    cursor.remove();
    cursor = next;
  }
  for (const key of [...htmlByKey.keys()]) if (!desired.has(key)) htmlByKey.delete(key);
}

function captureUIState(node) {
  return {
    details: [...node.querySelectorAll("details")].map(details => details.open),
    expanded: new Set([...node.querySelectorAll(".io-panel.expanded")].map(panel => panel.dataset.payload))
  };
}

function restoreUIState(node, state, payloads) {
  [...node.querySelectorAll("details")].forEach((details, index) => {
    if (state.details[index] !== undefined) details.open = state.details[index];
  });
  for (const panel of node.querySelectorAll(".io-panel")) {
    if (!state.expanded.has(panel.dataset.payload)) continue;
    const pre = panel.querySelector("pre");
    const toggle = panel.querySelector("[data-expand]");
    const label = toggle?.querySelector("span");
    if (toggle) toggle.dataset.hiddenChars = (label?.textContent || "").replace(/\D/g, "");
    if (toggle) toggle.dataset.original = pre?.textContent || "";
    pre?.replaceChildren(document.createTextNode(payloads.get(panel.dataset.payload) || ""));
    panel.classList.add("expanded");
    if (toggle) {
      if (label) label.textContent = "收回";
      toggle.title = "收回完整内容";
    }
  }
}

function elementFromHTML(html) {
  const template = document.createElement("template");
  template.innerHTML = html.trim();
  return template.content.firstElementChild;
}

function scrollState(container, followsTail) {
  return {
    top: container.scrollTop,
    height: container.scrollHeight,
    followsTail: followsTail || isNearBottom(container)
  };
}

// captureScrollAnchor 记住视口顶部第一条可见消息（key + 相对视口位置）。
// 加载更早历史时预置的新页会让内容整体下移，靠这条锚点把用户正在读的那一条
// 按回原处——比「按 scrollHeight 增量」稳：窗口整体后退（尾部被截）时高度几乎
// 不变，增量算法会失效并把用户甩到别的位置。
export function captureScrollAnchor(container) {
  const containerTop = container.getBoundingClientRect().top;
  for (const node of container.children) {
    if (!node.dataset || !node.dataset.conversationKey) continue;
    const rect = node.getBoundingClientRect();
    if (rect.bottom <= containerTop + 1) continue;
    return { key: node.dataset.conversationKey, top: rect.top - containerTop };
  }
  return null;
}

// restoreScrollAnchor 把锚点消息放回原来的视口位置；锚点已不在窗口内（被
// 截掉的尾页/会话切换）时返回 false，调用方退回粗略的 scrollHeight 增量。
export function restoreScrollAnchor(container, anchor) {
  if (!anchor || !anchor.key) return false;
  const node = container.querySelector(`[data-conversation-key="${CSS.escape(anchor.key)}"]`);
  if (!node) return false;
  const containerTop = container.getBoundingClientRect().top;
  const delta = node.getBoundingClientRect().top - containerTop - anchor.top;
  if (!delta) return true;
  setScrollInstantly(container, container.scrollTop + delta);
  return true;
}

// setScrollInstantly 立即设置滚动位置：.conversation 声明了
// scroll-behavior: smooth，直接赋值会变成动画（锚点恢复、轮轴拖拽都会「飘」）。
function setScrollInstantly(container, top) {
  const previous = container.style.scrollBehavior;
  container.style.scrollBehavior = "auto";
  container.scrollTop = top;
  container.style.scrollBehavior = previous;
}

function restoreScroll(container, before, mode, anchor) {
  if (mode === "anchor") {
    if (restoreScrollAnchor(container, anchor)) return;
    setScrollInstantly(container, before.top + Math.max(container.scrollHeight - before.height, 0));
    return;
  }
  if (mode === "bottom" || (mode === "auto" && before.followsTail)) {
    setScrollInstantly(container, container.scrollHeight);
  }
}

function isNearBottom(container) {
  return container.scrollHeight - container.scrollTop - container.clientHeight <= BOTTOM_THRESHOLD;
}
