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
    designNotice: Array.isArray(source.design_notice) ? source.design_notice.filter(item => typeof item === "string" && item) : []
  };
}

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
export function renderAgentTeam(view, presets) {
  const team = normalizeAgentTeam(view);
  const presetList = Array.isArray(presets) ? presets.filter(item => item && typeof item.team_kind === "string" && item.team_kind) : [];
  const blocks = [];

  if (team.designNotice.length) {
    blocks.push(`<div class="team-notice" role="status">${team.designNotice.map(escapeHtml).join("；")}</div>`);
  }

  blocks.push(presetRow(presetList, team));

  if (!team.configured) {
    blocks.push('<div class="team-empty muted">当前会话未装配 AgentTeam：装配后 TL / 评审等角色会出现在成员表，并进入工作顺序编排。</div>');
    return `<div class="team-panel">${blocks.join("")}</div>`;
  }

  blocks.push(metaRow(team));
  blocks.push(orderBlock(team));
  blocks.push(memberBlock(team));
  if (team.scheduled.length) blocks.push(scheduledBlock(team));
  return `<div class="team-panel">${blocks.join("")}</div>`;
}

function presetRow(presets, team) {
  if (!presets.length) return "";
  const buttons = presets.map(preset => {
    const kind = preset.team_kind;
    const active = kind === team.teamKind;
    const label = active ? `${kind} 已装配` : `装配 ${kind}`;
    return `<button type="button" class="text-button team-preset${active ? " is-active" : ""}" data-team-materialize="${escapeHtml(kind)}"${active ? ' title="重复装配是幂等的，不会新建第二个角色会话"' : ""}>${escapeHtml(label)}</button>`;
  }).join("");
  return `<div class="team-toolbar">${buttons}<button type="button" class="text-button team-refresh" data-team-refresh="1" title="重新拉取成员表与工作顺序">刷新</button></div>`;
}

function metaRow(team) {
  const policyLabel = POLICY_LABEL[team.orderPolicy] || team.orderPolicy || "未设置顺序策略";
  const options = POLICY_OPTIONS.map(([value, label]) =>
    `<option value="${escapeHtml(value)}"${value === team.orderPolicy ? " selected" : ""}>${escapeHtml(label)}</option>`).join("");
  return `<div class="team-meta">
    <span class="chip">${escapeHtml(team.teamKind || "team")}</span>
    <span class="team-policy-label" title="顺序策略决定 sequencer 的 role 顺序函数">${escapeHtml(policyLabel)}</span>
    <select class="team-policy-select" data-team-policy aria-label="顺序策略" title="只提交顺序策略字段，顺序表由角色注册表派生">${options}</select>
    <span class="team-floor" title="当前发言权（floor 随 message head 发布）">floor ${escapeHtml(team.floorRole || "—")}</span>
  </div>`;
}

function orderBlock(team) {
  const rows = team.orderRoles.map((roleName, index) => {
    const pinned = PINNED_ROLES.has(roleName);
    const onFloor = roleName === team.floorRole;
    return `<li class="team-order-item${onFloor ? " is-floor" : ""}" data-team-order-role="${escapeHtml(roleName)}">
      <span class="team-order-index">${index + 1}</span>
      <span class="team-order-role">${escapeHtml(roleName)}</span>
      ${onFloor ? '<span class="chip team-floor-chip">发言中</span>' : ""}
      <span class="team-order-actions">
        <button type="button" class="text-button" data-team-action="up" data-team-role="${escapeHtml(roleName)}"${index === 0 ? " disabled" : ""} title="上移">↑</button>
        <button type="button" class="text-button" data-team-action="down" data-team-role="${escapeHtml(roleName)}"${index === team.orderRoles.length - 1 ? " disabled" : ""} title="下移">↓</button>
        <button type="button" class="text-button" data-team-action="remove" data-team-role="${escapeHtml(roleName)}"${pinned ? ' disabled title="user/main 是群聊起手与收口，不能摘除"' : ' title="从工作顺序摘除（保留角色）"'}>摘除</button>
      </span>
    </li>`;
  }).join("");
  const outside = team.members.filter(member => !member.inOrder && member.roleKind !== "timer");
  const restore = outside.length
    ? `<div class="team-order-outside">未排入顺序：${outside.map(member =>
      `<button type="button" class="text-button" data-team-action="restore" data-team-role="${escapeHtml(member.roleName)}" title="加入工作顺序末尾">+ ${escapeHtml(member.roleName)}</button>`).join("")}</div>`
    : "";
  const list = rows || '<li class="team-order-item is-empty muted">工作顺序为空</li>';
  return `<div class="team-block">
    <div class="section-title sub-title"><span>工作顺序</span><span class="badge">${team.orderRoles.length}</span></div>
    <ol class="team-order">${list}</ol>
    ${restore}
  </div>`;
}

function memberBlock(team) {
  const rows = team.members.map(member => {
    const kind = ROLE_KIND_LABEL[member.roleKind] || member.roleKind || "—";
    const position = member.inOrder ? String(member.orderIndex + 1) : "—";
    const onFloor = member.roleName === team.floorRole;
    return `<li class="team-member${onFloor ? " is-floor" : ""}">
      <span class="team-member-name" title="${escapeHtml(member.roleSessionID || member.roleName)}">${escapeHtml(member.roleName)}</span>
      <span class="chip">${escapeHtml(kind)}</span>
      <span class="team-member-pos" title="工作顺序位置">#${escapeHtml(position)}</span>
      <span class="team-member-state">${member.inOrder ? "在顺序内" : "未排入"}</span>
      <button type="button" class="text-button team-member-remove" data-team-delete="${escapeHtml(member.roleName)}"${PINNED_ROLES.has(member.roleName) ? ' disabled title="user/main 由会话本身提供，不能删除"' : ' title="从注册表与工作顺序中删除该角色"'}>删除</button>
    </li>`;
  }).join("");
  return `<div class="team-block">
    <div class="section-title sub-title"><span>成员</span><span class="badge">${team.members.length}</span></div>
    <ul class="team-members">${rows || '<li class="team-member muted">暂无角色</li>'}</ul>
  </div>`;
}

function scheduledBlock(team) {
  const rows = team.scheduled.map(member =>
    `<li class="team-scheduled-item">
      <span class="team-member-name">${escapeHtml(member.roleName)}</span>
      <span class="chip">${escapeHtml(ROLE_KIND_LABEL[member.roleKind] || member.roleKind || "定时")}</span>
      <span class="team-scheduled-note">定时插话 · 不参与工作顺序</span>
    </li>`).join("");
  return `<div class="team-block">
    <div class="section-title sub-title"><span>定时任务 agent</span><span class="badge">${team.scheduled.length}</span></div>
    <ul class="team-scheduled">${rows}</ul>
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
