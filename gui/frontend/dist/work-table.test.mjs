import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const embedURL = `data:text/javascript;base64,${Buffer.from(await readFile(new URL("./html-embed.js", import.meta.url), "utf8")).toString("base64")}`;
const markdownSource = (await readFile(new URL("./markdown.js", import.meta.url), "utf8"))
  .replace('"./html-embed.js"', `"${embedURL}"`);
const markdownURL = `data:text/javascript;base64,${Buffer.from(markdownSource).toString("base64")}`;
const componentsSource = (await readFile(new URL("./components.js", import.meta.url), "utf8"))
  .replace('"./markdown.js"', `"${markdownURL}"`);
const componentsURL = `data:text/javascript;base64,${Buffer.from(componentsSource).toString("base64")}`;
const motionURL = `data:text/javascript;base64,${Buffer.from(await readFile(new URL("./motion.js", import.meta.url), "utf8")).toString("base64")}`;
const source = (await readFile(new URL("./work-table.js", import.meta.url), "utf8"))
  .replace('"./components.js"', `"${componentsURL}"`)
  .replace('"./motion.js"', `"${motionURL}"`);
const {
  createWorkTableView,
  workTableView,
  workTableBatches,
  renderShellHTML,
  renderWorkItemRow,
  renderWorkTraceHTML,
  workTableSignatures,
  countUnread,
  pageCount,
  pagedRows
} = await import(`data:text/javascript;base64,${Buffer.from(source).toString("base64")}`);

const uiState = () => ({ expanded: true, filter: "all", traces: new Set() });

function workTableViewHarness() {
  let clickHandler = null;
  const rowsContainer = {
    children: [],
    querySelector: () => null,
    append() {},
    insertAdjacentHTML() {},
    ownerDocument: { createElement: () => ({ innerHTML: "", content: { firstElementChild: null } }) }
  };
  const container = {
    classList: { add() {}, remove() {}, toggle() {} },
    dataset: {},
    innerHTML: "",
    addEventListener(type, fn) { if (type === "click") clickHandler = fn; },
    querySelector(selector) { return selector === "[data-work-rows]" ? rowsContainer : null; }
  };
  return { container, click: event => clickHandler(event) };
}

test("delegates detail clicks in the work table to onDetail", () => {
  const harness = workTableViewHarness();
  const view = createWorkTableView(harness.container);
  let opened = "";
  view.bind({ onDetail: id => { opened = id; }, onStatus() {} });
  view.render([{
    id: "plan:n1", phase: "plan", task: "调研", status: "running", kind: "plan",
    source_id: "n1", trace: []
  }]);
  harness.click({
    target: { closest(selector) {
      return selector === "[data-plan-node-open]" ? { dataset: { planNodeOpen: "n1" } } : null;
    } },
    stopPropagation() {}
  });
  assert.equal(opened, "n1");
});

test("trace toggle does not fall through to the detail delegation", () => {
  const harness = workTableViewHarness();
  const view = createWorkTableView(harness.container);
  let opened = "";
  view.bind({ onDetail: id => { opened = id; }, onStatus() {} });
  view.render([{
    id: "plan:n1", phase: "plan", task: "调研", status: "running", kind: "plan",
    source_id: "n1", trace: [{ status: "running", operation: "read_file" }]
  }]);
  harness.click({
    target: { closest(selector) {
      return selector === "[data-work-trace-toggle]" ? { dataset: { workTraceToggle: "plan:n1" } } : null;
    } }
  });
  assert.equal(opened, "");
});

test("normalizes work table rows defensively", () => {
  assert.deepEqual(workTableView(null), []);
  assert.deepEqual(workTableView("nope"), []);

  const rows = workTableView([
    { id: "plan:n1", phase: "plan", task: "调研", status: "running", trace: [{ status: "running", operation: "read_file", evidence: "x" }] },
    { id: "todo:0", phase: "tasklist", task: "写测试", status: "doing" },
    { id: "subagent:s1", phase: "subagent", task: "g", status: "failed" },
    { notAnObject: true },
    null
  ]);
  assert.equal(rows.length, 3);
  assert.equal(rows[0].phase, "plan");
  assert.equal(rows[1].status, "doing");
  assert.equal(rows[2].dependencies.length, 0);
  assert.equal(rows[0].trace[0].status, "running");
});

