import { escapeHtml } from "./components.js";

// team-board-view.js 是「团队（Teamwork）」看板的**纯渲染件**（不碰 DOM，便于 node:test）。
//
// 数据源三份，全部是后端只读投影——前端**没有任何写入口**（渲染结果不回写后端）：
//   1. plan   计划本体：sessionstore.TeamworkPlan（team_id / version / stages / members /
//              milestones）。顺序的**唯一事实**是 stages[].depends_on，本件不从调用姿势猜顺序。
//   2. jobs   作业投影：与工作表格同源的作业行（handle / state / exit_code / bytes /
//              node / scope.subject）。注意 jobs I-4：**句柄只是投影**，进程重启后一律视为过期，
//              真值以 Observe 的返回为准。
//   3. events 审计流水：sessionstore.TeamworkEvent[]（plan / dispatch / join / milestone / retire）。
//
// 本件只做两件事：把依赖边折成能看的顺序（拓扑 + 层号），把作业终态折成**阶段状态**。
// 阶段状态不是计划里的字段（计划只声明顺序），它是这里按 depends_on + 该阶段作业终态推出来的。
//
// 归属口径（为什么不能只看一个字段）：作业行里没有权威的 stage 字段时，按
//   job.stage → job.node（等于阶段 id）→ job.scope.subject（emp_<role>）/ job.role → 该角色**首次出现**的阶段
// 依次回落，与团队规范「一个角色只在一个阶段里当主语」同口径。接线时应让桥给出权威的
// job.stage + job.role，这条回落链只作为过渡。

const STAGE_PENDING = "pending";
const STAGE_READY = "ready";
const STAGE_RUNNING = "running";
const STAGE_DONE = "done";
const STAGE_FAILED = "failed";

// 作业终态：failed / killed 都算「这个阶段没走通」，不把被杀当成还在跑。
const FAILED_JOB_STATES = new Set(["failed", "killed"]);

const ROLE_SUBJECT_PREFIX = "emp_";
const TEXT_LIMIT = 160;
const CONTENT_LIMIT = 240;
const AUDIT_LIMIT = 6;

// ── 纯函数：计划 → 顺序 / 归属 / 状态 ─────────────────────────────

// stagesOf 取计划里的阶段数组（计划缺失 → []）。
export function stagesOf(plan) {
  return Array.isArray(plan?.stages) ? plan.stages.filter(stage => stage && stage.id) : [];
}

// roleOwnerStage 给出每个角色**首次出现**的阶段（团队规范的归属口径）。
export function roleOwnerStage(plan) {
  const owners = new Map();
  for (const stage of stagesOf(plan)) {
    for (const role of rolesOf(stage)) {
      if (!owners.has(role)) owners.set(role, String(stage.id));
    }
  }
  return owners;
}

// rolesOf 取一个阶段的角色名（去空白、去空项）。
export function rolesOf(stage) {
  return (Array.isArray(stage?.roles) ? stage.roles : [])
    .map(role => String(role || "").trim())
    .filter(Boolean);
}

// jobRoleOf 取作业行对应的角色：显式 role 优先，其次作用域主体 emp_<role>。
export function jobRoleOf(job) {
  const role = String(job?.role || "").trim();
  if (role) return role;
  const subject = String(job?.scope?.subject || "").trim();
  return subject.startsWith(ROLE_SUBJECT_PREFIX) ? subject.slice(ROLE_SUBJECT_PREFIX.length) : "";
}

// jobStageID 把一条作业归属到一个阶段 id；归不上 → ""（看板上不猜）。
export function jobStageID(plan, job) {
  const ids = new Set(stagesOf(plan).map(stage => String(stage.id)));
  const explicit = String(job?.stage || "").trim();
  if (ids.has(explicit)) return explicit;
  const node = String(job?.node || "").trim();
  if (ids.has(node)) return node;
  const role = jobRoleOf(job);
  return role ? (roleOwnerStage(plan).get(role) || "") : "";
}

// jobsByStage 按归属把作业行分组（归不上的落在 "" 桶，渲染时忽略）。
export function jobsByStage(plan, jobs) {
  const grouped = new Map();
  for (const job of Array.isArray(jobs) ? jobs : []) {
    if (!job) continue;
    const stageID = jobStageID(plan, job);
    if (!grouped.has(stageID)) grouped.set(stageID, []);
    grouped.get(stageID).push(job);
  }
  return grouped;
}

