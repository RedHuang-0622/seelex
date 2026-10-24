// account-admin-wiring.test.mjs — app.js 一侧的接线断言（结构级，不起 GUI）。
//
// 与 role-settings.test.mjs 同一姿势：模块里的判据由单测钉住，**"有没有接上"**只能在
// 源码里看。这里钉的是这两条链路最容易在改动里掉的那几根线：
//   · 词表/挡位从宿主读（不写死一份）；
//   · 添加成功要拉新快照（否则账号列表与余额行看不到新账号）；
//   · 添加失败要显示后端原话（"已写入账号档，重启后生效"这类语义不能被吞成"失败"）；
//   · 余额按**账号名**查，且读数按名字缓存（重画不冲掉刚查到的值）。
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const read = name => readFile(new URL(`./${name}`, import.meta.url), "utf8");

test("添加账户：词表从宿主读，不写死角色清单", async () => {
  const app = await read("app.js");
  assert.match(app, /invoke\("AccountRoles"\)/);
  // 反例：把四个角色写死在 app.js 里（那会在后端加角色时悄悄失配）。
  assert.doesNotMatch(app, /\["agent",\s*"subagent",\s*"goalplan",\s*"teammate"\]/);
});

test("添加账户：提交走 AddAccount，成功后拉新快照", async () => {
  const app = await read("app.js");
  assert.match(app, /invoke\("AddAccount", parsed\.draft\)/);
  // 成功分支里必须有 refresh(...)：热注册后账号池多了一条，界面只有靠新快照才看得见。
  const success = app.match(/const result = await invoke\("AddAccount", parsed\.draft\);[\s\S]{0,600}/);
  assert.ok(success, "找不到 AddAccount 的成功分支");
  assert.match(success[0], /await refresh\(/);
  // 密钥不能留在输入框里。
  assert.match(success[0], /account-add-api-key"\]\.value = ""/);
});

test("添加账户：失败显示后端原话（不把「已落盘、重启生效」说成失败）", async () => {
  const app = await read("app.js");
  assert.match(app, /添加失败：\$\{error\?\.message \|\| error\}/);
});

test("余额：按账号名查（不是「当前选中账号」），读数按名字缓存后重画", async () => {
  const app = await read("app.js");
  const handler = app.match(/async function queryAccountBalance\(name\)[\s\S]*?\n}/);
  assert.ok(handler, "找不到 queryAccountBalance");
  assert.match(handler[0], /invoke\("AccountBalance", name\)/);
  // 成功与失败分别记进 accountBalances（失败不当成「余额为 0」）。
  assert.match(handler[0], /accountBalances\.set\(name, \{ balance \}\)/);
  assert.match(handler[0], /accountBalances\.set\(name, \{ error:/);
});

test("余额：每个账号一行，行里带查看按钮与读数落点", async () => {
  const board = await read("role-permission-board.js");
  assert.match(board, /balanceReadingText\(balances instanceof Map/);
  assert.match(board, /data-role-account-balance="\$\{escapeHtml\(name\)\}"/);
  assert.match(board, /role-account-credit/);
});

test("设置弹窗打开会补词表；快照刷新会重画角色账户与余额", async () => {
  const app = await read("app.js");
  assert.match(app, /await syncAccountAdminForm\(\);/);
  const renderAccounts = app.match(/function renderAccounts\(runtime\)[\s\S]*?\n}/);
  assert.ok(renderAccounts, "找不到 renderAccounts");
  assert.match(renderAccounts[0], /renderPermissionRoleRows\(\);/);
});

test("账户增删改查都在所属角色卡下接线", async () => {
  const app = await read("app.js");
  const board = await read("role-permission-board.js");
  assert.match(board, /data-role-account-edit=/);
  assert.match(board, /data-role-account-delete=/);
  assert.match(app, /invoke\("AccountDetails", name\)/);
  assert.match(app, /invoke\("UpdateAccount", \{/);
  assert.match(app, /invoke\("DeleteAccount", name\)/);
  assert.match(app, /editingAccountName/);
  assert.match(app, /api_key: String\(elements\["account-add-api-key"\]\?\.value \|\| ""\)/);
});