test("renders shell with filter chips and totals", () => {
  const html = renderShellHTML([
    { id: "plan:n1", phase: "plan", task: "a", status: "running", trace: [] },
    { id: "todo:0", phase: "tasklist", task: "b", status: "done", trace: [{ status: "done" }] }
  ], uiState());
  assert.match(html, /工作表格/);
  assert.match(html, /2 项/);
  assert.match(html, /1 打点/);
  assert.match(html, /excel-grid/);
  assert.match(html, /data-work-filter="all"/);
  assert.match(html, /data-work-filter="plan"/);
  assert.match(html, /data-work-filter="task"/);
  assert.match(html, /data-work-filter="todo"/);
  assert.match(html, /data-work-filter="subagent"/);
  assert.match(html, />类型</);
  assert.match(html, />Assignee</);
  assert.match(html, />依赖</);
  assert.match(html, />附件</);
});

test("normalizes batch headers defensively", () => {
  assert.deepEqual(workTableBatches(null), []);
  assert.deepEqual(workTableBatches("nope"), []);

  const batches = workTableBatches([
    { id: "chat-1", label: "2026-08-23 09:15", created_at: "2026-08-23T09:15:00Z", counts: { all: 3, todo: 2, plan: 1, bogus: 9 } },
    { id: "", label: "早期任务", counts: null },
    null
  ]);
  assert.equal(batches.length, 2);
  assert.equal(batches[0].id, "chat-1");
  assert.equal(batches[0].counts.all, 3);
  assert.equal(batches[0].counts.task, 0);
  assert.equal(batches[0].counts.bogus, undefined);
  assert.equal(batches[1].label, "早期任务");
  assert.deepEqual(batches[1].counts, { all: 0, plan: 0, task: 0, todo: 0, subagent: 0 });
});

test("renders batch sheet tabs with labels, counts and row container", () => {
  const html = renderShellHTML([
    { id: "todo:0", phase: "tasklist", task: "a", status: "pending", kind: "todo", batch_id: "chat-1" },
    { id: "task:1", phase: "task", task: "b", status: "pending", kind: "task", batch_id: "chat-1" }
  ], {
    ...uiState(),
    batches: workTableBatches([
      { id: "chat-1", label: "2026-08-23 09:15", created_at: "2026-08-23T09:15:00Z", counts: { all: 2, todo: 1, task: 1 } }
    ])
  });
  assert.match(html, /data-work-sheet="chat-1"/);
  assert.match(html, /data-work-sheet="all"/);
  assert.match(html, /2026-08-23 09:15/);
  assert.match(html, /Task 1 · Todo 1/);
  assert.match(html, /data-work-rows/);
  assert.doesNotMatch(html, /data-work-batch="chat-1"/);
  assert.doesNotMatch(html, /data-work-batch-toggle/);

  // 无批次时保持扁平结构（向后兼容，旧快照/事件不分组）。
  const flat = renderShellHTML([
    { id: "todo:0", phase: "tasklist", task: "a", status: "pending", kind: "todo" }
  ], uiState());
  assert.match(flat, /data-work-rows/);
  assert.doesNotMatch(flat, /data-work-sheets/);
});

test("sheet switch and kind filter update view state", () => {
  const harness = workTableViewHarness();
  const view = createWorkTableView(harness.container);
  view.bind({ onDetail() {}, onStatus() {} });
  const rows = workTableView([
    { id: "todo:0", phase: "tasklist", task: "a", status: "pending", kind: "todo", batch_id: "chat-1" },
    { id: "task:1", phase: "task", task: "b", status: "pending", kind: "task", batch_id: "chat-1" }
  ]);
  const batches = workTableBatches([
    { id: "chat-1", label: "批次A", created_at: "", counts: { all: 2, todo: 1, task: 1 } }
  ]);
  view.render(rows, batches);

  // 批次维度切换（Excel sheet 页签）。
  harness.click({
    target: { closest(selector) {
      return selector === "[data-work-sheet]" ? { dataset: { workSheet: "chat-1" } } : null;
    } }
  });
  assert.equal(view.state.activeBatch, "chat-1");

  // kind 筛选切换（todo）。
  harness.click({
    target: { closest(selector) {
      return selector === "[data-work-filter]" ? { dataset: { workFilter: "todo" } } : null;
    } }
  });
  assert.equal(view.state.filter, "todo");
});

