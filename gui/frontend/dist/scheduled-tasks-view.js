import { escapeHtml } from "./components.js";

// ── 新建定时任务弹窗的载荷契约 ─────────────────────────────
//
// 为什么载荷形状要在这里（而不是 app.js 里就地拼一个对象）：Wails 的绑定层
// 用 encoding/json 反序列化参数（vendor/.../frontend/dispatcher/calls.go →
// BoundMethod.ParseArgs）。DTO 的电线字段里有 time.Time（runAt），而
// time.Time 收到**空串**会当场报
//   error parsing arguments: parsing time "" as "2006-01-02T15:04:05Z07:00"
// ——Go 侧连一行都执行不到，用户只看到一句解析错误（2026-10-07 现场）。
// 周期模式的 runAt 必须是 null（JSON null 对 time.Time 是 no-op），
// 这条口径由 buildScheduledTaskSpec 一处给出，node 用例 + Go 侧
// 反序列化用例两头钉住。

// 周期单位与锚点单位（与后端 dto.PeriodUnit / validatePeriod 同一份口径：
// minute/hour 是子日周期，不接受"几点开始"）。
const SCHED_PERIOD_UNITS = new Set(["minute", "hour", "day", "week", "month"]);
const SCHED_ANCHOR_UNITS = new Set(["day", "week", "month"]);
const SCHED_CLOCK_PATTERN = /^([01]\d|2[0-3]):[0-5]\d$/;

// buildScheduledTaskSpec 把弹窗字段组装成 Bridge.ScheduleTask 的入参。
// 返回 {spec} 或 {error}（error 是直接给用户看的短句）。
export function buildScheduledTaskSpec(fields) {
  const name = String(fields?.name ?? "").trim();
  if (!name) return { error: "请填写任务名称" };
  const prompt = String(fields?.prompt ?? "").trim();
  if (!prompt) return { error: "请填写提示词内容" };
  // 工作区可选：空 = 触发时新建的会话不绑项目。ID 是操作键，名称只做展示。
  const workspaceId = String(fields?.workspaceId ?? "").trim();
  // 会话绑定面板上不编辑：编辑既有任务时把原值原样带回（改个名字不该顺手
  // 把 API 侧设的绑定清掉）。新建路径这一格恒为空 = 默认新建会话。
  const sessionId = String(fields?.sessionId ?? "").trim();
  const now = Number.isFinite(Number(fields?.now)) ? Number(fields.now) : Date.now();

  if (fields?.mode === "at") {
    const raw = String(fields?.runAtValue ?? "").trim();
    if (!raw) return { error: "请选择定时执行时间" };
    const parsed = new Date(raw);
    if (Number.isNaN(parsed.getTime())) return { error: "定时时间格式无效" };
    if (parsed.getTime() <= now) return { error: "定时时间必须晚于当前时间" };
    return {
      spec: {
        name, kind: "prompt",
        interval: 0, periodUnit: "", periodValue: 0, startClock: "", startWeekday: 0,
        runAt: parsed.toISOString(), command: "", prompt, sessionId, workspaceId, enabled: true
      }
    };
  }

  const unit = SCHED_PERIOD_UNITS.has(fields?.periodUnit) ? fields.periodUnit : "";
  if (!unit) return { error: "请选择周期单位" };
  const value = Number(fields?.periodValue);
  if (!Number.isInteger(value) || value < 1) return { error: "周期数值至少为 1" };

  let startClock = "";
  let startWeekday = 0;
  if (SCHED_ANCHOR_UNITS.has(unit)) {
    const clock = String(fields?.startClock ?? "").trim();
    if (clock) {
      if (!SCHED_CLOCK_PATTERN.test(clock)) return { error: "开始时间要填 HH:MM" };
      startClock = clock;
      if (unit === "week") {
        const weekday = Number(fields?.startWeekday);
        startWeekday = weekday >= 1 && weekday <= 7 ? weekday : 1;
      }
    } else if (!fields?.anchorNow) {
      return { error: "请填写开始时间，或勾选「每个周期按当前时间」" };
    }
  }

  return {
    spec: {
      name, kind: "prompt",
      interval: periodToSeconds(unit, value) * 1e9,
      periodUnit: unit, periodValue: value,
      startClock, startWeekday,
      runAt: null, // 见文件头：空串会让 Wails 的参数反序列化当场失败
      command: "", prompt, sessionId,
      workspaceId,
      enabled: Boolean(fields?.enabled)
    }
  };
}

