// 「角色设置」四项（账号 / 上下文窗口 / 权限向量 / 默认 plugin）的接线。
//
// 现场：设置页原来第一段是「会话存储」（后端类型 / 路径 / DSN / 测试连接 / 保存并切换），
// 2026-10-23 起整段删掉；账号上下文窗口也从独立一段挪进「角色设置」，与账号、权限向量、
// 默认 plugin 四项并列。四件事最容易漏：
//   1. 取值面在 app.js 里另抄一份（标签表 / 顺序 / 归一化各写一遍）→ 必须来自 role-settings.js；
//   2. 权限向量提交失败却显示成功（桥面还没开方法，M1–M3 在另一条工作项上）→ 必须如实标"未生效"；
//   3. 未知标签被渲染出来却提交不了 → 标签表是唯一一份，未知一律丢弃；
//   4. 注册表漏登记（`elements["role-account"]` 静默为 undefined）。
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  PERMISSION_VECTOR_TAGS,
  cyclePermissionVector,
  normalizePermissionVector,
  permissionVectorLint,
  permissionVectorSummary,
  renderPermissionVectorHTML,
  roleAccountOptionsHTML,
  rolePluginOptionsHTML,
  togglePermissionVector,
} from "./role-settings.js";

const read = (name) => readFile(new URL(`./${name}`, import.meta.url), "utf8");

test("标签表就是那七项，id 即提交字面量", () => {
  assert.deepEqual(
    PERMISSION_VECTOR_TAGS.map(tag => tag.id),
    ["read", "write", "computeruse", "control", "admin", "ssh", "ssh.userspace"],
  );
  for (const tag of PERMISSION_VECTOR_TAGS) {
    assert.ok(tag.label && tag.tip, `${tag.id} 要有中文标签与一句说明`);
  }
});

test("归一化：丢弃未知、去重、按声明顺序稳定", () => {
  assert.deepEqual(normalizePermissionVector(["ssh", "read"]), ["read", "ssh"]);
  assert.deepEqual(normalizePermissionVector(["ssh", "ssh", "read"]), ["read", "ssh"]);
  assert.deepEqual(normalizePermissionVector(["root", "rw", ""]), []);
  assert.deepEqual(normalizePermissionVector("read"), []);
  assert.deepEqual(normalizePermissionVector(undefined), []);
  // 后端若把 ro/rw 这类旧组名塞回来，前端不认（不另做别名对照表）。
  assert.deepEqual(normalizePermissionVector(["ro", "ssh"]), ["ssh"]);
  assert.equal(permissionVectorSummary(["ssh"]), "ssh");
  assert.equal(permissionVectorSummary([]), "未选");
});

test("点标签：加/减都回到稳定顺序，未知 id 不改选择", () => {
  assert.deepEqual(togglePermissionVector([], "write"), ["write"]);
  assert.deepEqual(togglePermissionVector(["write"], "read"), ["read", "write"]);
  assert.deepEqual(togglePermissionVector(["read", "write"], "write"), ["read"]);
  assert.deepEqual(togglePermissionVector(["read"], "root"), ["read"]);
});

test("lint 与 permission-assembly §5 的四个 id 同名，只提示不拦截", () => {
  assert.deepEqual(permissionVectorLint([]), []);
  assert.deepEqual(permissionVectorLint(["read"]), []);
  assert.deepEqual(permissionVectorLint(["ssh"]).map(w => w.id), ["assembly_ssh_only"]);
  assert.deepEqual(permissionVectorLint(["write"]).map(w => w.id), ["assembly_write_without_read"]);
  assert.deepEqual(permissionVectorLint(["ssh.userspace"]).map(w => w.id), ["assembly_userspace_without_ssh"]);
  assert.deepEqual(permissionVectorLint(["admin"]).map(w => w.id), ["assembly_admin_without_control"]);
  // 成组的合规选择不出提示（ssh + read 是最常见的一组）。
  assert.deepEqual(permissionVectorLint(["read", "write", "ssh"]), []);
  for (const warning of permissionVectorLint(["ssh", "write", "admin"])) {
    assert.ok(warning.message, "每条提示都要有一句人能读懂的话");
  }
});

test("标签渲染：aria-pressed 跟着选择走，文本要转义", () => {
  const html = renderPermissionVectorHTML(["read", "ssh"]);
  assert.match(html, /data-perm-type="read"[^>]*aria-pressed="true"/);
  assert.match(html, /data-perm-type="write"[^>]*aria-pressed="false"/);
  assert.match(html, /data-perm-type="ssh\.userspace"/);
  // 七枚都在（不多不少）。
  assert.equal((html.match(/data-perm-type=/g) || []).length, PERMISSION_VECTOR_TAGS.length);
  assert.doesNotMatch(renderPermissionVectorHTML(["nope"]), /data-perm-type="nope"/);
});

