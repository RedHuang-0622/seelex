import { escapeHtml } from "./components.js";
import { renderGoalStack } from "./goal-stack-view.js";

// goal-board-view.js 是「目标」面板的**看板件**与**详情件**两个纯渲染件（不碰 DOM，
// 便于 node:test）。数据源只有一个：后端只读投影
// `snapshot.runtime.goal_governance`（+ 会话里最近一条用户输入，本地派生）。
//
//  1. renderGoalBoard：工作台「目标」子页那一块看板——**上面**是大的 active seq
//     （当前目标在本会话 goal 序列里的序号），**下面**是最近一次用户输入的小字。
//     栈上没有 active 帧（目标结束 / 收口）时返回 ""：结束就是没有了，不留空壳。
//  2. renderGoalDetail：点开看板的弹窗内容，像资源管理器的「内容详情」——一张属性表
//     逐项列出这一帧的全部内容（序号/状态/标题/正文/完成条件/非目标范围/打点流水/
//     时间戳），嵌套压栈时逐帧一节（栈顶 = 当前目标）。
//
// 这里**只渲染**：前端没有任何写 goal 状态的入口（把渲染结果回写会让后端真值变成
// 前端派生物）。
const BOARD_TITLE_LIMIT = 160;
const BOARD_TASK_LIMIT = 140;
const DETAIL_TEXT_LIMIT = 4000;

// activeGoalFrame 取看板要显示的那一帧：栈上最后一个 active 帧；后端没标 active 时
// 用栈顶（顺序事实在栈本身：末元素 = 当前目标）。没有帧 → null。
export function activeGoalFrame(governance) {
  const stack = Array.isArray(governance?.stack) ? governance.stack : [];
  for (let index = stack.length - 1; index >= 0; index -= 1) {
    const frame = stack[index];
    if (frame && typeof frame === "object" && frame.active) return frame;
  }
  const top = stack[stack.length - 1];
  return top && typeof top === "object" ? top : null;
}

// goalActiveSeq 取「active seq」：当前目标在本会话 goal 序列里的序号。
//
// 事实来源是 goal 记录自己的 id（`g-<n>`，由 Controller 的会话级自增计数器分配），
// 因此它**不是**对用户消息的编号，也不是打点条数：它就是"这是本会话第几个 goal"。
// id 解析不出来（老数据/自定义 id）时回退成它在栈里的位置（1 = 栈底）。
export function goalActiveSeq(frame, index = 0) {
  const id = String(frame?.id || "");
  const match = /^g-(\d+)$/.exec(id.trim());
  if (match) return Number(match[1]);
  return Number.isFinite(index) ? index + 1 : 0;
}

// renderGoalBoard 渲染看板块；无 active 帧 → ""（面板不显示空壳）。
//
// goalText = 会话里最近一条非空用户输入（小字那一行）。它可能为空（例如恢复出来的
// 会话里没有用户行），此时只显示 on-board 的大字序号与标题。
export function renderGoalBoard(governance, goalText = "") {
  const stack = Array.isArray(governance?.stack) ? governance.stack : [];
  const frame = activeGoalFrame(governance);
  if (!frame) return "";
  const seq = goalActiveSeq(frame, Math.max(0, stack.lastIndexOf(frame)));
  const title = String(frame.title || frame.id || "未命名目标");
  const status = String(frame.status || "active");
  const marks = Array.isArray(frame.progress_all) && frame.progress_all.length
    ? frame.progress_all.length
    : (Array.isArray(frame.progress) ? frame.progress.length : 0);
  const meta = [
    status.toUpperCase(),
    marks ? `打点 ${marks} 条` : "",
    formatGoalTime(frame.updated_at) !== "—" ? `更新 ${formatGoalTime(frame.updated_at)}` : "",
  ].filter(Boolean).join(" · ");
  const task = String(goalText || "").trim();
  return `<div class="goal-board" data-goal-board data-goal-seq="${escapeHtml(String(seq))}">
      <button type="button" class="goal-board-card" data-goal-board-open title="点开查看目标详情">
        <span class="goal-board-seq" title="active seq：当前目标在本会话 goal 序列里的序号">
          <span class="goal-board-seq-num">${escapeHtml(String(seq))}</span>
          <span class="goal-board-seq-unit">active seq</span>
        </span>
        <span class="goal-board-body">
          <span class="goal-board-title" title="${escapeHtml(title)}">${escapeHtml(truncate(title, BOARD_TITLE_LIMIT))}</span>
          <span class="goal-board-meta">${escapeHtml(meta)}</span>
        </span>
        <span class="goal-board-hint" aria-hidden="true">详情</span>
      </button>
      ${task ? `<div class="goal-board-task" title="${escapeHtml(task)}">${escapeHtml(truncate(task, BOARD_TASK_LIMIT))}</div>` : ""}
    </div>`;
}