test("paginates rows and clamps out-of-range page", () => {
  const rows = Array.from({ length: 45 }, (_, i) => ({
    id: `task:${i}`, phase: "task", task: `t${i}`, status: "pending", kind: "task"
  }));
  const state = { filter: "all", page: 3, pageSize: 20 };
  const paged = pagedRows(rows, state);
  assert.equal(paged.length, 5);
  assert.equal(paged[0].id, "task:40");
  assert.equal(state.page, 3);

  // 数据收缩后页码越界 → 钳制到最后一页。
  state.page = 99;
  const clamped = pagedRows(rows, state);
  assert.equal(state.page, 3);
  assert.equal(clamped.length, 5);

  // 非法页码 → 第 1 页。
  state.page = 0;
  pagedRows(rows, state);
  assert.equal(state.page, 1);
});

test("pageCount computes total pages with minimum 1", () => {
  assert.equal(pageCount(0, 20), 1);
  assert.equal(pageCount(20, 20), 1);
  assert.equal(pageCount(21, 20), 2);
  assert.equal(pageCount(45, 10), 5);
});

test("renders pager with page info and page-size options", () => {
  const rows = Array.from({ length: 25 }, (_, i) => ({
    id: `task:${i}`, phase: "task", task: `t${i}`, status: "pending", kind: "task"
  }));
  const html = renderShellHTML(rows, { ...uiState(), page: 2, pageSize: 10 });
  assert.match(html, /data-work-page-prev/);
  assert.match(html, /data-work-page-next/);
  assert.match(html, /data-work-page-info/);
  assert.match(html, /2 \/ 3 页 · 25 项/);
  assert.match(html, /data-work-page-size/);
  assert.match(html, /10 \/ 页/);
  assert.match(html, /20 \/ 页/);
  assert.match(html, /50 \/ 页/);
});

test("pager marks first/last page buttons as disabled", () => {
  const rows = Array.from({ length: 5 }, (_, i) => ({
    id: `task:${i}`, phase: "task", task: `t${i}`, status: "pending", kind: "task"
  }));
  const first = renderShellHTML(rows, { ...uiState(), page: 1, pageSize: 3 });
  assert.match(first, /data-work-page-prev disabled/);
  assert.doesNotMatch(first, /data-work-page-next disabled/);

  const last = renderShellHTML(rows, { ...uiState(), page: 2, pageSize: 3 });
  assert.doesNotMatch(last, /data-work-page-prev disabled/);
  assert.match(last, /data-work-page-next disabled/);
});

test("renders plan row with detail action and escaped content", () => {
  const row = workTableView([{
    id: "plan:n1", phase: "plan", task: "<script>alert(1)</script>", description: "desc",
    status: "running", source_id: "n1", dependencies: ["plan:n0"], attachments: ["docs/x.md"], trace: []
  }])[0];
  const html = renderWorkItemRow(row, uiState());
  assert.match(html, /data-work-row="plan:n1"/);
  assert.match(html, /data-plan-node-open="n1"/);
  assert.match(html, /详情/);
  assert.doesNotMatch(html, /data-work-status/); // 非 todo 行不渲染状态按钮
  assert.equal(html.includes("<script>"), false);
  assert.match(html, /&lt;script&gt;/);
  assert.match(html, /plan:n0/);
  assert.match(html, /docs\/x\.md/);
  assert.match(html, /RUNNING/);
});

test("renders todo row with three-state status control", () => {
  const row = workTableView([{
    id: "todo:0", phase: "tasklist", task: "写测试", status: "doing", kind: "todo", assignee: "main"
  }])[0];
  const html = renderWorkItemRow(row, uiState());
  assert.match(html, /data-work-todo="todo:0"/);
  assert.match(html, /data-work-status="todo:0" data-status="pending"/);
  assert.match(html, /data-status="doing"/);
  assert.match(html, /data-status="done"/);
  assert.match(html, /class="work-status-btn is-active" data-work-status="todo:0" data-status="doing"/);
  assert.equal(html.includes("详情"), false); // todo 行无节点详情入口
});

