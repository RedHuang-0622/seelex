import { escapeHtml } from "./components.js";
import { markEntering, rollNumber, syncScrollEdges } from "./motion.js";

// ── 工作表格（Work Table）视图 ──────────────────────────────
// 数据源：snapshot.runtime.work_table（权威投影，plan 节点 / todolist 项 /
// fork 子代理归一为 WorkItem 行）与 worktable.changed 增量。
//
// Excel 化交互：
//  - 工具栏：展开/折叠 + 类型筛选 chips（全部/Plan/Task/Todo/Subagent，
//    按权威 kind 筛选）+ 会话筛选 chips（全部会话/仅本会话，按行归属会话
//    session_id 筛选——表格是跨会话台账，会话维度只能靠这层收窄）+ 「实发」
//    开关（只看真正写进当前会话请求尾部的行）；
//  - `<table class="excel-grid">`：固定表头（类型/任务/描述/状态/Assignee/
//    会话/依赖/附件/打点/操作），行 keyed reconciliation；
//  - 底部 sheet 页签：批次 = 维度，「全部」页签居首，点击切换当前批次
//    （类 Excel 切换工作表，批次维度 = 不同批次任务的表格）；
//  - todo 行状态按钮 → Bridge.UpdateWorkItemStatus（pending/doing/done）；
//  - plan/subagent 行「详情」→ 既有节点详情弹窗；
//  - 行内「打点」→ 展开该行 trace 表（后端有界 ≤10 条）。
//
// 渲染策略：keyed reconciliation + html 缓存（只重建变化行）；展开/筛选/
// 批次切换/会话筛选是纯 UI 态（视图实例持有），不写回业务状态。所有文本
// escape。

const PHASE_LABELS = { plan: "Plan", task: "Task", tasklist: "Tasklist", subagent: "Subagent" };
// FILTERS 按权威类型（kind）筛选：全部 / Plan / Task / Todo / Subagent。
const FILTERS = [["all", "全部"], ["plan", "Plan"], ["task", "Task"], ["todo", "Todo"], ["subagent", "Subagent"]];
// SESSION_FILTERS 按行归属会话筛选：全部会话 / 仅本会话（视图会话）。
// 表格是跨会话台账（默认全量），「仅本会话」把会话维度收窄回来。
const SESSION_FILTERS = [["all", "全部会话"], ["mine", "仅本会话"]];
// TERMINAL_STATUSES 是行的终态集合：终态行不再参与请求尾部打点块，因此不
// 计入「实发」。后端判据是 completed/failed（workTableTraceBlockFor），前端
// 另把 todo/subagent 的展示态 done 视同 completed。
const TERMINAL_STATUSES = new Set(["completed", "failed", "done"]);
const KIND_LABELS = { plan: "Plan", task: "Task", todo: "Todo", subagent: "Subagent" };
const PAGE_SIZES = [10, 20, 50];
const DEFAULT_PAGE_SIZE = 20;
const STATUS_LABELS = {
  pending: "PENDING", queued: "QUEUED", running: "RUNNING",
  worktree_creating: "WORKTREE", rebasing: "REBASING", merging: "MERGING",
  completed: "DONE", failed: "FAILED", aborted: "ABORTED", skipped: "SKIPPED",
  canceled: "CANCELED", interrupted: "INTERRUPTED", panicked: "PANICKED", doing: "DOING", done: "DONE",
  retry: "RETRY", active: "ACTIVE", success: "SUCCESS", error: "ERROR"
};
// 表头固定列数（trace 展开行 colspan 对齐此数量）。
const GRID_COLUMNS = 10;

// workTableView 归一化工作表格行（防畸形载荷：非数组 → []，非法行丢弃）。
export function workTableView(items) {
  if (!Array.isArray(items)) return [];
  return items.filter(isWorkItem).map(row => ({
    id: textValue(row.id),
    phase: textValue(row.phase, "plan"),
    task: textValue(row.task),
    description: textValue(row.description),
    status: textValue(row.status, "unknown").toLowerCase(),
    assignee: textValue(row.assignee),
    session_id: textValue(row.session_id),
    kind: textValue(row.kind, "plan"),
    source_id: textValue(row.source_id),
    batch_id: textValue(row.batch_id),
    batch_label: textValue(row.batch_label),
    created_at: textValue(row.created_at),
    retry_count: finiteNumber(row.retry_count) ?? 0,
    dependencies: Array.isArray(row.dependencies) ? row.dependencies.map(textValue) : [],
    attachments: Array.isArray(row.attachments) ? row.attachments.map(textValue) : [],
    elapsed: textValue(row.elapsed),
    trace: Array.isArray(row.trace)
      ? row.trace.filter(isTracePoint).map(point => ({
        at: textValue(point.at),
        status: textValue(point.status, "unknown").toLowerCase(),
        operation: textValue(point.operation),
        evidence: textValue(point.evidence),
        duration: textValue(point.duration)
      }))
      : []
  })).filter(row => row.id);
}

