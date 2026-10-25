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
  permissionRoleAccountOptionsHTML,
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
    ["read", "write", "computeruse", "control", "admin", "ssh"],
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
  assert.match(html, /data-perm-type="ssh"/);
  // 七枚都在（不多不少）。
  assert.equal((html.match(/data-perm-type=/g) || []).length, PERMISSION_VECTOR_TAGS.length);
  assert.doesNotMatch(renderPermissionVectorHTML(["nope"]), /data-perm-type="nope"/);
});

test("两个下拉：空清单给一句可读的占位，当前项标（当前）", () => {
  assert.match(roleAccountOptionsHTML([], "x"), /读不到账号列表/);
  const accounts = [
    { name: "main", provider: "deepseek", model: "v3", api_key_set: true },
    { name: "cheap", provider: "deepseek", model: "lite", disabled: true },
    { name: "fallback", provider: "openai", model: "gpt-4o", api_key_set: false, fallback: true },
  ];
  const html = roleAccountOptionsHTML(accounts, "main");
  assert.match(html, /<option value="main" selected>main · deepseek \/ v3（当前）<\/option>/);
  assert.match(html, /<option value="cheap" disabled>cheap · deepseek \/ lite<\/option>/);
  assert.match(html, /fallback · openai \/ gpt-4o · 未配置密钥 \/ 内置兜底<\/option>/);

  assert.match(rolePluginOptionsHTML([], "x"), /读不到插件列表/);
  const plugins = [{ name: "core" }, { name: "goal" }];
  assert.match(rolePluginOptionsHTML(plugins, "goal"), /<option value="goal" selected>goal（当前）<\/option>/);
});

test("角色账号下拉只列该角色账号，并显示密钥与 fallback 状态", () => {
  const html = permissionRoleAccountOptionsHTML(
    ["teammate-1", "teammate-2"],
    "teammate-2",
    [
      { name: "teammate-1", provider: "deepseek", model: "v3", api_key_set: true },
      { name: "teammate-2", provider: "openai", model: "gpt-4o", api_key_set: false, fallback: true },
      { name: "agent-1", provider: "openai", model: "other", api_key_set: true },
    ],
    "teammate-2",
  );
  assert.match(html, /自动（按角色账号池）/);
  assert.match(html, /teammate-1 · deepseek \/ v3/);
  assert.match(html, /teammate-2 · openai \/ gpt-4o · 未配置密钥 \/ 内置兜底（当前）/);
  assert.doesNotMatch(html, /agent-1/, "不得把别的角色账号放进下拉");
});

test("设置弹窗：角色页用角色卡承载权限与账户，不再平铺会话向量", async () => {
  const html = await read("index.html");
  const modal = html.match(/<div id="settings-modal"[\s\S]*?\n  <\/div>/);
  assert.ok(modal, "要找得到 #settings-modal");
  const body = modal[0];

  // ① 存储策略区连同按钮、读数一起删干净（后端类型/路径/DSN/测试/保存）。
  for (const gone of ["storage-backend", "storage-path", "storage-dsn", "storage-test", "storage-save", "storage-status"]) {
    assert.doesNotMatch(body, new RegExp(`id="${gone}"`), `${gone} 必须从设置页消失`);
  }
  assert.doesNotMatch(body, /存储形式|保存并切换|测试连接/, "存储策略的文案也要一起删");

  // ② 角色页只保留角色卡；账号与上下文窗口都在所属角色卡内。
  assert.match(body, /id="role-settings-section"/, "角色设置要自成一段");
  assert.match(body, /<div id="permission-role-board" class="role-permission-board"><\/div>/);
  assert.doesNotMatch(body, /id="permission-role"/, "不再用单角色选择器承载全部角色");
  assert.doesNotMatch(body, /id="account-windows"|id="account-windows-status"/, "账户不得在角色页再平铺一张全局表");
  assert.match(body, /id="role-settings-status"/, "角色卡内的提交结果要有一行落点");
  assert.match(body, /<select id="role-default-plugin"><\/select>/, "默认 plugin 下拉移到应用页");
  assert.match(body, /id="default-plugin-status"/, "应用默认值要有一行落点");

  // 角色页只承载层级；应用级默认值不能回流到角色页。
  const section = body.match(/data-settings-page-panel="roles"[\s\S]*?data-settings-page-panel="application"/);
  assert.ok(section, "要找得到角色与权限页面");
  assert.match(section[0], /id="permission-role-board"/);
  assert.doesNotMatch(section[0], /id="role-default-plugin"|id="account-windows"/);

  // ④ 大段解说文案删了：每一段只留一句话（< 120 字）。
  for (const note of body.matchAll(/<p class="settings-note">([\s\S]*?)<\/p>/g)) {
    const text = note[1].replace(/<[^>]+>/g, "").trim();
    assert.ok(text.length > 0 && text.length <= 120, `这一段说明太长（${text.length} 字）：${text.slice(0, 40)}…`);
  }
});