test("renders trace table and escapes evidence", () => {
  const html = renderWorkTraceHTML([
    { at: "2026-08-09T00:00:00Z", operation: "read_file", status: "success", evidence: "<img onerror=x>", duration: "1.50s" },
    { at: "", operation: "node.lifecycle", status: "running", evidence: "", duration: "" }
  ]);
  assert.match(html, /read_file/);
  assert.match(html, /SUCCESS/);
  assert.match(html, /1\.50s/);
  assert.match(html, /RUNNING/);
  assert.equal(html.includes("<img"), false);
  assert.match(html, /&lt;img/);
});

test("renders retry status with retry count", () => {
  const row = workTableView([{
    id: "plan:n1", phase: "plan", task: "重试任务", status: "retry", retry_count: 2, kind: "plan"
  }])[0];
  const html = renderWorkItemRow(row, uiState());
  assert.match(html, /RETRY 2/);
  assert.match(html, /work-status is-retry/);
});

test("renders task phase chip and filter", () => {
  const row = workTableView([{ id: "task:1", phase: "task", task: "主动任务", status: "pending", kind: "task" }])[0];
  const html = renderWorkItemRow(row, uiState());
  assert.match(html, /work-phase-chip is-task/);
  const shell = renderShellHTML([row], uiState());
  assert.match(shell, /data-work-filter="task"/);
  assert.match(shell, />Task </);
});

test("collapsed state hides the table body", () => {
  const html = renderShellHTML([{ id: "todo:0", phase: "tasklist", task: "a", status: "pending" }], {
    expanded: false, filter: "all", traces: new Set()
  });
  assert.match(html, /work-entry-body is-collapsed/);
  assert.match(html, /aria-expanded="false"/);
});

test("counts unread entries by new rows and signature changes", () => {
  const rows = [
    { id: "plan:n1", status: "running", retry_count: 0 },
    { id: "todo:0", status: "doing", retry_count: 0 }
  ];
  // 从未打开 → 全部未读。
  assert.equal(countUnread(rows, null), 2);
  assert.equal(countUnread(rows, new Map()), 2);

  // 打开后记录已读 → 无未读。
  const seen = workTableSignatures(rows);
  assert.equal(countUnread(rows, seen), 0);

  // 状态变化（completed）→ 未读 1；retry 变化 → 未读。
  const changed = [{ id: "plan:n1", status: "completed", retry_count: 0 }, { id: "todo:0", status: "doing", retry_count: 0 }];
  assert.equal(countUnread(changed, seen), 1);
  const retried = [{ id: "plan:n1", status: "retry", retry_count: 2 }, { id: "todo:0", status: "doing", retry_count: 0 }];
  assert.equal(countUnread(retried, seen), 1);

  // 新行 → 未读。
  const added = [...rows, { id: "task:9", status: "pending", retry_count: 0 }];
  assert.equal(countUnread(added, seen), 1);
});

// ── 会话筛选轴（跨会话台账 + 「仅本会话」）──────────────────────────
// 工作表格是项目/全局台账：默认全部会话，会话维度只能靠这一层收窄。

const crossSessionRows = () => ([
  { id: "plan:n1", phase: "plan", task: "本会话的 plan", status: "running", kind: "plan", session_id: "sess-a", batch_id: "chat-1" },
  { id: "task:2", phase: "task", task: "别的会话的 task", status: "pending", kind: "task", session_id: "sess-b", batch_id: "chat-2" }
]);

test("renders session filter chips with per-scope counts", () => {
  const html = renderShellHTML(crossSessionRows(), uiState());
  // 默认「全部会话」：类型计数是两行。
  assert.match(html, /data-work-session-filter="all"/);
  assert.match(html, /data-work-session-filter="mine"/);
  assert.match(html, /全部会话 <span>2<\/span>/);
  assert.match(html, /data-work-filter="all" data-work-count="2"/);
  assert.match(html, />会话</);
});