// workTableBatches 归一化批次分片头（防畸形载荷：非数组 → []；计数缺失
// 的键补 0）。批次 ID 为空串表示「早期任务」伪批次。
export function workTableBatches(value) {
  if (!Array.isArray(value)) return [];
  return value
    .filter(batch => Boolean(batch) && typeof batch === "object" && !Array.isArray(batch))
    .map(batch => ({
      id: textValue(batch.id),
      label: textValue(batch.label, "批次"),
      created_at: textValue(batch.created_at),
      counts: normalizeBatchCounts(batch.counts)
    }));
}

function normalizeBatchCounts(counts) {
  const normalized = { all: 0, plan: 0, task: 0, todo: 0, subagent: 0 };
  if (!counts || typeof counts !== "object") return normalized;
  for (const key of Object.keys(normalized)) {
    normalized[key] = finiteNumber(counts[key]) ?? 0;
  }
  return normalized;
}

// createWorkTableView 创建视图实例：持有展开/筛选/会话筛选/实发开关/批次
// 维度/trace 展开的纯 UI 态。
//
// options.viewSessionID 是**函数**（返回当前视图会话 ID）：会话切换后
// 视图会重渲染，取值即时——「仅本会话」筛的永远是此刻的会话，而不是创建
// 视图时的会话。
export function createWorkTableView(container, options = {}) {
  const state = {
    expanded: true,
    filter: "all",
    sessionFilter: "all",
    sentOnly: false,
    viewSessionID: "",
    traces: new Set(),
    batches: [],
    activeBatch: "all",
    page: 1,
    pageSize: normalizePageSize(options.pageSize)
  };
  const htmlCache = new Map();
  let items = [];

  function render(nextItems = items, nextBatches) {
    items = workTableView(nextItems);
    if (typeof options.viewSessionID === "function") {
      state.viewSessionID = options.viewSessionID() || "";
    }
    if (nextBatches !== undefined) state.batches = workTableBatches(nextBatches);

    // 批次维度失效（批次头被清空/重建，或会话筛选把该批次的行走空）时回退
    // 「全部」页签。
    if (state.activeBatch !== "all" && !batchesInScope(items, state).some(batch => batch.id === state.activeBatch)) {
      state.activeBatch = "all";
    }
    if (!container.querySelector("[data-work-table]")) {
      container.classList.remove("muted");
      container.classList.add("work-table-view");
      container.innerHTML = renderShellHTML(items, state);
    } else {
      updateShell(container, items, state);
    }
    const filtered = visibleRows(items, state);
    const paged = pagedRows(filtered, state);
    const rowsContainer = container.querySelector("[data-work-rows]");
    reconcileRows(rowsContainer, paged, state, htmlCache);
    const sheetsRoot = ensureSheetsBar(container, state);
    if (sheetsRoot) reconcileSheets(sheetsRoot, items, state);
    if (options.onCount) options.onCount(items.length);
  }

  function bind(handlers) {
    container.addEventListener("click", event => {
      const entry = event.target.closest?.("[data-work-entry-toggle]");
      if (entry) {
        state.expanded = !state.expanded;
        render();
        return;
      }
      const sheet = event.target.closest?.("[data-work-sheet]");
      if (sheet?.dataset.workSheet !== undefined) {
        state.activeBatch = sheet.dataset.workSheet;
        state.page = 1;
        render();
        return;
      }
      const filter = event.target.closest?.("[data-work-filter]");
      if (filter?.dataset.workFilter) {
        state.filter = filter.dataset.workFilter;
        state.page = 1;
        render();
        return;
      }
      const sessionFilter = event.target.closest?.("[data-work-session-filter]");
      if (sessionFilter?.dataset.workSessionFilter) {
        state.sessionFilter = sessionFilter.dataset.workSessionFilter;
        state.page = 1;
        render();
        return;
      }
      const sentFilter = event.target.closest?.("[data-work-sent-filter]");
      if (sentFilter) {
        // 开关（不是单选组）：undefined 属性也命中，靠 toggle 翻转。
        state.sentOnly = !state.sentOnly;
        state.page = 1;
        render();
        return;
      }
      const pagePrev = event.target.closest?.("[data-work-page-prev]");
      if (pagePrev) {
        state.page = Math.max(1, state.page - 1);
        render();
        return;
      }
      const pageNext = event.target.closest?.("[data-work-page-next]");
      if (pageNext) {
        state.page += 1;
        render();
        return;
      }
      const trace = event.target.closest?.("[data-work-trace-toggle]");
      if (trace?.dataset.workTraceToggle) {
        const id = trace.dataset.workTraceToggle;
        if (state.traces.has(id)) state.traces.delete(id);
        else state.traces.add(id);
        render();
        return;
      }
      const detail = event.target.closest?.("[data-plan-node-open]");
      if (detail?.dataset.planNodeOpen) {
        // 详情入口委托给宿主（节点详情弹窗），弹窗内 self-contained。
        event.stopPropagation();
        handlers.onDetail?.(detail.dataset.planNodeOpen);
        return;
      }
      const status = event.target.closest?.("[data-work-status]");
      if (status?.dataset.workStatus && status.dataset.status) {
        handlers.onStatus?.(status.dataset.workStatus, status.dataset.status);
      }
    });
    container.addEventListener("change", event => {
      const pageSize = event.target.closest?.("[data-work-page-size]");
      if (!pageSize) return;
      const next = Number.parseInt(pageSize.value, 10);
      if (Number.isFinite(next) && next > 0 && next !== state.pageSize) {
        state.pageSize = next;
        state.page = 1;
        render();
      }
    });
  }

  return { render, bind, state, current: () => items };
}