test("app.js 接线：角色卡、权限组合、账号与余额", async () => {
  const app = await read("app.js");
  for (const id of ["permission-role-board", "role-default-plugin", "default-plugin-status", "role-settings-status"]) {
    assert.match(app, new RegExp(`"${id}"`), `${id} 必须注册进 elements`);
  }
  assert.doesNotMatch(app, /session-role-board/, "会话权限已并入同一角色卡，不再保留第二个宿主席位");
  assert.match(app, /from "\.\/role-settings\.js"/, "取值面必须来自纯模块，而不是 app.js 里另抄一份");
  assert.match(app, /from "\.\/role-permission-board\.js"/, "角色卡渲染必须来自独立模块");
  assert.match(app, /await syncRoleSettings\(\);/, "openSettings 要调它");
  assert.match(app, /SetRoleAccount/);
  assert.match(app, /SetRoleDefaultVector/);
  assert.match(app, /SetRoleSessionPermission/);
  // 上下文窗口：仍走 AccountWindowPresets / SetAccountWindow（判据在后端）。
  assert.match(app, /await invoke\("AccountWindowPresets"\)/);
  assert.match(app, /invoke\("SetAccountWindow", name, window\)/);
  // 权限向量：整组提交。
  assert.match(app, /invoke\("SetPermissionVector", wanted\)/);
  // 默认 plugin：名字透传。
  assert.match(app, /invoke\("SetDefaultPlugin", name\)/);

  // 主代理底部芯片仍保留 root 向量提交；角色卡走自己的作用域提交。
  const submit = app.match(/async function submitPermissionVector\(\)[\s\S]*?\n\}/);
  assert.ok(submit, "要找得到 submitPermissionVector");
  assert.match(submit[0], /catch \(error\) \{\s*permissionVectorApplied = false;/, "失败要落到未生效");
  assert.match(app, /permissionVectorLint\(normalized\)/, "角色卡提交要带上 lint 提示");

  // 存储策略的读写整条不能再有残留（否则删了 DOM 也会在打开时抛）。
  for (const gone of ["SessionStorageConfig", "TestSessionStorage", "ConfigureSessionStorage", "storage-status"]) {
    assert.doesNotMatch(app, new RegExp(gone), `${gone} 的 JS 逻辑必须一并删掉`);
  }
});

