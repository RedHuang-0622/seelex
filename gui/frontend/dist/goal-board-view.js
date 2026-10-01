import { escapeHtml } from "./components.js";
import { renderGoalInFlight, renderGoalStack } from "./goal-stack-view.js";

// goal-board-view.js 是「目标」面板的**面板件 / 看板件 / 详情件**三个纯渲染件（不碰
// DOM，便于 node:test）。数据源只有一个：后端只读投影
// `snapshot.runtime.goal_governance`（+ 会话里最近一条用户输入，本地派生）。
//
//  1. renderGoalPanel：工作台「目标」子页那一块面板，也是**标签的状态机**所在处。
//     状态由 goal 自己给：栈上还有 active 帧 = 目标在跑 →
//       面板可见 + GOAL 徽标亮 + goal 域 skill chips + 看板 + 治理块；
//     目标结束（收口 / 归档，栈上没有 active 帧）→
//       整块退场（hidden、徽标隐藏、正文空串）：**结束就是没有了，不留空壳**。
//     标签的判据**不能**是 skill 激活态：`$goal`/`$teamwork` 一召回就长期为真，
//     拿它当判据就是"goal 已经结束了、GOAL 徽标与 chips 还贴着"（2026-10-02 现场）。
//  2. renderGoalBoard：面板里的看板——**上面**是大的 active seq
//     （当前目标在本会话 goal 序列里的序号），**下面**是最近一次用户输入的小字。
//     栈上没有 active 帧（目标结束 / 收口）时返回 ""：结束就是没有了，不留空壳。
//  3. renderGoalDetail：点开看板的弹窗内容，像资源管理器的「内容详情」——一张属性表
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

// renderGoalPanel 渲染「目标」面板**这一块现在长什么样**：可见性 + GOAL 徽标 +
// 面板正文。它是标签的**状态机**所在处——面板与它的标签共用一个状态：
//
//   live  （栈上还有 active 帧 = 目标在跑）→ 面板可见，GOAL 徽标亮，正文 = 看板 +
//          治理块 + goal 域 skill chips；
//   !live （没开 goal / 目标已收口归档）  → 面板隐藏，徽标隐藏，正文空串。
//
// 判据只能来自 goal 自己（governance 的 active 帧），**不能**来自 skill 激活态：
// `$goal`/`$teamwork` 一召回就长期为真（见本文件头注释），拿它当判据就会让标签在
// 目标结束后继续贴着。
export function renderGoalPanel({ governance = null, goalText = "", activeSkills = [] } = {}) {
  if (!activeGoalFrame(governance)) {
    return { live: false, hidden: true, badgeHidden: true, badgeTitle: "", html: "" };
  }
  const skills = Array.isArray(activeSkills) ? activeSkills : [];
  const chips = skills.length
    ? `<div class="goal-skills">${skills.map(skill => `<span class="chip">$${escapeHtml(skill)}</span>`).join("")}</div>`
    : "";
  return {
    live: true,
    hidden: false,
    badgeHidden: false,
    badgeTitle: "目标进行中",
    html: `${renderGoalBoard(governance, goalText)}${renderGoalGovernance(governance)}${chips}`,
  };
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

// renderGoalGovernance 渲染「目标」面板的治理只读块：goal 状态 / TL 最近指令 /
// 进行中正文（governance 视图来自 runtime.goal_governance，goal 栈不入模型上下文）。
//
// 席位轮转退场后（2026-10-01 阶段三 W3）面板不再有"轮次 / 座次 / 断环 / 治理未
// 完成"这些循环概念：goal 的驱动是提示词驱动的 leader 派活，终态由 gate 判。
//
// 2026-10-02 用户裁决：**评审过程**（round_steps 时间线）从这里退场——它此前只要
// goal 栈上有 active 帧就常驻可见，而用户口径是"不要挂在 goal 是否存活下面"。
// 裁决是去掉，不迁移落点、不新建确认面；后端的 round_steps 投影仍在（前端不再消费）。
//
// 面板上**没有墙钟推断**：只说后端给的事实。
export function renderGoalGovernance(governance) {
  const status = escapeHtml(governance.status || "active");
  const peer = governance.peer_state ? escapeHtml(governance.peer_state) : "";
  const directive = governance.last_directive
    ? `<div class="goal-gov-directive" title="${escapeHtml(governance.last_directive)}">TL: ${escapeHtml(truncate(governance.last_directive, 160))}</div>`
    : "";
  // 进行中的 ADVISOR 正文（只读快照）：评审期间有，回合结束即清空。
  const inFlight = renderGoalInFlight(governance);
  const meta = [
    `<span class="goal-gov-status">${status}</span>`,
    peer ? `peer ${peer}` : "",
  ].filter(Boolean).join(" · ");
  return `<div class="goal-governance"><div class="goal-gov-meta">${meta}</div>${inFlight}${directive}</div>`;
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
