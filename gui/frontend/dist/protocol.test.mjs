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
    protocol_version: 1, delivery_seq: 10, revision: 2, request_id: "chat-1", kind: "message.added",
    payload: { id: "user-1", role: "user", content: "question" }
  });
  assert.equal(added.needsRefresh, false);
  assert.equal(added.snapshot.conversation.at(-1).id, "user-1");
  // 运行态只由后端下发：增量事件到达不等于"在跑"。
  assert.equal(added.snapshot.chat.running, false);

  const delta = applyEvent(added.snapshot, {
    protocol_version: 1, delivery_seq: 11, revision: 3, request_id: "chat-1", kind: "message.delta",
    payload: JSON.stringify({ message_id: "assistant-1", delta: "B" })
  }, added.lastSeq);
  assert.equal(delta.needsRefresh, false);
  assert.equal(delta.snapshot.conversation[0].content, "AB");
  assert.equal(delta.snapshot.chat.running, false);
});

test("applies authoritative chat state from chat.changed", () => {
  const running = applyEvent(snapshot(), {
    protocol_version: 1, delivery_seq: 12, revision: 0, request_id: "chat-1", kind: "chat.changed",
    payload: { running: true, request_id: "chat-1", queued_count: 2, input_queue: ["a", "b"] }
  }, 11);
  assert.equal(running.needsRefresh, false);
  assert.equal(running.snapshot.chat.running, true);
  assert.equal(running.snapshot.chat.request_id, "chat-1");
  assert.deepEqual(running.snapshot.chat.input_queue, ["a", "b"]);

  // 停止态同样由后端下发；revision=0 不受 revision floor 抑制。
  const idle = applyEvent(running.snapshot, {
    protocol_version: 1, delivery_seq: 13, revision: 0, kind: "chat.changed",
    payload: { running: false }
  }, running.lastSeq, 99);
  assert.equal(idle.needsRefresh, false);
  assert.equal(idle.snapshot.chat.running, false);
});

test("applies reasoning_content deltas without touching visible content", () => {
  const delta = applyEvent(snapshot(), {
    protocol_version: 1, delivery_seq: 11, revision: 3, request_id: "chat-1", kind: "message.delta",
    payload: JSON.stringify({ message_id: "assistant-1", reasoning_content: "thinking steps" })
  }, 10);
  assert.equal(delta.needsRefresh, false);
  assert.equal(delta.snapshot.conversation[0].reasoning_content, "thinking steps");
  assert.equal(delta.snapshot.conversation[0].content, "A");
});

// 会话归属是 application 投递端（SubscribeSession / 跟随视图订阅）的不变量：
// 前端一旦再判一次"这个事件属于哪个会话"，同一规则就有两份实现并各自漂移。
// 跨会话污染的正向防线见 gui/bridge_session_test.go。
test("never inspects session_id when applying events (attribution lives upstream)", () => {
  const current = { ...snapshot(), session: { id: "session-b" } };
  const added = applyEvent(current, {
    protocol_version: 1, delivery_seq: 12, revision: 3, request_id: "chat-a", kind: "message.added",
    session_id: "session-a",
    payload: { id: "msg-a", role: "assistant", content: "already routed by the hub" }
  }, 11);
  assert.equal(added.needsRefresh, false);
  assert.equal(added.snapshot.conversation.length, 2);
});

test("global seq jumps are normal; only delivery_seq gaps mean loss", () => {
  const current = { ...snapshot(), session: { id: "session-b" } };
  // 两次投递之间别会话产生了事件：全局 seq 跳号、投递序号连续 → 不得 resync。
  const jumped = applyEvent(current, {
    protocol_version: 1, seq: 40, delivery_seq: 12, revision: 3, kind: "message.added",
    session_id: "session-b",
    payload: { id: "msg-b", role: "user", content: "hello" }
  }, 11);
  assert.equal(jumped.needsRefresh, false);
  assert.equal(jumped.lastSeq, 12);
  assert.equal(jumped.snapshot.conversation.length, 2);

  const lost = applyEvent(jumped.snapshot, {
    protocol_version: 1, seq: 44, delivery_seq: 14, revision: 4, kind: "task.changed",
    payload: { task_id: "task-1", task: { id: "task-1", status: "pending" } }
  }, jumped.lastSeq);
  assert.equal(lost.needsRefresh, true);
});

