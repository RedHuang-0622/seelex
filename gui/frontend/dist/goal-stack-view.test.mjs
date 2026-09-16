import assert from "node:assert/strict";
import test from "node:test";

import { renderGoalFrame, renderGoalInFlight, renderGoalStack } from "./goal-stack-view.js";

// goal-stack-view.test.mjs 钉住两件事：
//   1. 有活动栈时渲染成**一张专用表格**（excel-grid 口径），一行一帧、栈底→栈顶，
//      栈下帧不能丢，栈顶标成当前目标；
//   2. ADVISOR 回合进行中的正文近端能渲染出来（渲染不及时的修复面）；
//      没有进行中正文时不多出空壳。
// 纯渲染函数：不碰 DOM，也不需要后端。

test("renderGoalStack 用专用表格逐帧表达活动栈，栈顶标为当前目标", () => {
  const html = renderGoalStack([
    { id: "g-0", title: "外层目标", status: "paused", statement: "先做外层", acceptance: ["a", "b"] },
    { id: "g-1", title: "内层目标", status: "active", statement: "再压一层", acceptance: ["c"], progress: [{ content: "跑了单测" }] }
  ]);
  assert.match(html, /data-goal-frames="2"/);
  assert.match(html, /<table class="excel-grid goal-stack-table" data-goal-stack-table data-goal-frames="2">/);
  // 表头 = 这一张表自己的列：帧/状态/目标/陈述/验收/最近进度/更新
  for (const head of ["帧", "状态", "目标", "陈述", "验收", "最近进度", "更新"]) {
    assert.match(html, new RegExp(`<th>${head}</th>`), `表头缺列：${head}`);
  }
  // 一行一帧，且顺序是栈底→栈顶（栈下帧不能丢）
  assert.equal((html.match(/<tr class="goal-frame(?: is-active)?"/g) || []).length, 2, "两帧各一行");
  assert.equal((html.match(/<tr class="goal-frame is-active"/g) || []).length, 1, "只有栈顶帧是 active");
  const outerAt = html.indexOf("外层目标");
  const innerAt = html.indexOf("内层目标");
  assert.ok(outerAt > 0 && innerAt > outerAt, "栈底帧在前、栈顶帧在后");
  assert.match(html, /data-goal-frame="1"[^>]*data-goal-status="paused"/);
  assert.match(html, /data-goal-frame="2"[^>]*data-goal-frame-active="true"/);
  assert.match(html, /验收 2 条/);
  assert.match(html, /跑了单测/);
});

test("renderGoalStack 空栈/非法输入返回空串（面板不显示空壳）", () => {
  assert.equal(renderGoalStack([]), "");
  assert.equal(renderGoalStack(null), "");
  assert.equal(renderGoalStack(undefined), "");
});

test("renderGoalFrame 输出表格行，转义内容、不把 HTML 当结构", () => {
  const html = renderGoalFrame({ id: "g-9", title: "<img src=x onerror=1>", status: "active" }, true, 3);
  assert.ok(html.startsWith("<tr class=\"goal-frame is-active\""), "帧渲染成表格行");
  assert.ok(!html.includes("<img"), "标题必须被转义");
  assert.match(html, /&lt;img/);
  assert.match(html, /class="goal-cell goal-cell-index">3</);
  // 空字段不编造内容
  assert.match(html, /class="goal-cell goal-cell-statement" title="">—</);
});

test("renderGoalInFlight 渲染进行中正文，无正文时不渲染", () => {
  const html = renderGoalInFlight({ in_flight: "正在核对 issue 清单", in_flight_chars: 42 });
  assert.match(html, /ADVISOR 评审中/);
  assert.match(html, /正在核对 issue 清单/);
  assert.equal(renderGoalInFlight({ in_flight: "" }), "");
  assert.equal(renderGoalInFlight({}), "");
  assert.equal(renderGoalInFlight(null), "");
});
