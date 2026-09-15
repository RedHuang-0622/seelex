import { escapeHtml } from "./components.js";

// ── Agent Team 面板（右侧栏 · 状态 → Agent Team）──────────────
//
// 数据源：Application API（Bridge.AgentTeamPresets / AgentTeamView /
// AgentTeamLibrary / AgentTeamGlobalConfig / AgentTeamMaterialize /
// AgentTeamMaterializeTeam / AgentTeamSaveTeam / AgentTeamSaveCurrentTeam /
// AgentTeamDeleteTeam / AgentTeamPutRole / AgentTeamDeleteRole / AgentTeamSetOrder /
// AgentTeamInstantiateRole / AgentTeamSaveEmployee / AgentTeamDeleteEmployee /
// AgentTeamSetDefaultOrder / AgentTeamPublishToGlobal / AgentTeamOptimizePrompt）。
// 渲染只读，动作由 app.js 事件委托转成一次 Bridge 调用。
//
// 事实源边界：
//   - 工作顺序的唯一事实是会话 lifecycle.order_policy/order_roles，员工栏就是
//     这个顺序（拖拽只提交整表，本模块不缓存、不乐观重排）；
//   - 员工配置的唯一事实是本会话的角色注册表（session/team/roles.json）；
//   - 团队库 / 员工库 / 默认顺序是**全局**母本（数据根下 `team/`）：会话读它的
//     深拷贝副本，会话内的入职/改序只改副本，只有「确认·普及搭配到全局」才回写。
//
// 交互口径（用户要求）：
//   - 发言顺序在员工栏直接拖拽调整（行首 ≡ 是拖拽条）；
//   - 员工入职/修改、团队新建/编辑都是**冷加载面板**：默认不渲染表单，点 + 或
//     编辑才弹出，面板带关闭键；
//   - 团队用一张小表管理（不再散装），并支持新增团队。

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

// 工具权限口径与后端 dto.ToolPolicy* 一一对应（空 = 继承宿主默认）。
export const TOOLS_POLICY_OPTIONS = [
  ["", "继承宿主默认"],
  ["readonly", "只读（不写文件 / 不执行命令）"],
  ["readwrite", "读写"],
  ["full", "完全"]
];

const MODEL_POLICY_OPTIONS = [
  ["", "继承宿主模型"],
  ["same-as-exec", "与 EXEC 同模型"]
];

// toolsPolicyLabel 把权限登记值映射成短标签（员工行里的权限 chip）。
export function toolsPolicyLabel(policy) {
  const value = String(policy || "").trim();
  const found = TOOLS_POLICY_OPTIONS.find(([code]) => code === value);
  if (!found) return value || "继承";
  return value ? found[1].replace(/（.*/, "") : "继承";
}

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

// normalizeTeamLibrary 归一化 Bridge 下发的团队库（**全局**作用域模板清单）。
// 非对象 → 空库（不是错误）；缺 team_id 的条目丢弃。
export function normalizeTeamLibrary(library) {
  const source = library && typeof library === "object" ? library : {};
  const teams = Array.isArray(source.teams) ? source.teams : [];
  return {
    configured: source.configured === true,
    teams: teams
      .filter(entry => entry && typeof entry === "object" && typeof entry.team_id === "string" && entry.team_id)
      .map(entry => ({
        teamID: entry.team_id,
        teamKind: typeof entry.team_kind === "string" ? entry.team_kind : "",
        name: typeof entry.name === "string" ? entry.name : "",
        orderPolicy: typeof entry.order_policy === "string" ? entry.order_policy : "",
        orderRoles: Array.isArray(entry.order_roles) ? entry.order_roles.filter(name => typeof name === "string" && name) : [],
        origin: typeof entry.origin === "string" ? entry.origin : "",
        roles: normalizeRoleSpecs(entry.roles)
      }))
  };
}

