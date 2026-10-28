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

test("会话标题和工具摘要都单行省略，轨迹主从区可调宽", () => {
  assert.match(
    css,
    /\.session-title \.entry-name\s*\{[^}]*overflow:\s*hidden[^}]*text-overflow:\s*ellipsis[^}]*white-space:\s*nowrap/s,
    "会话列表是导航而不是正文预览：长标题必须单行省略，完整标题留给提示气泡",
  );
  assert.match(css, /\.tool-command\s*\{[^}]*white-space:\s*normal[^}]*overflow-wrap:\s*anywhere/s);
  assert.match(css, /\.tool-call-summary \.tool-command\s*\{[^}]*text-overflow:\s*ellipsis[^}]*white-space:\s*nowrap/s);
  assert.match(css, /\.conversation\s*\{[^}]*min-width:\s*0[^}]*overflow-x:\s*hidden/s);
  assert.match(css, /\.conversation\s*\{[^}]*scrollbar-gutter:\s*stable both-edges/s, "加载时滚动条出现不能让整列内容横向跳位");
  assert.match(css, /\.trajectory-main\s*\{[^}]*grid-template-columns:\s*minmax\(220px,\s*var\(--trajectory-results-width,\s*58%\)\)\s*7px\s*minmax\(220px,\s*1fr\)/s);
});

test("对话正文按容器宽度自动换行，不再继承 Pico busy 的 nowrap", () => {
  assert.match(css, /\.message,\s*\.tool-run\s*\{[^}]*width:\s*min\(860px,\s*100%\)[^}]*max-width:\s*100%[^}]*min-width:\s*0/s);
  assert.match(css, /\.message-body\s*\{[^}]*max-width:\s*100%[^}]*white-space:\s*normal[^}]*overflow-wrap:\s*anywhere[^}]*word-break:\s*break-word/s);
  assert.match(css, /\.conversation\[aria-busy="true"\][\s\S]{0,220}white-space:\s*normal/s);
  assert.match(css, /\.conversation\[aria-busy="true"\]::before[\s\S]{0,500}display:\s*none/s);
});

test("Enter 提交先收起浮层再等待 RPC，避免输入瞬间错位或越界", () => {
  const handler = app.slice(
    app.indexOf('elements.composer.addEventListener("submit"'),
    app.indexOf('elements.prompt.addEventListener("keydown"')
  );
  assert.ok(handler.includes("hideInlineSuggestions();"));
  assert.ok(handler.indexOf("hideInlineSuggestions();") < handler.indexOf("await invoke(plan.rpc"));
});
