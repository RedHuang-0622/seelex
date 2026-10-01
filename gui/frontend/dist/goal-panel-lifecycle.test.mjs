import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

// goal-panel-lifecycle.test.mjs 钉住「目标」面板**标签的生命周期**这一条接线。
//
// 为什么是源码级断言：app.js 是 DOM 绑定脚本，node 直接 import 会挂在 document /
// invoke 上（与 agent-team-refresh.test.mjs / chat-view-input-lock.test.mjs 同口径：
// 接线用源码级断言钉，渲染逻辑放纯件里用 node:test 真跑，见 goal-board-view.test.mjs）。
//
// 现场（2026-10-02，工作台「目标」子页）：goal 已收口归档，GOAL 徽标与
// `$goal`/`$teamwork` chips 还贴着。根因是标签判据用错了事实——skill 激活态
// （`$goal` 一召回就长期为真）不是 goal 状态（栈上还有没有 active 帧）。
// 面板与它的标签必须同归 goal 状态机（goal-board-view.js renderGoalPanel）。

const appSource = await readFile(new URL("./app.js", import.meta.url), "utf8");

test("「目标」面板接线走 renderGoalPanel（面板与标签只有一处事实）", () => {
  assert.match(appSource, /import \{[^}]*renderGoalPanel[^}]*\} from "\.\/goal-board-view\.js"/);
  assert.match(appSource, /const panel = renderGoalPanel\(\{ governance, goalText, activeSkills \}\)/);
  assert.match(appSource, /goalSection\.classList\.toggle\("hidden", panel\.hidden\)/);
});

test("标签的判据是 goal 状态机，不是 skill 激活态", () => {
  assert.ok(
    !appSource.includes("goal_skill_active"),
    "「目标」面板不得再拿 skill 激活态当标签判据：`$goal`/`$teamwork` 一召回就长期为真，目标收口后 GOAL 徽标与 chips 会一直贴着"
  );
  assert.match(appSource, /badge\.classList\.toggle\("hidden", panel\.badgeHidden\)/);
  assert.match(appSource, /badge\.title = panel\.badgeTitle/);
});

test("目标结束 → 空壳退场：面板隐藏时清正文，不留上一轮的标签与文本", () => {
  assert.match(appSource, /if \(panel\.hidden\) \{[\s\S]*?view\.innerHTML = "";[\s\S]*?return;/);
  assert.ok(
    !appSource.includes("goal-task-status"),
    "非看板面的任务状态行随面板退场（任务状态在「状态」子页的 project-status 表里）"
  );
  assert.ok(!appSource.includes("truncateGoalText"), "面板件的文本截断随壳一起退场，不留死代码");
});