// workTableSignatures 生成当前行快照签名（status + retry_count）——
// “未读”判据：新出现或签名变化的条目计为未读。
export function workTableSignatures(rows) {
  const seen = new Map();
  for (const row of rows) {
    seen.set(row.id, `${row.status}|${row.retry_count}`);
  }
  return seen;
}

// countUnread 统计未读条目：从未打开过（seen 空）→ 全部未读；否则统计
// 新增/状态或 retry 变化的行。
export function countUnread(rows, seen) {
  if (!seen || seen.size === 0) return rows.length;
  let count = 0;
  for (const row of rows) {
    if (seen.get(row.id) !== `${row.status}|${row.retry_count}`) count += 1;
  }
  return count;
}

// rowsForSheet 按当前批次维度过滤（「全部」/无批次/未设维度时返回原列表）。
function rowsForSheet(items, state) {
  if (!state.activeBatch || state.activeBatch === "all" || !Array.isArray(state.batches) || !state.batches.length) return items;
  return items.filter(row => (row.batch_id || "") === state.activeBatch);
}

// rowBelongsToViewSession 判定行是否归属当前视图会话。空归属（草稿会话/
// 未归属行/旧版无键增量）按"本会话"处理：后端把实时注册表恒判给当前视图会话
// （见 seelebridge taskSnapshotAll 与 application/core publishTaskChanged），
// 一行丢了归属键不该因此从筛选与计数里消失（「仅本会话筛不到正在运行的子代理」
// 就是这样发生的）。渲染面另有「未归属（草稿）」占位，两处语义不冲突。
function rowBelongsToViewSession(row, state) {
  const owner = row.session_id || "";
  return owner === "" || owner === (state?.viewSessionID || "");
}

// sessionRows 按归属会话过滤：「仅本会话」只留归属当前视图会话的行。工作表格
// 是跨会话台账（后端读面默认全量），这是把会话维度收窄回来的唯一轴；默认
// 「全部会话」。
function sessionRows(rows, state) {
  if (state?.sessionFilter !== "mine") return rows;
  return rows.filter(row => rowBelongsToViewSession(row, state));
}

// isDispatchedRow 判定「实发」行：归属当前视图会话且未终态——这正是后端请求
// 尾部打点块（真正送进模型上下文的那段标记块）的筛选规则（会话作用域 + 仅
// 未终态）。台账默认全量（含终态历史与别的会话的行），「实发」是贴在台账上
// 最窄的一层视图，回答"模型此刻实际看到了哪些条目"。
function isDispatchedRow(row, state) {
  if (!rowBelongsToViewSession(row, state)) return false;
  return !TERMINAL_STATUSES.has(statusToken(row.status));
}

// dispatchRows 应用「实发」开关（关时原样返回）。
function dispatchRows(rows, state) {
  if (!state?.sentOnly) return rows;
  return rows.filter(row => isDispatchedRow(row, state));
}

