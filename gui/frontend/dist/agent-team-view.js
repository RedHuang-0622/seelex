import { escapeHtml } from "./components.js";

// ── Agent Team 面板（右侧栏 · 状态 → Agent Team）──────────────
//
// 数据源：Application API（Bridge.AgentTeamPresets / AgentTeamView /
// AgentTeamMaterialize / AgentTeamPutRole / AgentTeamDeleteRole /
// AgentTeamSetOrder）。渲染只读，动作由 app.js 事件委托转成一次 Bridge 调用。
//
// 事实源边界：工作顺序的唯一事实是会话 lifecycle.order_policy/order_roles
// （由角色注册表派生下发），本模块**不缓存顺序**、不做乐观重排——每次动作后
// 重新拉取视图。定时 agent 单独分区、永不进入 order_roles（设计稿 §7.1）。

const ROLE_KIND_LABEL = {
  user: "user",
  main: "main",
  techlead: "TL",
  agent: "agent",
  timer: "定时"
};

// roleDisplayName 把逻辑角色名映射成用户可读的 agent 身份：DS-A2A 里
// main 是 EXEC（执行会话 a），tl/techlead 是 ADVISOR（评审会话 b）。
// 两个 agent 的正文此前都渲染成同一个 AGENT/Seelex，用户无法区分。
export function roleDisplayName(roleName, roleKind = "") {
  const name = String(roleName || "").trim();
  const kind = String(roleKind || "").trim();
  if (name === "main" || kind === "main") return "EXEC";
  if (name === "tl" || kind === "techlead") return "ADVISOR";
  if (name === "user" || kind === "user") return "USER";
  return name || kind || "AGENT";
}

function shortID(value) {
  const text = String(value || "");
  return text.length > 12 ? `${text.slice(0, 12)}…` : text;
}

const POLICY_LABEL = {
  goal_loop: "固定循环 user → main ↔ TL",
  user_main_decided: "由 user / main 编排",
  scheduled_only: "仅定时插话"
};

const POLICY_OPTIONS = [
  ["goal_loop", "固定循环"],
  ["user_main_decided", "user / main 编排"],
  ["scheduled_only", "仅定时插话"]
];

// Pinned 角色不可从工作顺序里摘除：群聊的起手与收口必须存在。
const PINNED_ROLES = new Set(["user", "main"]);

// normalizeAgentTeam 归一化 Bridge 下发的 TeamView（防御畸形载荷：
// 非对象 → 未装配空态；成员/定时分区非数组 → []；缺 role_name 的条目丢弃）。
export function normalizeAgentTeam(view) {
  const source = view && typeof view === "object" ? view : {};
  const members = normalizeMembers(source.members);
  const scheduled = normalizeMembers(source.scheduled);
  const orderRoles = Array.isArray(source.order_roles) ? source.order_roles.filter(name => typeof name === "string" && name) : [];
  return {
    sessionID: typeof source.session_id === "string" ? source.session_id : "",
    teamID: typeof source.team_id === "string" ? source.team_id : "",
    teamKind: typeof source.team_kind === "string" ? source.team_kind : "",
    orderPolicy: typeof source.order_policy === "string" ? source.order_policy : "",
    orderRoles,
    members,
    scheduled,
    configured: source.configured === true,
    floorRole: typeof source.floor_role === "string" ? source.floor_role : "",
    schedule: normalizeSchedule(source.schedule),
    designNotice: Array.isArray(source.design_notice) ? source.design_notice.filter(item => typeof item === "string" && item) : []
  };
}

// normalizeSchedule 归一化发言调度运行态（链表顺序 + 轮次/无进展记账 + 逃生状态）。
// 后端不下发 schedule 时返回 null，前端只显示静态顺序，不伪造运行态。
export function normalizeSchedule(value) {
  if (!value || typeof value !== "object") return null;
  const order = Array.isArray(value.order) ? value.order.filter(name => typeof name === "string" && name) : [];
  return {
    orderPolicy: typeof value.order_policy === "string" ? value.order_policy : "",
    order,
    nextRole: typeof value.next_role === "string" ? value.next_role : "",
    round: Number.isInteger(value.round) ? value.round : 0,
    roundLimit: Number.isInteger(value.round_limit) ? value.round_limit : 0,
    noProgress: Number.isInteger(value.no_progress) ? value.no_progress : 0,
    noProgressLimit: Number.isInteger(value.no_progress_limit) ? value.no_progress_limit : 0,
    stopped: value.stopped === true,
    stopReason: typeof value.stop_reason === "string" ? value.stop_reason : "",
    userSeat: typeof value.user_seat === "string" ? value.user_seat : "",
    unexecuted: Array.isArray(value.unexecuted) ? value.unexecuted.filter(name => typeof name === "string" && name) : []
  };
}

