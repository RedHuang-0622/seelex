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

// ROLE_PALETTE_SIZE 是 teammate 色板的格数（--team-dag-role-0..5）。
const ROLE_PALETTE_SIZE = 6;

// ROLE_SLOT 是 teammate 配色的**登记处**：按 role 首次出现的次序分配，append-only。
//
// 为什么是模块级的登记处、而不是每次从 plan 现算：同一位 teammate 的颜色不许因为
// 「重渲染 / 推进一轮 / 换主题」而变（口径 3）。现算的话，计划里新增一位 teammate 就会把
// 后面所有人的 slot 挤一位，颜色跟着漂——那是「颜色在描述轮次」，不是「颜色在描述是谁」。
// 登记的唯一输入是 role 名（不是时间戳、不是随机数、也不是任何自增 id 写进行里），
// 所以同一份输入连渲两次，结果字符串逐字相同（幂等，口径 8）。
const ROLE_SLOT = new Map();

// roleSlotOf 返回 role 的登记序号（append-only，从 0 起）。slot >= ROLE_PALETTE_SIZE 表示
// 色板已经回绕（第 7 位 teammate 起会与前面某位同色）——渲染时给色带叠一层**斜纹第二通道**，
// 免得两位撞色的 teammate 只靠色带分不出来（口径 11：保留 6 色，斜纹是防撞色，不是装饰）。
//
// 口径校正（2026-10-07 · 独立验证 F4）：登记次序是「**首次进入渲染的次序**」，不是
// `plan.members[]` 的声明序 —— 本函数只有一个调用点（renderTeamWorkItem 的色带 / chip），
// 所以设计稿 README §2 那句「先扫 members[]，再补 work_items[].role」只对**稿子**成立，
// 对产品实现不成立。实测：members=[artist, frontend] 而首行属于 frontend 时，frontend 拿 slot 0。
// 这里的取舍是**进程内稳定**（同一份计划重渲染 / 跨轮 / 换主题都不变色），代价是与声明序无关。
export function roleSlotOf(role) {
  const name = String(role ?? "");
  if (!ROLE_SLOT.has(name)) ROLE_SLOT.set(name, ROLE_SLOT.size);
  return ROLE_SLOT.get(name);
}

// roleColorVar 把 role 折成色板里的令牌名（超出 6 个就回绕复用）。
export function roleColorVar(role) {
  return `--team-dag-role-${roleSlotOf(role) % ROLE_PALETTE_SIZE}`;
}

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

