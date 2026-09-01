import assert from "node:assert/strict";
import test from "node:test";

import {
  TRAJECTORY_KINDS,
  buildTrajectory,
  filterTrajectory,
  trajectoryStats,
  trajectoryKindLabel,
  renderTrajectoryFilters,
  renderTrajectorySummary,
  renderTrajectoryTable,
  renderTrajectoryRow,
  renderContextAxis,
  renderPromptInjection,
  escapeHtml
} from "./trajectory.js";

// 模拟后端 conversation 消息（Snapshot 契约：role + tool 对象）。
function userMessage(id, content) {
  return { id, role: "user", content, created_at: "2026-08-25T10:00:00Z" };
}
function llmMessage(id, content) {
  return { id, role: "assistant", content, created_at: "2026-08-25T10:00:01Z" };
}
function toolStart(id, name, argumentsJSON) {
  return { id: `${id}-start`, role: "tool", tool: { id, name, arguments: argumentsJSON, status: "running" }, created_at: "2026-08-25T10:00:02Z" };
}
function toolEnd(id, name, result, extra = {}) {
  return { id: `${id}-end`, role: "tool_result", content: result, tool: { id, name, result, status: "success", duration: 1_200_000_000, ...extra }, created_at: "2026-08-25T10:00:03Z" };
}
function errorMessage(id, content) {
  return { id, role: "error", content, created_at: "2026-08-25T10:00:04Z" };
}
function systemMessage(id, content) {
  return { id, role: "system", content, created_at: "2026-08-25T10:00:05Z" };
}

test("classifies conversation messages into response types", () => {
  const records = buildTrajectory([
    userMessage("u1", "请读 README"),
    llmMessage("a1", "好的，我来读取。"),
    toolStart("call-1", "read_file", '{"path":"README.md"}'),
    toolEnd("call-1", "read_file", "file content"),
    llmMessage("a2", "读取完成。"),
    errorMessage("e1", "网络超时"),
    systemMessage("s1", "已恢复会话")
  ]);

  assert.deepEqual(records.map(record => record.kind), ["input", "llm", "tool", "llm", "error", "notice"]);
  assert.deepEqual(records.map(record => record.name), ["输入", "LLM", "read_file", "LLM", "错误", "通知"]);
  // 工具记录：请求 + 响应合并为一条，带 IN/OUT/状态/耗时。
  const tool = records[2];
  assert.equal(tool.input, '{"path":"README.md"}');
  assert.equal(tool.output, "file content");
  assert.equal(tool.status, "success");
  assert.equal(tool.duration, 1_200_000_000);
  assert.equal(tool.startedAt, "2026-08-25T10:00:02Z");
});

test("skips empty assistant placeholders (tool-round markers)", () => {
  const records = buildTrajectory([
    userMessage("u1", "run"),
    { id: "a-empty", role: "assistant", content: "", created_at: "2026-08-25T10:00:01Z" },
    toolStart("call-1", "bash", "{}"),
    toolEnd("call-1", "bash", "done"),
    { id: "a-empty-2", role: "assistant", content: "", created_at: "2026-08-25T10:00:04Z" }
  ]);
  assert.deepEqual(records.map(record => record.kind), ["input", "tool"]);
});

test("carries reasoning_content on llm records for the trajectory view", () => {
  const records = buildTrajectory([
    { id: "a1", role: "assistant", content: "answer", reasoning_content: "thinking steps", created_at: "2026-08-25T10:00:01Z" }
  ]);
  assert.equal(records.length, 1);
  assert.equal(records[0].reasoning, "thinking steps");
});

test("renders THINK panel with full reasoning in trajectory detail", () => {
  const payloads = new Map();
  const records = buildTrajectory([
    { id: "a1", role: "assistant", content: "answer", reasoning_content: "step one\nstep two", created_at: "2026-08-25T10:00:01Z" }
  ]);
  const html = renderTrajectoryRow(records[0], records[0].key, payloads);
  assert.match(html, /trajectory-think/);
  assert.match(html, /class="io-label">THINK/);
  assert.equal(payloads.get("message:a1-out-think"), "step one\nstep two");
});

test("merges tool_result by tool id and marks error status", () => {
  const records = buildTrajectory([
    toolStart("call-1", "bash", '{"command":"ls"}'),
    toolEnd("call-1", "bash", "", { status: "error", error: "permission denied" })
  ]);
  assert.equal(records.length, 1);
  assert.equal(records[0].status, "error");
  assert.equal(records[0].output, "permission denied");
  assert.equal(records[0].input, '{"command":"ls"}');
});

