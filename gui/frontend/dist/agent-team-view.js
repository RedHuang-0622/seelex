import { escapeHtml, icon } from "./components.js";

// ── Agent Team 面板（右侧栏 · 状态 → Agent Team）──────────────
//
// 数据源：Application API（AgentTeamView / AgentTeamLibrary / AgentTeamGlobalConfig /
// AgentTeamMaterializeTeam / AgentTeamSaveTeam / AgentTeamSaveCurrentTeam /
// AgentTeamDeleteTeam / AgentTeamPutRole / AgentTeamDeleteRole / AgentTeamSetOrder /
// AgentTeamInstantiateRole / AgentTeamSaveEmployee / AgentTeamDeleteEmployee /
// AgentTeamSetDefaultOrder / AgentTeamPublishToGlobal / AgentTeamOptimizePrompt）。
// 渲染只读，动作由 app.js 事件委托转成一次 Bridge 调用。
//
// 事实源边界：
//   - **没有"团队形态"这个设定**（2026-10-01）：内置形态目录已随 Go 侧 presets.go 删除，
//     形态不再是可选项；`team_kind` 只是团队名的展示别名（缺省 = 团队 ID），面板既不给
//     输入框也不再显示形态 chip——一支团队有谁、什么顺序，由团队库条目 / 本会话在编决定；
//   - **顺序不是人编排的**：`order_roles` 唯一事实是会话 lifecycle，它的次序就是员工
//     **登记（入职 / 装配）的先后**；面板不给拖拽调序、不给位置列——leader-worker 目标态里
//     顺序归 leader 的 team plan（team plan 的 stages[].depends_on），人在面板上要做的只是
//     "谁在编"（入职 / 摘除 / 删除）；`order_policy` 是历史字段（只回读、不驱动轮次），
//     面板连只读展示都不再给（要核对时看落盘文件或 TUI）；
//   - 员工配置的唯一事实是本会话的角色注册表（session/team/roles.json）；
//   - 团队库 / 员工库 / 默认顺序是**全局**母本（数据根下 `team/`）：会话读它的
//     深拷贝副本，会话内的入职只改副本，只有「确认·普及搭配到全局」才回写。
//
// 交互口径（用户要求）：
//   - 员工入职/修改、团队新建/编辑都是**冷加载面板**：默认不渲染表单，点 + 或
//     团队名才弹出，面板带关闭键；
//   - 团队用一张表管理；「新建团队」只提供空白与「从当前会话填充」两种起手（没有内置模板）。

const ROLE_KIND_LABEL = {
  user: "user",
  main: "main",
  techlead: "TL",
  agent: "agent",
  timer: "定时"
};

// roleKindLabel 是 RoleKind 的短标签（成员行 / 详情用）。未知取值**原样显示**，
// 不折成 agent：后端加新生态位时，前端把它显示成 agent 就是把事实说错。
export function roleKindLabel(kind) {
  const value = String(kind || "").trim();
  if (!value) return "";
  return ROLE_KIND_LABEL[value] || value;
}

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

// employeeFieldRows 把一个员工档案摊成 **k→v 全字段**（E3「表格化」）：员工行此前
// 只在行内回显几个 chip，全字段藏在懒加载的编辑面板里——用户口径是"表格化"，
// 所以每个员工行都能展开一张 k→v 表，一栏一行，空栏也照列（"这一栏为空"本身是
// 一条事实，值渲染成"继承/未登记"，不藏起来）。
export function employeeFieldRows(role) {
  const source = role && typeof role === "object" ? role : {};
  const groups = normalizePermissionGroups(source.permissionGroups);
  return [
    { key: "role_kind", label: "类型", value: ROLE_KIND_LABEL[source.roleKind] || source.roleKind || "agent" },
    { key: "join_policy", label: "入职", value: JOIN_POLICY_LABEL[source.joinPolicy] || source.joinPolicy || "入职即入顺序" },
    { key: "presence_policy", label: "在席", value: source.presencePolicy || "继承" },
    { key: "tools_policy", label: "权限档", value: source.toolsPolicy ? toolsPolicyLabel(source.toolsPolicy) : "继承" },
    { key: "permission_groups", label: "权限格", value: groups ? permissionGroupsLabel(groups) : "未装配" },
    { key: "model_policy", label: "模型", value: source.modelPolicy || "继承" },
    { key: "system_prompt", label: "提示词", value: source.systemPrompt ? `${String(source.systemPrompt).length} 字符` : "未登记" }
  ];
}

// employeeFieldTable 把 employeeFieldRows 渲染成一张 k→v 表（可压缩进员工行）。
function employeeFieldTable(role) {
  const items = employeeFieldRows(role).map(row =>
    `<div class="team-kv-row" data-team-kv="${escapeHtml(row.key)}"><dt>${escapeHtml(row.label)}</dt><dd title="${escapeHtml(row.value)}">${escapeHtml(row.value)}</dd></div>`
  ).join("");
  return `<details class="team-kv" data-team-employee-kv="${escapeHtml(role?.roleName || "")}">
      <summary title="展开这个员工的全部字段（k→v）：表里一行一栏，空栏也照列">字段</summary>
      <dl class="team-kv-list">${items}</dl>
    </details>`;
}

const ROLE_KIND_OPTIONS = [
  ["agent", "agent"],
  ["techlead", "techlead"],
  ["timer", "定时"]
];

// JOIN_POLICY_OPTIONS 是「入职时机」的取值集合，标签**照 factory.placeRoleInOrder 的
// 实际行为写**（不写"应该怎样"）：
//   - on_team_create（缺省）= 入职就把人追加到工作顺序末尾；
//   - timer / scheduled = 不进工作顺序（定时分区）；
//   - 其余（on_goal_create / on_demand…）= **不自动入顺序**，由装配方显式排（原先标成
//     "goal 上线时入顺序"是过时说法：goal 上线早已不再自动装配团队）。
const JOIN_POLICY_OPTIONS = [
  ["on_team_create", "入职即入顺序"],
  ["on_goal_create", "不自动入顺序（由装配方排）"],
  ["scheduled", "定时触发（不入顺序）"],
  ["on_demand", "按需（不自动入顺序）"]
];

// JOIN_POLICY_LABEL 把登记的 join_policy 折成短词（员工行 k→v 用）。
const JOIN_POLICY_LABEL = Object.fromEntries(JOIN_POLICY_OPTIONS);

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
        // 门禁 / 压缩是团队形态级策略，条目里存着（dto.TeamLibraryEntry）。归一化漏掉
        // 它们 = 面板看不见、保存时清空：编辑一次就把这支团队的门禁口径抹了。
        gatePolicy: typeof entry.gate_policy === "string" ? entry.gate_policy : "",
        compactPolicy: typeof entry.compact_policy === "string" ? entry.compact_policy : "",
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