test("显式拒绝（!tag）必须原样保留：丢掉它就等于把「明确不给」擦成「继承」", () => {
  // 后端"非空即封闭"会把员工没列出的组投影成 `!组`；存量员工的显式格子就是这种形状。
  // 旧实现把它当未知标签丢掉 → 用户一按保存就把"拒绝"变成"继承"，是静默放权。
  assert.deepEqual(normalizePermissionVector(["read", "!ssh"]), ["read", "!ssh"]);
  assert.deepEqual(normalizePermissionVector(["!ssh", "ssh", "read"]), ["read", "!ssh"]);
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

test("底部芯片是主代理权限向量，不再是档位", async () => {
  const app = await read("app.js");
  // 芯片内容来自向量读数（permissionVectorSummary），不再读档位目录
  assert.match(app, /function renderPermissionVectorChip\(\)/, "要有芯片的向量渲染函数");
  assert.match(app, /permissionVectorSummary\(permissionVectorCommitted\)/, "芯片读数来自后端回执的向量");
  // 档位那套入口必须从 app.js 消失：芯片值 / 切换路径 / 弹窗列表都不能再挂档位
  for (const gone of ["permissionTierChipForRuntime", "permissionTierMenuItems", "permissionTierOptions", "SetPermissionTier", "permissionChipInner", "applyPermissionTier"]) {
    assert.doesNotMatch(app, new RegExp(gone), `${gone} 不能还留在 app.js（档位入口已由向量接管）`);
  }
  // 下拉里点标签 = 三态循环 + 整组提交（与设置面板同一条路径）
  assert.match(app, /perm-menu\"\]\.addEventListener\(\"click\"[\s\S]*?cyclePermissionVector\(permissionVectorDraft/, "下拉点击要走三态循环");
  assert.match(app, /perm-menu\"\]\.addEventListener\(\"click\"[\s\S]*?await submitPermissionVector\(\)/, "下拉点击要整组提交");
  // 运行状态弹窗里只能是只读镜像（inert），不能变成第二个点了没反应的入口
  assert.match(app, /host\.innerHTML = `<div class="perm-tag-row" inert>/, "弹窗里是 inert 只读镜像");
});

test("权限检查器里三个入口共用一份真值", async () => {
  const app = await read("app.js");
  // 芯片 / 下拉 / 设置面板都读 permissionVectorCommitted，提交后三处一起重绘
  const submit = app.match(/async function submitPermissionVector\(\)[\s\S]*?\n\}/);
  assert.ok(submit, "要找得到 submitPermissionVector");
  assert.match(submit[0], /renderPermissionVectorChip\(\)/, "提交后要重绘芯片");
  assert.match(submit[0], /renderPermissionMenu\(/, "提交后要重绘下拉");
  assert.doesNotMatch(submit[0], /renderRolePermissionVector\(\)/, "旧单角色设置面板已退场");
});

test("上下文窗口改完要读回来（send/ack），不能拿请求值当结果", async () => {
  const app = await read("app.js");
  const submit = app.match(/async function submitAccountWindow\(name, window\)[\s\S]*?\n\}/);
  assert.ok(submit, "要找得到 submitAccountWindow");
  const body = submit[0];
  // 1. 写：仍走 SetAccountWindow 并采纳返回值
  assert.match(body, /await invoke\("SetAccountWindow", name, window\)/, "提交仍走 SetAccountWindow");
  // 2. 拉：写完之后必须再拉一次快照（ack 的"再一次查看"）
  assert.match(body, /await refresh\(\{ scroll: false \}\)/, "改完要再拉一次快照");
  // 3. 核对：以读回来的值为准，并显式说读到什么
  assert.match(body, /accountWindowFromSnapshot\(name\)/, "要以读回来的快照值为准");
  assert.match(body, /读回生效窗口/, "要把读回的值说出来");
  // 渲染必须优先用 ack 读数，不能被旧快照覆盖（这正是"改了没反应"的根因）
  assert.match(app, /windowReadings: accountWindowsCommitted/, "角色卡渲染必须采纳 ack 读数");
  assert.match(app, /windowPresets: accountWindowPresets/, "挡位仍由后端下发");
  assert.doesNotMatch(app, /function renderAccountWindowRows\(\)/, "账户运行参数不得再单独平铺");
});

test("设置只编辑全局角色权限，本会话覆盖在会话入口", async () => {
  const app = await read("app.js");
  const index = await read("index.html");
  assert.match(index, /id="permission-role-board"/, "全局角色卡要在");
  assert.doesNotMatch(index, /id="session-role-board"/, "不再保留独立会话权限板");
  assert.doesNotMatch(index, /data-settings-page="session"/, "不再保留会话权限一级导航");
  assert.match(index, /id="session-permission-button"/, "会话区要有权限入口");
  assert.match(index, /id="session-permission-board"/, "会话权限弹窗要有落点");
  assert.match(app, /await invoke\("PermissionRoles"\)/, "角色名录要向桥要");
  assert.match(app, /mode: "default"[\s\S]{0,180}accounts/, "设置页只按默认作用域渲染");
  assert.match(app, /mode: "session"[\s\S]{0,180}showAccounts: false/, "会话弹窗只渲染权限，不重复账户");
  assert.match(app, /openSessionPermissions/, "会话入口要打开会话权限弹窗");
  assert.match(app, /const method = mode === "session" \? "SetRoleSessionPermission" : "SetRoleDefaultVector"/, "提交按作用域选择桥面方法");
  assert.doesNotMatch(app, /SetRoleDefaultVector", permissionRoleSelected/, "单角色选择器已经退场");
  assert.match(app, /SetRoleSessionPermission/, "会话覆盖走独立提交路径");
  assert.match(app, /SetRoleInheritance/, "继承 chip 要写回后端");
  assert.match(app, /SetRoleSessionSSHUserSpaces/, "会话 SSH 路径覆盖要写回后端");
  assert.match(app, /SetRoleSSHUserSpaces/, "角色默认 SSH 路径要写回后端");
  assert.match(app, /mode === "session" \? "SetRoleSessionSSHUserSpaces" : "SetRoleSSHUserSpaces"/);
  assert.match(app, /mode === "session" \? row\?\.session_ssh_user_spaces : row\?\.default_ssh_user_spaces/);
  assert.doesNotMatch(app, /renderAccountBalanceRows\(\)/, "已退场的全局余额表不得留下悬空调用");
});

test("会话改名：菜单入口 + 就地输入 + 走 SetSessionMeta 的别名", async () => {
  const app = await read("app.js");
  const css = await read("styles.css");
  assert.match(app, /data-rename-session=/, "菜单里要有重命名入口");
  assert.match(app, /data\.renameSession\)\s*\{\s*beginSessionRename/, "点击要分派到 beginSessionRename");
  const fn = app.match(/function beginSessionRename\(sessionID\)[\s\S]*?\n\}/);
  assert.ok(fn, "要找得到 beginSessionRename");
  // 走既有元数据接口（置顶的 pinned/sort_order 必须原样带回去，否则改名会顺手把置顶弄丢）
  assert.match(fn[0], /invoke\("SetSessionMeta", sessionID, Boolean\(meta\.pinned\), alias, meta\.sort_order \|\| 0\)/, "改名走 SetSessionMeta 且不动置顶/排序");
  assert.match(fn[0], /await refresh\(\{ scroll: false \}\)/, "改完要刷新目录");
  // 留空 = 清别名（展示回落默认标题）
  assert.match(fn[0], /留空 = 用默认标题/, "要说明留空的含义");
  // Esc / 失焦取消，Enter 提交
  assert.match(fn[0], /event\.key === "Escape"[\s\S]*finish\(false\)/, "Esc 取消");
  assert.match(fn[0], /event\.key === "Enter"[\s\S]*finish\(true\)/, "Enter 提交");
  assert.match(css, /\.session-rename-input/, "要有就地输入的样式");
});

test("SSH 环境：项目表只在项目设置维护，应用设置不再包含 SSH", async () => {
  const app = await read("app.js");
  const index = await read("index.html");
  assert.match(index, /id="ssh-env-list"/, "要有项目环境表");
  assert.match(index, /id="ssh-env-save"/, "要有保存环境");
  assert.doesNotMatch(index.match(/<div id="settings-modal"[\s\S]*?\n  <\/div>/)?.[0] || '', /session-ssh-allowed|SSH 环境/, "应用设置不含 SSH");
  // 读数来自后端，前端不自己造环境名
  assert.match(app, /await invoke\("WorkspaceEnvironments", currentWorkspaceID\(\)\)/, "环境表要按当前项目 ID 向桥要");
  assert.match(app, /await invoke\("SetWorkspaceEnvironment", currentWorkspaceID\(\), name, change\)/, "保存走项目作用域 SetWorkspaceEnvironment");
  assert.match(app, /await invoke\("DeleteWorkspaceEnvironment", currentWorkspaceID\(\), name\)/, "删除走项目作用域 DeleteWorkspaceEnvironment");
  // 口令三态：留空 = 不改（null），不是空串
  assert.match(app, /Password: passwordText === "" \? null : passwordText/, "口令留空必须是 null（不改），空串才表示清除");
  assert.match(app, /async function openProjectSettings\(\)[\s\S]*?await syncWorkspaceEnvironments\(\)/, "项目设置要拉项目连接表");
});
