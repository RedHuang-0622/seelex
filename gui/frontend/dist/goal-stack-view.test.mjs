import assert from "node:assert/strict";
import test from "node:test";

import { renderGoalFrame, renderGoalInFlight, renderGoalStack, renderGoalSteps } from "./goal-stack-view.js";

// goal-stack-view.test.mjs 钉住三件事：
//   1. 有活动栈时渲染成**一张专用表格**（excel-grid 口径），一行一帧、栈底→栈顶，
//      栈下帧不能丢，栈顶标成当前目标；
//   2. ADVISOR 回合进行中的正文近端能渲染出来（渲染不及时的修复面）；
//      没有进行中正文时不多出空壳；
//   3. ADVISOR **评审过程**（工具步骤）能渲染出来——工具调用一行、返回一行，
//      出错高亮；没有步骤时不渲染空壳。
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

test("renderGoalSteps 渲染评审过程：工具调用与返回各一行，出错高亮", () => {
  const html = renderGoalSteps({
    peer_state: "evaluating",
    round_steps: [
      { kind: "tool", name: "read_file", args: '{"path":"docs/x.md"}' },
      { kind: "tool_result", name: "read_file", result: "命中 3 处" },
      { kind: "tool", name: "bash", args: "go test" },
      { kind: "tool_result", name: "bash", err: "退出码 1" }
    ]
  });
  assert.match(html, /data-goal-steps="4"/);
  assert.match(html, /data-goal-steps-running="1"/);
  assert.match(html, /评审过程（进行中）/);
  assert.match(html, /▸ read_file/);
  assert.match(html, /▸ bash/);
  assert.match(html, /↳ 命中 3 处/);
  assert.match(html, /is-error/);
  assert.match(html, /错误: 退出码 1/);
  // 参数与结果都进 title（悬停可读全量），正文里截断
  assert.match(html, /title="\{&quot;path&quot;:&quot;docs\/x\.md&quot;\}"/);
});

test("renderGoalSteps 回合结束后标为「最近一轮」，无步骤时不渲染", () => {
  const html = renderGoalSteps({ peer_state: "advisory_pending", round_steps: [{ kind: "tool", name: "glob" }] });
  assert.match(html, /评审过程（最近一轮）/);
  assert.match(html, /data-goal-steps-running="0"/);
  assert.equal(renderGoalSteps({ round_steps: [] }), "");
  assert.equal(renderGoalSteps({}), "");
  assert.equal(renderGoalSteps(null), "");
});

test("renderGoalSteps 转义工具名与参数，不把内容当结构", () => {
  const html = renderGoalSteps({
    round_steps: [{ kind: "tool", name: "<img src=x onerror=1>", args: '"<b>"' }]
  });
  assert.ok(!html.includes("<img"), "工具名必须被转义");
  assert.ok(!html.includes("<b>"), "参数必须被转义");
});
