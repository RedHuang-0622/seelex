import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const protocolSource = await readFile(new URL("./protocol.js", import.meta.url), "utf8");
const protocolURL = `data:text/javascript;base64,${Buffer.from(protocolSource).toString("base64")}`;
const clientSource = (await readFile(new URL("./client-state.js", import.meta.url), "utf8"))
  .replace('"./protocol.js"', `"${protocolURL}"`);
const { createGUIClient } = await import(`data:text/javascript;base64,${Buffer.from(clientSource).toString("base64")}`);

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
});