// normalizeTeamGlobal 归一化 Bridge 下发的**全局母本**（团队库 + 员工库 +
// 默认顺序）与会话副本投影。非对象 → 空母本；缺 role_name 的条目丢弃。
export function normalizeTeamGlobal(config) {
  const source = config && typeof config === "object" ? config : {};
  const employees = source.employees && typeof source.employees === "object" ? source.employees : {};
  const order = source.order && typeof source.order === "object" ? source.order : {};
  const composition = source.composition && typeof source.composition === "object" ? source.composition : {};
  return {
    employees: normalizeRoleSpecs(employees.employees),
    employeesConfigured: employees.configured === true,
    orderPolicy: typeof order.order_policy === "string" ? order.order_policy : "",
    orderRoles: Array.isArray(order.order_roles) ? order.order_roles.filter(name => typeof name === "string" && name) : [],
    orderConfigured: order.configured === true,
    composition: {
      sessionID: typeof composition.session_id === "string" ? composition.session_id : "",
      teamID: typeof composition.team_id === "string" ? composition.team_id : "",
      teamKind: typeof composition.team_kind === "string" ? composition.team_kind : "",
      orderPolicy: typeof composition.order_policy === "string" ? composition.order_policy : "",
      orderRoles: Array.isArray(composition.order_roles) ? composition.order_roles.filter(name => typeof name === "string" && name) : [],
      employees: normalizeRoleSpecs(composition.employees)
    }
  };
}

// teamGlobalDrift 判定会话副本相对全局母本有没有差异（纯展示：差异存在时提示
// "确认普及后才会进全局"，不做任何写入）。
export function teamGlobalDrift(global) {
  if (!global || typeof global !== "object") return false;
  const master = global.employees.map(role => role.roleName).sort().join(",");
  const current = global.composition.employees.map(role => role.roleName).sort().join(",");
  return master !== current || global.orderRoles.join(",") !== global.composition.orderRoles.join(",");
}

// globalMasterBlock 是「全局母本」块：员工库表 + 默认顺序 + 会话副本差异 +
// 「确认·普及搭配到全局」。母本是全局粒度事实，所有会话共用一份；会话读的是它的
// 深拷贝副本，会话内的入职/改序只改副本——只有这个按钮会把副本写回母本。
function globalMasterBlock(global) {
  const rows = [];
  for (const role of global.employees) {
    rows.push(teamRow([
      `<span class="team-library-name" title="${escapeHtml(role.roleName)}">${escapeHtml(roleDisplayName(role.roleName, role.roleKind))}</span>`,
      `<span class="chip">${escapeHtml(ROLE_KIND_LABEL[role.roleKind] || role.roleKind || "agent")}</span>`,
      `<span class="team-perm-chip${role.toolsPolicy ? "" : " is-inherit"}">${escapeHtml(toolsPolicyLabel(role.toolsPolicy))}</span>`,
      `<span class="team-library-actions">
        <button type="button" class="text-button" data-team-employee-delete="${escapeHtml(role.roleName)}" data-tip="从全局员工库删除（已装配的会话副本不受影响）">删除</button>
      </span>`
    ], { className: "team-library-row", attrs: `data-team-employee="${escapeHtml(role.roleName)}"` }));
  }
  const drift = teamGlobalDrift(global);
  const driftNote = drift
    ? '<span class="team-drift">会话副本与母本有差异：点「确认·普及搭配到全局」才会写回</span>'
    : '<span class="muted">会话副本与母本一致</span>';
  const metaRows = [
    teamRow([
      '<span class="team-cell-key">默认顺序</span>',
      `<span class="team-cell-value team-schedule-order">${escapeHtml(global.orderRoles.join(" → ") || "—")}</span><span class="chip">${escapeHtml(global.orderPolicy || "—")}</span>`
    ]),
    teamRow([
      '<span class="team-cell-key">会话副本</span>',
      `<span class="team-cell-value team-schedule-order">${escapeHtml(global.composition.orderRoles.join(" → ") || "—")}</span>`
    ]),
    teamRow(['<span class="team-cell-key">差异</span>', driftNote])
  ];
  return `${teamRailHead("全局母本", global.employees.length, "全局粒度 · 会话读的是深拷贝副本",
    `<button type="button" class="text-button" data-team-default-order="1" data-tip="只把当前会话的发言顺序设为全局默认顺序">顺序设为默认</button>
     <button type="button" class="text-button" data-team-publish-global="1" data-tip="把当前会话的在编员工与发言顺序写回全局母本（员工库 + 默认顺序 + 一条团队库条目）">确认·普及搭配到全局</button>`)}
    ${teamTable({
      label: "全局员工库",
      head: ["员工（全局）", "类型", "权限", "操作"],
      rows: rows.join("") || teamEmptyRow("全局员工库为空：点「确认·普及搭配到全局」把当前会话的员工写回母本"),
      columns: "minmax(0, 1.5fr) 38px 64px minmax(0, 1fr)"
    })}
    ${teamTable({
      label: "全局默认顺序与会话副本",
      head: ["项", "值"],
      rows: metaRows.join(""),
      columns: "72px minmax(0, 1fr)"
    })}`;
}

