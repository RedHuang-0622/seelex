import { escapeHtml } from "./components.js";

// team-board-view.js 是「团队（Teamwork）」看板的**纯渲染件**（不碰 DOM，便于 node:test）。
//
// 口径（2026-10-03 · 里程碑 + Work Item）：看板画的是「里程碑 → 它名下的 Work Item」。
//   - Milestone 是**屏障**（里程碑之间串行，milestones[].depends_on）；
//   - 里程碑内部按工作项依赖并行（work_items[].depends_on）；
//   - **一个 Work Item 一个 teammate 一套 Session + worktree**；
//   - 甘特**不表示时间**，只表示依赖（里程碑屏障 + 工作项 DAG）。
//
// 数据源三份，全部是后端只读投影——前端**没有任何写入口**（渲染结果不回写后端）：
//   1. plan   计划本体：sessionstore.TeamworkPlan（team_id / version / members /
//              milestones / work_items）。**顺序的唯一事实是 depends_on**（里程碑之间看
//              milestones[].depends_on，里程碑内部看 work_items[].depends_on），本件不从
//              调用姿势猜顺序。
//   2. jobs   作业投影：与工作表格同源的作业行（handle / state / exit_code / bytes /
//              node / scope.subject）。注意 jobs I-4：**句柄只是投影**，进程重启后一律视为过期，
//              真值以 Observe 的返回为准。（本件只用它报头部计数，不按它分组。）
//   3. events 审计流水：sessionstore.TeamworkEvent[]（plan / dispatch / join / milestone / retire）。
//
// **没有阶段（stages）这个概念**（2026-10-04）：计划里只有里程碑 + 工作项，看板也只按
// 这两层画。老口径的计划即便在 JSON 里还带着 stages，它也不会被当成编排形状。

const TEXT_LIMIT = 160;
const CONTENT_LIMIT = 240;
const AUDIT_LIMIT = 6;

// ── 里程碑（屏障）与工作项（甘特节点）：纯函数 ────────────────────────

// milestonesOf 取里程碑数组；milestoneStatus 缺省 pending（空 = pending，见 sessionstore）。
export function milestonesOf(plan) {
  return Array.isArray(plan?.milestones) ? plan.milestones.filter(item => item && item.id) : [];
}

export function milestoneStatus(milestone) {
  const status = String(milestone?.status || "").trim().toLowerCase();
  return status || "pending";
}

// milestoneDepsOf 取里程碑之间的**屏障**依赖（去空白、去空项）。
export function milestoneDepsOf(milestone) {
  return (Array.isArray(milestone?.depends_on) ? milestone.depends_on : [])
    .map(dep => String(dep || "").trim())
    .filter(Boolean);
}

// milestoneAfterOf 取里程碑的**判据**（after；leader 写的验收边，与屏障是两回事）。
export function milestoneAfterOf(milestone) {
  return (Array.isArray(milestone?.after) ? milestone.after : [])
    .map(dep => String(dep || "").trim())
    .filter(Boolean);
}

// workItemsOf 取计划里的工作项（扁平数组，每条自带 milestone）。
export function workItemsOf(plan) {
  return Array.isArray(plan?.work_items) ? plan.work_items.filter(item => item && item.id) : [];
}

// itemStatus 读工作项状态（空 = pending，与 sessionstore 同口径）。
export function itemStatus(item) {
  return String(item?.status || "").trim().toLowerCase() || "pending";
}

// itemDepsOf 取工作项的**里程碑内**依赖（去空白、去空项）。
export function itemDepsOf(item) {
  return (Array.isArray(item?.depends_on) ? item.depends_on : [])
    .map(dep => String(dep || "").trim())
    .filter(Boolean);
}

// itemsOfMilestone 取一个里程碑名下的工作项（按后端给的顺序）。
export function itemsOfMilestone(plan, milestoneID) {
  const id = String(milestoneID || "");
  return workItemsOf(plan).filter(item => String(item.milestone || "") === id);
}

// orderMilestones 按 depends_on 屏障做拓扑排序 + 层号（层 = 最长依赖链上的位置）。
// 依赖指向不存在的里程碑时忽略该边；成环时把剩下的里程碑按原顺序接在末尾（层号归 0，
// cyclic=true）——宁可在看板上显形，也不静默丢里程碑。
export function orderMilestones(plan) {
  const milestones = milestonesOf(plan);
  const ids = new Set(milestones.map(milestone => String(milestone.id)));
  const deps = new Map(milestones.map(milestone => [String(milestone.id), milestoneDepsOf(milestone)]));
  const depth = new Map();
  const ordered = [];
  let frontier = milestones.filter(milestone => deps.get(String(milestone.id)).filter(dep => ids.has(dep)).length === 0);
  for (const milestone of frontier) depth.set(String(milestone.id), 0);
  while (frontier.length) {
    for (const milestone of frontier) ordered.push(milestoneEntry(plan, milestone, depth.get(String(milestone.id))));
    const next = [];
    for (const milestone of milestones) {
      const id = String(milestone.id);
      if (depth.has(id)) continue;
      const pending = deps.get(id).filter(dep => ids.has(dep) && !depth.has(dep));
      if (pending.length) continue;
      const level = Math.max(0, ...deps.get(id).filter(dep => ids.has(dep)).map(dep => (depth.get(dep) ?? 0) + 1));
      depth.set(id, level);
      next.push(milestone);
    }
    frontier = next;
  }
  for (const milestone of milestones) {
    if (!depth.has(String(milestone.id))) ordered.push(milestoneEntry(plan, milestone, 0, true));
  }
  return ordered;
}

function milestoneEntry(plan, milestone, depth, cyclic = false) {
  const id = String(milestone.id);
  const known = new Set(milestonesOf(plan).map(item => String(item.id)));
  return {
    id,
    name: String(milestone.name || "").trim(),
    depends_on: milestoneDepsOf(milestone),
    missing_deps: milestoneDepsOf(milestone).filter(dep => !known.has(dep)),
    after: milestoneAfterOf(milestone),
    content: String(milestone.content || "").trim(),
    status: milestoneStatus(milestone),
    depth,
    cyclic,
    items: itemsOfMilestone(plan, id),
  };
}

