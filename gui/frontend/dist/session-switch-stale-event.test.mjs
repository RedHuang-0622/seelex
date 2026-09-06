import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

// 复现「热会话切换时而灵时不灵」的链路（core resume → Bridge 重订阅 → 前端
// client-state/protocol 水位）：
//
// Bridge.ResumeSession(B) 期间旧订阅 A 的 relay 可能已经把 A 事件取出、
// 与新订阅 B 的 seelex:ready 基线并发投递（旧 relay 与 startRelay(B) 是两个
// goroutine，跨进程投递无顺序保证）。因此「A 的迟到事件」可能在新会话 B 的
// 基线被接受（已应用水位复位为 0）之后才到达渲染层。
//
// 关键缺陷：protocol.applyEvent 对「不属于当前视图会话」的事件走 dropped
// 分支时把 lastSeq 推进到该事件的 delivery_seq（旧订阅的编号，通常远大于新
// 订阅从 1 重计的编号），client-state 随之把它回报为已应用。此后新会话 B 的
// 事件 seq<=该污染水位全部被当成重复静默丢弃 → B 的正文冻结在基线，直到下
// 一次整份刷新或再切换一次才恢复 —— 与「时而灵时不灵」一致。
//
// 该场景对 热→热 与 热→冷 同时成立：差异只在目标冷时基线先给 restoring
// 空壳、后台装载完成后经 snapshot.changed → refresh 再给内容基线，污染窗口
// 不变。

const protocolSource = await readFile(new URL("./protocol.js", import.meta.url), "utf8");
const protocolURL = `data:text/javascript;base64,${Buffer.from(protocolSource).toString("base64")}`;
const shapeSource = await readFile(new URL("./snapshot-shape.js", import.meta.url), "utf8");
const shapeURL = `data:text/javascript;base64,${Buffer.from(shapeSource).toString("base64")}`;
const clientSource = (await readFile(new URL("./client-state.js", import.meta.url), "utf8"))
  .replace('"./protocol.js"', `"${protocolURL}"`)
  .replace('"./snapshot-shape.js"', `"${shapeURL}"`);
const { createGUIClient } = await import(`data:text/javascript;base64,${Buffer.from(clientSource).toString("base64")}`);

function snapshot(sessionID, revision, conversation, chat = {}) {
  return {
    protocol_version: 1,
    revision,
    session: { id: sessionID },
    conversation,
    chat,
    runtime: {}
  };
}

function event(sessionID, kind, deliverySeq, revision, payload) {
  return {
    protocol_version: 1,
    seq: 1000 + deliverySeq,
    delivery_seq: deliverySeq,
    revision,
    session_id: sessionID,
    kind,
    payload
  };
}

function makeClient(loadSnapshot) {
  const incrementals = [];
  let loads = 0;
  const client = createGUIClient({
    loadSnapshot: async () => {
      loads += 1;
      return loadSnapshot(loads);
    },
    onSnapshot: () => {},
    onIncremental: (snapshot, kind) => incrementals.push([snapshot.session.id, kind, snapshot.conversation.map(m => `${m.id}:${m.content}`)]),
    onApplied: () => {},
    onError: error => { throw error; }
  });
  return { client, incrementals, loads: () => loads };
}

