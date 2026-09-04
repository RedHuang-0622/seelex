import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const source = await readFile(new URL("./protocol.js", import.meta.url), "utf8");
const protocol = await import(`data:text/javascript;base64,${Buffer.from(source).toString("base64")}`);

function snapshot() {
  return {
    protocol_version: 1,
    revision: 1,
    session: { id: "sess-a" },
    conversation: [],
    chat: { running: true },
    runtime: {},
    capabilities: { session_resume: true }
  };
}

function deltaEvent(deliverySeq, messageID, text) {
  return {
    protocol_version: 1,
    delivery_seq: deliverySeq,
    revision: 2,
    session_id: "sess-a",
    kind: "message.delta",
    payload: { message_id: messageID, delta: text }
  };
}

// 复现：同视图会话运行中，若 message.delta 先于 message.added 到达（跨进程
// 传输乱序/丢 added 后重放），协议层不应为每条增量触发一次整份快照刷新，
// 而应缓冲到 added 到达后按序应用——否则高频流式会退化为刷新风暴，表现为
// “当前视图内容不及时”。
test("delta arriving before added is buffered, not refresh-stormed", () => {
  let current = snapshot();
  const deltas = [];
  for (let seq = 1; seq <= 5; seq++) {
    const result = protocol.applyEvent(current, deltaEvent(seq, "m-live", `chunk${seq}`), seq - 1, 0);
    assert.equal(result.needsRefresh, false, `delta ${seq} must not force full refresh`);
    assert.equal(result.lastSeq, seq);
    current = result.snapshot;
    deltas.push(result.changed);
  }
  assert.deepEqual(deltas, ["message.delta", "message.delta", "message.delta", "message.delta", "message.delta"]);

  // added 到达后缓冲的 delta 全部落到该消息上。
  const added = protocol.applyEvent(current, {
    protocol_version: 1,
    delivery_seq: 6,
    revision: 2,
    session_id: "sess-a",
    kind: "message.added",
    payload: { id: "m-live", role: "assistant", content: "", created_at: "2026-09-04T00:00:00Z" }
  }, 5, 0);
  assert.equal(added.needsRefresh, false);
  const message = added.snapshot.conversation.find(item => item.id === "m-live");
  assert.ok(message, "buffered message must exist after added");
  assert.equal(message.content, "chunk1chunk2chunk3chunk4chunk5");
});