// orderWorkItems 按**里程碑内**依赖做拓扑排序 + 层号；blocked = 前置还没验收通过。
//
// blocked 不是"不能派"，而是"现在派会被闸门拒"——它与后端的判据同源（依赖必须 done），
// 于是看板说"被卡住"与编排面真的拒收是同一条事实，不会各说各话。
export function orderWorkItems(items) {
  const list = (Array.isArray(items) ? items : []).filter(item => item && item.id);
  const ids = new Set(list.map(item => String(item.id)));
  const byID = new Map(list.map(item => [String(item.id), item]));
  const depth = new Map();
  const ordered = [];
  let frontier = list.filter(item => itemDepsOf(item).filter(dep => ids.has(dep)).length === 0);
  for (const item of frontier) depth.set(String(item.id), 0);
  while (frontier.length) {
    for (const item of frontier) ordered.push(workItemEntry(item, byID, depth.get(String(item.id))));
    const next = [];
    for (const item of list) {
      const id = String(item.id);
      if (depth.has(id)) continue;
      const pending = itemDepsOf(item).filter(dep => ids.has(dep) && !depth.has(dep));
      if (pending.length) continue;
      const level = Math.max(0, ...itemDepsOf(item).filter(dep => ids.has(dep)).map(dep => (depth.get(dep) ?? 0) + 1));
      depth.set(id, level);
      next.push(item);
    }
    frontier = next;
  }
  for (const item of list) {
    if (!depth.has(String(item.id))) ordered.push(workItemEntry(item, byID, 0, true));
  }
  return ordered;
}

function workItemEntry(item, byID, depth, cyclic = false) {
  const deps = itemDepsOf(item);
  return {
    item,
    id: String(item.id),
    depth,
    cyclic,
    depends_on: deps,
    missing_deps: deps.filter(dep => !byID.has(dep)),
    blocked: deps.some(dep => itemStatus(byID.get(dep)) !== "done"),
  };
}

// memberQueueOf 取某位 teammate 的工作项名称队列：优先用后端算好的 queue，缺失时按
// 工作项自行派生（**同一条判据**：未完成的工作项才占队列）。两种来源读法一致，
// 所以后端降级（旧投影）时看板不会显示成"这个人无事可做"。
export function memberQueueOf(plan, role) {
  const name = String(role || "");
  const member = (Array.isArray(plan?.members) ? plan.members : []).find(entry => entry && String(entry.role || "") === name);
  if (member && Array.isArray(member.queue)) return member.queue.map(entry => String(entry || "")).filter(Boolean);
  return workItemsOf(plan)
    .filter(item => String(item.role || "") === name && itemStatus(item) !== "done")
    .map(item => String(item.name || item.id));
}

// memberMessagesOf 取某位 teammate 的消息队列（尾插回执；后端给多少读多少）。
export function memberMessagesOf(plan, role) {
  const name = String(role || "");
  const member = (Array.isArray(plan?.members) ? plan.members : []).find(entry => entry && String(entry.role || "") === name);
  return Array.isArray(member?.messages) ? member.messages.filter(message => message && message.text) : [];
}

// memberStatusOf 取某位 teammate 的实时状态：**只有 running / free 两个值**
// （2026-10-04 用户口径）。
//
// 后端给什么认什么，但取值收敛到这两个：一个"没在干活的人"不该有 done / idle / review
// 三种写法（前端各写一遍就是三份口径）。后端缺失时按 free —— 缺状态字段是降级输入，
// 而"这个人此刻没在跑"是降级下最诚实的读法。
export function memberStatusOf(plan, role) {
  const name = String(role || "");
  const member = (Array.isArray(plan?.members) ? plan.members : []).find(entry => entry && String(entry.role || "") === name);
  const status = String(member?.status || "").trim().toLowerCase();
  if (status === "running") return "running";
  return "free";
}

// memberCurrentSessionOf 反解"这位 teammate 此刻那件事的会话"：优先在跑的工作项，
// 其次等验收的，都没有就退到它最近开过工的那件事；一件都没开过 → 全空（**调用方不挂
// 入口**：teammate 的角色会话读面是群聊车道形状——main 车道 = 主会话整段，而它自己的
// 行从来不写在那里，退过去就是把主代理的会话冒充成这位的会话，见 renderTeamQueue）。
//
// 为什么前端也要算一份：后端已经给了 current_session_id / current_work_item（权威），
// 这里在同一条口径上做**降级兜底**——老快照（没有这两个字段）仍然要能点出正确的入口，
// 而不是退回"员工的长期历史会话"。两处都读同一份事实（工作项 + 状态），不存在第二份口径。
export function memberCurrentSessionOf(plan, role) {
  const name = String(role || "");
  const member = (Array.isArray(plan?.members) ? plan.members : []).find(entry => entry && String(entry.role || "") === name);
  const provided = String(member?.current_session_id || "").trim();
  if (provided) {
    return { session_id: provided, work_item: String(member?.current_work_item || ""), name: "" };
  }
  let running = null;
  let review = null;
  let last = null;
  for (const item of workItemsOf(plan)) {
    if (String(item.role || "") !== name) continue;
    const session = String(item.session_id || "").trim();
    if (!session) continue;
    const status = itemStatus(item);
    if (status === "running") running = item;
    else if (status === "review" && !review) review = item;
    else last = item;
  }
  const chosen = running || review || last;
  if (!chosen) return { session_id: "", work_item: "", name: "" };
  return { session_id: String(chosen.session_id || ""), work_item: String(chosen.id || ""), name: String(chosen.name || "") };
}

// teammateSessionEntry 决定"查看这一位的会话"该开什么——**两条入口共用这一份判定**
// （看板的 teammate 行 + Agent Team 面板成员行的「查看」）。
//
// 判据只有一条事实：这位是不是这份团队计划里的 teammate。
//   - 是，且有"这件事自己的会话" → `{kind:"live", session_id, work_item}`：开实时读面
//     （一 Work Item 一套 Session，那一轮活跑在**进程内执行面**上，正文只能实时读）；
//   - 是，但没有（还没派活 / 已验收销项）→ `{kind:"none", reason}`：**不打开**。
//     为什么"不打开"而不是"退回角色会话"（2026-10-04 用户口径修正：查看 teammate 的
//     会话"总是看到主代理的会话"）：teammate 的角色会话读面是**群聊车道**形状——main
//     车道 = **主会话整段**，而 teammate 自己那条车道在存储里从来没有行（worker 回合不写
//     存储）。退过去，用户看到的就是主代理的会话被当成这位的会话。宁可不给入口。
//   - 不是 teammate（goal-a2a 的 tl 等真有自己角色的会话）→ `{kind:"role"}`：照旧走角色会话。
export function teammateSessionEntry(plan, role) {
  const name = String(role || "").trim();
  if (!name) return { kind: "none", reason: "角色名为空" };
  const current = memberCurrentSessionOf(plan, name);
  if (current.session_id) {
    return { kind: "live", session_id: current.session_id, work_item: current.work_item, name: current.name };
  }
  const member = (Array.isArray(plan?.members) ? plan.members : []).find(entry => entry && String(entry.role || "") === name);
  if (member) {
    return { kind: "none", reason: noOwnSessionNote(name) };
  }
  return { kind: "role" };
}

// noOwnSessionNote 是"这一位此刻没有自己的会话"的同一句话（看板上是 title，面板上是提示）：
// 一句话只写一次，用户在两处读到的是同一份解释。
function noOwnSessionNote(role) {
  return `${role} 这一位此刻没有自己的会话：teammate 的每一件事各自一套 Session（一 Work Item 一套），没有在跑或待验收的工作项就没有可看的正文`;
}