const STOP_REASON_LABEL = {
  round_limit: "到达轮次上限",
  no_progress: "连续无进展",
  no_executor: "环内无执行者",
  empty_ring: "空环",
  external_break: "外部停止（裁决/中断）"
};

const USER_SEAT_LABEL = {
  queued: "排队插话（有输入才占位）",
  member: "与员工同权（每轮固定占位）",
  absent: "不占位"
};

const ROLE_KIND_OPTIONS = [
  ["agent", "agent"],
  ["techlead", "techlead"],
  ["timer", "定时"]
];

const JOIN_POLICY_OPTIONS = [
  ["on_team_create", "入职即入顺序"],
  ["on_goal_create", "goal 上线时入顺序"],
  ["scheduled", "定时触发（不入顺序）"],
  ["on_demand", "按需（手动编排）"]
];

function normalizeMembers(items) {
  if (!Array.isArray(items)) return [];
  return items
    .filter(item => item && typeof item === "object" && typeof item.role_name === "string" && item.role_name)
    .map(item => ({
      roleName: item.role_name,
      roleKind: typeof item.role_kind === "string" ? item.role_kind : "",
      roleSessionID: typeof item.role_session_id === "string" ? item.role_session_id : "",
      orderIndex: Number.isInteger(item.order_index) ? item.order_index : -1,
      inOrder: item.in_order === true,
      joinPolicy: typeof item.join_policy === "string" ? item.join_policy : "",
      toolsPolicy: typeof item.tools_policy === "string" ? item.tools_policy : ""
    }));
}

// renderAgentTeam 渲染面板主体。presets 来自 Bridge.AgentTeamPresets()，
// 缺省时只渲染当前团队状态（不伪造 preset 按钮）。
//
// 面板按用户口径拆成两个不同的东西，各自条目化：
//   「员工栏」= 员工管理：谁在编、类型、独立会话、入职时机、工具策略；
//   「Team 栏」= 装配与编排：装哪个团队形态、顺序策略、工作顺序、发言调度、
//                定时 agent。两栏都是表格行（role=table），不再是 chip 混排。
export function renderAgentTeam(view, presets) {
  const team = normalizeAgentTeam(view);
  const presetList = Array.isArray(presets) ? presets.filter(item => item && typeof item.team_kind === "string" && item.team_kind) : [];
  const blocks = [];

  if (team.designNotice.length) {
    blocks.push(`<div class="team-notice" role="status">${team.designNotice.map(escapeHtml).join("；")}</div>`);
  }

  blocks.push(presetRow(presetList, team));

  if (!team.configured) {
    blocks.push('<div class="team-empty muted">当前会话未装配 AgentTeam：先装配一个团队形态，或直接给这个会话增加员工。</div>');
    // 未装配也要能"增加员工"：装配不是入职的前置条件，否则用户必须先套一个 preset 才能加人。
    blocks.push(staffSection(team));
    return `<div class="team-panel">${blocks.join("")}</div>`;
  }

  blocks.push(staffSection(team));
  blocks.push(teamSection(team));
  return `<div class="team-panel">${blocks.join("")}</div>`;
}

// staffSection 是「员工栏」：员工名单表（条目化）+ 一步实例化表单。
// 列口径按窄右栏（~250px）定：身份 / 类型 / 位置 / 操作；逻辑角色名跟在身份
// 后面（同格内联），角色独立会话 id 进 hover 提示与详情弹窗——照搬两张表列会把
// 每列挤到 20 多像素（真机截图里表头只剩"类型/会话/位置"）。
function staffSection(team) {
  return `${teamSectionHead("员工栏", team.members.length, "员工管理")}
    ${teamTable({
      label: "员工栏",
      head: ["员工", "类型", "位置", "操作"],
      rows: team.members.map(memberRow).join("") || teamEmptyRow("暂无员工：在下方入职一个角色"),
      columns: "minmax(0, 1fr) 42px 30px minmax(0, 1.1fr)"
    })}
    ${hireBlock(team)}`;
}