test("两个下拉：空清单给一句可读的占位，当前项标（当前）", () => {
  assert.match(roleAccountOptionsHTML([], "x"), /读不到账号列表/);
  const accounts = [
    { name: "main", provider: "deepseek", model: "v3" },
    { name: "cheap", provider: "deepseek", model: "lite", disabled: true },
  ];
  const html = roleAccountOptionsHTML(accounts, "main");
  assert.match(html, /<option value="main" selected>main · deepseek \/ v3（当前）<\/option>/);
  assert.match(html, /<option value="cheap" disabled>cheap · deepseek \/ lite<\/option>/);

  assert.match(rolePluginOptionsHTML([], "x"), /读不到插件列表/);
  const plugins = [{ name: "core" }, { name: "goal" }];
  assert.match(rolePluginOptionsHTML(plugins, "goal"), /<option value="goal" selected>goal（当前）<\/option>/);
});

test("设置弹窗：存储策略整段没了，角色设置占了它原来的位置", async () => {
  const html = await read("index.html");
  const modal = html.match(/<div id="settings-modal"[\s\S]*?\n  <\/div>/);
  assert.ok(modal, "要找得到 #settings-modal");
  const body = modal[0];

  // ① 存储策略区连同按钮、读数一起删干净（后端类型/路径/DSN/测试/保存）。
  for (const gone of ["storage-backend", "storage-path", "storage-dsn", "storage-test", "storage-save", "storage-status"]) {
    assert.doesNotMatch(body, new RegExp(`id="${gone}"`), `${gone} 必须从设置页消失`);
  }
  assert.doesNotMatch(body, /存储形式|保存并切换|测试连接/, "存储策略的文案也要一起删");

  // ② 角色设置四项并列：账号 / 上下文窗口 / 权限向量 / 默认 plugin。
  assert.match(body, /id="role-settings-section"/, "角色设置要自成一段");
  assert.match(body, /<div class="section-title">角色设置<\/div>/);
  assert.match(body, /<select id="role-account"><\/select>/, "账号下拉是空的（选项由快照填）");
  assert.match(body, /id="account-windows"><\/div>/, "上下文窗口的行由前端渲染（容器是空的）");
  assert.match(body, /id="account-windows-status"/, "上下文窗口要有一行读数/说明落点");
  assert.match(body, /id="role-permission-vector" class="perm-vector"/, "权限向量是多选标签容器");
  assert.match(body, /id="role-permission-vector-status"/, "权限向量要有提示行（lint / 未生效）");
  assert.match(body, /<select id="role-default-plugin"><\/select>/, "默认 plugin 下拉是空的（选项由快照填）");
  assert.match(body, /id="role-settings-status"/, "四项的提交结果要有一行落点");

  // 四项都在同一段里（顺序即并列顺序），且「账号上下文窗口」不再自成一段。
  const section = body.match(/id="role-settings-section"[\s\S]*?\n      <\/div>/);
  assert.ok(section, "要找得到角色设置那一段");
  for (const id of ["role-account", "account-windows", "role-permission-vector", "role-default-plugin"]) {
    assert.match(section[0], new RegExp(`id="${id}"`), `${id} 必须落在角色设置这一段里`);
  }
  assert.doesNotMatch(body, /<div class="section-title">账号上下文窗口<\/div>/, "它已经并入角色设置");

  // ④ 大段解说文案删了：每一段只留一句话（< 120 字）。
  for (const note of body.matchAll(/<p class="settings-note">([\s\S]*?)<\/p>/g)) {
    const text = note[1].replace(/<[^>]+>/g, "").trim();
    assert.ok(text.length > 0 && text.length <= 120, `这一段说明太长（${text.length} 字）：${text.slice(0, 40)}…`);
  }
});