test("flags sequence gaps for incremental replay and unknown events for resync", () => {
  // 缺口刻意不推进 lastSeq：宿主先按 delivery_seq 增量补取（C4），补不齐才整份
  // 重拉，重拉后落到 gapSeq。
  const gap = applyEvent(snapshot(), { protocol_version: 1, delivery_seq: 4, kind: "message.delta" }, 2);
  assert.equal(gap.needsRefresh, true);
  assert.equal(gap.gap, true);
  assert.equal(gap.lastSeq, 2);
  assert.equal(gap.gapSeq, 4);

  // 未知事件类型不是缺口，补取补不回来：直接整份重拉并把水位推到该处。
  const unknown = applyEvent(snapshot(), { protocol_version: 1, delivery_seq: 5, kind: "future.event" }, 4);
  assert.equal(unknown.needsRefresh, true);
  assert.equal(unknown.gap, undefined);
  assert.equal(unknown.lastSeq, 5);

  // 没有 delivery_seq 的事件无法定位区间：按缺口上报，但水位只能停在已知处。
  const unnumbered = applyEvent(snapshot(), { protocol_version: 1, kind: "message.delta" }, 3);
  assert.equal(unnumbered.gap, true);
  assert.equal(unnumbered.lastSeq, 3);
  assert.equal(unnumbered.gapSeq, 3);
});

test("rejects incompatible events without mutating state", () => {
  const current = snapshot();
  const result = applyEvent(current, { protocol_version: 9, delivery_seq: 1, kind: "message.added" });
  assert.equal(result.snapshot, current);
  assert.match(result.error.message, /不受支持/);
});

test("ignores events already represented by an authoritative snapshot", () => {
  const current = { ...snapshot(), revision: 4, conversation: [{ id: "assistant-1", role: "assistant", content: "AB" }] };
  const result = applyEvent(current, {
    protocol_version: 1, delivery_seq: 7, revision: 4, request_id: "chat-1", kind: "message.delta",
    payload: { message_id: "assistant-1", delta: "B" }
  }, 6, 4);

  assert.equal(result.needsRefresh, false);
  assert.equal(result.lastSeq, 7);
  assert.equal(result.changed, undefined);
  assert.equal(result.snapshot.conversation[0].content, "AB");
});

test("applies sibling events sharing a revision above the snapshot floor", () => {
  const first = applyEvent(snapshot(), {
    protocol_version: 1, delivery_seq: 1, revision: 2, request_id: "chat-1", kind: "message.added",
    payload: { id: "user-1", role: "user", content: "question" }
  }, 0, 1);
  const second = applyEvent(first.snapshot, {
    protocol_version: 1, delivery_seq: 2, revision: 2, request_id: "chat-1", kind: "message.added",
    payload: { id: "assistant-2", role: "assistant", content: "" }
  }, first.lastSeq, 1);

  assert.equal(second.needsRefresh, false);
  assert.deepEqual(second.snapshot.conversation.slice(-2).map(message => message.id), ["user-1", "assistant-2"]);
});

test("applies runtime and interaction events", () => {
  const runtime = applyEvent(snapshot(), {
    protocol_version: 1, delivery_seq: 1, revision: 2, kind: "runtime.changed", payload: {
      model: "next",
      plan: { name: "build", status: "running", progress: 0.5, nodes: [{ id: "test", status: "running" }] }
    }
  });
  assert.equal(runtime.snapshot.runtime.model, "next");
  assert.equal(runtime.snapshot.runtime.plan.nodes[0].status, "running");
  assert.equal(runtime.changed, "runtime.changed");

  const opened = applyEvent(runtime.snapshot, {
    protocol_version: 1, delivery_seq: 2, revision: 3, kind: "interaction.opened", payload: { id: "approval-1" }
  }, runtime.lastSeq);
  assert.equal(opened.snapshot.interaction.id, "approval-1");

  const closed = applyEvent(opened.snapshot, {
    protocol_version: 1, delivery_seq: 3, revision: 4, kind: "interaction.closed"
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
    protocol_version: 1, delivery_seq: 1, revision: 2, kind: "worktable.changed",
    payload: { items: [
      { id: "plan:n1", phase: "plan", task: "新", status: "running", trace: [{ status: "running", operation: "node.lifecycle" }] },
      { id: "todo:0", phase: "tasklist", task: "a", status: "doing" }
    ], batches: [
      { id: "chat-1", label: "2026-08-23 09:15", created_at: "2026-08-23T09:15:00Z", counts: { all: 2, todo: 1, plan: 1 } }
    ] }
  });
  assert.equal(result.needsRefresh, false);
  assert.equal(result.changed, "worktable.changed");
  assert.equal(result.snapshot.runtime.work_table.length, 2);
  assert.equal(result.snapshot.runtime.work_table[0].task, "新");
  assert.equal(result.snapshot.runtime.work_table_batches.length, 1);
  assert.equal(result.snapshot.runtime.work_table_batches[0].id, "chat-1");
  // 结构共享：worktable.changed 只替换 work_table，plan 对象引用不变（无深拷贝）。
  assert.equal(result.snapshot.runtime.plan, current.runtime.plan);
  assert.equal(current.runtime.work_table[0].task, "旧");
});