function normalizeRoleSpecs(items) {
  if (!Array.isArray(items)) return [];
  return items
    .filter(item => item && typeof item === "object" && typeof item.role_name === "string" && item.role_name)
    .map(item => ({
      roleName: item.role_name,
      roleKind: typeof item.role_kind === "string" ? item.role_kind : "",
      systemPrompt: typeof item.system_prompt === "string" ? item.system_prompt : "",
      toolsPolicy: typeof item.tools_policy === "string" ? item.tools_policy : "",
      modelPolicy: typeof item.model_policy === "string" ? item.model_policy : "",
      joinPolicy: typeof item.join_policy === "string" ? item.join_policy : "",
      presencePolicy: typeof item.presence_policy === "string" ? item.presence_policy : ""
    }));
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
      toolsPolicy: typeof item.tools_policy === "string" ? item.tools_policy : "",
      systemPrompt: typeof item.system_prompt === "string" ? item.system_prompt : "",
      modelPolicy: typeof item.model_policy === "string" ? item.model_policy : "",
      presencePolicy: typeof item.presence_policy === "string" ? item.presence_policy : ""
    }));
}

// renderAgentTeam 渲染面板主体。presets 来自 Bridge.AgentTeamPresets()、
// library 来自 Bridge.AgentTeamLibrary()、global 来自 Bridge.AgentTeamGlobalConfig()
// （**全局母本**：团队库 + 员工库 + 默认顺序 + 会话副本投影）；缺省时只渲染当前
// 会话状态（不伪造按钮）。
//
// 面板分四块，各自条目化：
//   「团队库」= 团队模板表：装配 / 入库 / 删除 / 新建（团队不再散装）；
//   「全局母本」= 员工库 + 默认顺序 + 「确认·普及搭配到全局」（母本 vs 会话副本）；
//   「员工栏」= 在编员工 + 发言顺序（拖拽 ≡ 直接调序） + 冷加载入职/修改面板；
//   「Team 栏」= 装配参数 + 发言调度 + 定时 agent。
export function renderAgentTeam(view, presets, library, global) {
  const team = normalizeAgentTeam(view);
  const presetList = Array.isArray(presets) ? presets.filter(item => item && typeof item.team_kind === "string" && item.team_kind) : [];
  const blocks = [];

  if (team.designNotice.length) {
    blocks.push(`<div class="team-notice" role="status">${team.designNotice.map(escapeHtml).join("；")}</div>`);
  }

  blocks.push(teamLibraryBlock(team, presetList, normalizeTeamLibrary(library)));
  if (global && typeof global === "object") {
    blocks.push(globalMasterBlock(normalizeTeamGlobal(global)));
  }
  if (!team.configured) {
    blocks.push('<div class="team-empty muted">当前会话未装配 AgentTeam：装配一支团队，或直接给这个会话增加员工。</div>');
  }
  blocks.push(staffSection(team));

  if (team.configured) {
    blocks.push(teamSection(team));
  }
  return `<div class="team-panel">${blocks.join("")}</div>`;
}