test("session filter narrows rows, counts and sheet tabs to the view session", () => {
  const batches = workTableBatches([
    { id: "chat-1", label: "本会话批次", created_at: "", counts: { all: 1, plan: 1 } },
    { id: "chat-2", label: "别会话批次", created_at: "", counts: { all: 1, task: 1 } }
  ]);
  const html = renderShellHTML(crossSessionRows(), {
    ...uiState(),
    sessionFilter: "mine",
    viewSessionID: "sess-a",
    batches
  });
  // 「仅本会话」激活：本会话 1 条、全部会话仍是 2 条。
  assert.match(html, /class="work-filter is-session is-active" data-work-session-filter="mine"/);
  assert.match(html, /data-work-session-count="1"/);
  assert.match(html, /data-work-session-count="2"/);
  assert.match(html, /1 项/);
  // 类型计数随 scope 收窄（Task 行属于别会话 → 0）。
  assert.match(html, /data-work-filter="all" data-work-count="1"/);
  assert.match(html, /data-work-filter="task" data-work-count="0"/);
  // 批次页签只留本会话有行的批次，「全部」页签恒在。
  assert.match(html, /data-work-sheet="chat-1"/);
  assert.doesNotMatch(html, /data-work-sheet="chat-2"/);
  assert.match(html, /data-work-sheet="all"/);
});

test("session filter treats rows without an owning key as the view session", () => {
  // 后端已把实时注册表恒判给当前视图会话（seelebridge taskSnapshotAll /
  // core publishTaskChanged）；但一条丢了归属键的增量行（旧版载荷/异常路径）
  // 不该因此从「仅本会话」筛选与计数里消失——这正是"筛不到正在运行的子代理"
  // 的可见形态。归属判定与计数共用同一口径（不出现"计数 1、列表 0"）。
  const rows = [
    { id: "task:sub", phase: "task", task: "正在跑的子代理", status: "running", kind: "subagent" },
    { id: "task:2", phase: "task", task: "别的会话的 task", status: "pending", kind: "task", session_id: "sess-b" }
  ];
  const html = renderShellHTML(rows, {
    ...uiState(),
    sessionFilter: "mine",
    viewSessionID: "sess-a"
  });
  assert.match(html, /data-work-session-count="1"/);
  assert.match(html, /<span class="work-total">1 项<\/span>/);
});

test("session filter state tracks the current session getter on every render", () => {
  const harness = workTableViewHarness();
  let sessionID = "sess-a";
  const view = createWorkTableView(harness.container, { viewSessionID: () => sessionID });
  view.bind({ onDetail() {}, onStatus() {} });
  view.render(crossSessionRows());
  assert.equal(view.state.viewSessionID, "sess-a");

  // 会话切换后重渲染：取值跟着变（不是创建视图时的旧会话）。
  sessionID = "sess-b";
  view.render(crossSessionRows());
  assert.equal(view.state.viewSessionID, "sess-b");

  // 会话筛选切换是纯 UI 态。
  assert.equal(view.state.sessionFilter, "all");
  harness.click({
    target: { closest(selector) {
      return selector === "[data-work-session-filter]" ? { dataset: { workSessionFilter: "mine" } } : null;
    } }
  });
  assert.equal(view.state.sessionFilter, "mine");
});

test("row carries its owning session for the filter axis", () => {
  const row = workTableView([{
    id: "task:2", phase: "task", task: "别的会话的 task", status: "pending", kind: "task", session_id: "sess-b"
  }])[0];
  const html = renderWorkItemRow(row, uiState());
  assert.match(html, /data-work-session="sess-b"/);
  assert.match(html, /class="work-cell work-cell-session" title="sess-b">sess-b</);
  assert.equal(html.includes(">sess-b</td>"), true);

  // 未归属（草稿/旧数据）渲染占位符，不是空单元格。
  const unowned = workTableView([{ id: "task:3", phase: "task", task: "x", status: "pending", kind: "task" }])[0];
  const unownedHTML = renderWorkItemRow(unowned, uiState());
  assert.match(unownedHTML, /data-work-session=""/);
  assert.match(unownedHTML, /未归属（草稿）/);
});

// ── 实发轴（台账 ≠ 实发块）─────────────────────────────────────
// 台账是全局全量档案；「实发」= 后端请求尾部打点块真正送进模型上下文的行
// （归属当前会话且未终态）。两者必须能区分，否则会把"记在账上"当成"模型看到"。

const dispatchRows = () => ([
  { id: "plan:n1", phase: "plan", task: "本会话在跑", status: "running", kind: "plan", session_id: "sess-a" },
  { id: "todo:0", phase: "tasklist", task: "本会话已做", status: "done", kind: "todo", session_id: "sess-a" },
  { id: "task:2", phase: "task", task: "别会话在跑", status: "running", kind: "task", session_id: "sess-b" }
]);

