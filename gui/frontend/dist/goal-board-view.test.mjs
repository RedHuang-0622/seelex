import assert from "node:assert/strict";
import test from "node:test";

import { activeGoalFrame, goalActiveSeq, renderGoalBoard, renderGoalDetail, renderGoalFrameDetail } from "./goal-board-view.js";

// goal-board-view.test.mjs 钉住四件事：
//   1. 看板：大的 active seq 取自 goal 记录自己的序号（g-<n>），小字那一行是最近
//      一次用户输入；没有 active 帧（目标结束）→ 空串，不留空壳；
//   2. active seq 不是打点条数，也不是栈位置——id 解析不出来才回退位置；
//   3. 详情：属性表逐项列出全部字段（正文/完成条件/非目标范围/**完整**打点流水/
//      时间戳），多帧逐节，栈顶标"当前目标"；
//   4. 纯渲染：内容一律转义，不把目标文本当结构。
// 纯渲染函数：不碰 DOM，也不需要后端。

const GOVERNANCE = {
  active: true,
  goal_id: "g-3",
  title: "把 goal 看板搬到工作台",
  status: "active",
  stack: [
    { id: "g-2", title: "外层目标", status: "paused", statement: "先做外层", acceptance: ["a"], active: false },
    {
      id: "g-3", title: "把 goal 看板搬到工作台", status: "active", active: true,
      statement: "看板出到工作台的「目标」子页", acceptance: ["看板可见", "点开有详情"],
      out_of_scope: ["不做动画"], created_at: 1700000000, updated_at: 1700000123,
      progress: [{ at: 1700000100, kind: "milestone", content: "最近一条打点" }],
      progress_all: [
        { at: 1700000001, kind: "milestone", content: "起手" },
        { at: 1700000050, kind: "finding", content: "发现投影缺字段" },
        { at: 1700000100, kind: "decision", content: "最近一条打点" }
      ]
    }
  ]
};

test("renderGoalBoard 上面是大 active seq，下面是最近一次输入的小字", () => {
  const html = renderGoalBoard(GOVERNANCE, "把 goal 看板搬到工作台，点开能看详情");
  assert.match(html, /data-goal-board data-goal-seq="3"/);
  assert.match(html, /class="goal-board-seq-num">3</);
  assert.match(html, /active seq/);
  assert.match(html, /class="goal-board-title"[^>]*>把 goal 看板搬到工作台</);
  assert.match(html, /打点 3 条/);
  assert.match(html, /data-goal-board-open/);
  // 小字那一行 = 最近一次用户输入
  assert.match(html, /class="goal-board-task"[^>]*>把 goal 看板搬到工作台，点开能看详情</);
});

test("renderGoalBoard 目标结束（无 active 帧）时没有看板", () => {
  assert.equal(renderGoalBoard({ active: false, stack: [] }, "某条输入"), "");
  assert.equal(renderGoalBoard(null, "某条输入"), "");
  assert.equal(renderGoalBoard({ active: true }, "某条输入"), "");
});

