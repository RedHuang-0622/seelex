// 会话切换必须把「展开中的帧正文」与「帧正文弹框」整份清掉，而不是留着上一个会话
// 的字节（用户口径：「前端会跨会话留存压缩帧的污染而不是根据会话重读压缩帧」）。
//
// app.js 是 DOM 绑定脚本（node 里跑不起来），按仓库既有口径做**源码级断言**
// （见 chat-view-input-lock.test.mjs / agent-team-refresh.test.mjs）；渲染侧的
// (会话, ref) 判定有行为用例钉在 context-summary.test.mjs。
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const appSource = await readFile(new URL("./app.js", import.meta.url), "utf8");
const summarySource = await readFile(new URL("./context-summary.js", import.meta.url), "utf8");

test("app.js：视图会话变化时清空帧正文视图态（右栏展开 + 弹框 + 在途读取）", () => {
  const start = appSource.indexOf("if (sessionID !== lastViewSessionID)");
  assert.ok(start > 0, "找不到 onSnapshot 的会话切换分支");
  const branch = appSource.slice(start, appSource.indexOf("render(snapshot, options)", start));
  assert.match(branch, /resetCompactionViewState\(\)/, "会话切换分支必须清帧正文视图态");

  // 清的必须是全部三样：条目正文、在途 token、弹框；只清正文会让弹框继续显示
  // 上一个会话的字节，只清弹框则右栏那一行仍挂着旧正文。
  assert.match(appSource, /function resetCompactionViewState\(\) \{[\s\S]*?clearCompactionDetail\(\);[\s\S]*?compactionFrameLoadToken \+= 1;[\s\S]*?compactionFrameModal = \{ record: null, detail: null \};[\s\S]*?setModal\("compaction-frame-modal", false\);/);
});

test("app.js：展开身份是 frame_ref，不是记录数组下标", () => {
  assert.match(appSource, /if \(compactionDetail\.ref === ref\)/);
  assert.doesNotMatch(appSource, /compactionDetail\.index === index/);
  // 续读只认视图态里存的 ref：从 compactions[index] 重取会让重排后的续页接到别人尾巴上。
  assert.match(appSource, /const ref = String\(compactionDetail\.ref \|\| ""\);/);
  assert.doesNotMatch(appSource, /const ref = String\(compactions\[index\]\?\.frame_ref/);
});

test("app.js：帧正文视图态自带会话，供渲染侧拒绝跨会话正文", () => {
  assert.match(appSource, /function compactionViewSessionID\(\)/);
  assert.match(appSource, /sessionID: compactionViewSessionID\(\)/);
  assert.match(appSource, /renderContextCompactions\(list, \{\s*detail: compactionDetail, progress: compactionProgress, sessionID: compactionViewSessionID\(\)\s*\}\)/);
  assert.match(summarySource, /detailRef === frameRef/);
  assert.match(summarySource, /detailSession === sessionID/);
});