test("app.js 接线：注册表 + 四项各自的动作 + 权限向量未生效不谎报", async () => {
  const app = await read("app.js");
  for (const id of ["role-account", "role-permission-vector", "role-permission-vector-status", "role-default-plugin", "role-settings-status"]) {
    assert.match(app, new RegExp(`"${id}"`), `${id} 必须注册进 elements`);
  }
  assert.match(app, /from "\.\/role-settings\.js"/, "取值面必须来自纯模块，而不是 app.js 里另抄一份");
  assert.match(app, /await syncRoleSettings\(\);/, "openSettings 要调它");

  // 账号：切账号走既有的 SelectAccount（不另造桥面方法）。
  assert.match(app, /invoke\("SelectAccount", name\)/);
  // 上下文窗口：仍走 AccountWindowPresets / SetAccountWindow（判据在后端）。
  assert.match(app, /await invoke\("AccountWindowPresets"\)/);
  assert.match(app, /invoke\("SetAccountWindow", name, window\)/);
  // 权限向量：整组提交。
  assert.match(app, /invoke\("SetPermissionVector", wanted\)/);
  // 默认 plugin：名字透传。
  assert.match(app, /invoke\("SetDefaultPlugin", name\)/);

  // 提交失败 → permissionVectorApplied = false，且状态行说「未生效」。
  const submit = app.match(/async function submitPermissionVector\(\)[\s\S]*?\n\}/);
  assert.ok(submit, "要找得到 submitPermissionVector");
  assert.match(submit[0], /catch \(error\) \{\s*permissionVectorApplied = false;/, "失败要落到未生效");
  assert.match(app, /permissionVectorApplied\) notes\.push\("未生效/, "未生效必须显式写在状态行上");
  assert.match(app, /permissionVectorLint\(permissionVectorDraft\)/, "lint 提示要跟着当前选择走");

  // 存储策略的读写整条不能再有残留（否则删了 DOM 也会在打开时抛）。
  for (const gone of ["SessionStorageConfig", "TestSessionStorage", "ConfigureSessionStorage", "storage-status"]) {
    assert.doesNotMatch(app, new RegExp(gone), `${gone} 的 JS 逻辑必须一并删掉`);
  }
});

test("显式拒绝（!tag）必须原样保留：丢掉它就等于把「明确不给」擦成「继承」", () => {
  // 后端"非空即封闭"会把员工没列出的组投影成 `!组`；存量员工的显式格子就是这种形状。
  // 旧实现把它当未知标签丢掉 → 用户一按保存就把"拒绝"变成"继承"，是静默放权。
  assert.deepEqual(normalizePermissionVector(["read", "!ssh"]), ["read", "!ssh"]);
  assert.deepEqual(normalizePermissionVector(["!ssh.userspace", "ssh", "read"]), ["read", "ssh", "!ssh.userspace"]);
  // 去重
  assert.deepEqual(normalizePermissionVector(["!ssh", "!ssh"]), ["!ssh"]);
  // 同一标签既给又拒 → 按拒绝落地（fail-closed），且只保留一个形态
  assert.deepEqual(normalizePermissionVector(["ssh", "!ssh"]), ["!ssh"]);
  assert.deepEqual(normalizePermissionVector(["!ssh", "ssh"]), ["!ssh"]);
  // 未知标签即便带 ! 也丢弃
  assert.deepEqual(normalizePermissionVector(["!root"]), []);
});

test("三态循环：不选 → 给 → 拒 → 不选", () => {
  assert.deepEqual(cyclePermissionVector([], "write"), ["write"]);
  assert.deepEqual(cyclePermissionVector(["write"], "write"), ["!write"]);
  assert.deepEqual(cyclePermissionVector(["!write"], "write"), []);
  // 未知 id 不动现有选择（与 toggle 同口径）
  assert.deepEqual(cyclePermissionVector(["read"], "root"), ["read"]);
  // 拒态不影响别的标签
  assert.deepEqual(cyclePermissionVector(["read", "!ssh"], "write"), ["read", "write", "!ssh"]);
});

test("读数与渲染都认得拒态", () => {
  assert.equal(permissionVectorSummary(["read", "!ssh"]), "read · 拒 ssh");
  assert.equal(permissionVectorSummary(["!ssh"]), "拒 ssh");
  const html = renderPermissionVectorHTML(["!ssh"]);
  assert.match(html, /data-perm-type="ssh"[^>]*class=|class="perm-tag is-denied"/);
  assert.match(html, /aria-pressed="true"/);
  assert.match(html, /SSH（拒）/);
  // 给态仍然是 is-on
  assert.match(renderPermissionVectorHTML(["ssh"]), /perm-tag is-on/);
});

test("全拒配方要报 assembly_nothing_allowed", () => {
  const warnings = permissionVectorLint(["!ssh", "!read"]);
  assert.ok(warnings.some(item => item.id === "assembly_nothing_allowed"), JSON.stringify(warnings));
  // 只要有一个"给"，就不报这条
  assert.ok(!permissionVectorLint(["read", "!ssh"]).some(item => item.id === "assembly_nothing_allowed"));
});

test("app.js 用三态循环接线（不是两态 toggle）", async () => {
  const app = await read("app.js");
  assert.match(app, /cyclePermissionVector,\s*\n\} from "\.\/role-settings\.js";/, "要从 role-settings 引三态循环");
  assert.match(app, /cyclePermissionVector\(permissionVectorDraft, tag\.dataset\.permType\)/, "点击要用三态循环");
  assert.doesNotMatch(app, /togglePermissionVector\(permissionVectorDraft/, "不能再走两态 toggle");
});
