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

test("switch resync: acceptSnapshot resets baseline, foreign events dropped, current applied (S0)", async () => {
  const incrementals = [];
  const snapshots = [];
  const client = createGUIClient({
    loadSnapshot: async () => makeSnapshot(1, "A"),
    onSnapshot: snapshot => snapshots.push(snapshot.session?.id),
    onIncremental: (snapshot, kind) => incrementals.push([snapshot.session?.id, kind]),
    onError: error => { throw error; }
  });

  await client.refresh({ scroll: "bottom" });

  // 后台会话 A 的事件（当前视图是 A，事件也是 A）→ 正常应用。
  await client.handleEvent({
    protocol_version: 1, seq: 1, revision: 2, session_id: "session-a",
    kind: "message.delta",
    payload: { message_id: "assistant-1", delta: "B" }
  });
  assert.deepEqual(incrementals, [["session-a", "message.delta"]]);

  // 切换到会话 B：权威基线重置（acceptSnapshot），lastSeq 归零语义由后续
  // 增量验证。
  const baselineB = {
    protocol_version: 1, revision: 10,
    session: { id: "session-b" },
    conversation: [{ id: "b-1", role: "assistant", content: "hi from B" }],
    chat: { running: true, request_id: "chat-b" },
    runtime: {}
  };
  client.acceptSnapshot(baselineB, "bottom");
  assert.equal(client.current().session.id, "session-b");

  // 旧会话 A 的迟到事件 → 丢弃（推进 seq，不触发 resync、不 upsert）。
  const dropped = await client.handleEvent({
    protocol_version: 1, seq: 11, revision: 11, session_id: "session-a",
    kind: "message.added",
    payload: { id: "a-late", role: "assistant", content: "late A" }
  });
  assert.equal(client.current().conversation.some(message => message.id === "a-late"), false);
  assert.equal(incrementals.filter(item => item[0] === "session-b").length, 0);

  // 当前会话 B 的事件 → 正常应用。
  await client.handleEvent({
    protocol_version: 1, seq: 12, revision: 12, session_id: "session-b",
    kind: "message.delta",
    payload: { message_id: "b-1", delta: "!" }
  });
  assert.equal(client.current().conversation[0].content, "hi from B!");
  assert.deepEqual(incrementals.at(-1), ["session-b", "message.delta"]);
});
