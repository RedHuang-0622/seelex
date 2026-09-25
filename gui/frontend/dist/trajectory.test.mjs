import assert from "node:assert/strict";
import test from "node:test";

import {
  AXIS_LANES,
  AXIS_PAGE_SIZE_DEFAULT,
  AXIS_PAGE_SIZE_STEPS,
  AXIS_WIDE_MIN_SLOT_PERCENT,
  TRAJECTORY_KINDS,
  axisBlocks,
  axisPageForIndex,
  axisPageWindow,
  axisWheelStep,
  buildTrajectory,
  filterTrajectory,
  trajectoryStats,
  trajectoryKindLabel,
  normalizeAxisPageSize,
  renderTrajectoryFilters,
  renderTrajectorySummary,
  renderTrajectoryTable,
  renderTrajectoryRow,
  renderContextAxis,
  renderTrajectoryWindowInfo,
  renderAxisDetail,
  prefixLayerSegments,
  compactionMarks,
  resolveAxisPage,
  stepAxisPageSize,
  escapeHtml,
  compactionRangeText
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
  const compressBlock = html.match(/<button type="button" class="axis-segment is-compress[^"]*"[^>]*>/)?.[0] || "";
  assert.doesNotMatch(compressBlock, /data-trajectory-key/);
  assert.match(compressBlock, /data-compact-idx="0"/);
  // 折叠帧的先后层次靠灰阶：栈顶（前沿）深灰、被更晚折叠取代的浅灰。前沿的判定
  // 只看被压区间终点（compactionFrontier），这条记录没有消息边界 ⇒ 判不出前沿，
  // 刻度留在浅灰——不用别的量（时间/版本号）顶替，否则三处读数会分叉。
  assert.match(compressBlock, /is-stale/);
  const framed = [
    { version: 1, reason: "context_budget", message_from: "message-1", message_to: "message-9", compacted_at: "2026-08-25T10:00:01Z" },
    { version: 2, reason: "context_budget", message_from: "message-1", message_to: "message-20", compacted_at: "2026-08-25T10:00:02Z" }
  ];
  const framedHTML = renderContextAxis(records, { compactions: framed });
  const frontierBlock = framedHTML.match(/<button type="button" class="axis-segment is-compress is-frontier"[^>]*>/)?.[0] || "";
  const staleBlocks = framedHTML.match(/<button type="button" class="axis-segment is-compress is-stale"[^>]*>/g) || [];
  assert.ok(frontierBlock, "最新一次折叠的刻度没标 is-frontier");
  assert.equal(staleBlocks.length, 1, "过时折叠的刻度数不对（灰阶会把读序带错）");
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

test("axis detail renders compaction range, origin and read-back entry", () => {
  const records = buildTrajectory([userMessage("u1", "hi")]);
  const compactions = [
    {
      version: 3, reason: "context_budget", origin: "explicit_after_turn", messages_before: 88,
      estimated_tokens: 200_000, compacted_at: "2026-08-25T10:00:01Z",
      message_from: "message-1", message_to: "message-8", event_from: 1, event_to: 6,
      frame_ref: "tr-frame", frame_bytes: 512, frame_tokens: 160
    }
  ];
  const mark = compactionMarks(records, compactions)[0];
  const html = renderAxisDetail({ type: "compression", mark });
  assert.match(html, /上下文压缩 #3/);
  assert.match(html, /消息 message-1\.\.message-8（事件 1\.\.6）/);
  assert.match(html, /来源 显式要求（回合后）/);
  assert.match(html, /200,000/);
  assert.match(html, /tr-frame/);
  // 前端自己的入口：不再把用户甩给模型侧工具（read_compressed_turn）。
  assert.match(html, /data-compact-frame-load="first"/);
  assert.doesNotMatch(html, /read_compressed_turn/);
  // messages_before 是装配前的引擎历史条数，不得冒充"压缩前 88 条消息"。
  assert.doesNotMatch(html, /88 条/);
});

test("axis detail reads back the folded frame body page by page", () => {
  const records = buildTrajectory([userMessage("u1", "hi")]);
  const compactions = [
    { version: 4, reason: "context_budget", frame_ref: "tr-frame", frame_bytes: 90, frame_tokens: 20 }
  ];
  const mark = compactionMarks(records, compactions)[0];
  const loading = renderAxisDetail({ type: "compression", mark, frame: { loading: true, text: "" } });
  assert.match(loading, /读取中…/);
  const first = renderAxisDetail({
    type: "compression", mark,
    frame: { loading: false, error: "", text: "# Context checkpoint frame v4\nfolded: 消息 message-1..message-8", hasMore: true, nextOffset: 40, totalBytes: 90 }
  });
  assert.match(first, /折叠帧正文/);
  assert.match(first, /Context checkpoint frame v4/);
  assert.match(first, /data-compact-frame-load="more"/);
  assert.match(first, /剩余约 50 bytes/);
  const last = renderAxisDetail({
    type: "compression", mark,
    frame: { loading: false, error: "", text: "tail", hasMore: false, nextOffset: 90, totalBytes: 90 }
  });
  assert.match(last, /已加载完/);
  assert.doesNotMatch(last, /data-compact-frame-load="more"/);
});

test("axis detail says so when a record has no frame body", () => {
  const records = buildTrajectory([userMessage("u1", "hi")]);
  const mark = compactionMarks(records, [{ version: 1, reason: "context_budget" }])[0];
  const html = renderAxisDetail({ type: "compression", mark });
  assert.match(html, /本次没有可回读正文/);
});

test("compactionRangeText keeps to recorded boundaries", () => {
  assert.equal(
    compactionRangeText({ message_from: "message-1", message_to: "message-8", event_from: 1, event_to: 6 }),
    "消息 message-1..message-8（事件 1..6）"
  );
  assert.equal(compactionRangeText({ message_from: "message-3", message_to: "message-3" }), "消息 message-3");
  assert.equal(compactionRangeText({ event_from: 2, event_to: 5 }), "事件 2..5");
  assert.equal(compactionRangeText({ messages_before: 88 }), "");
});

test("compression cut line marks where the prefix was folded", () => {
  const records = buildTrajectory([userMessage("u1", "hi"), llmMessage("a1", "hello")]);
  const compactions = [
    { version: 5, reason: "context_budget", message_from: "message-1", message_to: "message-2", event_from: 1, event_to: 4,
      compacted_at: "2026-08-25T10:00:01Z" }
  ];
  const html = renderContextAxis(records, { compactions });
  assert.match(html, /axis-compress-cut/);
  assert.match(html, /以上 消息 message-1\.\.message-2（事件 1\.\.4）已被折叠/);
  assert.match(html, /虚线以上已被折叠/);
});

test("compression cut line stays out of clamped ticks", () => {
  // 压缩点早于已加载窗口：刻度被钳到轴起点，位置不真实 → 不画分界虚线（只留刻度），
  // 提示里写明"分界早于已加载窗口"——不用别的刻度顶替。
  const records = buildTrajectory([userMessage("u1", "hi")]);
  const compactions = [
    { version: 1, reason: "context_budget", compacted_at: "2020-01-01T00:00:00Z", message_from: "message-9", message_to: "message-9" }
  ];
  const marks = compactionMarks(records, compactions, { pageSize: 1 });
  assert.equal(marks[0].anchored, false);
  const html = renderContextAxis(records, { compactions, pageSize: 1 });
  assert.doesNotMatch(html, /axis-compress-cut/);
  assert.match(html, /会话压缩分界早于已加载窗口/);
});

test("axis draws one cut line for the session frontier, not one per record", () => {
  // 分界是会话单例：被折出保留窗口的最后一条消息（message_to 序号最大者）。历次压缩
  // 各画一条线，读者会以为两条线之间那段还给模型（其实早折掉了）——所以分界虚线恒为
  // 一条，且区间取整段已折出的上下文（起点最早、终点最新：只报最后一段会漏掉更早的）。
  const records = buildTrajectory([
    userMessage("u1", "hi"),
    llmMessage("a1", "hello"),
    userMessage("u2", "again"),
    llmMessage("a2", "ok")
  ]);
  const compactions = [
    { version: 1, reason: "context_budget", message_from: "message-1", message_to: "message-2", event_from: 1, event_to: 2, compacted_at: "2026-08-25T10:00:01Z" },
    { version: 2, reason: "context_budget", message_from: "message-3", message_to: "message-4", event_from: 3, event_to: 4, compacted_at: "2026-08-25T10:00:02Z" }
  ];
  const html = renderContextAxis(records, { compactions });
  assert.equal((html.match(/class="axis-compress-cut"/g) || []).length, 1);
  assert.match(html, /以上 消息 message-1\.\.message-4（事件 1\.\.4）已被折叠/);
  // 两个刻度都还在（历次记录可逐条查看），但只有前沿那个被标记为分界。
  const marks = compactionMarks(records, compactions, {});
  assert.equal(marks.length, 2);
  assert.equal(marks.filter(mark => mark.isFrontier).length, 1);
  assert.equal(marks[1].isFrontier, true);
  assert.equal(marks[0].isFrontier, undefined);
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

test("axis lane order is fixed, derived from the response-type table, and self-explaining", () => {
  // 轨序 = 响应类型表顺序（单一事实源，不会两处漂移）
  assert.deepEqual(AXIS_LANES.map(lane => lane.kind), ["input", "llm", "tool", "error", "system", "notice"]);
  assert.deepEqual(AXIS_LANES.map(lane => lane.kind), TRAJECTORY_KINDS.map(entry => entry.kind));
  for (const lane of AXIS_LANES) assert.ok(lane.why.length > 0, `${lane.kind} 轨必须有一句话语义`);

  const records = buildTrajectory([userMessage("u1", "hi"), llmMessage("a1", "hello")]);
  const html = renderContextAxis(records, {
    prefixLayers: [{ kind: "identity", name: "seelex", text: "你是 Seelex。" }],
    compactions: [{ version: 1, reason: "context_budget", compacted_at: "2026-08-25T10:00:00Z" }]
  });
  // 固定顺序：前缀注入（元数据轨，最上）→ 六条类型轨 → 压缩（元数据轨，最下）
  let cursor = -1;
  for (const marker of [
    "context-axis-lane is-prefix",
    ...AXIS_LANES.map(lane => `context-axis-lane is-${lane.kind}`),
    "context-axis-lane is-compress"
  ]) {
    const at = html.indexOf(marker);
    assert.ok(at > cursor, `${marker} 必须出现在上一条轨之后`);
    cursor = at;
  }
  // 轨标签 title 带上"这一轨是什么"的一句话说明
  assert.match(html, /title="输入：轮次起点：人在这一轮发出的请求/);
});

test("axis blocks enter the lane of their response kind and keep global order", () => {
  const records = buildTrajectory([
    userMessage("u1", "hi"),
    llmMessage("a1", "hello"),
    toolStart("call-1", "bash", "{}"),
    toolEnd("call-1", "bash", "done"),
    errorMessage("e1", "boom")
  ]);
  const { lanes, window } = axisBlocks(records, { pageSize: 16, page: 0 });
  const lane = kind => lanes.find(entry => entry.kind === kind);
  assert.deepEqual(lane("input").blocks.map(block => block.index), [0]);
  assert.deepEqual(lane("llm").blocks.map(block => block.index), [1]);
  assert.deepEqual(lane("tool").blocks.map(block => block.index), [2]);
  assert.deepEqual(lane("error").blocks.map(block => block.index), [3]);
  // 本段没有块的轨保留占位（空轨是"看得见的结论"，不是轨消失）
  assert.equal(lane("system").empty, true);
  assert.equal(lane("notice").empty, true);
  // 轮次只进 tooltip、不参与分轨：第一个输入块之后都算第 1 轮
  assert.deepEqual(lane("input").blocks.map(block => block.turn), [1]);
  assert.deepEqual(lane("llm").blocks.map(block => block.turn), [1]);
  // 单页装得下 → 整段铺满横轴
  assert.deepEqual([window.total, window.pageCount, window.capacity, window.slot], [4, 1, 4, 25]);
});

test("axis block geometry is slot-only: position and width never encode volume", () => {
  const records = buildTrajectory([
    userMessage("u1", "短问题"),
    llmMessage("a1", "x".repeat(4000)),
    toolStart("call-1", "bash", "y".repeat(2000)),
    toolEnd("call-1", "bash", "z".repeat(3000))
  ]);
  const { lanes, window } = axisBlocks(records, { pageSize: 16, page: 0 });
  const blocks = ["input", "llm", "tool"].map(kind => lanes.find(entry => entry.kind === kind).blocks[0]);
  // 体量差上千倍，几何完全一致：x = 页内序号 × 槽宽，宽 = 槽宽
  blocks.forEach((block, slotIndex) => {
    assert.ok(Math.abs(block.x - slotIndex * window.slot) < 1e-9);
    assert.equal(block.width, window.slot);
  });
  const html = renderContextAxis(records);
  const widths = [...html.matchAll(/--w:([0-9.]+)%/g)].map(match => Number(match[1]));
  assert.deepEqual(new Set(widths), new Set([Number((100 / 3).toFixed(3))]));
  // 槽宽 33.3% ≥ 6% → 每块都直接显示标签
  assert.equal((html.match(/ is-wide/g) || []).length, 3);
});

test("axis wide-label rule follows slot width, not content volume", () => {
  const records = buildTrajectory(Array.from({ length: 40 }, (unused, index) => userMessage(`u${index}`, `第 ${index} 条`)));
  // 页大小 16 → 槽宽 6.25% ≥ AXIS_WIDE_MIN_SLOT_PERCENT → 本页 16 块都显示标签
  const small = renderContextAxis(records, { pageSize: 16 });
  assert.equal((small.match(/ is-wide/g) || []).length, 16);
  assert.ok(100 / 16 >= AXIS_WIDE_MIN_SLOT_PERCENT);
  // 页大小 128（单页装 40 条）→ 槽宽 2.5% → 不显示标签（悬停才显示）
  const large = renderContextAxis(records, { pageSize: 128 });
  assert.equal((large.match(/ is-wide/g) || []).length, 0);
});

test("axis block tooltip explains why the block sits there in one sentence", () => {
  const records = buildTrajectory([userMessage("u1", "hi"), llmMessage("a1", "hello")]);
  const html = renderContextAxis(records);
  assert.match(html, /输入轨 · 输入 · /);
  assert.match(html, /体量 \d+ 字符（不参与位置）/);
  assert.match(html, /第 1 轮/);
  assert.match(html, /本页第 1 槽 \/ 共 2 槽/);
  assert.match(html, /全局第 1 条/);
});

test("axis page window splits records into fixed-size pages with honest last page", () => {
  assert.deepEqual(
    { ...axisPageWindow(40, 16, 0) },
    { total: 40, pageSize: 16, pageCount: 3, page: 0, capacity: 16, start: 0, end: 16, slot: 6.25 }
  );
  const last = axisPageWindow(40, 16, 2);
  assert.deepEqual([last.start, last.end, last.pageCount], [32, 40, 3]);
  // 页号越界/负数一律钳位（页数变小时不会停在空白页）
  assert.equal(axisPageWindow(40, 16, 99).page, 2);
  assert.equal(axisPageWindow(40, 16, -5).page, 0);
  // 单页装得下 → 容量=总条数、整段铺满横轴
  const single = axisPageWindow(5, 16, 0);
  assert.deepEqual([single.pageCount, single.capacity, single.start, single.end, single.slot], [1, 5, 0, 5, 20]);
  // 非法页大小归一到默认档
  assert.equal(axisPageWindow(40, 7, 0).pageSize, AXIS_PAGE_SIZE_DEFAULT);
  // 空数据不除零，保持一页空窗口
  const empty = axisPageWindow(0, 16, 3);
  assert.deepEqual([empty.pageCount, empty.page, empty.start, empty.end], [1, 0, 0, 0]);
});

test("resolveAxisPage prefers the content anchor, then the tail, then the requested page", () => {
  const records = Array.from({ length: 40 }, (unused, index) => ({ key: `k${index}` }));
  // 尾页跟随：停在最后一页（新记录到达时继续跟随）
  assert.equal(resolveAxisPage(records, { pageSize: 16, tail: true }).page, 2);
  // 锚点优先：加载更早内容把记录整体前移，视图跟着原记录走（不跳位）
  const shifted = [{ key: "earlier" }, ...records];
  const anchored = resolveAxisPage(shifted, { pageSize: 16, anchorKey: "k16", page: 2 });
  assert.deepEqual([anchored.page, anchored.start, anchored.end], [1, 16, 32]);
  const anchorIndex = shifted.findIndex(record => record.key === "k16");
  assert.ok(anchorIndex >= anchored.start && anchorIndex < anchored.end, "锚点记录必须仍在本页窗口内");
  // 锚点消失 → 回到期望页号（钳位）
  assert.equal(resolveAxisPage(shifted, { pageSize: 16, anchorKey: "missing", page: 2 }).page, 2);
  // 序号 → 页号
  assert.deepEqual(
    [axisPageForIndex(0, 40, 16), axisPageForIndex(16, 40, 16), axisPageForIndex(39, 40, 16), axisPageForIndex(999, 40, 16)],
    [0, 1, 2, 2]
  );
});

test("shift page-size ladder steps one notch and clamps visibly at both ends", () => {
  assert.deepEqual(AXIS_PAGE_SIZE_STEPS, [8, 16, 32, 64, 128]);
  assert.equal(AXIS_PAGE_SIZE_DEFAULT, 16);
  assert.equal(normalizeAxisPageSize(32), 32);
  assert.equal(normalizeAxisPageSize("16"), 16);
  assert.equal(normalizeAxisPageSize(999), AXIS_PAGE_SIZE_DEFAULT);
  const up = stepAxisPageSize(16, 1);
  assert.deepEqual([up.size, up.previous, up.changed, up.atMax], [32, 16, true, false]);
  const down = stepAxisPageSize(8, -1);
  assert.deepEqual([down.size, down.changed, down.atMin], [8, false, true]);
  const max = stepAxisPageSize(128, 1);
  assert.deepEqual([max.size, max.changed, max.atMax], [128, false, true]);
  // 非法输入先归一到默认档再步进
  assert.equal(stepAxisPageSize(7, 1).size, 32);
});

test("axis wheel delta accumulates into one page turn per gesture", () => {
  assert.equal(axisWheelStep(0, 12).accumulated, 12);
  assert.equal(axisWheelStep(12, 12).step, 0);
  const turn = axisWheelStep(24, 20);
  assert.deepEqual(turn, { accumulated: 0, step: 1 });
  // 反向手势清零：滚轮来回抖不翻页
  assert.deepEqual(axisWheelStep(30, -5), { accumulated: -5, step: 0 });
  assert.equal(axisWheelStep(0, -60).step, -1);
  // 一次大 delta 只翻一页（不按像素数翻多页）
  assert.equal(axisWheelStep(0, 480).step, 1);
  assert.deepEqual(axisWheelStep(0, 0), { accumulated: 0, step: 0 });
});

test("compaction marks align to the anchored record slot and clamp at page edges", () => {
  // 16 条记录、页大小 8（档位下限）→ 两页，每槽 12.5%
  const records = buildTrajectory(Array.from({ length: 16 }, (unused, index) => ({
    id: `u${index}`,
    role: "user",
    content: `第 ${index} 条`,
    created_at: new Date(Date.UTC(2026, 7, 25, 10, 0, index)).toISOString()
  })));
  assert.equal(records.length, 16);
  const stamp = index => new Date(Date.UTC(2026, 7, 25, 10, 0, index)).toISOString();
  // 单页装得下（默认页大小 16）：容量=16、槽宽 6.25%，刻度=锚定记录（index 5）槽位右边界
  const single = compactionMarks(records, [{ version: 1, reason: "context_budget", compacted_at: stamp(5) }], { pageSize: 16, page: 0 })[0];
  assert.deepEqual([single.anchored, single.offPage, single.anchorIndex, single.x, single.anchorPage], [true, "", 5, 37.5, 0]);
  // 分页（每页 8 条）第 1 页：锚点 5 在本页内 → 刻度落本页槽位右边界 (5+1)×12.5
  const inPage = compactionMarks(records, [{ version: 1, reason: "context_budget", compacted_at: stamp(5) }], { pageSize: 8, page: 0 })[0];
  assert.deepEqual([inPage.x, inPage.offPage], [75, ""]);
  // 锚点在本页之前 → 钳到左边界并标注锚点页（点击跳页，不静默位移）
  const before = compactionMarks(records, [{ version: 1, reason: "context_budget", compacted_at: stamp(3) }], { pageSize: 8, page: 1 })[0];
  assert.deepEqual([before.x, before.offPage, before.anchorPage], [0, "before", 0]);
  // 锚点在本页之后 → 钳到右边界
  const later = compactionMarks(records, [{ version: 2, reason: "context_budget", compacted_at: stamp(9) }], { pageSize: 8, page: 0 })[0];
  assert.deepEqual([later.x, later.offPage, later.anchorPage], [100, "after", 1]);
  // 锚点早于已加载窗口 → 钳到轴起点并注明（旧行为保持）
  const unknown = compactionMarks(records, [{ version: 3, reason: "context_budget", compacted_at: "2026-08-01T00:00:00Z" }], { pageSize: 8, page: 1 })[0];
  assert.deepEqual([unknown.x, unknown.anchored, unknown.offPage], [0, false, "unknown"]);
  // 同一锚点上的多个刻度：同一 x 但按 stack 朝轨道内侧错位（每个都点得到）
  const duplicated = [
    { version: 4, reason: "context_budget", compacted_at: stamp(5) },
    { version: 5, reason: "large_tool_output", compacted_at: stamp(5) }
  ];
  assert.deepEqual(compactionMarks(records, duplicated, { pageSize: 8, page: 0 }).map(mark => [mark.x, mark.stack]), [[75, 0], [75, 1]]);
  const stacked = renderContextAxis(records, { compactions: duplicated, pageSize: 8, page: 0 });
  assert.match(stacked, /--x:75\.000%/);
  assert.match(stacked, /transform:translateX\(calc\(-50% \+ 6px\)\)/);
});

test("axis head reports page, range, page size and transient notice", () => {
  const records = buildTrajectory(Array.from({ length: 40 }, (unused, index) => userMessage(`u${index}`, "x")));
  const html = renderContextAxis(records, { page: 1, pageSize: 16, notice: "页大小 16 → 32 条", hasMore: true });
  assert.match(html, /data-axis-page="1"/);
  assert.match(html, /data-axis-page-count="3"/);
  assert.match(html, /data-axis-page-size="16"/);
  assert.match(html, /第 2\/3 页/);
  assert.match(html, /第 17–32 条/);
  assert.match(html, /共 40 条/);
  assert.match(html, /滚轮翻页，Shift\+滚轮调页大小/);
  assert.match(html, /data-axis-note>页大小 16 → 32 条/);
  // 只画本页的块：第 2 页的 data-axis-index ∈ [16, 32)
  const indexes = [...html.matchAll(/data-axis-index="(\d+)"/g)].map(match => Number(match[1]));
  assert.equal(indexes.length, 16);
  assert.ok(indexes.every(index => index >= 16 && index < 32));
  // 最早一页且有未加载的更早回合 → 明确提示（不让人以为轴丢了早期内容）
  assert.match(renderContextAxis(records, { page: 0, pageSize: 16, hasMore: true }), /更早的回合尚未加载/);
  assert.doesNotMatch(renderContextAxis(records, { page: 0, pageSize: 16, hasMore: false }), /更早的回合尚未加载/);
});

test("prefix lane is page-independent while record lanes follow the page", () => {
  const layers = [{ kind: "identity", name: "seelex", text: "你是 Seelex。" }];
  const records = buildTrajectory(Array.from({ length: 40 }, (unused, index) => userMessage(`u${index}`, "x")));
  const page0 = renderContextAxis(records, { prefixLayers: layers, page: 0, pageSize: 16 });
  const page1 = renderContextAxis(records, { prefixLayers: layers, page: 1, pageSize: 16 });
  const prefixGeometry = html => (html.match(/class="axis-segment is-prefix[^"]*" style="--x:([0-9.]+)%;--w:([0-9.]+)%"/) || []).slice(1);
  assert.equal(prefixGeometry(page0).length, 2);
  assert.deepEqual(prefixGeometry(page0), prefixGeometry(page1));
  assert.match(page0, /data-axis-index="0"/);
  assert.doesNotMatch(page0, /data-axis-index="16"/);
  assert.match(page1, /data-axis-index="16"/);
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