// summarizeTeam 是看板头部的一行计数（里程碑 / 工作项 / 在编 / 作业）。
//
// 只报事实，不给阶段数——阶段口径已退场，工作项才是"排了多少活、走了多少"的单位。
export function summarizeTeam(plan, jobs) {
  const members = Array.isArray(plan?.members) ? plan.members.filter(Boolean) : [];
  const milestones = milestonesOf(plan);
  const items = workItemsOf(plan);
  const statuses = items.map(itemStatus);
  const jobList = (Array.isArray(jobs) ? jobs : []).filter(Boolean);
  return {
    members: members.length,
    milestones_total: milestones.length,
    milestones_done: milestones.filter(milestone => milestoneStatus(milestone) === "done").length,
    items_total: items.length,
    items_done: statuses.filter(status => status === "done").length,
    items_running: statuses.filter(status => status === "running").length,
    items_review: statuses.filter(status => status === "review").length,
    items_failed: statuses.filter(status => status === "failed").length,
    jobs_running: jobList.filter(job => jobState(job) === "running").length,
    jobs_failed: jobList.filter(jobFailed).length,
    jobs_done: jobList.filter(job => jobState(job) === "done").length,
  };
}

// 作业终态：failed / killed 都算「这条作业没走通」，不把被杀当成还在跑。
const FAILED_JOB_STATES = new Set(["failed", "killed"]);

// jobState / jobFailed 是作业行的状态读法：缺字段一律当未知，不当作 done。
export function jobState(job) {
  return String(job?.state || "").trim().toLowerCase();
}

export function jobFailed(job) {
  return FAILED_JOB_STATES.has(jobState(job));
}

// ── 渲染 ────────────────────────────────────────────────────────

// renderTeamBoard 渲染整块看板：里程碑（它的卡片列出名下工作项）+ teammate 区块 + 审计。
//
// 没有里程碑也没有工作项 → ""（不留空壳，口径同目标看板）。判据只看里程碑与工作项：
// 阶段口径已删除，不存在"计划的形状里有阶段"这回事。
export function renderTeamBoard(input = {}) {
  const { plan = null, jobs = [], events = [], maxMembers = 0, stale = false, recovered = false } = input;
  const milestones = milestonesOf(plan);
  const items = workItemsOf(plan);
  if (milestones.length === 0 && items.length === 0) return "";
  const summary = summarizeTeam(plan, jobs);
  return `<div class="team-board" data-team-board data-team-id="${escapeHtml(String(plan?.team_id || ""))}">
      ${renderTeamHead(plan, summary, maxMembers, stale, recovered)}
      ${renderTeamGantt(plan)}
      ${renderTeamQueue(plan)}
      ${renderTeamAudit(events)}
    </div>`;
}

// renderTeamHead 是看板头：team_id + 版本 + 一行计数。计数只报事实（不写「进度良好」这类话）。
//
// 两个标记都是**痕迹**，不是装饰：
//   - recovered：这份看板来自存档快照（活体投影给不出时兜底），不是活体算出来的；
//   - stale：里面的作业行句柄来自上一个进程（jobs I-4），真值以 Observe 为准。
// 恢复出来的看板两者同时为真，所以同屏出现（不是二选一）。
export function renderTeamHead(plan, summary, maxMembers = 0, stale = false, recovered = false) {
  const version = Number(plan?.version) || 0;
  const teamID = String(plan?.team_id || "未命名团队");
  const roster = maxMembers > 0 ? `在编 ${summary.members}/${maxMembers}` : `在编 ${summary.members}`;
  const counts = [
    `工作项 ${summary.items_done}/${summary.items_total} 完成`,
    summary.items_running ? `${summary.items_running} 在跑` : "",
    summary.items_review ? `${summary.items_review} 待验收` : "",
    summary.items_failed ? `${summary.items_failed} 未通过` : "",
    roster,
    `作业 ${summary.jobs_running} 跑 / ${summary.jobs_done} 完 / ${summary.jobs_failed} 败`,
    `里程碑 ${summary.milestones_done}/${summary.milestones_total}`,
  ].filter(Boolean).join(" · ");
  const recoveredChip = recovered
    ? '<span class="chip team-recovered" title="这块看板来自会话存档快照（活体投影给不出时才兜底），活体一恢复就会覆盖回活体结果">自快照恢复</span>'
    : "";
  const staleChip = stale
    ? '<span class="chip team-stale" title="jobs I-4：句柄只在内存，进程重启后的投影一律过期，真值以 Observe 为准">句柄投影可能过期</span>'
    : "";
  return `<div class="team-board-head">
      <span class="team-badge">TEAM</span>
      <span class="team-board-id" title="${escapeHtml(teamID)}">${escapeHtml(truncate(teamID, TEXT_LIMIT))}</span>
      ${version > 0 ? `<span class="team-version">v${escapeHtml(String(version))}</span>` : ""}
      ${recoveredChip}
      ${staleChip}
      <span class="team-counts">${escapeHtml(counts)}</span>
    </div>`;
}

// renderTeamGantt 是**里程碑甘特**：一个里程碑一张卡，卡里列出它名下的 Work Item。
//
// 里程碑之间是**屏障**（depends_on，串行）；里程碑内部是工作项的依赖 DAG。它不表示时间——
// 只表示依赖（用户口径：甘特不表示时间，只表示 Work Item 的依赖项与 Milestone 之间的依赖）。
//
// 每张里程碑卡：id · name / 状态 / 屏障层号 / 屏障依赖 / 判据（after）/ leader 撰写的内容 /
// 名下工作项（名称 / 描述 / 达成目标 / 执行 teammate / 依赖 / 状态）。
export function renderTeamGantt(plan) {
  const entries = orderMilestones(plan);
  if (entries.length === 0) return "";
  const blocks = entries.map(entry => {
    const deps = entry.depends_on.length
      ? entry.depends_on.map(dep => `<span class="chip team-dep">${escapeHtml(dep)}</span>`).join("")
      : '<span class="muted">—</span>';
    const after = entry.after.length
      ? `<div class="team-deps"><span class="team-label">判据</span>${entry.after.map(dep => `<span class="chip team-dep">${escapeHtml(dep)}</span>`).join("")}</div>`
      : "";
    const missing = entry.missing_deps.length
      ? `<div class="team-warn" title="depends_on 指向计划里不存在的里程碑">依赖缺失：${escapeHtml(entry.missing_deps.join("、"))}</div>`
      : "";
    const cyclic = entry.cyclic
      ? '<div class="team-warn" title="depends_on 成环，层号归 0 并接在末尾">依赖成环</div>'
      : "";
    const content = entry.content
      ? `<div class="team-milestone-content" title="${escapeHtml(entry.content)}">${escapeHtml(truncate(entry.content, CONTENT_LIMIT))}</div>`
      : "";
    const title = entry.name ? `${escapeHtml(entry.id)} · ${escapeHtml(entry.name)}` : escapeHtml(entry.id);
    const ordered = orderWorkItems(entry.items);
    const items = ordered.length
      ? ordered.map(item => renderTeamWorkItem(item)).join("")
      : '<li class="team-item is-empty">尚未排活</li>';
    return `<article class="team-milestone-gantt" data-milestone-id="${escapeHtml(entry.id)}" data-status="${escapeHtml(entry.status)}" data-depth="${escapeHtml(String(entry.depth))}">
        <div class="team-milestone-head">
          <span class="team-milestone-id">${title}</span>
          <span class="team-depth" title="屏障层号 L${escapeHtml(String(entry.depth))}（同层 = 依赖边允许并行）">L${escapeHtml(String(entry.depth))}</span>
          <span class="team-status is-${escapeHtml(entry.status)}">${escapeHtml(entry.status)}</span>
        </div>
        <div class="team-deps"><span class="team-label">屏障</span>${deps}</div>
        ${after}${missing}${cyclic}${content}
        <ul class="team-items">${items}</ul>
      </article>`;
  }).join("");
  return `<section class="team-section" data-team-gantt>
      <div class="team-section-title"><span>里程碑 · 工作项</span><span class="chip team-count">${entries.length}</span></div>
      <div class="team-gantt">${blocks}</div>
    </section>`;
}

