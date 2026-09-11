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
  renderTrajectoryWindowInfo,
  renderAxisDetail,
  prefixLayerSegments,
  compactionMarks,
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

  assert.deepEqual(records.map(record => record.kind), ["input", "llm", "tool", "llm", "error", "system"]);
  assert.deepEqual(records.map(record => record.name), ["输入", "LLM", "read_file", "LLM", "错误", "SYSTEM"]);
  // 工具记录：请求 + 响应合并为一条，带 IN/OUT/状态/耗时。
  const tool = records[2];
  assert.equal(tool.input, '{"path":"README.md"}');
  assert.equal(tool.output, "file content");
  assert.equal(tool.status, "success");
  assert.equal(tool.duration, 1_200_000_000);
  assert.equal(tool.startedAt, "2026-08-25T10:00:02Z");
});

test("prefers explicit kind over role fallback for multi-track classification", () => {
  const records = buildTrajectory([
    { id: "x1", role: "assistant", kind: "user_input", content: "typed input", created_at: "2026-08-25T10:00:00Z" },
    { id: "x2", role: "user", kind: "error", content: "user-side failure", created_at: "2026-08-25T10:00:01Z" },
    { id: "x3", role: "system", kind: "internal", content: "<!-- seelex:active-skill:v1 -->skill", created_at: "2026-08-25T10:00:02Z" },
    { id: "x4", role: "assistant", kind: "llm", content: "", created_at: "2026-08-25T10:00:03Z" },
    { id: "x5", role: "user", kind: "system", content: "TL 指令回放", created_at: "2026-08-25T10:00:04Z" }
  ]);
  assert.deepEqual(records.map(record => record.kind), ["input", "error", "notice", "system"]);
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

test("keeps an unpaired tool_result as its own row instead of guessing by name", () => {
  const records = buildTrajectory([
    { id: "t1", role: "tool", tool: { name: "grep", arguments: "{}" }, created_at: "2026-08-25T10:00:02Z" },
    { id: "t2", role: "tool_result", content: "hits", tool: { name: "grep", result: "hits", status: "success" }, created_at: "2026-08-25T10:00:03Z" }
  ]);
  // 没有配对键就不配对：按名字猜会把同名并发工具并成一行（错误呈现）。
  assert.equal(records.length, 2);
  assert.equal(records[0].output, "");
  assert.equal(records[1].output, "hits");
});

test("pairs concurrent same-name tools strictly by call id, even out of order", () => {
  const records = buildTrajectory([
    { id: "c1", role: "tool", tool: { id: "call-1", name: "bash", arguments: "ls" } },
    { id: "c2", role: "tool", tool: { id: "call-2", name: "bash", arguments: "pwd" } },
    { id: "c3", role: "tool_result", tool: { id: "call-2", name: "bash", result: "/work", status: "success" } },
    { id: "c4", role: "tool_result", tool: { id: "call-1", name: "bash", result: "a.txt", status: "success" } }
  ]);
  assert.equal(records.length, 2);
  assert.equal(records[0].input, "ls");
  assert.equal(records[0].output, "a.txt");
  assert.equal(records[1].input, "pwd");
  assert.equal(records[1].output, "/work");
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

test("prefix layers fold into the context axis as a full-width meta lane", () => {
  const records = buildTrajectory([userMessage("u1", "hi"), llmMessage("a1", "hello")]);
  const layers = [
    { kind: "identity", name: "seelex", text: "你是 Seelex。" },
    { kind: "effort", name: "high", text: "高力度：逐步验证并核对每一步。" },
    { kind: "instructions", name: "", text: "<script>alert(1)</script> 指令正文" }
  ];
  const html = renderContextAxis(records, { prefixLayers: layers, compactions: [] });
  // 前缀注入不再是独立面板：作为轴内一条整轴带状轨（段宽=层文本占比）。
  assert.match(html, /context-axis-lane is-prefix/);
  assert.match(html, /data-prefix-layer="0"/);
  assert.match(html, /data-prefix-layer="2"/);
  assert.match(html, /axis-segment is-prefix is-identity/);
  assert.match(html, /axis-segment is-prefix is-effort/);
  assert.match(html, /axis-segment is-prefix is-instructions/);
  assert.match(html, /前缀注入=层文本占比/);
  // 注入层文本不得出现在轴 DOM（只在点击后的轴详情里出现，且已转义）。
  assert.doesNotMatch(html, /<script>/);
  // 段按装配顺序排列、宽度为层文本占比（identity 最短 → 占比最小）。
  const segments = prefixLayerSegments(layers);
  assert.deepEqual(segments.map(segment => segment.kind), ["identity", "effort", "instructions"]);
  assert.ok(segments[0].width < segments[1].width);
  assert.ok(segments[1].width < segments[2].width);
  const widthSum = segments.reduce((sum, segment) => sum + segment.width, 0);
  assert.ok(Math.abs(widthSum - 100) < 0.001);
});

test("compaction events mark the conversation axis at their anchor time", () => {
  const records = buildTrajectory([
    userMessage("u1", "short"),
    { id: "a1", role: "assistant", content: "mid length reply", created_at: "2026-08-25T10:00:01Z" },
    { id: "a2", role: "assistant", content: "a much longer third message that dominates weight", created_at: "2026-08-25T10:00:04Z" }
  ]);
  const compactions = [
    { version: 1, reason: "context_budget", messages_before: 42, estimated_tokens: 96_000, compacted_at: "2026-08-25T10:00:02Z" }
  ];
  const marks = compactionMarks(records, compactions);
  assert.equal(marks.length, 1);
  assert.equal(marks[0].version, 1);
  assert.equal(marks[0].anchored, true);
  // 锚定最后一条 startedAt <= compacted_at 的记录（index 1），x 在其体量终点。
  assert.ok(marks[0].x > 0 && marks[0].x < 100);
  const html = renderContextAxis(records, { prefixLayers: [], compactions });
  assert.match(html, /context-axis-lane is-compress/);
  assert.match(html, /data-compact-idx="0"/);
  assert.match(html, /压缩 ×1/);
  // 刻度是压缩位置的元数据标记（点击开详情），不携带轨迹行定位键。
  const compressBlock = html.match(/<button type="button" class="axis-segment is-compress"[^>]*>/)?.[0] || "";
  assert.doesNotMatch(compressBlock, /data-trajectory-key/);
  assert.match(compressBlock, /data-compact-idx="0"/);
});

test("compaction earlier than the loaded window clamps to the axis start", () => {
  const records = buildTrajectory([userMessage("u1", "newest visible turn")]);
  const compactions = [
    { version: 2, reason: "context_budget", messages_before: 999, estimated_tokens: 120_000, compacted_at: "2026-08-20T08:00:00Z" }
  ];
  const marks = compactionMarks(records, compactions);
  assert.equal(marks.length, 1);
  assert.equal(marks[0].anchored, false);
  assert.equal(marks[0].x, 0);
});

test("axis detail renders escaped prefix layer text on demand", () => {
  const layers = [
    { kind: "identity", name: "seelex", text: "你是 Seelex。" },
    { kind: "instructions", name: "", text: "<script>alert(1)</script> 指令正文" }
  ];
  const segments = prefixLayerSegments(layers);
  const html = renderAxisDetail({ type: "prefix", layer: segments[1] });
  assert.match(html, /前缀注入层 · 指令/);
  assert.match(html, /data-axis-detail/);
  assert.doesNotMatch(html, /<script>/);
  assert.match(html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
  assert.match(html, /每请求前置（system 前缀）/);
});

test("axis detail renders public compaction metadata without private content", () => {
  const records = buildTrajectory([userMessage("u1", "hi")]);
  const compactions = [
    { version: 3, reason: "context_budget", messages_before: 88, estimated_tokens: 200_000, compacted_at: "2026-08-25T10:00:01Z" }
  ];
  const mark = compactionMarks(records, compactions)[0];
  const html = renderAxisDetail({ type: "compression", mark });
  assert.match(html, /上下文压缩 #3/);
  assert.match(html, /88/);
  assert.match(html, /200,000/);
  assert.match(html, /read_compressed_turn/);
  assert.doesNotMatch(html, /checkpoint 正文/);
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

test("renders context axis as per-kind lanes with shared axis positions", () => {
  const records = buildTrajectory([
    userMessage("u1", "hi"),
    llmMessage("a1", "hello"),
    toolStart("call-1", "bash", "{}"),
    toolEnd("call-1", "bash", "done")
  ]);
  const html = renderContextAxis(records);
  assert.match(html, /context-axis-track/);
  // 五种类型各占一条横轨（多线谱分轨），无记录的轨道保留为空轨。
  for (const kind of ["input", "llm", "tool", "error", "notice"]) {
    assert.match(html, new RegExp(`context-axis-lane is-${kind}`));
  }
  assert.match(html, /context-axis-lane is-error is-empty/);
  assert.match(html, /context-axis-lane is-notice is-empty/);
  assert.match(html, /axis-lane-label/);
  // 每个记录块带稳定 key，并以共享横轴上的 --x/--w 定位。
  assert.match(html, /data-trajectory-key="message:u1"/);
  assert.match(html, /data-trajectory-key="message:a1"/);
  assert.match(html, /data-trajectory-key="tool:call-1"/);
  assert.match(html, /axis-segment is-input/);
  assert.match(html, /axis-segment is-llm/);
  assert.match(html, /axis-segment is-tool/);
  assert.match(html, /style="--x:/);
  assert.match(html, /--w:/);
});

test("context axis marks wide blocks so the lane shows content, not blank bars", () => {
  // 两个块的体量差 40 倍：宽块要带 is-wide（直接显示标签），窄块不带。
  const records = buildTrajectory([
    userMessage("u1", "短问题"),
    llmMessage("a1", "x".repeat(4000))
  ]);
  const html = renderContextAxis(records);
  const segments = [...html.matchAll(/class="axis-segment (is-[a-z]+) ([a-z-]+)( is-wide)?"[^>]*--w:([0-9.]+)%/g)];
  assert.equal(segments.length, 2);
  const wide = segments.filter(match => match[3] === " is-wide");
  assert.equal(wide.length, 1, "只有宽块应带 is-wide");
  assert.ok(Number(wide[0][4]) >= 6, `宽块占比 ${wide[0][4]}% 应 ≥6%`);
  const narrow = segments.find(match => match[3] === undefined);
  assert.ok(Number(narrow[4]) < 6, `窄块占比 ${narrow[4]}% 应 <6%`);
});

test("tool steps keep the reasoning that produced them", () => {
  // 持久化形状：思考挂在发起工具调用的那条消息上（assistant/tool_call 行）。
  const records = buildTrajectory([
    {
      id: "m1", role: "assistant", reasoning_content: "先读文件确认结构",
      tool: { id: "call-1", name: "read_file", arguments: "{\"path\":\"a.go\"}", status: "success" },
      created_at: "2026-08-25T10:00:02Z"
    },
    {
      id: "m2", role: "tool_result", content: "package a",
      tool: { id: "call-1", name: "read_file", result: "package a", status: "success" },
      created_at: "2026-08-25T10:00:03Z"
    }
  ]);
  const tool = records.find(record => record.kind === "tool");
  assert.ok(tool, "工具记录必须存在");
  assert.equal(tool.reasoning, "先读文件确认结构");

  // 工具行详情里也要有 THINK 面板，而不是只有 IN/OUT。
  const model = renderTrajectoryTable(records);
  const row = model.items.find(item => item.key.startsWith("tool:"));
  assert.match(row.html, /trajectory-think/);
  assert.match(row.html, /THINK/);
  assert.match(row.html, /先读文件确认结构/);
});

test("trajectory window bar states which slice is loaded", () => {
  const windowed = renderTrajectoryWindowInfo({ messages: 200, total: 830, hasMore: true });
  assert.match(windowed, /已加载窗口 <strong>200<\/strong> \/ 会话共 <strong>830<\/strong> 条消息/);
  assert.match(windowed, /更早的回合尚未加载/);
  assert.match(windowed, /data-trajectory-load-earlier/);

  const complete = renderTrajectoryWindowInfo({ messages: 12, total: 12, hasMore: false });
  assert.match(complete, /已加载 <strong>12<\/strong> 条消息/);
  assert.doesNotMatch(complete, /data-trajectory-load-earlier/);
  assert.doesNotMatch(complete, /更早的回合尚未加载/);
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
  assert.equal(trajectoryKindLabel("system"), "系统");
  assert.equal(trajectoryKindLabel("unknown"), "unknown");
  assert.equal(TRAJECTORY_KINDS.length, 6);
  assert.equal(escapeHtml("<b>&\"'"), "&lt;b&gt;&amp;&quot;&#039;");
});
