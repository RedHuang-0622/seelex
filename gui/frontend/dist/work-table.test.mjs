import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const markdownSource = await readFile(new URL("./markdown.js", import.meta.url), "utf8");
const markdownURL = `data:text/javascript;base64,${Buffer.from(markdownSource).toString("base64")}`;
const componentsSource = (await readFile(new URL("./components.js", import.meta.url), "utf8"))
  .replace('"./markdown.js"', `"${markdownURL}"`);
const componentsURL = `data:text/javascript;base64,${Buffer.from(componentsSource).toString("base64")}`;
const source = (await readFile(new URL("./work-table.js", import.meta.url), "utf8"))
  .replace('"./components.js"', `"${componentsURL}"`);
const {
  workTableView,
  renderShellHTML,
  renderWorkItemRow,
  renderWorkTraceHTML,
  workTableSignatures,
  countUnread
} = await import(`data:text/javascript;base64,${Buffer.from(source).toString("base64")}`);

const uiState = () => ({ expanded: true, filter: "all", traces: new Set() });

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
  assert.match(html, /data-work-filter="all"/);
  assert.match(html, /data-work-filter="plan"/);
  assert.match(html, /data-work-filter="tasklist"/);
  assert.match(html, /data-work-filter="subagent"/);
  assert.match(html, />阶段</);
  assert.match(html, />Assignee</);
  assert.match(html, />Dependency</);
  assert.match(html, />附件</);
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
