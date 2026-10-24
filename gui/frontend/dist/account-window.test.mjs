// 「每账号上下文窗口」在设置弹窗里的接线（index.html 的落点 + app.js 的读写 + 取值
// 面与后端口径一致）。
//
// 现场：窗口的优先级在后端（账号级 > seelex.yaml 全局 > 账号档），所以前端**不自己算**，
// 只渲染快照下发的生效值；挡位表也来自后端。最容易漏的三处：注册表漏登记
// （`elements["account-windows"]` 静默为 undefined）、提交失败不回滚（控件停在一个其实
// 没生效的值上）、挡位表前端另抄一份（改后端不生效）。
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { ACCOUNT_WINDOW_CUSTOM, accountWindowLabel, accountWindowOptions, normalizeAccountWindowInput } from "./account-window.js";

const read = (name) => readFile(new URL(`./${name}`, import.meta.url), "utf8");

test("取值面：短标签 / 挡位选项 / 自定义输入归一", () => {
  assert.equal(accountWindowLabel(400000), "400k");
  assert.equal(accountWindowLabel(1000000), "1M");
  assert.equal(accountWindowLabel(0), "未设置");
  assert.equal(accountWindowLabel("x"), "未设置");

  const options = accountWindowOptions([200000, 400000, 600000, 1000000]);
  assert.deepEqual(options.map(option => option.value), ["200000", "400000", "600000", "1000000", ACCOUNT_WINDOW_CUSTOM]);
  assert.equal(options[0].label, "200k");
  assert.equal(options.at(-1).label, "自定义…");
  // 脏挡位（后端给错东西）被滤掉，但「自定义」永远在。
  assert.deepEqual(accountWindowOptions([0, -1, "x", 200000]).map(option => option.value), ["200000", ACCOUNT_WINDOW_CUSTOM]);

  assert.equal(normalizeAccountWindowInput("500000"), 500000);
  assert.equal(normalizeAccountWindowInput(" 500000 "), 500000);
  for (const bad of ["", "   ", "0", "-1", "4e5", "400k", "abc"]) {
    assert.equal(normalizeAccountWindowInput(bad), null, `"${bad}" 不该被当成合法窗口`);
  }
});

test("每账号上下文窗口落在所属角色卡内，不再单开全局账户表", async () => {
  const html = await read("index.html");
  const modal = html.match(/<div id="settings-modal"[\s\S]*?\n  <\/div>/);
  assert.ok(modal, "要找得到 #settings-modal");
  const body = modal[0];
  // 2026-10-24 起账户属于角色；窗口控件由 role-permission-board 逐账号渲染。
  assert.match(body, /id="role-settings-section"/, "角色页要在");
  assert.match(body, /id="permission-role-board"/, "角色卡是唯一账户层级");
  assert.doesNotMatch(body, /id="account-windows"|id="account-windows-status"/, "不再平铺全局账户表");
  const board = await read("role-permission-board.js");
  assert.match(board, /data-role-account-window-select=/, "账号行要在所属角色卡里渲染窗口控件");
  // 优先级与来源的解说留在 app.js 的那一段注释里（弹窗只留一句必要说明）。
  const app = await read("app.js");
  assert.match(app, /limits\.context_window/, "要说明窗口的来源与优先级");
});

test("app.js 注册并接线：开弹窗拉挡位表、提交采纳生效值、失败回滚", async () => {
  const app = await read("app.js");
  assert.match(app, /"default-plugin-status", "role-settings-status", "permission-role-board"/, "角色卡与状态落点都要注册");
  assert.match(app, /from "\.\/account-window\.js"/, "取值面必须来自纯模块，而不是 app.js 里另抄一份");
  assert.match(app, /await invoke\("AccountWindowPresets"\)/, "挡位表必须由后端下发");
  assert.match(app, /syncAccountWindowReading\(\);/, "openSettings 要调它");
  assert.match(app, /invoke\("SetAccountWindow", name, window\)/, "提交要透传账号名与整数");
  assert.match(app, /elements\[id\]\?\.addEventListener\("change"/, "角色卡用 change 事件委托提交账号窗口");
  // 失败回滚：catch 分支必须重画到最后一份权威读数，并露出错误。
  const handler = app.match(/async function submitAccountWindow[\s\S]*?\n\}/);
  assert.ok(handler, "要找得到 submitAccountWindow");
  assert.match(handler[0], /catch \(error\)[\s\S]*renderPermissionRoleRows\(\)[\s\S]*showToast\(error\)/, "失败要回滚并露出来");
});

test("挡位表两端同源（backend/limit/accountwindow 是唯一事实源）", async () => {
  const go = await readFile(
    new URL("../../../backend/limit/accountwindow/accountwindow.go", import.meta.url),
    "utf8",
  );
  const presets = go.match(/func Presets\(\) \[\]int \{\s*return \[\]int\{([^}]*)\}/);
  assert.ok(presets, "要在 backend/limit/accountwindow/accountwindow.go 里找到挡位表");
  const values = presets[1].split(",").map(part => Number(part.replace(/_/g, "").trim())).filter(value => Number.isFinite(value));
  assert.deepEqual(values, [200000, 400000, 600000, 1000000], "挡位表就是这四挡（后端下发，前端不另抄）");
  assert.match(go, /func Validate\(/, "校验面归后端（判据在 accountwindow，用例在 application/core）");
});
