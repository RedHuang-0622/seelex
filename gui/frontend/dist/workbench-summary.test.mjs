import test from "node:test";
import assert from "node:assert/strict";
import { renderWorkbenchSummary } from "./workbench-summary.js";

const rows = [
  { id: "todo-1", kind: "todo", task: "核对发布清单", description: "检查 artifacts", status: "doing", trace: [
    { at: "2026-10-20T10:00:00Z", status: "completed", operation: "inspect" }
  ] },
  { id: "task-1", kind: "task", task: "构建 Windows 包", description: "产出 exe", status: "running", session_id: "s1" },
  { id: "sub-1", kind: "subagent", task: "审查安装脚本", description: "并行审查", status: "completed", session_id: "s1" }
];

test("todo is rendered as a dot table in its own block", () => {
  const html = renderWorkbenchSummary(rows, { viewSessionID: "s1" });
  assert.match(html, /data-workbench-section="todo"/);
  assert.match(html, /class="workbench-todo-table"/);
  assert.match(html, /workbench-todo-dot/);
  assert.match(html, /核对发布清单/);
});

test("task and subagent use separate blocks and the same long-running language", () => {
  const html = renderWorkbenchSummary(rows, { viewSessionID: "s1" });
  assert.match(html, /data-workbench-section="task"/);
  assert.match(html, /data-workbench-section="subagent"/);
  assert.match(html, /class="workbench-run-list"/);
  assert.equal((html.match(/class="workbench-run-row/g) || []).length, 2);
});

test("empty todo keeps an explicit actionable empty state", () => {
  const html = renderWorkbenchSummary([], { viewSessionID: "s1" });
  assert.match(html, /暂时没有 todo 项/);
  assert.doesNotMatch(html, /data-workbench-section="task"/);
  assert.doesNotMatch(html, /data-workbench-section="subagent"/);
});
