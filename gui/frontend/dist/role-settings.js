// role-settings.js — 「角色设置」四项（账号 / 上下文窗口 / 权限向量 / 默认 plugin）的
// **纯取值面**：标签表、归一化、开关、lint、以及三项选择器的 HTML 串。
//
// 抽成模块的理由与 permission-tier.js 一致：app.js 是巨型 DOM 脚本（`node --test` 进不去），
// 而"哪些标签合法 / 顺序怎么定 / 少了哪个会出事"都是纯判定——放在这里才能被测试直接钉住。
// app.js 只保留 DOM 写入与 invoke。
//
// 口径：
//   1. 权限向量的**标签 id 就是发给后端的字面量**（read / write / computeruse / control /
//      bash / ssh），前端不另起一套别名，也不做 ro→read 这类对照表——
//      对照表一旦出现就会有两份真相。中文只作 label（展示）。
//   2. 顺序固定为 PERMISSION_VECTOR_TAGS 的声明顺序：同一组选择永远渲染成同一行，
//      后端拿到的数组也稳定（便于比对与去抖）。
//   3. 未知标签一律丢弃（既不渲染也不提交）：标签表是这一份，别处不再兜底。
//   4. lint 的 id 与 docs/arch/permission-assembly.md §5 的四条同名（assembly_*），
//      只做提示、不做拦截——最终判定归后端 PermissionGate。
//   5. 标签有**三态**：不选 / 给（`id`）/ 显式拒绝（`!id`）。第三态不是装饰：后端的
//      "非空即封闭"会把员工没列出的组投影成 `!组`，所以前端**必须原样保留** `!id`——
//      旧实现把它当未知标签丢掉，等于用户一按保存就把"明确拒绝"擦成"继承"，是静默放权。

import { escapeHtml } from "./components.js";

// PERMISSION_VECTOR_TAGS 是权限向量的唯一标签表。label 仅用于按钮文字，tip 是一句
// "这把钥匙开什么门"；两列都不参与提交。
export const PERMISSION_VECTOR_TAGS = [
  { id: "read", label: "读", tip: "读文件、搜内容、列目录" },
  { id: "write", label: "写", tip: "写文件、改文件（不含命令执行）" },
  { id: "computeruse", label: "用电脑", tip: "截屏与桌面操作" },
  { id: "control", label: "控制", tip: "结束/恢复执行、派生任务、调度团队" },
  { id: "bash", label: "Bash", tip: "直接执行 shell 命令与后台命令" },
  { id: "ssh", label: "SSH", tip: "在会话固定环境上执行远程命令与文件操作（路径范围在该能力内配置）" },
];

const PERMISSION_VECTOR_IDS = PERMISSION_VECTOR_TAGS.map(tag => tag.id);

export function permissionVectorLabel(id) {
  const found = PERMISSION_VECTOR_TAGS.find(tag => tag.id === id);
  return found ? found.label : "";
}

// splitPermissionTag 拆出"这一项是给还是拒"。`!` 前缀是后端契约（显式拒绝），
// 前端只做识别，不另造标记。
function splitPermissionTag(raw) {
  const text = String(raw ?? "").trim();
  return text.startsWith("!") ? { id: text.slice(1), denied: true } : { id: text, denied: false };
}

// normalizePermissionVector 把任意输入（后端返回值、脏 JSON、用户点出来的数组）收成
// "已知标签 + 去重 + 声明顺序"的数组，**保留 `!id` 显式拒绝**。非数组输入 → 空数组。
//
// 同一标签既给又拒时按**拒绝**落地（fail-closed）：两者并存只可能是上游拼装出错，
// 此时宁可少给能力。输出里永远只有一个形态（`id` 或 `!id`），不会同时出现。
export function normalizePermissionVector(input) {
  const list = Array.isArray(input) ? input : [];
  const allowed = new Set();
  const denied = new Set();
  for (const item of list) {
    const parsed = splitPermissionTag(item);
    if (!PERMISSION_VECTOR_IDS.includes(parsed.id)) continue;
    if (parsed.denied) denied.add(parsed.id);
    else allowed.add(parsed.id);
  }
  for (const id of denied) allowed.delete(id);
  return PERMISSION_VECTOR_IDS
    .filter(id => allowed.has(id) || denied.has(id))
    .map(id => (denied.has(id) ? `!${id}` : id));
}

// cyclePermissionVector 是"点一下标签"的三态循环：不选 → 给 → 拒 → 不选。
// 三态而不是两态，是因为"没列出来"（继承）与"明确不给"在后端是两个事实。
export function cyclePermissionVector(current, id) {
  const base = normalizePermissionVector(current);
  const target = String(id || "").trim();
  if (!PERMISSION_VECTOR_IDS.includes(target)) return base;
  if (base.includes(target)) {
    return normalizePermissionVector(base.filter(item => item !== target).concat([`!${target}`]));
  }
  if (base.includes(`!${target}`)) {
    return base.filter(item => item !== `!${target}`);
  }
  return normalizePermissionVector(base.concat([target]));
}

// togglePermissionVector 是"点一下标签"的纯函数：已选则去掉，未选则加上，
// 结果同样是归一化后的稳定顺序。未知 id 原样返回（不改用户现有选择）。
export function togglePermissionVector(current, id) {
  const base = normalizePermissionVector(current);
  const target = String(id || "").trim();
  if (!PERMISSION_VECTOR_IDS.includes(target)) return base;
  return base.includes(target) ? base.filter(item => item !== target) : normalizePermissionVector([...base, target]);
}