// teamLibraryBlock 是「团队库」表：团队模板一行一支（内置形态以"模板（未入库）"
// 单独列出），操作是装配 / 存进团队库 / 编辑 / 删除；右上角 + 新建团队开冷加载面板。
function teamLibraryBlock(team, presets, library) {
  const rows = [];
  for (const entry of library.teams) {
    const members = entry.roles.length;
    const active = entry.teamKind === team.teamKind;
    rows.push(teamRow([
      `<span class="team-library-name" title="${escapeHtml(entry.teamID)}">${escapeHtml(entry.name || entry.teamID)}</span>`,
      `<span class="chip">${escapeHtml(entry.teamKind || "team")}</span>`,
      `<span class="team-library-meta" title="角色数 / 顺序策略">${members} 人 · ${escapeHtml(entry.orderPolicy || "—")}</span>`,
      `<span class="team-library-actions">
        <button type="button" class="text-button" data-team-materialize-team="${escapeHtml(entry.teamID)}"${active ? ' title="重复装配是幂等的，不会新建第二个角色会话"' : ' data-tip="把这支团队装配到当前会话"'}${active ? " disabled" : ""}>${active ? "已装配" : "装配"}</button>
        <button type="button" class="text-button" data-team-edit-team="${escapeHtml(entry.teamID)}" data-tip="编辑团队（名称/形态/成员/顺序策略）">编辑</button>
        <button type="button" class="text-button" data-team-delete-team="${escapeHtml(entry.teamID)}" data-tip="从团队库删除">删除</button>
      </span>`
    ], { className: "team-library-row", attrs: `data-team-library-entry="${escapeHtml(entry.teamID)}"` }));
  }
  for (const preset of presets) {
    const kind = preset.team_kind;
    const active = kind === team.teamKind;
    const roles = Array.isArray(preset.roles) ? preset.roles.filter(role => role && typeof role.role_name === "string" && role.role_name) : [];
    rows.push(teamRow([
      `<span class="team-library-name">${escapeHtml(kind)}</span><span class="team-perm-chip is-inherit">内置</span>`,
      `<span class="chip">${escapeHtml(kind)}</span>`,
      `<span class="team-library-meta">${roles.length} 配置 · ${escapeHtml(preset.order_policy || "—")}</span>`,
      `<span class="team-library-actions">
        <button type="button" class="text-button" data-team-materialize="${escapeHtml(kind)}"${active ? ' title="重复装配是幂等的，不会新建第二个角色会话"' : ' data-tip="按内置形态装配"'}${active ? " disabled" : ""}>${active ? "已装配" : "装配"}</button>
        <button type="button" class="text-button" data-team-save-template="${escapeHtml(kind)}" data-tip="把内置形态存成团队库条目（之后可改成员/顺序策略）">存入库</button>
      </span>`
    ], { className: "team-library-row", attrs: `data-team-library-template="${escapeHtml(kind)}"` }));
  }
  return `${teamRailHead("团队库", library.teams.length, `${presets.length} 个内置形态`,
    `<button type="button" class="text-button" data-team-save-current="1" data-tip="把当前会话在编员工（含提示词/权限）存成一支团队">存当前会话</button>
     <button type="button" class="text-button" data-team-open-team="1" data-tip="新建团队：空白 / 从当前会话 / 从内置模板">+ 新建团队</button>`)}
    ${teamTable({
      label: "团队库",
      head: ["团队", "形态", "规模", "操作"],
      rows: rows.join("") || teamEmptyRow("团队库为空：点「+ 新建团队」建第一支"),
      columns: "minmax(0, 1fr) minmax(0, .7fr) minmax(0, 1fr) minmax(0, 1.3fr)"
    })}
    <div class="team-editor-slot" data-team-team-slot hidden></div>`;
}

// staffSection 是「员工栏」：在编员工 + 发言顺序（同一张表：顺序就是发言次序）。
// 拖动行首的 ≡（或键盘 ↑/↓）调整顺序；点 + 入职、点「编辑」改提示词与权限。
function staffSection(team) {
  const rows = staffRows(team);
  return `${teamRailHead("员工栏", team.members.length, team.configured ? "拖拽 ≡ 调整发言顺序" : "尚未装配团队",
    `<button type="button" class="text-button" data-team-open-hire="1" data-tip="入职一个新员工（角色名 / 提示词 / 权限）">+ 入职</button>`)}
    ${teamTable({
      label: "员工栏",
      head: ["员工（拖拽调序 · 权限）", "类型", "位置", "操作"],
      rows: rows.join("") || teamEmptyRow("暂无员工：点「+ 入职」增加一个角色"),
      columns: "minmax(0, 1.5fr) 38px 32px minmax(0, 1.5fr)"
    })}
    <div class="team-drop-end" data-team-order-drop="end" title="把员工拖到这里 = 排到发言顺序末尾">拖到这里 → 排到发言顺序末尾</div>
    <div class="team-editor-slot" data-team-hire-slot hidden></div>`;
}