// teamSection 是「Team 栏」：装配参数 + 发言调度 + 工作顺序 + 定时 agent。
function teamSection(team) {
  return `${metaRow(team)}${scheduleBlock(team)}${orderBlock(team)}${team.scheduled.length ? scheduledBlock(team) : ""}`;
}

// teamSectionHead 是两栏共用的栏头（栏名 + 计数 + 用途提示）。
function teamSectionHead(title, count, hint) {
  return `<div class="section-title sub-title team-rail-head">
    <span>${escapeHtml(title)}</span>
    <span class="badge">${Number(count) || 0}</span>
    <span class="team-rail-hint muted">${escapeHtml(hint)}</span>
  </div>`;
}

// teamTable 渲染条目化表格：表头行 + 数据行（CSS 网格，role=table 语义）。
function teamTable({ label, head, rows, columns = "" }) {
  if (!rows) return "";
  const style = columns ? ` style="--team-cols:${escapeHtml(columns)}"` : "";
  return `<div class="team-table" role="table" aria-label="${escapeHtml(label)}"${style}>
    <div class="team-table-row is-head" role="row">${head.map(cell => `<span role="columnheader">${escapeHtml(cell)}</span>`).join("")}</div>
    ${rows}
  </div>`;
}

// teamRow 渲染一行表格数据；cells 是已 escape/已渲染好的单元格 HTML。
function teamRow(cells, options = {}) {
  const className = ["team-table-row", options.className].filter(Boolean).join(" ");
  const attrs = options.attrs ? ` ${options.attrs}` : "";
  return `<div class="${className}" role="row"${attrs}>${cells.map(cell => `<span role="cell">${cell}</span>`).join("")}</div>`;
}

// teamEmptyRow 渲染"整行说明"（跨满整行栅格）。
function teamEmptyRow(text) {
  return teamRow([`<span class="muted">${escapeHtml(text)}</span>`], { className: "is-empty" });
}

// scheduleBlock 显示**运行时**的发言调度（Team 栏里的条目化表格）：走到第几轮、
// 下一个谁发言、user 席位口径、逃生路径有没有被触发。没有 schedule（旧宿主/未
// 接线）时整块隐藏，不拿静态顺序冒充运行态。
function scheduleBlock(team) {
  const schedule = team.schedule;
  if (!schedule) return "";
  const limit = schedule.roundLimit > 0 ? String(schedule.roundLimit) : "∞";
  const nextState = schedule.stopped
    ? `<span class="chip team-stop">已收束 · ${escapeHtml(STOP_REASON_LABEL[schedule.stopReason] || schedule.stopReason || "停止")}</span>`
    : `<span class="chip team-next">下一个：${escapeHtml(schedule.nextRole || "—")}</span>`;
  const rows = [
    teamRow(['<span class="team-cell-key">轮次</span>', `<span class="team-cell-value">${schedule.round} / ${escapeHtml(limit)}</span>`]),
    teamRow(['<span class="team-cell-key">下一步</span>', nextState]),
    teamRow(['<span class="team-cell-key">user 席位</span>', `<span class="team-cell-value">${escapeHtml(USER_SEAT_LABEL[schedule.userSeat] || schedule.userSeat || "—")}</span>`]),
    teamRow(['<span class="team-cell-key">链表</span>', `<span class="team-cell-value team-schedule-order">${escapeHtml(schedule.order.join(" → ") || "—")}</span>`])
  ];
  if (schedule.unexecuted.length) {
    rows.push(teamRow(['<span class="team-cell-key">环内无执行者</span>', `<span class="team-cell-value">${escapeHtml(schedule.unexecuted.join("、"))}（占位但不会自动产生回合）</span>`]));
  }
  return `<div class="team-block team-schedule">
    <div class="section-title sub-title"><span>发言调度</span><span class="badge">${schedule.round}/${escapeHtml(limit)}</span></div>
    ${teamTable({ label: "发言调度", head: ["项", "值"], rows: rows.join(""), columns: "72px minmax(0, 1fr)" })}
  </div>`;
}

