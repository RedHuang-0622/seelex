import { escapeHtml } from "./components.js";

// ── 定时周期任务面板（右侧栏）──────────────────────────────
// 数据源：snapshot.runtime.scheduled_tasks（权威 Snapshot / runtime.changed
// 增量投影，seelebridge 调度器状态变化时发布）。渲染只读展示，不维护本地
// 猜测状态；所有渲染文本 escape；命令键与提示词内容非 secret，可展示。
// 取消按钮以 data-sched-cancel 携带任务 ID（ID 是操作键，名称只展示）。

// scheduledTasksView 归一化任务列表（防御畸形载荷：非数组 → []；
// 缺 id/name 或非对象的条目丢弃）。
export function scheduledTasksView(items) {
  if (!Array.isArray(items)) return [];
  return items.filter(item => isScheduledTask(item) && typeof item?.id === "string" && typeof item?.name === "string");
}

function isScheduledTask(value) {
  return Boolean(value) && typeof value === "object";
}

// renderScheduledTasks 渲染任务列表 HTML（名称/类型/启用状态/下次运行/
// 上次结果/日志尾部/取消按钮；命令类型补白名单展示名）。
export function renderScheduledTasks(items, commands) {
  const list = scheduledTasksView(items);
  if (!list.length) {
    return '<span class="muted list-empty">暂无周期任务</span>';
  }
  const labelByKey = new Map((Array.isArray(commands) ? commands : []).map(command => [command.key, command.label]));
  return `<ul class="sched-list">${list.map(task => {
    const kind = task.kind === "prompt" ? "提示词" : "命令";
    const commandLabel = task.kind === "command" ? (labelByKey.get(task.command) || task.command || "") : "";
    return `<li class="sched-item" data-sched-id="${escapeHtml(task.id)}">
      <div class="sched-head">
        <strong title="${escapeHtml(task.name)}">${escapeHtml(task.name)}</strong>
        <span class="chip">${escapeHtml(kind)}</span>
        <span class="chip ${task.enabled ? "sched-chip-on" : "sched-chip-off"}">${task.enabled ? "已启用" : "已停用"}</span>
        <span class="sched-status is-${schedStatusClass(task)}">${escapeHtml(schedStatusText(task))}</span>
      </div>
      <div class="sched-meta">
        <span>每 ${formatInterval(task.interval_seconds)}</span>
        <span>下次 ${formatRunTime(task.next_run_at)}</span>
        <span>共 ${Number(task.run_count) || 0} 次</span>
      </div>
      ${task.kind === "command" && commandLabel ? `<div class="sched-detail" title="${escapeHtml(task.command)}">脚本 ${escapeHtml(commandLabel)}</div>` : ""}
      ${task.kind === "prompt" && task.prompt ? `<div class="sched-detail" title="${escapeHtml(task.prompt)}">${escapeHtml(task.prompt)}</div>` : ""}
      ${task.last_error ? `<div class="sched-result sched-error" title="${escapeHtml(task.last_error)}">${escapeHtml(task.last_error)}</div>` : ""}
      ${!task.last_error && task.last_result ? `<div class="sched-result" title="${escapeHtml(task.last_result)}">${escapeHtml(task.last_result)}</div>` : ""}
      ${Array.isArray(task.log_tail) && task.log_tail.length ? `<pre class="sched-log">${escapeHtml(task.log_tail.join("\n"))}</pre>` : ""}
      <div class="sched-actions">
        <button type="button" class="text-button sched-cancel" data-sched-cancel="${escapeHtml(task.id)}">取消</button>
      </div>
    </li>`;
  }).join("")}</ul>`;
}

// schedStatusText 状态文案（权威 JSON 的 running/last_status 驱动）。
function schedStatusText(task) {
  if (task.running) return "运行中";
  switch (task.last_status) {
    case "ok": return "上次成功";
    case "failed": return "上次失败";
    case "running": return "运行中";
    case "skipped": return "上次跳过";
    default: return "待运行";
  }
}

// schedStatusClass 状态样式类（running/failed/ok/off，其余待运行）。
function schedStatusClass(task) {
  if (task.running) return "running";
  if (task.last_status === "failed") return "failed";
  if (task.last_status === "ok") return "ok";
  return "pending";
}

// formatInterval 周期文案：秒 → 分/小时/天。
function formatInterval(seconds) {
  const value = Number(seconds) || 0;
  if (value % 86400 === 0 && value > 0) return `${value / 86400} 天`;
  if (value % 3600 === 0 && value > 0) return `${value / 3600} 小时`;
  if (value % 60 === 0 && value > 0) return `${value / 60} 分钟`;
  return `${value} 秒`;
}

// formatRunTime 运行时间文案（RFC3339 → 本地短格式；空 = "—"）。
function formatRunTime(value) {
  if (!value) return "—";
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return "—";
  return parsed.toLocaleString([], { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}
