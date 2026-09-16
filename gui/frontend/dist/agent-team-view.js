import { escapeHtml, icon } from "./components.js";

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

// ── 员工权限的「逐格装配」（路由组 × 位）──────────────────────────────
//
// 两层口径，别混：
//   - TOOLS_POLICY_OPTIONS 是**档位预设**（与后端 dto.ToolPolicy* 一一对应，落盘为
//     tools_policy）：一键的粗粒度选择；
//   - PERMISSION_GROUPS × PERMISSION_BITS 是**逐格装配**（落盘为 permission_groups）：
//     用户自己给某个员工勾出"能读、能写项目、碰不到桌面"这种组合。
//
// 组名与位值都是**后端写入侧校验的枚举**（dto.PermissionGroup* / dto.PermissionBit*），
// 前端只做镜像，不能自造取值——拼错的组名在后端会被显式拒绝，而不是静默变成"没装配"。
export const PERMISSION_GROUPS = [
  { name: "ro", label: "读", hint: "读文件 / 搜索（不改任何共享状态）" },
  { name: "rw", label: "写项目", hint: "写文件 / 编辑 / 执行命令（限定在项目根内）" },
  { name: "rw_session", label: "工作台", hint: "改本会话的可变工作区（如压缩上下文）" },
  { name: "rw_desktop", label: "桌面", hint: "共享外设：鼠标 / 键盘注入（所有并行角色共用一块屏幕）" },
  { name: "ctl", label: "叫停", hint: "结束 / 挂起 / 派生 / 装载执行结构" },
  { name: "adm", label: "属主", hint: "改变能力面本身（换插件 / 加载技能）" }
];

export const PERMISSION_BITS = [
  { bit: 4, label: "r", hint: "读" },
  { bit: 2, label: "w", hint: "写" },
  { bit: 1, label: "x", hint: "执行" }
];

// PERMISSION_CUSTOM_TOOLS 是权限下拉里的 **UI 哨兵**（不是落盘的 tools_policy 取值）：
// 选中它 = 提交 permission_groups 而不是 tools_policy。
export const PERMISSION_CUSTOM_TOOLS = "custom";

// 档位 → 格子的默认形状：选档位时把格子摆成该档位（用户可继续精调，也可以直接
// 用档位而不装配格子）。口径与后端 EmployeeGroupsForPolicy 一致。
const POLICY_GROUP_PRESET = {
  readonly: { ro: 4, rw: 0, rw_session: 0, rw_desktop: 0, ctl: 0, adm: 0 },
  readwrite: { ro: 4, rw: 6, rw_session: 0, rw_desktop: 0, ctl: 0, adm: 0 }
};

// normalizePermissionGroups 归一后端下发的 permission_groups：只保留已知组名与
// 0..7 的整数位；畸形/空载荷返回 null（= 没装配，不伪造成"装配了空格子"）。
export function normalizePermissionGroups(groups) {
  if (!groups || typeof groups !== "object" || Array.isArray(groups)) return null;
  const known = new Set(PERMISSION_GROUPS.map(group => group.name));
  const normalized = {};
  let count = 0;
  for (const [name, bits] of Object.entries(groups)) {
    if (!known.has(name)) continue;
    normalized[name] = typeof bits === "number" && Number.isInteger(bits) && bits >= 0 && bits <= 7 ? bits : 0;
    count += 1;
  }
  return count ? normalized : null;
}

// permissionGroupsLabel 把格子映射成短标签（员工行/编辑回填的 chip）。
export function permissionGroupsLabel(groups) {
  const normalized = normalizePermissionGroups(groups);
  if (!normalized) return "继承";
  const has = (name, bit) => (Number(normalized[name] || 0) & bit) === bit;
  const parts = [];
  if (has("ro", 4)) parts.push("读");
  if (has("rw", 2)) parts.push("写项目");
  if (has("rw_session", 2)) parts.push("工作台");
  if (has("rw_desktop", 2)) parts.push("桌面");
  if (has("ctl", 1)) parts.push("叫停");
  if (has("adm", 7)) parts.push("属主");
  return parts.length ? parts.join("+") : "无权限";
}