// jobsOfRole 取一个角色的全部作业（成员表的「当前状态」用）。
export function jobsOfRole(plan, jobs, role) {
  return (Array.isArray(jobs) ? jobs : []).filter(job => job && jobRoleOf(job) === role);
}

// orderStages 按 depends_on 做拓扑排序 + 层号（层 = 最长依赖链上的位置）。
// 依赖指向不存在的阶段时忽略该边；成环时把剩下的阶段按原顺序接在末尾（层号归 0，
// cyclic=true）——宁可在看板上显形，也不静默丢阶段。
export function orderStages(plan) {
  const stages = stagesOf(plan);
  const ids = new Set(stages.map(stage => String(stage.id)));
  const missing = new Map(stages.map(stage => [String(stage.id), depsOf(stage).filter(dep => !ids.has(dep))]));
  const waiting = new Map(stages.map(stage => [String(stage.id), depsOf(stage).filter(dep => ids.has(dep))]));
  const depth = new Map();
  const ordered = [];
  let frontier = stages.filter(stage => waiting.get(String(stage.id)).length === 0);
  for (const stage of frontier) depth.set(String(stage.id), 0);
  while (frontier.length) {
    for (const stage of frontier) ordered.push(entryFor(stage, depth.get(String(stage.id)), missing.get(String(stage.id))));
    const next = [];
    for (const stage of stages) {
      const id = String(stage.id);
      if (depth.has(id)) continue;
      const pending = waiting.get(id).filter(dep => !depth.has(dep));
      waiting.set(id, pending);
      if (pending.length) continue;
      const level = Math.max(0, ...depsOf(stage).map(dep => (depth.get(dep) ?? 0) + 1));
      depth.set(id, level);
      next.push(stage);
    }
    frontier = next;
  }
  for (const stage of stages) {
    if (!depth.has(String(stage.id))) ordered.push(entryFor(stage, 0, missing.get(String(stage.id)), true));
  }
  return ordered;
}

// depsOf 取一个阶段的依赖（去空白、去空项）。
export function depsOf(stage) {
  return (Array.isArray(stage?.depends_on) ? stage.depends_on : [])
    .map(dep => String(dep || "").trim())
    .filter(Boolean);
}

function entryFor(stage, depth, missing = [], cyclic = false) {
  return {
    id: String(stage.id),
    roles: rolesOf(stage),
    depends_on: depsOf(stage),
    missing_deps: missing,
    depth,
    cyclic,
  };
}

// jobState / jobFailed 是作业行的状态读法：缺字段一律当未知，不当作 done。
export function jobState(job) {
  return String(job?.state || "").trim().toLowerCase();
}

export function jobFailed(job) {
  return FAILED_JOB_STATES.has(jobState(job));
}

// ownStageStatus 只看**该阶段自己的作业**：失败 > 全完 > 其余都算在途；没有作业 → ""（由依赖折算）。
//
// 「其余都算在途」是有意的：作业状态是开放取值，只有 done 与 failed/killed 是**终态事实**，
// 认不出来的状态不该被读成"还没派发"（那是把未知当成好消息）。
export function ownStageStatus(jobs) {
  const list = (Array.isArray(jobs) ? jobs : []).filter(Boolean);
  if (list.some(jobFailed)) return STAGE_FAILED;
  if (list.length === 0) return "";
  return list.every(job => jobState(job) === "done") ? STAGE_DONE : STAGE_RUNNING;
}

// stageStatuses 折出每个阶段的状态：
//    FAILED/RUNNING/DONE 由作业决定；没有作业时，依赖全 done → READY（可以派活了），否则 PENDING。
// statusById 是返回值（阶段 id → 状态），供里程碑与汇总复用。
export function stageStatuses(plan, jobs) {
  const grouped = jobsByStage(plan, jobs);
  const statusById = new Map();
  for (const entry of orderStages(plan)) {
    statusById.set(entry.id, STAGE_PENDING);
  }
  for (const entry of orderStages(plan)) {
    const own = ownStageStatus(grouped.get(entry.id));
    if (own) {
      statusById.set(entry.id, own);
      continue;
    }
    const blocked = entry.depends_on.some(dep => statusById.get(dep) !== STAGE_DONE);
    statusById.set(entry.id, entry.depends_on.length === 0 || !blocked ? STAGE_READY : STAGE_PENDING);
  }
  return statusById;
}