test("worktable.changed without batches keeps existing batch headers", () => {
  const current = {
    ...snapshot(),
    runtime: {
      work_table: [],
      work_table_batches: [{ id: "chat-1", label: "批次A", counts: { all: 0 } }]
    }
  };
  const result = applyEvent(current, {
    protocol_version: 1, delivery_seq: 1, revision: 2, kind: "worktable.changed",
    payload: {
      items: [{ id: "task:1", phase: "task", task: "t", status: "pending", kind: "task", batch_id: "chat-1" }]
    }
  });
  assert.equal(result.needsRefresh, false);
  assert.equal(result.snapshot.runtime.work_table.length, 1);
  assert.equal(result.snapshot.runtime.work_table_batches.length, 1);
  assert.equal(result.snapshot.runtime.work_table_batches[0].id, "chat-1");
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
    protocol_version: 1, delivery_seq: 1, revision: 2, kind: "task.changed",
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
    protocol_version: 1, delivery_seq: 2, revision: 3, kind: "task.changed",
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
    protocol_version: 1, delivery_seq: 1, revision: 2, kind: "subagent.changed",
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
    protocol_version: 1, delivery_seq: 1, revision: 2, kind: "subagent.changed",
    payload: {
      node_id: "worker", plan_status: "running", progress: 0.5,
      node: { id: "worker", label: "Worker", status: "worktree_creating", tool_events: [], children: [] }
    }
  });
  const started = applyEvent(lifecycle.snapshot, {
    protocol_version: 1, delivery_seq: 2, revision: 3, kind: "subagent.tool.started",
    payload: { id: "subtool-1", node_id: "worker", name: "read_file", status: "running" }
  }, lifecycle.lastSeq);
  const completed = applyEvent(started.snapshot, {
    protocol_version: 1, delivery_seq: 3, revision: 4, kind: "subagent.tool.completed",
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

// 窗口投影属于后端 view_state：reducer 只追加消息，不再本地截断或推算
// total_messages / history_offset / has_more_history（旧实现在 JS 里复刻了同一
// 套规则，两边一旦漂移就出现"客户端少显示历史"）。权威游标随下一次
// snapshot.changed 重拉到达。
test("leaves window counters and array bounds to the authoritative snapshot", () => {
  let result = {
    snapshot: {
      ...snapshot(),
      conversation_window: 2,
      total_messages: 1,
      history_offset: 0,
      has_more_history: false,
      conversation: [{ id: "m1", role: "assistant", content: "one" }]
    },
    lastSeq: 0
  };
  for (const [index, id] of ["m2", "m3"].entries()) {
    result = applyEvent(result.snapshot, {
      protocol_version: 1, delivery_seq: index + 1, revision: index + 2, kind: "message.added",
      payload: { id, role: "assistant", content: id }
    }, result.lastSeq);
    assert.equal(result.needsRefresh, false);
  }
  assert.deepEqual(result.snapshot.conversation.map(message => message.id), ["m1", "m2", "m3"]);
  assert.equal(result.snapshot.total_messages, 1);
  assert.equal(result.snapshot.history_offset, 0);
  assert.equal(result.snapshot.has_more_history, false);
});