test("renderGoalBoard 没有用户输入时只显示看板本身，不编造小字", () => {
  const html = renderGoalBoard(GOVERNANCE, "");
  assert.ok(!html.includes("goal-board-task"), "没有输入就不留小字壳");
  assert.match(html, /goal-board-seq-num">3</);
});

test("goalActiveSeq 取 goal 记录自己的序号，不是打点条数也不是栈位置", () => {
  const frame = GOVERNANCE.stack[1];
  assert.equal(goalActiveSeq(frame, 1), 3, "g-3 → 3（打点有 3 条只是巧合）");
  assert.equal(goalActiveSeq({ id: "g-11" }, 0), 11);
  assert.equal(goalActiveSeq({ id: "自定义" }, 1), 2, "id 解析不出来才回退栈位置");
  assert.equal(goalActiveSeq({ id: "" }, 4), 5);
});

test("activeGoalFrame 取栈上 active 帧；后端没标时退回栈顶", () => {
  assert.equal(activeGoalFrame(GOVERNANCE).id, "g-3");
  assert.equal(activeGoalFrame({ stack: [{ id: "g-1" }, { id: "g-2" }] }).id, "g-2", "顺序事实在栈本身");
  assert.equal(activeGoalFrame({ stack: [] }), null);
  assert.equal(activeGoalFrame(null), null);
});

test("renderGoalDetail 用属性表逐帧列全字段，栈顶标当前目标", () => {
  const html = renderGoalDetail(GOVERNANCE, "把 goal 看板搬到工作台，点开能看详情");
  assert.match(html, /data-goal-frames="2"/);
  assert.equal((html.match(/<section class="goal-detail-frame(?: is-active)?"/g) || []).length, 2, "两帧各一节");
  assert.equal((html.match(/<section class="goal-detail-frame is-active"/g) || []).length, 1, "只有栈顶标当前");
  for (const label of ["序号", "状态", "标题", "目标正文", "完成条件", "非目标范围", "打点流水", "创建", "更新"]) {
    assert.match(html, new RegExp(`<th scope="row">${label}</th>`), `属性表缺行：${label}`);
  }
  assert.match(html, /3（g-3）/);
  assert.match(html, /看板出到工作台的「目标」子页/);
  assert.match(html, /不做动画/);
  assert.match(html, /我发出的最近一次任务/);
  // 嵌套压栈（2 帧）时详情前面有栈总览；单帧不给冗余的一行表
  assert.match(html, /活动栈总览（2 帧/);
  assert.match(html, /goal-stack-table/);
  const single = renderGoalDetail({ active: true, stack: [GOVERNANCE.stack[1]] }, "");
  assert.ok(!single.includes("goal-stack-table"), "单帧不渲染只含一行的栈总览表");
});

test("renderGoalDetail 的打点流水是完整的（不是卡片那一行的最近 3 条）", () => {
  const frame = {
    id: "g-1", title: "流水", status: "active", active: true,
    progress: [{ content: "p3" }],
    progress_all: [{ kind: "milestone", content: "p1" }, { kind: "finding", content: "p2" }, { kind: "decision", content: "p3" }, { kind: "risk", content: "p4" }]
  };
  const html = renderGoalDetail({ active: true, stack: [frame] }, "");
  for (const mark of ["p1", "p2", "p3", "p4"]) {
    assert.match(html, new RegExp(mark), `完整流水缺 ${mark}`);
  }
  assert.equal((html.match(/class="goal-detail-mark"/g) || []).length, 4);
});

test("renderGoalFrameDetail 空字段写 —，不编造内容", () => {
  const html = renderGoalFrameDetail({ id: "g-0", status: "paused" }, false, 0);
  assert.match(html, /data-goal-detail-status="paused"/);
  assert.match(html, /<span class="muted">—<\/span>/);
  assert.ok(!html.includes("当前目标"), "非栈顶帧不标当前目标");
});

test("renderGoalDetail 空栈返回空串", () => {
  assert.equal(renderGoalDetail({ active: false, stack: [] }), "");
  assert.equal(renderGoalDetail(null), "");
});

test("renderGoalBoard / renderGoalDetail 转义目标内容，不把文本当结构", () => {
  const frame = {
    id: "g-1", title: "<img src=x onerror=1>", status: "active", active: true,
    statement: "<script>alert(1)</script>",
    acceptance: ["<b>验收</b>"],
    progress_all: [{ kind: "milestone", content: "<i>打点</i>" }]
  };
  const board = renderGoalBoard({ stack: [frame] }, "<u>输入</u>");
  assert.ok(!board.includes("<img"), "标题必须被转义");
  assert.ok(!board.includes("<u>输入</u>"), "输入必须被转义");
  const detail = renderGoalDetail({ stack: [frame] }, "<u>输入</u>");
  assert.ok(!detail.includes("<script>"), "正文必须被转义");
  assert.ok(!detail.includes("<b>验收</b>"), "完成条件必须被转义");
  assert.ok(!detail.includes("<i>打点</i>"), "打点内容必须被转义");
});
