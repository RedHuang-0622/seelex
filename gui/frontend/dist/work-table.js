import { escapeHtml } from "./components.js";

// ── 工作表格（Work Table）视图 ──────────────────────────────
// 数据源：snapshot.runtime.work_table（权威投影，plan 节点 / todolist 项 /
// fork 子代理归一为 WorkItem 行）与 worktable.changed 增量。
//
// 交互：
//  - 条目（工作表格）点开 → 展开多维表格（阶段/任务/描述/状态/Assignee/
//    Dependency/附件/操作）；
//  - 筛选 chips 切换全部/Plan/Tasklist/Subagent（纯客户端过滤）；
//  - todo 行状态按钮 → Bridge.UpdateWorkItemStatus（pending/doing/done）；
//  - plan/subagent 行「详情」→ 既有节点详情弹窗（会话/上下文/打点/时间线/
//    工具活动，数据面 SubagentSessionDetail）；
//  - 行内「打点」→ 展开该行 trace 表（后端有界 ≤10 条）。
//
// 渲染策略：keyed reconciliation + html 缓存（只重建变化行），展开/筛选
// 是纯 UI 态（视图实例持有），不写回业务状态。所有文本 escape。

const PHASE_LABELS = { plan: "Plan", task: "Task", tasklist: "Tasklist", subagent: "Subagent" };
const FILTERS = [["all", "全部"], ["plan", "Plan"], ["task", "Task"], ["tasklist", "Tasklist"], ["subagent", "Subagent"]];
const STATUS_LABELS = {
  pending: "PENDING", queued: "QUEUED", running: "RUNNING",
  worktree_creating: "WORKTREE", rebasing: "REBASING", merging: "MERGING",
  completed: "DONE", failed: "FAILED", aborted: "ABORTED", skipped: "SKIPPED",
  canceled: "CANCELED", panicked: "PANICKED", doing: "DOING", done: "DONE",
  retry: "RETRY", active: "ACTIVE", success: "SUCCESS", error: "ERROR"
};

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
    kind: textValue(row.kind, "plan"),
    source_id: textValue(row.source_id),
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