// summarizeTeam 是看板头部的一行计数（阶段 / 在编 / 作业 / 里程碑）。
export function summarizeTeam(plan, jobs) {
  const statusById = stageStatuses(plan, jobs);
  const statuses = [...statusById.values()];
  const members = Array.isArray(plan?.members) ? plan.members.filter(Boolean) : [];
  const milestones = milestonesOf(plan);
  const jobList = (Array.isArray(jobs) ? jobs : []).filter(Boolean);
  return {
    stages: statuses.length,
    done: statuses.filter(status => status === STAGE_DONE).length,
    running: statuses.filter(status => status === STAGE_RUNNING).length,
    failed: statuses.filter(status => status === STAGE_FAILED).length,
    members: members.length,
    milestones_total: milestones.length,
    milestones_done: milestones.filter(milestone => milestoneStatus(milestone) === "done").length,
    jobs_running: jobList.filter(job => jobState(job) === "running").length,
    jobs_failed: jobList.filter(jobFailed).length,
    jobs_done: jobList.filter(job => jobState(job) === "done").length,
  };
}

// milestonesOf 取里程碑数组；milestoneStatus 缺省 pending（空 = pending，见 sessionstore）。
export function milestonesOf(plan) {
  return Array.isArray(plan?.milestones) ? plan.milestones.filter(item => item && item.id) : [];
}

export function milestoneStatus(milestone) {
  const status = String(milestone?.status || "").trim().toLowerCase();
  return status || "pending";
}

// ── 渲染 ────────────────────────────────────────────────────────

