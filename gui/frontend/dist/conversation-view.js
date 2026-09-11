import { createConversationWheel } from "./conversation-wheel.js";

const BOTTOM_THRESHOLD = 72;

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
  const wheel = createConversationWheel(container);

  async function loadOlder() {
    if (!canLoadMore || loadingOlder || !sentinelArmed || typeof options.loadMore !== "function") return;
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
      reconcile(container, model.items, htmlByKey, payloads);
      restoreScroll(container, before, options.scrollMode || "auto", anchor);
      // 几何来自 DOM：加载更早历史/增量新消息/换行后线表自动重建。
      wheel.refresh();
      followsTail = isNearBottom(container);
    },
    payload(key) { return payloads.get(key) || ""; }
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

function reconcile(container, items, htmlByKey, payloads) {
  const existing = new Map([...container.children]
    .filter(node => node.dataset.conversationKey)
    .map(node => [node.dataset.conversationKey, node]));
  const desired = new Set();
  const sentinel = container.querySelector("[data-conversation-sentinel]");
  let cursor = sentinel ? sentinel.nextElementSibling : container.firstElementChild;
  for (const item of items) {
    desired.add(item.key);
    let node = existing.get(item.key);
    if (!node || htmlByKey.get(item.key) !== item.html) {
      const replacement = elementFromHTML(item.html);
      if (node) {
        const wasCursor = node === cursor;
        const uiState = captureUIState(node);
        node.replaceWith(replacement);
        restoreUIState(replacement, uiState, payloads);
        if (wasCursor) cursor = replacement;
      }
      node = replacement;
      htmlByKey.set(item.key, item.html);
    }
    if (node !== cursor) container.insertBefore(node, cursor);
    cursor = node.nextElementSibling;
  }
  while (cursor) {
    const next = cursor.nextElementSibling;
    htmlByKey.delete(cursor.dataset.conversationKey);
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
