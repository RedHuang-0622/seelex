import { escapeHtml } from "./components.js";

// goal-stack-view.js 是「目标」面板的两个纯渲染件（不碰 DOM，便于 node:test）：
//
//  1. renderGoalStack：把 goal **活动栈**渲染成一张**专用表格**（excel-grid 口径，
//     与工作表格/定时任务表同一套表样式）：一行一帧，栈底→栈顶，栈顶 = 当前目标，
//     栈下 = 被嵌套压栈而暂停的目标。会话里有活动栈时，工作台据此逐帧查看，
//     而不是只看栈顶一帧；
//  2. renderGoalInFlight：ADVISOR 回合**进行中**的正文近端（只读快照）。治理回合
//     是同步跑完的，回合结束才推一次状态，所以进行中正文必须靠这个快照渲染出来，
//     否则用户看不到评审在写什么（渲染不及时）。
//
// 数据来源都是后端只读投影（snapshot.runtime.goal_governance.stack / .in_flight）。
// 这里**只渲染**：前端没有任何写 goal 状态或 team work 前缀的入口——把渲染结果
// 回写会让后端真值变成前端派生物，前缀随即与后端帧账本不一致。
const FRAME_STATEMENT_LIMIT = 200;
const FRAME_PROGRESS_LIMIT = 120;
const IN_FLIGHT_LIMIT = 200;

// renderGoalStack 把活动栈渲染成一张按帧分行的表；空栈返回 ""（面板不显示空壳）。
export function renderGoalStack(stack) {
  if (!Array.isArray(stack) || stack.length === 0) return "";
  const frames = stack
    .map((frame, index) => renderGoalFrame(frame, index === stack.length - 1, index + 1))
    .join("");
  return `<table class="excel-grid goal-stack-table" data-goal-stack-table data-goal-frames="${stack.length}">
      <thead><tr class="excel-head-row">
        <th>帧</th><th>状态</th><th>目标</th><th>陈述</th><th>验收</th><th>最近进度</th><th>更新</th>
      </tr></thead>
      <tbody>${frames}</tbody>
    </table>`;
}

// renderGoalFrame 渲染一帧（表格的一行）。frameIndex 是它在栈里的序号（1 = 栈底）；
// isTop：栈顶帧即使后端没标 active，也按当前目标渲染（顺序事实在栈本身）。
export function renderGoalFrame(frame, isTop = false, frameIndex = 1) {
  if (!frame || typeof frame !== "object") return "";
  const active = Boolean(frame.active) || isTop;
  const status = String(frame.status || (active ? "active" : "paused"));
  const title = frame.title || frame.id || "未命名目标";
  const statement = frame.statement || "";
  const progress = Array.isArray(frame.progress) ? frame.progress : [];
  const acceptance = Array.isArray(frame.acceptance) ? frame.acceptance.length : 0;
  return `<tr class="goal-frame${active ? " is-active" : ""}" data-goal-frame="${frameIndex}" data-goal-frame-id="${escapeHtml(frame.id || "")}" data-goal-status="${escapeHtml(status)}"${active ? ' data-goal-frame-active="true"' : ""}>
        <td class="goal-cell goal-cell-index">${frameIndex}</td>
        <td class="goal-cell goal-cell-status"><span class="chip goal-frame-status is-${escapeHtml(status)}">${escapeHtml(status.toUpperCase())}</span></td>
        <td class="goal-cell goal-cell-title" title="${escapeHtml(title)}">${escapeHtml(title)}</td>
        <td class="goal-cell goal-cell-statement" title="${escapeHtml(statement)}">${statement ? escapeHtml(truncate(statement, FRAME_STATEMENT_LIMIT)) : "—"}</td>
        <td class="goal-cell goal-cell-acceptance">${acceptance ? `验收 ${acceptance} 条` : "—"}</td>
        <td class="goal-cell goal-cell-progress">${progress.length ? progress.map(item => `· ${escapeHtml(truncate(item?.content || "", FRAME_PROGRESS_LIMIT))}`).join("<br>") : "—"}</td>
        <td class="goal-cell goal-cell-updated muted">${formatGoalTime(frame.updated_at)}</td>
      </tr>`;
}

// renderGoalInFlight 渲染"评审中"的正文近端；空 = 当前没有进行中的 b 回合。
export function renderGoalInFlight(governance) {
  const text = typeof governance?.in_flight === "string" ? governance.in_flight.trim() : "";
  if (!text) return "";
  const chars = Number(governance?.in_flight_chars || 0);
  const counter = chars > text.length ? `（近端 ${text.length}/${chars} 字）` : "";
  return `<div class="goal-gov-inflight" title="${escapeHtml(text)}">ADVISOR 评审中${escapeHtml(counter)}: ${escapeHtml(truncate(text, IN_FLIGHT_LIMIT))}</div>`;
}

// formatGoalTime 容忍秒/毫秒两种时间戳口径：解析不出来就显示 "—"，
// 不把渲染层的猜测当成事实写进面板。
function formatGoalTime(at) {
  const value = Number(at || 0);
  if (!Number.isFinite(value) || value <= 0) return "—";
  const ms = value < 1e12 ? value * 1000 : value;
  const date = new Date(ms);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleTimeString("zh-CN", { hour12: false });
}

function truncate(text, max) {
  const value = String(text ?? "");
  return value.length <= max ? value : `${value.slice(0, max)}…`;
}