// renderTeamBoard 渲染整块看板；没有计划 / 计划里没有阶段 → ""（不留空壳，口径同目标看板）。
export function renderTeamBoard(input = {}) {
  const { plan = null, jobs = [], events = [], maxMembers = 0, stale = false, recovered = false } = input;
  const entries = orderStages(plan);
  if (entries.length === 0) return "";
  const grouped = jobsByStage(plan, jobs);
  const statusById = stageStatuses(plan, jobs);
  const summary = summarizeTeam(plan, jobs);
  const head = renderTeamHead(plan, summary, maxMembers, stale, recovered);
  const stages = entries
    .map(entry => renderTeamStageCard(entry, { jobs: grouped.get(entry.id) || [], status: statusById.get(entry.id) }))
    .join("");
  return `<div class="team-board" data-team-board data-team-id="${escapeHtml(String(plan?.team_id || ""))}">
      ${head}
      <div class="team-stages">${stages}</div>
      ${renderTeamRoster(plan, jobs)}
      ${renderTeamMilestones(plan)}
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
    `阶段 ${summary.done}/${summary.stages} 完成`,
    summary.running ? `${summary.running} 在跑` : "",
    summary.failed ? `${summary.failed} 未通过` : "",
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

// renderTeamStageCard 渲染一个阶段卡：层号 + 状态 + 角色 + 依赖 + 该阶段的作业行。
export function renderTeamStageCard(entry, context = {}) {
  const status = String(context.status || STAGE_PENDING);
  const jobs = Array.isArray(context.jobs) ? context.jobs : [];
  const roles = entry.roles.length
    ? entry.roles.map(role => `<span class="chip team-role">${escapeHtml(role)}</span>`).join("")
    : '<span class="chip team-role is-empty">无角色</span>';
  const deps = entry.depends_on.length
    ? entry.depends_on.map(dep => `<span class="chip team-dep">${escapeHtml(dep)}</span>`).join("")
    : '<span class="muted">—</span>';
  const missing = entry.missing_deps.length
    ? `<div class="team-stage-warn" title="depends_on 指向计划里不存在的阶段">依赖缺失：${escapeHtml(entry.missing_deps.join("、"))}</div>`
    : "";
  const cyclic = entry.cyclic
    ? '<div class="team-stage-warn" title="depends_on 成环，层号归 0 并接在末尾">依赖成环</div>'
    : "";
  return `<article class="team-stage" data-stage-id="${escapeHtml(entry.id)}" data-status="${escapeHtml(status)}" data-depth="${escapeHtml(String(entry.depth))}">
      <div class="team-stage-head">
        <span class="team-stage-id">${escapeHtml(entry.id)}</span>
        <span class="team-depth" title="依赖层号 L${escapeHtml(String(entry.depth))}（同层 = 依赖边允许并行）">L${escapeHtml(String(entry.depth))}</span>
        <span class="team-status is-${escapeHtml(status)}">${escapeHtml(status)}</span>
      </div>
      <div class="team-stage-roles">${roles}</div>
      <div class="team-stage-deps"><span class="team-label">依赖</span>${deps}</div>
      ${missing}${cyclic}
      ${renderStageJobs(jobs)}
    </article>`;
}

function renderStageJobs(jobs) {
  if (!jobs.length) return '<div class="team-stage-jobs is-empty">尚未派发</div>';
  const rows = jobs.map(job => {
    const state = jobState(job) || "unknown";
    const handle = String(job?.handle || "—");
    const subject = String(job?.scope?.subject || jobRoleOf(job) || "");
    const bytes = Number(job?.bytes) || 0;
    const exit = Number(job?.exit_code) || 0;
    const detail = [subject, state, `${formatBytes(bytes)} 输出`, exit ? `exit=${exit}` : ""].filter(Boolean).join(" · ");
    return `<li class="team-job is-${escapeHtml(state)}" data-handle="${escapeHtml(handle)}">
        <code class="team-job-handle">${escapeHtml(handle)}</code>
        <span class="team-job-detail" title="${escapeHtml(detail)}">${escapeHtml(truncate(detail, TEXT_LIMIT))}</span>
      </li>`;
  }).join("");
  return `<ul class="team-stage-jobs">${rows}</ul>`;
}

// renderTeamRoster 是在编成员表：角色 / 权责档 / 工作区 / 该角色当前作业状态。
export function renderTeamRoster(plan, jobs) {
  const members = Array.isArray(plan?.members) ? plan.members.filter(Boolean) : [];
  if (members.length === 0) return "";
  const rows = members.map(member => {
    const role = String(member.role || "");
    const policy = String(member.tools_policy || "").trim();
    const worktree = String(member.worktree || "").trim();
    const session = String(member.role_session_id || "").trim();
    const own = jobsOfRole(plan, jobs, role);
    const state = ownStageStatus(own) || (own.length ? jobState(own[own.length - 1]) : "");
    const policyChip = policy
      ? `<span class="chip team-policy is-${escapeHtml(policy)}">${escapeHtml(policy)}</span>`
      : '<span class="muted">继承</span>';
    const stateChip = state
      ? `<span class="team-status is-${escapeHtml(state)}">${escapeHtml(state)}</span>`
      : '<span class="muted">空闲</span>';
    return `<li class="team-member" data-role="${escapeHtml(role)}">
        <span class="team-member-role">${escapeHtml(role)}</span>
        ${policyChip}
        <span class="team-member-wt" title="${escapeHtml(worktree || "未指派工作区（回退主工作区）")}">${escapeHtml(worktree || "主工作区")}</span>
        ${stateChip}
        ${session ? `<span class="team-member-session" title="${escapeHtml(session)}">${escapeHtml(session)}</span>` : ""}
      </li>`;
  }).join("");
  return `<section class="team-section" data-team-roster>
      <div class="team-section-title"><span>在编</span><span class="chip team-count">${members.length}</span></div>
      <ul class="team-members">${rows}</ul>
    </section>`;
}

// renderTeamMilestones 是里程碑条：id / after（判据是依赖边）/ 状态 / leader 撰写的内容。
export function renderTeamMilestones(plan) {
  const milestones = milestonesOf(plan);
  if (milestones.length === 0) return "";
  const rows = milestones.map(milestone => {
    const status = milestoneStatus(milestone);
    const after = (Array.isArray(milestone.after) ? milestone.after : [])
      .map(id => `<span class="chip team-dep">${escapeHtml(String(id))}</span>`).join("");
    const content = String(milestone.content || "").trim();
    return `<li class="team-milestone" data-milestone-id="${escapeHtml(String(milestone.id))}" data-status="${escapeHtml(status)}">
        <div class="team-milestone-head">
          <span class="team-milestone-id">${escapeHtml(String(milestone.id))}</span>
          <span class="team-status is-${escapeHtml(status)}">${escapeHtml(status)}</span>
        </div>
        <div class="team-milestone-after"><span class="team-label">判据</span>${after || '<span class="muted">—</span>'}</div>
        <div class="team-milestone-content${content ? "" : " is-empty"}" title="${escapeHtml(content)}">${content ? escapeHtml(truncate(content, CONTENT_LIMIT)) : "尚无内容"}</div>
      </li>`;
  }).join("");
  return `<section class="team-section" data-team-milestones>
      <div class="team-section-title"><span>里程碑</span><span class="chip team-count">${milestones.length}</span></div>
      <ul class="team-milestones">${rows}</ul>
    </section>`;
}

// renderTeamAudit 是审计流水的最近几行（只追加、不重写；倒序 = 最新的在最上面）。
export function renderTeamAudit(events) {
  const list = (Array.isArray(events) ? events : []).filter(item => item && item.kind);
  if (list.length === 0) return "";
  const recent = list.slice(-AUDIT_LIMIT).reverse();
  const rows = recent.map(event => {
    const kind = String(event.kind || "");
    const target = [event.stage, event.role, event.handle, event.milestone].filter(Boolean).map(String).join(" · ");
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

function formatBytes(bytes) {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0B";
  if (bytes < 1024) return `${bytes}B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}KiB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)}MiB`;
}

function truncate(text, limit) {
  const value = String(text ?? "");
  return value.length > limit ? `${value.slice(0, limit - 1)}…` : value;
}

// TEAM_BOARD_CSS 是这块看板自己的样式（只吃 styles.css 的语义 token，不写第二套色值）。
// 接线进右栏时把它并入 styles.css 的同一节；静态预览页直接注入这一段。
export const TEAM_BOARD_CSS = `
.team-board { display: flex; flex-direction: column; gap: 8px; min-width: 0; font-size: var(--text-sm); color: var(--text-strong); }
.team-board-head { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; font-size: var(--text-xs); color: var(--text-dim); }
.team-badge { flex: none; padding: 1px 5px; border: 1px solid var(--border-strong); border-radius: 4px; font-weight: 600; letter-spacing: .06em; color: var(--text-mid); }
.team-board-id { min-width: 0; max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--font-mono); color: var(--text-strong); }
.team-version { flex: none; color: var(--text-dim); }
.team-counts { flex: 1 1 100%; min-width: 0; overflow-wrap: anywhere; }
.team-recovered { border-color: var(--border-strong); color: var(--text-mid); }
.team-stale { border-color: var(--border-running); color: var(--status-running); }
.team-stages { display: grid; grid-template-columns: repeat(auto-fill, minmax(168px, 1fr)); gap: 6px; }
.team-stage { display: flex; flex-direction: column; gap: 4px; min-width: 0; padding: 6px 7px; border: 1px solid var(--border); border-left-width: 2px; border-radius: 6px; background: var(--code-bg); }
.team-stage[data-status="running"] { border-left-color: var(--status-running); background: var(--tint-running); }
.team-stage[data-status="done"] { border-left-color: var(--status-done); }
.team-stage[data-status="failed"] { border-left-color: var(--status-failed); background: var(--tint-failed); }
.team-stage[data-status="ready"] { border-left-color: var(--status-info); }
.team-stage[data-status="pending"] { border-left-color: var(--status-idle); }
.team-stage-head { display: flex; align-items: center; gap: 5px; min-width: 0; }
.team-stage-id { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--font-mono); font-size: var(--text-sm); color: var(--text-bright); }
.team-depth { flex: none; color: var(--faint); font-size: var(--text-xs); }
.team-status { flex: none; margin-left: auto; padding: 0 5px; border: 1px solid var(--border); border-radius: 999px; font-size: 10px; letter-spacing: .04em; text-transform: uppercase; }
.team-status.is-running { color: var(--status-running); border-color: var(--border-running); background: var(--tint-running); }
.team-status.is-done { color: var(--status-done); border-color: var(--border-done); background: var(--tint-done); }
.team-status.is-failed { color: var(--status-failed); border-color: var(--border-failed); background: var(--tint-failed); }
.team-status.is-ready { color: var(--status-info); border-color: var(--border-info); background: var(--tint-info); }
.team-status.is-pending { color: var(--status-idle); }
.team-stage-roles, .team-stage-deps { display: flex; align-items: center; gap: 4px; flex-wrap: wrap; min-width: 0; }
.team-label { color: var(--faint); font-size: var(--text-xs); }
.team-stage-roles .chip, .team-stage-deps .chip { padding: 0 5px; border-radius: 4px; font-size: var(--text-xs); }
.team-role { color: var(--text-mid); border: 1px solid var(--border-hairline); }
.team-dep { color: var(--text-dim); border: 1px dashed var(--border-strong); }
.team-stage-warn { color: var(--status-failed); font-size: var(--text-xs); }
.team-stage-jobs { margin: 0; padding: 0; list-style: none; display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.team-stage-jobs.is-empty { color: var(--faint); font-size: var(--text-xs); }
.team-job { display: flex; align-items: baseline; gap: 5px; min-width: 0; }
.team-job-handle { flex: none; font-family: var(--font-mono); font-size: var(--text-xs); color: var(--text-strong); }
.team-job-detail { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-dim); font-size: var(--text-xs); }
.team-job.is-running .team-job-detail { color: var(--status-running); }
.team-job.is-failed .team-job-detail, .team-job.is-killed .team-job-detail { color: var(--status-failed); }
.team-section { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.team-section-title { display: flex; align-items: center; gap: 5px; font-size: var(--text-xs); color: var(--text-dim); }
.team-count { padding: 0 5px; border: 1px solid var(--border-hairline); border-radius: 4px; font-size: 10px; }
.team-members, .team-milestones, .team-events { display: flex; flex-direction: column; gap: 3px; margin: 0; padding: 0; list-style: none; min-width: 0; }
.team-member { display: grid; grid-template-columns: minmax(0, 1fr) auto; grid-template-areas: "role policy" "wt state"; gap: 2px 6px; padding: 4px 6px; border: 1px solid var(--border-hairline); border-radius: 5px; min-width: 0; }
.team-member-role { grid-area: role; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--font-mono); color: var(--text-strong); }
.team-member .team-status { grid-area: state; margin-left: 0; justify-self: end; }
.team-policy { grid-area: policy; justify-self: end; padding: 0 5px; border: 1px solid var(--border-hairline); border-radius: 4px; font-size: 10px; color: var(--text-mid); }
.team-policy.is-readonly { color: var(--status-info); border-color: var(--border-info); background: var(--tint-info); }
.team-member-wt { grid-area: wt; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-dim); font-size: var(--text-xs); }
.team-member-session { display: none; }
.team-milestone { display: flex; flex-direction: column; gap: 2px; padding: 4px 6px; border: 1px solid var(--border-hairline); border-left-width: 2px; border-radius: 5px; min-width: 0; }
.team-milestone[data-status="done"] { border-left-color: var(--status-done); }
.team-milestone[data-status="active"] { border-left-color: var(--status-running); }
.team-milestone[data-status="pending"] { border-left-color: var(--status-idle); }
.team-milestone-head { display: flex; align-items: center; gap: 5px; min-width: 0; }
.team-milestone-id { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--font-mono); font-size: var(--text-sm); }
.team-milestone-after { display: flex; align-items: center; gap: 4px; flex-wrap: wrap; }
.team-milestone-after .chip { padding: 0 5px; border-radius: 4px; font-size: var(--text-xs); }
.team-milestone-content { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-mid); font-size: var(--text-xs); }
.team-milestone-content.is-empty { color: var(--faint); }
.team-event { display: flex; align-items: baseline; gap: 5px; flex-wrap: wrap; min-width: 0; font-size: var(--text-xs); }
.team-event-kind { padding: 0 5px; border: 1px solid var(--border-hairline); border-radius: 4px; font-size: 10px; color: var(--text-mid); }
.team-event-kind.is-dispatch { color: var(--status-info); border-color: var(--border-info); }
.team-event-kind.is-milestone { color: var(--status-done); border-color: var(--border-done); }
.team-event-kind.is-retire { color: var(--text-dim); }
.team-event-target { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--font-mono); color: var(--text-mid); }
.team-event-at { flex: none; color: var(--faint); }
.team-event-detail { flex: 1 1 100%; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--faint); }
`;