// memberPermLabel 员工行的权限标签：**装配了格子就以格子为准**（档位只是"没精调"
// 时的预设）；两者都没有 = 继承宿主默认。
export function memberPermLabel(role) {
  if (normalizePermissionGroups(role?.permissionGroups)) return permissionGroupsLabel(role.permissionGroups);
  return toolsPolicyLabel(role?.toolsPolicy);
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

// employeePool 汇编面板里的「可用员工」：**全局员工库 ∪ 本会话在编员工**（读侧合并，
// 母本优先）。两份事实各自的来源留在每行的 chip 上：库里的行可以改 / 删母本，只在
// 会话里的行只能「入库」。合并口径解决的是"我现在能用的人是谁、有几个"——只要会话
// 里有员工（比如装配 goal-a2a 后的 tl），员工库就不再是 0 人空壳。
export function employeePool(global, team) {
  const master = Array.isArray(global?.employees) ? global.employees : [];
  const composition = Array.isArray(global?.composition?.employees) ? global.composition.employees : [];
  const members = (Array.isArray(team?.members) ? team.members : [])
    .filter(member => member?.roleName && !PINNED_ROLES.has(member.roleName) && member.roleKind !== "timer");
  const pool = new Map();
  const push = (role, source) => {
    if (!role || !role.roleName) return;
    const entry = pool.get(role.roleName) || {
      roleName: role.roleName, roleKind: "", toolsPolicy: "", permissionGroups: null, systemPrompt: "",
      modelPolicy: "", joinPolicy: "", presencePolicy: "", inLibrary: false, inSession: false
    };
    if (!entry.roleKind && role.roleKind) entry.roleKind = role.roleKind;
    if (!entry.toolsPolicy && role.toolsPolicy) entry.toolsPolicy = role.toolsPolicy;
    if (!entry.permissionGroups && normalizePermissionGroups(role.permissionGroups)) {
      entry.permissionGroups = normalizePermissionGroups(role.permissionGroups);
    }
    if (!entry.systemPrompt && role.systemPrompt) entry.systemPrompt = role.systemPrompt;
    if (!entry.modelPolicy && role.modelPolicy) entry.modelPolicy = role.modelPolicy;
    if (!entry.joinPolicy && role.joinPolicy) entry.joinPolicy = role.joinPolicy;
    if (!entry.presencePolicy && role.presencePolicy) entry.presencePolicy = role.presencePolicy;
    entry[source] = true;
    pool.set(entry.roleName, entry);
  };
  for (const role of master) push(role, "inLibrary");
  for (const role of composition) push(role, "inSession");
  for (const role of members) push(role, "inSession");
  return [...pool.values()];
}

// employeeLibraryBlock 是「员工库」块：可用员工一份清单，行首 ≡ 直接拖到员工栏或
// 团队成员表 = 把人排进发言顺序。逐行只留必要动作——库里的行可改 / 可删，只在本会话
// 里的行给一个「入库」。
function employeeLibraryBlock(global, team) {
  const available = Boolean(global && typeof global === "object");
  const pool = employeePool(global, team);
  const drift = available ? teamGlobalDrift(global) : false;
  const rows = pool.map(role => {
    const actions = [];
    if (available && role.inLibrary) {
      actions.push(`<button type="button" class="image-button" data-team-employee-edit="${escapeHtml(role.roleName)}" aria-label="修改 ${escapeHtml(role.roleName)}" data-tip="修改员工库里的这个人（提示词 / 权限 / 类型）">${icon("settings", 12)}</button>`);
      actions.push(`<button type="button" class="image-button" data-team-employee-delete="${escapeHtml(role.roleName)}" aria-label="从员工库删除 ${escapeHtml(role.roleName)}" data-tip="从员工库删除（已装配的会话副本不受影响）">${icon("close", 12)}</button>`);
    } else if (available) {
      actions.push(`<button type="button" class="text-button" data-team-employee-save="${escapeHtml(role.roleName)}" data-tip="把这一位写进员工库（全局事实，不装配到任何会话）">入库</button>`);
    }
    return teamRow([
      `<span class="team-staff-main">
        ${available ? `<span class="team-drag-handle" data-team-employee-drag="${escapeHtml(role.roleName)}" draggable="true" role="button" tabindex="0" title="拖到员工栏或团队成员表 = 排进发言顺序" aria-label="拖拽 ${escapeHtml(role.roleName)} 到发言顺序">${icon("grip", 12)}</span>` : ""}
        <span class="team-library-name" title="${escapeHtml(role.roleName)}">${escapeHtml(roleDisplayName(role.roleName, role.roleKind))}</span>
        <span class="team-member-role" title="逻辑角色名（metadata，不是 provider role）">${escapeHtml(role.roleName)}</span>
        <span class="chip">${escapeHtml(ROLE_KIND_LABEL[role.roleKind] || role.roleKind || "agent")}</span>
        <span class="team-perm-chip${role.toolsPolicy || role.permissionGroups ? "" : " is-inherit"}" title="工具权限（登记在角色注册表）">${escapeHtml(memberPermLabel(role))}</span>
      </span>`,
      `<span class="team-source-chip${role.inLibrary ? "" : " is-session"}" title="${role.inLibrary ? "员工库（全局母本）" : "只在本会话在编名单里"}">${role.inLibrary ? "库" : "本会话"}</span>`,
      `<span class="team-library-actions">${actions.join("")}</span>`
    ], { className: "team-library-row team-employee-row", attrs: `data-team-employee="${escapeHtml(role.roleName)}"` });
  });
  const emptyNote = available
    ? "还没有员工：点「新建员工」建一个，或在员工栏「入职」后回来入库"
    : "员工库为空（宿主未下发）";
  // 写按钮只在宿主真的下发了员工库时出现：旧后端不会接受这些调用，不伪造能点的按钮。
  const headActions = available
    ? `<button type="button" class="text-button" data-team-employee-new="1" data-tip="新建一个员工（不装配到任何会话）">${icon("plus", 12)}新建员工</button>`
    : '<span class="team-editor-hint muted">旧宿主：员工库不可写</span>';
  return `${teamRailHead("员工库", pool.length, headActions)}
    ${teamTable({
      label: "员工库",
      head: ["员工（拖拽调序）", "来源", "操作"],
      rows: rows.join("") || teamEmptyRow(emptyNote),
      columns: "minmax(0, 1.6fr) 46px minmax(0, 1fr)"
    })}
    ${drift ? '<div class="team-drift">本会话在编名单与员工库有差异：会话里的行点「入库」写回母本</div>' : ""}`;
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
      permissionGroups: normalizePermissionGroups(item.permission_groups),
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
      permissionGroups: normalizePermissionGroups(item.permission_groups),
      systemPrompt: typeof item.system_prompt === "string" ? item.system_prompt : "",
      modelPolicy: typeof item.model_policy === "string" ? item.model_policy : "",
      presencePolicy: typeof item.presence_policy === "string" ? item.presence_policy : ""
    }));
}

