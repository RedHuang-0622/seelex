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
    wheel.updateCursor(container);
    if (container.scrollTop > 240) sentinelArmed = true;
    else if (typeof IntersectionObserver !== "function") void loadOlder();
  }, { passive: true });
  container.addEventListener("click", event => handleAction(event, () => payloads, options));

  return {
    render(model, options = {}) {
      const before = scrollState(container, followsTail);
      payloads = model.payloads;
      canLoadMore = Boolean(options.hasMoreHistory);
      reconcile(container, model.items, htmlByKey, payloads);
      restoreScroll(container, before, options.scrollMode || "auto");
      wheel.update(model.items);
      wheel.updateCursor(container);
      followsTail = isNearBottom(container);
    },
    payload(key) { return payloads.get(key) || ""; }
  };
}

function createConversationWheel(container) {
  const parent = container.parentElement || container;
  const rail = document.createElement("section");
  rail.className = "conversation-wheel";
  rail.setAttribute("aria-label", "对话时间线拨轮：拖拽滚动，点击线条跳转");
  const track = document.createElement("div");
  track.className = "wheel-track";
  track.setAttribute("role", "slider");
  track.setAttribute("aria-orientation", "vertical");
  rail.appendChild(track);
  parent.appendChild(rail);

  let items = [];
  let dragging = null;
  let moved = false;

  function trackHeight() {
    const rect = track.getBoundingClientRect();
    return rect.height > 0 ? rect.height : 1;
  }

  function rowsFromItems() {
    const rows = [];
    for (const item of items) {
      const nodeKey = item.key;
      const meta = item.meta || {};
      if (meta.kind === "message") {
        const role = meta.role || "";
        if (role === "user") {
          rows.push({ nodeKey, type: "user", weight: 3 });
          continue;
        }
        if (role === "assistant") {
          if (meta.hasReasoning) rows.push({ nodeKey, type: "thinking", weight: 2 });
          if (meta.hasContent) rows.push({ nodeKey, type: "llm", weight: 2 });
          continue;
        }
        rows.push({ nodeKey, type: "other", weight: 2 });
        continue;
      }
      rows.push({ nodeKey, type: meta.kind === "axis" ? "tools" : "other", weight: meta.kind === "axis" ? 1 : 1 });
    }
    return rows;
  }

  function renderLines() {
    track.replaceChildren();
    const rows = rowsFromItems();
    if (rows.length === 0) {
      rail.classList.add("is-empty");
      return;
    }
    rail.classList.remove("is-empty");
    const totalWeight = rows.reduce((sum, row) => sum + row.weight, 0);
    const trackH = trackHeight();
    const step = trackH / totalWeight;
    let cursor = 0;
    for (const row of rows) {
      const px = Math.max(1, row.weight * step * 0.9);
      const center = (cursor + row.weight / 2) * step;
      const line = document.createElement("div");
      line.className = `wheel-line is-${row.type}`;
      line.dataset.wheelNode = row.nodeKey;
      line.style.top = `${Math.max(0, Math.min(trackH - px, center - px / 2))}px`;
      line.style.height = `${px}px`;
      line.title = row.nodeKey;
      track.appendChild(line);
      cursor += row.weight;
    }
  }

  function cursorFraction(container) {
    const max = container.scrollHeight - container.clientHeight;
    if (max <= 0) return 0;
    return Math.min(1, Math.max(0, container.scrollTop / max));
  }

  function updateCursor(container) {
    rail.style.setProperty("--wheel-progress", String(cursorFraction(container)));
  }

  function scrollToFraction(fraction, container) {
    const max = container.scrollHeight - container.clientHeight;
    if (max <= 0) return;
    container.scrollTop = max * Math.min(1, Math.max(0, fraction));
    updateCursor(container);
  }

  function scrollToKey(nodeKey) {
    if (!nodeKey) return;
    const node = container.querySelector(`[data-conversation-key="${CSS.escape(nodeKey)}"]`);
    if (!node) return;
    node.scrollIntoView({ behavior: "smooth", block: "center" });
    node.classList.add("is-wheel-target");
    window.setTimeout(() => node.classList.remove("is-wheel-target"), 1400);
    updateCursor(container);
  }

  function fractionFromEvent(clientY) {
    const rect = track.getBoundingClientRect();
    if (rect.height <= 0) return 0;
    const y = Math.max(0, Math.min(rect.height, clientY - rect.top));
    return y / rect.height;
  }

  function refresh() {
    renderLines();
    updateCursor(container);
  }

  track.addEventListener("pointerdown", event => {
    if (event.button !== 0) return;
    moved = false;
    dragging = { id: event.pointerId, startY: event.clientY };
    if (typeof track.setPointerCapture === "function") {
      try { track.setPointerCapture(event.pointerId); } catch { /* ignore */ }
    }
    scrollToFraction(fractionFromEvent(event.clientY), container);
    event.preventDefault();
  });
  track.addEventListener("pointermove", event => {
    if (!dragging || dragging.id !== event.pointerId) return;
    if (Math.abs(event.clientY - dragging.startY) > 3) moved = true;
    scrollToFraction(fractionFromEvent(event.clientY), container);
    event.preventDefault();
  });
  track.addEventListener("pointerup", event => {
    if (!dragging || dragging.id !== event.pointerId) return;
    dragging = null;
    if (!moved) {
      const line = event.target instanceof Element ? event.target.closest(".wheel-line") : null;
      if (line?.dataset?.wheelNode) scrollToKey(line.dataset.wheelNode);
    }
  });
  track.addEventListener("pointercancel", () => { dragging = null; });

  if (typeof ResizeObserver === "function") {
    const observer = new ResizeObserver(() => refresh());
    observer.observe(container);
  }

  return {
    update(nextItems) {
      items = Array.isArray(nextItems) ? nextItems : [];
      refresh();
    },
    refresh,
    updateCursor,
    scrollToKey
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

function restoreScroll(container, before, mode) {
  if (mode === "bottom" || (mode === "auto" && before.followsTail)) {
    container.scrollTop = container.scrollHeight;
    return;
  }
  if (mode === "anchor") {
    container.scrollTop = before.top + Math.max(container.scrollHeight - before.height, 0);
  }
}

function isNearBottom(container) {
  return container.scrollHeight - container.scrollTop - container.clientHeight <= BOTTOM_THRESHOLD;
}
