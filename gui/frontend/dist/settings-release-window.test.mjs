// 「会话驻留窗口」在设置弹窗里的接线（index.html 的落点 + app.js 的读写 + 取值面与
// 后端口径一致）。
//
// 现场：这个窗口是**进程级**读数（不像 composer 两条拉条那样挂在按会话的 runtime
// 投影上），所以它的读法姿势与 SessionStorageConfig 一致——弹窗打开时拉一次
// `SessionReleaseWindow`，提交走 `SetSessionReleaseWindow` 并**采纳后端返回的生效
// 值**。三件事最容易漏：注册表漏登记（`elements["release-window"]` 静默为 undefined）、
// 提交失败不回滚（控件停在一个其实没生效的值上）、前端边界与后端边界漂移。
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import { RELEASE_WINDOW_MAX, RELEASE_WINDOW_MIN } from "./release-window.js";

const read = (name) => readFile(new URL(`./${name}`, import.meta.url), "utf8");

test("设置弹窗里有「会话驻留」一段：下拉框由前端填，另有一行读数说明", async () => {
  const html = await read("index.html");
  const modal = html.match(/<div id="settings-modal"[\s\S]*?\n  <\/div>/);
  assert.ok(modal, "要找得到 #settings-modal");
  const body = modal[0];
  assert.match(body, /<div class="section-title">会话驻留<\/div>/, "段落标题要在");
  assert.match(body, /<select id="release-window"><\/select>/, "下拉框是空的（选项由 release-window.js 的选项表填）");
  assert.match(body, /id="release-window-status"/, "要有一行读数/说明落点");
  assert.match(body, /limits\.session_release_window|会话驻留|受保护集/, "段落要说明它改的是什么");
});

test("app.js 注册并接线：开弹窗读一次、change 提交并采纳生效值、失败回滚", async () => {
  const app = await read("app.js");
  assert.match(app, /"release-window", "release-window-status"/, "elements 注册表要有这两个 id（否则又是静默失效）");
  assert.match(app, /from "\.\/release-window\.js"/, "取值面必须来自纯模块，而不是 app.js 里另抄一份");
  assert.match(app, /await invoke\("SessionReleaseWindow"\)/, "弹窗打开时要拉一次权威读数");
  assert.match(app, /syncReleaseWindowReading\(\);/, "openSettings 要调它");
  assert.match(app, /invoke\("SetSessionReleaseWindow", requested\)/, "提交要透传整数");
  assert.match(app, /elements\["release-window"\]\?\.addEventListener\("change"/, "change 是唯一的提交时机");
  // 失败回滚：catch 分支必须把控件对齐到最后一份权威值，并弹错误。
  const handler = app.match(/elements\["release-window"\]\?\.addEventListener\("change",[\s\S]*?\n\}\);/);
  assert.ok(handler, "要找得到 change 处理器");
  assert.match(handler[0], /catch \(error\)[\s\S]*syncReleaseWindowSelect\(releaseWindowCommitted\)/, "失败要回滚到最后一份权威值");
  assert.match(handler[0], /showToast\(error\)/, "失败要露出来（静默失败会让用户以为改成了）");
  assert.match(handler[0], /normalizeReleaseWindow\(await invoke\("SetSessionReleaseWindow"/, "要采纳后端返回的生效值");
});

test("前端边界与后端边界一致（kernel/limit 的同一组常量）", async () => {
  const go = await readFile(new URL("../../../kernel/limit/limits.go", import.meta.url), "utf8");
  const goMin = go.match(/SessionReleaseWindowMin = (\d+)/);
  const goMax = go.match(/SessionReleaseWindowMax = (\d+)/);
  assert.ok(goMin && goMax, "要在 kernel/limit/limits.go 里找到窗口边界常量");
  assert.equal(RELEASE_WINDOW_MIN, Number(goMin[1]), "下界两份口径必须一致");
  assert.equal(RELEASE_WINDOW_MAX, Number(goMax[1]), "上界两份口径必须一致");
});
