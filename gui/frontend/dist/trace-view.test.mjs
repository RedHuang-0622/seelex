import assert from "node:assert/strict";
import test from "node:test";

import {
  TRACE_VIEW_LIMIT,
  projectTraceView,
  renderTraceView,
  renderTraceViewEmpty
} from "./trace-view.js";

function spanEvent(seq, sessionID, name, extra = {}) {
  return {
    seq,
    type: "llm.after",
    name,
    phase: "after",
    status: "success",
    trace_id: `trace-${sessionID}-${seq}`,
    attributes: { session_id: sessionID },
    ...extra
  };
}

test("T3.9 projects trace by session (data source T_i, not Π_traj)", () => {
  const payload = {
    events: [
      spanEvent(1, "sess-a", "llm-a"),
      spanEvent(2, "sess-b", "llm-b"),
      spanEvent(3, "sess-a", "llm-a-2"),
      spanEvent(4, "sess-b", "tool-b")
    ]
  };
  const viewA = projectTraceView(payload, "sess-a");
  assert.equal(viewA.total, 2);
  assert.deepEqual(viewA.items.map((item) => item.name), ["llm-a", "llm-a-2"]);

  const viewB = projectTraceView(payload, "sess-b");
  assert.equal(viewB.total, 2);
  assert.deepEqual(viewB.items.map((item) => item.name), ["llm-b", "tool-b"]);

  // 空/未指定会话：无命中（不把其它会话 trace 混入）。
  assert.equal(projectTraceView(payload, "").total, 0);
  assert.equal(projectTraceView(payload, "sess-missing").total, 0);
  // trace 视图渲染带会话隔离标记（数据源 T_i，非轨迹投影）。
  const html = renderTraceView(viewA);
  assert.match(html, /data-trace-key="trace-sess-a-1"/);
  assert.doesNotMatch(html, /llm-b/);
});

test("T3.8 renders empty state for empty trace payload", () => {
  const html = renderTraceViewEmpty();
  assert.match(html, /trace-empty/);
  assert.match(html, /暂无 trace 记录/);
  assert.equal(renderTraceView(projectTraceView(null, "sess-a")).includes("trace-empty"), true);
  assert.equal(renderTraceView(projectTraceView({ events: [] }, "sess-a")).includes("trace-empty"), true);
});

test("T3.8 bounds oversized trace view (no DOM overflow)", () => {
  const events = Array.from({ length: TRACE_VIEW_LIMIT + 50 }, (_, index) =>
    spanEvent(index + 1, "sess-big", `step-${index}`));
  const model = projectTraceView({ events }, "sess-big");
  assert.equal(model.total, TRACE_VIEW_LIMIT + 50);
  assert.equal(model.truncated, true);
  assert.equal(model.items.length, TRACE_VIEW_LIMIT);
  const html = renderTraceView(model);
  assert.match(html, /trace-banner/);
  assert.match(html, /有界截断，不溢出/);
  const rowCount = (html.match(/class="trace-row/g) || []).length;
  assert.equal(rowCount, TRACE_VIEW_LIMIT);
});

test("T1.4/T3.8 keeps duration and size bounded and escaped", () => {
  const payload = {
    events: [
      spanEvent(1, "sess-a", "<script>alert(1)</script>", {
        duration_ns: 1_200_000_000,
        total_chars: 9000
      })
    ]
  };
  const model = projectTraceView(payload, "sess-a");
  const html = renderTraceView(model);
  assert.match(html, /1\.2s/);
  assert.match(html, /8\.8 KB/);
  assert.doesNotMatch(html, /<script>/);
  assert.match(html, /&lt;script&gt;/);
});