test("sent chip counts only the current session's non-terminal rows", () => {
  const html = renderShellHTML(dispatchRows(), { ...uiState(), viewSessionID: "sess-a" });
  assert.match(html, /data-work-sent-filter="sent"/);
  // 本会话 2 行里只有 running 那一行是实发。
  assert.match(html, /data-work-sent-count="1"/);
  assert.match(html, /实发 <span>1<\/span>/);
  // 默认不开启：台账仍是三行。
  assert.match(html, /3 项/);
});

test("row badge and attribute mark only dispatched rows", () => {
  const rows = workTableView(dispatchRows());
  const outgoing = renderWorkItemRow(rows[0], { ...uiState(), viewSessionID: "sess-a" });
  assert.match(outgoing, /data-work-dispatched="1"/);
  assert.match(outgoing, /class="work-sent-chip"/);
  assert.match(outgoing, /实发<\/span>/);

  // 同会话但已终态 → 不在实发块里。
  const finished = renderWorkItemRow(rows[1], { ...uiState(), viewSessionID: "sess-a" });
  assert.match(finished, /data-work-dispatched="0"/);
  assert.doesNotMatch(finished, /work-sent-chip/);

  // 别的会话的 running 行 → 台账可见，但不是本会话实发。
  const elsewhere = renderWorkItemRow(rows[2], { ...uiState(), viewSessionID: "sess-a" });
  assert.match(elsewhere, /data-work-dispatched="0"/);
  assert.doesNotMatch(elsewhere, /work-sent-chip/);
});

test("sent-only scope hides ledger-only rows, counts and empty batch tabs", () => {
  const batches = workTableBatches([
    { id: "chat-1", label: "本会话批次", created_at: "", counts: { all: 2, plan: 1, todo: 1 } },
    { id: "chat-2", label: "别会话批次", created_at: "", counts: { all: 1, task: 1 } }
  ]);
  const rows = dispatchRows().map((row, index) => ({ ...row, batch_id: index === 2 ? "chat-2" : "chat-1" }));
  const html = renderShellHTML(rows, {
    ...uiState(),
    sentOnly: true,
    viewSessionID: "sess-a",
    batches
  });
  // 只剩 1 条实发行。
  assert.match(html, /1 项/);
  assert.match(html, /class="work-filter is-sent is-active" data-work-sent-filter="sent"/);
  // 实发计数不受开关自身影响：仍是 scope 内的 1。
  assert.match(html, /data-work-sent-count="1"/);
  // 类型计数按实发 scope 收窄（Todo 已终态 → 0；别会话 Task → 0）。
  assert.match(html, /data-work-filter="todo" data-work-count="0"/);
  assert.match(html, /data-work-filter="task" data-work-count="0"/);
  // 只留实发行所在的批次页签。
  assert.match(html, /data-work-sheet="chat-1"/);
  assert.doesNotMatch(html, /data-work-sheet="chat-2"/);
});

test("sent filter is a toggle held in pure UI state", () => {
  const harness = workTableViewHarness();
  const view = createWorkTableView(harness.container, { viewSessionID: () => "sess-a" });
  view.bind({ onDetail() {}, onStatus() {} });
  view.render(dispatchRows());
  assert.equal(view.state.sentOnly, false);
  const clickSent = () => harness.click({
    target: { closest(selector) {
      return selector === "[data-work-sent-filter]" ? { dataset: {} } : null;
    } }
  });
  clickSent();
  assert.equal(view.state.sentOnly, true);
  clickSent();
  assert.equal(view.state.sentOnly, false);
});

test("新插入的行 / trace 行挂一次性入场类（只有首次插入才播，替换不重播）", () => {
  assert.ok(source.includes("markEntering(element)"), "新建主行应挂入场类");
  assert.ok(source.includes("markEntering(traceRow)"), "新建 trace 展开行应挂入场类");
  assert.ok(source.includes("rollNumber(total,"), "计数（N 项）应走数字滚动");
  assert.ok(source.includes("rollNumber(span,"), "类型/会话/实发计数应走数字滚动");
  assert.ok(source.includes('class="excel-sheets scroll-edges-x"'), "sheet 栏应是横向滚轴（滚轮/拖动可翻）");
});

