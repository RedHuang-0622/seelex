// role-permission-board.js — 角色权限板的纯渲染面。
//
// 权限只有两个持久化粒度：角色默认（跨会话）与会话覆盖（当前会话）。账户只展示
// provider/model/余额并继承角色权限，不提供独立的权限编辑入口。
import { escapeHtml } from "./components.js";
import { balanceReadingText } from "./account-admin.js";
import { ACCOUNT_WINDOW_CUSTOM, accountWindowLabel, accountWindowOptions } from "./account-window.js";
import {
  PERMISSION_VECTOR_TAGS,
  normalizePermissionVector,
  permissionVectorLabel,
  permissionVectorSummary,
} from "./role-settings.js";

export const PERMISSION_ROLE_ORDER = ["goalplan", "agent", "subagent", "teammate"];
export const PERMISSION_ROLE_LABELS = {
  agent: "mainagent",
  goalplan: "goalplan",
  subagent: "subagent",
  teammate: "teammate",
};

export function permissionRoleLabel(role) {
  return PERMISSION_ROLE_LABELS[role] || role;
}

export function orderedPermissionRoles(rows) {
  const list = Array.isArray(rows) ? rows.filter(row => row && row.role) : [];
  const rank = new Map(PERMISSION_ROLE_ORDER.map((role, index) => [role, index]));
  return list.slice().sort((left, right) =>
    (rank.get(left.role) ?? 99) - (rank.get(right.role) ?? 99) ||
    String(left.role).localeCompare(String(right.role)));
}

export function permissionCandidateTags(vector) {
  const active = new Set(normalizePermissionVector(vector).map(tag => tag.replace(/^!/, "")));
  return PERMISSION_VECTOR_TAGS.filter(tag => !active.has(tag.id));
}

function permissionChipsHTML(vector, role, mode, disabled, userSpaceCount = 0) {
  const selected = normalizePermissionVector(vector);
  const selectedHTML = selected.length
    ? selected.map(tag => {
        const denied = tag.startsWith("!");
        const id = denied ? tag.slice(1) : tag;
        const pathEditor = id === "ssh" && !denied
          ? `<button type="button" class="perm-path-edit" data-board="${mode}" data-role="${escapeHtml(role)}" data-role-user-spaces="${escapeHtml(role)}" title="编辑 SSH 允许路径范围"${disabled ? " disabled" : ""}>路径 ${userSpaceCount}</button>`
          : "";
        return `<span class="perm-selected-pair"><button type="button" class="perm-tag ${denied ? "is-denied" : "is-on"}" data-board="${mode}" data-role="${escapeHtml(role)}" data-role-perm-cycle="${escapeHtml(id)}" title="${denied ? "点击改为允许，再点移除" : "点击改为显式拒绝，再点移除"}"${disabled ? " disabled" : ""}>${escapeHtml(permissionVectorLabel(id) || id)}${denied ? "（拒）" : ""}</button>${pathEditor}</span>`;
      }).join("")
    : '<span class="muted">尚未选择权限</span>';
  const candidates = permissionCandidateTags(vector).map(tag =>
    `<button type="button" class="perm-candidate" data-board="${mode}" data-role="${escapeHtml(role)}" data-role-perm-add="${escapeHtml(tag.id)}" title="${escapeHtml(tag.tip)}"${disabled ? " disabled" : ""}>+ ${escapeHtml(tag.label)}</button>`).join("");
  return `<div class="perm-composer${disabled ? " is-disabled" : ""}">
    <div class="perm-composer-label">候选权限</div>
    <div class="perm-candidate-lane">${candidates || '<span class="muted">全部权限已加入</span>'}</div>
    <div class="perm-composer-label">确认选择</div>
    <div class="perm-confirmed-lane" aria-label="${escapeHtml(role)} 已选择权限">${selectedHTML}</div>
  </div>`;
}

function accountWindowHTML(role, account, windowPresets, windowReadings) {
  const name = String(account?.name || "");
  const hasReading = windowReadings instanceof Map && windowReadings.has(name);
  const snapshotWindow = Number(account?.context_window);
  const effective = hasReading
    ? windowReadings.get(name)
    : (Number.isFinite(snapshotWindow) && snapshotWindow > 0 ? snapshotWindow : null);
  const options = accountWindowOptions(windowPresets);
  const selected = effective !== null && options.some(option => option.value === String(effective))
    ? String(effective)
    : ACCOUNT_WINDOW_CUSTOM;
  const optionHTML = options.map(option =>
    `<option value="${escapeHtml(option.value)}"${option.value === selected ? " selected" : ""}>${escapeHtml(option.label)}</option>`).join("");
  const customValue = selected === ACCOUNT_WINDOW_CUSTOM && effective !== null ? String(effective) : "";
  return `<label class="role-account-window">
    <span>上下文</span>
    <select data-role-account-window-select="${escapeHtml(name)}" data-role="${escapeHtml(role)}" aria-label="${escapeHtml(name)} 的上下文窗口">${optionHTML}</select>
    <input type="number" min="1" step="1000" data-role-account-window-input="${escapeHtml(name)}" data-role="${escapeHtml(role)}" value="${escapeHtml(customValue)}" placeholder="token 数"${selected === ACCOUNT_WINDOW_CUSTOM ? "" : " hidden"}>
    <em>当前 ${escapeHtml(accountWindowLabel(effective ?? 0))}</em>
  </label>`;
}