// hireBlock 是「一步实例化一个角色」的入口：新增与修改走同一条路径
// （AgentTeamInstantiateRole 按 role_name 幂等覆盖），因此表单只有一个。
function hireBlock(team) {
  const kindOptions = ROLE_KIND_OPTIONS.map(([value, label]) =>
    `<option value="${escapeHtml(value)}">${escapeHtml(label)}</option>`).join("");
  const joinOptions = JOIN_POLICY_OPTIONS.map(([value, label]) =>
    `<option value="${escapeHtml(value)}">${escapeHtml(label)}</option>`).join("");
  return `<div class="team-block team-hire">
    <div class="section-title sub-title"><span>${team.configured ? "入职 / 修改员工" : "入职员工"}</span><span class="badge">一步实例化</span></div>
    <form class="team-hire-form" data-team-hire-form autocomplete="off">
      <label class="team-field"><span>角色名</span>
        <input type="text" name="role_name" data-team-hire-name placeholder="reviewer / auditor…" required>
      </label>
      <label class="team-field"><span>类型</span>
        <select name="role_kind" data-team-hire-kind>${kindOptions}</select>
      </label>
      <label class="team-field"><span>入职时机</span>
        <select name="join_policy" data-team-hire-join>${joinOptions}</select>
      </label>
      <label class="team-field"><span>工具策略</span>
        <input type="text" name="tools_policy" data-team-hire-tools placeholder="read-only / 留空继承">
      </label>
      <div class="team-hire-actions">
        <button type="submit" class="text-button primary team-hire-submit" data-team-hire-submit>入职</button>
        <button type="button" class="text-button hidden" data-team-hire-cancel>取消修改</button>
        <span class="team-hire-hint muted" data-team-hire-hint>同名角色会被就地修改（幂等：不会新建第二个角色会话）。</span>
      </div>
    </form>
  </div>`;
}

// presetRow 是 Team 栏的装配入口（工具栏）：点一次装配一个团队形态
// （幂等：同名角色就地覆盖，不会新建第二个角色会话），右侧是刷新。
function presetRow(presets, team) {
  if (!presets.length) return "";
  const buttons = presets.map(preset => {
    const kind = preset.team_kind;
    const active = kind === team.teamKind;
    const label = active ? `${kind} 已装配` : `装配 ${kind}`;
    return `<button type="button" class="text-button team-preset${active ? " is-active" : ""}" data-team-materialize="${escapeHtml(kind)}"${active ? ' title="重复装配是幂等的，不会新建第二个角色会话"' : ""}>${escapeHtml(label)}</button>`;
  }).join("");
  return `<div class="team-toolbar" role="toolbar" aria-label="团队装配">${buttons}<button type="button" class="text-button team-refresh" data-team-refresh="1" data-tip="重新拉取员工表与工作顺序">刷新</button></div>`;
}

// metaRow 是 Team 栏的团队参数表（形态 / 顺序策略 / 当前发言权）。
function metaRow(team) {
  const policyLabel = POLICY_LABEL[team.orderPolicy] || team.orderPolicy || "未设置顺序策略";
  const options = POLICY_OPTIONS.map(([value, label]) =>
    `<option value="${escapeHtml(value)}"${value === team.orderPolicy ? " selected" : ""}>${escapeHtml(label)}</option>`).join("");
  const rows = [
    teamRow(['<span class="team-cell-key">团队形态</span>', `<span class="chip">${escapeHtml(team.teamKind || "team")}</span>`]),
    teamRow(['<span class="team-cell-key">顺序策略</span>', `<select class="team-policy-select" data-team-policy aria-label="顺序策略" title="${escapeHtml(policyLabel)}">${options}</select>`]),
    teamRow(['<span class="team-cell-key">发言权</span>', `<span class="team-floor" title="当前发言权（floor 随 message head 发布）">floor ${escapeHtml(team.floorRole || "—")}</span>`])
  ];
  return `${teamSectionHead("Team 栏", team.orderRoles.length, "装配与编排")}
    ${teamTable({ label: "Team 装配参数", head: ["项", "值"], rows: rows.join(""), columns: "72px minmax(0, 1fr)" })}`;
}

