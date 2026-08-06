import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const source = await readFile(new URL("./plan-dsl.js", import.meta.url), "utf8");
const { planToDSL, renderPlanDSL, renderNodeDetail, renderNodeContext } = await import(`data:text/javascript;base64,${Buffer.from(source).toString("base64")}`);

function parallelPlan(status = "queued", progress = 0) {
  return {
    name: "parallel build",
    entry_node_id: "start",
    status: "running",
    progress,
    elapsed: "1.2s",
    nodes: [
      { id: "start", label: "Start", kind: "auto", status: "completed" },
      { id: "left", label: "Left branch", kind: "auto", status, output: "left output" },
      { id: "right", label: "Right branch", kind: "manual", status: "pending" },
      { id: "join", label: "Join", status: "pending" }
    ],
    edges: [
      { from: "start", to: "left" },
      { from: "start", to: "right", label: "fork" },
      { from: "left", to: "join" },
      { from: "right", to: "join", condition: { when: "approved" } }
    ]
  };
}

test("converts a parallel plan JSON graph into a stable DSL", () => {
  const queued = planToDSL(parallelPlan("queued", 0.25));
  const running = planToDSL(parallelPlan("running", 0.5));

  assert.equal(queued.schema, "seelex.plan/v1");
  assert.equal(queued.key, "start");
  assert.equal(queued.progressPercent, 25);
  assert.deepEqual(queued.nodes.map(node => node.key), running.nodes.map(node => node.key));
  assert.deepEqual(queued.edges.map(edge => edge.key), running.edges.map(edge => edge.key));
  assert.equal(running.nodes.find(node => node.id === "left").status, "running");
  assert.equal(running.nodes.find(node => node.id === "start").outgoing.length, 2);
  assert.equal(running.edges.find(edge => edge.to === "left").status, "active");
  assert.equal(running.edges.find(edge => edge.to === "join" && edge.from === "right").condition, '{"when":"approved"}');
});

test("flattens nested children while retaining hierarchy and unique keys", () => {
  const dsl = planToDSL({
    name: "nested",
    status: "pending",
    nodes: [{
      id: "fork", label: "Fork", status: "pending", children: [
        { id: "child", status: "queued" },
        { id: "child", status: "pending", children: [{ label: "leaf", status: "pending" }] }
      ]
    }]
  });

  assert.deepEqual(dsl.nodes.map(node => node.key), ["fork", "child", "child#2", "child#2/1"]);
  assert.deepEqual(dsl.nodes.map(node => node.depth), [0, 1, 1, 2]);
  assert.equal(dsl.nodes[3].parentKey, "child#2");
});

test("normalizes malformed fields without hiding dangling edges", () => {
  const dsl = planToDSL({
    name: 123,
    status: "future-state",
    progress: 9,
    nodes: [null, { label: "No ID", status: "future-node", depth: 99 }],
    edges: [{ from: "missing", to: "also-missing" }, null, {}]
  });

  assert.equal(dsl.name, "123");
  assert.equal(dsl.status, "unknown");
  assert.equal(dsl.progress, 1);
  assert.equal(dsl.nodes[0].status, "unknown");
  assert.equal(dsl.nodes[0].depth, 12);
  assert.equal(dsl.edges.length, 1);
  assert.equal(dsl.edges[0].dangling, true);
});