test("stale cross-session event after switch must not freeze the new session (hot→hot)", async () => {
  const { client, incrementals } = makeClient(loads =>
    loads === 1 ? snapshot("session-a", 5, [{ id: "assistant-a", role: "assistant", content: "A0" }]) : snapshot("session-b", 20, [{ id: "assistant-b", role: "assistant", content: "B0" }])
  );

  // 阶段 1：会话 A 是自己的订阅，事件按 delivery_seq 1..3 正常落地。
  await client.refresh({ scroll: "bottom" });
  for (const chunk of ["A1", "A2", "A3"]) {
    await client.handleEvent(event("session-a", "message.delta", Number(chunk[1]), 6, { message_id: "assistant-a", delta: chunk }));
  }
  assert.equal(client.current().session.id, "session-a");

  // 阶段 2：切到热会话 B —— Bridge 重订阅后发出的权威基线（session 变化，
  // applied 水位复位为 0）。
  client.acceptSnapshot(snapshot("session-b", 20, [{ id: "assistant-b", role: "assistant", content: "B0" }]), "bottom");
  assert.equal(client.current().session.id, "session-b");

  // 阶段 3：B 自己的订阅从 delivery_seq=1 重计，先落两段增量。
  await client.handleEvent(event("session-b", "message.delta", 1, 21, { message_id: "assistant-b", delta: "B1" }));
  await client.handleEvent(event("session-b", "message.delta", 2, 22, { message_id: "assistant-b", delta: "B2" }));

  // 阶段 4（竞态窗口）：旧订阅 A 的迟到事件此刻才到达（delivery_seq=99，
  // 旧订阅编号远大于 B 新订阅的当前水位 2）。它不属于当前视图 B。
  await client.handleEvent(event("session-a", "message.delta", 99, 30, { message_id: "assistant-a", delta: "late" }));

  // 阶段 5：B 的后续事件 seq=3。若迟到事件把水位推进到 99，seq=3 会被当成
  // 旧序号静默丢弃，B 正文冻结在 B0B1B2 —— 正是“切过去但内容不更新”。
  await client.handleEvent(event("session-b", "message.delta", 3, 23, { message_id: "assistant-b", delta: "B3" }));

  assert.deepEqual(incrementals.slice(-1), [
    ["session-b", "message.delta", ["assistant-b:B0B1B2B3"]]
  ]);
  assert.equal(client.current().conversation[0].content, "B0B1B2B3");
});

test("stale cross-session event after cold-restore baseline must not freeze content (hot→cold)", async () => {
  // 热→冷：A 运行中切到冷 B → restoring 空壳基线 → 后台装载完成 snapshot.changed
  // → 整份 refresh 得到 B 内容基线（session 已变，水位复位 0）→ 迟到 A 事件仍可能
  // 把水位污染，B 装载后新产生的增量被吞。
  const restoringShell = snapshot("session-b", 8, [], { running: true }); // restoring 空壳
  const coldBaseline = snapshot("session-b", 30, [{ id: "assistant-b", role: "assistant", content: "B0" }]); // 装载完成
  const { client, incrementals } = makeClient(loads =>
    loads === 1 ? snapshot("session-a", 5, [{ id: "assistant-a", role: "assistant", content: "A0" }])
    : coldBaseline
  );

  await client.refresh({ scroll: "bottom" }); // 视图 A
  // A 运行中，切到冷 B：空壳基线
  client.acceptSnapshot(restoringShell, "bottom");
  assert.equal(client.current().session.id, "session-b");
  // 后台装载完成：snapshot.changed 是非增量 kind（本订阅 delivery_seq=1）→
  // 触发整份 refresh（B 内容基线）
  await client.handleEvent(event("session-b", "snapshot.changed", 1, 25, null));
  assert.equal(client.current().conversation[0].content, "B0");

  // 装载完成后 B 开始流式输出（订阅续号：seq 2..）
  await client.handleEvent(event("session-b", "message.delta", 2, 31, { message_id: "assistant-b", delta: "B1" }));
  await client.handleEvent(event("session-b", "message.delta", 3, 32, { message_id: "assistant-b", delta: "B2" }));
  // 旧订阅 A 的迟到事件（旧订阅编号 77）此刻到达
  await client.handleEvent(event("session-a", "message.delta", 77, 40, { message_id: "assistant-a", delta: "late" }));
  // B 的 seq=4 增量
  await client.handleEvent(event("session-b", "message.delta", 4, 33, { message_id: "assistant-b", delta: "B3" }));

  assert.deepEqual(incrementals.slice(-1), [
    ["session-b", "message.delta", ["assistant-b:B0B1B2B3"]]
  ]);
  assert.equal(client.current().conversation[0].content, "B0B1B2B3");
});