// staffRows 渲染员工行：已排入顺序的按顺序在前，未排入的跟在后面（可拖进顺序）。
function staffRows(team) {
  const ordered = team.orderRoles.map(name => team.members.find(member => member.roleName === name) || {
    roleName: name, roleKind: "", roleSessionID: "", orderIndex: -1, inOrder: true,
    joinPolicy: "", toolsPolicy: "", systemPrompt: "", modelPolicy: "", presencePolicy: ""
  });
  const outside = team.members.filter(member => !team.orderRoles.includes(member.roleName) && member.roleKind !== "timer");
  const scheduled = team.members.filter(member => member.roleKind === "timer" && !team.orderRoles.includes(member.roleName));
  return ordered.map((member, index) => staffRow(member, index, team))
    .concat(outside.map(member => staffRow(member, -1, team)))
    .concat(scheduled.map(member => staffRow(member, -1, team, true)));
}

// staffRow 渲染一行员工。窄右栏里用"可换行的条目行"而不是多列表格：列多了每列只剩
// 二十几像素（旧版表头被挤成"类型/会话/位置"就是这个原因）。
function staffRow(member, orderIndex, team, scheduled = false) {
  const display = roleDisplayName(member.roleName, member.roleKind);
  const pinned = PINNED_ROLES.has(member.roleName);
  const kind = ROLE_KIND_LABEL[member.roleKind] || member.roleKind || "—";
  const session = shortID(member.roleSessionID) || "会话未创建";
  const onFloor = member.roleName === team.floorRole;
  const inOrder = orderIndex >= 0;
  const position = inOrder ? `#${orderIndex + 1}` : scheduled ? "定时" : "未排入";
  const perm = toolsPolicyLabel(member.toolsPolicy);
  const promptChip = member.systemPrompt
    ? `<span class="team-perm-chip" title="已登记提示词（${escapeHtml(String(member.systemPrompt.length))} 字符）">提示词</span>`
    : `<span class="team-perm-chip is-inherit" title="未登记提示词">无提示词</span>`;
  const actions = [
    `<button type="button" class="text-button" data-team-action="up" data-team-role="${escapeHtml(member.roleName)}"${!inOrder || orderIndex === 0 ? " disabled" : ""} data-tip="上移">↑</button>`,
    `<button type="button" class="text-button" data-team-action="down" data-team-role="${escapeHtml(member.roleName)}"${!inOrder || orderIndex === team.orderRoles.length - 1 ? " disabled" : ""} data-tip="下移">↓</button>`,
    pinned
      ? `<button type="button" class="text-button" data-team-action="remove" data-team-role="${escapeHtml(member.roleName)}" disabled title="user/main 是群聊起手与收口，不能摘除">✕</button>`
      : `<button type="button" class="text-button" data-team-action="remove" data-team-role="${escapeHtml(member.roleName)}"${inOrder ? "" : " disabled"} data-tip="从工作顺序摘除（保留角色）">✕</button>`,
    `<button type="button" class="text-button team-member-edit" data-team-edit="${escapeHtml(member.roleName)}"${pinned ? ' disabled title="user/main 由会话本身提供，配置不可改"' : ' data-tip="打开编辑面板（提示词 / 权限 / 类型）"'}>编辑</button>`,
    `<button type="button" class="text-button team-member-remove" data-team-delete="${escapeHtml(member.roleName)}"${pinned ? ' disabled title="user/main 由会话本身提供，不能删除"' : ' data-tip="从注册表与工作顺序中删除该角色"'}>删除</button>`
  ].join("");
  const handle = scheduled
    ? '<span class="team-drag-handle is-static" title="定时 agent 不参与发言顺序，不能排序">·</span>'
    : `<span class="team-drag-handle" data-team-drag="${escapeHtml(member.roleName)}" draggable="true" role="button" tabindex="0" title="拖拽调整发言顺序" aria-label="拖拽调整 ${escapeHtml(display)} 的发言顺序">≡</span>`;
  return teamRow([
    `<span class="team-staff-main">
      ${handle}
      <button type="button" class="text-button team-member-name" data-team-role-open="${escapeHtml(member.roleName)}" data-team-role-session="${escapeHtml(member.roleSessionID)}" data-tip="查看 ${escapeHtml(display)} 的独立会话\nrole=${escapeHtml(member.roleName)} · ${escapeHtml(session)}" aria-label="查看 ${escapeHtml(display)} 的独立会话">${escapeHtml(display)}</button>
      <span class="team-member-role" title="逻辑角色名（metadata，不是 provider role）">${escapeHtml(member.roleName)}</span>
      <span class="team-perm-chip${perm === "继承" ? " is-inherit" : ""}" title="工具权限（登记在角色注册表）">${escapeHtml(perm)}</span>
      ${promptChip}
      ${onFloor ? '<span class="chip team-floor-chip" title="当前发言权在这一位">发言中</span>' : ""}
    </span>`,
    `<span class="chip">${escapeHtml(kind)}</span>`,
    `<span class="team-member-pos" title="${inOrder ? "工作顺序位置" : "不在工作顺序里"}">${escapeHtml(position)}</span>`,
    `<span class="team-member-actions">${actions}</span>`
  ], {
    className: `team-staff-row${onFloor ? " is-floor" : ""}${scheduled ? " team-outside-row" : ""}`,
    attrs: `data-team-staff-role="${escapeHtml(member.roleName)}" data-team-order-role="${escapeHtml(member.roleName)}" data-team-in-order="${inOrder ? "1" : "0"}" draggable="true"`
  });
}