// scheduledTaskFormFields 把一条任务快照还原成新建/编辑弹窗的字段值——编辑入口
// 唯一一处「任务快照 → 表单」的映射，node 用例钉得住（与 buildScheduledTaskSpec
// 的「表单 → 载荷」正好是来回两条腿，中间不再各写一份）。
//
// 快照读不到或字段缺失时给"安全默认"（空名/空提示词/无工作区/未勾选），而不是
// 抛错：编辑一份畸形记录时该由提交时的校验给出可读错误，不该在打开弹窗时就炸。
export function scheduledTaskFormFields(task) {
  const value = task && typeof task === "object" ? task : {};
  const oneShot = Boolean(value.one_shot);
  const startClock = typeof value.start_clock === "string" ? value.start_clock.trim() : "";
  const period = periodFields(value);
  return {
    name: typeof value.name === "string" ? value.name : "",
    prompt: typeof value.prompt === "string" ? value.prompt : "",
    mode: oneShot ? "at" : "period",
    periodValue: String(period.value),
    periodUnit: period.unit,
    startClock,
    startWeekday: Number.isInteger(value.start_weekday) && value.start_weekday >= 1 && value.start_weekday <= 7
      ? String(value.start_weekday)
      : "1",
    // 没给墙钟锚点的周期 = 「每个周期按当前时间」（子日周期不看这一格）。
    anchorNow: !startClock,
    runAtValue: formatDateTimeLocal(value.run_at),
    enabled: value.enabled !== false,
    workspaceId: typeof value.workspace_id === "string" ? value.workspace_id.trim() : "",
    sessionId: typeof value.session_id === "string" ? value.session_id.trim() : ""
  };
}

// periodFields 取任务的周期（值 + 单位）：period_unit 齐就用它；只有 interval_seconds
// 的旧任务按最大可整除单位回推（都不整除时落到分钟并取整——面板只有这几种单位，
// 取整后的值会显示在表单里，用户点保存前看得见）。
function periodFields(task) {
  if (SCHED_PERIOD_UNITS.has(task?.period_unit) && Number(task.period_value) >= 1) {
    return { unit: task.period_unit, value: Number(task.period_value) };
  }
  const seconds = Number(task?.interval_seconds);
  if (!Number.isFinite(seconds) || seconds <= 0) {
    return { unit: "day", value: 1 };
  }
  const candidates = [["day", 86400], ["hour", 3600], ["minute", 60]];
  for (const [unit, size] of candidates) {
    if (seconds % size === 0) return { unit, value: seconds / size };
  }
  return { unit: "minute", value: Math.max(1, Math.round(seconds / 60)) };
}

// formatDateTimeLocal 把 RFC3339 时刻转成 <input type="datetime-local"> 要的本地
// 墙钟（YYYY-MM-DDTHH:MM）；空值/解析不了返回空串（控件留空，提交时给可读提示）。
function formatDateTimeLocal(value) {
  if (!value) return "";
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return "";
  const pad = number => String(number).padStart(2, "0");
  return `${parsed.getFullYear()}-${pad(parsed.getMonth() + 1)}-${pad(parsed.getDate())}T${pad(parsed.getHours())}:${pad(parsed.getMinutes())}`;
}

// periodToSeconds 周期单位 → 等价秒（month 用 30 天名义值，仅用于 interval
// 字段与后端最小周期校验；真实排期由调度器按日历推进）。
function periodToSeconds(unit, value) {
  switch (unit) {
    case "minute": return value * 60;
    case "day": return value * 86400;
    case "week": return value * 604800;
    case "month": return value * 2592000;
    case "hour":
    default: return value * 3600;
  }
}

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