// createWorkTableView 创建视图实例：持有展开/筛选/trace 展开的纯 UI 态。
export function createWorkTableView(container, options = {}) {
  const state = { expanded: true, filter: "all", traces: new Set() };
  const htmlCache = new Map();
  let items = [];

  function render(nextItems = items) {
    items = workTableView(nextItems);
    if (!container.querySelector("[data-work-table]")) {
      container.classList.remove("muted");
      container.classList.add("work-table-view");
      container.innerHTML = renderShellHTML(items, state);
    } else {
      updateShell(container, items, state);
    }
    const rowsContainer = container.querySelector("[data-work-rows]");
    reconcileRows(rowsContainer, visibleRows(items, state), state, htmlCache);
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
      const filter = event.target.closest?.("[data-work-filter]");
      if (filter?.dataset.workFilter) {
        state.filter = filter.dataset.workFilter;
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
      const status = event.target.closest?.("[data-work-status]");
      if (status?.dataset.workStatus && status.dataset.status) {
        handlers.onStatus?.(status.dataset.workStatus, status.dataset.status);
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

function visibleRows(items, state) {
  if (state.filter === "all") return items;
  return items.filter(row => row.phase === state.filter);
}

function updateShell(container, items, state) {
  container.dataset.workCount = String(items.length);
  const toggle = container.querySelector("[data-work-entry-toggle]");
  if (toggle) {
    toggle.setAttribute("aria-expanded", String(state.expanded));
    const chevron = toggle.querySelector(".work-chevron");
    if (chevron) chevron.textContent = state.expanded ? "▾" : "▸";
    const total = toggle.querySelector(".work-total");
    if (total) total.textContent = `${items.length} 项`;
    const traceTotal = toggle.querySelector(".work-trace-total");
    if (traceTotal) traceTotal.textContent = `${countTrace(items)} 打点`;
  }
  container.querySelector("[data-work-entry-body]")?.classList.toggle("is-collapsed", !state.expanded);
  const counts = phaseCounts(items);
  container.querySelectorAll("[data-work-filter]").forEach(button => {
    const key = button.dataset.workFilter;
    button.classList.toggle("is-active", state.filter === key);
    const span = button.querySelector("span");
    if (span) span.textContent = String(counts[key] ?? 0);
  });
}

export function renderShellHTML(items, state) {
  const counts = phaseCounts(items);
  const filters = FILTERS.map(([key, label]) => {
    const active = state.filter === key;
    return `<button type="button" class="work-filter${active ? " is-active" : ""}" data-work-filter="${key}" data-work-count="${counts[key] ?? 0}">${escapeHtml(label)} <span>${counts[key] ?? 0}</span></button>`;
  }).join("");
  return `<div class="work-table" data-work-table>
    <header class="work-table-head">
      <button type="button" class="work-entry-toggle" data-work-entry-toggle aria-expanded="${state.expanded}" title="展开/折叠工作表格">
        <span class="work-chevron" aria-hidden="true">${state.expanded ? "▾" : "▸"}</span>
        <strong>工作表格</strong>
        <span class="work-total">${items.length} 项</span>
        <span class="work-trace-total">${countTrace(items)} 打点</span>
      </button>
      <div class="work-filters" data-work-filters>${filters}</div>
    </header>
    <div class="work-entry-body${state.expanded ? "" : " is-collapsed"}" data-work-entry-body>
      <div class="work-grid work-grid-head" aria-hidden="true">
        <span>阶段</span><span>任务</span><span>描述</span><span>状态</span>
        <span>Assignee</span><span>Dependency</span><span>附件</span><span>操作</span>
      </div>
      <div class="work-rows" data-work-rows></div>
    </div>
  </div>`;
}

function phaseCounts(items) {
  const counts = { all: items.length, plan: 0, task: 0, tasklist: 0, subagent: 0 };
  for (const row of items) {
    if (counts[row.phase] !== undefined) counts[row.phase] += 1;
  }
  return counts;
}

function reconcileRows(rowsContainer, visible, state, htmlCache) {
  const existing = new Map(Array.from(rowsContainer.children)
    .filter(element => element.dataset?.workRow)
    .map(element => [element.dataset.workRow, element]));
  const used = new Set();
  const renderedHtml = new Map();

  for (const row of visible) {
    used.add(row.id);
    const html = renderWorkItemRow(row, state);
    renderedHtml.set(row.id, html);
    let element = existing.get(row.id);
    if (!element) {
      element = elementFromHTML(rowsContainer.ownerDocument, html);
      rowsContainer.append(element);
      continue;
    }
    if (htmlCache.get(row.id) !== html) {
      const replacement = elementFromHTML(rowsContainer.ownerDocument, html);
      element.replaceWith(replacement);
      element = replacement;
    }
    rowsContainer.append(element);
  }

  existing.forEach((element, id) => {
    if (!used.has(id)) element.remove();
  });
  for (const [id, html] of renderedHtml) htmlCache.set(id, html);
  for (const id of [...htmlCache.keys()]) {
    if (!used.has(id)) htmlCache.delete(id);
  }
  const empty = rowsContainer.querySelector(":scope > .work-empty");
  if (!visible.length) {
    if (!empty) rowsContainer.insertAdjacentHTML("beforeend", '<div class="work-empty">当前筛选无任务</div>');
  } else {
    empty?.remove();
  }
}

export function renderWorkItemRow(row, state) {
  const status = statusToken(row.status);
  const trace = row.trace || [];
  const traceOpen = state.traces.has(row.id);
  const deps = row.dependencies || [];
  const attachments = row.attachments || [];
  const actions = row.kind === "todo"
    ? renderTodoStatusControl(row)
    : `<button type="button" class="work-row-detail-btn" data-plan-node-open="${escapeHtml(row.source_id || row.id)}" title="查看会话记录 / 上下文 / 打点详情">详情</button>`;
  return `<article class="work-row is-${status}" data-work-row="${escapeHtml(row.id)}" data-work-kind="${escapeHtml(row.kind)}">
    <div class="work-grid work-row-grid">
      <span class="work-cell" title="${escapeHtml(row.phase)}"><span class="work-phase-chip is-${escapeHtml(row.phase)}">${escapeHtml(PHASE_LABELS[row.phase] || row.phase)}</span></span>
      <span class="work-cell work-cell-task" title="${escapeHtml(row.task)}">${escapeHtml(shorten(row.task, 80))}</span>
      <span class="work-cell work-cell-desc" title="${escapeHtml(row.description)}">${escapeHtml(shorten(row.description, 140)) || '<span class="muted">—</span>'}</span>
      <span class="work-cell"><span class="work-status is-${status}">${escapeHtml(statusCellLabel(row))}</span></span>
      <span class="work-cell work-cell-assignee" title="${escapeHtml(row.assignee)}">${escapeHtml(row.assignee || "—")}</span>
      <span class="work-cell work-cell-deps">${deps.length ? deps.map(dep => `<span class="work-dep" title="${escapeHtml(dep)}">${escapeHtml(shorten(dep, 24))}</span>`).join("") : '<span class="muted">—</span>'}</span>
      <span class="work-cell work-cell-attachments">${attachments.length ? attachments.map(path => `<span class="work-attachment" title="${escapeHtml(path)}">${escapeHtml(shorten(path, 24))}</span>`).join("") : '<span class="muted">—</span>'}</span>
      <span class="work-cell work-cell-actions">
        ${actions}
        ${trace.length ? `<button type="button" class="work-trace-toggle" data-work-trace-toggle="${escapeHtml(row.id)}" aria-expanded="${traceOpen}" title="展开任务打点">打点 ${trace.length}</button>` : ""}
      </span>
    </div>
    ${traceOpen ? `<div class="work-trace-panel" data-work-trace="${escapeHtml(row.id)}">${renderWorkTraceHTML(trace)}</div>` : ""}
  </article>`;
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
