import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const protocolSource = await readFile(new URL("./protocol.js", import.meta.url), "utf8");
const protocolURL = `data:text/javascript;base64,${Buffer.from(protocolSource).toString("base64")}`;
const shapeSource = await readFile(new URL("./snapshot-shape.js", import.meta.url), "utf8");
const shapeURL = `data:text/javascript;base64,${Buffer.from(shapeSource).toString("base64")}`;
const clientSource = (await readFile(new URL("./client-state.js", import.meta.url), "utf8"))
  .replace('"./protocol.js"', `"${protocolURL}"`)
  .replace('"./snapshot-shape.js"', `"${shapeURL}"`);
const { createGUIClient } = await import(`data:text/javascript;base64,${Buffer.from(clientSource).toString("base64")}`);

function snapshotA() {
  return {
    protocol_version: 1,
    revision: 1,
    session: { id: "session-a" },
    conversation: [{ id: "assistant-a", role: "assistant", content: "A0" }],
    chat: { running: true },
    runtime: {}
  };
}

function snapshotB() {
  return {
    protocol_version: 1,
    revision: 20,
    session: { id: "session-b" },
    conversation: [{ id: "assistant-b", role: "assistant", content: "B0" }],
    chat: { running: true },
    runtime: {}
  };
}

function event(sessionID, kind, deliverySeq, revision, payload) {
  return {
    protocol_version: 1,
    seq: 100 + deliverySeq,
    delivery_seq: deliverySeq,
    revision,
    session_id: sessionID,
    kind,
    payload
  };
}

// 复现「多会话 + 单会话进行」：会话 A 的事件已把客户端 applied 水位推到 N；
// 切到会话 B 时 Bridge 重建订阅并重发基线（delivery_seq 从 1 重新计），随后
// B 的流式事件（seq 1..N）若被当成旧序号丢弃，正文就停在基线，只有下一次
// 用户交互触发的整份 refresh 才看得到新内容。
test("switch to another session resets the applied watermark so its first events apply", async () => {
  const snapshots = [];
  const incrementals = [];
  const acked = [];
  let loads = 0;
  const client = createGUIClient({
    loadSnapshot: async () => {
      loads += 1;
      return loads === 1 ? snapshotA() : snapshotB();
    },
    onSnapshot: snapshot => snapshots.push([snapshot.session.id, snapshot.conversation[0].content]),
    onIncremental: (snapshot, kind) => incrementals.push([snapshot.session.id, kind, snapshot.conversation.map(m => `${m.id}:${m.content}`)]),
    onApplied: seq => acked.push(seq),
    onError: error => { throw error; }
  });

  // 阶段 1：会话 A 是自己的订阅，事件按 delivery_seq 1..3 正常落地。
  await client.refresh({ scroll: "bottom" });
  for (const chunk of ["A1", "A2", "A3"]) {
    await client.handleEvent(event("session-a", "message.delta", Number(chunk[1]), 2, { message_id: "assistant-a", delta: chunk }));
  }
  assert.equal(client.current().session.id, "session-a");
  assert.equal(client.current().conversation[0].content, "A0A1A2A3");

  // 阶段 2：会话列表切到 B——Bridge 重订阅后发出的权威基线（session 变化）。
  client.acceptSnapshot(snapshotB(), "bottom");
  assert.equal(client.current().session.id, "session-b");
  snapshots.length = 0; // 只看切换后的增量是否落地
  incrementals.length = 0;
  acked.length = 0;

  // 阶段 3：B 的订阅从 delivery_seq=1 重新计；这些事件必须先于旧水位被接受。
  await client.handleEvent(event("session-b", "message.delta", 1, 31, { message_id: "assistant-b", delta: "B1" }));
  await client.handleEvent(event("session-b", "message.delta", 2, 31, { message_id: "assistant-b", delta: "B2" }));
  await client.handleEvent(event("session-b", "message.delta", 3, 31, { message_id: "assistant-b", delta: "B3" }));

  assert.deepEqual(incrementals, [
    ["session-b", "message.delta", ["assistant-b:B0B1"]],
    ["session-b", "message.delta", ["assistant-b:B0B1B2"]],
    ["session-b", "message.delta", ["assistant-b:B0B1B2B3"]]
  ]);
  assert.equal(client.current().conversation[0].content, "B0B1B2B3");
  assert.deepEqual(acked, [1, 2, 3], "切换后新订阅的 delivered_seq 必须回报宿主");
  assert.deepEqual(snapshots, [], "切换后的流式增量不得触发整份快照刷新");
  assert.equal(loads, 1, "切换后的流式增量不得触发整份快照重拉");
});