// scopeRows 只应用会话 + 实发两轴（不含批次维度）——供批次页签判定使用，
// 避免把当前 sheet 的批次过滤混进"这个批次该不该出现"的判断。
function scopeRows(items, state) {
  return dispatchRows(sessionRows(items, state), state);
}

// scopedRows 依次应用批次维度、会话筛选与实发开关（计数、sheet 页签、可见行
// 共用同一口径，避免"计数说 5 条、列表只显示 3 条"）。
function scopedRows(items, state) {
  return scopeRows(rowsForSheet(items, state), state);
}

// visibleRows 在 scope（批次 + 会话）上再应用类型筛选（纯客户端过滤）。
function visibleRows(items, state) {
  const rows = scopedRows(items, state);
  if (state.filter === "all") return rows;
  return rows.filter(row => row.kind === state.filter);
}

// sessionCounts 会话筛选轴的计数：分母是批次维度内的全部行（不受会话筛选与
// 类型筛选影响），分子是其中归属当前会话的行。实发计数（dispatchCount）同源
// ——按批次维度内的行统计，不受「仅本会话」/「实发」自身影响，否则开启后计数
// 会自我坍缩。归属判定与 sessionRows 共用 rowBelongsToViewSession（同一口径，
// 否则会出现"计数说 1 条、列表 0 条"）。
function sessionCounts(items, state) {
  const base = rowsForSheet(items, state);
  return {
    all: base.length,
    mine: base.filter(row => rowBelongsToViewSession(row, state)).length
  };
}

// dispatchCount 统计批次维度内「实发」行数（本会话 + 未终态）。
function dispatchCount(items, state) {
  return rowsForSheet(items, state).filter(row => isDispatchedRow(row, state)).length;
}

// batchesInScope 会话/实发筛选生效时只保留「在该 scope 里有行」的批次页签
// ——否则「仅本会话」或「实发」下会列出一堆空批次（点进去是空表）。
function batchesInScope(items, state) {
  const batches = Array.isArray(state?.batches) ? state.batches : [];
  if (state?.sessionFilter !== "mine" && !state?.sentOnly) return batches;
  const present = new Set(scopeRows(items, state).map(row => row.batch_id || ""));
  return batches.filter(batch => present.has(batch.id));
}

// pageCount 计算分页总数（空列表也至少 1 页）。
export function pageCount(count, pageSize) {
  const size = normalizePageSize(pageSize);
  if (count == null || count <= 0) return 1;
  return Math.max(1, Math.ceil(Number(count) / size));
}

// pagedRows 按当前页截取行并钳制页码（数据收缩后不越界）。
export function pagedRows(rows, state) {
  const list = Array.isArray(rows) ? rows : [];
  const size = normalizePageSize(state?.pageSize);
  const pages = pageCount(list.length, size);
  state.page = Math.min(Math.max(1, state.page || 1), pages);
  const start = (state.page - 1) * size;
  return list.slice(start, start + size);
}

function normalizePageSize(value) {
  const size = Number.parseInt(value, 10);
  return Number.isFinite(size) && size > 0 ? size : DEFAULT_PAGE_SIZE;
}

function updateShell(container, items, state) {
  container.dataset.workCount = String(items.length);
  const toggle = container.querySelector("[data-work-entry-toggle]");
  if (toggle) {
    toggle.setAttribute("aria-expanded", String(state.expanded));
    const chevron = toggle.querySelector(".work-chevron");
    if (chevron) chevron.textContent = state.expanded ? "▾" : "▸";
    const total = toggle.querySelector(".work-total");
    if (total) rollNumber(total, `${scopedRows(items, state).length} 项`);
    const traceTotal = toggle.querySelector(".work-trace-total");
    if (traceTotal) rollNumber(traceTotal, `${countTrace(scopedRows(items, state))} 打点`);
  }
  container.querySelector("[data-work-entry-body]")?.classList.toggle("is-collapsed", !state.expanded);
  const base = scopedRows(items, state);
  const counts = kindCounts(base);
  container.querySelectorAll("[data-work-filter]").forEach(button => {
    const key = button.dataset.workFilter;
    button.classList.toggle("is-active", state.filter === key);
    const span = button.querySelector("span");
    if (span) rollNumber(span, String(counts[key] ?? 0));
  });
  const sessions = sessionCounts(items, state);
  container.querySelectorAll("[data-work-session-filter]").forEach(button => {
    const key = button.dataset.workSessionFilter;
    button.classList.toggle("is-active", (state.sessionFilter || "all") === key);
    const span = button.querySelector("span");
    if (span) rollNumber(span, String(sessions[key] ?? 0));
  });
  const dispatched = dispatchCount(items, state);
  container.querySelectorAll("[data-work-sent-filter]").forEach(button => {
    button.classList.toggle("is-active", Boolean(state.sentOnly));
    button.setAttribute("aria-pressed", String(Boolean(state.sentOnly)));
    const span = button.querySelector("span");
    if (span) rollNumber(span, String(dispatched));
  });
  const filtered = visibleRows(items, state);
  const pages = pageCount(filtered.length, state.pageSize);
  const page = clampPage(state.page, pages);
  const pageInfo = container.querySelector("[data-work-page-info]");
  if (pageInfo) pageInfo.textContent = `${page} / ${pages} 页 · ${filtered.length} 项`;
  const prev = container.querySelector("[data-work-page-prev]");
  if (prev) prev.disabled = page <= 1;
  const next = container.querySelector("[data-work-page-next]");
  if (next) next.disabled = page >= pages;
  const size = container.querySelector("[data-work-page-size]");
  if (size) size.value = String(state.pageSize);
}