test("renders escaped cards, status dots, progress, and dependency edges", () => {
  const plan = parallelPlan("completed", 1);
  plan.name = '<img src=x onerror="boom">';
  plan.nodes[1].label = "<script>alert(1)</script>";
  plan.nodes[1].output = "done <b>not markup</b>";
  const html = renderPlanDSL(planToDSL(plan));

  assert.match(html, /data-plan-board/);
  assert.match(html, /aria-valuenow="100"/);
  assert.match(html, /plan-dsl-node is-completed/);
  assert.match(html, /plan-edge is-completed/);
  assert.match(html, /&lt;img src=x onerror=&quot;boom&quot;&gt;/);
  assert.match(html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
  assert.doesNotMatch(html, /<script>/);
  assert.doesNotMatch(html, /<b>not markup<\/b>/);
});

test("maps queued, running, and completed JSON updates without stale state", () => {
  const states = ["queued", "running", "completed"].map(status => {
    const dsl = planToDSL(parallelPlan(status, status === "completed" ? 0.5 : 0.25));
    return {
      key: dsl.nodes.find(node => node.id === "left").key,
      status: dsl.nodes.find(node => node.id === "left").status,
      html: renderPlanDSL(dsl)
    };
  });

  assert.deepEqual(states.map(state => state.key), ["left", "left", "left"]);
  assert.deepEqual(states.map(state => state.status), ["queued", "running", "completed"]);
  assert.match(states[0].html, /plan-dsl-node is-queued/);
  assert.match(states[1].html, /plan-dsl-node is-running/);
  assert.match(states[2].html, /plan-dsl-node is-completed/);
});

test("returns null for missing Plan JSON", () => {
  assert.equal(planToDSL(null), null);
  assert.equal(planToDSL([]), null);
  assert.equal(renderPlanDSL(null), "");
});

test("extracts node event timeline for the detail page", () => {
  const plan = {
    name: "with events",
    status: "running",
    nodes: [{
      id: "agent-1", label: "Audit module", kind: "agent", status: "running",
      events: [
        { status: "queued", at: "2026-08-02T10:00:00Z" },
        { status: "running", at: "2026-08-02T10:00:05Z", output: "started" },
        { status: "running", at: "2026-08-02T10:00:20Z" },
        { status: "completed", at: "2026-08-02T10:01:00Z", output: "done reading module" }
      ]
    }]
  };
  const dsl = planToDSL(plan);
  const node = dsl.nodes[0];
  assert.equal(node.events.length, 4);
  assert.deepEqual(node.events.map(event => event.status), ["queued", "running", "running", "completed"]);
  assert.equal(node.events[3].output, "done reading module");
  assert.equal(node.events[0].at, "2026-08-02T10:00:00Z");

  const html = renderPlanDSL(dsl);
  assert.match(html, /data-plan-node-open="agent-1"/);
  assert.match(html, /has-events/);
});

test("renders node detail timeline with escaped content", () => {
  const plan = {
    name: "detail",
    status: "completed",
    nodes: [{
      id: "n1", label: "<script>alert(1)</script>", kind: "agent", status: "completed", elapsed: "12s",
      output: "ok <b>not markup</b>",
      events: [
        { status: "queued", at: "2026-08-02T10:00:00Z" },
        { status: "completed", at: "2026-08-02T10:00:12Z", output: "done" }
      ]
    }]
  };
  const dsl = planToDSL(plan);
  const detail = renderNodeDetail({ ...dsl.nodes[0], mode: dsl.mode });

  assert.match(detail, /data-node-detail/);
  assert.match(detail, /node-event is-completed/);
  assert.match(detail, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
  assert.doesNotMatch(detail, /<script>/);
  assert.match(detail, /ok &lt;b&gt;not markup&lt;\/b&gt;/);
  assert.doesNotMatch(detail, /<b>not markup<\/b>/);
  assert.match(detail, /data-node-event-status="completed"/);
  assert.match(detail, />TASKLIST</); // 模式徽标（completed 状态 → tasklist）
});

test("renders empty timeline hint for a node without events", () => {
  const dsl = planToDSL({ name: "quiet", status: "pending", nodes: [{ id: "n1", status: "pending" }] });
  const detail = renderNodeDetail({ ...dsl.nodes[0], mode: dsl.mode });
  assert.match(detail, /暂无事件/);
});

test("renders Dify-style branch flow inside nodes (outgoing targets, conditions, parallel forks)", () => {
  const plan = parallelPlan("pending", 0);
  const dsl = planToDSL(plan);
  const start = dsl.nodes.find(node => node.id === "start");
  assert.equal(start.outgoing.length, 2); // fork ×2 → 并行分支
  const html = renderPlanDSL(dsl);
  // 分支行：目标节点名 + 条件/标签；并行箭头 ⚡→
  assert.match(html, /data-plan-node-field="branches"/);
  assert.match(html, /plan-branch-row/);
  assert.match(html, /⚡→/);
  assert.match(html, /data-branch-target="left">Left branch</);
  assert.match(html, /branch-condition">fork</);
  // 节点仍带完整 outgoing 对象（targetLabel 供树语义使用）
  assert.equal(start.outgoing[0].targetLabel, "Left branch");
  assert.equal(start.outgoing[1].label, "fork"); // 条件边在 left→join / right→join 上
  const rightToJoin = dsl.edges.find(edge => edge.from === "right" && edge.to === "join");
  assert.equal(rightToJoin.condition, '{"when":"approved"}');
  // 依赖（incoming）仍在
  assert.match(html, /plan-dependency/);
});

test("labels tasklist gate vs plan-run mode from authoritative status", () => {
  const running = planToDSL(parallelPlan("running", 0.5));
  assert.equal(running.mode, "plan");
  assert.match(renderPlanDSL(running), /plan-mode is-plan/);
  assert.match(renderPlanDSL(running), />PLAN RUN</);

  const pending = planToDSL({ ...parallelPlan("pending", 0), status: "pending" });
  assert.equal(pending.mode, "tasklist");
  const html = renderPlanDSL(pending);
  assert.match(html, /plan-mode is-tasklist/);
  assert.match(html, />TASKLIST</);
  assert.match(html, /task_check_node/);

  const completed = planToDSL({ ...parallelPlan("completed", 1), status: "completed" });
  assert.equal(completed.mode, "tasklist");
  assert.match(renderPlanDSL(completed), />TASKLIST</);
});

test("renders tasklist checkpoints and subagent tool events in the function instrumentation table", () => {
  const plan = {
    name: "instrumented", status: "pending", progress: 0.5,
    nodes: [
      {
        id: "check", label: "Check source", kind: "auto", status: "completed",
        events: [{ status: "completed", at: "2026-08-04T08:00:00Z", output: "read <main>" }]
      },
      {
        id: "worker", label: "Worker", kind: "agent", status: "running",
        tool_events: [{ id: "tool-1", node_id: "worker", name: "read_file", status: "running", arguments: "main.go", started_at: "2026-08-04T08:01:00Z" }]
      }
    ]
  };
  const dsl = planToDSL(plan);
  const board = renderPlanDSL(dsl);
  assert.match(board, /功能打点/);
  assert.match(board, /task_check_node/);
  assert.match(board, /read_file/);
  assert.match(board, /read &lt;main&gt;/);
  const detail = renderNodeDetail({ ...dsl.nodes[0], mode: dsl.mode });
  assert.match(detail, /data-node-tab="instrumentation"/);
  assert.match(detail, /task_check_node/);
});

test("renders worktree lifecycle states and bounded subagent tool activity", () => {
  const plan = {
    name: "worktree flow", status: "running", progress: 0.5,
    nodes: [{
      id: "worker", label: "Worker", kind: "agent", status: "rebasing",
      tool_events: [{
        id: "subtool-1", node_id: "worker", name: "bash", status: "error",
        arguments: "<unsafe>", error: "conflict <main>", started_at: "2026-08-04T08:00:00Z", duration: 250000000
      }]
    }]
  };
  const dsl = planToDSL(plan);
  assert.equal(dsl.nodes[0].status, "rebasing");
  assert.equal(dsl.nodes[0].toolEvents[0].name, "bash");
  assert.match(renderPlanDSL(dsl), /plan-dsl-node is-rebasing/);
  const detail = renderNodeDetail({ ...dsl.nodes[0], mode: dsl.mode });
  assert.match(detail, />REBASING</);
  assert.match(detail, /data-node-tool-id="subtool-1"/);
  assert.match(detail, /conflict &lt;main&gt;/);
  assert.doesNotMatch(detail, /<unsafe>/);
  assert.match(detail, /250ms/);
});

test("renders the context tab placeholder in the node detail modal", () => {
  const dsl = planToDSL({ name: "ctx", status: "running", nodes: [{ id: "agent-1", kind: "agent", status: "running" }] });
  const detail = renderNodeDetail({ ...dsl.nodes[0], mode: dsl.mode });
  assert.match(detail, /data-node-tab="context"/);
  assert.match(detail, /data-node-panel="context"/);
  assert.match(detail, /data-node-context/);
  assert.match(detail, /加载上下文快照/);
});

test("renders subagent context snapshot with escaped content", () => {
  const html = renderNodeContext({
    goal: "<script>alert(1)</script>",
    progress: "60%",
    message_count: 12,
    token_estimate: 1234,
    findings: ["found <b>race</b>"],
    decisions: [{ what: "use mutex", why: "because <i>reason</i>" }],
    constraints: ["no merge"],
    pending_work: ["run tests"]
  });
  assert.match(html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
  assert.doesNotMatch(html, /<script>/);
  assert.match(html, /found &lt;b&gt;race&lt;\/b&gt;/);
  assert.doesNotMatch(html, /<b>race<\/b>/);
  assert.match(html, /use mutex — because &lt;i&gt;reason&lt;\/i&gt;/);
  assert.match(html, /60%/);
  assert.match(html, /12/);
  assert.match(html, /1,234/);
  assert.match(html, /no merge/);
  assert.match(html, /run tests/);
});

test("renders empty context placeholder for missing or empty snapshots", () => {
  assert.match(renderNodeContext(null), /无上下文快照/);
  assert.match(renderNodeContext(undefined), /无上下文快照/);
  assert.match(renderNodeContext({}), /上下文快照为空/);
});