// orderBlock 是 Team 栏的工作顺序表：顺序事实源是会话 lifecycle.order_roles，
// 前端只提交用户改动后的整表，不做本地重排缓存。窄栏里动作键用紧凑记号
// （↑ / ↓ / ✕），完整口径进 hover 提示与 disabled title。
function orderBlock(team) {
  const rows = team.orderRoles.map((roleName, index) => {
    const pinned = PINNED_ROLES.has(roleName);
    const onFloor = roleName === team.floorRole;
    return teamRow([
      `<span class="team-order-index" title="工作顺序位置">${index + 1}</span>`,
      `<span class="team-order-role" title="${escapeHtml(roleName)}">${escapeHtml(roleDisplayName(roleName, ""))}</span>`,
      onFloor
        ? '<span class="chip team-floor-chip" title="当前发言权在这一位">发言中</span>'
        : `<span class="muted team-order-meta" title="逻辑角色名">${escapeHtml(roleName)}</span>`,
      `<span class="team-order-actions">
        <button type="button" class="text-button" data-team-action="up" data-team-role="${escapeHtml(roleName)}"${index === 0 ? " disabled" : ""} data-tip="上移">↑</button>
        <button type="button" class="text-button" data-team-action="down" data-team-role="${escapeHtml(roleName)}"${index === team.orderRoles.length - 1 ? " disabled" : ""} data-tip="下移">↓</button>
        <button type="button" class="text-button" data-team-action="remove" data-team-role="${escapeHtml(roleName)}"${pinned ? ' disabled title="user/main 是群聊起手与收口，不能摘除"' : ' data-tip="从工作顺序摘除（保留角色）"'}>✕</button>
      </span>`
    ], { className: onFloor ? "is-floor" : "", attrs: `data-team-order-role="${escapeHtml(roleName)}"` });
  });
  const outside = team.members.filter(member => !member.inOrder && member.roleKind !== "timer");
  const restore = outside.length
    ? `<div class="team-order-outside">未排入顺序：${outside.map(member =>
      `<button type="button" class="text-button" data-team-action="restore" data-team-role="${escapeHtml(member.roleName)}" title="加入工作顺序末尾">+ ${escapeHtml(member.roleName)}</button>`).join("")}</div>`
    : "";
  return `<div class="team-block">
    <div class="section-title sub-title"><span>工作顺序</span><span class="badge">${team.orderRoles.length}</span></div>
    ${teamTable({
      label: "工作顺序",
      head: ["#", "员工", "状态", "动作"],
      rows: rows.join("") || teamEmptyRow("工作顺序为空"),
      columns: "20px minmax(0, 1fr) minmax(0, .85fr) 66px"
    })}
    ${restore}
  </div>`;
}

// memberRow 渲染「员工栏」的一行员工：身份（EXEC/ADVISOR/角色名）+ 逻辑角色名
// 内联 + 类型 + 工作顺序位置 + 操作（编辑 / 删除）。角色独立会话 id 在身份按钮的
// hover 提示里（窄栏里它换不来可读性，完整信息在「查看会话」弹窗）。
function memberRow(member) {
  const kind = ROLE_KIND_LABEL[member.roleKind] || member.roleKind || "—";
  const display = roleDisplayName(member.roleName, member.roleKind);
  const pinned = PINNED_ROLES.has(member.roleName);
  const position = member.inOrder ? `#${member.orderIndex + 1}` : "—";
  const session = shortID(member.roleSessionID) || "会话未创建";
  return teamRow([
    `<button type="button" class="text-button team-member-name" data-team-role-open="${escapeHtml(member.roleName)}" data-team-role-session="${escapeHtml(member.roleSessionID)}" data-tip="查看 ${escapeHtml(display)} 的独立会话\nrole=${escapeHtml(member.roleName)} · ${escapeHtml(session)}" aria-label="查看 ${escapeHtml(display)} 的独立会话">${escapeHtml(display)}</button><span class="team-member-role" title="逻辑角色名（metadata，不是 provider role）">${escapeHtml(member.roleName)}</span>`,
    `<span class="chip">${escapeHtml(kind)}</span>`,
    `<span class="team-member-pos" title="工作顺序位置">${escapeHtml(position)}</span>`,
    `<span class="team-member-actions">
      <button type="button" class="text-button team-member-edit" data-team-edit="${escapeHtml(member.roleName)}" data-team-edit-kind="${escapeHtml(member.roleKind)}" data-team-edit-join="${escapeHtml(member.joinPolicy)}" data-team-edit-tools="${escapeHtml(member.toolsPolicy)}"${pinned ? ' disabled title="user/main 由会话本身提供，配置不可改"' : ' data-tip="回填到下方入职表单（改完点入职）"'}>编辑</button>
      <button type="button" class="text-button team-member-remove" data-team-delete="${escapeHtml(member.roleName)}"${pinned ? ' disabled title="user/main 由会话本身提供，不能删除"' : ' data-tip="从注册表与工作顺序中删除该角色"'}>删除</button>
    </span>`
  ]);
}