// renderAgentTeam 渲染面板主体。presets 来自 Bridge.AgentTeamPresets()、
// library 来自 Bridge.AgentTeamLibrary()、global 来自 Bridge.AgentTeamGlobalConfig()
// （全局母本：团队库 + 员工库 + 默认顺序 + 会话副本投影）；缺省时只渲染当前会话状态
// （不伪造按钮）。
//
// 面板分四块，各自条目化：
//   「员工库」= 可用员工（全局库 ∪ 本会话在编）+ 入库/新建/编辑/删除，行首可拖进顺序；
//   「团队库」= 用户自己的团队（点团队名开团队面板）+ 内置形态 chip + 新建团队；
//   「员工栏」= 本会话在编员工 + 发言顺序（拖拽 ≡ 直接调序）+ 冷加载入职/修改面板；
//   「发言调度」= 运行态：轮次 / 下一个 / 席位 / 收束（顺序串珠条，不是表格）。
export function renderAgentTeam(view, presets, library, global) {
  const team = normalizeAgentTeam(view);
  const presetList = Array.isArray(presets) ? presets.filter(item => item && typeof item.team_kind === "string" && item.team_kind) : [];
  const blocks = [];

  if (team.designNotice.length) {
    blocks.push(`<div class="team-notice" role="status">${team.designNotice.map(escapeHtml).join("；")}</div>`);
  }

  const globalConfig = global && typeof global === "object" ? normalizeTeamGlobal(global) : null;
  // 员工库在前、且与团队无关：不选团队也能看到离散员工并逐个增删改（解耦）。
  blocks.push(employeeLibraryBlock(globalConfig, team));
  blocks.push(teamLibraryBlock(team, presetList, normalizeTeamLibrary(library)));
  if (!team.configured) {
    blocks.push('<div class="team-empty muted">当前会话未装配 AgentTeam：装配一支团队，或直接给这个会话增加员工。</div>');
  }
  blocks.push(staffSection(team));

  if (team.configured) {
    blocks.push(teamSection(team));
  }
  return `<div class="team-panel">${blocks.join("")}</div>`;
}

// teamLibraryBlock 是「团队库」：一行一支用户自己的团队（点团队名打开团队面板）。
// 内置形态（goal-a2a / review-team…）是代码里的模板，不是用户数据，所以只留一行
// 小 chip 用来"就地装配"，不再往库里塞假条目。团队只负责三件事——装配谁、按什么
// 顺序回答、呼叫谁；员工本身的增删改在「员工库」块里。
function teamLibraryBlock(team, presets, library) {
  const rows = library.teams.map(entry => {
    const members = entry.roles.length;
    const active = entry.teamKind === team.teamKind;
    return teamRow([
      `<span class="team-staff-main">
        <button type="button" class="text-button team-library-name" data-team-edit-team="${escapeHtml(entry.teamID)}" data-tip="打开团队面板：成员与发言顺序 / 从员工库加人 / 保存" aria-label="打开团队 ${escapeHtml(entry.teamID)}">${escapeHtml(entry.name || entry.teamID)}</button>
        <span class="chip">${escapeHtml(entry.teamKind || "team")}</span>
      </span>`,
      `<span class="team-library-meta" title="角色数 / 顺序策略">${members} 人 · ${escapeHtml(POLICY_LABEL[entry.orderPolicy] || entry.orderPolicy || "—")}</span>`,
      `<span class="team-library-actions">
        <button type="button" class="text-button" data-team-materialize-team="${escapeHtml(entry.teamID)}"${active ? ' title="重复装配是幂等的，不会新建第二个角色会话"' : ' data-tip="把这支团队装配到当前会话"'}>${active ? "已装配" : "装配"}</button>
        <button type="button" class="image-button" data-team-delete-team="${escapeHtml(entry.teamID)}" aria-label="从团队库删除 ${escapeHtml(entry.teamID)}" data-tip="从团队库删除">${icon("close", 12)}</button>
      </span>`
    ], { className: "team-library-row", attrs: `data-team-library-entry="${escapeHtml(entry.teamID)}"` });
  });
  const presetChips = presets.map(preset => {
    const kind = preset.team_kind;
    const active = kind === team.teamKind;
    return `<button type="button" class="team-preset-chip${active ? " is-active" : ""}" data-team-materialize="${escapeHtml(kind)}"${active ? ' disabled title="当前会话就是这个形态"' : ` data-tip="按内置形态 ${escapeHtml(kind)} 装配一支团队"`}>${escapeHtml(kind)}</button>`;
  }).join("");
  return `${teamRailHead("团队库", library.teams.length,
    `<button type="button" class="text-button" data-team-open-team="1" data-tip="新建一支团队（空白 / 从当前会话 / 从内置模板）">${icon("plus", 12)}新建团队</button>`)}
    ${teamTable({
      label: "团队库",
      head: ["团队", "规模", "操作"],
      rows: rows.join("") || teamEmptyRow("还没有自己的团队：点「新建团队」，或直接用下面的内置形态装配"),
      columns: "minmax(0, 1.2fr) minmax(0, 1.2fr) minmax(0, 1fr)"
    })}
    ${presetChips ? `<div class="team-preset-row"><span class="team-preset-label muted">内置</span>${presetChips}</div>` : ""}
    <div class="team-editor-slot" data-team-team-slot hidden></div>`;
}

