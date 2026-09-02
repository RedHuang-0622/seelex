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
    session: { id: "session-a" },
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
    protocol_version: 1, seq: 1, delivery_seq: 1, revision: 2, request_id: "chat-1", kind: "message.delta",
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
  await client.handleEvent({ protocol_version: 1, seq: 3, delivery_seq: 3, revision: 3, kind: "message.delta" });

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
    protocol_version: 1, seq: 1, delivery_seq: 1, revision: 4, request_id: "chat-1", kind: "message.delta",
    payload: { message_id: "assistant-1", delta: "B" }
  });

  assert.equal(client.current().conversation[0].content, "AB");
  assert.deepEqual(incrementals, []);
});

test("switch resync: acceptSnapshot resets the baseline and view increments keep applying", async () => {
  const incrementals = [];
  const snapshots = [];
  let loads = 0;
  const client = createGUIClient({
    loadSnapshot: async () => { loads += 1; return makeSnapshot(1, "A"); },
    onSnapshot: snapshot => snapshots.push(snapshot.session?.id),
    onIncremental: (snapshot, kind) => incrementals.push([snapshot.session?.id, kind]),
    onError: error => { throw error; }
  });

  await client.refresh({ scroll: "bottom" });

  await client.handleEvent({
    protocol_version: 1, seq: 1, delivery_seq: 1, revision: 2, session_id: "session-a",
    kind: "message.delta",
    payload: { message_id: "assistant-1", delta: "B" }
  });
  assert.deepEqual(incrementals, [["session-a", "message.delta"]]);

  // 切换到会话 B：权威基线重置（acceptSnapshot）。
  const baselineB = {
    protocol_version: 1, revision: 10,
    session: { id: "session-b" },
    conversation: [{ id: "b-1", role: "assistant", content: "hi from B" }],
    chat: { running: true, request_id: "chat-b" },
    runtime: {}
  };
  client.acceptSnapshot(baselineB, "bottom");
  assert.equal(client.current().session.id, "session-b");

  // 基线之后视图会话的增量继续生效，且不额外触发快照重载。
  await client.handleEvent({
    protocol_version: 1, seq: 12, delivery_seq: 2, revision: 12, session_id: "session-b",
    kind: "message.delta",
    payload: { message_id: "b-1", delta: "!" }
  });
  assert.equal(client.current().conversation[0].content, "hi from B!");
  assert.deepEqual(incrementals.at(-1), ["session-b", "message.delta"]);
  assert.equal(loads, 1, "切换后的增量不得回落到快照重载");

  // 旧会话 A 的迟到事件不会出现在这里：会话归属由 application 在投递端
  // 过滤，前端不判定 session_id（正向防线见 gui/bridge_session_test.go）。
});