// ── 角色会话详情（成员行「查看」）──────────────────────────────
//
// 数据源：Bridge.AgentTeamRoleSnapshot（application/contract/dto.RoleSnapshot）。
// 目的：EXEC（main）与 ADVISOR（tl）是两个独立会话；主对话只显示 EXEC 的
// 可见消息，ADVISOR 自己的行在这里按角色身份单独列出，避免"两个 agent 都叫
// AGENT"的歧义。纯渲染，不写任何状态。

function normalizeRoleRows(items) {
  if (!Array.isArray(items)) return [];
  return items.map(item => {
    const row = item && typeof item === "object" ? item : {};
    return {
      seq: Number.isInteger(row.seq) ? row.seq : 0,
      role: typeof row.role === "string" ? row.role : "",
      roleName: typeof row.role_name === "string" ? row.role_name : "",
      roleSessionID: typeof row.role_session_id === "string" ? row.role_session_id : "",
      content: typeof row.content === "string" ? row.content : "",
      reasoning: typeof row.reasoning_content === "string" ? row.reasoning_content : "",
      toolCalls: Array.isArray(row.tool_calls) ? row.tool_calls.filter(call => call && typeof call === "object") : []
    };
  }).filter(row => row.content || row.reasoning || row.toolCalls.length);
}

export function normalizeRoleSession(snapshot) {
  const source = snapshot && typeof snapshot === "object" ? snapshot : {};
  const floor = source.floor && typeof source.floor === "object" ? source.floor : {};
  const draftRows = Array.isArray(source.draft_rows)
    ? source.draft_rows.map(item => (item && typeof item === "object" ? item.event : null))
    : [];
  return {
    mainSessionID: typeof source.main_session_id === "string" ? source.main_session_id : "",
    roleName: typeof source.role_name === "string" ? source.role_name : "",
    roleSessionID: typeof source.role_session_id === "string" ? source.role_session_id : "",
    root: typeof source.root === "string" ? source.root : "",
    joinSeqID: Number.isFinite(source.join_seq_id) ? source.join_seq_id : 0,
    orderPolicy: typeof source.order_policy === "string" ? source.order_policy : "",
    orderRoles: Array.isArray(source.order_roles) ? source.order_roles.filter(name => typeof name === "string" && name) : [],
    floorRole: typeof floor.role_name === "string" ? floor.role_name : "",
    roleRows: normalizeRoleRows(source.role_rows),
    draftRows: normalizeRoleRows(draftRows),
    unassignedRoleRows: Number.isInteger(source.unassigned_role_rows) ? source.unassigned_role_rows : 0,
    designWarnings: Array.isArray(source.design_warnings) ? source.design_warnings.filter(item => typeof item === "string" && item) : []
  };
}