// renderTeamWorkItem 渲染一个工作项行：名称 / 描述 / 达成目标 / 执行 teammate / 依赖 / 状态。
//
// 「一件事一套隔离」写在行上：Session 与 worktree 是这一件事自己的，验收通过之后会被释放
// （所以 done 的行不再显示它们）。被中断（interrupted）的行显式标出来——那是"可以重派"，
// 不是"跑丢了"。
export function renderTeamWorkItem(entry) {
  const item = entry?.item || {};
  const status = itemStatus(item);
  const role = String(item.role || "").trim();
  const deps = entry.depends_on.length
    ? entry.depends_on.map(dep => `<span class="chip team-dep">${escapeHtml(dep)}</span>`).join("")
    : '<span class="muted">—</span>';
  const session = String(item.session_id || "").trim();
  const worktree = String(item.worktree || "").trim();
  const goal = String(item.goal || "").replace(/\s+/g, " ").trim();
  const description = String(item.description || "").replace(/\s+/g, " ").trim();
  const note = String(item.note || "").replace(/\s+/g, " ").trim();
  const marks = [
    entry.blocked ? '<span class="chip team-blocked" title="前置工作项还没验收通过：现在派发会被闸门拒">被依赖卡住</span>' : "",
    item.interrupted ? '<span class="chip team-interrupted" title="状态说在跑、而本进程的作业表里查不到它的句柄（jobs I-4）。可以重派——会话与现场都还在">可重派</span>' : "",
    item.live ? '<span class="chip team-live" title="这件事现在真的有一份未释放的工作区绑定">现场在</span>' : "",
  ].filter(Boolean).join("");
  const name = String(item.name || item.id || "");
  const label = session
    ? `<button type="button" class="team-item-name is-openable" data-team-item-open="${escapeHtml(String(item.id || ""))}" data-team-item-session="${escapeHtml(session)}" data-team-item-role="${escapeHtml(role)}" data-tip="查看这件事的执行进度（子页面）">${escapeHtml(name)}</button>`
    : `<span class="team-item-name">${escapeHtml(name)}</span>`;
  return `<li class="team-item is-${escapeHtml(status)}" data-item-id="${escapeHtml(String(item.id || ""))}" data-status="${escapeHtml(status)}" data-depth="${escapeHtml(String(entry.depth))}">
      <div class="team-item-head">
        ${label}
        ${role ? `<span class="chip team-role">${escapeHtml(role)}</span>` : ""}
        <span class="team-status is-${escapeHtml(status)}">${escapeHtml(status)}</span>
      </div>
      ${description ? `<div class="team-item-desc" title="${escapeHtml(description)}">${escapeHtml(truncate(description, CONTENT_LIMIT))}</div>` : ""}
      ${goal ? `<div class="team-item-goal"><span class="team-label">达成目标</span>${escapeHtml(truncate(goal, CONTENT_LIMIT))}</div>` : ""}
      <div class="team-item-meta">
        <span class="team-label">依赖</span>${deps}
        ${session ? `<span class="team-item-session" title="${escapeHtml(session)}">${escapeHtml(session)}</span>` : ""}
        ${worktree && status !== "done" ? `<span class="team-item-wt" title="${escapeHtml(worktree)}">${escapeHtml(worktree)}</span>` : ""}
        ${marks}
      </div>
      ${note ? `<div class="team-item-note" title="${escapeHtml(note)}">${escapeHtml(truncate(note, CONTENT_LIMIT))}</div>` : ""}
    </li>`;
}