// effStatus 是**线框色的唯一判据**（口径 2）：interrupted 先抬成红，再按 status 取色。
//
// 一条红不裂成两条：failed 与 killed 同色；interrupted === true 也走这条红——被中断是
// 「需要恢复、可以重派」，与「跑失败了」在颜色上是同一件事（文案上才分开，见行内 `· 可重派`）。
// 为什么必须有这一个判据：颜色若在几处各算一遍，行框、色点、chip 就会各说各话。
export function effStatus(item) {
  if (item?.interrupted === true) return "failed";
  const status = itemStatus(item);
  if (status === "failed" || status === "killed") return "failed";
  if (status === "review") return "review";
  if (status === "done") return "done";
  if (status === "running") return "running";
  return "pending";
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
//
// `allByID` 是**全计划**的工作项索引（ganttModel 传进来；单独调用时缺省退回本里程碑）。
// 三个字段各说各的那一件事（独立验证 ③：以前一个 byID 身兼三职，"在别的里程碑里"被读成
// "不存在"——既写「依赖缺失」，又永久 `blocked`，而几何那边 `slotOf` 用的是全计划索引、
// 明明按跨里程碑算了槽位，同一件事三个说法）：
//   missing_deps = **全计划里根本不存在**的 id（真写错了，只有这一类才配叫"依赖缺失"）；
//   cross_deps   = 全计划里存在、但**不在本里程碑**的 id（编排口径只允许同里程碑内）；
//   blocked      = 按**全计划**的状态判：存在且 done 才算放行（取不到 = 没 done = 卡住）。
export function orderWorkItems(items, allByID = null) {
  const list = (Array.isArray(items) ? items : []).filter(item => item && item.id);
  const ids = new Set(list.map(item => String(item.id)));
  const byID = new Map(list.map(item => [String(item.id), item]));
  const scope = allByID instanceof Map ? allByID : byID;
  const depth = new Map();
  const ordered = [];
  let frontier = list.filter(item => itemDepsOf(item).filter(dep => ids.has(dep)).length === 0);
  for (const item of frontier) depth.set(String(item.id), 0);
  while (frontier.length) {
    for (const item of frontier) ordered.push(workItemEntry(item, byID, depth.get(String(item.id)), false, scope));
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
    if (!depth.has(String(item.id))) ordered.push(workItemEntry(item, byID, 0, true, scope));
  }
  return ordered;
}

function workItemEntry(item, byID, depth, cyclic = false, scope = byID) {
  const deps = itemDepsOf(item);
  return {
    item,
    id: String(item.id),
    depth,
    cyclic,
    depends_on: deps,
    missing_deps: deps.filter(dep => !scope.has(dep)),
    cross_deps: deps.filter(dep => !byID.has(dep) && scope.has(dep)),
    blocked: deps.some(dep => itemStatus(scope.get(dep)) !== "done"),
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
  // 甘特的边要**实测**（行高由内容撑，见文件头 x/y 两条轴的口径）：字符串给不出 y，等调用方
  // 把这段 HTML 插进 DOM 之后，由这里排一次实测布局把 top/height 写实（没有 DOM 时是空操作）。
  // 调用姿势不变：签名不动、app.js 也不用改。
  scheduleTeamGanttLayout();
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

// ── 里程碑·工作项真甘特：几何（x = CSS calc 槽位；y = **实测**）───────────────────
//
// 这一节把「里程碑 · 工作项」画成**真甘特几何**：左侧任务名列 + 顶部刻度尺（横轴 = 依赖
// 槽位，不是时间）+ 每行一根横条 + 里程碑汇总条 + finish→start 的正交折线箭头；里程碑之间
// 的屏障画在**汇总条之间**（同一套语法）。
//
// 上一版的形状是「按依赖排序的满宽卡片列表 + 左侧 gutter 连线」：没有 x 轴、没有条形、没有
// 错峰的起点（用户判「很奇怪」）。本节就是那句判词的落点。
//
// 两条轴的**事实来源故意不同**（2026-10-08 · wi-gantt-fit）：
//   - **x 是算式**：`labelW + 槽位 × slotW`，写成 CSS `calc()`（槽位/常量走 var）。窄栏容器
//     查询把几何压小时，条、汇总条、边一起缩——不用量、不用重绘、不写第二套数字。
//   - **y 是量出来的**：行高由**内容**撑（字多行就长，见 .team-dag-row 的 min-height 口径），
//     于是「y = 前面那些块的高度之和」根本不是一条算得出来的算式（行高不是常量，框高更不是
//     「行数 × 常量」）。所以边的 y 一律在 DOM 插好之后**实测**（行 / 汇总条那一行相对
//     `.team-dag-content` 顶边的位置），见 layoutTeamGanttEdges。渲染字符串里那段 top/height
//     只是**占位**，整层 `.team-dag-edges` 在量出来之前 visibility: hidden —— 宁可先不画，
//     也不画一条假线（与"退化的边不画"同一条口径）。
const GANTT = {
  arrow: 7, // 箭头长度（px）：x 上把箭尖退回目标条左沿，CSS 的 border-left 同值
  hook: 7, // 同槽（gap = 0）时绕行钩横向伸出的量（px）
  shelf: 12, // 绕行钩的"搁板"落在目标条中心下方多少 px（条高一半才 8~9px，所以落在条下）
  lineHalf: 0.75, // 线段沿锚点**居中**画时各让的一半线宽（px）
  arrowHalf: 3.5, // 箭头是 7×7 的 border 三角形：算式给的是它的上沿（中心 = 上沿 + 3.5）
};

// X_VARS 是 x 的 calc 里用到的变量名。顺序写死，同一份输入算出来的字符串才逐字相同（幂等）。
// **没有 Y_VARS**：y 不再写成 calc 算式（见上面的口径）。
const X_VARS = ["--team-dag-label-w", "--team-dag-slot-w"];

// span = 「一段坐标 = 常量(px) + Σ 系数 × CSS 变量」。为什么要这层抽象：边的位置必须**跟着
// 窄栏容器查询一起缩**，而渲染件量不到 rect —— 于是把几何写成 calc，让浏览器按同一套变量
// 自己算。系数是整数（半行写成 0.5），结果稳定、可逐字比对。
function span(px = 0, vars = {}) {
  return { px, vars };
}

function spanAdd(...list) {
  const out = span();
  for (const item of list) {
    if (!item) continue;
    out.px += item.px;
    for (const name of Object.keys(item.vars)) out.vars[name] = (out.vars[name] || 0) + item.vars[name];
  }
  return out;
}

function spanNeg(item) {
  const vars = {};
  for (const name of Object.keys(item.vars)) vars[name] = -item.vars[name];
  return span(-item.px, vars);
}

function spanSub(a, b) {
  return spanAdd(a, spanNeg(b));
}

// spanNum 把常量/系数固定到两位小数：不加它，浮点误差会让两次渲染的字符串不同（幂等就破）。
// 实测坐标（layoutTeamGanttEdges）也用同一个刻度——两边得是同一把尺子。
function round2(value) {
  return Math.round(Number(value) * 100) / 100;
}

// spanCSS 把 span 写成 CSS 长度：`calc(4px + 1 * var(--team-dag-slot-w))`。
function spanCSS(item, order) {
  const parts = [`${round2(item.px)}px`];
  for (const name of order) {
    const factor = item.vars[name];
    if (!factor) continue;
    parts.push(`${factor > 0 ? "+" : "-"} ${round2(Math.abs(factor))} * var(${name})`);
  }
  return parts.length === 1 ? parts[0] : `calc(${parts.join(" ")})`;
}

function xCSS(item) {
  return spanCSS(item, X_VARS);
}

// spanPositive 判一段**横向**长度是否可能为正（常量 > 0，或某个槽位变量的系数 > 0）。长度
// 恒 0/负的横段**不画**：宁可少一段线，也不要画一段读起来像真依赖、其实什么都不表示的退化线。
// （纵向长度在这里判不了：它是实测出来的，渲染时还不知道——见 layoutTeamGanttEdges。）
function spanPositive(item) {
  if (item.px > 0) return true;
  return X_VARS.some(name => (item.vars[name] || 0) > 0);
}

// xSlot 是「第 k 个依赖槽位的左边界」：x = labelW + k * slotW。
function xSlot(k) {
  return span(0, { "--team-dag-label-w": 1, "--team-dag-slot-w": k });
}

// BAR_SEAM 是相邻槽之间留的 2px 视觉缝：条的右端 = 槽右边界 - 2px。
const BAR_SEAM = span(-2);

// PLOT_INSET 是"箭尖顶到绘图区左沿"时留下的那点余量：竖线宽 1.5px 是**居中**画的
// （left = 锚点 − 0.75px），框又有 1px 边框（框内绘图区实际从 labelW + 1 起），所以锚点要
// 落在左沿内侧 ≥1.75px 才两头都不越界。取整数 2px。
const PLOT_INSET = span(2);

// xApproach 是接近段的 x（箭尖锚点）：正常 = 目标条左端 − 箭头长（箭头尖正好落在条左沿）。
// 目标锚点在**槽 0** 时条左端就在绘图区左沿上，再减一个箭头长会把「竖线 + 最后一段横线 +
// 箭头」整段塞进任务名列（独立验证 ②：越列 7.75px）。这一档把接近段顶到绘图区左沿**内侧**：
// 箭头整体留在槽 0 里，箭尖朝右指进条里。
function xApproach(slot) {
  const k = Math.max(0, Number(slot) || 0);
  return k >= 1 ? spanAdd(xSlot(k), span(-GANTT.arrow)) : spanAdd(xSlot(0), PLOT_INSET);
}

// slotOf 是 FS（finish→start）语义的槽位：无依赖 = 0，否则 max(slot(d) + dur(d))，dur 恒 1
// —— 紧贴最后一个前驱的右端。成环走 seen 集合兜底当 0（不假造槽位）；**自指依赖不占槽位**
// ——它不表示任何先后关系，占一格只会让这一行凭空右移一格（与"不画退化自环"同一条口径）。
function slotOf(id, byID, seen = new Set()) {
  const key = String(id);
  const item = byID.get(key);
  if (!item || seen.has(key)) return 0;
  const deps = itemDepsOf(item).filter(dep => dep !== key && byID.has(dep));
  if (!deps.length) return 0;
  const next = new Set(seen);
  next.add(key);
  return Math.max(...deps.map(dep => slotOf(dep, byID, next) + 1));
}

// hasUndoneDep 是**屏障**的单一判据：depends_on 里只要有一个不是 done（含指向不存在的
// 里程碑）就算没放行。它同时决定框的 🔒/🔓 与两块之间的闸门带开合——一处判定，两处显示。
function hasUndoneDep(frames, deps) {
  return (Array.isArray(deps) ? deps : []).some(dep => {
    const owner = frames.find(frame => frame.id === dep);
    return !owner || owner.status !== "done";
  });
}

// ganttModel 把计划排成甘特模型：每行一个工作项（`slot` = 起点槽位、`dur` 恒 1；**计划里没有
// 工时事实，条长不编**）、每个里程碑一根汇总条（横跨名下条目的 min(start)..max(end)）、
// 外加每个行/汇总结点的内容坐标 y 算式。
//
// 行序 = 依赖拓扑序（里程碑之间按 milestones[].depends_on，里程碑内部按
// work_items[].depends_on）；同层并行、并列回落声明序（orderMilestones / orderWorkItems 保证）。
// `slot` 与 orderWorkItems 给的 depth 是同一个数（依赖图上的最长路径长度）——"被依赖者在前"
// 在几何上就是这个等式，单测里钉住它。
export function ganttModel(plan) {
  const byID = new Map(workItemsOf(plan).map(item => [String(item.id), item]));
  const ordered = orderMilestones(plan);
  const frames = ordered.map((frame, layer) => ({
    ...frame,
    layer,
    tone: layer % 5,
    locked: hasUndoneDep(ordered, frame.depends_on),
    rows: [],
    sum: { s: 0, e: 0, empty: true },
  }));
  for (const frame of frames) {
    // 全计划 byID 传进去：行上的「依赖缺失 / 跨里程碑依赖 / 被卡住」三条判据都要看全计划
    // （几何那边的 slotOf 用的也是它），否则同一件事会被说成三个样（独立验证 ③）。
    for (const entry of orderWorkItems(frame.items, byID)) {
      const slot = slotOf(entry.id, byID);
      // depth 也用几何槽位：跨里程碑的工作项依赖（设计稿夹具里就有）会让 orderWorkItems 的
      // 里程碑内层号与几何位置分叉，而条的位置才是这一节要说的那件事。
      frame.rows.push({ ...entry, milestone_id: frame.id, layer: frame.layer, slot, dur: 1, end: slot + 1, depth: slot });
    }
  }
  const rows = frames.flatMap(frame => frame.rows);
  const frameByID = new Map(frames.map(frame => [frame.id, frame]));
  for (const frame of frames) {
    if (!frame.rows.length) continue;
    frame.sum = {
      s: Math.min(...frame.rows.map(row => row.slot)),
      e: Math.max(...frame.rows.map(row => row.end)),
      empty: false,
    };
  }
  // 空里程碑不画空条：零宽菱形落在它屏障前驱汇总条的**右端**（无前驱 → 槽 0）。
  for (const frame of frames) {
    if (!frame.sum.empty) continue;
    let at = 0;
    for (const dep of frame.depends_on) {
      const owner = frameByID.get(dep);
      if (owner) at = Math.max(at, owner.sum.e);
    }
    frame.sum = { s: at, e: at, empty: true };
  }
  let slots = 1;
  for (const row of rows) slots = Math.max(slots, row.end);
  for (const frame of frames) slots = Math.max(slots, frame.sum.e);
  return { frames, rows, slots, edges: ganttEdges(frames, rows) };
}

// ganttEdges 把依赖折成边：工作项边（前驱条**右端** → 后继条**左端**，标准 finish→start）与
// 里程碑边（被依赖汇总条右端 → 依赖方汇总条左端，同一套语法）。
//
// 两种边**不画**：指向计划里不存在的东西（端点画不出来——缺依赖由行上的告警 chip 显形），
// 以及**指向自己**（只会画出一圈退化自环，看着像条真边，其实什么也不表示）。空里程碑没有
// 汇总条，它的边也不画（端点不存在）。
function ganttEdges(frames, rows) {
  const rowByID = new Map(rows.map(row => [row.id, row]));
  const frameByID = new Map(frames.map(frame => [frame.id, frame]));
  const edges = [];
  for (const row of rows) {
    let lane = 0;
    for (const dep of row.depends_on) {
      const source = rowByID.get(dep);
      if (!source || source === row) continue;
      edges.push({
        kind: "item",
        from: source.id,
        to: row.id,
        gap: row.slot - source.end,
        lane: Math.min(lane, 2),
        src: { x: spanAdd(xSlot(source.end), BAR_SEAM), y: `row:${source.id}` },
        dst: { x: xSlot(row.slot), slot: row.slot, y: `row:${row.id}` },
      });
      lane += 1;
    }
  }
  for (const frame of frames) {
    let lane = 0;
    for (const dep of frame.depends_on) {
      const source = frameByID.get(dep);
      if (!source || source === frame || source.sum.empty || frame.sum.empty) continue;
      edges.push({
        kind: "milestone",
        from: source.id,
        to: frame.id,
        gap: frame.sum.s - source.sum.e,
        lane: Math.min(lane, 2),
        src: { x: xSlot(source.sum.e), y: `sum:${source.id}` },
        dst: { x: xSlot(frame.sum.s), slot: frame.sum.s, y: `sum:${frame.id}` },
      });
      lane += 1;
    }
  }
  return edges;
}

// yref = 「一段线的纵向位置 = 某个**锚点**（工作项行 / 里程碑汇总条那一行）+ 一个像素微调」。
// 锚点的像素位置渲染件给不出来（行高由内容撑），所以这里只记锚点 id 与微调量；真正的位置由
// layoutTeamGanttEdges 量出来写进 style。微调只有三种：线宽居中（−0.75）、箭头上沿（−3.5）、
// 绕行钩搁板（+12）。
function yref(anchor, dy = 0) {
  return { a: anchor, dy };
}

// edgeSegs 把一个边折成**几段线**的结构描述（纯数据：x 是 span 算式，y 是锚点引用）。
// 两种边的走法（与修之前一模一样，只是 y 由算式换成了锚点）：
//   - 有槽位富余（gap ≥ 1）：横着走到目标条左侧 → 垂直落下 → 进条；
//   - 同槽（gap = 0，紧贴的 finish→start）：直线会退化成零长线，往下绕一个钩（右伸 → 落到
//     目标条下方 → 折回条左侧 → 抬上来 → 进条）；lane 让同一行的多条钩错开高度。
function edgeSegs(edge) {
  const segs = [];
  const horizontal = (fromX, width, y) => {
    if (!spanPositive(width)) return;
    segs.push({ kind: "h", x: fromX, w: width, y });
  };
  const vertical = (atX, y1, y2) => {
    segs.push({ kind: "v", x: atX, y1, y2 });
  };
  // 箭头占 [tip, tip + arrow]，箭头尖正好落在目标条的左端（槽 0 那一档顶到绘图区内侧，
  // 见 xApproach —— 否则整段越列到任务名列里）。
  const tipX = xApproach(edge.dst.slot);
  if (edge.gap >= 1) {
    horizontal(edge.src.x, spanSub(tipX, edge.src.x), yref(edge.src.y, -GANTT.lineHalf));
    vertical(tipX, yref(edge.src.y), yref(edge.dst.y));
  } else {
    const reach = GANTT.hook + edge.lane * 6;
    const hookX = spanAdd(edge.src.x, span(reach));
    horizontal(edge.src.x, span(reach), yref(edge.src.y, -GANTT.lineHalf));
    vertical(hookX, yref(edge.src.y), yref(edge.dst.y, GANTT.shelf));
    horizontal(tipX, spanSub(hookX, tipX), yref(edge.dst.y, GANTT.shelf - GANTT.lineHalf));
    vertical(tipX, yref(edge.dst.y), yref(edge.dst.y, GANTT.shelf));
  }
  segs.push({ kind: "arrow", x: tipX, y: yref(edge.dst.y, -GANTT.arrowHalf) });
  return segs;
}

// 段线的占位几何：**渲染时还不知道** y（行高由内容撑，见文件头的口径），所以 top/height 先写
// 0px，等 layoutTeamGanttEdges 量完再写成 px。x 相反——它是槽位算式，这里就写死。
const Y_PLACEHOLDER = "0px";

function segHTML(seg) {
  if (seg.kind === "h") {
    return `<i class="team-dag-edge-seg is-h" style="left:${xCSS(seg.x)};top:${Y_PLACEHOLDER};width:${xCSS(seg.w)}" data-y="${escapeHtml(seg.y.a)}" data-y-dy="${round2(seg.y.dy)}"></i>`;
  }
  if (seg.kind === "v") {
    return `<i class="team-dag-edge-seg is-v" style="left:${xCSS(spanAdd(seg.x, span(-GANTT.lineHalf)))};top:${Y_PLACEHOLDER};height:${Y_PLACEHOLDER}" data-y="${escapeHtml(seg.y1.a)}" data-y-dy="${round2(seg.y1.dy)}" data-y2="${escapeHtml(seg.y2.a)}" data-y2-dy="${round2(seg.y2.dy)}"></i>`;
  }
  return `<i class="team-dag-edge-arrow" style="left:${xCSS(seg.x)};top:${Y_PLACEHOLDER}" data-y="${escapeHtml(seg.y.a)}" data-y-dy="${round2(seg.y.dy)}"></i>`;
}

// renderGanttEdges 把边画成一组**绝对定位的线段**（不是一条会被 sticky 框头盖住的整张 SVG）：
// x 是 calc 算式（跟着容器查询一起缩），y 是**实测占位**（见上面的口径）。每段线都把自己的
// 纵向锚点写在 data 属性里，交给 layoutTeamGanttEdges。
//
// 颜色一律中性（--team-dag-edge）：**不借状态色、不借 teammate 色**——线只说"谁依赖谁"，
// 状态由条描边说，归属由条填充说，三条通道各说各的。
function renderGanttEdges(model) {
  const parts = [];
  for (const edge of model.edges) {
    const segs = edgeSegs(edge).map(segHTML).join("");
    parts.push(`<span class="team-dag-edge" data-edge="${escapeHtml(`${edge.from}->${edge.to}`)}" data-kind="${edge.kind}" data-gap="${edge.gap}">${segs}</span>`);
  }
  return parts.join("");
}

// ── 边的**实测布局**（wi-gantt-fit）────────────────────────────────────────
//
// 行高由内容撑（字多行就长、框跟着长），所以「y = 前面那些块的高度之和」这条算式不成立了：
// 边的 y 只能量。量的对象是**纵向锚点**：
//   - `row:<工作项 id>`  = 那一行 `.team-dag-row` 的实测中心；
//   - `sum:<里程碑 id>`  = 那一行 `.team-dag-ms-sum`（汇总条所在行）的实测中心；
// 两者都相对 `.team-dag-content` 的顶边——内容坐标，与滚动位置无关（一起滚，差值不变）。
// 然后按每段线自己的锚点 + 微调，把 `top`（竖段还有 `height`）写成 px。x 一律不动：x 是槽位
// 算式，与行高无关，窄栏压几何时还得跟着缩。
//
// 三条不变式：
//   1. **没有 DOM 就是空操作**：拿不到 root，或 root 不像 DOM（node:test）→ 返回 0，不抛；
//   2. **幂等**：同一份实测连跑两次，写出来的坐标逐字相同（数字固定两位小数）；
//   3. **可桩测**：这个函数只认一个"像 DOM 的对象"（querySelectorAll / querySelector /
//      getBoundingClientRect / getAttribute / children / style），单测拿桩测量就能驱动它，
//      不必起浏览器。渲染件本身仍然是纯字符串函数（不碰真 DOM）。
export function layoutTeamGanttEdges(root) {
  if (!root || typeof root.querySelectorAll !== "function") return 0;
  let patched = 0;
  for (const scroll of Array.from(root.querySelectorAll(".team-dag-scroll") || [])) {
    if (!scroll || typeof scroll.querySelector !== "function") continue;
    const content = scroll.querySelector(".team-dag-content");
    if (!content || typeof content.getBoundingClientRect !== "function") continue;
    const contentTop = content.getBoundingClientRect().top;
    const anchors = new Map();
    const centerOf = el => {
      const rect = el.getBoundingClientRect();
      return round2(rect.top - contentTop + rect.height / 2);
    };
    for (const row of Array.from(scroll.querySelectorAll(".team-dag-row") || [])) {
      const id = row.getAttribute("data-item-id");
      if (id) anchors.set(`row:${id}`, centerOf(row));
    }
    for (const row of Array.from(scroll.querySelectorAll(".team-dag-ms-sum") || [])) {
      const id = row.getAttribute("data-ms");
      if (id) anchors.set(`sum:${id}`, centerOf(row));
    }
    for (const edge of Array.from(scroll.querySelectorAll(".team-dag-edge") || [])) {
      patched += patchEdgeY(edge, anchors);
    }
    // 量过了才显示（没量之前整层 visibility: hidden，见 TEAM_BOARD_CSS）：宁可先不画，
    // 也不画一条停在 0px 的假线。
    const layer = scroll.querySelector(".team-dag-edges");
    if (layer && typeof layer.setAttribute === "function") layer.setAttribute("data-laid-out", "true");
  }
  return patched;
}

// patchEdgeY 按实测锚点重写一条边里每段线的 y；锚点缺（DOM 里没有那一行）就跳过这一段
// ——保持占位，不猜一个位置出来。
function patchEdgeY(edge, anchors) {
  let patched = 0;
  for (const seg of Array.from((edge && edge.children) || [])) {
    if (!seg || !seg.style || typeof seg.getAttribute !== "function") continue;
    const a = anchors.get(seg.getAttribute("data-y") || "");
    if (a === undefined) continue;
    const top = round2(a + numAttr(seg, "data-y-dy"));
    seg.style.top = `${top}px`;
    const b = anchors.get(seg.getAttribute("data-y2") || "");
    // 竖段的**高度**也是实测差：两个锚点换了位置，长度跟着变（不是常量）。
    if (b !== undefined) seg.style.height = `${Math.max(0, round2(b + numAttr(seg, "data-y2-dy") - top))}px`;
    patched += 1;
  }
  return patched;
}

function numAttr(el, name) {
  const value = Number(el.getAttribute(name));
  return Number.isFinite(value) ? value : 0;
}

// scheduleTeamGanttLayout 在**有 DOM 时**排一次实测：渲染件是纯字符串函数，不知道自己会被插进
// 哪棵树，所以只能在渲染完之后、浏览器把这一帧交回来时量（微任务 + 一帧后各排一次；同一帧里
// 连渲多次只排一次）。**没有 DOM（node:test）直接返回 false，什么都不做**——单测里的渲染件
// 必须是纯的。
let layoutPending = null;

export function scheduleTeamGanttLayout() {
  if (typeof document === "undefined" || !document || typeof document.querySelectorAll !== "function") return false;
  if (layoutPending) return true;
  const run = () => {
    if (layoutPending !== run) return;
    layoutPending = null;
    layoutTeamGanttEdges(document);
    observeTeamGanttContents(document);
  };
  layoutPending = run;
  if (typeof queueMicrotask === "function") queueMicrotask(run);
  else Promise.resolve().then(run);
  if (typeof requestAnimationFrame === "function") requestAnimationFrame(run);
  return true;
}

// observeTeamGanttContents 盯住每块看板的**内容高度**：换字号、窄栏让名字少折/多折一行、
// 字体加载完——凡是会改行高的，都再量一次。量只写 top/height（绝对定位的线段不占位），
// 不会改变内容尺寸，所以不会自激。
const OBSERVED_CONTENTS = typeof WeakSet === "function" ? new WeakSet() : null;

let ganttObserver = null;

function observeTeamGanttContents(root) {
  if (typeof ResizeObserver !== "function") return;
  if (!ganttObserver) ganttObserver = new ResizeObserver(() => { layoutTeamGanttEdges(document); });
  for (const content of Array.from(root.querySelectorAll(".team-dag-content") || [])) {
    if (!OBSERVED_CONTENTS || OBSERVED_CONTENTS.has(content)) continue;
    OBSERVED_CONTENTS.add(content);
    ganttObserver.observe(content);
  }
}

// renderTeamGantt 是**里程碑 · 工作项**那一节：刻度尺（sticky）→ 竖网格线 → 依赖边 →
// 里程碑大框（框头 + 汇总条 + 名下的工作项条）→ 闸门带 → 下一个框…… 横轴是**依赖槽位，
// 不是时间**；刻度尺上就写着这句话，免得读的人自己脑补出工期。
export function renderTeamGantt(plan) {
  const model = ganttModel(plan);
  if (!model.frames.length) return "";
  const ticks = [];
  for (let i = 0; i <= model.slots; i += 1) {
    ticks.push(`<span class="team-dag-tick" data-tick="${i}" style="--i:${i}"><i></i><b>${i}</b></span>`);
  }
  const body = model.frames.map((frame, index) => renderGanttFrame(frame)
    + (index < model.frames.length - 1 ? renderGanttGate(frame, model.frames[index + 1]) : "")).join("");
  return `<section class="team-section" data-team-gantt>
      <div class="team-section-title"><span>里程碑 · 工作项</span><span class="chip team-count">${model.frames.length}</span></div>
      <div class="team-dag-scroll">
        <div class="team-dag-content" style="--team-dag-slots:${model.slots}">
          <div class="team-dag-ruler" data-role="ruler">
            <span class="team-dag-ruler-label">任务（id · name）</span>
            <div class="team-dag-ruler-plot">${ticks.join("")}<span class="team-dag-ruler-basis">横轴 = 依赖槽位（非时间）· 1 槽 = 1 层依赖 · 条长恒 1 槽</span></div>
          </div>
          <div class="team-dag-grid" aria-hidden="true"></div>
          <div class="team-dag-edges" aria-hidden="true" data-laid-out="false">${renderGanttEdges(model)}</div>
          ${body}
        </div>
      </div>
    </section>`;
}

// renderGanttFrame 渲染一个里程碑大框：极淡填充 + 自己的淡色描边（逐框轮换，不吃状态色），
// 框头一行读得到 id · name · L<n>（屏障层号）· status chip · 屏障 deps · 判据 · 🔒/🔓 ·
// n items · content（口径 4）；框内第一行是**汇总条**（横跨名下条目的总跨度，两端向下短折），
// 空里程碑画零宽菱形。
function renderGanttFrame(frame) {
  const deps = frame.depends_on.length
    ? frame.depends_on.map(dep => `<span class="chip team-dep">${escapeHtml(dep)}</span>`).join("")
    : '<span class="muted">—</span>';
  const after = frame.after.length
    ? `<span class="team-label">判据</span>${frame.after.map(dep => `<span class="chip team-dep">${escapeHtml(dep)}</span>`).join("")}`
    : "";
  const missing = frame.missing_deps.length
    ? `<span class="team-dag-warn" title="depends_on 指向计划里不存在的里程碑">依赖缺失：${escapeHtml(frame.missing_deps.join("、"))}</span>`
    : "";
  const cyclic = frame.cyclic
    ? '<span class="team-dag-warn" title="depends_on 成环，层号归 0 并接在末尾">依赖成环</span>'
    : "";
  const empty = frame.rows.length ? "" : '<span class="team-dag-ms-empty">尚未排活</span>';
  const content = frame.content
    ? `<span class="team-dag-ms-content" title="${escapeHtml(frame.content)}">${escapeHtml(truncate(frame.content, CONTENT_LIMIT))}</span>`
    : "";
  // 屏障没放行 = 虚线 + 降透明度 + 🔒 待解锁（口径 5）——三样一起写，少一样都读不出"进不去"。
  const lock = frame.locked
    ? '<span class="team-dag-lock" title="屏障未放行：depends_on 里还有没 done 的里程碑（现在进不去）">🔒 待解锁</span>'
    : `<span class="team-dag-lock is-open">${frame.status === "done" ? "✅ 已完成" : "🔓 已解锁"}</span>`;
  const sum = frame.sum;
  // data-layer = 这一个框在**拓扑排序里的位置**（排序事实）；框头 `L<n>` = 屏障**深度**
  // （依赖链最长路径）。两者在链式计划里恰好相同，而在「两个互不依赖的里程碑」或成环时
  // 会分叉（独立验证 F2）——所以两个数都挂出来，别让读的人以为它们必然是同一个数。
  return `<section class="team-dag-frame" data-milestone-id="${escapeHtml(frame.id)}" data-status="${escapeHtml(frame.status)}" data-layer="${escapeHtml(String(frame.layer))}" data-locked="${frame.locked ? "true" : "false"}" data-depth="${escapeHtml(String(frame.depth))}" data-ms="${escapeHtml(frame.id)}" data-sum-start="${sum.s}" data-sum-end="${sum.e}" data-empty="${sum.empty ? "true" : "false"}" style="--team-dag-ms-tone:var(--team-dag-ms-tone-${frame.tone})">
        <header class="team-dag-frame-head">
          <span class="team-dag-ms-id">${escapeHtml(frame.id)}</span>
          ${frame.name ? `<span class="team-dag-ms-name">${escapeHtml(frame.name)}</span>` : ""}
          <span class="team-dag-layer" title="屏障层号 L${escapeHtml(String(frame.depth))}（同层 = 依赖边允许并行）">L${escapeHtml(String(frame.depth))}</span>
          <span class="team-status is-${escapeHtml(frame.status)}">${escapeHtml(frame.status)}</span>
          <span class="team-label">屏障</span>${deps}${after}
          ${lock}
          <span class="team-dag-ms-count">${frame.rows.length} items</span>
          ${missing}${cyclic}${empty}${content}
        </header>
        <div class="team-dag-ms-sum" data-ms="${escapeHtml(frame.id)}">
          <span class="team-dag-ms-sum-label">milestone</span>
          <div class="team-dag-plot">
            <span class="team-dag-sum-bar" data-ms="${escapeHtml(frame.id)}" data-empty="${sum.empty ? "true" : "false"}" style="--s:${sum.s};--e:${sum.e}"><i class="team-dag-sum-cap team-dag-sum-cap-l"></i><i class="team-dag-sum-cap team-dag-sum-cap-r"></i></span>
            <span class="team-dag-sum-diamond" data-ms="${escapeHtml(frame.id)}" data-empty="${sum.empty ? "true" : "false"}" style="--s:${sum.s};--e:${sum.e}"></span>
            <span class="team-dag-sum-note">槽 ${sum.s}–${sum.e}${sum.empty ? " · 空" : ""}</span>
          </div>
        </div>
        <div class="team-dag-rows">${frame.rows.map(row => renderTeamWorkItem(row)).join("")}</div>
      </section>`;
}

// renderGanttGate 渲染两块之间的**闸门带**：横向虚线 + 「上一层全部 done 才放行（等 deps）」；
// 放行后转绿并写「闸门已放行」（口径 5）。
function renderGanttGate(frame, next) {
  const deps = Array.isArray(next.depends_on) ? next.depends_on : [];
  const open = !next.locked;
  // 文案只说**判据本身**（独立验证 F1：旧文案在放行时说「已放行 → <上一块> 全部 done」，
  // 可放行判据看的是**下一块**的 depends_on —— 当下一块压根没声明依赖（deps 为空）时，
  // 那句话就是假话：上一块还没 done，闸门却写「已放行 → 它全部 done」。所以：
  //   没声明依赖 → 说明这里根本没有屏障；放行 → 报出**被等的那些 deps**，不报上一块的名字。
  const label = deps.length === 0
    ? `无屏障 → ${next.id} 未声明 depends_on（谁都关不住）`
    : open
      ? `闸门已放行 → ${deps.join(" + ")} 全部 done`
      : `闸门 → 上一层全部 done 才放行（等 ${deps.join(" + ")}）`;
  return `<div class="team-dag-gate" data-gate="${escapeHtml(`${frame.id}->${next.id}`)}" data-open="${open ? "true" : "false"}">
        <span class="team-dag-gate-line"></span>
        <span class="team-dag-gate-label">${escapeHtml(label)}</span>
        <span class="team-dag-gate-line"></span>
      </div>`;
}

// renderTeamWorkItem 渲染**一个工作项 = 一行**：左边名列（id · 名称（可点进子页面）· 依赖
// 与三个标记），右边绘图区（条左端上方的 depends_on 小字 + 一根横条 + 条后的 role / 状态 /
// 会话 chip）。
//
// 两条颜色通道在这一行里正交（口径 3/13）：**条描边 = 状态**（data-eff；pending 另加虚线），
// **条填充（14% 淡色）+ 条左端 4px 实色 cap + role chip 的小色点 = 归属（teammate）**；
// 行文字一律中性色，不拿状态色染整行。
//
// 信息不许丢（口径 9）：达成目标 / 描述 / 结论 / 会话 / 工作区与三个标记的布尔值既进这一行的
// `title`，也在 DOM 里留一份（`.team-dag-full`，视觉隐藏）—— 窄栏折叠次级信息时读不到的那几项，
// 仍然能被读到。
export function renderTeamWorkItem(entry) {
  const item = entry?.item || {};
  const status = itemStatus(item);
  const eff = effStatus(item);
  const role = String(item.role || "").trim();
  // slot 优先用模型算好的起点槽位；单独调用（只有 orderWorkItems 的 depth）时退到 depth ——
  // 两者在依赖图上是同一个数（见 ganttModel 的注释）。
  const slot = Number(entry?.slot ?? entry?.depth ?? 0);
  const dur = Number(entry?.dur ?? 1);
  const session = String(item.session_id || "").trim();
  const worktree = String(item.worktree || "").trim();
  const goal = String(item.goal || "").replace(/\s+/g, " ").trim();
  const description = String(item.description || "").replace(/\s+/g, " ").trim();
  const note = String(item.note || "").replace(/\s+/g, " ").trim();
  const deps = Array.isArray(entry?.depends_on) ? entry.depends_on : itemDepsOf(item);
  const missing = Array.isArray(entry?.missing_deps) ? entry.missing_deps : [];
  const cross = Array.isArray(entry?.cross_deps) ? entry.cross_deps : [];
  const depLine = deps.length ? `depends_on: ${deps.join(", ")} → 槽 ${slot}` : `无依赖 → 槽 0`;
  const missingLine = missing.length ? ` · 缺失依赖: ${missing.join(", ")}` : "";
  const crossLine = cross.length ? ` · 跨里程碑: ${cross.join(", ")}` : "";
  const depNote = `${depLine}${missingLine}${crossLine}`;
  const marks = [
    entry?.blocked ? '<span class="chip team-blocked" title="前置工作项还没验收通过：现在派发会被闸门拒">被依赖卡住</span>' : "",
    item.interrupted === true ? '<span class="chip team-interrupted" title="状态说在跑、而本进程的作业表里查不到它的句柄（jobs I-4）。可以重派——会话与现场都还在">可重派</span>' : "",
    item.live ? '<span class="chip team-live" title="这件事现在真的有一份未释放的工作区绑定">现场在</span>' : "",
  ].filter(Boolean).join("");
  const flags = [
    ["blocked", Boolean(entry?.blocked), "还有前驱没 done：现在派发会被闸门拒"],
    ["interrupted", item.interrupted === true, "中断待恢复（可重派）"],
    ["live", Boolean(item.live), "真的有一份未释放的工作区绑定"],
  ].map(([flag, on, tip]) => `<span class="team-dag-flag" data-flag="${flag}" data-on="${on ? "true" : "false"}" title="${escapeHtml(tip)}">${flag}</span>`).join("");
  const missingChip = missing.length
    ? `<span class="team-dag-warn" title="depends_on 指向**计划里**不存在的工作项（写错了 id）">依赖缺失：${escapeHtml(missing.join("、"))}</span>`
    : "";
  // 跨里程碑依赖：几何按**全计划**算了槽位（slotOf 用的就是全计划索引），所以这一行会被
  // 推到槽位上；但编排口径只允许同里程碑内 —— 两个说法都要摆出来，不能只报一个（独立验证 ③）。
  const crossChip = cross.length
    ? `<span class="team-dag-warn" title="这条依赖落在**别的里程碑**里：几何（槽位）按全计划算了，但编排口径只允许同里程碑内 —— 派活时这条边作废">跨里程碑依赖：${escapeHtml(cross.join("、"))}（只允许同里程碑内）</span>`
    : "";
  const cyclic = entry?.cyclic
    ? '<span class="team-dag-warn" title="depends_on 成环，层号归 0 并接在末尾">依赖成环</span>'
    : "";
  const name = String(item.name || item.id || "");
  const label = session
    ? `<button type="button" class="team-dag-name is-openable" data-team-item-open="${escapeHtml(String(item.id || ""))}" data-team-item-session="${escapeHtml(session)}" data-team-item-role="${escapeHtml(role)}" data-tip="查看这件事的执行进度（子页面）">${escapeHtml(name)}</button>`
    : `<span class="team-dag-name">${escapeHtml(name)}</span>`;
  // 全文：既进 title，也在 DOM 里留一份（口径 9）。
  const full = [
    `id: ${String(item.id || "")}`,
    `role: ${role || "—"}`,
    `status: ${status}`,
    `depends_on: ${deps.length ? deps.join(", ") : "—"}`,
    `cross_deps: ${cross.length ? cross.join(", ") : "—"}`,
    `missing_deps: ${missing.length ? missing.join(", ") : "—"}`,
    `slot: ${slot}`,
    `session_id: ${session || "—"}`,
    `worktree: ${worktree || "—"}`,
    `goal: ${goal || "—"}`,
    `description: ${description || "—"}`,
    `note: ${note || "—"}`,
    `blocked: ${entry?.blocked === true}`,
    `interrupted: ${item.interrupted === true}`,
    `live: ${item.live === true}`,
  ].join("; ");
  // teammate 色只在这一处（行上的变量）+ role chip 色点用；slot 超过 6（色板回绕）时
  // 条左端的 cap 叠斜纹第二通道 —— 防撞色，不是装饰（口径 11）。
  const wrapped = role && roleSlotOf(role) >= ROLE_PALETTE_SIZE ? " is-wrapped" : "";
  const skin = role ? ` style="--team-dag-role-color:var(${roleColorVar(role)})"` : "";
  return `<article class="team-dag-row" data-item-id="${escapeHtml(String(item.id || ""))}" data-status="${escapeHtml(status)}" data-eff="${escapeHtml(eff)}" data-item="${escapeHtml(String(item.id || ""))}" data-role="${escapeHtml(role)}" data-slot="${slot}" data-dur="${dur}" data-end="${slot + dur}" data-depth="${escapeHtml(String(entry?.depth ?? slot))}" data-milestone-id="${escapeHtml(String(item.milestone || entry?.milestone_id || ""))}" data-interrupted="${item.interrupted === true ? "true" : "false"}" data-deps="${escapeHtml(deps.join(","))}" data-cross-deps="${escapeHtml(cross.join(","))}"${skin}>
        <div class="team-dag-card" title="${escapeHtml(full)}">
          <div class="team-dag-label">
            <span class="team-dag-label-top">
              <code class="team-dag-id">${escapeHtml(String(item.id || ""))}</code>
              ${label}
            </span>
            <span class="team-dag-label-sub">
              <span class="team-label">依赖</span>${deps.length ? deps.map(dep => `<span class="chip team-dep">${escapeHtml(dep)}</span>`).join("") : '<span class="muted">—</span>'}
              ${marks}${missingChip}${crossChip}${cyclic}
              <span class="team-dag-flags">${flags}</span>
            </span>
            <span class="team-dag-full" aria-hidden="true">${escapeHtml(full)}</span>
          </div>
          <div class="team-dag-plot" style="--s:${slot};--d:${dur}">
            <span class="team-dag-depnote" title="${escapeHtml(depNote)}">${escapeHtml(depNote)}</span>
            <span class="team-dag-bar" data-item="${escapeHtml(String(item.id || ""))}" data-eff="${escapeHtml(eff)}">
              <i class="team-dag-band${wrapped}" title="条左端 4px 实色 cap = 归属（teammate）；条描边 = 状态，是两条正交通道"></i>
              <span class="team-dag-bar-name">${escapeHtml(name)}</span>
            </span>
            <span class="team-dag-after">
              ${role ? `<span class="chip team-role" title="teammate"><i class="team-dag-dot"></i>${escapeHtml(role)}</span>` : ""}
              <span class="team-status is-${escapeHtml(eff)}" data-eff="${escapeHtml(eff)}" title="条描边 = 状态（${escapeHtml(status)}）">${escapeHtml(status)}${item.interrupted === true ? " · 可重派" : ""}</span>
              ${session ? `<span class="team-dag-session" data-team-session="${escapeHtml(session)}" title="session ${escapeHtml(session)}">${escapeHtml(session)}</span>` : ""}
              ${worktree ? `<span class="team-dag-wt" title="worktree ${escapeHtml(worktree)}">${escapeHtml(worktree)}</span>` : ""}
              <span class="team-dag-goal" title="${escapeHtml([goal ? `达成目标：${goal}` : "", description ? `描述：${description}` : "", note ? `结论：${note}` : ""].filter(Boolean).join(" ｜ "))}">${escapeHtml(goal ? truncate(goal, CONTENT_LIMIT) : "—")}</span>
            </span>
          </div>
        </div>
      </article>`;
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
      <div class="team-section-title"><span>teammate</span><span class="chip team-count">${members.length}</span></div>
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
      ${teammateLivePager(view)}
    </div>`;
}

// TEAMMATE_LIVE_PAGE_SIZE 是翻页时前端申请的页大小（与后端 limit<=0 的默认页一致）。
export const TEAMMATE_LIVE_PAGE_SIZE = 40;

// teammateLivePager 画「这件事的会话」的翻页条。
//
// 后端读数已经是**有界窗口 + 分页**（offset/limit/total/has_more）：前端只搬这些读数，
// 不自己推算事实——offset 是后端归一过的起点，has_more 是后端算的"后面还有"。
// 读不到这些键（老载荷/夹具）时退化成"只有这一页"：不画假的翻页键。
function teammateLivePager(view) {
  const messages = Array.isArray(view?.messages) ? view.messages : [];
  const total = Number.isFinite(Number(view?.total)) ? Number(view.total) : messages.length;
  if (total <= 0) return "";
  const offset = Number.isFinite(Number(view?.offset)) && Number(view.offset) > 0 ? Number(view.offset) : 0;
  const limit = Number.isFinite(Number(view?.limit)) && Number(view.limit) > 0 ? Number(view.limit) : TEAMMATE_LIVE_PAGE_SIZE;
  const hasMore = view?.has_more === true;
  const first = offset + 1;
  const last = offset + messages.length;
  const prevOffset = Math.max(0, offset - limit);
  const prev = offset > 0
    ? `<button class="chip team-live-page" data-teammate-live-page="${prevOffset}">上一页</button>`
    : '<button class="chip team-live-page" data-teammate-live-page="0" disabled>上一页</button>';
  const next = hasMore
    ? `<button class="chip team-live-page" data-teammate-live-page="${offset + limit}">下一页</button>`
    : `<button class="chip team-live-page" data-teammate-live-page="${offset + limit}" disabled>下一页</button>`;
  return `<div class="team-live-pager">
      ${prev}
      <span class="team-live-range">第 ${first}–${last} 条 / 共 ${total} 条</span>
      ${next}
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
/* 作用域：整块看板的令牌都定义在 .team-board 里（不泄露到全局）。
   - 几何（--team-dag-*）：与渲染件的 DAG 常量逐字对应；
   - teammate 色板（--team-dag-role-0..5）：本件自造（既有令牌里没有"归属色"这一族）；
   - 里程碑框的淡色描边（--team-dag-ms-tone-0..4）：同上，低饱和、刻意避开四状态色。
   状态色/淡色底一律引用既有 --status-* / --tint-* / --border-*，不另造 hex。
   container-type 让窄栏（产品右栏 ~360px）能用 @container 收缩，不用另加一套裁剪口径。 */
.team-board { display: flex; flex-direction: column; gap: 8px; min-width: 0; font-size: var(--text-sm); color: var(--text-strong); container-type: inline-size;
  --team-dag-scroll-max-h: 380px;
  --team-dag-slots: 1;
  --team-dag-label-w: 208px;
  --team-dag-slot-w: 64px;
  --team-dag-bar-h: 18px;
  --team-dag-row-h: 38px;
  --team-dag-ruler-h: 30px;
  --team-dag-sum-h: 22px;
  --team-dag-head-h: 26px;
  --team-dag-gate-h: 18px;
  --team-dag-tail-w: 168px;
  --team-dag-edge: #3f93d0;
  --team-dag-role-0: #6f5bd6;
  --team-dag-role-1: #a4538f;
  --team-dag-role-2: #0f8f9e;
  --team-dag-role-3: #6b7f2e;
  --team-dag-role-4: #4b4fa8;
  --team-dag-role-5: #5a5f68;
  --team-dag-ms-tone-0: #6a7fa8;
  --team-dag-ms-tone-1: #8a7fa8;
  --team-dag-ms-tone-2: #6f8f7f;
  --team-dag-ms-tone-3: #a08a6a;
  --team-dag-ms-tone-4: #8f6f7f;
}
[data-theme="dark"] .team-board {
  --team-dag-role-0: #a08bf0;
  --team-dag-role-1: #c98fc3;
  --team-dag-role-2: #45c2bd;
  --team-dag-role-3: #a9c46a;
  --team-dag-role-4: #8f93e0;
  --team-dag-role-5: #9aa0aa;
  --team-dag-edge: #6fb6e0;
  --team-dag-ms-tone-0: #8fa6c8;
  --team-dag-ms-tone-1: #a89ac4;
  --team-dag-ms-tone-2: #8fb09a;
  --team-dag-ms-tone-3: #c4ab84;
  --team-dag-ms-tone-4: #b894a4;
}
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
.team-event { display: flex; align-items: baseline; gap: 5px; flex-wrap: wrap; min-width: 0; font-size: var(--text-xs); }
.team-event-kind { padding: 0 5px; border: 1px solid var(--border-hairline); border-radius: 4px; font-size: 10px; color: var(--text-mid); }
.team-event-kind.is-dispatch { color: var(--status-info); border-color: var(--border-info); }
.team-event-kind.is-milestone { color: var(--status-done); border-color: var(--border-done); }
.team-event-kind.is-retire { color: var(--text-dim); }
.team-event-target { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--font-mono); color: var(--text-mid); }
.team-event-at { flex: none; color: var(--faint); }
.team-event-detail { flex: 1 1 100%; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--faint); }
/* ── 里程碑 · 工作项：真甘特几何（wi-gantt-impl，2026-10-08）─────────────────────────
   左侧任务名列 + 顶部刻度尺（横轴 = **依赖槽位**，不是时间）+ 竖网格线 + 每行一根横条 +
   里程碑汇总条 + finish→start 的正交折线箭头。几何只有**一份**：--team-dag-* 定义在这里，
   渲染件只读变量名、不另写一份数字——x（槽位轴）由 CSS calc 与渲染件的算式各算各的，同一个
   变量说了算；y（行高轴）由**实测**给（wi-gantt-fit：行高由内容撑，见 .team-dag-row 与
   .team-dag-edges 两处注释）。
   响应式：宿主 ≤520px（产品右栏 ~360px）时把几何整体压小（见文件末尾的 @container），**纯
   CSS**，不另做一套裁剪数据的口径。 */
.team-dag-scroll { max-height: var(--team-dag-scroll-max-h, 380px); overflow: auto; overscroll-behavior: contain; scrollbar-width: thin; scrollbar-color: var(--border-strong) transparent; }
.team-dag-scroll::-webkit-scrollbar { width: 10px; height: 10px; }
.team-dag-scroll::-webkit-scrollbar-track { background: transparent; }
.team-dag-scroll::-webkit-scrollbar-thumb { background: var(--border-strong); border-radius: 999px; border: 2px solid transparent; background-clip: padding-box; }
.team-dag-scroll::-webkit-scrollbar-thumb:hover { background: var(--faint); background-clip: padding-box; }
/* 内容 = 竖着排的一列块：刻度尺 → 框 → 闸门带 → 框……。flex 子项的 margin **不合并**，
   所以"行在哪一行、框长到多高"完全是内容自己撑出来的（不是按高度相加算出来的：行高由内容定，
   量得出来才算得出来——见 layoutTeamGanttEdges）。 */
.team-dag-content { position: relative; display: flex; flex-direction: column; padding-top: 4px; min-width: calc(var(--team-dag-label-w) + var(--team-dag-slots, 1) * var(--team-dag-slot-w) + var(--team-dag-tail-w)); }
/* 刻度尺（sticky 在滚动容器顶边）：横轴是依赖槽位，尺头就把这句话写出来。 */
.team-dag-ruler { position: sticky; top: 0; z-index: 6; display: grid; grid-template-columns: var(--team-dag-label-w) 1fr; box-sizing: border-box; height: var(--team-dag-ruler-h); border-bottom: 1px solid var(--border-strong); background: var(--panel-solid); }
.team-dag-ruler-label { display: flex; align-items: center; padding: 0 8px 0 10px; border-right: 1px solid var(--border); font-family: var(--font-mono); font-size: 10.5px; font-weight: 700; color: var(--text-mid); white-space: nowrap; overflow: hidden; }
.team-dag-ruler-plot { position: relative; overflow: hidden; }
.team-dag-tick { position: absolute; top: 0; bottom: 0; left: calc(var(--i) * var(--team-dag-slot-w)); }
.team-dag-tick i { position: absolute; left: 0; top: 16px; bottom: 0; width: 1px; background: var(--border-strong); }
.team-dag-tick b { position: absolute; left: 3px; top: 15px; font-family: var(--font-mono); font-size: 9.5px; font-weight: 700; color: var(--faint); }
.team-dag-ruler-basis { position: absolute; left: 4px; top: 1px; font-family: var(--font-mono); font-size: 9.5px; font-weight: 700; color: var(--text-dim); white-space: nowrap; }
/* 竖网格线：从 label 列右侧起，每 slotW 一条（重复渐变，跟着容器查询一起缩）。 */
.team-dag-grid { position: absolute; z-index: 0; top: calc(4px + var(--team-dag-ruler-h)); bottom: 0; left: var(--team-dag-label-w); width: calc(var(--team-dag-slots, 1) * var(--team-dag-slot-w)); pointer-events: none; border-left: 1px solid var(--border); background: repeating-linear-gradient(to right, var(--border-hairline) 0 1px, transparent 1px var(--team-dag-slot-w)); background-position: var(--team-dag-slot-w) 0; }
/* 依赖边：一组绝对定位的线段（坐标 = 内容坐标，由渲染件写成 calc 算式）。颜色一律中性
   --team-dag-edge：**不借状态色、不借 teammate 色**（线只说"谁依赖谁"，状态由条描边说，
   归属由条填充说，三条通道各说各的）。线段与框同处一个层叠上下文、按 z-index 排：线段
   （z-index auto）在框（2）**之下**，所以跨框的那一段滚动时会被 sticky 框头盖住 —— 与设计稿
   同行为，见 README §7.1。**箭头单独抬到框之上（3）**：目标锚点在槽 0 时条左端就是绘图区
   左沿，接近段只能顶到左沿内侧（见渲染件 xApproach），箭尖因此落进汇总条起点那一格；
   不抬箭头，整条边最关键的"到了"那一点就会被实色汇总条吃掉（独立验证 F1/② 同一条病）。
   抬到 3 仍在 sticky 框头（5）与刻度尺（6）之下，线段本身的层序一点没动。 */
.team-dag-edges { position: absolute; left: 0; top: 0; right: 0; bottom: 0; pointer-events: none; }
/* 量出来之前整层不显示：渲染字符串里的 top/height 是**占位**（行高由内容撑，字符串给不出 y），
   等 layoutTeamGanttEdges 按实测把每段线写实之后置 data-laid-out="true"。宁可先不画，也不画
   一条停在 0px 的假线。 */
.team-dag-edges:not([data-laid-out="true"]) { visibility: hidden; }
.team-dag-edge { position: absolute; left: 0; top: 0; width: 0; height: 0; }
.team-dag-edge-seg { position: absolute; background: var(--team-dag-edge); }
.team-dag-edge-seg.is-h { height: 1.5px; }
.team-dag-edge-seg.is-v { width: 1.5px; }
.team-dag-edge-arrow { position: absolute; z-index: 3; width: 0; height: 0; border-left: 7px solid var(--team-dag-edge); border-top: 3.5px solid transparent; border-bottom: 3.5px solid transparent; }
/* 里程碑 = 一个大框：极淡填充 + 自己的淡色描边（逐框轮换），不吃状态色、不吃 teammate 色。 */
.team-dag-frame { position: relative; z-index: 2; margin: 4px 0; padding-bottom: 4px; border: 1px solid var(--team-dag-ms-tone, var(--border-strong)); border-radius: var(--r-lg); background: color-mix(in srgb, var(--team-dag-ms-tone, transparent) 5%, transparent); }
.team-dag-frame[data-locked="true"] { border-style: dashed; opacity: .55; }
.team-dag-frame-head { position: sticky; top: var(--team-dag-ruler-h); z-index: 5; box-sizing: border-box; display: flex; flex-wrap: nowrap; align-items: center; gap: 6px; height: var(--team-dag-head-h); padding: 0 8px; overflow: hidden; border-bottom: 1px solid var(--border); border-radius: var(--r-lg) var(--r-lg) 0 0; background: var(--panel-solid); }
.team-dag-frame-head > * { flex: none; white-space: nowrap; }
.team-dag-ms-id { font-family: var(--font-mono); font-size: var(--text-xs); font-weight: 700; color: var(--team-dag-ms-tone, var(--text-strong)); }
.team-dag-ms-name { font-size: var(--text-sm); font-weight: 700; color: var(--text-strong); }
.team-dag-layer, .team-dag-ms-deps, .team-dag-ms-count, .team-dag-ms-empty { font-size: var(--text-xs); color: var(--text-dim); }
.team-dag-ms-content { flex: 1 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; font-size: var(--text-xs); color: var(--faint); }
.team-dag-lock { padding: 1px 6px; border: 1px solid var(--border-strong); border-radius: 999px; font-size: 10px; color: var(--status-idle); }
.team-dag-lock.is-open { color: var(--status-done); border-color: var(--border-done); background: var(--tint-done); }
.team-dag-warn { color: var(--status-failed); font-size: var(--text-xs); }
/* 汇总条那一行：横跨名下条目的 min(start)..max(end)，两端向下短折；空里程碑画零宽菱形。 */
.team-dag-ms-sum { position: relative; z-index: 2; display: grid; grid-template-columns: var(--team-dag-label-w) 1fr; box-sizing: border-box; height: var(--team-dag-sum-h); }
.team-dag-ms-sum-label { display: flex; align-items: center; padding: 0 8px 0 10px; border-right: 1px solid var(--border); font-family: var(--font-mono); font-size: 9.5px; font-weight: 700; color: var(--faint); }
.team-dag-plot { position: relative; min-width: 0; }
.team-dag-sum-bar { position: absolute; top: 50%; transform: translateY(-50%); left: calc(var(--s) * var(--team-dag-slot-w)); width: calc((var(--e) - var(--s)) * var(--team-dag-slot-w)); height: 9px; border-radius: 2px; background: var(--team-dag-ms-tone); }
.team-dag-sum-bar[data-empty="true"] { display: none; }
.team-dag-sum-cap { position: absolute; bottom: -6px; width: 2px; height: 6px; background: var(--team-dag-ms-tone); }
.team-dag-sum-cap-l { left: 0; }
.team-dag-sum-cap-r { right: 0; }
.team-dag-sum-diamond { position: absolute; top: 50%; left: calc(var(--s) * var(--team-dag-slot-w)); width: 9px; height: 9px; margin: -4.5px 0 0 -4.5px; transform: rotate(45deg); background: var(--team-dag-ms-tone); }
.team-dag-sum-diamond[data-empty="false"] { display: none; }
.team-dag-sum-note { position: absolute; top: 50%; transform: translateY(-50%); left: calc(var(--e) * var(--team-dag-slot-w) + 8px); font-family: var(--font-mono); font-size: 9.5px; color: var(--faint); white-space: nowrap; }
/* 工作项行：左边名列 + 右边绘图区（一根条）。**行高由内容撑**（wi-gantt-fit，2026-10-08）：
   --team-dag-row-h 从"钉死的行高"降为**保底值**（min-height）——名字多折两行，行就长两行，
   它所在的里程碑框跟着长。框高不是算出来的（不是"头+汇总+行数×常量"），是行撑出来的；
   边的 y 也由实测给（见 .team-dag-edges 与布局件的 layoutTeamGanttEdges）。 */
.team-dag-rows { display: flex; flex-direction: column; }
.team-dag-row { position: relative; z-index: 2; box-sizing: border-box; height: auto; min-height: var(--team-dag-row-h); }
.team-dag-card { display: grid; grid-template-columns: var(--team-dag-label-w) 1fr; height: auto; min-height: var(--team-dag-row-h); overflow: visible; }
/* 名列的可读性责任在这里：id 一行（mono），**name 另起一行并可折行**（行数由内容定，不设
   max-height、不裁）——370px 的右栏里名字也读得完，不靠 hover/title。 */
.team-dag-label { display: flex; flex-direction: column; justify-content: center; gap: 2px; min-width: 0; padding: 3px 8px 3px 10px; }
.team-dag-label-top { display: flex; flex-direction: column; align-items: stretch; gap: 1px; min-width: 0; white-space: normal; }
.team-dag-id { flex: none; overflow-wrap: anywhere; font-family: var(--font-mono); font-size: 10.5px; font-weight: 700; color: var(--text-strong); }
.team-dag-name { min-width: 0; max-width: 100%; overflow-wrap: anywhere; white-space: normal; font-size: var(--text-sm); font-weight: 600; color: var(--text-strong); }
.team-dag-name.is-openable { padding: 0; border: 0; background: none; text-align: left; text-decoration: underline dotted var(--border-strong); text-underline-offset: 2px; cursor: pointer; }
.team-dag-name.is-openable:hover { color: var(--text-bright); }
.team-dag-name.is-openable:focus-visible { outline: 1px solid var(--border-info); outline-offset: 1px; border-radius: 3px; }
.team-dag-label-sub { display: flex; align-items: center; gap: 3px; min-width: 0; overflow: hidden; white-space: nowrap; font-size: 9.5px; color: var(--text-dim); }
.team-dag-label-sub .chip { padding: 0 4px; border-radius: 3px; font-size: 9.5px; }
.team-dag-flags { display: flex; flex: none; gap: 3px; }
.team-dag-flag { padding: 0 3px; border: 1px solid var(--border); border-radius: 3px; font-family: var(--font-mono); font-size: 9px; font-weight: 700; color: var(--faint); }
.team-dag-flag[data-on="false"] { opacity: .28; }
.team-dag-flag[data-flag="blocked"][data-on="true"] { color: var(--status-idle); border-color: var(--border-strong); }
.team-dag-flag[data-flag="interrupted"][data-on="true"] { color: var(--status-failed); border-color: var(--border-failed); background: var(--tint-failed); }
.team-dag-flag[data-flag="live"][data-on="true"] { color: var(--status-running); border-color: var(--border-running); background: var(--tint-running); }
/* 全文（goal / description / note / session / worktree / 三个标记的布尔值）：视觉隐藏，
   但在 DOM 里 —— 窄栏折叠了次级信息时，仍然读得到（口径 9）。 */
.team-dag-full { position: absolute; width: 1px; height: 1px; margin: -1px; padding: 0; border: 0; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
.team-dag-depnote { position: absolute; top: 0; left: calc(var(--s, 0) * var(--team-dag-slot-w) + 2px); max-width: 100%; overflow: hidden; text-overflow: ellipsis; font-family: var(--font-mono); font-size: 9.5px; line-height: 10px; color: var(--faint); white-space: nowrap; pointer-events: none; }
/* 条：**描边 = 状态**（--row-line，pending 虚线），**填充 = teammate 色 14% 淡色 + 左端
   4px 实色 cap**。宽 = 1 槽 - 2px（相邻槽留缝），高 = --team-dag-bar-h。 */
.team-dag-bar { position: absolute; top: 50%; transform: translateY(-50%); left: calc(var(--s, 0) * var(--team-dag-slot-w)); box-sizing: border-box; width: calc(var(--d, 1) * var(--team-dag-slot-w) - 2px); height: var(--team-dag-bar-h); border: 1px solid var(--row-line, var(--status-idle)); border-radius: 3px; background: color-mix(in srgb, var(--team-dag-role-color, var(--faint)) 14%, transparent); }
.team-dag-row[data-eff="pending"] .team-dag-bar { border-style: dashed; }
.team-dag-band { position: absolute; left: 0; top: 0; bottom: 0; width: 4px; border-radius: 2px 0 0 2px; background: var(--team-dag-role-color, var(--faint)); }
.team-dag-band.is-wrapped { background: repeating-linear-gradient(45deg, var(--team-dag-role-color, var(--faint)) 0 2px, transparent 2px 5px); }
.team-dag-bar-name { position: absolute; left: 7px; right: 4px; top: 0; bottom: 0; display: flex; align-items: center; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 10.5px; font-weight: 600; color: var(--text-strong); pointer-events: none; }
/* 条后那一串：role chip（色点）+ 状态 chip + 会话 chip + 达成目标。 */
.team-dag-after { position: absolute; top: 50%; transform: translateY(-50%); left: calc((var(--s, 0) + var(--d, 1)) * var(--team-dag-slot-w) + 10px); display: flex; align-items: center; gap: 5px; white-space: nowrap; }
.team-dag-dot { display: inline-block; width: 8px; height: 8px; margin-right: 3px; border-radius: 50%; background: var(--team-dag-role-color, transparent); vertical-align: -1px; }
.team-dag-session, .team-dag-wt, .team-dag-goal { overflow: hidden; text-overflow: ellipsis; font-size: 10.5px; white-space: nowrap; }
.team-dag-session, .team-dag-goal { font-family: var(--font-mono); color: var(--faint); }
.team-dag-goal { max-width: 220px; color: var(--text-mid); }
.team-dag-wt { max-width: 160px; font-family: var(--font-mono); color: var(--faint); }
/* 线框色 = 状态（唯一判据 effStatus，见渲染件的 data-eff）；条描边读这两个变量。 */
.team-dag-row[data-eff="running"] { --row-line: var(--status-running); --row-tint: var(--tint-running); }
.team-dag-row[data-eff="done"] { --row-line: var(--status-done); --row-tint: var(--tint-done); }
.team-dag-row[data-eff="review"] { --row-line: var(--status-info); --row-tint: var(--tint-info); }
.team-dag-row[data-eff="failed"] { --row-line: var(--status-failed); --row-tint: var(--tint-failed); }
.team-dag-row[data-eff="pending"] { --row-line: var(--status-idle); --row-tint: transparent; }
/* 闸门带：横向虚线 + 标注；放行后转绿。 */
.team-dag-gate { position: relative; z-index: 2; display: flex; align-items: center; gap: 8px; box-sizing: border-box; height: var(--team-dag-gate-h); margin: 2px 4px; --team-dag-gate-tone: var(--status-idle); }
.team-dag-gate[data-open="true"] { --team-dag-gate-tone: var(--status-done); }
.team-dag-gate-line { flex: 1 1 auto; border-top: 1.5px dashed var(--team-dag-gate-tone); }
.team-dag-gate-label { flex: none; padding: 1px 7px; border: 1px dashed var(--team-dag-gate-tone); border-radius: 999px; font-size: 10px; color: var(--team-dag-gate-tone); }
/* 窄栏自适应（口径 10）：产品右栏只有 ~360px。几何整体压小 —— 渲染件的边坐标全是 calc
   算式，会跟着一起缩；折叠的只是次级信息（全文仍在 title 与 .team-dag-full 里）。 */
@container (max-width: 520px) {
  .team-dag-scroll { --team-dag-slot-w: 40px; --team-dag-label-w: 118px; --team-dag-bar-h: 16px; --team-dag-row-h: 30px; --team-dag-ruler-h: 28px; --team-dag-tail-w: 76px; }
  .team-dag-ms-content, .team-dag-ms-deps, .team-dag-ms-empty, .team-dag-layer, .team-dag-sum-note { display: none; }
  .team-dag-depnote, .team-dag-session, .team-dag-wt, .team-dag-goal, .team-dag-label-sub { display: none; }
  /* 条内那串字是**视觉回声**（名字在左侧名列里已经读得完）：窄栏一根条只有 40px 宽，
     塞进去就是"半截字"（实测 20px / 全名 227px）——窄栏直接不画它，不留半截。 */
  .team-dag-bar-name { display: none; }
}
.team-queues, .team-item-messages { display: flex; flex-direction: column; gap: 3px; margin: 0; padding: 0; list-style: none; min-width: 0; }
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