// employeeLibraryBlock 是「员工库」块：**全局**那份事实——员工是谁 + 档案（提示词/权限/类型）。
// pool 取「全局库 ∪ 本会话在编」（读侧合并，母本优先）：只要会话里有员工（比如装配过一支
// 团队后的 tl），员工库就不再是 0 人空壳；每行的「来源」chip 说明它落在哪份事实里。
// 行内动作只留必要项——库里的行可「入职」（装进本会话）/可改/可删，只在本会话里的行给一个
// 「入库」（写回母本）。**没有拖拽**：面板不提供任何编排手势（次序 = 登记先后）。
function employeeLibraryBlock(global, team) {
  const available = Boolean(global && typeof global === "object");
  const pool = employeePool(global, team);
  const drift = available ? teamGlobalDrift(global) : false;
  const rows = pool.map(role => {
    const actions = [];
    if (available && role.inLibrary) {
      if (!role.inSession) {
        // 只给"还没在本会话在编"的行：入职 = 把员工库这一份装进当前会话（幂等覆盖），
        // 取代原先"从员工库拖到员工栏"的拖拽手势（面板不再提供任何拖拽编排）。
        actions.push(`<button type="button" class="text-button" data-team-employee-hire="${escapeHtml(role.roleName)}" data-tip="把这一位装进当前会话（入职；同名就地覆盖副本）">入职</button>`);
      }
      actions.push(`<button type="button" class="image-button" data-team-employee-edit="${escapeHtml(role.roleName)}" aria-label="修改 ${escapeHtml(role.roleName)}" data-tip="修改员工库里的这个人（提示词 / 权限 / 类型）">${icon("settings", 12)}</button>`);
      actions.push(`<button type="button" class="image-button" data-team-employee-delete="${escapeHtml(role.roleName)}" aria-label="从员工库删除 ${escapeHtml(role.roleName)}" data-tip="从员工库删除（已装配的会话副本不受影响）">${icon("close", 12)}</button>`);
    } else if (available) {
      actions.push(`<button type="button" class="text-button" data-team-employee-save="${escapeHtml(role.roleName)}" data-tip="把这一位写进员工库（全局事实，不装配到任何会话）">入库</button>`);
    }
    return teamRow([
      `<span class="team-staff-main">
        <span class="team-library-name" title="${escapeHtml(role.roleName)}">${escapeHtml(roleDisplayName(role.roleName, role.roleKind))}</span>
        <span class="team-member-role" title="逻辑角色名（metadata，不是 provider role）">${escapeHtml(role.roleName)}</span>
        <span class="chip">${escapeHtml(ROLE_KIND_LABEL[role.roleKind] || role.roleKind || "agent")}</span>
        <span class="team-perm-chip${role.toolsPolicy || role.permissionGroups ? "" : " is-inherit"}" title="工具权限（员工库档案；库里有就以库里的为准）">${escapeHtml(memberPermLabel(role))}</span>
        ${employeeFieldTable(role)}
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
  return `${teamRailHead("员工库", pool.length, headActions,
    "员工库（全局事实）：员工是谁、档案（提示词 / 权限 / 类型）都在这里改——员工档案的唯一编辑入口，与团队解耦")}
    ${teamTable({
      label: "员工库",
      head: ["员工", "来源", "操作"],
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
      presencePolicy: typeof item.presence_policy === "string" ? item.presence_policy : "",
      // 生态位的两个字段（dto.RoleSpec 里有，权限档位派生与治理指令集读它们）：
      // 归一化不认识它们 = 团队面板看不见、保存时丢。
      directiveSchema: Array.isArray(item.directive_schema) ? item.directive_schema.filter(step => typeof step === "string" && step) : [],
      orderPriority: Number.isInteger(item.order_priority) ? item.order_priority : 0
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
    unexecuted: Array.isArray(value.unexecuted) ? value.unexecuted.filter(name => typeof name === "string" && name) : []
  };
}

const STOP_REASON_LABEL = {
  round_limit: "到达轮次上限",
  no_progress: "连续无进展",
  no_executor: "顺序里没有执行者",
  empty_ring: "发言顺序为空",
  external_break: "外部停止（裁决/中断）"
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

// renderAgentTeam 渲染面板主体。library 来自 AgentTeamLibrary()、global 来自
// AgentTeamGlobalConfig()（全局母本：团队库 + 员工库 + 默认顺序 + 会话副本投影）；
// 缺省时只渲染当前会话状态（不伪造按钮）。
//
// 面板分四块，各自条目化——**一块只管一份事实**（用户口径：语意弄清楚、别功能耦合）：
//   「员工库」= 全局：员工是谁 + 档案（提示词/权限/类型）的唯一编辑入口（入库/新建/修改/删除），
//     行内可「入职」把这一位装进本会话；
//   「团队库」= 全局：用户自己的团队（点团队名开团队面板）+ 新建团队；
//   「员工栏」= 本会话：在编员工 + 入职 + 摘除/移出本会话（次序 = 登记先后，不由人编排）；
//     档案只回显、不在这里改（改档案回「员工库」，再入职一次即覆盖副本）；
//   「发言调度」= 运行态：轮次 / 下一个 / 收束（顺序串珠条，不是表格；串珠条里不含
//   user——用户经回合尾的消息队列提升发言，不占排班位）。
export function renderAgentTeam(view, library, global) {
  const team = normalizeAgentTeam(view);
  const blocks = [];

  if (team.designNotice.length) {
    blocks.push(`<div class="team-notice" role="status">${team.designNotice.map(escapeHtml).join("；")}</div>`);
  }

  const globalConfig = global && typeof global === "object" ? normalizeTeamGlobal(global) : null;
  // 员工库在前、且与团队无关：不选团队也能看到离散员工并逐个增删改（解耦）。
  blocks.push(employeeLibraryBlock(globalConfig, team));
  blocks.push(teamLibraryBlock(team, normalizeTeamLibrary(library)));
  if (!team.configured) {
    blocks.push('<div class="team-empty muted">当前会话未装配 AgentTeam：装配一支团队，或直接给这个会话增加员工。</div>');
  }
  blocks.push(staffSection(team));

  if (team.configured) {
    blocks.push(teamSection(team));
  }
  // 手动刷新键（E3）：Agent Team 没有心跳（用户口径），热更新靠事件驱动
  // （team.changed / 母本 CRUD 事件），事件之外再给一枚常驻的手动刷新——"我现在就要
  // 最新读数"不用切会话、也不用把面板收起再展开。
  const toolbar = `<div class="team-panel-toolbar">
      <button type="button" class="image-button" data-team-refresh="1" title="立即重取员工库 / 团队库 / 本会话员工（手动刷新键；Agent Team 无心跳，平时靠事件热更新）" aria-label="刷新 Agent Team 面板">${icon("refresh", 12)}</button>
    </div>`;
  return `<div class="team-panel">${toolbar}${blocks.join("")}</div>`;
}

// teamLibraryBlock 是「团队库」：一行一支用户自己的团队（点团队名打开团队面板）。
// **没有内置形态**（2026-10-01）：形态目录已删，所以这里不再有"内置 chip 行"——
// 库里那一行就是团队的全部事实（有谁、装配后谁在编）。团队只负责两件事——装配谁、
// 呼叫谁；员工本身的增删改在「员工库」块里，次序由登记先后决定（不由人编排）。
function teamLibraryBlock(team, library) {
  const rows = library.teams.map(entry => {
    const members = entry.roles.length;
    const active = entry.teamKind === team.teamKind;
    // 规模列只说"几个人"，成员名进 title：**顺序不是这份链表对人的承诺**（面板不提供
    // 调序，次序 = 登记先后），所以这里既不说"固定循环"，也不把旧顺序当事实展示。
    const memberNames = entry.roles.map(role => role.roleName).filter(Boolean).join("、") || "—";
    return teamRow([
      `<span class="team-staff-main">
        <button type="button" class="text-button team-library-name" data-team-edit-team="${escapeHtml(entry.teamID)}" data-tip="打开团队面板：成员 / 从员工库加人 / 保存" aria-label="打开团队 ${escapeHtml(entry.teamID)}">${escapeHtml(entry.name || entry.teamID)}</button>
      </span>`,
      `<span class="team-library-meta" title="${escapeHtml(`成员：${memberNames}`)}">${members} 人</span>`,
      `<span class="team-library-actions">
        <button type="button" class="text-button" data-team-materialize-team="${escapeHtml(entry.teamID)}"${active ? ' title="重复装配是幂等的，不会新建第二个角色会话"' : ' data-tip="把这支团队装配到当前会话"'}>${active ? "已装配" : "装配"}</button>
        <button type="button" class="image-button" data-team-delete-team="${escapeHtml(entry.teamID)}" aria-label="从团队库删除 ${escapeHtml(entry.teamID)}" data-tip="从团队库删除">${icon("close", 12)}</button>
      </span>`
    ], { className: "team-library-row", attrs: `data-team-library-entry="${escapeHtml(entry.teamID)}"` });
  });
  const presetChips = "";
  return `${teamRailHead("团队库", library.teams.length,
    `<button type="button" class="text-button" data-team-open-team="1" data-tip="新建一支团队（空白 / 从当前会话填充）">${icon("plus", 12)}新建团队</button>`,
    "团队库（全局事实）：一行一支团队——有谁、装配后谁在编；员工本身的增删改在「员工库」")}
    ${teamTable({
      label: "团队库",
      head: ["团队", "规模", "操作"],
      rows: rows.join("") || teamEmptyRow("还没有自己的团队：点「新建团队」建一支（可「从当前会话填充」）"),
      columns: "minmax(0, 1.2fr) minmax(0, 1.2fr) minmax(0, 1fr)"
    })}
    <div class="team-editor-slot" data-team-team-slot hidden></div>`;
}
// staffSection 是「员工栏」：本会话在编员工（谁在编 = 这份表唯一要说的事）。点「+ 入职」
// 把人装进本会话；点 ✕ 摘除（出工作顺序、留角色）、点「删除」连会话注册表一起删。
// 档案（提示词/权限/类型）**只回显**：唯一编辑入口是「员工库」（否则同一个人的档案有
// 两套按钮、写两份事实）。
//
// **不提供人工编排**（2026-10-01）：拖拽调序与"位置"列已删——次序就是登记（入职/装配）
// 的先后；leader-worker 目标态里顺序归 leader 的 team plan（stages[].depends_on）。面板要
// 做的只是"谁在编"。
//
// 窄栏口径：类型 chip 并进身份格，表只留"身份 / 操作"两列——列一多，每列只剩二十几像素。
function staffSection(team) {
  const rows = staffRows(team);
  return `${teamRailHead("员工栏", team.members.length,
    `<button type="button" class="text-button" data-team-open-hire="1" data-tip="入职一个新员工（角色名 / 提示词 / 权限）">${icon("plus", 12)}入职</button>`,
    "员工栏（本会话）：谁在编（摘除 / 删除 / 查看）；次序 = 登记先后，不由人编排。档案（提示词 / 权限 / 类型）在「员工库」改，改完再「入职」一次即覆盖本会话副本")}
    ${teamTable({
      label: "员工栏",
      head: ["员工（权限）", "操作"],
      rows: rows.join("") || teamEmptyRow("暂无员工：点「+ 入职」增加一个角色"),
      columns: "minmax(0, 1.6fr) minmax(0, 1.3fr)"
    })}
    <div class="team-editor-slot" data-team-hire-slot hidden></div>`;
}

// staffRows 渲染员工行：先按会话当前顺序（= 登记先后）出在编的，再补"注册了但不在顺序里"
// 的（定时 agent 单列一档）。
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
  const perm = memberPermLabel(member);
  const promptChip = member.systemPrompt
    ? `<span class="team-perm-chip" title="本会话在编副本已登记提示词（${escapeHtml(String(member.systemPrompt.length))} 字符）">提示词</span>`
    : `<span class="team-perm-chip is-inherit" title="本会话在编副本未登记提示词（继承档案）">无提示词</span>`;
  // 行内动作只留**本会话**的两件：摘除（出工作顺序、留角色）/ 删除（连会话注册表一起删）。
  // 「编辑」在这一版下线（用户口径：员工栏不该和员工库功能耦合）——档案（提示词 / 权限 /
  // 类型）的唯一编辑入口是「员工库」：本会话那份是入职/装配时取下的副本，要改档案回员工库改，
  // 再「入职」一次即覆盖（同 role_name 幂等覆盖）。于是"同一个人的档案有两套按钮、写两份事实"
  // 只剩一处；员工栏只回显本副本的现状。
  const actions = [
    `<button type="button" class="text-button" data-team-action="remove" data-team-role="${escapeHtml(member.roleName)}"${pinned || !inOrder ? " disabled" : ""}${pinned ? ' title="user/main 是群聊起手与收口，不能摘除"' : ` aria-label="摘除 ${escapeHtml(display)}" data-tip="从工作顺序摘除（保留角色）"`}>${icon("close", 12)}</button>`,
    `<button type="button" class="text-button team-member-remove" data-team-delete="${escapeHtml(member.roleName)}"${pinned ? ' disabled title="user/main 由会话本身提供，不能删除"' : ' data-tip="从注册表与工作顺序中删除该角色"'}>删除</button>`
  ].join("");
  return teamRow([
    `<span class="team-staff-main">
      <button type="button" class="text-button team-member-name" data-team-role-open="${escapeHtml(member.roleName)}" data-team-role-session="${escapeHtml(member.roleSessionID)}" data-tip="查看 ${escapeHtml(display)} 的独立会话\nrole=${escapeHtml(member.roleName)} · ${escapeHtml(session)}" aria-label="查看 ${escapeHtml(display)} 的独立会话">${escapeHtml(display)}</button>
      <span class="team-member-role" title="逻辑角色名（metadata，不是 provider role）">${escapeHtml(member.roleName)}</span>
      <span class="chip">${escapeHtml(kind)}</span>
      <span class="team-perm-chip${perm === "继承" ? " is-inherit" : ""}" title="工具权限（本会话在编副本）">${escapeHtml(perm)}</span>
      ${promptChip}
      ${onFloor ? '<span class="chip team-floor-chip" title="当前发言权在这一位">发言中</span>' : ""}
      ${scheduled ? '<span class="team-perm-chip is-inherit" title="定时 agent 不参与发言顺序">定时</span>' : ""}
      ${!inOrder && !scheduled ? '<span class="team-perm-chip is-inherit" title="注册了但当前不在工作顺序里">不在顺序</span>' : ""}
    </span>`,
    `<span class="team-member-actions">${actions}</span>`
  ], {
    className: `team-staff-row${onFloor ? " is-floor" : ""}${scheduled ? " team-outside-row" : ""}`,
    attrs: `data-team-staff-role="${escapeHtml(member.roleName)}" data-team-order-role="${escapeHtml(member.roleName)}"`
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

// ── 团队成员带哪一份 RoleSpec（"生态位 vs 人"）────────────────────────
//
// 后端团队库条目存的是**整套 RoleSpec**（dto.TeamLibraryEntry.Roles），不只角色名。
// 团队面板此前只搬 role_name/role_kind/tools_policy/system_prompt，于是"保存团队"
// 这一步就把其余字段静默丢掉：一支 goal-a2a 存回库里时，tl 的 techlead 规格
// （role_kind + join_policy/presence_policy/directive_schema）被静默降级——生态位
// 这份事实就此丢失。
//
// 两条口径，别混：
//   - **生态位**（role_kind / join_policy / presence_policy / directive_schema /
//     order_priority）由**团队库条目**定义：这一支团队存过的规格说了算，条目没登记
//     才回落到员工库那一份（形态目录删掉后没有了"哪个内置形态规定生态位"这一层）；
//   - **人**（系统提示词 / 权限档 / 逐格权限 / 模型档）由**员工库 / 本会话在编**定义，
//     没登记的才回落到条目里已存的那一份。
// 表单里没暴露的字段（门禁 / 压缩策略）随条目带入、原样保存。
export const TEAM_ROLE_SPEC_FIELDS = [
  "role_kind",
  "join_policy",
  "presence_policy",
  "directive_schema",
  "order_priority",
  "tools_policy",
  "permission_groups",
  "model_policy",
  "system_prompt"
];

// 生态位字段（条目优先的那一组）：其余字段算"人的档案"。
const TEAM_NICHE_FIELDS = ["role_kind", "join_policy", "presence_policy", "directive_schema", "order_priority"];

// 字段两种拼法都认，输出一律是**协议拼法**（snake_case）：Bridge 下发的协议载荷是
// snake_case，前端归一化后的库条目 / 员工池 / 在编是 camelCase，团队面板同时消费
// 这两种来源——只认一种就是把另一半来源的字段当"没登记"丢掉。
const ROLE_SPEC_ALIASES = {
  role_kind: "roleKind",
  join_policy: "joinPolicy",
  presence_policy: "presencePolicy",
  directive_schema: "directiveSchema",
  order_priority: "orderPriority",
  tools_policy: "toolsPolicy",
  permission_groups: "permissionGroups",
  model_policy: "modelPolicy",
  system_prompt: "systemPrompt"
};

// teamRoleNameOf 取角色名（两种拼法都认）：协议载荷用 role_name，归一化对象用 roleName。
export function teamRoleNameOf(role) {
  const value = role?.roleName ?? role?.role_name;
  return typeof value === "string" ? value.trim() : "";
}

// teamRoleSpec 从任意来源（库条目角色 / 员工库那一份 RoleSpec）摘出
// 团队库条目能存的字段：空值不落盘（空 = 未登记，由后端默认值兜底，不伪造成"登记了空"）。
export function teamRoleSpec(source) {
  const out = {};
  if (!source || typeof source !== "object") return out;
  for (const field of TEAM_ROLE_SPEC_FIELDS) {
    const value = source[field] ?? source[ROLE_SPEC_ALIASES[field]];
    if (value === undefined || value === null || value === "") continue;
    // 数值 0 = 未登记（order_priority 在 Go 侧是 omitempty 的 int，0 就是"没设"）：
    // 不把它写成事实，免得"没设"和"设成 0"两件事在条目里分不开。
    if (value === 0) continue;
    if (Array.isArray(value)) {
      const items = value.filter(item => item !== undefined && item !== null && item !== "");
      if (!items.length) continue;
      out[field] = items;
      continue;
    }
    out[field] = value;
  }
  return out;
}

// teamMemberSpecMap 算出成员表里每个成员该带的 RoleSpec：**条目**（这一支团队存过的
// 事实）+ **员工库/本会话在编**（人的档案：提示词 / 权限档 / 逐格权限 / 模型）。
//
// 形态目录删掉之后不再有 preset 这一层（2026-10-01）：成员的生态位（role_kind /
// join_policy / presence_policy / directive_schema / order_priority）与人的档案都从
// "条目优先、其次员工库"来——生态位不再由哪个内置形态规定。
export function teamMemberSpecMap(names, { entry = null, pool = [] } = {}) {
  const index = source => new Map(
    (Array.isArray(source) ? source : [])
      .map(item => [teamRoleNameOf(item), item])
      .filter(([name]) => name)
  );
  const entryRoles = index(entry?.roles);
  const poolRoles = index(pool);
  const map = {};
  for (const name of Array.isArray(names) ? names : []) {
    if (typeof name !== "string" || !name) continue;
    const storedRole = teamRoleSpec(entryRoles.get(name));
    const personRole = teamRoleSpec(poolRoles.get(name));
    // 人的档案：条目（这支团队存过的事实）当底，员工库/本会话在编覆盖。
    const spec = { ...storedRole, ...personRole };
    // 生态位：条目登记过就按条目；条目没有才回落到员工库那一份。
    for (const field of TEAM_NICHE_FIELDS) {
      const value = storedRole[field] ?? personRole[field];
      if (value === undefined) continue;
      spec[field] = value;
    }
    map[name] = spec;
  }
  return map;
}

// teamEntryFromMembers 把团队面板的草稿换算成团队库条目载荷。抽成纯函数是为了让它
// 可被单测：字段丢失是这个面板最容易悄悄回归的地方（丢一个 role_kind 就是丢一份生态位）。
export function teamEntryFromMembers({
  teamID = "",
  teamKind = "",
  name = "",
  orderPolicy = "",
  gatePolicy = "",
  compactPolicy = "",
  members = [],
  origin = "custom"
} = {}) {
  const list = (Array.isArray(members) ? members : [])
    .map(member => ({ roleName: String(member?.roleName || "").trim(), spec: teamRoleSpec(member?.spec) }))
    .filter(member => member.roleName);
  return {
    team_id: String(teamID || "").trim() || String(name || "").trim(),
    team_kind: String(teamKind || "").trim(),
    name: String(name || "").trim(),
    order_policy: String(orderPolicy || "").trim(),
    order_roles: ["user", "main", ...list.map(member => member.roleName)],
    roles: list.map(member => ({ role_name: member.roleName, ...member.spec })),
    gate_policy: String(gatePolicy || "").trim(),
    compact_policy: String(compactPolicy || "").trim(),
    origin
  };
}

// renderTeamMemberList 渲染团队面板里的成员表：行序 = 登记次序（保存时按这个次序写
// order_roles）。行上带 ✕（移除），也可以从下拉里挑员工加进来。
//
// **不做人工调序**（2026-10-01）：拖拽已删——这份表的次序只是"装配后谁先谁后"的登记
// 先后，leader-worker 目标态里真正的顺序归 leader 的 team plan。
//
// 每行还挂两件事实：**生态位**（role_kind 的短标签，可见）与**这份 RoleSpec 载荷**
// （data-team-member-spec，JSON）——草稿活在 DOM 里，保存时按行序读回来，所以规格
// 必须跟着行一起走，否则"删一个人"就把别人的规格丢掉。
export function renderTeamMemberList(names, pool = [], specs = {}) {
  const list = Array.isArray(names) ? names : [];
  if (!list.length) {
    return '<div class="team-member-empty muted" data-team-member-empty="1">还没有成员：从下面挑一个加进来（或点「从当前会话填充」）</div>';
  }
  const specOf = specs && typeof specs === "object" ? specs : {};
  const poolKind = new Map((Array.isArray(pool) ? pool : []).map(role => [teamRoleNameOf(role), role.roleKind || role.role_kind || ""]));
  return list.map(name => {
    const spec = teamRoleSpec(specOf[name]);
    const kind = String(spec.role_kind || poolKind.get(name) || "");
    const kindLabel = roleKindLabel(kind);
    const payload = JSON.stringify(spec);
    return `<div class="team-member-item" data-team-member-item="${escapeHtml(name)}" data-team-member-kind="${escapeHtml(kind)}" data-team-member-spec="${escapeHtml(payload)}">
      <span class="team-member-label">${escapeHtml(roleDisplayName(name, kind))}</span>
      ${kindLabel ? `<span class="chip team-member-kind" title="生态位（role_kind）：角色身份标签，并决定权限档位派生">${escapeHtml(kindLabel)}</span>` : ""}
      <span class="team-member-role">${escapeHtml(name)}</span>
      <button type="button" class="team-member-drop" data-team-member-remove="${escapeHtml(name)}" aria-label="移除 ${escapeHtml(name)}" title="从这个团队里移除">${icon("close", 12)}</button>
    </div>`;
  }).join("");
}

// teamEditorPanel 是「新建 / 编辑团队」的冷加载面板：团队库条目的增改都从这里走。
// 成员表可以从员工库挑人加入 / ✕ 移除，也可以「从当前会话填充」；面板自己带 ✕ 关闭
// 与保存，所以"现在在设置哪支团队"由面板标题写明。
//
// **没有"团队形态"这个字段了**（2026-10-01，用户口径）：团队形态（内置目录）已从
// 代码里删除，`team_kind` 只是团队名的展示别名——面板不再给输入框，只在隐藏字段里
// 原样带回条目已有的取值（改一次团队不该把没展示的字段洗掉）。同理，`order_policy`
// 与形态级 `gate_policy` / `compact_policy` 也只走隐藏字段：它们是随团队带来的既有
// 事实，面板不提供编辑入口，保存时原样保留。
//
// 成员表也不再宣称"行序即发言顺序"（面板不提供调序）：行序 = 登记先后，
// 保存时按它写 `order_roles`。
export function teamEditorPanel(team, entry, pool = []) {
  const editing = Boolean(entry && entry.teamID);
  const data = entry || {};
  const employees = Array.isArray(pool) ? pool : [];
  const memberNames = teamMemberNames(data);
  const memberSpecs = teamMemberSpecMap(memberNames, { entry: data, pool: employees });
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
      <input type="hidden" name="team_kind" data-team-form-kind value="${escapeHtml(data.teamKind || "")}">
      <input type="hidden" name="order_policy" data-team-form-policy value="${escapeHtml(data.orderPolicy || "")}">
      <input type="hidden" name="gate_policy" data-team-form-gate value="${escapeHtml(data.gatePolicy || "")}">
      <input type="hidden" name="compact_policy" data-team-form-compact value="${escapeHtml(data.compactPolicy || "")}">
      ${fieldGroup("身份", [
        fieldItem(1, "团队名", `<input type="text" name="name" data-team-form-name placeholder="我的评审队" value="${escapeHtml(data.name || "")}" required>`, "会话里显示、也是召唤时用的团队名。"),
        fieldItem(2, "团队 ID", `<input type="text" name="team_id" data-team-form-id placeholder="my-review-team" value="${escapeHtml(data.teamID || "")}"${editing ? " readonly" : ""}>`, "团队库主键；编辑时不可改（要改就新建一支）。")
      ])}
      ${fieldGroup("成员", [
        fieldItem(3, "成员", `<div class="team-member-list" data-team-member-list>${renderTeamMemberList(memberNames, employees, memberSpecs)}</div>`, "user / main 自动包含；每行的生态位（role_kind）是角色身份标签。"),
        fieldItem(4, "添加成员", `<span class="team-member-add"><select data-team-member-pick aria-label="从员工库选择员工">${pickOptions}</select><button type="button" class="text-button" data-team-member-add="1"${candidates.length ? "" : " disabled"}>添加</button></span>`, "候选 = 员工库 ∪ 本会话在编（不含 timer）。")
      ])}
      <div class="team-editor-actions">
        <button type="button" class="text-button" data-team-form-fill-current="1" data-tip="用当前会话在编员工填充成员">从当前会话填充</button>
        ${currentMembers.length ? `<span class="team-editor-hint muted">当前会话：${escapeHtml(currentMembers.join("、"))}</span>` : ""}
      </div>
      <div class="team-editor-actions">
        <button type="submit" class="text-button primary" data-team-form-submit>${editing ? "保存团队" : "新建团队"}</button>
        <button type="button" class="text-button" data-team-editor-close="1">取消</button>
        <span class="team-editor-hint">团队库是跨会话复用的模板；装配 = 把它写成当前会话的在编员工表。</span>
      </div>
    </form>
  </div>`;
}

function options(pairs, selected) {
  return pairs.map(([value, label]) =>
    `<option value="${escapeHtml(value)}"${String(value) === String(selected) ? " selected" : ""}>${escapeHtml(label)}</option>`).join("");
}

// teamSection 是「Team 栏」：当前发言权 + 发言调度 + 定时 agent（都是运行态读数）。
function teamSection(team) {
  return `${metaRow(team)}${scheduleBlock(team)}${team.scheduled.length ? scheduledBlock(team) : ""}`;
}

// teamRailHead 是各栏共用的栏头（栏名 + 计数 + 可选动作）。
// tip 是这块的**事实域**口径（"员工库写全局、员工栏写本会话"）——挂在栏头的 title 上：
// 上一轮已经把"栏头摆注解文字"这条口径钉掉了（见 agent-team-view.test.mjs 的
// "head carries no annotation text"），所以域只在 hover 与 aria 里说，不再占版面。
function teamRailHead(title, count, action = "", tip = "") {
  return `<div class="section-title sub-title team-rail-head"${tip ? ` title="${escapeHtml(tip)}"` : ""}>
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

// scheduleBlock 显示**运行时的发言调度**：顺序串珠条（位置即次序）+ 轮次 / 收束。
// 参照群聊的通用做法——顺序是一条可视的链，"下一个"与"发言中"用行尾标签与高亮
// 表达，不摆 项/值 表（窄栏里表头比内容还宽）。没有 schedule（旧宿主/未接线）时
// 整块隐藏，不拿静态顺序冒充运行态。
//
// 这是**运行态读数**，不是设定：面板不提供任何调序入口，这里的次序 = 登记（入职 /
// 装配）先后（leader-worker 目标态里 = leader 的 team plan 顺序）。
// 串珠条就是**发言顺序**（旧环序的运行态投影）：成员 = order_roles − user（user 的
// 发言机会是回合尾消息队列被整批提升为下一轮，不是排班位），所以这里不会出现 user
// 珠子，"下一个"也永远不会指向 user。
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
      ? `第 ${index + 1} 位 · 顺序里没有执行者：占位但不会自动产生回合`
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
      ${schedule.unexecuted.length ? `<span class="schedule-note" title="顺序里没有执行者的角色：占位但不会自动产生回合">无执行者 ${escapeHtml(schedule.unexecuted.join("、"))}</span>` : ""}
    </div>
  </div>`;
}

// metaRow 是 Team 栏的装配参数：这一版只剩**当前发言权**（运行态事实）。
//
// 「团队形态」chip 与「顺序策略」chip 都删了（2026-10-01，用户口径）：形态目录已从
// 代码里删除（`team_kind` 只是团队名的别名，不是设定），`order_policy` 是只回读、
// 不驱动任何行为的历史字段——把这两样摆在装配参数里，只会让人以为它们是可配置项。
function metaRow(team) {
  const floor = team.floorRole
    ? `<span class="team-floor" title="当前发言权（floor 随 message head 发布）">发言中 ${escapeHtml(team.floorRole)}</span>`
    : '<span class="team-floor is-empty" title="还没有角色拿到发言权">暂无发言权</span>';
  return `<div class="team-meta-row">${floor}</div>`;
}

// ── 角色会话详情（成员行「查看」）──────────────────────────────
//
// 数据源：Bridge.AgentTeamRoleSnapshot（application/contract/dto.RoleSnapshot）。
// 目的：EXEC（main）与 ADVISOR（tl）是两个独立会话；主对话只显示 EXEC 的
// 可见消息，ADVISOR 自己的行在这里按角色身份单独列出，避免"两个 agent 都叫
// AGENT"的歧义。纯渲染，不写任何状态。

// DRAFT_KIND_LABEL 把草稿行的 kind 翻成用户可读的类别（后端 goal_team_recorder
// 的三条生产者：role_context / tl_directive / round_host，逃生收口是 goal_archive）。
// 未登记的 kind 原样显示——不猜。
const DRAFT_KIND_LABEL = {
  role_context: "本轮上下文",
  tl_directive: "本轮裁决",
  round_host: "本轮主持",
  goal_archive: "收口归档"
};

function normalizeRoleRow(item) {
  const row = item && typeof item === "object" ? item : {};
  return {
    seq: Number.isInteger(row.seq) ? row.seq : 0,
    kind: typeof row.kind === "string" ? row.kind : "",
    role: typeof row.role === "string" ? row.role : "",
    roleName: typeof row.role_name === "string" ? row.role_name : "",
    roleSessionID: typeof row.role_session_id === "string" ? row.role_session_id : "",
    content: typeof row.content === "string" ? row.content : "",
    reasoning: typeof row.reasoning_content === "string" ? row.reasoning_content : "",
    toolCalls: Array.isArray(row.tool_calls) ? row.tool_calls.filter(call => call && typeof call === "object") : []
  };
}

function isRenderableRoleRow(row) {
  return Boolean(row.content || row.reasoning || row.toolCalls.length);
}

function normalizeRoleRows(items) {
  if (!Array.isArray(items)) return [];
  return items.map(normalizeRoleRow).filter(isRenderableRoleRow);
}

// draftIDOf 读草稿的轮次 / 单元号：外层（sequencer 的排序凭据）优先，内层 event
// 兜底——两边都可能是 0（"还没分配"），0 不代表任何一个回合。
function draftIDOf(outer, inner) {
  if (Number.isInteger(outer) && outer > 0) return outer;
  return Number.isInteger(inner) && inner > 0 ? inner : 0;
}

// normalizeRoleDrafts 归一化**未同步草稿**（dto.RoleDraftRow）：外层是 sequencer
// 的排序/幂等凭据，内层 event 才是将来要 append 进 message 的行。
//
// 为什么保留 round_id / unit_seq / kind：草稿还没有 message seq（seq 由 sequencer
// 在同步时分配），所以它们的身份是"第几轮的第几个单元 + 类别"；把它们当普通行按
// seq 处理会让同一批草稿全部落到 seq 0 上互相覆盖。
function normalizeRoleDrafts(items) {
  if (!Array.isArray(items)) return [];
  return items.map(item => {
    const row = item && typeof item === "object" ? item : {};
    const event = row.event && typeof row.event === "object" ? row.event : {};
    return {
      ...normalizeRoleRow(event),
      roundID: draftIDOf(row.round_id, event.round_id),
      unitSeq: draftIDOf(row.unit_seq, event.unit_seq)
    };
  }).filter(isRenderableRoleRow);
}

export function normalizeRoleSession(snapshot) {
  const source = snapshot && typeof snapshot === "object" ? snapshot : {};
  const floor = source.floor && typeof source.floor === "object" ? source.floor : {};
  return {
    mainSessionID: typeof source.main_session_id === "string" ? source.main_session_id : "",
    roleName: typeof source.role_name === "string" ? source.role_name : "",
    roleSessionID: typeof source.role_session_id === "string" ? source.role_session_id : "",
    root: typeof source.root === "string" ? source.root : "",
    joinSeqID: Number.isFinite(source.join_seq_id) ? source.join_seq_id : 0,
    prefixCutSeq: Number.isFinite(source.prefix_cut_seq) ? source.prefix_cut_seq : 0,
    orderPolicy: typeof source.order_policy === "string" ? source.order_policy : "",
    orderRoles: Array.isArray(source.order_roles) ? source.order_roles.filter(name => typeof name === "string" && name) : [],
    floorRole: typeof floor.role_name === "string" ? floor.role_name : "",
    mainRows: normalizeRoleRows(source.main_rows),
    visibleMainRows: Number.isInteger(source.visible_main_rows) ? source.visible_main_rows : 0,
    outsidePrefixMainRows: Number.isInteger(source.outside_prefix_main_rows) ? source.outside_prefix_main_rows : 0,
    roleRows: normalizeRoleRows(source.role_rows),
    draftRows: normalizeRoleDrafts(source.draft_rows),
    unassignedRoleRows: Number.isInteger(source.unassigned_role_rows) ? source.unassigned_role_rows : 0,
    designWarnings: Array.isArray(source.design_warnings) ? source.design_warnings.filter(item => typeof item === "string" && item) : []
  };
}

// renderRoleSessionSwitcher 渲染「切员工」切换条：对话视图（员工会话）顶部的一排
// 员工 chip，点谁就把视图目标换成谁的角色会话——不用关掉视图再回列表点下一位。
// 只在拿到在职员工名单时渲染（旧宿主/单员工时不摆一条只有一个 chip 的条）。
export function renderRoleSessionSwitcher(members, currentRoleName) {
  const list = (Array.isArray(members) ? members : [])
    .filter(member => member && typeof member.roleName === "string" && member.roleName);
  if (list.length <= 1) return "";
  const current = String(currentRoleName || "");
  const chips = list.map(member => {
    const active = member.roleName === current;
    const display = roleDisplayName(member.roleName, member.roleKind);
    return `<button type="button" class="role-session-switch-chip${active ? " is-active" : ""}" data-role-session-switch="${escapeHtml(member.roleName)}" data-role-session-switch-sid="${escapeHtml(member.roleSessionID || "")}"${active ? ' aria-current="true" title="正在看这一位"' : ` title="切到 ${escapeHtml(display)} 的会话"`}>${escapeHtml(display)}</button>`;
  }).join("");
  return `<div class="role-session-switcher" role="tablist" aria-label="切换员工会话">${chips}</div>`;
}

export function renderRoleSessionDetail(snapshot, identity = null) {
  const view = normalizeRoleSession(snapshot);
  const roleName = String(identity?.roleName || view.roleName || "");
  const roleSessionID = String(identity?.roleSessionID ?? view.roleSessionID ?? "");
  const display = roleDisplayName(view.roleName);
  // 手动刷新键（E1）：权威读数（角色会话投影）是**拉取**面，刷新键是"我现在就要最新"的
  // 兜底。但"此刻在做什么"不靠它——那条走 teammate.tool.started/completed 的**推送**
  // 路（见 renderRoleLiveTools 与 app.js 的 applyTeammateToolActivity）：员工一动手，
  // 这一节就逐帧长出来，不必等这一轮跑完（2026-10-03 口径修正）。
  const toolbar = `<div class="role-session-toolbar">
      <span class="role-session-toolbar-hint muted">运行详情 · 实时（teammate.tool.*）+ 手动刷新</span>
      <button type="button" class="image-button" data-role-session-refresh="1" data-role-session-role="${escapeHtml(roleName)}" data-role-session-sid="${escapeHtml(roleSessionID)}" title="重新拉取这个员工会话的读数（权威记录是拉取面）" aria-label="刷新员工运行详情">${icon("refresh", 12)}</button>
    </div>`;
  // 「切员工」切换条（用例 5 的对话视图）：把视图目标从一个员工会话换成另一个。
  const switcher = renderRoleSessionSwitcher(identity?.members, roleName);
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
  const record = renderRoleRecordTable(snapshot);
  // 「正在做（实时）」——这一节的数据**不进快照**：它来自 teammate.tool.started/completed
  // 事件（见 app.js 的 applyTeammateToolActivity），是"此刻在做什么"的瞬态。放在记录表
  // 之前：先看它此刻在动什么，再看它的记录。
  const live = renderRoleLiveTools(identity?.liveTools);
  // 已发布的 message 行与未同步草稿**各自成区**：草稿是 sequencer 的 WAL，还没成为
  // 这个角色的 message；混在一起时用户看不出"哪一行还没同步"。
  const published = view.roleRows.length
    ? `<div class="role-session-rows">${view.roleRows.map(row => renderPublishedRoleRow(row, display)).join("")}</div>`
    : "";
  const drafts = renderRoleDraftBlock(view, display);
  if (!published && !drafts && !record && !live) {
    return `<div class="role-session-detail" data-role-session="${escapeHtml(view.roleName)}">${switcher}${toolbar}${warnings}${header}
      <div class="role-session-empty muted">该角色还没有独立会话行（未发言或尚未同步）。</div></div>`;
  }
  return `<div class="role-session-detail" data-role-session="${escapeHtml(view.roleName)}">${switcher}${toolbar}${warnings}${header}${live}${record}${published}${drafts}</div>`;
}

// ROLE_LIVE_STATUS_LABEL 把 teammate.tool.* 载荷的状态翻成面板上的短词。
// 未知取值**原样显示**（后端加新状态时，前端把它写死成 running 就是把事实说错）。
const ROLE_LIVE_STATUS_LABEL = { running: "进行中", success: "完成", error: "失败" };

// renderRoleLiveTools 渲染「正在做（实时）」一节：这位 teammate 此刻的工具活动。
//
// 数据来源是 `teammate.tool.started/completed` 事件的载荷（**不进快照**的瞬态，与
// subagent 详情里的 tool_events 同一形态）：一条一步、最新在下。它是"正在发生什么"，
// 不是权威记录——权威记录在下面的记录表与已发布/草稿行里（那两份来自角色会话投影，
// 这一份来自事件流）。没有活动 → 不渲染空壳。
export function renderRoleLiveTools(steps) {
  const list = (Array.isArray(steps) ? steps : []).filter(step => step && step.id);
  if (list.length === 0) return "";
  const rows = list.map(step => {
    const status = String(step.status || "").trim();
    const label = ROLE_LIVE_STATUS_LABEL[status] || status || "—";
    const detail = String(step.result || step.error || step.arguments || "").replace(/\s+/g, " ").trim();
    const shown = detail.length > 160 ? `${detail.slice(0, 159)}…` : detail;
    return `<li class="role-live-step is-${escapeHtml(status)}" data-live-id="${escapeHtml(String(step.id))}">
        <span class="chip role-live-status is-${escapeHtml(status)}">${escapeHtml(label)}</span>
        <code class="role-live-name">${escapeHtml(String(step.name || "?"))}</code>
        ${shown ? `<span class="role-live-detail" title="${escapeHtml(detail)}">${escapeHtml(shown)}</span>` : ""}
      </li>`;
  }).join("");
  return `<section class="team-block role-live-tools" data-role-live-tools="${list.length}">
      <div class="section-title sub-title"><span>正在做（实时）</span><span class="badge">${list.length}</span></div>
      <div class="role-session-meta muted">每一行都来自 ${escapeHtml("teammate.tool.*")} 事件（不进快照的瞬态）：这一轮还没跑完时，这里就是它此刻在做什么。</div>
      <ul class="role-live-steps">${rows}</ul>
    </section>`;
}

// renderPublishedRoleRow 是**已发布**的角色行（同步进 message 之后的行，有 seq）。
function renderPublishedRoleRow(row, display) {
  return `<article class="role-session-row">
      <div class="role-session-row-head"><strong>${escapeHtml(display)}</strong><span class="muted">seq ${row.seq}</span><span class="muted">${escapeHtml(row.role)}</span></div>
      ${row.reasoning ? `<div class="role-session-reasoning muted">${escapeHtml(row.reasoning)}</div>` : ""}
      ${row.content ? `<div class="role-session-text">${escapeHtml(row.content)}</div>` : ""}
      ${row.toolCalls.map(call => `<div class="role-session-tool muted">tool ${escapeHtml(call.name || "?")} ${escapeHtml(call.arguments || "")}</div>`).join("")}
    </article>`;
}

// renderRoleDraftBlock 渲染「未同步草稿」独立区：条数徽标 + 每行 is-draft 标记 +
// 待同步文案 + 轮次/单元/类别（草稿还没有 seq，不能拿 seq 冒充身份）。
//
// 生命周期口径：草稿在本轮**完成**时由 sequencer 同步成 message 行（SyncRoleDraft：
// 发布 head+floor 后删除 draft 文件），本轮**取消/结束**时同理收敛——前端只渲染后端
// 权威投影，不自己推演"同步后应该长什么样"；草稿区消失的唯一原因是后端
// snapshot.draft_rows 变空。
function renderRoleDraftBlock(view, display) {
  if (!view.draftRows.length) return "";
  const items = view.draftRows.map((row, index) => {
    const kind = DRAFT_KIND_LABEL[row.kind] || row.kind || "草稿";
    return `<article class="role-session-row is-draft" data-draft-index="${index + 1}" data-draft-kind="${escapeHtml(row.kind)}" data-draft-round="${row.roundID}" data-draft-unit="${row.unitSeq}" title="未同步草稿：本轮结束（完成或取消）同步后才写进 message">
      <div class="role-session-row-head"><strong>${escapeHtml(display)}</strong><span class="muted">${escapeHtml(draftRowMeta(row))}</span><span class="chip is-draft">待同步</span><span class="muted">${escapeHtml(kind)}</span><span class="muted">${escapeHtml(row.role)}</span></div>
      ${row.reasoning ? `<div class="role-session-reasoning muted">${escapeHtml(row.reasoning)}</div>` : ""}
      ${row.content ? `<div class="role-session-text">${escapeHtml(row.content)}</div>` : ""}
      ${row.toolCalls.map(call => `<div class="role-session-tool muted">tool ${escapeHtml(call.name || "?")} ${escapeHtml(call.arguments || "")}</div>`).join("")}
    </article>`;
  }).join("");
  return `<div class="team-block role-session-drafts" data-role-drafts="${view.draftRows.length}">
    <div class="section-title sub-title"><span>未同步草稿</span><span class="badge">${view.draftRows.length}</span></div>
    <div class="role-session-meta muted">本轮还没结束（或上一次同步没成功）：这些行还在 draft 里，同步成功后才出现在上面的 message 行中。</div>
    <div class="role-session-rows">${items}</div>
  </div>`;
}

// draftRowMeta 是草稿行的身份串：轮次 + 单元（后端 sequencer 的排序键）。
function draftRowMeta(row) {
  const parts = [];
  if (row.roundID > 0) parts.push(`round ${row.roundID}`);
  if (row.unitSeq > 0) parts.push(`unit ${row.unitSeq}`);
  return parts.length ? parts.join(" · ") : "未分配回合号";
}

// renderRoleRecordTable 用**竖排记录**表达该 teammate 自己那份 team work 记录：
// **一条回合一行**，行里两栏 = main 车道 / 它自己那条车道。
//
// 为什么竖排（2026-10-03 现场口径）：修前是横向 excle-grid——车道当行、回合号当列，
// 时间往右长。回合一多，窄弹窗里横向滚动才有内容，读起来是"一张表"而不是"一条过程"；
// 而这块记录要回答的恰恰是"这位在时间轴上依次经历了什么"。改成回合当行之后，时间
// 自上而下，回合序号就是行号，草稿行接在末尾（"草稿N"当行号——草稿还没有发布 seq，
// 不能冒充第 0 回合）。
//
//   main 车道 = 主会话自己的回合（整段）；
//   自身车道 = 它入伙之后的共享回合（seq > prefix_cut_seq，标成 is-shared）
//              + 它自己的行（标成 is-own）；
//              入伙之前（或已被压缩掉）的回合两栏都是占位 —（is-outside）：
//              **不冒充它记得的上下文**。
//
// 切点来自后端只读投影（prefix_cut_seq，判据与角色 wire 装配一致：join_seq_id，
// compact 后取更大的 applied_seq；main 复用主会话本身、切点恒为 0）。前端只渲染，
// 没有任何回写口——把渲染结果推回去会让后端真值变成前端派生物。
export function renderRoleRecordTable(snapshot) {
  const view = normalizeRoleSession(snapshot);
  const ownRows = view.roleRows;
  const draftRows = view.draftRows;
  if (!view.mainRows.length && !ownRows.length && !draftRows.length) return "";
  const display = roleDisplayName(view.roleName);
  const cut = view.prefixCutSeq;
  const mainBySeq = new Map(view.mainRows.map(row => [seqOf(row), row]));
  const ownBySeq = new Map(ownRows.map(row => [seqOf(row), row]));
  const seqs = [...new Set([...mainBySeq.keys(), ...ownBySeq.keys()])].sort((a, b) => a - b);
  const mainCell = (seq) => mainBySeq.has(seq)
    ? `<td class="role-record-cell is-main" data-seq="${seq}" title="主会话自己的回合">${escapeHtml(recordCellText(mainBySeq.get(seq)))}</td>`
    : `<td class="role-record-cell is-empty" data-seq="${seq}" title="这一回合主会话没有行">·</td>`;
  const ownCell = (seq) => {
    if (ownBySeq.has(seq)) {
      return `<td class="role-record-cell is-own" data-seq="${seq}" title="${escapeHtml(display)} 自己的回合">${escapeHtml(recordCellText(ownBySeq.get(seq)))}</td>`;
    }
    if (!mainBySeq.has(seq)) {
      return `<td class="role-record-cell is-empty" data-seq="${seq}" title="这一回合谁都没有行">·</td>`;
    }
    if (cut > 0 && seq <= cut) {
      return `<td class="role-record-cell is-outside" data-seq="${seq}" title="不在 ${escapeHtml(display)} 的前缀匹配区间">—</td>`;
    }
    return `<td class="role-record-cell is-shared" data-seq="${seq}" title="共享上下文（它能看到的 main 回合）">${escapeHtml(recordCellText(mainBySeq.get(seq)))}</td>`;
  };
  // 轮次行：一条回合一行（时间自上而下）；两条车道的归属各自成栏。
  const rounds = seqs.map(seq => `<tr class="role-record-round" data-seq="${seq}">
        <th class="role-record-seq" scope="row">${seq}</th>
        ${mainCell(seq)}
        ${ownCell(seq)}
      </tr>`).join("");
  // 草稿行接在末尾：行号是「草稿N」而不是 seq（seq 由 sequencer 在同步时分配）。
  const drafts = draftRows.map((row, index) => `<tr class="role-record-round is-draft" data-draft="${index + 1}">
        <th class="role-record-seq is-draft" scope="row" title="未同步草稿：同步后才分配 seq">草稿${index + 1}</th>
        <td class="role-record-cell is-empty" data-draft="${index + 1}" title="草稿只属于它自己的车道">·</td>
        <td class="role-record-cell is-own is-draft" data-draft="${index + 1}" title="未同步草稿：本轮结束（完成或取消）同步后才写进 message">${escapeHtml(recordCellText(row))}</td>
      </tr>`).join("");
  const legend = cut > 0
    ? `前缀匹配自 seq ${cut + 1} 起：更早的 ${view.outsidePrefixMainRows} 行不在它的记录里（占位 —）`
    : "该会话整段都在它的记录里";
  const draftNote = draftRows.length
    ? `；末尾 ${draftRows.length} 行「草稿N」是未同步草稿，同步后才成为 message 行（还没有 seq）`
    : "";
  return `<div class="role-record" data-record-cut="${cut}" data-record-outside="${view.outsidePrefixMainRows}" data-record-visible="${view.visibleMainRows}" data-record-own="${ownRows.length}" data-record-draft="${draftRows.length}">
      <div class="role-record-legend muted">${escapeHtml(legend + draftNote)}</div>
      <table class="role-record-table" data-role-record-table>
        <thead><tr class="role-record-head"><th class="role-record-seq" scope="col">回合</th><th class="role-record-lane-head" scope="col">main</th><th class="role-record-lane-head is-own" scope="col" title="${escapeHtml(display)}">${escapeHtml(view.roleName || display)}</th></tr></thead>
        <tbody>${rounds}${drafts}</tbody>
      </table>
    </div>`;
}

function seqOf(row) {
  const value = Number(row?.seq);
  return Number.isFinite(value) ? value : 0;
}

function recordCellText(row) {
  const value = String(row?.content || row?.reasoning || "").replace(/\s+/g, " ").trim();
  return value.length <= 60 ? value : `${value.slice(0, 60)}…`;
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
// 面板不提供任何调序通道（次序 = 登记先后；目标态归 leader 的 team plan 的
// stages[].depends_on），所以这个函数只回答"在不在发言顺序里"。返回 null = 动作非法。
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

// isPinnedRole 暴露 pin 规则给 app.js 的按钮禁用判断（与渲染同源）。
export function isPinnedRole(roleName) {
  return PINNED_ROLES.has(roleName);
}