// renderMemberAssembly 渲染一位 teammate 的**插件装配**（声明面 + 生效读数）。
//
// 数据两格（2026-10-05 · leader 冻结的契约，后端 commit 39a8b63）：
//   - member.plugins  = **声明面**（计划 members[].plugins 规整后那一份；空 = 不覆盖）；
//   - member.assembly = **生效读数**（mode / 技能目录字节 / 插件面工具数 / 黄牌 / 失灵）。
// 两格合成一件事的两个侧面：只有声明面说不清"空集 = 继承宿主"还是"装了个不存在的名字"
// （前者 inherit-host，后者 plugin_face_faulted）；只有读数说不清"这位被**要求**装什么"。
//
// **装配是 teammate 级的**：一位 teammate = 一条长期角色会话 + 每个工作项**自己的**会话，
// 声明落在这一位身上，对它手上的**每个工作项会话**都生效——所以这一行的文案要说这句，
// 而不是让读的人以为这是"某个工作项"的装配。
//
// 口径（照写，不自由发挥）：mode 的每个取值都**显式写出来**，不靠字段缺失暗示——
//   replace      → 每个插件一枚 chip + 一行读数（技能 n / 目录 xB · ≈y tok / 工具面 a/b）
//                  + 黄牌（title = yellow_reason）；黄牌**只报不拒**，不遮其它读数；
//   inherit-host → 显式写「继承宿主」（空集 = 不覆盖：工具面继承宿主 + 技能目录不注入）；
//   **assembly 为 nil → 不显示装配格**（2026-10-05 审查 P0 修正，回到契约原文）：
//                  `dto/teamwork_board.go` 的 Assembly 注释写的是"nil 表示**桥这一侧给不出
//                  读数**（未装配插件域 / 成员行不在读数里）：前端据此不显示装配格，而不是
//                  把缺失读成'0 个工具、0 份技能'"。所以这里既不许写「按不覆盖处理」（读数
//                  缺失 ≠ 空集），也不许写「继承宿主」——前者把缺失当读数，后者替后端断言了
//                  前端无从知道的语义（"空集 = 不覆盖"只能由 mode=inherit-host 回答）。
//                   有声明面时只列声明 chips（计划的事实），一个 mode 结论都不写。
// plugin_face_faulted / plugin_face_missing 是**失灵读数**：声明过的插件在本进程已无定义
// （root 撤销过 / 名字漂了）。它们与"没装配"**语义相反**，所以**绝不**渲染成继承宿主那
// 一支——Mode 仍是 replace、声明仍是那一份，本件只在此之上加「失灵 / 已撤」标记 + Note。
export function renderMemberAssembly(member) {
  const declared = (Array.isArray(member?.plugins) ? member.plugins : [])
    .map(name => String(name ?? "").trim())
    .filter(Boolean);
  const assembly = member?.assembly && typeof member.assembly === "object" ? member.assembly : null;
  const mode = String(assembly?.mode || "").trim();
  // 声明面 chips：复用本文件既有 chip 类。长插件名不许撑破行——三层一起兜（chips 容器
  // 换行 + 单枚 max-width + 文本截断），title 里留全名。
  const chips = declared.map(name =>
    `<span class="chip team-assembly-plugin" title="${escapeHtml(name)}">${escapeHtml(truncate(name, 48))}</span>`
  ).join("");
  const scope = `<span class="team-assembly-scope" title="装配声明在 teammate 级：一位 teammate = 一条长期角色会话 + 每个工作项自己的会话；这份装配对它手上的每个工作项会话都生效">teammate 级 · 对这位每个工作项会话都生效</span>`;
  // **读数给不出 / 没写明 mode ⟹ 不下 mode 结论**（契约：Assembly 为 nil = 桥这一侧给不出
  // 读数 → 前端不显示装配格）。声明面是**计划**的事实、与读数无关：有声明时就只列声明 chips
  // ——它既不否认也不证实任何 mode，所以一个字都不说「继承宿主」（"被要求装了两个插件"与
  // "继承宿主"互斥，同屏就是自相矛盾）；没有声明面就整格退场，不留空壳。
  if (!assembly || (mode !== "replace" && mode !== "inherit-host")) {
    if (declared.length === 0) return "";
    return `<div class="team-member-assembly">
        <span class="team-label">装配</span>
        ${chips}
        ${scope}
      </div>`;
  }
  // 黄牌：目录段超阈值（6k token 估算 / 上下文窗口 2%）。**只报不拒**——它是一句提示，
  // 不改变这一位能不能干活，所以和读数同屏、不替换读数。
  const yellow = assembly?.yellow === true
    ? `<span class="chip team-assembly-yellow" title="${escapeHtml(String(assembly?.yellow_reason || "技能目录超阈值（只报不拒）"))}">黄牌</span>`
    : "";
  const missing = (Array.isArray(assembly?.plugin_face_missing) ? assembly.plugin_face_missing : [])
    .map(name => String(name ?? "").trim())
    .filter(Boolean);
  const faulted = assembly?.plugin_face_faulted === true
    ? `<span class="chip team-assembly-faulted" title="${escapeHtml(String(assembly?.plugin_face_note || "声明过的插件在本进程已无定义（工具面为空）"))}">失灵</span>`
    : "";
  const withdrawn = missing.length
    ? `<span class="chip team-assembly-missing" title="${escapeHtml("已被撤销 / 名字漂移（本进程已无定义）：" + missing.join("、"))}">已撤 ${escapeHtml(String(missing.length))}</span>`
    : "";
  const note = String(assembly?.plugin_face_note || "").trim();
  let readout;
  if (mode === "replace") {
    const count = value => Number(value) || 0;
    // 窄栏里这一行会被省略号吃掉（审查 Hypothesis）：title 里放**同一句话的全文** +
    // 上界口径说明，悬停就能读到被截掉的部分，而不是让读者猜。
    const readoutText = `技能 ${count(assembly?.skill_count)} / 目录 ${count(assembly?.skill_catalog_runes)}B · ≈${count(assembly?.skill_catalog_tokens_est)} tok / 工具面 ${count(assembly?.plugin_face_tools)}/${count(assembly?.total_tools)}`;
    const readoutTip = `${readoutText}（生效读数：插件面工具数是上界——全量工具里过得了插件收窄的那些，实际可见面还要与权限面相交，只会更小）`;
    readout = `<span class="team-assembly-readout" title="${escapeHtml(readoutTip)}">${escapeHtml(readoutText)}</span>`;
  } else {
    readout = `<span class="team-assembly-inherit" title="空集 = 不覆盖：工具面继承宿主当前装配，技能目录不注入（运行事实，不是读数没算）">继承宿主</span>`;
  }
  return `<div class="team-member-assembly" data-assembly-mode="${escapeHtml(mode)}">
        <span class="team-label">装配</span>
        ${chips}
        ${readout}
        ${yellow}${faulted}${withdrawn}
        ${note ? `<span class="team-assembly-note" title="${escapeHtml(note)}">${escapeHtml(truncate(note, CONTENT_LIMIT))}</span>` : ""}
        ${scope}
      </div>`;
}

