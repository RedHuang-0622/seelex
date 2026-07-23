import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const protocolSource = await readFile(new URL("./protocol.js", import.meta.url), "utf8");
const protocolURL = `data:text/javascript;base64,${Buffer.from(protocolSource).toString("base64")}`;
const clientSource = (await readFile(new URL("./client-state.js", import.meta.url), "utf8"))
  .replace('"./protocol.js"', `"${protocolURL}"`);
const { createGUIClient } = await import(`data:text/javascript;base64,${Buffer.from(clientSource).toString("base64")}`);

function makeSnapshot(revision = 1, content = "A") {
  return {
    protocol_version: 1,
    revision,
    conversation: [{ id: "assistant-1", role: "assistant", content }],
    chat: { running: true, request_id: "chat-1" },
    runtime: {}
  };
}

test("uses event deltas without reloading snapshots", async () => {
  let loads = 0;
  const incrementals = [];
  const client = createGUIClient({
    loadSnapshot: async () => { loads += 1; return makeSnapshot(); },
    onSnapshot() {},
    onIncremental: (snapshot, kind) => incrementals.push([snapshot.conversation[0].content, kind]),
    onError: error => { throw error; }
  });

  await client.refresh({ scroll: "bottom" });
  await client.handleEvent({
    protocol_version: 1, seq: 1, revision: 2, request_id: "chat-1", kind: "message.delta",
    payload: { message_id: "assistant-1", delta: "B" }
  });

  assert.equal(loads, 1);
  assert.deepEqual(incrementals, [["AB", "message.delta"]]);
});

test("reloads a snapshot when an event sequence has a gap", async () => {
  let loads = 0;
  const snapshots = [];
  const client = createGUIClient({
    loadSnapshot: async () => { loads += 1; return makeSnapshot(loads, `S${loads}`); },
    onSnapshot: snapshot => snapshots.push(snapshot.conversation[0].content),
    onIncremental() {},
    onError: error => { throw error; }
  });

  await client.refresh();
  await client.handleEvent({ protocol_version: 1, seq: 3, revision: 3, kind: "message.delta" });

  assert.equal(loads, 2);
  assert.deepEqual(snapshots, ["S1", "S2"]);
});

test("ignores stale ready snapshots", () => {
  const rendered = [];
  const client = createGUIClient({
    loadSnapshot: async () => makeSnapshot(),
    onSnapshot: snapshot => rendered.push(snapshot.revision),
    onIncremental() {},
    onError: error => { throw error; }
  });

  assert.equal(client.acceptSnapshot(makeSnapshot(4), "bottom"), true);
  assert.equal(client.acceptSnapshot(makeSnapshot(3), "bottom"), false);
  assert.deepEqual(rendered, [4]);
});

test("does not replay a delta already included in a loaded snapshot", async () => {
  const incrementals = [];
  const client = createGUIClient({
    loadSnapshot: async () => makeSnapshot(4, "AB"),
    onSnapshot() {},
    onIncremental: (_snapshot, kind) => incrementals.push(kind),
    onError: error => { throw error; }
  });

  await client.refresh();
  await client.handleEvent({
    protocol_version: 1, seq: 1, revision: 4, request_id: "chat-1", kind: "message.delta",
    payload: { message_id: "assistant-1", delta: "B" }
  });

  assert.equal(client.current().conversation[0].content, "AB");
  assert.deepEqual(incrementals, []);
});
