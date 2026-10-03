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
// **阶段（stages）口径已退场**：计划里就算有 stages，看板也不再按阶段分组（那会把
// 「哪一步做什么、谁做、做完的判据」拆成看不见的碎片）。计划里**没有 stages 也能照常出图**。

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

export function memberStatusOf(plan, role) {
  const name = String(role || "");
  const member = (Array.isArray(plan?.members) ? plan.members : []).find(entry => entry && String(entry.role || "") === name);
  const status = String(member?.status || "").trim().toLowerCase();
  if (status) return status;
  return "idle";
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
// 没有里程碑也没有工作项 → ""（不留空壳，口径同目标看板）。**计划里没有 stages 也照常出图**：
// 阶段不再参与渲染，所以"没有 stages"不等于"没有编排"。
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

// renderTeamQueue 是**teammate 区块**：名称（角色名 = 员工会话入口）/ 状态 / 它负责的
// Work Item 名称队列 / 权责与工作区 / 尾插回执。
//
// 队列与状态都从工作项算（后端算好的优先）：看板回答"这个人手上还有什么"，而不是让读的人
// 自己按 role 再分一次组。
//
// 角色名是**成员会话的入口**（2026-10-02 · S7）：点它打开这位员工的独立会话
// （`data-team-role-open` / `data-team-role-session`，与团队面板成员行同一对钩子，
// app.js 的看板委托把它们接到同一个 openRoleSessionDetail 上）。**没有 role_session_id
// 就不渲染按钮**：一个点不动的入口比没有入口更坏（空值只可能来自降级输入，那时老实写文本）。
export function renderTeamQueue(plan) {
  const members = Array.isArray(plan?.members) ? plan.members.filter(Boolean) : [];
  const items = workItemsOf(plan);
  if (members.length === 0 || items.length === 0) return "";
  const rows = members.map(member => {
    const role = String(member.role || "");
    const policy = String(member.tools_policy || "").trim();
    const worktree = String(member.worktree || "").trim();
    const session = String(member.role_session_id || "").trim();
    const queue = memberQueueOf(plan, role);
    const messages = memberMessagesOf(plan, role);
    const status = memberStatusOf(plan, role);
    const queueChips = queue.length
      ? queue.map(name => `<span class="chip team-queue-item">${escapeHtml(name)}</span>`).join("")
      : '<span class="muted">空</span>';
    const policyChip = policy
      ? `<span class="chip team-policy is-${escapeHtml(policy)}">${escapeHtml(policy)}</span>`
      : "";
    const roleLabel = session
      ? `<button type="button" class="team-member-role is-openable" data-team-role-open="${escapeHtml(role)}" data-team-role-session="${escapeHtml(session)}" data-tip="打开 ${escapeHtml(role)} 的员工会话（这位此刻在干什么）" aria-label="打开 ${escapeHtml(role)} 的员工会话">${escapeHtml(role)}</button>`
      : `<span class="team-member-role">${escapeHtml(role)}</span>`;
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
// 阶段口径已退场：不再有 .team-stage* / .team-job* 一套。
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
.team-status.is-idle { color: var(--status-idle); }
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
`;