// renderTeamQueue 是**teammate 区块**：名称（角色名 = **这件事的会话**入口）/ 状态 / 它负责的
// Work Item 名称队列 / 权责与工作区 / 尾插回执。
//
// 队列与状态都从工作项算（后端算好的优先）：看板回答"这个人手上还有什么"，而不是让读的人
// 自己按 role 再分一次组。
//
// 角色名是**成员会话的入口**（2026-10-02 · S7）：点它打开这位 teammate 的会话
// （`data-team-role-open` / `data-team-role-session`，与团队面板成员行同一对钩子，
// app.js 的看板委托把它们接到同一个 openRoleSessionDetail 上）。
//
// **入口只指向"这件事自己的会话"**（2026-10-04，用户口径修正：查看 teammate 的会话
// "总是看到主代理的会话"）。原因是一条**读面事实**，不是偏好：
//   - teammate 的一轮活跑在**这件事自己的会话**上（一 Work Item 一套 Session），
//     而那是**进程内执行面**（刻意不接 DurableHistory），正文只在它活着的时候读得到；
//   - 而"员工的长期角色会话"（role_session_id）是**群聊车道**形状的读面：它在存储里
//     从来没有 teammate 自己的行（worker 回合不写存储），`RoleSnapshot` 的 main 车道
//     却是**主会话整段**——于是打开它，用户看到的就是主代理的会话，被当成了这位的会话。
// 所以没有"这件事自己的会话"时**不挂入口**（纯文本 + 说清为什么），而不是回退到角色会话
// ——一个内容错的入口比没有入口更坏。
export function renderTeamQueue(plan) {
  const members = Array.isArray(plan?.members) ? plan.members.filter(Boolean) : [];
  const items = workItemsOf(plan);
  if (members.length === 0 || items.length === 0) return "";
  const rows = members.map(member => {
    const role = String(member.role || "");
    const policy = String(member.tools_policy || "").trim();
    const worktree = String(member.worktree || "").trim();
    const queue = memberQueueOf(plan, role);
    const messages = memberMessagesOf(plan, role);
    const status = memberStatusOf(plan, role);
    const queueChips = queue.length
      ? queue.map(name => `<span class="chip team-queue-item">${escapeHtml(name)}</span>`).join("")
      : '<span class="muted">空</span>';
    const policyChip = policy
      ? `<span class="chip team-policy is-${escapeHtml(policy)}">${escapeHtml(policy)}</span>`
      : "";
    // 入口开的是**这位此刻那件事的会话**（2026-10-04 用户口径：点开要是"当前的
    // teammate 的会话"，不是员工的长期历史会话，更不是主代理的会话）。判定与面板成员行
    // 共用一份（teammateSessionEntry）：只认"这件事自己的会话"，没有就不挂入口。
    const entry = teammateSessionEntry(plan, role);
    const tip = entry.kind === "live"
      ? `打开 ${role} 当前这件事的会话（工作项 ${entry.work_item}${entry.name ? " · " + entry.name : ""}）`
      : String(entry.reason || "");
    const roleLabel = entry.kind === "live"
      ? `<button type="button" class="team-member-role is-openable" data-team-role-open="${escapeHtml(role)}" data-team-role-session="${escapeHtml(entry.session_id)}" data-team-item="${escapeHtml(entry.work_item || "")}" data-tip="${escapeHtml(tip)}" aria-label="${escapeHtml(tip)}">${escapeHtml(role)}</button>`
      : `<span class="team-member-role" title="${escapeHtml(tip)}">${escapeHtml(role)}</span>`;
    const latest = messages.length ? messages[messages.length - 1] : null;
    const body = latest
      ? `<div class="team-member-message" title="${escapeHtml(String(latest.text || ""))}">${escapeHtml(truncate(String(latest.text || ""), CONTENT_LIMIT))}</div>`
      : "";
    return `<li class="team-queue" data-role="${escapeHtml(role)}" data-status="${escapeHtml(status)}">
        <div class="team-queue-head">
          ${roleLabel}
          ${policyChip}
          <span class="team-member-wt" title="${escapeHtml(worktree || "未指派工作区（回退主工作区）")}">${escapeHtml(worktree || "主工作区")}</span>
          <span class="team-status is-${escapeHtml(status)}">${escapeHtml(status)}</span>
        </div>
        ${renderMemberAssembly(member)}
        <div class="team-queue-items">${queueChips}</div>
        ${body}
      </li>`;
  }).join("");
  return `<section class="team-section" data-team-queue>
      <div class="team-section-title"><span>teammate</span><span class="chip team-count">${rows.length}</span></div>
      <ul class="team-queues">${rows}</ul>
    </section>`;
}

// renderWorkItemSessionPanel 是**执行进度子页面**的主体（点工作项打开）。
//
// 复用目前详情子页面的形状（k→v 条目），但**不要下面的表格**——子页面回答的是"这件事做到
// 哪一步了"，不是一份状态台账：条目给出这件事自己的会话、工作区、依赖、结论与尾插回执，
// 表格（状态行清单）留在主面板。
export function renderWorkItemSessionPanel(plan, itemID) {
  const item = workItemsOf(plan).find(entry => String(entry.id || "") === String(itemID || ""));
  if (!item) return '<div class="role-session-view is-empty">这件事已不在计划里（可能已收口）</div>';
  const status = itemStatus(item);
  const role = String(item.role || "").trim();
  const rows = workItemsOf(plan);
  const byID = new Map(rows.map(entry => [String(entry.id || ""), entry]));
  const deps = itemDepsOf(item).map(dep => {
    const owner = byID.get(dep);
    const state = owner ? itemStatus(owner) : "unknown";
    return `<span class="chip team-dep">${escapeHtml(dep)} · ${escapeHtml(state)}</span>`;
  }).join("");
  const entries = [
    ["工作项", String(item.id || "")],
    ["名称", String(item.name || "")],
    ["执行 teammate", role],
    ["状态", status],
    ["里程碑", String(item.milestone || "")],
    ["依赖", itemDepsOf(item).length ? "" : "—"],
    ["达成目标", String(item.goal || "")],
    ["描述", String(item.description || "")],
    ["会话", String(item.session_id || "")],
    ["工作区", String(item.worktree || "")],
    ["句柄", String(item.handle || "")],
    ["结论", String(item.note || "")],
  ];
  const kv = entries.map(([key, value]) => {
    const text = String(value ?? "").trim();
    return `<div class="role-kv-row"><span class="role-kv-key">${escapeHtml(key)}</span><span class="role-kv-value" title="${escapeHtml(text)}">${text ? escapeHtml(text) : "—"}</span></div>`;
  }).join("");
  const messages = memberMessagesOf(plan, role)
    .filter(message => String(message.work_item || "") === String(item.id || ""))
    .map(message => `<li class="team-item-message"><span class="team-event-at">${escapeHtml(formatEventTime(message.at || ""))}</span><span>${escapeHtml(String(message.text || ""))}</span></li>`)
    .join("");
  return `<div class="role-session-view team-item-panel" data-team-item-panel="${escapeHtml(String(item.id || ""))}">
      <div class="role-session-head"><span class="team-status is-${escapeHtml(status)}">${escapeHtml(status)}</span></div>
      <div class="role-kv">${kv}</div>
      <div class="team-item-deps"><span class="team-label">依赖</span>${deps || '<span class="muted">—</span>'}</div>
      ${messages ? `<ul class="team-item-messages">${messages}</ul>` : ""}
    </div>`;
}