export function renderShellHTML(items, state) {
  const base = scopedRows(items, state);
  const counts = kindCounts(base);
  const sessions = sessionCounts(items, state);
  const filtered = visibleRows(items, state);
  const pages = pageCount(filtered.length, state.pageSize);
  const page = clampPage(state.page, pages);
  const filters = FILTERS.map(([key, label]) => {
    const active = state.filter === key;
    return `<button type="button" class="work-filter${active ? " is-active" : ""}" data-work-filter="${key}" data-work-count="${counts[key] ?? 0}">${escapeHtml(label)} <span>${counts[key] ?? 0}</span></button>`;
  }).join("");
  const sessionFilters = SESSION_FILTERS.map(([key, label]) => {
    const active = (state.sessionFilter || "all") === key;
    return `<button type="button" class="work-filter is-session${active ? " is-active" : ""}" data-work-session-filter="${key}" data-work-session-count="${sessions[key] ?? 0}">${escapeHtml(label)} <span>${sessions[key] ?? 0}</span></button>`;
  }).join("");
  const dispatched = dispatchCount(items, state);
  const sentFilter = `<button type="button" class="work-filter is-sent${state.sentOnly ? " is-active" : ""}" data-work-sent-filter="sent" aria-pressed="${Boolean(state.sentOnly)}" data-work-sent-count="${dispatched}" title="只显示真正写进当前会话请求尾部的行（本会话且未终态）">实发 <span>${dispatched}</span></button>`;
  const pageSizes = PAGE_SIZES.map(size =>
    `<option value="${size}"${state.pageSize === size ? " selected" : ""}>${size} / 页</option>`
  ).join("");
  const batches = Array.isArray(state?.batches) ? state.batches : [];
  const sheets = batches.length ? renderSheetTabsHTML(items, state) : "";
  return `<div class="work-table" data-work-table>
    <header class="work-table-head">
      <button type="button" class="work-entry-toggle" data-work-entry-toggle aria-expanded="${state.expanded}" title="展开/折叠工作表格">
        <span class="work-chevron" aria-hidden="true">${state.expanded ? "▾" : "▸"}</span>
        <strong>工作表格</strong>
        <span class="work-total">${base.length} 项</span>
        <span class="work-trace-total">${countTrace(base)} 打点</span>
      </button>
      <div class="work-filters" data-work-filters>
        <span class="work-filter-group" data-work-session-filters role="group" aria-label="按归属会话筛选">${sessionFilters}</span>
        <span class="work-filter-group" data-work-scope-filters role="group" aria-label="按是否实发筛选">${sentFilter}</span>
        <span class="work-filter-group" data-work-kind-filters role="group" aria-label="按类型筛选">${filters}</span>
      </div>
    </header>
    <div class="work-entry-body${state.expanded ? "" : " is-collapsed"}" data-work-entry-body>
      <div class="work-table-scroll" data-work-table-scroll>
        <table class="excel-grid" data-excel-grid>
          <thead>
            <tr class="excel-head-row">
              <th>类型</th><th>任务</th><th>描述</th><th>状态</th><th>Assignee</th>
              <th>会话</th><th>依赖</th><th>附件</th><th>打点</th><th>操作</th>
            </tr>
          </thead>
          <tbody data-work-rows></tbody>
        </table>
      </div>
      <div class="work-pager" data-work-pager>
        <button type="button" class="work-page-btn" data-work-page-prev${page <= 1 ? " disabled" : ""}>‹ 上一页</button>
        <span class="work-page-info" data-work-page-info>${page} / ${pages} 页 · ${filtered.length} 项</span>
        <button type="button" class="work-page-btn" data-work-page-next${page >= pages ? " disabled" : ""}>下一页 ›</button>
        <label class="work-page-size-label">每页
          <select class="work-page-size" data-work-page-size aria-label="每页条数">${pageSizes}</select>
        </label>
      </div>
    </div>
    ${sheets}
  </div>`;
}

