import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const protocolSource = await readFile(new URL("./protocol.js", import.meta.url), "utf8");
const protocolURL = `data:text/javascript;base64,${Buffer.from(protocolSource).toString("base64")}`;
const clientSource = (await readFile(new URL("./client-state.js", import.meta.url), "utf8"))
  .replace('"./protocol.js"', `"${protocolURL}"`);
const { createGUIClient } = await import(`data:text/javascript;base64,${Buffer.from(clientSource).toString("base64")}`);
const markdownSource = await readFile(new URL("./markdown.js", import.meta.url), "utf8");
const markdownURL = `data:text/javascript;base64,${Buffer.from(markdownSource).toString("base64")}`;
const componentSource = (await readFile(new URL("./components.js", import.meta.url), "utf8"))
  .replace('"./markdown.js"', `"${markdownURL}"`);
const { renderConversationModel } = await import(`data:text/javascript;base64,${Buffer.from(componentSource).toString("base64")}`);
const planSource = await readFile(new URL("./plan-dsl.js", import.meta.url), "utf8");
const { planToDSL, renderPlanDSL } = await import(`data:text/javascript;base64,${Buffer.from(planSource).toString("base64")}`);

test("relays a completed main-agent tool result through the GUI reducer and renderer", async () => {
  const listeners = new Map();
  const runtime = {
    EventsOn(name, listener) { listeners.set(name, listener); },
    async emit(name, payload) { await listeners.get(name)?.(payload); }
  };
  let loads = 0;
  const incrementals = [];
  const initial = {
    protocol_version: 1,
    revision: 1,
    conversation: [{ id: "assistant-1", role: "assistant", content: "" }],
    conversation_window: 50,
    total_messages: 1,
    chat: { running: true, request_id: "request-1" },
    runtime: {}
  };
  const client = createGUIClient({
    loadSnapshot: async () => { loads += 1; return initial; },
    onSnapshot() {},
    onIncremental: (_snapshot, kind) => incrementals.push(kind),
    onError: error => { throw error; }
  });
  runtime.EventsOn("seelex:event", event => client.handleEvent(event));
  await client.refresh();

  await runtime.emit("seelex:event", {
    protocol_version: 1, seq: 1, revision: 2, request_id: "request-1", kind: "tool.started",
    payload: {
      id: "message-tool-start", role: "tool",
      tool: { id: "tool-1", name: "bash", arguments: '{"command":"pwd"}', status: "running" }
    }
  });
  const result = '{"stdout":"C:\\\\workspace","stderr":"","exit_code":0}';
  await runtime.emit("seelex:event", {
    protocol_version: 1, seq: 2, revision: 3, request_id: "request-1", kind: "tool.completed",
    payload: {
      id: "message-tool-result", role: "tool_result", content: result,
      tool: { id: "tool-1", name: "bash", result, status: "success", duration: 12000000 }
    }
  });
  await runtime.emit("seelex:event", {
    protocol_version: 1, seq: 3, revision: 4, request_id: "request-1", kind: "runtime.changed",
    payload: { ...client.current().runtime, full_access: true }
  });

  const current = client.current();
  const rendered = renderConversationModel(current.conversation, current.chat);
  const tool = rendered.items.find(item => item.key === "tool:tool-1");
  assert.equal(loads, 1, "main tool events must not fall back to Snapshot reloads");
  assert.deepEqual(incrementals, ["tool.started", "tool.completed", "runtime.changed"]);
  assert.equal(current.runtime.full_access, true, "authoritative full-access state must reach the frontend");
  assert.ok(tool, "completed tool card must retain the started tool key");
  assert.deepEqual(JSON.parse(rendered.payloads.get("tool:tool-1-out")), JSON.parse(result));
  assert.doesNotMatch(tool.html, /Waiting for output/);
  assert.match(tool.html, /tool-run is-success/);
});