// renderScheduledTasks 渲染任务列表 HTML（名称/类型/工作区/启用状态/下次运行/
// 上次结果/日志尾部/取消按钮；命令类型补白名单展示名）。
// workspaces 是快照里的工作区表：任务只记 workspace_id，名字只做展示。
export function renderScheduledTasks(items, commands, workspaces) {
  const list = scheduledTasksView(items);
  if (!list.length) {
    return '<span class="muted list-empty">暂无定时任务</span>';
  }
  const labelByKey = new Map((Array.isArray(commands) ? commands : []).map(command => [command.key, command.label]));
  const workspaceLabelByID = workspaceLabels(workspaces);
  return `<ul class="sched-list">${list.map(task => {
    const kind = task.kind === "prompt" ? "提示词" : "命令";
    const commandLabel = task.kind === "command" ? (labelByKey.get(task.command) || task.command || "") : "";
    const scheduleText = task.one_shot ? `定时 ${formatRunTime(task.run_at)}` : `每 ${formatInterval(task)}`;
    const workspace = workspaceChip(task, workspaceLabelByID);
    return `<li class="sched-item" data-sched-id="${escapeHtml(task.id)}">
      <div class="sched-head">
        <strong title="${escapeHtml(task.name)}">${escapeHtml(task.name)}</strong>
        <span class="chip">${escapeHtml(kind)}</span>
        ${workspace}
        ${task.one_shot ? '<span class="chip">一次性</span>' : ""}
        <span class="chip ${task.enabled ? "sched-chip-on" : "sched-chip-off"}">${task.enabled ? "已启用" : "已停用"}</span>
        <span class="sched-status is-${schedStatusClass(task)}">${escapeHtml(schedStatusText(task))}</span>
      </div>
      <div class="sched-meta">
        <span>${scheduleText}</span>
        <span>下次 ${formatRunTime(task.next_run_at)}</span>
        <span>共 ${Number(task.run_count) || 0} 次</span>
      </div>
      ${task.kind === "command" && commandLabel ? `<div class="sched-detail" title="${escapeHtml(task.command)}">脚本 ${escapeHtml(commandLabel)}</div>` : ""}
      ${task.kind === "prompt" && task.prompt ? `<div class="sched-detail" title="${escapeHtml(task.prompt)}">${escapeHtml(task.prompt)}</div>` : ""}
      ${task.last_error ? `<div class="sched-result sched-error" title="${escapeHtml(task.last_error)}">${escapeHtml(task.last_error)}</div>` : ""}
      ${!task.last_error && task.last_result ? `<div class="sched-result" title="${escapeHtml(task.last_result)}">${escapeHtml(task.last_result)}</div>` : ""}
      ${Array.isArray(task.log_tail) && task.log_tail.length ? `<pre class="sched-log">${escapeHtml(task.log_tail.join("\n"))}</pre>` : ""}
      <div class="sched-actions">
        <button type="button" class="text-button sched-edit" data-sched-edit="${escapeHtml(task.id)}">编辑</button>
        <button type="button" class="text-button sched-cancel" data-sched-cancel="${escapeHtml(task.id)}">取消</button>
      </div>
    </li>`;
  }).join("")}</ul>`;
}

// renderScheduledTasksTable 渲染定时任务 Excel 表格（弹窗内展示；列：
// 名称/类型/周期/下次运行/状态/操作；取消按钮以 data-sched-cancel 携带
// 任务 ID——ID 是操作键，名称只展示）。
//
// 整块的名字由头带那一条给出（<strong>定时任务</strong> N 项，坐在 --surface-2
// 的浅色带上）：弹窗头不再重复标题（口径同工作表格弹窗），所以名字必须留在表内。
// 头带与表体之间只有 .sched-table-scroll 一个滚动容器——弹窗纵向只此一层可滚。
export function renderScheduledTasksTable(items, commands, workspaces) {
  const list = scheduledTasksView(items);
  const labelByKey = new Map((Array.isArray(commands) ? commands : []).map(command => [command.key, command.label]));
  const workspaceLabelByID = workspaceLabels(workspaces);
  const rows = list.map(task => {
    const kind = task.kind === "prompt" ? "提示词" : "命令";
    const commandLabel = task.kind === "command" ? (labelByKey.get(task.command) || task.command || "") : "";
    const scheduleText = task.one_shot ? `定时 ${formatRunTime(task.run_at)}` : `每 ${formatInterval(task)}`;
    const workspace = workspaceChip(task, workspaceLabelByID);
    const statusClass = schedStatusClass(task);
    return `<tr class="sched-row is-${statusClass}" data-sched-id="${escapeHtml(task.id)}">
      <td class="work-cell work-cell-task" title="${escapeHtml(task.name)}">${escapeHtml(task.name)}</td>
      <td class="work-cell">${escapeHtml(kind)}${workspace}${task.one_shot ? '<span class="chip">一次性</span>' : ""}</td>
      <td class="work-cell">${escapeHtml(scheduleText)}${task.kind === "command" && commandLabel ? `<small class="sched-table-command" title="${escapeHtml(task.command)}">${escapeHtml(commandLabel)}</small>` : ""}</td>
      <td class="work-cell">${escapeHtml(formatRunTime(task.next_run_at))}</td>
      <td class="work-cell"><span class="chip ${task.enabled ? "sched-chip-on" : "sched-chip-off"}">${task.enabled ? "已启用" : "已停用"}</span> <span class="sched-status is-${statusClass}">${escapeHtml(schedStatusText(task))}</span></td>
      <td class="work-cell work-cell-actions"><button type="button" class="text-button sched-edit" data-sched-edit="${escapeHtml(task.id)}">编辑</button> <button type="button" class="text-button sched-cancel" data-sched-cancel="${escapeHtml(task.id)}">取消</button></td>
    </tr>`;
  }).join("");
  const body = list.length ? `<table class="excel-grid scheduled-table" data-scheduled-table>
    <thead><tr class="excel-head-row">
      <th>名称</th><th>类型</th><th>周期</th><th>下次运行</th><th>状态</th><th>操作</th>
    </tr></thead>
    <tbody>${rows}</tbody>
  </table>` : '<span class="muted list-empty">暂无定时任务</span>';
  return `<div class="sched-table" data-sched-table>
    <header class="sched-table-band">
      <strong>定时任务</strong>
      <span class="sched-table-total">${list.length} 项</span>
    </header>
    <div class="sched-table-scroll" data-sched-table-scroll>${body}</div>
  </div>`;
}