test("carries result_ref truncation metadata for oversized outputs", () => {
  const records = buildTrajectory([
    toolStart("call-big", "bash", "{}"),
    toolEnd("call-big", "bash", "preview", {
      result_ref: "tr-abc", truncated: true, total_chars: 9000
    })
  ]);
  assert.equal(records[0].resultRef, "tr-abc");
  assert.equal(records[0].truncated, true);
  assert.equal(records[0].totalChars, 9000);
});

test("matches tool_result by name when tool id is missing", () => {
  const records = buildTrajectory([
    { id: "t1", role: "tool", tool: { name: "grep", arguments: "{}" }, created_at: "2026-08-25T10:00:02Z" },
    { id: "t2", role: "tool_result", content: "hits", tool: { name: "grep", result: "hits", status: "success" }, created_at: "2026-08-25T10:00:03Z" }
  ]);
  assert.equal(records.length, 1);
  assert.equal(records[0].output, "hits");
  assert.equal(records[0].status, "success");
});

test("filters trajectory by kind and aggregates stats", () => {
  const records = buildTrajectory([
    userMessage("u1", "hi"),
    llmMessage("a1", "hello"),
    toolStart("call-1", "get_time", "{}"),
    toolEnd("call-1", "get_time", "now"),
    errorMessage("e1", "boom")
  ]);
  // 5 条消息 → 4 条轨迹记录（tool 请求/响应配对为一条）。
  assert.equal(filterTrajectory(records, "all").length, 4);
  assert.deepEqual(filterTrajectory(records, "tool").map(record => record.kind), ["tool"]);
  assert.deepEqual(filterTrajectory(records, "llm").map(record => record.kind), ["llm"]);
  assert.equal(filterTrajectory(records, "notice").length, 0);

  const stats = trajectoryStats(records);
  assert.equal(stats.total, 4);
  assert.equal(stats.success, 2); // llm + tool
  assert.equal(stats.error, 1);
  assert.equal(stats.running, 0);
  assert.equal(stats.info, 1); // input（info 状态不计 success）
  assert.deepEqual(stats.byKind, { input: 1, llm: 1, tool: 1, error: 1 });
});

test("renders network-style table with stable row keys", () => {
  const records = buildTrajectory([
    userMessage("u1", "hi"),
    toolStart("call-1", "bash", "{}"),
    toolEnd("call-1", "bash", "done")
  ]);
  const model = renderTrajectoryTable(records);
  assert.deepEqual(model.items.map(item => item.key), ["message:u1", "tool:call-1"]);
  assert.match(model.items[1].html, /data-trajectory-key="tool:call-1"/);
  assert.match(model.html, /data-trajectory-key="message:u1"/);
  // 表头列：时间 / 类型 / 名称 / 状态 / 耗时 / 大小
  assert.match(model.html, /时间/);
  assert.match(model.html, /状态/);
  assert.match(model.html, /耗时/);
  // 工具行详情：IN/OUT 双栏与完整 payload
  assert.equal(model.payloads.get("tool:call-1-in"), "{}");
  assert.equal(model.payloads.get("tool:call-1-out"), "done");
  assert.match(model.items[1].html, /class="io-label">IN/);
  assert.match(model.items[1].html, /class="io-label">OUT/);
});

test("escapes untrusted text in trajectory rows", () => {
  const records = buildTrajectory([
    toolStart("call-x", "<img src=x onerror=alert(1)>", '{"cmd":"<script>alert(1)</script>"}'),
    toolEnd("call-x", "bash", "</pre><script>alert(2)</script>")
  ]);
  const model = renderTrajectoryTable(records);
  assert.doesNotMatch(model.items[0].html, /<script>/);
  assert.match(model.items[0].html, /&lt;script&gt;/);
  assert.equal(model.payloads.get("tool:call-x-in"), '{\n  "cmd": "<script>alert(1)</script>"\n}');
});

test("renders result_ref loader for truncated tool output", () => {
  const records = buildTrajectory([
    toolStart("call-big", "bash", "{}"),
    toolEnd("call-big", "bash", "preview", { result_ref: "tr-xyz", truncated: true, total_chars: 9000 })
  ]);
  const model = renderTrajectoryTable(records);
  assert.match(model.items[0].html, /data-load-ref="tr-xyz"/);
  assert.match(model.items[0].html, /加载完整输出/);
  assert.match(model.items[0].html, /全文 9000 字符/);
});

test("renders empty state for empty trajectory", () => {
  const model = renderTrajectoryTable([]);
  assert.equal(model.items.length, 0);
  assert.match(model.html, /trajectory-empty/);
  assert.match(model.html, /暂无轨迹记录/);
});