export function renderRoleSessionDetail(snapshot) {
  const view = normalizeRoleSession(snapshot);
  const display = roleDisplayName(view.roleName);
  const rows = view.roleRows.concat(view.draftRows);
  const warnings = view.designWarnings.length
    ? `<div class="team-notice" role="status">${view.designWarnings.map(escapeHtml).join("；")}</div>`
    : "";
  const header = `<div class="role-session-ident">
      <span class="chip role-session-identity">${escapeHtml(display)}</span>
      <span class="role-session-name" title="role_name（逻辑角色名）">${escapeHtml(view.roleName || "—")}</span>
      <span class="role-session-sid muted" title="role_session_id">${escapeHtml(shortID(view.roleSessionID) || "会话未创建")}</span>
      ${view.floorRole ? `<span class="chip team-floor-chip">发言中 · ${escapeHtml(view.floorRole)}</span>` : ""}
    </div>
    <div class="role-session-meta muted">
      main ${escapeHtml(shortID(view.mainSessionID) || "—")} · join_seq ${view.joinSeqID} · policy ${escapeHtml(view.orderPolicy || "—")}${view.orderRoles.length ? ` · order ${escapeHtml(view.orderRoles.join(" → "))}` : ""}
    </div>`;
  if (!rows.length) {
    return `<div class="role-session-detail" data-role-session="${escapeHtml(view.roleName)}">${warnings}${header}
      <div class="role-session-empty muted">该角色还没有独立会话行（未发言或尚未同步）。</div></div>`;
  }
  const body = rows.map(row => `<article class="role-session-row">
      <div class="role-session-row-head"><strong>${escapeHtml(display)}</strong><span class="muted">seq ${row.seq}</span><span class="muted">${escapeHtml(row.role)}</span></div>
      ${row.reasoning ? `<div class="role-session-reasoning muted">${escapeHtml(row.reasoning)}</div>` : ""}
      ${row.content ? `<div class="role-session-text">${escapeHtml(row.content)}</div>` : ""}
      ${row.toolCalls.map(call => `<div class="role-session-tool muted">tool ${escapeHtml(call.name || "?")} ${escapeHtml(call.arguments || "")}</div>`).join("")}
    </article>`).join("");
  return `<div class="role-session-detail" data-role-session="${escapeHtml(view.roleName)}">${warnings}${header}<div class="role-session-rows">${body}</div></div>`;
}

// scheduledBlock 是 Team 栏里的定时 agent 表：定时 agent 永不进 order_roles
// （设计稿 §7.1），所以它单列一张表，别让人以为它们也在工作顺序里。
function scheduledBlock(team) {
  const rows = team.scheduled.map(member => teamRow([
    `<span class="team-member-name">${escapeHtml(roleDisplayName(member.roleName, member.roleKind))}</span><span class="team-member-role" title="逻辑角色名">${escapeHtml(member.roleName)}</span>`,
    `<span class="chip">${escapeHtml(ROLE_KIND_LABEL[member.roleKind] || member.roleKind || "定时")}</span>`,
    '<span class="team-scheduled-note muted">定时插话 · 不参与工作顺序</span>'
  ])).join("");
  return `<div class="team-block">
    <div class="section-title sub-title"><span>定时 agent</span><span class="badge">${team.scheduled.length}</span></div>
    ${teamTable({
      label: "定时 agent",
      head: ["员工", "类型", "说明"],
      rows,
      columns: "minmax(0, 1fr) 44px minmax(0, 1.3fr)"
    })}
  </div>`;
}

// nextAgentTeamOrder 把一次顺序动作换算成新的 order_roles（纯函数，便于测试）。
// 返回 null 表示动作非法（角色不在表内、pin 角色被摘除、越界移动）。
export function nextAgentTeamOrder(view, action, roleName) {
  const team = normalizeAgentTeam(view);
  const order = [...team.orderRoles];
  const index = order.indexOf(roleName);
  if (action === "restore") {
    if (index >= 0) return null;
    order.push(roleName);
    return { policy: team.orderPolicy, orderRoles: order };
  }
  if (index < 0) return null;
  if (action === "remove") {
    if (PINNED_ROLES.has(roleName)) return null;
    order.splice(index, 1);
    return { policy: team.orderPolicy, orderRoles: order };
  }
  const target = action === "up" ? index - 1 : action === "down" ? index + 1 : -1;
  if (target < 0 || target >= order.length) return null;
  [order[index], order[target]] = [order[target], order[index]];
  return { policy: team.orderPolicy, orderRoles: order };
}

// isPinnedRole 暴露 pin 规则给 app.js 的按钮禁用判断（与渲染同源）。
export function isPinnedRole(roleName) {
  return PINNED_ROLES.has(roleName);
}
