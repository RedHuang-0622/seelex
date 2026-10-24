import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const read = name => readFile(new URL(`./${name}`, import.meta.url), "utf8");

test("项目工作环境支持本地与 SSH 两种执行面", async () => {
  const html = await read("index.html");
  assert.match(html, /主机留空即本地环境/);
  assert.match(html, /placeholder="主机（留空 = 本地）"/);

  const app = await read("app.js");
  assert.match(app, /const mode = row\.host \? "ssh" : "local"/);
  assert.match(app, /已保存本地环境/);
  assert.doesNotMatch(app, /空端点已按删除处理/, "清空主机只切换为本地环境，删除必须走显式按钮");
});

test("工作环境固定在会话层，绑定后 UI 不允许 agent 或用户切换", async () => {
  const html = await read("index.html");
  const app = await read("app.js");
  assert.match(html, /id="session-environment-button"/);
  assert.match(html, /id="session-environment-modal"/);
  assert.match(html, /首次固定后不可切换/);
  assert.match(app, /invoke\("SetSessionSSHEnv", env\)/);
  assert.match(app, /select\.disabled = fixed/);
  assert.match(app, /agent 与工具入参都不能切换/);
  assert.doesNotMatch(app, /elements\["session-environment-modal"\][\s\S]{0,200}settings-modal/, "会话环境不是全局设置项");
});