// hirePanel 是「入职 / 修改员工」的冷加载面板：默认不渲染，点 + / 编辑才注入。
// staff 为空 = 新增；否则回填该员工的登记值（提示词/权限必须回填，否则一次编辑
// 就会把用户登记的东西清空）。
export function hirePanel(team, member) {
  const editing = Boolean(member && member.roleName);
  const role = member || {};
  const pinnedRole = editing && isPinnedRole(role.roleName);
  const kindOptions = options(ROLE_KIND_OPTIONS, role.roleKind || "agent");
  const joinOptions = options(JOIN_POLICY_OPTIONS, role.joinPolicy || "on_team_create");
  const toolsOptions = options(TOOLS_POLICY_OPTIONS, role.toolsPolicy || "");
  const modelOptions = options(MODEL_POLICY_OPTIONS, role.modelPolicy || "");
  return `<div class="team-editor" data-team-editor="hire">
    <div class="team-editor-head">
      <span class="team-editor-title">${editing ? `修改员工 · ${escapeHtml(roleDisplayName(role.roleName, role.roleKind))}` : "入职员工"}</span>
      <button type="button" class="team-editor-close" data-team-editor-close="1" title="关闭面板（Esc）" aria-label="关闭入职面板">✕</button>
    </div>
    <form class="team-hire-form" data-team-hire-form autocomplete="off">
      <label class="team-field"><span>角色名</span>
        <input type="text" name="role_name" data-team-hire-name placeholder="reviewer / auditor…" value="${escapeHtml(role.roleName || "")}"${editing ? " readonly" : ""} required>
      </label>
      <div class="team-field-row">
        <label class="team-field"><span>类型</span>
          <select name="role_kind" data-team-hire-kind>${kindOptions}</select>
        </label>
        <label class="team-field"><span>入职时机</span>
          <select name="join_policy" data-team-hire-join>${joinOptions}</select>
        </label>
      </div>
      <div class="team-field-row">
        <label class="team-field"><span>权限</span>
          <select name="tools_policy" data-team-hire-tools>${toolsOptions}</select>
        </label>
        <label class="team-field"><span>模型</span>
          <select name="model_policy" data-team-hire-model>${modelOptions}</select>
        </label>
      </div>
      <label class="team-field"><span>在席策略</span>
        <input type="text" name="presence_policy" data-team-hire-presence placeholder="留空继承（online_when_goal_active…）" value="${escapeHtml(role.presencePolicy || "")}">
      </label>
      <label class="team-field"><span>员工提示词</span>
        <textarea name="system_prompt" data-team-hire-prompt placeholder="这个员工怎么干活：职责边界、输入、输出格式、约束">${escapeHtml(role.systemPrompt || "")}</textarea>
      </label>
      <div class="team-prompt-actions">
        <button type="button" class="text-button" data-team-optimize="1" data-tip="让模型把这个提示词改写成更明确可执行的版本（只产出候选，点保存才落盘）">优化提示词</button>
        <span class="team-editor-hint" data-team-optimize-state></span>
      </div>
      <div class="team-editor-slot" data-team-prompt-result hidden></div>
      <div class="team-editor-actions">
        <button type="submit" class="text-button primary team-hire-submit" data-team-hire-submit>${editing ? "保存修改" : "入职"}</button>
        <button type="button" class="text-button" data-team-editor-close="1">取消</button>
        <span class="team-editor-hint">${editing ? "同名角色就地覆盖（幂等：不会新建第二个角色会话）。" : "同名角色会被就地修改（幂等：不会新建第二个角色会话）。"}${pinnedRole ? "" : "提示词/权限登记在角色注册表里，装配后随会话保存。"}</span>
      </div>
    </form>
  </div>`;
}