function kindCounts(items) {
  const counts = { all: items.length, plan: 0, task: 0, todo: 0, subagent: 0 };
  for (const row of items) {
    if (counts[row.kind] !== undefined) counts[row.kind] += 1;
  }
  return counts;
}

// renderSheetTabsHTML 渲染批次 sheet 页签（「全部」居首；批次页签带权威批次
// 计数摘要，如 Task 1 · Todo 1）。会话筛选生效时只列该会话里有行的批次
// （否则「仅本会话」下会点出一堆空批次），但「全部」页签恒在，DOM 结构不随
// 筛选变化。
function renderSheetTabsHTML(items, state) {
  const all = Array.isArray(state?.batches) ? state.batches : [];
  if (!all.length) return "";
  const tabs = [
    sheetTabHTML("all", "全部", `共 ${scopedRows(items, state).length} 项`, state.activeBatch === "all")
  ];
  for (const batch of batchesInScope(items, state)) {
    tabs.push(sheetTabHTML(batch.id, batch.label || "批次", batchCountsText(batch.counts), state.activeBatch === batch.id));
  }
  return `<div class="excel-sheets scroll-edges-x" data-work-sheets>${tabs.join("")}</div>`;
}

function sheetTabHTML(id, label, countsText, active) {
  const safeID = escapeHtml(id);
  return `<button type="button" class="excel-sheet${active ? " is-active" : ""}" data-work-sheet="${safeID}" title="${escapeHtml(label)}">
    <span class="excel-sheet-label">${escapeHtml(label)}</span>
    <span class="excel-sheet-counts">${escapeHtml(countsText)}</span>
  </button>`;
}

// ensureSheetsBar 保证批次 sheet 栏存在/移除（快照从有批次切到无批次时移除）。
function ensureSheetsBar(container, state) {
  let sheetsRoot = container.querySelector("[data-work-sheets]");
  if (!state.batches.length) {
    sheetsRoot?.remove();
    return null;
  }
  if (!sheetsRoot) {
    const anchor = container.querySelector("[data-excel-grid]") || container.querySelector("[data-work-table-scroll]");
    if (anchor) anchor.insertAdjacentHTML("afterend", '<div class="excel-sheets scroll-edges-x" data-work-sheets></div>');
    sheetsRoot = container.querySelector("[data-work-sheets]");
  }
  return sheetsRoot;
}

function reconcileSheets(sheetsRoot, items, state) {
  if (!sheetsRoot) return;
  const html = renderSheetTabsHTML(items, state);
  if (sheetsRoot.innerHTML !== html) sheetsRoot.innerHTML = html;
  // 横条滚到中间时两端浮出内阴影（节点在 app.js 的委托滚动监听里也会更新，
  // 这里补一次保证重绘后立即可见）。
  syncScrollEdges(sheetsRoot, "x");
}

// batchCountsText 渲染批次各类计数（仅展示非零类型）。
function batchCountsText(counts) {
  const parts = [];
  for (const [key, label] of [["plan", "Plan"], ["task", "Task"], ["todo", "Todo"], ["subagent", "Subagent"]]) {
    const value = counts?.[key] || 0;
    if (value > 0) parts.push(`${label} ${value}`);
  }
  return parts.join(" · ") || "空批次";
}

// clampPage 将页码钳制到 [1, pages]；非法输入按第 1 页处理。
function clampPage(page, pages) {
  const value = Number.parseInt(page, 10);
  const p = Number.isFinite(value) && value > 0 ? value : 1;
  return Math.min(p, Math.max(1, pages));
}

