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

test("设置弹窗里有「账号上下文窗口」一段：容器 + 读数落点 + 说明压账号档", async () => {
  const html = await read("index.html");
  const modal = html.match(/<div id="settings-modal"[\s\S]*?\n  <\/div>/);
  assert.ok(modal, "要找得到 #settings-modal");
  const body = modal[0];
  assert.match(body, /<div class="section-title">账号上下文窗口<\/div>/, "段落标题要在");
  assert.match(body, /<div id="account-windows"><\/div>/, "账号行由前端渲染（容器是空的）");
  assert.match(body, /id="account-windows-status"/, "要有一行读数/说明落点");
  assert.match(body, /limits\.context_window|账号档 max_tokens/, "要说明窗口的来源与优先级");
});

test("app.js 注册并接线：开弹窗拉挡位表、提交采纳生效值、失败回滚", async () => {
  const app = await read("app.js");
  assert.match(app, /"account-windows", "account-windows-status"/, "elements 注册表要有这两个 id（否则又是静默失效）");
  assert.match(app, /from "\.\/account-window\.js"/, "取值面必须来自纯模块，而不是 app.js 里另抄一份");
  assert.match(app, /await invoke\("AccountWindowPresets"\)/, "挡位表必须由后端下发");
  assert.match(app, /syncAccountWindowReading\(\);/, "openSettings 要调它");
  assert.match(app, /invoke\("SetAccountWindow", name, window\)/, "提交要透传账号名与整数");
  assert.match(app, /elements\["account-windows"\]\?\.addEventListener\("change"/, "change 是提交时机（行内事件委托）");
  // 失败回滚：catch 分支必须重画到最后一份权威读数，并露出错误。
  const handler = app.match(/async function submitAccountWindow[\s\S]*?\n\}/);
  assert.ok(handler, "要找得到 submitAccountWindow");
  assert.match(handler[0], /catch \(error\)[\s\S]*renderAccountWindowRows\(\)[\s\S]*showToast\(error\)/, "失败要回滚并露出来");
});

test("挡位表两端同源（seelexctx/window.go 是唯一事实源）", async () => {
  const go = await readFile(new URL("../../../seelexctx/window.go", import.meta.url), "utf8");
  const presets = go.match(/func AccountContextWindowPresets\(\) \[\]int \{\s*return \[\]int\{([^}]*)\}/);
  assert.ok(presets, "要在 seelexctx/window.go 里找到挡位表");
  const values = presets[1].split(",").map(part => Number(part.replace(/_/g, "").trim())).filter(value => Number.isFinite(value));
  assert.deepEqual(values, [200000, 400000, 600000, 1000000], "挡位表就是这四挡（后端下发，前端不另抄）");
  assert.match(go, /func ValidateAccountContextWindow\(/, "校验面归后端（见 service_account_window.go）");
});
