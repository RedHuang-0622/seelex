import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const source = await readFile(new URL("./protocol.js", import.meta.url), "utf8");
const { applyEvent, validateSnapshot } = await import(`data:text/javascript;base64,${Buffer.from(source).toString("base64")}`);

function snapshot() {
  return {
    protocol_version: 1,
    revision: 1,
    conversation: [{ id: "assistant-1", role: "assistant", content: "A" }],
    chat: { running: false },
    runtime: { model: "test" }
  };
}

test("validates snapshot protocol versions", () => {
  assert.equal(validateSnapshot(snapshot()).protocol_version, 1);
  assert.throws(() => validateSnapshot({ ...snapshot(), protocol_version: 2 }), /不受支持/);
  assert.throws(() => validateSnapshot({ protocol_version: 1 }), /conversation/);
});

test("applies message additions and deltas without a snapshot refresh", () => {
  const added = applyEvent(snapshot(), {
    protocol_version: 1, seq: 10, revision: 2, request_id: "chat-1", kind: "message.added",
    payload: { id: "user-1", role: "user", content: "question" }
  });
  assert.equal(added.needsRefresh, false);
  assert.equal(added.snapshot.chat.running, true);
  assert.equal(added.snapshot.conversation.at(-1).id, "user-1");

  const delta = applyEvent(added.snapshot, {
    protocol_version: 1, seq: 11, revision: 3, request_id: "chat-1", kind: "message.delta",
    payload: JSON.stringify({ message_id: "assistant-1", delta: "B" })
  }, added.lastSeq);
  assert.equal(delta.needsRefresh, false);
  assert.equal(delta.snapshot.conversation[0].content, "AB");
});

test("requests resync for sequence gaps and unknown events", () => {
  const gap = applyEvent(snapshot(), { protocol_version: 1, seq: 4, kind: "message.delta" }, 2);
  assert.equal(gap.needsRefresh, true);
  assert.equal(gap.lastSeq, 4);

  const unknown = applyEvent(snapshot(), { protocol_version: 1, seq: 5, kind: "future.event" }, 4);
  assert.equal(unknown.needsRefresh, true);
});

test("rejects incompatible events without mutating state", () => {
  const current = snapshot();
  const result = applyEvent(current, { protocol_version: 9, seq: 1, kind: "message.added" });
  assert.equal(result.snapshot, current);
  assert.match(result.error.message, /不受支持/);
});

test("ignores events already represented by an authoritative snapshot", () => {
  const current = { ...snapshot(), revision: 4, conversation: [{ id: "assistant-1", role: "assistant", content: "AB" }] };
  const result = applyEvent(current, {
    protocol_version: 1, seq: 7, revision: 4, request_id: "chat-1", kind: "message.delta",
    payload: { message_id: "assistant-1", delta: "B" }
  }, 6, 4);

  assert.equal(result.needsRefresh, false);
  assert.equal(result.lastSeq, 7);
  assert.equal(result.changed, undefined);
  assert.equal(result.snapshot.conversation[0].content, "AB");
});

test("applies sibling events sharing a revision above the snapshot floor", () => {
  const first = applyEvent(snapshot(), {
    protocol_version: 1, seq: 1, revision: 2, request_id: "chat-1", kind: "message.added",
    payload: { id: "user-1", role: "user", content: "question" }
  }, 0, 1);
  const second = applyEvent(first.snapshot, {
    protocol_version: 1, seq: 2, revision: 2, request_id: "chat-1", kind: "message.added",
    payload: { id: "assistant-2", role: "assistant", content: "" }
  }, first.lastSeq, 1);

  assert.equal(second.needsRefresh, false);
  assert.deepEqual(second.snapshot.conversation.slice(-2).map(message => message.id), ["user-1", "assistant-2"]);
});

test("applies runtime and interaction events", () => {
  const runtime = applyEvent(snapshot(), {
    protocol_version: 1, seq: 1, revision: 2, kind: "runtime.changed", payload: { model: "next" }
  });
  assert.equal(runtime.snapshot.runtime.model, "next");

  const opened = applyEvent(runtime.snapshot, {
    protocol_version: 1, seq: 2, revision: 3, kind: "interaction.opened", payload: { id: "approval-1" }
  }, runtime.lastSeq);
  assert.equal(opened.snapshot.interaction.id, "approval-1");

  const closed = applyEvent(opened.snapshot, {
    protocol_version: 1, seq: 3, revision: 4, kind: "interaction.closed"
  }, opened.lastSeq);
  assert.equal(closed.snapshot.interaction, null);
});