// workspaceLabels 构造 workspace_id → 展示名（无工作区表时退回 ID 本身）。
function workspaceLabels(workspaces) {
  const map = new Map();
  for (const workspace of Array.isArray(workspaces) ? workspaces : []) {
    if (workspace?.id) map.set(String(workspace.id), String(workspace.name || workspace.id));
  }
  return map;
}

// workspaceChip 渲染工作区 chip（没绑工作区的任务不显示这一格——"没有"不该
// 占一个空 chip）。名称取不到时退回 ID：ID 是索引，展示层宁可显示 ID 也不丢信息。
function workspaceChip(task, workspaceLabelByID) {
  const workspaceID = typeof task?.workspace_id === "string" ? task.workspace_id.trim() : "";
  if (!workspaceID) return "";
  const label = workspaceLabelByID.get(workspaceID) || workspaceID;
  return `<span class="chip sched-chip-workspace" title="${escapeHtml(workspaceID)}">${escapeHtml(label)}</span>`;
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

// formatInterval 周期文案：优先 period_unit/period_value（每 n 分钟/小时/天/
// 周/月），旧任务回退到 interval_seconds（秒 → 分/小时/天）。给了锚点的任务
// 补上「几点开始 / 周几几点开始」，没给的写明「按创建时间」——面板上要能看出
// 这个周期是从哪儿起算的（2026-10-07 用户口径：没说清就得勾"每个周期按当前时间"）。
function formatInterval(task) {
  if (task?.period_unit && Number(task.period_value) > 0) {
    return `${Number(task.period_value)} ${periodUnitLabel(task.period_unit)}${formatAnchor(task)}`;
  }
  const value = Number(task?.interval_seconds) || 0;
  if (value % 86400 === 0 && value > 0) return `${value / 86400} 天${formatAnchor(task)}`;
  if (value % 3600 === 0 && value > 0) return `${value / 3600} 小时`;
  if (value % 60 === 0 && value > 0) return `${value / 60} 分钟`;
  return `${value} 秒`;
}

// formatAnchor 锚点文案（空锚点 = 每个周期按创建时刻滚动）。
function formatAnchor(task) {
  const clock = typeof task?.start_clock === "string" ? task.start_clock.trim() : "";
  if (!clock) return "（按创建时间）";
  const weekday = Number(task?.start_weekday);
  if (task?.period_unit === "week" && weekday >= 1 && weekday <= 7) {
    return ` ${weekdayLabel(weekday)} ${clock}`;
  }
  return ` ${clock}`;
}

function weekdayLabel(weekday) {
  return `周${"一二三四五六日"[weekday - 1] || ""}`;
}

function periodUnitLabel(unit) {
  switch (unit) {
    case "minute": return "分钟";
    case "hour": return "小时";
    case "day": return "天";
    case "week": return "周";
    case "month": return "月";
    default: return String(unit || "");
  }
}

// formatRunTime 运行时间文案（RFC3339 → 本地短格式；空 = "—"）。
function formatRunTime(value) {
  if (!value) return "—";
  const parsed = new Date(value);
  const wall = /^\d{4}-(\d{2})-(\d{2})T(\d{2}):(\d{2})/.exec(value);
  if (wall) return `${wall[1]}-${wall[2]} ${wall[3]}:${wall[4]}`;
  if (Number.isNaN(parsed.getTime())) return "—";
  return parsed.toLocaleString([], { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });
}