// renderGoalDetail 渲染「点开看」的详情：一帧一节属性表（栈底→栈顶，栈顶标当前）。
// 嵌套压栈（栈深 > 1）时，最前面多一块**活动栈总览表**（一行一帧，截断口径）——
// 单帧时那张表只会是一行冗余，所以不渲染。无帧 → ""（弹窗不开，见 app.js）。
export function renderGoalDetail(governance, goalText = "") {
  const stack = Array.isArray(governance?.stack) ? governance.stack : [];
  if (stack.length === 0) return "";
  const task = String(goalText || "").trim();
  const overview = stack.length > 1
    ? `<section class="goal-detail-section"><div class="goal-detail-section-title">活动栈总览（${stack.length} 帧，栈底 → 栈顶）</div>${renderGoalStack(stack)}</section>`
    : "";
  const frames = stack
    .map((frame, index) => renderGoalFrameDetail(frame, index === stack.length - 1, index))
    .join("");
  const taskSection = task
    ? `<section class="goal-detail-section">
        <div class="goal-detail-section-title">我发出的最近一次任务</div>
        <div class="goal-detail-text">${escapeHtml(truncate(task, DETAIL_TEXT_LIMIT))}</div>
      </section>`
    : "";
  return `<div class="goal-detail" data-goal-detail data-goal-frames="${stack.length}">${overview}${frames}${taskSection}</div>`;
}

// renderGoalFrameDetail 渲染一帧的属性表。字段与顺序 = 资源管理器"内容详情"的口径：
// 有就列，没有就写 "—"，不猜、不省。
export function renderGoalFrameDetail(frame, isTop = false, index = 0) {
  if (!frame || typeof frame !== "object") return "";
  const active = Boolean(frame.active) || isTop;
  const status = String(frame.status || (active ? "active" : "paused"));
  const seq = goalActiveSeq(frame, index);
  const acceptance = Array.isArray(frame.acceptance) ? frame.acceptance : [];
  const outOfScope = Array.isArray(frame.out_of_scope) ? frame.out_of_scope : [];
  const marks = Array.isArray(frame.progress_all) && frame.progress_all.length
    ? frame.progress_all
    : (Array.isArray(frame.progress) ? frame.progress : []);
  const rows = [
    detailRow("序号", `${seq}（${frame.id || "—"}）`),
    detailRow("状态", `<span class="chip goal-frame-status is-${escapeHtml(status)}">${escapeHtml(status.toUpperCase())}</span>`),
    detailRow("标题", escapeHtml(String(frame.title || "—"))),
    detailRow("目标正文", textCell(frame.statement)),
    detailRow("完成条件", listCell(acceptance, "条完成条件")),
    detailRow("非目标范围", listCell(outOfScope, "条不进范围")),
    detailRow("打点流水", marksCell(marks)),
    detailRow("创建", escapeHtml(formatGoalTime(frame.created_at))),
    detailRow("更新", escapeHtml(formatGoalTime(frame.updated_at))),
  ].join("");
  return `<section class="goal-detail-frame${active ? " is-active" : ""}" data-goal-detail-frame="${escapeHtml(frame.id || "")}" data-goal-detail-status="${escapeHtml(status)}">
      <div class="goal-detail-frame-head"><span class="goal-detail-frame-title">${escapeHtml(String(frame.title || "未命名目标"))}</span>${active ? '<span class="chip is-active">当前目标</span>' : ""}</div>
      <table class="goal-detail-table"><tbody>${rows}</tbody></table>
    </section>`;
}

// detailRow 是一行"属性 → 值"。th 用 scope=row：这是属性表，不是数据表。
function detailRow(label, valueHtml) {
  return `<tr><th scope="row">${escapeHtml(label)}</th><td>${valueHtml}</td></tr>`;
}

function textCell(text) {
  const value = String(text || "").trim();
  return value ? escapeHtml(truncate(value, DETAIL_TEXT_LIMIT)) : '<span class="muted">—</span>';
}

function listCell(items, unit) {
  if (!Array.isArray(items) || items.length === 0) return '<span class="muted">—</span>';
  const rows = items.map(item => `<li>${escapeHtml(String(item))}</li>`).join("");
  return `<ol class="goal-detail-list" title="${escapeHtml(`${items.length} ${unit}`)}">${rows}</ol>`;
}

// marksCell 渲染完整打点流水：时间 + 类型 + 内容逐条一行。空流水写 "—"（不写"0 条"，
// 那是"什么都没发生"的另一种说法，属性表只报事实）。
function marksCell(marks) {
  if (!Array.isArray(marks) || marks.length === 0) return '<span class="muted">—</span>';
  const rows = marks.map((item, index) => {
    const kind = String(item?.kind || "");
    const at = formatGoalTime(item?.at);
    const label = kind ? `<span class="chip goal-mark-kind is-${escapeHtml(kind)}">${escapeHtml(kind)}</span>` : "";
    return `<li class="goal-detail-mark" data-goal-mark="${index + 1}">
        <span class="goal-detail-mark-time muted">${escapeHtml(at)}</span>${label}<span class="goal-detail-mark-text">${escapeHtml(String(item?.content || ""))}</span>
      </li>`;
  }).join("");
  return `<ol class="goal-detail-marks">${rows}</ol>`;
}

// formatGoalTime 容忍秒/毫秒两种时间戳口径：解析不出来就写 "—"，不把渲染层的
// 猜测当成事实。
export function formatGoalTime(at) {
  const value = Number(at || 0);
  if (!Number.isFinite(value) || value <= 0) return "—";
  const ms = value < 1e12 ? value * 1000 : value;
  const date = new Date(ms);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleString("zh-CN", { hour12: false });
}

function truncate(text, max) {
  const value = String(text ?? "");
  return value.length <= max ? value : `${value.slice(0, max)}…`;
}
