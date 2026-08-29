// 轨迹视图组件：Network 风格轨迹面板（对话区「轨迹」子页）。
//
// 职责：
//  - 骨架渲染（过滤条 + 摘要条 + 表格区）；
//  - keyed reconciliation（复用 conversation-view 的 html 缓存 + 局部 UI
//    状态捕获/恢复，避免流式更新无意义重建大列表）；
//  - 行内交互委托：复制 IN/OUT、展开完整内容、result_ref 分页读回；
//  - 过滤按钮切换（本地状态，经 onFilterChange 回传给 app.js）。
// 本地 UI 状态（当前过滤、展开、滚动）只存在前端，不进入 Snapshot。
import {
  buildTrajectory,
  filterTrajectory,
  trajectoryStats,
  renderTrajectoryFilters,
  renderTrajectorySummary,
  renderTrajectoryTable
} from "./trajectory.js";

export function createTrajectoryView(container, options = {}) {
  const htmlByKey = new Map();
  let payloads = new Map();
  let records = [];
  let filter = "all";
  let lastRecordCount = -1;

  // 骨架：过滤条 / 摘要 / 表格区三个固定子容器（各自独立更新）。
  container.innerHTML = [
    '<div class="trajectory-filters" data-trajectory-filters></div>',
    '<div class="trajectory-summary" data-trajectory-summary></div>',
    '<div class="trajectory-list" data-trajectory-list></div>'
  ].join("");
  const filtersEl = container.querySelector("[data-trajectory-filters]");
  const summaryEl = container.querySelector("[data-trajectory-summary]");
  const listEl = container.querySelector("[data-trajectory-list]");

  container.addEventListener("click", event => handleAction(event, () => payloads, options));
  filtersEl.addEventListener("click", event => {
    const button = event.target.closest("[data-trajectory-filter]");
    if (!button) return;
    setFilter(button.dataset.trajectoryFilter);
  });

  // setFilter 更新本地过滤状态并重渲染（按钮 active 由 render 负责）。
  function setFilter(next) {
    const normalized = next && next !== "all" ? next : "all";
    if (normalized === filter) return;
    filter = normalized;
    options.onFilterChange?.(filter);
    render(records, filter, true);
  }

  function render(nextRecords, nextFilter = filter, active = true) {
    records = Array.isArray(nextRecords) ? nextRecords : [];
    filter = nextFilter || "all";
    if (!active) return;
    const stats = trajectoryStats(records);
    filtersEl.innerHTML = renderTrajectoryFilters(records, filter);
    summaryEl.innerHTML = renderTrajectorySummary(stats);
    const model = renderTrajectoryTable(filterTrajectory(records, filter));
    payloads = model.payloads;
    reconcile(listEl, model.items, htmlByKey, payloads);
    // 底部跟随：新记录到达且列表本来就贴底时滚到底（Network 面板追加行）。
    if (records.length !== lastRecordCount) {
      lastRecordCount = records.length;
      if (isNearBottom(listEl)) listEl.scrollTop = listEl.scrollHeight;
    }
  }

  return {
    render,
    setFilter,
    currentFilter: () => filter,
    current: () => records,
    payload(key) { return payloads.get(key) || ""; }
  };
}

// reconcile 与 conversation-view 同一套 keyed DOM 协调：
// key → node Map；HTML 未变化复用节点；变化时捕获 details/open 与
// 展开状态后替换；目标集合外删除；清理 html 缓存。
function reconcile(container, items, htmlByKey, payloads) {
  const existing = new Map([...container.children]
    .filter(node => node.dataset.trajectoryKey)
    .map(node => [node.dataset.trajectoryKey, node]));
  const desired = new Set();
  let cursor = container.firstElementChild;
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
    htmlByKey.delete(cursor.dataset.trajectoryKey);
    cursor.remove();
    cursor = next;
  }
  for (const key of [...htmlByKey.keys()]) if (!desired.has(key)) htmlByKey.delete(key);
}

function captureUIState(node) {
  return {
    open: node.open,
    details: [...node.querySelectorAll("details")].map(details => details.open),
    expanded: new Set([...node.querySelectorAll(".io-panel.expanded")].map(panel => panel.dataset.payload))
  };
}

function restoreUIState(node, state, payloads) {
  node.open = state.open;
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

// handleAction 处理轨迹行内的复制 / 展开 / result_ref 分页读回
// （与 conversation-view 同一交互契约）。
async function handleAction(event, getPayloads, options) {
  const copyButton = event.target.closest("[data-copy]");
  if (copyButton) {
    const value = getPayloads().get(copyButton.dataset.copy) || "";
    try {
      await options.copyText(value);
      options.notify?.("已复制");
    } catch (error) { options.notify?.(error); }
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
  }
}

function elementFromHTML(html) {
  const template = document.createElement("template");
  template.innerHTML = html.trim();
  return template.content.firstElementChild;
}

function isNearBottom(container) {
  return container.scrollHeight - container.scrollTop - container.clientHeight <= 72;
}