// permissionVectorSummary 给一行短读数（没选任何标签时说"未选"；显式拒绝标"拒 x"）。
export function permissionVectorSummary(selected) {
  const list = normalizePermissionVector(selected);
  if (!list.length) return "未选";
  return list.map(id => (id.startsWith("!") ? `拒 ${id.slice(1)}` : id)).join(" · ");
}

// permissionVectorLint 是"这组选择会走不通"的提示，与 permission-assembly §5 同 id：
//   assembly_ssh_only                只给 ssh 不给 read：远程列目录/读文件都走不通
//   assembly_write_without_read    写而无读：改文件前先要能读
export function permissionVectorLint(selected) {
  const list = normalizePermissionVector(selected);
  const has = id => list.includes(id);
  const warnings = [];
  if (has("ssh") && !has("read")) {
    warnings.push({ id: "assembly_ssh_only", message: "选了 SSH 却没选读：远程列目录与读文件都会走不通" });
  }
  if (has("write") && !has("read")) {
    warnings.push({ id: "assembly_write_without_read", message: "选了写却没选读：改文件前先得能读" });
  }
  if (list.length && list.every(id => id.startsWith("!"))) {
    warnings.push({ id: "assembly_nothing_allowed", message: "全部是显式拒绝：这组装配等于什么都不给" });
  }
  return warnings;
}

// renderPermissionVectorHTML 渲染一排多选标签（按声明顺序）。三态：不选 / 给（is-on）/
// 显式拒绝（is-denied，文字后缀「（拒）」）。两态都算"按下了"（aria-pressed=true）——
// 对读屏来说"我明确表过态"才是这一格的信息，具体给还是拒由 title 与后缀说清。
export function renderPermissionVectorHTML(selected) {
  const list = normalizePermissionVector(selected);
  const allowed = new Set(list.filter(id => !id.startsWith("!")));
  const denied = new Set(list.filter(id => id.startsWith("!")).map(id => id.slice(1)));
  return PERMISSION_VECTOR_TAGS.map(tag => {
    const on = allowed.has(tag.id);
    const isDenied = denied.has(tag.id);
    const classes = `perm-tag${on ? " is-on" : ""}${isDenied ? " is-denied" : ""}`;
    const tip = isDenied ? `${tag.tip}（显式拒绝：不是"没提"，是"不给"）` : tag.tip;
    const text = isDenied ? `${tag.label}（拒）` : tag.label;
    return `<button type="button" class="${classes}" data-perm-type="${escapeHtml(tag.id)}" aria-pressed="${on || isDenied}" title="${escapeHtml(tip)}">${escapeHtml(text)}</button>`;
  }).join("");
}

// roleAccountOptionsHTML 是「账号」下拉的选项串：名字 + （供应商 / 模型）。当前账号加
// 「当前」后缀，且永远排在列表里它自己的位置（不重排，读起来才是稳定的一张表）。
export function roleAccountOptionsHTML(accounts, current) {
  const list = Array.isArray(accounts) ? accounts.filter(account => account && account.name) : [];
  if (!list.length) return '<option value="">（读不到账号列表）</option>';
  return list.map(account => {
    const detail = [account.provider, account.model].filter(Boolean).join(" / ");
    const health = [];
    if (account.api_key_set === false) health.push("未配置密钥");
    if (account.fallback) health.push("内置兜底");
    const suffix = account.name === current ? "（当前）" : "";
    const base = detail ? `${account.name} · ${detail}` : account.name;
    const label = `${base}${health.length ? ` · ${health.join(" / ")}` : ""}${suffix}`;
    return `<option value="${escapeHtml(account.name)}"${account.name === current ? " selected" : ""}${account.disabled ? " disabled" : ""}>${escapeHtml(label)}</option>`;
  }).join("");
}

// rolePluginOptionsHTML 是「默认 plugin」下拉的选项串：名字 + （当前）标记。
export function rolePluginOptionsHTML(plugins, current) {
  const list = Array.isArray(plugins) ? plugins.filter(plugin => plugin && plugin.name) : [];
  if (!list.length) return '<option value="">（读不到插件列表）</option>';
  return list.map(plugin => {
    const suffix = plugin.name === current ? "（当前）" : "";
    return `<option value="${escapeHtml(plugin.name)}"${plugin.name === current ? " selected" : ""}>${escapeHtml(`${plugin.name}${suffix}`)}</option>`;
  }).join("");
}

// permissionRoleAccountOptionsHTML 是角色页的账号下拉：只列该角色组自己的账号。
// 空值 = 自动选择（账号池按角色负载），不是“没有权限”。
export function permissionRoleAccountOptionsHTML(names, selected, accounts = [], current = "") {
  const list = Array.isArray(names) ? names.filter(Boolean) : [];
  if (!list.length) return '<option value="">（该角色没有可用账号）</option>';
  const details = new Map((Array.isArray(accounts) ? accounts : []).map(account => [account?.name, account]));
  const options = ['<option value="">自动（按角色账号池）</option>'];
  for (const name of list) {
    const account = details.get(name) || {};
    const detail = [account.provider, account.model].filter(Boolean).join(" / ");
    const health = [];
    if (account.api_key_set === false) health.push("未配置密钥");
    if (account.fallback) health.push("内置兜底");
    let label = detail ? `${name} · ${detail}` : name;
    if (health.length) label += ` · ${health.join(" / ")}`;
    if (name === current) label += "（当前）";
    options.push(`<option value="${escapeHtml(name)}"${name === selected ? " selected" : ""}${account.disabled ? " disabled" : ""}>${escapeHtml(label)}</option>`);
  }
  return options.join("");
}