// teamEditorPanel 是「新建 / 编辑团队」的冷加载面板：团队库条目的增改都从这里走。
// 成员用一行一个角色名表达（团队库定义"有谁、什么顺序"；每个员工怎么干活在员工栏
// 里逐个编辑），也支持"从当前会话 / 从内置模板"一键带入。
export function teamEditorPanel(team, entry, presets) {
  const editing = Boolean(entry && entry.teamID);
  const data = entry || {};
  const templates = (Array.isArray(presets) ? presets : [])
    .filter(preset => preset && typeof preset.team_kind === "string" && preset.team_kind)
    .map(preset => `<button type="button" class="text-button" data-team-template="${escapeHtml(preset.team_kind)}" data-tip="用这个内置形态填充成员与顺序策略">${escapeHtml(preset.team_kind)}</button>`)
    .join("");
  const memberNames = (data.roles || []).map(role => role.roleName);
  const currentMembers = team.members
    .filter(member => !PINNED_ROLES.has(member.roleName) && member.roleKind !== "timer")
    .map(member => member.roleName);
  return `<div class="team-editor" data-team-editor="team">
    <div class="team-editor-head">
      <span class="team-editor-title">${editing ? `编辑团队 · ${escapeHtml(data.name || data.teamID)}` : "新建团队"}</span>
      <button type="button" class="team-editor-close" data-team-editor-close="1" title="关闭面板（Esc）" aria-label="关闭团队面板">✕</button>
    </div>
    <form class="team-team-form" data-team-form autocomplete="off">
      <div class="team-field-row">
        <label class="team-field"><span>团队名</span>
          <input type="text" name="name" data-team-form-name placeholder="我的评审队" value="${escapeHtml(data.name || "")}" required>
        </label>
        <label class="team-field"><span>团队 ID</span>
          <input type="text" name="team_id" data-team-form-id placeholder="my-review-team" value="${escapeHtml(data.teamID || "")}"${editing ? " readonly" : ""}>
        </label>
      </div>
      <div class="team-field-row">
        <label class="team-field"><span>团队形态</span>
          <input type="text" name="team_kind" data-team-form-kind placeholder="留空 = 用团队 ID" value="${escapeHtml(data.teamKind || "")}">
        </label>
        <label class="team-field"><span>顺序策略</span>
          <select name="order_policy" data-team-form-policy>${options(POLICY_OPTIONS, data.orderPolicy || "user_main_decided")}</select>
        </label>
      </div>
      <label class="team-field"><span>成员（每行一个角色名，发言顺序按行序）</span>
        <textarea name="members" data-team-form-members placeholder="reviewer&#10;auditor">${escapeHtml(memberNames.join("\n"))}</textarea>
      </label>
      <div class="team-editor-actions">
        <span class="team-editor-hint">user / main 自动包含，不必填。</span>
        <button type="button" class="text-button" data-team-form-fill-current="1" data-tip="用当前会话在编员工填充成员">从当前会话</button>
        ${currentMembers.length ? `<span class="team-editor-hint muted">当前会话：${escapeHtml(currentMembers.join("、"))}</span>` : ""}
      </div>
      <div class="team-template-picks">${templates}</div>
      <div class="team-editor-actions">
        <button type="submit" class="text-button primary" data-team-form-submit>${editing ? "保存团队" : "新建团队"}</button>
        <button type="button" class="text-button" data-team-editor-close="1">取消</button>
        <span class="team-editor-hint">团队库存项目级（跨会话复用）；装配 = 把这支团队写成当前会话的在编员工表 + 发言顺序。</span>
      </div>
    </form>
  </div>`;
}

