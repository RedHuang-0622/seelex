import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const css = await readFile(new URL("./styles.css", import.meta.url), "utf8");
const app = await readFile(new URL("./app.js", import.meta.url), "utf8");

test("用户消息操作键紧凑并跟随气泡集群，不撑出对话列", () => {
  assert.match(css, /\.message-user-cluster\s*\{[^}]*width:\s*fit-content[^}]*max-width:\s*min\(680px,\s*92%\)[^}]*margin-left:\s*auto/s);
  assert.match(css, /\.message-user-cluster \.message-body\s*\{[^}]*width:\s*100%[^}]*max-width:\s*100%/s);
  assert.match(css, /\.message-action\s*\{[^}]*min-height:\s*20px[^}]*padding:\s*1px 5px/s);
  assert.match(css, /\.message-action \.icon\s*\{[^}]*width:\s*12px[^}]*height:\s*12px/s);
});

test("会话与工具条目允许自然换行，超长内容不撑破对话列", () => {
  assert.match(css, /\.session-title \.entry-name\s*\{[^}]*white-space:\s*normal[^}]*overflow-wrap:\s*anywhere/s);
  assert.match(css, /\.tool-command\s*\{[^}]*white-space:\s*normal[^}]*overflow-wrap:\s*anywhere/s);
  assert.match(css, /\.conversation\s*\{[^}]*min-width:\s*0[^}]*overflow-x:\s*hidden/s);
  assert.match(css, /\.trajectory-main\s*\{[^}]*grid-template-columns:\s*minmax\(0,\s*1\.15fr\)\s*minmax\(0,\s*\.85fr\)/s);
});

test("Enter 提交先收起浮层再等待 RPC，避免输入瞬间错位或越界", () => {
  const handler = app.slice(
    app.indexOf('elements.composer.addEventListener("submit"'),
    app.indexOf('elements.prompt.addEventListener("keydown"')
  );
  assert.ok(handler.includes("hideInlineSuggestions();"));
  assert.ok(handler.indexOf("hideInlineSuggestions();") < handler.indexOf("await invoke(plan.rpc"));
});
