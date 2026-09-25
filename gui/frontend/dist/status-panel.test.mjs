// 状态子页的摆放口径（两次用户口径叠加后的现态）：
//   ① 「上下文压缩内容放到状态的概要下面」（2026-09-24）：块挂在 #status-panel
//      折叠区**里面**、紧跟概要；
//   ② 「概要有且仅有压缩栈的表格」（2026-09-26）：概要下面原来那句英文作用域说明
//      删掉（它说的两件事由项目名/根路径与状态行承载），压缩记录从卡片改成表格。
//   可见性仍由 app.js 兜住：repaintCompactions 见到本轮门禁进度就把折叠区打开
//   （只开不收）——块一挪进折叠区，"按下回车到底动没动"就不能再靠用户记得展开。
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const read = (name) => readFile(new URL(`./${name}`, import.meta.url), "utf8");

test("概要有且仅有压缩栈表格：概要标题之后紧跟压缩块，且没有别的内容块", async () => {
  const html = await read("index.html");
  const start = html.indexOf('id="status-panel"');
  assert.ok(start > 0, "状态折叠区必须存在");
  const details = html.slice(start, html.indexOf("</details>", start));
  const title = details.indexOf(">概要<");
  const compactions = details.indexOf('<div id="context-compactions"');
  assert.ok(title > 0 && compactions > 0, "概要标题与压缩块都要在折叠区里面");
  assert.ok(title < compactions, "压缩块要排在概要之后");
  // 概要区里除压缩块外不再有别的容器（重复的英文作用域说明已删）。
  assert.equal(details.indexOf("project-overview"), -1, "概要再放第二个内容块就是又一轮漂移");
  assert.equal(details.slice(title, compactions).indexOf("<div"), -1, "概要与压缩块之间不得再插内容块");
});

test("压缩块渲染的是表格（栈）而不是卡片列表", async () => {
  const summary = await read("context-summary.js");
  assert.match(summary, /class="compaction-stack"/);
  assert.match(summary, /role="columnheader"/);
  assert.doesNotMatch(summary, /<article class="context-summary-item"/);
});

test("折叠区默认收起，所以进度要靠 repaintCompactions 自动展开 状态", async () => {
  const app = await read("app.js");
  assert.match(app, /function revealStatusPanel\(\)/);
  assert.match(app, /panel\.open = true/);
  // 只在本轮门禁进度存在时打开：历次记录不触发，免得跟用户手动收起打架。
  assert.match(app, /if \(compactionProgress\) revealStatusPanel\(\);/);
});
