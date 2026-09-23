// 轨迹视图组件：Network 风格轨迹面板（对话区「轨迹」子页）。
//
// 职责：
//  - 骨架渲染（窗口边界条 + 上下文轴 + 轴详情 + 过滤条 + 摘要条 + 表格区）；
//  - keyed reconciliation（复用 conversation-view 的 html 缓存 + 局部 UI
//    状态捕获/恢复，避免流式更新无意义重建大列表）；
//  - 行内交互委托：复制 IN/OUT、展开完整内容、result_ref 分页读回；
//  - 过滤按钮切换（本地状态，经 onFilterChange 回传给 app.js）；
//  - 上下文轴滚轮分页：滚轮 = 翻页（上一页/下一页），Shift+滚轮 = 调整页大小
//    档位（轴头给页码/页大小反馈）；只在轴区域内响应，不抢对话区滚轮。
// 本地 UI 状态（当前过滤、展开、滚动、轴分页）只存在前端，不进入 Snapshot。
import {
  AXIS_PAGE_SIZE_DEFAULT,
  buildTrajectory,
  filterTrajectory,
  trajectoryStats,
  renderTrajectoryFilters,
  renderTrajectorySummary,
  renderTrajectoryTable,
  renderContextAxis,
  renderTrajectoryWindowInfo,
  renderAxisDetail,
  prefixLayerSegments,
  compactionMarks,
  resolveAxisPage,
  stepAxisPageSize,
  axisWheelStep
} from "./trajectory.js";