// reconcileRows 在 tbody 内做行级 keyed reconciliation：主行
// （data-work-row）+ trace 展开行（data-work-trace，紧随主行）。
function reconcileRows(rowsContainer, visible, state, htmlCache) {
  if (!rowsContainer) return;
  const existing = new Map();
  for (const child of Array.from(rowsContainer.children)) {
    const key = child.dataset?.workRow;
    if (key !== undefined) existing.set(key, child);
  }
  const used = new Set();

  for (const row of visible) {
    used.add(row.id);
    const html = renderWorkItemRow(row, state);
    let element = existing.get(row.id);
    if (!element) {
      element = elementFromHTML(rowsContainer.ownerDocument, html);
      rowsContainer.append(element);
      // 新行入场（工具过程中新出现的条目/操作类型）：只在**首次插入**时挂
      // 一次性入场类，后续内容替换（replacement）不重播，避免行在流式更新里
      // 反复闪。
      markEntering(element);
    } else if (htmlCache.get(row.id) !== html) {
      const oldNext = element.nextElementSibling;
      const replacement = elementFromHTML(rowsContainer.ownerDocument, html);
      element.replaceWith(replacement);
      // 主行内容变化时一并移除旧 trace 展开行（稍后按需重建）。
      if (oldNext?.dataset?.workTrace === row.id) oldNext.remove();
      element = replacement;
    }
    htmlCache.set(row.id, html);
    reconcileTraceRow(rowsContainer, element, row, state);
  }

  existing.forEach((element, id) => {
    if (!used.has(id)) {
      if (element.nextElementSibling?.dataset?.workTrace === id) element.nextElementSibling.remove();
      element.remove();
    }
  });
  for (const child of Array.from(rowsContainer.children)) {
    if (child.dataset?.workTrace !== undefined && !used.has(child.dataset.workTrace)) child.remove();
  }
  for (const id of [...htmlCache.keys()]) {
    if (!used.has(id)) htmlCache.delete(id);
  }

  if (!visible.length) {
    if (!rowsContainer.querySelector(":scope > .work-empty-row")) {
      rowsContainer.insertAdjacentHTML("beforeend", `<tr class="work-empty-row" data-work-empty><td colspan="${GRID_COLUMNS}">当前筛选无任务</td></tr>`);
    }
  } else {
    rowsContainer.querySelector(":scope > .work-empty-row")?.remove();
  }
}

// reconcileTraceRow 维护主行后的 trace 展开行（state.traces 驱动）。
function reconcileTraceRow(rowsContainer, mainRow, row, state) {
  if (!mainRow) return;
  const next = mainRow.nextElementSibling;
  const hasTraceRow = next?.dataset?.workTrace === row.id;
  if (state.traces.has(row.id) && !hasTraceRow) {
    const traceRow = elementFromHTML(rowsContainer.ownerDocument, renderWorkTraceRow(row));
    mainRow.after(traceRow);
    markEntering(traceRow);
  } else if (!state.traces.has(row.id) && hasTraceRow) {
    next.remove();
  }
}

export function renderWorkItemRow(row, state) {
  const status = statusToken(row.status);
  const trace = row.trace || [];
  const traceOpen = state.traces.has(row.id);
  const deps = row.dependencies || [];
  const attachments = row.attachments || [];
  const kindLabel = KIND_LABELS[row.kind] || PHASE_LABELS[row.phase] || row.kind || row.phase || "—";
  const dispatched = isDispatchedRow(row, state);
  const sentBadge = dispatched
    ? '<span class="work-sent-chip" title="实发：归属本会话且未终态，已进入请求尾部打点块">实发</span>'
    : "";
  const actions = row.kind === "todo"
    ? renderTodoStatusControl(row)
    : `<button type="button" class="work-row-detail-btn" data-plan-node-open="${escapeHtml(row.source_id || row.id)}" title="查看会话记录 / 上下文 / 打点详情">详情</button>`;
  return `<tr class="work-row is-${status}" data-work-row="${escapeHtml(row.id)}" data-work-kind="${escapeHtml(row.kind)}" data-work-session="${escapeHtml(row.session_id)}" data-work-dispatched="${dispatched ? "1" : "0"}">
    <td class="work-cell work-cell-kind" title="${escapeHtml(row.phase)}"><span class="work-phase-chip is-${escapeHtml(row.kind)}">${escapeHtml(kindLabel)}</span></td>
    <td class="work-cell work-cell-task" title="${escapeHtml(row.task)}">${escapeHtml(shorten(row.task, 80))}</td>
    <td class="work-cell work-cell-desc" title="${escapeHtml(row.description)}">${escapeHtml(shorten(row.description, 140)) || '<span class="muted">—</span>'}</td>
    <td class="work-cell"><span class="work-status is-${status}">${escapeHtml(statusCellLabel(row))}</span>${sentBadge}</td>
    <td class="work-cell work-cell-assignee" title="${escapeHtml(row.assignee)}">${escapeHtml(row.assignee || "—")}</td>
    <td class="work-cell work-cell-session" title="${escapeHtml(row.session_id || "未归属（草稿）")}">${escapeHtml(shorten(row.session_id, 24)) || '<span class="muted">—</span>'}</td>
    <td class="work-cell work-cell-deps">${deps.length ? deps.map(dep => `<span class="work-dep" title="${escapeHtml(dep)}">${escapeHtml(shorten(dep, 24))}</span>`).join("") : '<span class="muted">—</span>'}</td>
    <td class="work-cell work-cell-attachments">${attachments.length ? attachments.map(path => `<span class="work-attachment" title="${escapeHtml(path)}">${escapeHtml(shorten(path, 24))}</span>`).join("") : '<span class="muted">—</span>'}</td>
    <td class="work-cell work-cell-trace">${trace.length ? `<button type="button" class="work-trace-toggle" data-work-trace-toggle="${escapeHtml(row.id)}" aria-expanded="${traceOpen}" title="展开任务打点">打点 ${trace.length}</button>` : '<span class="muted">—</span>'}</td>
    <td class="work-cell work-cell-actions">${actions}</td>
  </tr>`;
}

