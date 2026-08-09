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
    runtime: { model: "test" },
    conversation_window: 50,
    total_messages: 1,
    history_offset: 0,
    has_more_history: false
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
    protocol_version: 1, seq: 1, revision: 2, kind: "runtime.changed", payload: {
      model: "next",
      plan: { name: "build", status: "running", progress: 0.5, nodes: [{ id: "test", status: "running" }] }
    }
  });
  assert.equal(runtime.snapshot.runtime.model, "next");
  assert.equal(runtime.snapshot.runtime.plan.nodes[0].status, "running");
  assert.equal(runtime.changed, "runtime.changed");

  const opened = applyEvent(runtime.snapshot, {
    protocol_version: 1, seq: 2, revision: 3, kind: "interaction.opened", payload: { id: "approval-1" }
  }, runtime.lastSeq);
  assert.equal(opened.snapshot.interaction.id, "approval-1");

  const closed = applyEvent(opened.snapshot, {
    protocol_version: 1, seq: 3, revision: 4, kind: "interaction.closed"
  }, opened.lastSeq);
  assert.equal(closed.snapshot.interaction, null);
});

test("applies worktable.changed without deep-cloning the plan", () => {
  const current = {
    ...snapshot(),
    runtime: {
      model: "test",
      plan: { status: "running", progress: 0, nodes: [{ id: "n1", status: "running" }], edges: [] },
      work_table: [{ id: "plan:n1", phase: "plan", task: "旧", status: "pending" }]
    }
  };
  const result = applyEvent(current, {
    protocol_version: 1, seq: 1, revision: 2, kind: "worktable.changed",
    payload: { items: [
      { id: "plan:n1", phase: "plan", task: "新", status: "running", trace: [{ status: "running", operation: "node.lifecycle" }] },
      { id: "todo:0", phase: "tasklist", task: "a", status: "doing" }
    ] }
  });
  assert.equal(result.needsRefresh, false);
  assert.equal(result.changed, "worktable.changed");
  assert.equal(result.snapshot.runtime.work_table.length, 2);
  assert.equal(result.snapshot.runtime.work_table[0].task, "新");
  // 结构共享：worktable.changed 只替换 work_table，plan 对象引用不变（无深拷贝）。
  assert.equal(result.snapshot.runtime.plan, current.runtime.plan);
  assert.equal(current.runtime.work_table[0].task, "旧");
});

test("applies task.changed as a single-row upsert", () => {
  const current = {
    ...snapshot(),
    runtime: {
      work_table: [
        { id: "plan:n1", phase: "plan", task: "旧", status: "pending" },
        { id: "todo:0", phase: "tasklist", task: "a", status: "doing" }
      ]
    }
  };
  const result = applyEvent(current, {
    protocol_version: 1, seq: 1, revision: 2, kind: "task.changed",
    payload: { task_id: "plan:n1", task: { id: "plan:n1", phase: "plan", task: "新", status: "retry", retry_count: 2 } }
  });
  assert.equal(result.needsRefresh, false);
  assert.equal(result.changed, "task.changed");
  assert.equal(result.snapshot.runtime.work_table.length, 2);
  assert.equal(result.snapshot.runtime.work_table[0].status, "retry");
  assert.equal(result.snapshot.runtime.work_table[0].retry_count, 2);
  assert.equal(result.snapshot.runtime.work_table[1].task, "a");
  assert.equal(current.runtime.work_table[0].status, "pending"); // 旧快照不变

  // 未知 task_id → 插入新行（add 语义）。
  const added = applyEvent(result.snapshot, {
    protocol_version: 1, seq: 2, revision: 3, kind: "task.changed",
    payload: { task_id: "task:9", task: { id: "task:9", phase: "task", task: "新任务", status: "pending" } }
  }, result.lastSeq);
  assert.equal(added.snapshot.runtime.work_table.length, 3);
  assert.equal(added.snapshot.runtime.work_table[2].id, "task:9");
});

test("subagent updates share unchanged sibling nodes instead of cloning the whole plan", () => {
  const sibling = { id: "sibling", status: "pending", tool_events: [] };
  const current = {
    ...snapshot(),
    runtime: {
      plan: {
        status: "running", progress: 0,
        nodes: [{ id: "root", status: "running", children: [{ id: "worker", status: "queued", tool_events: [] }, sibling] }],
        edges: []
      }
    }
  };
  const result = applyEvent(current, {
    protocol_version: 1, seq: 1, revision: 2, kind: "subagent.changed",
    payload: {
      node_id: "worker", plan_status: "running", progress: 0.5,
      node: { id: "worker", label: "Worker", status: "running", tool_events: [], children: [] }
    }
  });
  // 未命中的兄弟节点保持同一对象引用（路径级结构共享，非整树克隆）。
  assert.equal(result.snapshot.runtime.plan.nodes[0].children[1], sibling);
  assert.equal(current.runtime.plan.nodes[0].children[0].status, "queued");
});

test("applies recursive subagent lifecycle and tool events without mutating the previous snapshot", () => {
  const current = {
    ...snapshot(),
    runtime: {
      plan: {
        status: "running", progress: 0,
        nodes: [{ id: "root", status: "running", children: [{ id: "worker", status: "queued", tool_events: [] }] }],
        edges: []
      }
    }
  };
  const lifecycle = applyEvent(current, {
    protocol_version: 1, seq: 1, revision: 2, kind: "subagent.changed",
    payload: {
      node_id: "worker", plan_status: "running", progress: 0.5,
      node: { id: "worker", label: "Worker", status: "worktree_creating", tool_events: [], children: [] }
    }
  });
  const started = applyEvent(lifecycle.snapshot, {
    protocol_version: 1, seq: 2, revision: 3, kind: "subagent.tool.started",
    payload: { id: "subtool-1", node_id: "worker", name: "read_file", status: "running" }
  }, lifecycle.lastSeq);
  const completed = applyEvent(started.snapshot, {
    protocol_version: 1, seq: 3, revision: 4, kind: "subagent.tool.completed",
    payload: { id: "subtool-1", node_id: "worker", name: "read_file", status: "success", result: "done" }
  }, started.lastSeq);

  const worker = completed.snapshot.runtime.plan.nodes[0].children[0];
  assert.equal(worker.status, "worktree_creating");
  assert.equal(worker.tool_events.length, 1);
  assert.equal(worker.tool_events[0].status, "success");
  assert.equal(completed.snapshot.runtime.plan.progress, 0.5);
  assert.equal(current.runtime.plan.nodes[0].children[0].status, "queued");
  assert.deepEqual(current.runtime.plan.nodes[0].children[0].tool_events, []);
});

test("bounds incrementally added conversation messages to the advertised window", () => {
  let result = {
    snapshot: {
      ...snapshot(),
      conversation_window: 2,
      total_messages: 1,
      conversation: [{ id: "m1", role: "assistant", content: "one" }]
    },
    lastSeq: 0
  };
  for (const [index, id] of ["m2", "m3"].entries()) {
    result = applyEvent(result.snapshot, {
      protocol_version: 1, seq: index + 1, revision: index + 2, kind: "message.added",
      payload: { id, role: "assistant", content: id }
    }, result.lastSeq);
  }
  assert.deepEqual(result.snapshot.conversation.map(message => message.id), ["m2", "m3"]);
  assert.equal(result.snapshot.total_messages, 3);
  assert.equal(result.snapshot.history_offset, 1);
  assert.equal(result.snapshot.has_more_history, true);
});