test("relays mocked seelex:event subagent activity through the GUI client reducer", async () => {
  const listeners = new Map();
  const runtime = {
    EventsOn(name, listener) { listeners.set(name, listener); },
    async emit(name, payload) { await listeners.get(name)?.(payload); }
  };
  let loads = 0;
  const incrementals = [];
  const initial = {
    protocol_version: 1,
    revision: 1,
    conversation: [],
    conversation_window: 50,
    total_messages: 0,
    chat: {},
    runtime: {
      plan: {
        status: "running", progress: 0,
        nodes: [{ id: "root", status: "running", children: [{ id: "worker", status: "queued", tool_events: [] }] }],
        edges: []
      }
    }
  };
  const client = createGUIClient({
    loadSnapshot: async () => { loads += 1; return initial; },
    onSnapshot() {},
    onIncremental: (_snapshot, kind) => incrementals.push(kind),
    onError: error => { throw error; }
  });
  runtime.EventsOn("seelex:event", event => client.handleEvent(event));
  await client.refresh();

  await runtime.emit("seelex:event", {
    protocol_version: 1, seq: 1, revision: 2, kind: "subagent.changed",
    payload: {
      node_id: "worker", plan_status: "running", progress: 0.5,
      node: { id: "worker", status: "running", tool_events: [], children: [] }
    }
  });
  await runtime.emit("seelex:event", {
    protocol_version: 1, seq: 2, revision: 3, kind: "subagent.tool.started",
    payload: { id: "subtool-1", node_id: "worker", name: "bash", arguments: "{}", status: "running" }
  });
  await runtime.emit("seelex:event", {
    protocol_version: 1, seq: 3, revision: 4, kind: "subagent.tool.completed",
    payload: { id: "subtool-1", node_id: "worker", name: "bash", status: "success", result: "ok" }
  });

  const worker = client.current().runtime.plan.nodes[0].children[0];
  assert.equal(loads, 1, "incremental event chain must not fall back to Snapshot reloads");
  assert.equal(worker.status, "running");
  assert.deepEqual(worker.tool_events.map(event => [event.name, event.status, event.result]), [["bash", "success", "ok"]]);
  assert.deepEqual(incrementals, ["subagent.changed", "subagent.tool.started", "subagent.tool.completed"]);
  const renderedPlan = renderPlanDSL(planToDSL(client.current().runtime.plan));
  assert.match(renderedPlan, /功能打点/);
  assert.match(renderedPlan, /bash/);
  assert.match(renderedPlan, /SUCCESS/);
});

test("relays worktable.changed without falling back to a snapshot reload", async () => {
  const listeners = new Map();
  const runtime = {
    EventsOn(name, listener) { listeners.set(name, listener); },
    async emit(name, payload) { await listeners.get(name)?.(payload); }
  };
  let loads = 0;
  const incrementals = [];
  const initial = {
    protocol_version: 1,
    revision: 1,
    conversation: [],
    conversation_window: 50,
    total_messages: 0,
    chat: {},
    runtime: { work_table: [] }
  };
  const client = createGUIClient({
    loadSnapshot: async () => { loads += 1; return initial; },
    onSnapshot() {},
    onIncremental: (_snapshot, kind) => incrementals.push(kind),
    onError: error => { throw error; }
  });
  runtime.EventsOn("seelex:event", event => client.handleEvent(event));
  await client.refresh();

  await runtime.emit("seelex:event", {
    protocol_version: 1, seq: 1, revision: 2, kind: "worktable.changed",
    payload: {
      items: [
        { id: "plan:n1", phase: "plan", task: "调研", status: "running", kind: "plan" },
        { id: "todo:0", phase: "tasklist", task: "写测试", status: "doing", kind: "todo" },
        { id: "subagent:s1", phase: "subagent", task: "g", status: "running", kind: "subagent" }
      ]
    }
  });

  const current = client.current();
  assert.equal(loads, 1, "worktable.changed must not trigger a Snapshot reload");
  assert.deepEqual(incrementals, ["worktable.changed"]);
  assert.equal(current.runtime.work_table.length, 3);
  assert.equal(current.runtime.work_table[1].status, "doing");
});