// renderWorkTraceRow 渲染 trace 展开行（colspan 对齐表头列数）。
function renderWorkTraceRow(row) {
  return `<tr class="work-trace-expand" data-work-trace="${escapeHtml(row.id)}">
    <td colspan="${GRID_COLUMNS}">${renderWorkTraceHTML(row.trace || [])}</td>
  </tr>`;
}

function renderTodoStatusControl(row) {
  const current = statusToken(row.status);
  const states = [["pending", "未做"], ["doing", "进行中"], ["done", "完成"]];
  return `<span class="work-todo-status" data-work-todo="${escapeHtml(row.id)}" role="group" aria-label="更新任务状态">
    ${states.map(([value, label]) => `<button type="button" class="work-status-btn${current === value ? " is-active" : ""}" data-work-status="${escapeHtml(row.id)}" data-status="${value}" title="标记为${escapeHtml(label)}">${escapeHtml(label)}</button>`).join("")}
  </span>`;
}

export function renderWorkTraceHTML(trace) {
  return `<div class="work-trace" role="table" aria-label="任务打点">
    <div class="work-trace-row is-head" role="row"><span>时间</span><span>操作</span><span>状态</span><span>证据</span><span>耗时</span></div>
    ${trace.map(point => `<div class="work-trace-row is-${statusToken(point.status)}" role="row">
      <time datetime="${escapeHtml(point.at)}">${escapeHtml(formatEventTime(point.at))}</time>
      <strong>${escapeHtml(point.operation || "—")}</strong>
      <span>${escapeHtml(statusLabel(point.status))}</span>
      <small title="${escapeHtml(point.evidence)}">${escapeHtml(shorten(point.evidence, 120)) || "—"}</small>
      <span>${escapeHtml(point.duration || "—")}</span>
    </div>`).join("")}
  </div>`;
}

function countTrace(items) {
  return items.reduce((total, row) => total + (row.trace?.length || 0), 0);
}

function isWorkItem(value) {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function isTracePoint(value) {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function statusToken(status) {
  return /^[a-z][a-z0-9_-]*$/.test(status || "") ? status : "unknown";
}

function statusLabel(status) {
  return STATUS_LABELS[statusToken(status)] || "UNKNOWN";
}

// statusCellLabel 状态单元格：retry 展示 RETRY n（重试数字）。
function statusCellLabel(row) {
  const status = statusToken(row.status);
  if (status === "retry") return `RETRY ${Math.max(row.retry_count || 1, 1)}`;
  return statusLabel(status);
}

function finiteNumber(value) {
  const number = Number(value);
  return Number.isFinite(number) ? number : null;
}

function shorten(value, limit) {
  const text = String(value || "").replace(/\s+/g, " ").trim();
  if (text.length <= limit) return text;
  return `${text.slice(0, Math.max(limit - 1, 0))}…`;
}

function formatEventTime(iso) {
  if (!iso) return "—";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleTimeString([], { hour12: false });
}

function textValue(value, fallback = "") {
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return fallback;
}

function elementFromHTML(ownerDocument, markup) {
  const template = ownerDocument.createElement("template");
  template.innerHTML = markup.trim();
  return template.content.firstElementChild;
}