// renderTeammateLiveSession 渲染**当前 teammate 会话**的实时子页面（这件事自己的会话）。
//
// 为什么单开一个渲染形状而不是复用 renderRoleSessionDetail（2026-10-04 用户口径：
// 「查看 teammate 的会话，全是历史会话不是当前的 teammate 的会话」）：角色会话详情读的是
// **员工的长期角色会话**（跨工作项、跨轮次的落盘历史），而一个 Work Item 的会话是**进程内
// 执行面**——它的正文不在会话库里，只能从执行面实时读（后端 TeammateSessionLive 的那份
// 投影）。把后者硬塞进前者的形状，就会出现"看到的是历史、还以为在看当前"。
//
// 判据诚实：`running=false` 表示这一轮的执行面不在本进程（重启过 / 已收口），此时**不画
// 空壳**，直接说清"正文不落盘、看不到"——那正是用户这次报的那件事的反面。
export function renderTeammateLiveSession(view, meta = {}) {
  const session = String(view?.session_id || "");
  const role = String(view?.role || meta.role || "");
  const workItem = String(meta.work_item || "");
  const chips = [
    `<span class="chip team-item-ref">${escapeHtml(workItem || "这件事")}</span>`,
    role ? `<span class="chip team-role">${escapeHtml(role)}</span>` : "",
    view?.live ? '<span class="chip team-status is-running">正在跑</span>' : '<span class="chip team-status is-free">已跑完</span>',
  ].filter(Boolean).join("");
  const head = `<div class="role-session-head">${chips}</div>`;
  if (!view?.running) {
    return `<div class="role-session-view team-item-panel" data-teammate-live="${escapeHtml(session)}">
      ${head}
      <div class="team-item-note">这一轮的执行面不在本进程（可能重启过或已收口）。teammate 的一轮活是**进程内**执行面，正文不落盘——这里读不到，不是"会话是空的"。</div>
      <div class="team-item-note">结论与回执走团队看板：尾插回执在 teammate 那一行，产物正文在 team_context / jobs_manage。</div>
    </div>`;
  }
  const rows = (Array.isArray(view?.messages) ? view.messages : [])
    // 只画输入与回答两类：工具行的原始载荷是执行细节（后端已经筛过一遍，这里再筛一次是
    // 纯函数该有的自觉——渲染件不画它不理解的东西）。
    .filter(message => message && (String(message.role || "") === "user" || String(message.role || "") === "assistant"))
    .filter(message => String(message.text || "").trim())
    .map(message => {
      const who = String(message.role || "") === "user" ? "输入" : (role || "teammate");
      const text = String(message.text || "").trim();
      return `<div class="role-kv-row team-live-row is-${escapeHtml(String(message.role || ""))}"><span class="role-kv-key">${escapeHtml(who)}</span><span class="role-kv-value" title="${escapeHtml(text)}">${escapeHtml(text)}</span></div>`;
    }).join("");
  const truncated = view?.truncated
    ? '<div class="team-item-note">（只显示最近若干条；单条过长已截断）</div>'
    : "";
  return `<div class="role-session-view team-item-panel" data-teammate-live="${escapeHtml(session)}">
      ${head}
      <div class="role-kv">${rows || '<div class="team-item-note">这一轮还没有对话（刚派发，或回合还没写第一行）。</div>'}</div>
      ${truncated}
    </div>`;
}

// renderTeamAudit 是审计流水的最近几行（只追加、不重写；倒序 = 最新的在最上面）。
export function renderTeamAudit(events) {
  const list = (Array.isArray(events) ? events : []).filter(item => item && item.kind);
  if (list.length === 0) return "";
  const recent = list.slice(-AUDIT_LIMIT).reverse();
  const rows = recent.map(event => {
    const kind = String(event.kind || "");
    const target = [event.milestone, event.work_item, event.role, event.handle].filter(Boolean).map(String).join(" · ");
    const detail = String(event.detail || "").replace(/\s+/g, " ").trim();
    return `<li class="team-event" data-kind="${escapeHtml(kind)}">
        <span class="chip team-event-kind is-${escapeHtml(kind)}">${escapeHtml(kind)}</span>
        <span class="team-event-target">${escapeHtml(target || "—")}</span>
        <time class="team-event-at">${escapeHtml(formatEventTime(event.at))}</time>
        ${detail ? `<span class="team-event-detail" title="${escapeHtml(detail)}">${escapeHtml(truncate(detail, TEXT_LIMIT))}</span>` : ""}
      </li>`;
  }).join("");
  return `<section class="team-section" data-team-audit>
      <div class="team-section-title"><span>审计</span><span class="chip team-count" title="共 ${list.length} 条，显示最近 ${recent.length} 条">${list.length}</span></div>
      <ul class="team-events">${rows}</ul>
    </section>`;
}

// formatEventTime 只取时间部分（HH:MM）：跨时区的完整时间戳在窄栏里没有信息量，
// 而且切片是确定性的，用例不会随机器时区变色。
export function formatEventTime(at) {
  const text = String(at || "").trim();
  const match = /^\d{4}-\d{2}-\d{2}T(\d{2}:\d{2})/.exec(text);
  return match ? match[1] : (text ? truncate(text, 11) : "—");
}

function truncate(text, limit) {
  const value = String(text ?? "");
  return value.length > limit ? `${value.slice(0, limit - 1)}…` : value;
}