// staffSection 是「员工栏」：本会话在编员工 + 发言顺序（同一张表：顺序就是发言次序）。
// 顺序**只用拖拽**调整（有拖拽就不需要 ↑/↓ 按钮）；点「+ 入职」加人，点「编辑」改
// 提示词与权限；员工库里的行也能拖进来（未在编的先入职，再落到拖放位置）。
// 窄栏口径：类型 chip 并进身份格，表只留"身份 / 位置 / 操作"三列——列一多，每列
// 只剩二十几像素（"user" 会被折成 "use r" 就是这个原因）。
function staffSection(team) {
  const rows = staffRows(team);
  return `${teamRailHead("员工栏", team.members.length,
    `<button type="button" class="text-button" data-team-open-hire="1" data-tip="入职一个新员工（角色名 / 提示词 / 权限）">${icon("plus", 12)}入职</button>`)}
    ${teamTable({
      label: "员工栏",
      head: ["员工（拖拽调序 · 权限）", "位置", "操作"],
      rows: rows.join("") || teamEmptyRow("暂无员工：点「+ 入职」增加一个角色"),
      columns: "minmax(0, 1.6fr) 34px minmax(0, 1.3fr)"
    })}
    <div class="team-drop-end" data-team-order-drop="end" title="把员工拖到这里 = 排到发言顺序末尾">拖到这里 → 排到发言顺序末尾</div>
    <div class="team-editor-slot" data-team-hire-slot hidden></div>`;
}