function accountRowsHTML(role, accounts, balances, selectedAccount, windowPresets, windowReadings) {
  const list = Array.isArray(accounts) ? accounts : [];
  if (!list.length) return '<div class="role-account-empty">该角色还没有账户</div>';
  return list.map(account => {
    const name = String(account?.name || "");
    const health = account?.api_key_set === false ? "未配置密钥" : "";
    const fallback = account?.fallback ? "内置兜底" : "";
    const state = [health, fallback].filter(Boolean).join(" · ");
    const reading = balanceReadingText(balances instanceof Map ? balances.get(name) : balances?.[name]);
    const active = name === selectedAccount;
    return `<div class="role-account-row${active ? " is-active" : ""}">
      <button type="button" class="role-account-identity" data-role-account-pick="${escapeHtml(name)}" data-role="${escapeHtml(role)}" aria-pressed="${active}">
        <strong>${escapeHtml(name)}</strong>
        <span>${escapeHtml([account.provider, account.model].filter(Boolean).join(" / ") || "未标注模型")}</span>
        ${state ? `<em>${escapeHtml(state)}</em>` : ""}
      </button>
      <div class="role-account-actions">
        <button type="button" class="text-button" data-role-account-balance="${escapeHtml(name)}">余额</button>
        <button type="button" class="text-button" data-role-account-edit="${escapeHtml(name)}" data-role="${escapeHtml(role)}">编辑</button>
        <button type="button" class="text-button danger" data-role-account-delete="${escapeHtml(name)}" data-role="${escapeHtml(role)}">删除</button>
      </div>
      <span class="role-account-credit${reading ? "" : " muted"}">${escapeHtml(reading || "未查询")}</span>
      ${accountWindowHTML(role, account, windowPresets, windowReadings)}
    </div>`;
  }).join("");
}

export function rolePermissionBoardHTML(rows, options = {}) {
  const mode = options.mode === "session" ? "session" : "default";
  const drafts = options.drafts instanceof Map ? options.drafts : new Map();
  const expanded = options.expanded instanceof Set ? options.expanded : new Set();
  const accounts = Array.isArray(options.accounts) ? options.accounts : [];
  const balances = options.balances instanceof Map ? options.balances : new Map();
  const windowPresets = Array.isArray(options.windowPresets) ? options.windowPresets : [];
  const windowReadings = options.windowReadings instanceof Map ? options.windowReadings : new Map();
  const ordered = orderedPermissionRoles(rows);
  const mainAgent = ordered.find(row => row.role === "agent");
  const mainVector = normalizePermissionVector(
    mode === "session" ? mainAgent?.permission_vector : mainAgent?.default_permission_vector || [],
  );
  if (!ordered.length) return '<div class="role-permission-empty">读不到角色目录</div>';
  return ordered.map(row => {
    const role = String(row.role);
    const inherit = role !== "agent" && (role === "subagent" || role === "teammate") && row.inherit_main_agent === true;
    const baseVector = mode === "session"
      ? row.permission_vector || []
      : (inherit ? mainVector : row.default_permission_vector || []);
    const draft = drafts.has(role) ? drafts.get(role) : baseVector;
    const roleAccounts = accounts.filter(account => account?.role === role);
    const open = expanded.has(role);
    const accountCount = roleAccounts.length || row.accounts?.length || 0;
    const inheritedLocked = mode === "default" && inherit;
    return `<article class="role-permission-card${open ? " is-expanded" : ""}" data-role-card="${escapeHtml(role)}">
      <button type="button" class="role-permission-head" data-role-toggle="${escapeHtml(role)}" aria-expanded="${open}">
        <span class="role-permission-name">${escapeHtml(permissionRoleLabel(role))}</span>
        <span class="role-permission-summary">${escapeHtml(permissionVectorSummary(draft))}</span>
        <span class="role-permission-account-count">${accountCount} 个账户</span>
        <span class="role-permission-chevron" aria-hidden="true">${open ? "−" : "+"}</span>
      </button>
      <div class="role-permission-body"${open ? "" : " hidden"}>
        ${(role === "subagent" || role === "teammate") && mode === "default" ? `<button type="button" class="role-inherit-chip${inherit ? " is-on" : ""}" data-role-inherit="${escapeHtml(role)}" aria-pressed="${inherit}">继承 mainagent 权限</button>` : ""}
        ${permissionChipsHTML(
          draft,
          role,
          mode,
          inheritedLocked,
          (mode === "session" ? row.session_ssh_user_spaces : row.default_ssh_user_spaces)?.length || 0,
        )}
        <div class="role-accounts-head"><span>账户与余额</span><button type="button" class="text-button" data-role-add-account="${escapeHtml(role)}">添加账户</button></div>
        <div class="role-account-list">${accountRowsHTML(role, roleAccounts, balances, row.account, windowPresets, windowReadings)}</div>
      </div>
    </article>`;
  }).join("");
}