export function createTrajectoryView(container, options = {}) {
  const htmlByKey = new Map();
  let payloads = new Map();
  let records = [];
  let filter = "all";
  let lastRecordCount = -1;
  // 元数据轨状态：前缀层（Bridge.PromptLayers）与压缩记录
  // （Snapshot.Task.ContextCompactions）在 render 时归一化，供轴渲染与详情。
  let prefixLayers = [];
  let compactions = [];
  let axisPrefixSegments = [];
  let axisCompactionMarks = [];
  let axisDetailKey = "";
  // axisFrame 是已打开的压缩详情里「折叠帧正文」的本地状态
  // （{ loading, error, text, hasMore, nextOffset, totalBytes } 或 null）：
  // 按 ref 从会话内容存储分页读取，不进 Snapshot，切换详情即清空。
  let axisFrame = null;
  // 轴分页状态（本地 UI 状态，不进 Snapshot）：
  //  - axisPageSize：分页页大小档位（Shift+滚轮步进，默认 AXIS_PAGE_SIZE_DEFAULT）；
  //  - axisPage / axisAnchorKey / axisTail：当前页；anchorKey 指向本页首条记录，
  //    加载更早内容把记录整体前移时视图跟着原记录走（不跳位）；停在尾页时
  //    tail=true 跟随新记录；
  //  - axisNotice：瞬时反馈文案（页大小变更/更早内容未加载…），随轴头渲染保留。
  let axisPageSize = AXIS_PAGE_SIZE_DEFAULT;
  let axisPage = 0;
  let axisAnchorKey = "";
  let axisTail = true;
  let axisNotice = "";
  let axisNoticeTimer = null;
  let wheelAccumulator = 0;
  let axisHasMore = false;
  // axisPageable：轴有可翻的页（多页）或还有未加载的更早内容——决定滚轮是否
  // 由轴接管（单页且已全部加载时让滚轮正常滚动页面，不做无意义拦截）。
  let axisPageable = false;
  let axisLoadPending = false;
  let axisLoadCooldownUntil = 0;
  let windowInfo = null;

  // 骨架：上下文轴（记录轨 + 前缀注入/压缩元数据轨）/ 轴详情 / 过滤条 /
  // 摘要 / 表格区（各自独立更新；轴详情只在点击元数据块时展开）。
container.innerHTML = [
  '<div class="trajectory-window" data-trajectory-window></div>',
  '<div class="trajectory-axis" data-trajectory-axis></div>',
    '<div class="trajectory-axis-detail" data-axis-detail hidden></div>',
    '<div class="trajectory-filters" data-trajectory-filters></div>',
    '<div class="trajectory-summary" data-trajectory-summary></div>',
    '<div class="trajectory-list" data-trajectory-list></div>'
  ].join("");
  const axisEl = container.querySelector("[data-trajectory-axis]");
  const windowEl = container.querySelector("[data-trajectory-window]");
  const detailEl = container.querySelector("[data-axis-detail]");
  const filtersEl = container.querySelector("[data-trajectory-filters]");
  const summaryEl = container.querySelector("[data-trajectory-summary]");
  const listEl = container.querySelector("[data-trajectory-list]");

  container.addEventListener("click", event => handleAction(event, () => payloads, options));
  // 窗口边界条：轨迹只覆盖"已加载的可见窗口"，更早的回合由这里按需加载。
  windowEl.addEventListener("click", event => {
    if (!event.target.closest("[data-trajectory-load-earlier]")) return;
    if (typeof options.loadMore !== "function") return;
    event.preventDefault();
    Promise.resolve(options.loadMore()).catch(() => {});
  });
  filtersEl.addEventListener("click", event => {
    const button = event.target.closest("[data-trajectory-filter]");
    if (!button) return;
    setFilter(button.dataset.trajectoryFilter);
  });
  detailEl.addEventListener("click", event => {
    if (event.target.closest("[data-axis-detail-close]")) {
      closeAxisDetail();
      return;
    }
    // 压缩详情里的「折叠帧正文」入口：首次加载 / 续读下一页 / 收起正文。
    const frameButton = event.target.closest("[data-compact-frame-load]");
    if (!frameButton) return;
    const mark = axisCompactionMarks[Number(String(axisDetailKey).split(":")[1])];
    if (mark) loadCompactionFrame(mark, frameButton.dataset.compactFrameLoad);
  });
  // 上下文轴点击：元数据块（前缀层/压缩刻度）开/关轴详情；普通轨迹块先切
  // 回全量过滤保证行存在，再滚动定位并短暂高亮。
  axisEl.addEventListener("click", event => {
    const segment = event.target.closest(".axis-segment");
    if (!segment) return;
    const prefixIndex = segment.dataset.prefixLayer;
    if (prefixIndex !== undefined) {
      const layer = axisPrefixSegments[Number(prefixIndex)];
      if (layer) {
        toggleAxisDetail(`prefix:${prefixIndex}`, { type: "prefix", layer });
        return;
      }
    }
    const compactIndex = segment.dataset.compactIdx;
    if (compactIndex !== undefined) {
      // 刻度在本页是钳位标记时先跳页（显式提示，不静默位移），再打开详情——
      // 否则用户看到的是"点了一个边界刻度却什么都没发生/看到别处的压缩"。
      const mark = axisCompactionMarks[Number(compactIndex)];
      if (mark) {
        if (mark.anchored && mark.offPage && mark.anchorPage !== axisPage) {
          applyAxisPage(records, resolveAxisPage(records, { pageSize: axisPageSize, page: mark.anchorPage }));
          renderAxis();
          setAxisNotice(`压缩 #${mark.version} 的锚点在第 ${mark.anchorPage + 1} 页，已跳转到该页`);
        }
        const current = axisCompactionMarks[Number(compactIndex)] || mark;
        const wasOpen = axisDetailKey === `compact:${compactIndex}`;
        toggleAxisDetail(`compact:${compactIndex}`, { type: "compression", mark: current });
        // 打开即取折叠帧正文（后端把帧写进会话内容存储、快照只带 ref）：用户
        // 点一下就看到"压掉了什么、留下了什么"，而不是被指向模型侧工具。
        if (!wasOpen && current.frameRef) loadCompactionFrame(current, "first");
        return;
      }
    }
    const key = segment.dataset.trajectoryKey;
    if (!key) return;
    if (filter !== "all") setFilter("all");
    requestAnimationFrame(() => focusRow(key));
  });

  // 上下文轴滚轮（只在轴区域内响应，使用 passive:false + preventDefault，
  // 与对话区滚轮互不抢占）：
  //   滚轮        = 翻页（向上=更早一页，向下=更新一页；只有一页且没有更早的
  //                 未加载内容时放行滚轮，不做无意义的拦截）
  //   Shift+滚轮  = 调整分页页大小（档位步进，轴头给可见反馈）
  // 累计阈值（axisWheelStep）让一次连续手势只翻一页；横向滚动不属于轴手势，
  // 原样放行（不 preventDefault）。轴未渲染（空数据）时也不接管滚轮。
  axisEl.addEventListener("wheel", event => {
    if (event.ctrlKey) return;
    if (!axisEl.querySelector(".context-axis-page")) return;
    const vertical = Math.abs(event.deltaY) >= Math.abs(event.deltaX);
    if (!event.shiftKey && (!vertical || !axisPageable)) return;
    const delta = vertical ? event.deltaY : event.deltaX;
    if (!delta) return;
    event.preventDefault();
    if (event.shiftKey) {
      const stepped = stepAxisPageSize(axisPageSize, delta > 0 ? 1 : -1);
      if (!stepped.changed) {
        setAxisNotice(`页大小已是${stepped.atMin ? "最小" : "最大"} ${stepped.size} 条`);
        return;
      }
      axisPageSize = stepped.size;
      wheelAccumulator = 0;
      renderAxis();
      const current = resolveAxisPage(records, { pageSize: axisPageSize, page: axisPage, anchorKey: axisAnchorKey, tail: axisTail });
      setAxisNotice(`页大小 ${stepped.previous} → ${stepped.size} 条：现在共 ${current.pageCount} 页，本页${axisRangeLabel(current)}`);
      return;
    }
    const stepped = axisWheelStep(wheelAccumulator, delta);
    wheelAccumulator = stepped.accumulated;
    if (stepped.step) goToAxisPage(stepped.step);
  }, { passive: false });

  // goToAxisPage 翻页（step = +1 更新 / -1 更早）。越界一律显式提示：
  //  - 往更早翻到边界：有 hasMore 就请求加载更早内容（既有 loadMore 接线），
  //    没有就说明会话已全部加载——不静默跳位；
  //  - 往更新翻到边界：提示已在最新一页。
  function goToAxisPage(step) {
    const current = resolveAxisPage(records, { pageSize: axisPageSize, page: axisPage, anchorKey: axisAnchorKey, tail: axisTail });
    const target = current.page + step;
    if (target < 0) {
      if (requestEarlierContent()) return;
      setAxisNotice("已到最早一页（已加载窗口内没有更早的记录）");
      return;
    }
    if (target > current.pageCount - 1) {
      setAxisNotice("已到最新一页");
      return;
    }
    const next = resolveAxisPage(records, { pageSize: axisPageSize, page: target, tail: target === current.pageCount - 1 });
    applyAxisPage(records, next);
    renderAxis();
  }

  // applyAxisPage 把解析出的页窗口写回本地状态：停在尾页时 tail=true（跟随新
  // 记录），否则用本页首条记录做锚点（加载更早内容后视图跟着原记录走）。
  function applyAxisPage(list, view) {
    axisPage = view.page;
    axisTail = view.page === view.pageCount - 1;
    axisAnchorKey = axisTail ? "" : (list[view.start]?.key || "");
  }

  // axisRangeLabel 本页区间文案（空页不写成"第 1–0 条"，与轴头口径一致）。
  function axisRangeLabel(view) {
    return view.end > view.start ? `第 ${view.start + 1}–${view.end} 条` : "为空";
  }

  // requestEarlierContent 请求加载更早内容（复用既有 app.js loadMore 接线）：
  // 先给明确提示，再去重（加载中不重复请求）与冷却（连续滚轮不反复触发）。
  function requestEarlierContent() {
    if (!axisHasMore || typeof options.loadMore !== "function") return false;
    const now = Date.now();
    if (axisLoadPending) {
      setAxisNotice("正在加载更早的回合…");
      return true;
    }
    if (now < axisLoadCooldownUntil) {
      setAxisNotice("更早的回合尚未加载（刚请求过，稍候再看）");
      return true;
    }
    axisLoadPending = true;
    axisLoadCooldownUntil = now + 4000;
    setAxisNotice("更早的回合尚未加载，正在加载更早内容…");
    Promise.resolve(options.loadMore())
      .catch(() => setAxisNotice("加载更早内容失败"))
      .finally(() => { axisLoadPending = false; });
    return true;
  }

  // setAxisNotice 写瞬时反馈：直接落到轴头节点（不重新渲染），并记进状态，
  // 这样随后的轴重渲染不会把提示吞掉；到点自动清空。
  function setAxisNotice(text, ttl = 2800) {
    axisNotice = text || "";
    const node = axisEl.querySelector("[data-axis-note]");
    if (node) node.textContent = axisNotice;
    if (axisNoticeTimer !== null) window.clearTimeout(axisNoticeTimer);
    axisNoticeTimer = null;
    if (!axisNotice) return;
    axisNoticeTimer = window.setTimeout(() => {
      axisNoticeTimer = null;
      axisNotice = "";
      const current = axisEl.querySelector("[data-axis-note]");
      if (current) current.textContent = "";
    }, ttl);
  }

  // renderAxis 渲染上下文轴（含分页）：分页状态只在本地，过滤切换/详情开关/
  // reconcile 重建都走这里，因此页号与页大小在这些操作后保持一致。
  function renderAxis() {
    const view = resolveAxisPage(records, { pageSize: axisPageSize, page: axisPage, anchorKey: axisAnchorKey, tail: axisTail });
    axisPrefixSegments = prefixLayerSegments(prefixLayers);
    axisCompactionMarks = compactionMarks(records, compactions, { page: view.page, pageSize: view.pageSize });
    axisEl.innerHTML = renderContextAxis(records, {
      prefixLayers,
      compactions,
      page: view.page,
      pageSize: view.pageSize,
      notice: axisNotice,
      hasMore: axisHasMore
    });
    applyAxisPage(records, view);
    axisPageable = view.pageCount > 1 || axisHasMore;
    refreshOpenAxisDetail();
  }

  // toggleAxisDetail 打开/关闭轴详情；再次点击同一块收起。
  function toggleAxisDetail(key, selection) {
    if (axisDetailKey === key) {
      closeAxisDetail();
      return;
    }
    axisDetailKey = key;
    axisFrame = null;
    renderAxisDetailContent(selection);
  }

  function closeAxisDetail() {
    if (!axisDetailKey && detailEl.hidden) return;
    axisDetailKey = "";
    axisFrame = null;
    detailEl.innerHTML = "";
    detailEl.hidden = true;
  }

  // renderAxisDetailContent 渲染详情；内容未变化时跳过（流式重渲染不闪烁）。
  // selection 里的 frame = 已加载的折叠帧正文页（视图本地状态，不进 Snapshot）。
  function renderAxisDetailContent(selection) {
    const merged = selection && selection.type === "compression" ? { ...selection, frame: axisFrame } : selection;
    const html = renderAxisDetail(merged);
    if (!html) {
      closeAxisDetail();
      return;
    }
    if (axisFrame?.loading && detailEl.querySelector(".axis-detail-text")) {
      detailEl.querySelector(".axis-detail-text").textContent = "读取中…";
      return;
    }
    detailEl.innerHTML = html;
    detailEl.hidden = false;
  }

  // loadCompactionFrame 按 ref 分页读取「折叠帧正文」（会话内容存储里的有界
  // checkpoint 帧）。offset=0 首次加载；page="more" 续读下一页；"hide" 收起正文
  // （保留详情本身）。失败只更新详情内的错误文案，不弹全局提示（用户就在这里）。
  async function loadCompactionFrame(mark, mode = "first") {
    if (!mark) return;
    if (mode === "hide") {
      axisFrame = null;
      renderAxisDetailContent({ type: "compression", mark });
      return;
    }
    const offset = mode === "more" ? Number(axisFrame?.nextOffset || 0) : 0;
    if (mode === "more" && !(offset > 0)) return;
    const previous = mode === "more" ? String(axisFrame?.text || "") : "";
    axisFrame = {
      loading: true,
      error: "",
      text: previous,
      hasMore: Boolean(axisFrame?.hasMore),
      nextOffset: offset,
      totalBytes: Number(axisFrame?.totalBytes || 0)
    };
    renderAxisDetailContent({ type: "compression", mark });
    if (typeof options.loadResultRef !== "function" || !mark.frameRef) {
      axisFrame = { loading: false, error: "当前环境不支持按 ref 读回正文", text: "", hasMore: false, nextOffset: 0, totalBytes: 0 };
      renderAxisDetailContent({ type: "compression", mark });
      return;
    }
    try {
      const page = await options.loadResultRef(mark.frameRef, offset, options.resultPageLimit || 12000);
      axisFrame = {
        loading: false,
        error: "",
        text: previous + String(page?.content || ""),
        hasMore: Boolean(page?.has_more),
        nextOffset: Number(page?.next_offset || 0),
        totalBytes: Number(page?.total_bytes || 0)
      };
    } catch (error) {
      axisFrame = { loading: false, error: String(error), text: previous, hasMore: false, nextOffset: 0, totalBytes: Number(axisFrame?.totalBytes || 0) };
    }
    renderAxisDetailContent({ type: "compression", mark });
  }

  // refreshOpenAxisDetail 在 render 重算元数据后保持已打开的详情有效（例如
  // 压缩刻度随分页窗口移动）；索引仍存在则刷新内容，否则收起。
  function refreshOpenAxisDetail() {
    if (!axisDetailKey) return;
    const separator = axisDetailKey.indexOf(":");
    const type = axisDetailKey.slice(0, separator);
    const index = Number(axisDetailKey.slice(separator + 1));
    if (type === "prefix") {
      const layer = axisPrefixSegments[index];
      if (!layer) {
        closeAxisDetail();
        return;
      }
      renderAxisDetailContent({ type: "prefix", layer });
      return;
    }
    if (type === "compact") {
      const mark = axisCompactionMarks[index];
      if (!mark) {
        closeAxisDetail();
        return;
      }
      renderAxisDetailContent({ type: "compression", mark });
    }
  }

  function focusRow(key) {
    const row = listEl.querySelector(`[data-trajectory-key="${CSS.escape(key)}"]`);
    if (!row) return;
    const reduced = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
    row.scrollIntoView({ block: "center", behavior: reduced ? "auto" : "smooth" });
    row.classList.add("is-flash");
    setTimeout(() => row.classList.remove("is-flash"), 1400);
  }

  // setFilter 更新本地过滤状态并重渲染（按钮 active 由 render 负责）。
  function setFilter(next) {
    const normalized = next && next !== "all" ? next : "all";
    if (normalized === filter) return;
    filter = normalized;
    options.onFilterChange?.(filter);
    render(records, filter, true);
  }

  function render(nextRecords, nextFilter = filter, active = true, extras = {}) {
    records = Array.isArray(nextRecords) ? nextRecords : [];
    filter = nextFilter || "all";
    // 元数据只按显式提供更新；未提供（如本地过滤切换重渲染）时沿用已缓存值。
    if (Array.isArray(extras?.prefixLayers)) prefixLayers = extras.prefixLayers;
    if (Array.isArray(extras?.compactions)) compactions = extras.compactions;
    if (extras?.window) windowInfo = extras.window;
    // hasMore（轴翻页到更早边界的提示与"请求加载更早"依据）优先取显式的
    // extras.hasMore，否则与窗口边界条同源（extras.window.hasMore）。
    if (typeof extras?.hasMore === "boolean") axisHasMore = extras.hasMore;
    else if (extras?.window) axisHasMore = Boolean(extras.window.hasMore);
    if (!active) return;
    // 窗口边界条同样按缓存值渲染：本地过滤切换重渲染不该把"加载更早"入口抹掉。
    windowEl.innerHTML = windowInfo ? renderTrajectoryWindowInfo(windowInfo) : "";
    // 上下文轴始终反映完整对话顺序（与过滤状态无关），并保持本地分页状态
    // （页号/页大小/锚点）；extras 附加前缀注入与压缩两条元数据轨。
    renderAxis();
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