// staffRows 渲染员工行：已排入顺序的按顺序在前，未排入的跟在后面（可拖进顺序）。
function staffRows(team) {
  const ordered = team.orderRoles.map(name => team.members.find(member => member.roleName === name) || {
    roleName: name, roleKind: "", roleSessionID: "", orderIndex: -1, inOrder: true,
    joinPolicy: "", toolsPolicy: "", permissionGroups: null, systemPrompt: "", modelPolicy: "", presencePolicy: ""
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
  const perm = memberPermLabel(member);
  const promptChip = member.systemPrompt
    ? `<span class="team-perm-chip" title="已登记提示词（${escapeHtml(String(member.systemPrompt.length))} 字符）">提示词</span>`
    : `<span class="team-perm-chip is-inherit" title="未登记提示词">无提示词</span>`;
  const actions = [
    pinned
      ? `<button type="button" class="text-button" data-team-action="remove" data-team-role="${escapeHtml(member.roleName)}" disabled title="user/main 是群聊起手与收口，不能摘除">${icon("close", 12)}</button>`
      : `<button type="button" class="text-button" data-team-action="remove" data-team-role="${escapeHtml(member.roleName)}"${inOrder ? "" : " disabled"} aria-label="摘除 ${escapeHtml(display)}" data-tip="从工作顺序摘除（保留角色）">${icon("close", 12)}</button>`,
    `<button type="button" class="text-button team-member-edit" data-team-edit="${escapeHtml(member.roleName)}"${pinned ? ' disabled title="user/main 由会话本身提供，配置不可改"' : ' data-tip="打开编辑面板（提示词 / 权限 / 类型）"'}>编辑</button>`,
    `<button type="button" class="text-button team-member-remove" data-team-delete="${escapeHtml(member.roleName)}"${pinned ? ' disabled title="user/main 由会话本身提供，不能删除"' : ' data-tip="从注册表与工作顺序中删除该角色"'}>删除</button>`
  ].join("");
  const handle = scheduled
    ? '<span class="team-drag-handle is-static" title="定时 agent 不参与发言顺序，不能排序">·</span>'
    : `<span class="team-drag-handle" data-team-drag="${escapeHtml(member.roleName)}" draggable="true" role="button" tabindex="0" title="拖拽调整发言顺序" aria-label="拖拽调整 ${escapeHtml(display)} 的发言顺序">${icon("grip", 12)}</span>`;
  return teamRow([
    `<span class="team-staff-main">
      ${handle}
      <button type="button" class="text-button team-member-name" data-team-role-open="${escapeHtml(member.roleName)}" data-team-role-session="${escapeHtml(member.roleSessionID)}" data-tip="查看 ${escapeHtml(display)} 的独立会话\nrole=${escapeHtml(member.roleName)} · ${escapeHtml(session)}" aria-label="查看 ${escapeHtml(display)} 的独立会话">${escapeHtml(display)}</button>
      <span class="team-member-role" title="逻辑角色名（metadata，不是 provider role）">${escapeHtml(member.roleName)}</span>
      <span class="chip">${escapeHtml(kind)}</span>
      <span class="team-perm-chip${perm === "继承" ? " is-inherit" : ""}" title="工具权限（登记在角色注册表）">${escapeHtml(perm)}</span>
      ${promptChip}
      ${onFloor ? '<span class="chip team-floor-chip" title="当前发言权在这一位">发言中</span>' : ""}
    </span>`,
    `<span class="team-member-pos" title="${inOrder ? "工作顺序位置" : "不在工作顺序里"}">${escapeHtml(position)}</span>`,
    `<span class="team-member-actions">${actions}</span>`
  ], {
    className: `team-staff-row${onFloor ? " is-floor" : ""}${scheduled ? " team-outside-row" : ""}`,
    attrs: `data-team-staff-role="${escapeHtml(member.roleName)}" data-team-order-role="${escapeHtml(member.roleName)}" data-team-in-order="${inOrder ? "1" : "0"}" draggable="true"`
  });
}

// hirePanel 是「入职 / 修改员工」的冷加载面板：默认不渲染，点 + 入职 / 编辑才注入。
// 字段**条目化 + 序列化**：一条字段一行（序号 + 标签 + 控件），分组分节，右栏窄也
// 读得成一条流水线，而不是并排的挤字表格。
// 口径：**面板里不摆小字备注**——字段说明一律走控件的 hover 提示（title），下拉里的
// 选项标签自己就把话说完了（"只读（不写文件 / 不执行命令）"），不再在下面复述一遍。
// scope 决定落盘位置：session = 当前会话在编员工（入职/覆盖），library = 员工库
// （全局事实，不装配到任何会话）——员工库的增删改与团队解耦，走的就是后者。
export function hirePanel(team, member, scope = "session") {
  const editing = Boolean(member && member.roleName);
  const role = member || {};
  const toLibrary = scope === "library";
  const kindOptions = options(ROLE_KIND_OPTIONS, role.roleKind || "agent");
  const joinOptions = options(JOIN_POLICY_OPTIONS, role.joinPolicy || "on_team_create");
  // 权限下拉 = 登记值（与 dto.ToolPolicy* 一致的档位）+ 一个 **UI 哨兵**（"逐格装配"，
  // 不落盘为 tools_policy）。哨兵不放进 TOOLS_POLICY_OPTIONS：那是一份与后端枚举
  // 一一对应的词表，混进 UI 专有取值会让"登记的取值集合"变成两回事。
  const customPermission = normalizePermissionGroups(role.permissionGroups) !== null;
  const toolsOptions = options(
    TOOLS_POLICY_OPTIONS.concat([[PERMISSION_CUSTOM_TOOLS, "逐格装配（在下面勾选）"]]),
    customPermission ? PERMISSION_CUSTOM_TOOLS : (role.toolsPolicy || "")
  );
  const modelOptions = options(MODEL_POLICY_OPTIONS, role.modelPolicy || "");
  const title = toLibrary
    ? (editing ? `修改员工 · ${escapeHtml(roleDisplayName(role.roleName, role.roleKind))}` : "新建员工 · 员工库")
    : (editing ? `修改员工 · ${escapeHtml(roleDisplayName(role.roleName, role.roleKind))}` : "入职员工");
  const fields = [];
  fields.push(fieldItem(1, "角色名",
    `<input type="text" name="role_name" data-team-hire-name placeholder="reviewer / auditor…" value="${escapeHtml(role.roleName || "")}" title="小写英文 / 下划线；同名即就地覆盖（幂等，不会新建第二个角色会话）"${editing ? " readonly" : ""} required>`));
  fields.push(fieldItem(2, "类型", `<select name="role_kind" data-team-hire-kind title="agent 干活 / techlead 评审收口 / 定时 不参与发言顺序">${kindOptions}</select>`));
  fields.push(fieldItem(3, "入职时机", `<select name="join_policy" data-team-hire-join title="什么时候把这个员工拉进会话（定时触发的不进发言顺序）">${joinOptions}</select>`));
  fields.push(fieldItem(4, "在席策略",
    `<input type="text" name="presence_policy" data-team-hire-presence placeholder="留空继承（online_when_goal_active…）" value="${escapeHtml(role.presencePolicy || "")}" title="留空 = 继承团队 / 会话默认">`));
  fields.push(fieldItem(5, "权限", `<select name="tools_policy" data-team-hire-tools title="档位预设：readonly 只读 / readwrite 读写 / full 全权；留空继承宿主默认。要精调就选「逐格装配」">${toolsOptions}</select>`));
  fields.push(fieldBlock(6, "权限位", permissionGrid(role, customPermission)));
  fields.push(fieldItem(7, "模型", `<select name="model_policy" data-team-hire-model title="这一位用哪个模型档位（供应商与模型在「账号」页配）">${modelOptions}</select>`));
  fields.push(fieldItem(8, "员工提示词",
    `<textarea name="system_prompt" data-team-hire-prompt placeholder="这个员工怎么干活：职责边界、输入、输出格式、约束" title="装配时会作为该角色会话的系统提示词">${escapeHtml(role.systemPrompt || "")}</textarea>`));
  return `<div class="team-editor" data-team-editor="hire">
    <div class="team-editor-head">
      <span class="team-editor-title">${title}</span>
      ${toLibrary ? '<span class="chip" title="这份面板只写员工库，不装配到当前会话">员工库</span>' : ""}
      <button type="button" class="team-editor-close" data-team-editor-close="1" title="关闭面板（Esc）" aria-label="关闭面板">${icon("close", 12)}</button>
    </div>
    <form class="team-hire-form" data-team-hire-form data-team-hire-scope="${toLibrary ? "library" : "session"}" autocomplete="off">
      ${fieldGroup("身份", fields.slice(0, 2))}
      ${fieldGroup("编排", fields.slice(2, 4))}
      ${fieldGroup("能力", fields.slice(4, 7))}
      ${fieldGroup("提示词", fields.slice(7))}
      <div class="team-prompt-actions">
        <button type="button" class="text-button" data-team-optimize="1" data-tip="让模型把这个提示词改写成更明确可执行的版本（只产出候选，点保存才落盘）">优化提示词</button>
        <span class="team-prompt-state" data-team-optimize-state></span>
      </div>
      <div class="team-editor-slot" data-team-prompt-result hidden></div>
      <div class="team-editor-actions">
        <button type="submit" class="text-button primary team-hire-submit" data-team-hire-submit title="${toLibrary
          ? (editing ? "改的是员工库里的这一位（全局事实）；已装配的会话读的是自己的副本，不会被动改。" : "只写员工库（全局事实），不装配到当前会话；要进会话再点「入职」或装配团队。")
          : "同名角色就地覆盖（幂等：不会新建第二个角色会话）；提示词 / 权限登记在角色注册表里，装配后随会话保存。"}">${toLibrary ? (editing ? "保存到员工库" : "存入员工库") : (editing ? "保存修改" : "入职")}</button>
        <button type="button" class="text-button" data-team-editor-close="1">取消</button>
      </div>
    </form>
  </div>`;
}

// fieldItem 渲染一条"序列化字段"：序号 + 标签 + 控件 + 说明，纵向一条一行。
// 右栏只有 220~480px，标签与控件并排会把每列挤到 80px（旧版挤字就是这个原因）。
function fieldItem(index, label, control, hint = "") {
  return `<label class="team-field">
      <span class="team-field-label"><span class="team-field-no">${index}</span>${escapeHtml(label)}</span>
      ${control}
      ${hint ? `<span class="team-field-hint">${escapeHtml(hint)}</span>` : ""}
    </label>`;
}

// fieldBlock 与 fieldItem 同形，但用 div 而不是 label：控件里**自带 label**（例如
// 逐格权限的每个勾选框）时不能套外层 label——HTML 不允许 label 嵌套，点一个勾选框
// 会连带切换另一个（真是"点一下就装错权限"）。
function fieldBlock(index, label, control, hint = "") {
  return `<div class="team-field">
      <span class="team-field-label"><span class="team-field-no">${index}</span>${escapeHtml(label)}</span>
      ${control}
      ${hint ? `<span class="team-field-hint">${escapeHtml(hint)}</span>` : ""}
    </div>`;
}

// permissionGrid 渲染"逐格装配"的权限面板（路由组 × r/w/x）。默认隐藏：只有权限
// 下拉选到「逐格装配」时才显示（否则用户会以为没选档位就以格子为准）。初始勾选
// 取"已装配的格子"，没有则取当前档位的预设形状（切到逐格时是一个合理的起点）。
function permissionGrid(role, visible) {
  const current = normalizePermissionGroups(role?.permissionGroups) || POLICY_GROUP_PRESET[role?.toolsPolicy] || {};
  const rows = PERMISSION_GROUPS.map(group => {
    const bits = Number(current[group.name] || 0);
    const boxes = PERMISSION_BITS.map(item => `<label class="team-perm-bit" title="${escapeHtml(`${group.label} · ${item.hint}`)}"><input type="checkbox" data-team-hire-perm="${escapeHtml(group.name)}" data-team-hire-perm-bit="${item.bit}"${(bits & item.bit) === item.bit ? " checked" : ""}><span>${escapeHtml(item.label)}</span></label>`).join("");
    return `<div class="team-perm-grid-row"><span class="team-perm-grid-name" title="${escapeHtml(group.hint)}">${escapeHtml(group.label)}</span><span class="team-perm-grid-bits">${boxes}</span></div>`;
  }).join("");
  return `<div class="team-perm-grid" data-team-hire-perm-grid${visible ? "" : " hidden"} title="逐格装配（路由组 × 位）：勾了就按这格子判，未勾的族 = 不开；选档位预设时这几格不生效">${rows}</div>`;
}

// fieldGroup 把若干条目字段收进一节（节头 + 条目），让长面板有可扫读的分段。
function fieldGroup(title, items) {
  return `<section class="team-field-group">
      <div class="team-field-group-head">${escapeHtml(title)}</div>
      ${items.join("")}
    </section>`;
}

// teamMemberNames 取团队条目的成员顺序（去掉 user/main：它们由会话本身提供，
// 装配时自动补齐）。
export function teamMemberNames(entry) {
  const roles = Array.isArray(entry?.roles) ? entry.roles.map(role => role?.roleName) : [];
  const order = Array.isArray(entry?.orderRoles) ? entry.orderRoles : [];
  const names = [];
  for (const name of [...order, ...roles]) {
    if (typeof name !== "string" || !name) continue;
    if (PINNED_ROLES.has(name) || names.includes(name)) continue;
    names.push(name);
  }
  return names;
}

// renderTeamMemberList 渲染团队面板里的成员表：行序就是发言顺序。行上带 ✕（移除）
// 与整行拖拽（调序），也可以承接从「员工库」拖过来的员工。
export function renderTeamMemberList(names, pool = []) {
  const list = Array.isArray(names) ? names : [];
  if (!list.length) {
    return '<div class="team-member-empty muted" data-team-member-empty="1">还没有成员：从下面挑一个，或把「员工库」里的人拖进来</div>';
  }
  const kindOf = new Map((Array.isArray(pool) ? pool : []).map(role => [role.roleName, role.roleKind]));
  return list.map((name, index) => `<div class="team-member-item" data-team-member-item="${escapeHtml(name)}" data-team-member-drag="${escapeHtml(name)}" draggable="true" title="拖拽调整顺序（拖到某位成员上 = 插到它之前）">
      <span class="team-member-idx">${index + 1}</span>
      <span class="team-member-label">${escapeHtml(roleDisplayName(name, kindOf.get(name) || ""))}</span>
      <span class="team-member-role">${escapeHtml(name)}</span>
      <button type="button" class="team-member-drop" data-team-member-remove="${escapeHtml(name)}" aria-label="移除 ${escapeHtml(name)}" title="从这个团队里移除">${icon("close", 12)}</button>
    </div>`).join("");
}

// teamEditorPanel 是「新建 / 编辑团队」的冷加载面板：团队库条目的增改都从这里走。
// 成员是一张可拖拽的表（行序 = 发言顺序），可以 ✕ 掉、从员工库拖进来、或从下拉挑；
// 也可以「从当前会话填充」「用内置模板起手」。面板自己带 ✕ 关闭与保存，所以"现在在
// 设置哪支团队"由面板标题写明。
export function teamEditorPanel(team, entry, presets, pool = []) {
  const editing = Boolean(entry && entry.teamID);
  const data = entry || {};
  const employees = Array.isArray(pool) ? pool : [];
  const memberNames = teamMemberNames(data);
  const templates = (Array.isArray(presets) ? presets : [])
    .filter(preset => preset && typeof preset.team_kind === "string" && preset.team_kind)
    .map(preset => `<button type="button" class="text-button" data-team-template="${escapeHtml(preset.team_kind)}" data-tip="用这个内置形态填充成员与顺序策略">${escapeHtml(preset.team_kind)}</button>`)
    .join("");
  const candidates = employees.filter(role => !memberNames.includes(role.roleName));
  const pickOptions = candidates
    .map(role => `<option value="${escapeHtml(role.roleName)}">${escapeHtml(roleDisplayName(role.roleName, role.roleKind))} · ${escapeHtml(role.roleName)}</option>`)
    .join("");
  const currentMembers = team.members
    .filter(member => !PINNED_ROLES.has(member.roleName) && member.roleKind !== "timer")
    .map(member => member.roleName);
  return `<div class="team-editor" data-team-editor="team">
    <div class="team-editor-head">
      <span class="team-editor-title">${editing ? `团队 · ${escapeHtml(data.name || data.teamID)}` : "新建团队"}</span>
      <button type="button" class="team-editor-close" data-team-editor-close="1" title="关闭面板（Esc）" aria-label="关闭团队面板">${icon("close", 12)}</button>
    </div>
    <form class="team-team-form" data-team-form autocomplete="off">
      ${fieldGroup("身份", [
        fieldItem(1, "团队名", `<input type="text" name="name" data-team-form-name placeholder="我的评审队" value="${escapeHtml(data.name || "")}" required>`, "会话里显示的团队名。"),
        fieldItem(2, "团队 ID", `<input type="text" name="team_id" data-team-form-id placeholder="my-review-team" value="${escapeHtml(data.teamID || "")}"${editing ? " readonly" : ""}>`, "团队库主键；编辑时不可改（要改就新建一支）。")
      ])}
      ${fieldGroup("形态与顺序", [
        fieldItem(3, "团队形态", `<input type="text" name="team_kind" data-team-form-kind placeholder="留空 = 用团队 ID" value="${escapeHtml(data.teamKind || "")}">`, "装配后写进会话的 team_kind。"),
        fieldItem(4, "顺序策略", `<select name="order_policy" data-team-form-policy>${options(POLICY_OPTIONS, data.orderPolicy || "user_main_decided")}</select>`, "谁决定发言顺序（user_main_decided = 用户/主管点将）。")
      ])}
      ${fieldGroup("成员与发言顺序", [
        fieldItem(5, "成员（行序即发言顺序）", `<div class="team-member-list" data-team-member-list>${renderTeamMemberList(memberNames, employees)}</div>`, "user / main 自动包含。"),
        fieldItem(6, "添加成员", `<span class="team-member-add"><select data-team-member-pick aria-label="从员工库选择员工">${pickOptions}</select><button type="button" class="text-button" data-team-member-add="1"${candidates.length ? "" : " disabled"}>添加</button></span>`, "候选 = 员工库 ∪ 本会话在编（不含 timer）。")
      ])}
      <div class="team-editor-actions">
        <button type="button" class="text-button" data-team-form-fill-current="1" data-tip="用当前会话在编员工填充成员">从当前会话填充</button>
        ${currentMembers.length ? `<span class="team-editor-hint muted">当前会话：${escapeHtml(currentMembers.join("、"))}</span>` : ""}
      </div>
      <div class="team-template-picks">${templates}</div>
      <div class="team-editor-actions">
        <button type="submit" class="text-button primary" data-team-form-submit>${editing ? "保存团队" : "新建团队"}</button>
        <button type="button" class="text-button" data-team-editor-close="1">取消</button>
        <span class="team-editor-hint">团队库是跨会话复用的模板；装配 = 把它写成当前会话的在编员工表 + 发言顺序。</span>
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

// teamRailHead 是各栏共用的栏头（栏名 + 计数 + 可选动作）。
function teamRailHead(title, count, action = "") {
  return `<div class="section-title sub-title team-rail-head">
    <span>${escapeHtml(title)}</span>
    <span class="badge">${Number(count) || 0}</span>
    ${action ? `<span class="team-rail-actions">${action}</span>` : ""}
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

// scheduleBlock 显示**运行时的发言调度**：顺序串珠条（位置即次序）+ 轮次 / 席位 /
// 收束。参照群聊的通用做法——顺序是一条可视的链，"下一个"与"发言中"用行尾标签与
// 高亮表达，不摆 项/值 表（窄栏里表头比内容还宽）。没有 schedule（旧宿主/未接线）
// 时整块隐藏，不拿静态顺序冒充运行态。
function scheduleBlock(team) {
  const schedule = team.schedule;
  if (!schedule) return "";
  const limit = schedule.roundLimit > 0 ? String(schedule.roundLimit) : "∞";
  const unexecuted = new Set(schedule.unexecuted);
  const stopped = schedule.stopped;
  const pills = schedule.order.map((roleName, index) => {
    const onFloor = roleName === team.floorRole;
    const isNext = !stopped && roleName === schedule.nextRole;
    const idle = unexecuted.has(roleName);
    const tip = idle
      ? `第 ${index + 1} 位 · 环内没有执行者：占位但不会自动产生回合`
      : `第 ${index + 1} 位${onFloor ? " · 当前发言权" : ""}${isNext ? " · 下一个发言" : ""}`;
    const cls = ["schedule-pill", onFloor ? "is-floor" : "", isNext ? "is-next" : "", idle ? "is-idle" : ""].filter(Boolean).join(" ");
    return `<span class="${cls}" role="listitem" title="${escapeHtml(tip)}">
      <span class="schedule-idx">${index + 1}</span>
      <span class="schedule-name">${escapeHtml(roleDisplayName(roleName))}</span>
      ${onFloor ? '<span class="schedule-tag">发言中</span>' : ""}
      ${isNext ? '<span class="schedule-tag is-next">下一个</span>' : ""}
    </span>`;
  }).join("");
  const stopChip = stopped
    ? `<span class="chip team-stop">已收束 · ${escapeHtml(STOP_REASON_LABEL[schedule.stopReason] || schedule.stopReason || "停止")}</span>`
    : "";
  return `<div class="team-block team-schedule">
    <div class="section-title sub-title"><span>发言调度</span><span class="badge">${schedule.round}/${escapeHtml(limit)}</span></div>
    <div class="schedule-strip" role="list" aria-label="发言顺序">${pills || '<span class="muted">顺序里还没有员工</span>'}</div>
    <div class="schedule-meta">
      ${stopChip}
      <span class="schedule-seat" title="user 在群聊里的席位口径">user 席位 · ${escapeHtml(USER_SEAT_LABEL[schedule.userSeat] || schedule.userSeat || "—")}</span>
      ${schedule.unexecuted.length ? `<span class="schedule-note" title="环内没有执行者的角色：占位但不会自动产生回合">无执行者 ${escapeHtml(schedule.unexecuted.join("、"))}</span>` : ""}
    </div>
  </div>`;
}

// metaRow 是 Team 栏的装配参数（形态 / 顺序策略 / 当前发言权）：一行 chip 表达，
// 不再摆 项/值 表。
function metaRow(team) {
  const policyLabel = POLICY_LABEL[team.orderPolicy] || team.orderPolicy || "未设置顺序策略";
  const select = `<select class="team-policy-select" data-team-policy aria-label="顺序策略" title="${escapeHtml(policyLabel)}">${options(POLICY_OPTIONS, team.orderPolicy)}</select>`;
  const floor = team.floorRole
    ? `<span class="team-floor" title="当前发言权（floor 随 message head 发布）">发言中 ${escapeHtml(team.floorRole)}</span>`
    : '<span class="team-floor is-empty" title="还没有角色拿到发言权">暂无发言权</span>';
  return `<div class="team-meta-row">
    <span class="chip" title="会话的 team_kind">${escapeHtml(team.teamKind || "team")}</span>
    ${select}
    ${floor}
  </div>`;
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

// nextAgentTeamOrder 把一次"摘除 / 恢复"换算成新的 order_roles（纯函数，便于测试）。
// 顺序**位置**的调整只有一条路：拖拽（agentTeamOrderForDrag）——按钮式 ↑/↓ 已按用户
// 口径移除，所以这个函数只回答"在不在发言顺序里"。返回 null = 动作非法。
export function nextAgentTeamOrder(view, action, roleName) {
  const team = normalizeAgentTeam(view);
  const order = [...team.orderRoles];
  const index = order.indexOf(roleName);
  if (action === "restore") {
    if (index >= 0) return null;
    order.push(roleName);
    return { policy: team.orderPolicy, orderRoles: order };
  }
  if (action === "remove") {
    if (index < 0) return null;
    if (PINNED_ROLES.has(roleName)) return null;
    order.splice(index, 1);
    return { policy: team.orderPolicy, orderRoles: order };
  }
  return null;
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
