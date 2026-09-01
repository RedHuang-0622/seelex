// trace-view.js —— 按会话投影的 trace 视图（thin-wrapper mbd-models.md §3.5）：
//   - 数据源 T_i（会话级 telemetry span/事件，经 session_id 过滤），
//     不是 Π_traj（轨迹投影来自 View.Conversation）；
//   - 会话过滤（INV-T2）：查询只含该会话 span；
//   - 有界（T1.4/T3.8）：超限截断、空态文案，无 DOM 溢出。
export const TRACE_VIEW_LIMIT = 200;

export function projectTraceView(payload = {}, sessionID = "") {
  const events = Array.isArray(payload?.events) ? payload.events : [];
  // 未指定会话 → 空视图（T1.4：空/其它会话不命中，不把共享存储混入）。
  const own = sessionID
    ? events.filter((event) => sessionOf(event) === sessionID)
    : [];
  const bounded = own.slice(0, TRACE_VIEW_LIMIT);
  return {
    sessionID: sessionID || "",
    total: own.length,
    truncated: own.length > TRACE_VIEW_LIMIT,
    items: bounded.map(toTraceItem),
  };
}

export function renderTraceView(model) {
  if (!model || !Array.isArray(model.items) || model.items.length === 0) {
    return `<div class="trace-view trace-empty" data-trace-empty="1">暂无 trace 记录。该视图只展示当前会话（T_i）的遥测 span，数据源与轨迹视图（Π_traj）分离。</div>`;
  }
  const rows = model.items
    .map((item) => {
      const statusClass = traceStatusClass(item.status);
      const duration = item.duration ? formatTraceDuration(item.duration) : "";
      const size = item.size > 0 ? formatTraceSize(item.size) : "";
      return `<div class="trace-row ${statusClass}" data-trace-key="${escapeTrace(item.key)}">
        <span class="trace-name">${escapeTrace(item.name)}</span>
        <span class="trace-status">${escapeTrace(item.status)}</span>
        <span class="trace-duration">${escapeTrace(duration)}</span>
        <span class="trace-size">${escapeTrace(size)}</span>
      </div>`;
    })
    .join("");
  const banner = model.truncated
    ? `<div class="trace-banner">仅显示前 ${TRACE_VIEW_LIMIT} 条，共 ${model.total} 条（有界截断，不溢出）</div>`
    : "";
  return `<div class="trace-view">${banner}<div class="trace-list">${rows}</div></div>`;
}

export function renderTraceViewEmpty() {
  return renderTraceView(null);
}

function toTraceItem(event) {
  const attributes = event?.attributes && typeof event.attributes === "object" ? event.attributes : {};
  return {
    key: event.trace_id || event.span_id || String(event.seq ?? 0),
    type: event.type || "event",
    name: event.name || event.type || "span",
    status: event.status || (event.phase === "before" ? "running" : "success"),
    duration: Number(event.duration_ns || attributes.duration_ns || 0),
    size: Number(event.total_chars || attributes.total_chars || 0),
  };
}

function sessionOf(event) {
  if (event?.session_id) return event.session_id;
  if (event?.attributes && typeof event.attributes === "object") {
    return event.attributes.session_id || "";
  }
  return "";
}

function traceStatusClass(status) {
  if (status === "error" || status === "failed") return "is-error";
  if (status === "running" || status === "pending") return "is-running";
  return "is-success";
}

function formatTraceDuration(duration) {
  const milliseconds = Number(duration) / 1e6;
  if (!Number.isFinite(milliseconds) || milliseconds <= 0) return "";
  return milliseconds >= 1000
    ? `${(milliseconds / 1000).toFixed(1)}s`
    : `${Math.round(milliseconds)}ms`;
}

function formatTraceSize(size) {
  const bytes = Number(size);
  if (!Number.isFinite(bytes) || bytes <= 0) return "";
  if (bytes < 1024) return `${bytes} B`;
  return `${(bytes / 1024).toFixed(bytes < 10240 ? 1 : 0)} KB`;
}

function escapeTrace(value = "") {
  return String(value).replace(/[&<>"']/g, (char) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#39;",
  })[char]);
}