function options(pairs, selected) {
  return pairs.map(([value, label]) =>
    `<option value="${escapeHtml(value)}"${String(value) === String(selected) ? " selected" : ""}>${escapeHtml(label)}</option>`).join("");
}

// teamSection 是「Team 栏」：装配参数 + 发言调度 + 定时 agent。
function teamSection(team) {
  return `${metaRow(team)}${scheduleBlock(team)}${team.scheduled.length ? scheduledBlock(team) : ""}`;
}

// teamRailHead 是各栏共用的栏头（栏名 + 计数 + 用途提示 + 可选动作）。
function teamRailHead(title, count, hint, action = "") {
  return `<div class="section-title sub-title team-rail-head">
    <span>${escapeHtml(title)}</span>
    <span class="badge">${Number(count) || 0}</span>
    ${action ? `<span class="team-rail-actions">${action}</span>` : ""}
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

// metaRow 是 Team 栏的团队参数表（形态 / 顺序策略 / 当前发言权）。
function metaRow(team) {
  const policyLabel = POLICY_LABEL[team.orderPolicy] || team.orderPolicy || "未设置顺序策略";
  const select = `<select class="team-policy-select" data-team-policy aria-label="顺序策略" title="${escapeHtml(policyLabel)}">${options(POLICY_OPTIONS, team.orderPolicy)}</select>`;
  const rows = [
    teamRow(['<span class="team-cell-key">团队形态</span>', `<span class="chip">${escapeHtml(team.teamKind || "team")}</span>`]),
    teamRow(['<span class="team-cell-key">顺序策略</span>', select]),
    teamRow(['<span class="team-cell-key">发言权</span>', `<span class="team-floor" title="当前发言权（floor 随 message head 发布）">floor ${escapeHtml(team.floorRole || "—")}</span>`])
  ];
  return `${teamRailHead("Team 栏", team.orderRoles.length, "装配与编排")}
    ${teamTable({ label: "Team 装配参数", head: ["项", "值"], rows: rows.join(""), columns: "72px minmax(0, 1fr)" })}`;
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

// agentTeamOrderForDrag 把一次拖拽换算成新的 order_roles（纯函数）：把
// sourceRole 插到 targetRole **之前**；targetRole 为空 = 插到末尾（拖到"顺序末尾"
// 落区）。sourceRole 还不在顺序里（未排入的员工）时也成立——这就是"把员工拖进
// 发言顺序"的动作。返回 null 表示无变化或非法目标。
export function agentTeamOrderForDrag(view, sourceRole, targetRole = "") {
  const team = normalizeAgentTeam(view);
  const source = String(sourceRole || "").trim();
  const target = String(targetRole || "").trim();
  if (!source) return null;
  const order = [...team.orderRoles];
  const from = order.indexOf(source);
  if (target && target === source) return null;
  if (target && !order.includes(target)) return null;
  if (from >= 0) order.splice(from, 1);
  if (target) {
    const at = order.indexOf(target);
    if (at < 0) return null;
    order.splice(at, 0, source);
  } else {
    order.push(source);
  }
  if (order.join("\u0000") === team.orderRoles.join("\u0000")) return null;
  return { policy: team.orderPolicy, orderRoles: order };
}

// isPinnedRole 暴露 pin 规则给 app.js 的按钮禁用判断（与渲染同源）。
export function isPinnedRole(roleName) {
  return PINNED_ROLES.has(roleName);
}
