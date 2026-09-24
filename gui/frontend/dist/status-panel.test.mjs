// 状态子页的摆放口径（用户口径，2026-09-24 反转一次）：
//   「上下文压缩内容需要放到状态的概要下面」——块从 #status-panel 折叠区**外面**
//   挪回**里面**、紧跟概要；"按下回车到底动没动"这条可见性改由 app.js 兜住
//   （repaintCompactions 见到本轮门禁进度就把折叠区打开，只开不收）。
//
// 这两条一起才是完整口径：只挪进去 = 折叠态下进度条与记录又整块消失（旧问题），
// 只自动展开不挪 = 又回到用户点名的"没放在概要下面"。所以这里把两条一起钉住。
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const read = (name) => readFile(new URL(`./${name}`, import.meta.url), "utf8");

test("上下文压缩块挂在 状态/概要 折叠区里面、紧随概要之后", async () => {
  const html = await read("index.html");
  const start = html.indexOf('id="status-panel"');
  assert.ok(start > 0, "状态折叠区必须存在");
  const details = html.slice(start, html.indexOf("</details>", start));
  const overview = details.indexOf('id="project-overview"');
  const compactions = details.indexOf('id="context-compactions"');
  assert.ok(overview > 0 && compactions > 0, "概要与上下文压缩块都要在折叠区里面");
  assert.ok(overview < compactions, "上下文压缩块要排在概要之后");
});

test("折叠区默认收起，所以进度要靠 repaintCompactions 自动展开 状态", async () => {
  const app = await read("app.js");
  assert.match(app, /function revealStatusPanel\(\)/);
  assert.match(app, /panel\.open = true/);
  // 只在本轮门禁进度存在时打开：历次记录不触发，免得跟用户手动收起打架。
  assert.match(app, /if \(compactionProgress\) revealStatusPanel\(\);/);
});
