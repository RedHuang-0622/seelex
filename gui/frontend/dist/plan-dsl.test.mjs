import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const source = await readFile(new URL("./plan-dsl.js", import.meta.url), "utf8");
const { planToDSL, renderPlanDSL } = await import(`data:text/javascript;base64,${Buffer.from(source).toString("base64")}`);

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

