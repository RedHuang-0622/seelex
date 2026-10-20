import { escapeHtml } from "./components.js";
import { workItemStatusLabel, workItemStatusToken, workTableView } from "./work-table.js";

const TODO_NEXT = { pending: "doing", doing: "done", done: "pending" };

// renderWorkbenchSummary 把工作表格投影压成右侧栏可读的三块：
// Todo 是纯打点表；Task 与 Subagent 是后台长任务，共用一套“状态轨 + 目标 + 打点”语言。
// 三块的数据都来自 workTableView，不另造归一化或状态词表。
export function renderWorkbenchSummary(items, options = {}) {
  const rows = workTableView(items);
  const viewSessionID = String(options.viewSessionID || "");
  const scoped = viewSessionID
    ? rows.filter(row => !row.session_id || row.session_id === viewSessionID)
    : rows;
  const todos = scoped.filter(row => row.kind === "todo");
  const tasks = scoped.filter(row => row.kind === "task");
  const subagents = scoped.filter(row => row.kind === "subagent");
  return `<div class="workbench-summary-board" data-workbench-summary>
    ${renderTodoSection(todos)}
    ${tasks.length ? renderLongRunSection("Task", "后台任务", tasks) : ""}
    ${subagents.length ? renderLongRunSection("Subagent", "后台子代理", subagents) : ""}
  </div>`;
}

function renderTodoSection(rows) {
  const open = rows.filter(row => workItemStatusToken(row.status) !== "done").length;
  const body = rows.length
    ? rows.map(row => {
      const status = workItemStatusToken(row.status);
      const traces = Array.isArray(row.trace) ? row.trace : [];
      return `<tr class="workbench-todo-row is-${escapeHtml(status)}" data-workbench-row="${escapeHtml(row.id)}">
        <td class="workbench-todo-state-cell">
          <button type="button" class="workbench-todo-state is-${escapeHtml(status)}" data-workbench-status="${escapeHtml(row.id)}" data-status="${escapeHtml(TODO_NEXT[status] || "doing")}" title="切换待办状态">
            <span class="workbench-todo-dot" aria-hidden="true"></span><span>${escapeHtml(workItemStatusLabel(status))}</span>
          </button>
        </td>
        <td class="workbench-todo-title" title="${escapeHtml(row.task)}">${escapeHtml(shorten(row.task, 100))}</td>
        <td class="workbench-todo-traces">${renderTraceDots(traces)}</td>
      </tr>`;
    }).join("")
    : '<tr class="workbench-todo-empty-row"><td colspan="3">暂时没有 todo 项</td></tr>';
  return `<section class="workbench-block workbench-todo-block" data-workbench-section="todo">
    <header class="workbench-block-head"><div><span class="workbench-kicker">TODO</span><h3>待办打点</h3></div><span class="workbench-block-count">${open}/${rows.length} 未完成</span></header>
    <table class="workbench-todo-table" aria-label="Todo 打点表">
      <thead><tr><th>状态</th><th>待办</th><th>打点</th></tr></thead>
      <tbody>${body}</tbody>
    </table>
  </section>`;
}

function renderLongRunSection(kind, label, rows) {
  return `<section class="workbench-block workbench-run-block" data-workbench-section="${kind.toLowerCase()}">
    <header class="workbench-block-head"><div><span class="workbench-kicker">${escapeHtml(kind.toUpperCase())}</span><h3>${escapeHtml(label)}</h3></div><span class="workbench-block-count">${rows.length} 项</span></header>
    <ol class="workbench-run-list">${rows.map(renderLongRunRow).join("")}</ol>
  </section>`;
}

function renderLongRunRow(row) {
  const status = workItemStatusToken(row.status);
  const traces = Array.isArray(row.trace) ? row.trace : [];
  const assignee = row.assignee ? `<span>${escapeHtml(row.assignee)}</span>` : "";
  const elapsed = row.elapsed ? `<span>${escapeHtml(row.elapsed)}</span>` : "";
  return `<li class="workbench-run-row is-${escapeHtml(status)}" data-workbench-row="${escapeHtml(row.id)}">
    <span class="workbench-run-rail" aria-hidden="true"><i></i></span>
    <button type="button" class="workbench-run-main" data-workbench-detail="${escapeHtml(row.source_id || row.id)}" title="查看任务详情">
      <span class="workbench-run-head"><strong>${escapeHtml(shorten(row.task || row.description || row.id, 110))}</strong><em>${escapeHtml(workItemStatusLabel(status))}</em></span>
      <span class="workbench-run-meta"><b>${escapeHtml(row.kind)}</b>${assignee}${elapsed}${traces.length ? `<span>${traces.length} 打点</span>` : ""}</span>
      ${row.description ? `<span class="workbench-run-note">${escapeHtml(shorten(row.description, 160))}</span>` : ""}
      ${traces.length ? `<span class="workbench-run-trace">${renderTraceDots(traces)}</span>` : ""}
    </button>
  </li>`;
}

function renderTraceDots(traces) {
  const points = (Array.isArray(traces) ? traces : []).slice(-5);
  if (!points.length) return '<span class="workbench-no-traces">—</span>';
  return `<span class="workbench-trace-dots" title="${escapeHtml(points.map(point => `${point.operation || point.status || "打点"}${point.at ? ` · ${point.at}` : ""}`).join("\n"))}">${points.map(point => {
    const status = workItemStatusToken(point.status);
    return `<i class="is-${escapeHtml(status)}" aria-label="${escapeHtml(workItemStatusLabel(status))}"></i>`;
  }).join("")}<small>${traces.length}</small></span>`;
}

export function createWorkbenchSummaryView(container, options = {}) {
  let items = [];

  function render(nextItems = items) {
    items = workTableView(nextItems);
    const sessionID = typeof options.viewSessionID === "function" ? options.viewSessionID() : "";
    container.classList.remove("muted");
    container.innerHTML = renderWorkbenchSummary(items, { viewSessionID: sessionID });
    hydrateTraceNumbers(container);
  }

  container.addEventListener("click", event => {
    const status = event.target.closest?.("[data-workbench-status]");
    if (status?.dataset.workbenchStatus) {
      options.onStatus?.(status.dataset.workbenchStatus, status.dataset.status || "doing");
      return;
    }
    const detail = event.target.closest?.("[data-workbench-detail]");
    if (detail?.dataset.workbenchDetail) options.onDetail?.(detail.dataset.workbenchDetail);
  });

  return { render, current: () => items };
}

function hydrateTraceNumbers() {
  // 打点数量是静态文本；保留钩子便于以后接入与工作表相同的数字滚动而不改 API。
}

function shorten(value, limit) {
  const text = String(value || "").replace(/\s+/g, " ").trim();
  if (text.length <= limit) return text;
  return `${text.slice(0, Math.max(limit - 1, 0))}…`;
}