test("renders prompt injection empty state for no layers", () => {
  const html = renderPromptInjection([]);
  assert.match(html, /data-prompt-injection="1"/);
  assert.match(html, /暂无前缀注入层/);
  assert.equal(renderPromptInjection(null).includes("暂无前缀注入层"), true);
  assert.equal(renderPromptInjection(undefined).includes("暂无前缀注入层"), true);
});

test("renders prompt injection layers with kind labels and escaped text", () => {
  const html = renderPromptInjection([
    { kind: "base", name: "system", text: "你是 Seelex，负责代码审查。" },
    { kind: "effort", name: "high", text: "高力度：逐步验证。" },
    { kind: "skill", name: "review", text: "<script>alert(1)</script> 技能内容" }
  ]);
  assert.match(html, /基础\/系统提示/);
  assert.match(html, /力度/);
  assert.match(html, /技能/);
  assert.match(html, /你是 Seelex，负责代码审查。/);
  assert.match(html, /高力度：逐步验证。/);
  // 技能内容转义，无未受控注入。
  assert.doesNotMatch(html, /<script>/);
  assert.match(html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
  // 每个层都是可展开 details。
  assert.equal((html.match(/<details class="trajectory-prompt-layer"/g) || []).length, 3);
});

test("renders filters with counts and active state", () => {
  const records = buildTrajectory([
    userMessage("u1", "hi"),
    toolStart("call-1", "bash", "{}"),
    toolEnd("call-1", "bash", "done")
  ]);
  const html = renderTrajectoryFilters(records, "tool");
  assert.match(html, /data-trajectory-filter="all"/);
  assert.match(html, /data-trajectory-filter="tool"/);
  // 计数徽标
  assert.match(html, /全部<\/span><span class="trajectory-filter-count">2<\/span>/);
  assert.match(html, /工具<\/span><span class="trajectory-filter-count">1<\/span>/);
  // active 标记（按钮 class 先于 data-trajectory-filter）
  assert.match(html, /class="trajectory-filter-btn is-active"[\s\S]*data-trajectory-filter="tool"/);
  assert.match(html, /class="trajectory-filter-btn" data-trajectory-filter="all"/);
});

test("renders summary counts", () => {
  const records = buildTrajectory([
    toolStart("call-1", "bash", "{}"),
    toolEnd("call-1", "bash", "ok"),
    errorMessage("e1", "boom")
  ]);
  const html = renderTrajectorySummary(trajectoryStats(records));
  assert.match(html, /共 2 条/);
  assert.match(html, /成功 1/);
  assert.match(html, /失败 1/);
});

test("renders context axis segments for every trajectory record", () => {
  const records = buildTrajectory([
    userMessage("u1", "hi"),
    llmMessage("a1", "hello"),
    toolStart("call-1", "bash", "{}"),
    toolEnd("call-1", "bash", "done")
  ]);
  const html = renderContextAxis(records);
  assert.match(html, /context-axis-track/);
  assert.match(html, /data-trajectory-key="message:u1"/);
  assert.match(html, /data-trajectory-key="message:a1"/);
  assert.match(html, /data-trajectory-key="tool:call-1"/);
  assert.match(html, /axis-segment is-input/);
  assert.match(html, /axis-segment is-llm/);
  assert.match(html, /axis-segment is-tool/);
  assert.match(html, /axis-legend-item/);
});

test("renders context axis empty state", () => {
  const html = renderContextAxis([]);
  assert.match(html, /context-axis-empty/);
});

test("row rendering keeps duration and size columns", () => {
  const payloads = new Map();
  const records = buildTrajectory([
    toolStart("call-1", "bash", "{}"),
    toolEnd("call-1", "bash", "x".repeat(2048), { duration: 2_500_000_000 })
  ]);
  const html = renderTrajectoryRow(records[0], records[0].key, payloads);
  assert.match(html, />2\.5s<\/span>/);
  assert.match(html, /2\.0 KB/);
  // 可展开详情（details + summary 主行）
  assert.match(html, /<details class="trajectory-row/);
  assert.match(html, /<summary class="trajectory-row-main"/);
});

test("kind labels and html escaping helpers are stable", () => {
  assert.equal(trajectoryKindLabel("tool"), "工具");
  assert.equal(trajectoryKindLabel("llm"), "LLM");
  assert.equal(trajectoryKindLabel("unknown"), "unknown");
  assert.equal(TRAJECTORY_KINDS.length, 5);
  assert.equal(escapeHtml("<b>&\"'"), "&lt;b&gt;&amp;&quot;&#039;");
});
