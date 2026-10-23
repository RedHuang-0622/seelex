// role-settings.js — 「角色设置」四项（账号 / 上下文窗口 / 权限向量 / 默认 plugin）的
// **纯取值面**：标签表、归一化、开关、lint、以及三项选择器的 HTML 串。
//
// 抽成模块的理由与 permission-tier.js 一致：app.js 是巨型 DOM 脚本（`node --test` 进不去），
// 而"哪些标签合法 / 顺序怎么定 / 少了哪个会出事"都是纯判定——放在这里才能被测试直接钉住。
// app.js 只保留 DOM 写入与 invoke。
//
// 口径：
//   1. 权限向量的**标签 id 就是发给后端的字面量**（read / write / computeruse / control /
//      admin / ssh / ssh.userspace），前端不另起一套别名，也不做 ro→read 这类对照表——
//      对照表一旦出现就会有两份真相。中文只作 label（展示）。
//   2. 顺序固定为 PERMISSION_VECTOR_TAGS 的声明顺序：同一组选择永远渲染成同一行，
//      后端拿到的数组也稳定（便于比对与去抖）。
//   3. 未知标签一律丢弃（既不渲染也不提交）：标签表是这一份，别处不再兜底。
//   4. lint 的 id 与 docs/arch/permission-assembly.md §5 的四条同名（assembly_*），
//      只做提示、不做拦截——最终判定归后端 PermissionGate。

import { escapeHtml } from "./components.js";

// PERMISSION_VECTOR_TAGS 是权限向量的唯一标签表。label 仅用于按钮文字，tip 是一句
// "这把钥匙开什么门"；两列都不参与提交。
export const PERMISSION_VECTOR_TAGS = [
  { id: "read", label: "读", tip: "读文件、搜内容、列目录" },
  { id: "write", label: "写", tip: "写文件、改文件、跑命令" },
  { id: "computeruse", label: "用电脑", tip: "截屏与桌面操作" },
  { id: "control", label: "控制", tip: "会话与作业的调度控制" },
  { id: "admin", label: "管理", tip: "装配、插件与策略本身" },
  { id: "ssh", label: "SSH", tip: "连远程主机（命令与文件传输）" },
  { id: "ssh.userspace", label: "SSH 用户空间", tip: "远程落在工作区之外（含 ~ 与系统路径）" },
];

const PERMISSION_VECTOR_IDS = PERMISSION_VECTOR_TAGS.map(tag => tag.id);

export function permissionVectorLabel(id) {
  const found = PERMISSION_VECTOR_TAGS.find(tag => tag.id === id);
  return found ? found.label : "";
}

// normalizePermissionVector 把任意输入（后端返回值、脏 JSON、用户点出来的数组）收成
// "已知标签 + 去重 + 声明顺序"的数组。非数组输入 → 空数组。
export function normalizePermissionVector(input) {
  const list = Array.isArray(input) ? input : [];
  const picked = new Set(list.map(item => String(item || "").trim()).filter(id => PERMISSION_VECTOR_IDS.includes(id)));
  return PERMISSION_VECTOR_IDS.filter(id => picked.has(id));
}

// togglePermissionVector 是"点一下标签"的纯函数：已选则去掉，未选则加上，
// 结果同样是归一化后的稳定顺序。未知 id 原样返回（不改用户现有选择）。
export function togglePermissionVector(current, id) {
  const base = normalizePermissionVector(current);
  const target = String(id || "").trim();
  if (!PERMISSION_VECTOR_IDS.includes(target)) return base;
  return base.includes(target) ? base.filter(item => item !== target) : normalizePermissionVector([...base, target]);
}

// permissionVectorSummary 给一行短读数（没选任何标签时说"未选"）。
export function permissionVectorSummary(selected) {
  const list = normalizePermissionVector(selected);
  return list.length ? list.join(" · ") : "未选";
}

// permissionVectorLint 是"这组选择会走不通"的四条提示，与 permission-assembly §5 同 id：
//   assembly_ssh_only                只给 ssh 不给 read：远程列目录/读文件都走不通
//   assembly_write_without_read    写而无读：改文件前先要能读
//   assembly_userspace_without_ssh  用户空间没有 ssh 打底
//   assembly_admin_without_control   管装配却管不了会话
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
  if (has("ssh.userspace") && !has("ssh")) {
    warnings.push({ id: "assembly_userspace_without_ssh", message: "选了 SSH 用户空间却没选 SSH：它只是放宽范围，不能单独生效" });
  }
  if (has("admin") && !has("control")) {
    warnings.push({ id: "assembly_admin_without_control", message: "选了管理却没选控制：管装配却管不了会话" });
  }
  return warnings;
}

// renderPermissionVectorHTML 渲染一排多选标签（按声明顺序）。选中态走 aria-pressed，
// 键盘/读屏都能读到"这一枚按下了没有"。
export function renderPermissionVectorHTML(selected) {
  const list = normalizePermissionVector(selected);
  return PERMISSION_VECTOR_TAGS.map(tag => {
    const on = list.includes(tag.id);
    return `<button type="button" class="perm-tag${on ? " is-on" : ""}" data-perm-type="${escapeHtml(tag.id)}" aria-pressed="${on}" title="${escapeHtml(tag.tip)}">${escapeHtml(tag.label)}</button>`;
  }).join("");
}

// roleAccountOptionsHTML 是「账号」下拉的选项串：名字 + （供应商 / 模型）。当前账号加
// 「当前」后缀，且永远排在列表里它自己的位置（不重排，读起来才是稳定的一张表）。
export function roleAccountOptionsHTML(accounts, current) {
  const list = Array.isArray(accounts) ? accounts.filter(account => account && account.name) : [];
  if (!list.length) return '<option value="">（读不到账号列表）</option>';
  return list.map(account => {
    const detail = [account.provider, account.model].filter(Boolean).join(" / ");
    const suffix = account.name === current ? "（当前）" : "";
    const label = detail ? `${account.name} · ${detail}${suffix}` : `${account.name}${suffix}`;
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