// TEAM_BOARD_CSS 是这块看板自己的样式（只吃 styles.css 的语义 token，不写第二套色值）。
// 接线进右栏时把它并入 styles.css 的同一节；静态预览页直接注入这一段。
// 阶段口径已删除（2026-10-04）：不再有 .team-stage* / .team-job* 一套。
export const TEAM_BOARD_CSS = `
.team-board { display: flex; flex-direction: column; gap: 8px; min-width: 0; font-size: var(--text-sm); color: var(--text-strong); }
.team-board-head { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; font-size: var(--text-xs); color: var(--text-dim); }
.team-badge { flex: none; padding: 1px 5px; border: 1px solid var(--border-strong); border-radius: 4px; font-weight: 600; letter-spacing: .06em; color: var(--text-mid); }
.team-board-id { min-width: 0; max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--font-mono); color: var(--text-strong); }
.team-version { flex: none; color: var(--text-dim); }
.team-counts { flex: 1 1 100%; min-width: 0; overflow-wrap: anywhere; }
.team-recovered { border-color: var(--border-strong); color: var(--text-mid); }
.team-stale { border-color: var(--border-running); color: var(--status-running); }
.team-status { flex: none; margin-left: auto; padding: 0 5px; border: 1px solid var(--border); border-radius: 999px; font-size: 10px; letter-spacing: .04em; text-transform: uppercase; }
.team-status.is-running { color: var(--status-running); border-color: var(--border-running); background: var(--tint-running); }
.team-status.is-active { color: var(--status-running); border-color: var(--border-running); background: var(--tint-running); }
.team-status.is-done { color: var(--status-done); border-color: var(--border-done); background: var(--tint-done); }
.team-status.is-failed { color: var(--status-failed); border-color: var(--border-failed); background: var(--tint-failed); }
.team-status.is-review { color: var(--status-info); border-color: var(--border-info); background: var(--tint-info); }
.team-status.is-pending { color: var(--status-idle); }
.team-status.is-free { color: var(--status-idle); }
.team-label { color: var(--faint); font-size: var(--text-xs); }
.team-deps { display: flex; align-items: center; gap: 4px; flex-wrap: wrap; min-width: 0; }
.team-deps .chip { padding: 0 5px; border-radius: 4px; font-size: var(--text-xs); }
.team-role { color: var(--text-mid); border: 1px solid var(--border-hairline); }
.team-dep { color: var(--text-dim); border: 1px dashed var(--border-strong); }
.team-warn { color: var(--status-failed); font-size: var(--text-xs); }
.team-section { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.team-section-title { display: flex; align-items: center; gap: 5px; font-size: var(--text-xs); color: var(--text-dim); }
.team-count { padding: 0 5px; border: 1px solid var(--border-hairline); border-radius: 4px; font-size: 10px; }
.team-milestone-content { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-mid); font-size: var(--text-xs); }
.team-event { display: flex; align-items: baseline; gap: 5px; flex-wrap: wrap; min-width: 0; font-size: var(--text-xs); }
.team-event-kind { padding: 0 5px; border: 1px solid var(--border-hairline); border-radius: 4px; font-size: 10px; color: var(--text-mid); }
.team-event-kind.is-dispatch { color: var(--status-info); border-color: var(--border-info); }
.team-event-kind.is-milestone { color: var(--status-done); border-color: var(--border-done); }
.team-event-kind.is-retire { color: var(--text-dim); }
.team-event-target { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--font-mono); color: var(--text-mid); }
.team-event-at { flex: none; color: var(--faint); }
.team-event-detail { flex: 1 1 100%; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--faint); }
/* ── 里程碑 → Work Item（2026-10-03）：里程碑屏障 + 工作项 + teammate 队列 + 执行进度子页面 ── */
.team-gantt { display: flex; flex-direction: column; gap: 6px; }
.team-milestone-gantt { display: flex; flex-direction: column; gap: 3px; padding: 5px 7px; border: 1px solid var(--border); border-left-width: 2px; border-radius: 6px; background: var(--code-bg); min-width: 0; }
.team-milestone-gantt[data-status="done"] { border-left-color: var(--status-done); }
.team-milestone-gantt[data-status="active"] { border-left-color: var(--status-running); }
.team-milestone-gantt[data-status="pending"] { border-left-color: var(--status-idle); }
.team-milestone-head { display: flex; align-items: center; gap: 5px; min-width: 0; }
.team-milestone-id { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--font-mono); font-size: var(--text-sm); color: var(--text-bright); }
.team-depth { flex: none; color: var(--faint); font-size: var(--text-xs); }
.team-items, .team-queues, .team-item-messages { display: flex; flex-direction: column; gap: 3px; margin: 0; padding: 0; list-style: none; min-width: 0; }
.team-item { display: flex; flex-direction: column; gap: 2px; padding: 4px 6px; border: 1px solid var(--border-hairline); border-left-width: 2px; border-radius: 5px; min-width: 0; }
.team-item[data-status="running"] { border-left-color: var(--status-running); background: var(--tint-running); }
.team-item[data-status="review"] { border-left-color: var(--status-info); background: var(--tint-info); }
.team-item[data-status="done"] { border-left-color: var(--status-done); }
.team-item[data-status="failed"] { border-left-color: var(--status-failed); background: var(--tint-failed); }
.team-item[data-status="pending"] { border-left-color: var(--status-idle); }
.team-item.is-empty { color: var(--faint); font-size: var(--text-xs); border-left-color: var(--border-hairline); background: none; }
.team-item-head { display: flex; align-items: center; gap: 5px; min-width: 0; }
.team-item-name { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-strong); }
.team-item-name.is-openable { padding: 0; border: 0; background: none; text-align: left; font-size: inherit; color: var(--text-strong); cursor: pointer; text-decoration: underline dotted var(--border-strong); text-underline-offset: 2px; }
.team-item-name.is-openable:hover { color: var(--text-bright); }
.team-item-name.is-openable:focus-visible { outline: 1px solid var(--border-info); outline-offset: 1px; border-radius: 3px; }
.team-item-head .team-status { margin-left: auto; }
.team-item-desc { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-mid); font-size: var(--text-xs); }
.team-item-goal { display: flex; align-items: baseline; gap: 4px; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-mid); font-size: var(--text-xs); }
.team-item-meta { display: flex; align-items: center; gap: 4px; flex-wrap: wrap; min-width: 0; font-size: var(--text-xs); color: var(--text-dim); }
.team-item-session, .team-item-wt { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--font-mono); font-size: var(--text-xs); color: var(--faint); }
.team-item-note { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-mid); font-size: var(--text-xs); }
.team-item-message { display: flex; gap: 5px; min-width: 0; font-size: var(--text-xs); color: var(--text-mid); }
.team-interrupted { color: var(--status-failed); border-color: var(--border-failed); }
.team-blocked { color: var(--status-idle); }
.team-live { color: var(--status-done); border-color: var(--border-done); }
.team-queue { display: flex; flex-direction: column; gap: 3px; padding: 4px 6px; border: 1px solid var(--border-hairline); border-radius: 5px; min-width: 0; }
.team-queue-head { display: flex; align-items: center; gap: 5px; min-width: 0; }
.team-queue-head .team-status { margin-left: auto; }
.team-member-role { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--font-mono); color: var(--text-strong); }
.team-member-role.is-openable { padding: 0; border: 0; background: none; text-align: left; font-family: var(--font-mono); font-size: inherit; color: var(--text-strong); cursor: pointer; text-decoration: underline dotted var(--border-strong); text-underline-offset: 2px; }
.team-member-role.is-openable:hover { color: var(--text-bright); text-decoration-color: var(--text-bright); }
.team-member-role.is-openable:focus-visible { outline: 1px solid var(--border-info); outline-offset: 1px; border-radius: 3px; }
.team-policy { flex: none; padding: 0 5px; border: 1px solid var(--border-hairline); border-radius: 4px; font-size: 10px; color: var(--text-mid); }
.team-policy.is-readonly { color: var(--status-info); border-color: var(--border-info); background: var(--tint-info); }
.team-member-wt { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-dim); font-size: var(--text-xs); }
.team-queue-items { display: flex; align-items: center; gap: 4px; flex-wrap: wrap; min-width: 0; }
.team-queue-item { padding: 0 5px; border: 1px solid var(--border-hairline); border-radius: 4px; font-size: var(--text-xs); color: var(--text-mid); }
.team-member-message { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--faint); font-size: var(--text-xs); }
.team-item-panel .role-kv-row { display: flex; gap: 6px; min-width: 0; }
.team-item-panel .role-kv-key { flex: none; min-width: 72px; color: var(--faint); }
.team-item-panel .role-kv-value { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-mid); }
.team-item-deps { display: flex; align-items: center; gap: 4px; flex-wrap: wrap; margin-top: 4px; }
/* ── 插件装配（2026-10-05）：声明面 chips + 生效读数 + 黄牌 / 失灵（teammate 级） ── */
.team-member-assembly { display: flex; align-items: center; gap: 4px; flex-wrap: wrap; min-width: 0; font-size: var(--text-xs); }
.team-assembly-plugin { max-width: 160px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; padding: 0 5px; border: 1px solid var(--border-hairline); border-radius: 4px; font-size: var(--text-xs); color: var(--text-mid); }
.team-assembly-readout { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--font-mono); color: var(--text-dim); }
.team-assembly-inherit { padding: 0 5px; border: 1px dashed var(--border-strong); border-radius: 4px; color: var(--text-dim); }
.team-assembly-scope { color: var(--faint); font-size: var(--text-xs); }
.team-assembly-note { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-mid); }
.team-assembly-yellow { color: var(--status-running); border-color: var(--border-running); background: var(--tint-running); }
.team-assembly-faulted { color: var(--status-failed); border-color: var(--border-failed); background: var(--tint-failed); }
.team-assembly-missing { color: var(--status-info); border-color: var(--border-info); }
`;
