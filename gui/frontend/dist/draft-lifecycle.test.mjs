import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const source = await readFile(new URL("./draft-lifecycle.js", import.meta.url), "utf8");
const {
  draftLifecycle, draftLifecycleFromSnapshot, composerDraftRows, draftRoundEvent,
  DRAFT_PHASE_IDLE, DRAFT_PHASE_UNSENT, DRAFT_PHASE_SENDING, DRAFT_ROW_KIND, MESSAGE_ROW_KIND
} = await import(`data:text/javascript;base64,${Buffer.from(source).toString("base64")}`);

// ── 权威起点：快照 → 本地草稿状态 ──────────────────────────────

test("derives an attached draft from the snapshot's draft session", () => {
  const state = draftLifecycleFromSnapshot({ sessionID: "draft_1", draft: true, composer: "没发出去的字" });
  assert.deepEqual(state, { sessionID: "draft_1", attached: true, text: "没发出去的字", phase: DRAFT_PHASE_UNSENT });
});

test("a materialized session has no draft attribution", () => {
  // 已物化会话即使快照里残留 composer，也不构成"草稿归属"（草稿只属于未物化会话）。
  const state = draftLifecycleFromSnapshot({ sessionID: "sess-1", draft: false, composer: "残留" });
  assert.deepEqual(state, { sessionID: "sess-1", attached: false, text: "", phase: DRAFT_PHASE_IDLE });
});

test("missing snapshot fields degrade to an empty, idle draft", () => {
  assert.deepEqual(draftLifecycleFromSnapshot(), {
    sessionID: "", attached: false, text: "", phase: DRAFT_PHASE_IDLE
  });
});

// ── append / submit：一轮开始前的迁移 ─────────────────────────

test("append marks the box content as the unsent draft", () => {
  const start = draftLifecycleFromSnapshot({ sessionID: "draft_1", draft: true });
  const typed = draftLifecycle(start, { type: "append", text: "写测试" });
  assert.equal(typed.text, "写测试");
  assert.equal(typed.phase, DRAFT_PHASE_UNSENT);
});

test("clearing the box drops the draft back to idle", () => {
  const typed = draftLifecycle(draftLifecycleFromSnapshot({ draft: true }), { type: "append", text: "写测试" });
  assert.equal(draftLifecycle(typed, { type: "append", text: "" }).phase, DRAFT_PHASE_IDLE);
});

test("submit moves the box to sending and keeps only what is still unsent", () => {
  const typed = draftLifecycle(draftLifecycleFromSnapshot({ sessionID: "draft_1", draft: true }), { type: "append", text: "写测试继续" });
  // remaining 由 app.js 用 clearSubmittedText(当前, 已发送) 算好传进来。
  const sent = draftLifecycle(typed, { type: "submit", remaining: "继续" });
  assert.equal(sent.text, "继续");
  assert.equal(sent.phase, DRAFT_PHASE_SENDING);
  // 在途期间继续敲字仍是"本轮在途"，不被打断成 unsent。
  assert.equal(draftLifecycle(sent, { type: "append", text: "继续再补" }).phase, DRAFT_PHASE_SENDING);
});

// ── materialize / cancel：一轮结束的两种收敛 ────────────────────

test("materialize detaches the draft session and keeps the unsent remainder", () => {
  const typed = draftLifecycle(draftLifecycleFromSnapshot({ sessionID: "draft_1", draft: true }), { type: "append", text: "第一句" });
  const sent = draftLifecycle(typed, { type: "submit", remaining: "补一句" });
  const settled = draftLifecycle(sent, { type: "materialize" });
  assert.equal(settled.attached, false); // 草稿会话已物化，已发送的正文归消息
  assert.equal(settled.text, "补一句"); // 未发送的剩余部分仍是草稿
  assert.equal(settled.phase, DRAFT_PHASE_UNSENT);
});

test("cancel keeps the text as unsent (nothing is swallowed)", () => {
  const typed = draftLifecycle(draftLifecycleFromSnapshot({ sessionID: "draft_1", draft: true }), { type: "append", text: "给这一轮的插话" });
  const cancelled = draftLifecycle(typed, { type: "cancel" });
  assert.equal(cancelled.text, "给这一轮的插话");
  assert.equal(cancelled.phase, DRAFT_PHASE_UNSENT);
  // 取消不改变草稿归属：这门会话仍是未物化草稿。
  assert.equal(cancelled.attached, true);
});

test("clear drops the draft (authoritative reset)", () => {
  const typed = draftLifecycle(draftLifecycleFromSnapshot({ sessionID: "draft_1", draft: true }), { type: "append", text: "写测试" });
  assert.deepEqual(draftLifecycle(typed, { type: "clear" }), {
    sessionID: "draft_1", attached: true, text: "", phase: DRAFT_PHASE_IDLE
  });
});

test("unknown events and half-built states are no-ops, not crashes", () => {
  const typed = draftLifecycle(draftLifecycleFromSnapshot({ draft: true }), { type: "append", text: "写测试" });
  assert.deepEqual(draftLifecycle(typed, { type: "nope" }), typed);
  assert.equal(draftLifecycle(typed).text, "写测试");
  assert.equal(draftLifecycle(null, { type: "append", text: "从空状态起步" }).text, "从空状态起步");
});

// ── 页面 context：既定的 message + 未发送的草稿（可区分）──────────

test("page rows show committed messages plus the current unsent draft", () => {
  const conversation = [
    { id: "m1", role: "user", content: "改一下登录页" },
    { id: "m2", role: "assistant", content: "改完了", tool: { name: "edit" } }
  ];
  const state = draftLifecycle(draftLifecycleFromSnapshot({ sessionID: "draft_1", draft: true }), { type: "append", text: "顺便加个测试" });
  const rows = composerDraftRows({ conversation, state });
  assert.deepEqual(rows.map(row => row.kind), [MESSAGE_ROW_KIND, MESSAGE_ROW_KIND, DRAFT_ROW_KIND]);
  assert.equal(rows[2].draft, true);
  assert.equal(rows[2].unsent, true);
  assert.equal(rows[2].text, "顺便加个测试");
  assert.equal(rows[2].sessionID, "draft_1");
  // 既定消息原样透传、且不被标成草稿。
  assert.equal(rows[0].id, "m1");
  assert.equal(rows[1].tool.name, "edit");
  assert.equal(rows[0].draft, false);
});

test("no draft row when there is no unsent content", () => {
  const conversation = [{ id: "m1", role: "user", content: "只发了一半" }];
  const rows = composerDraftRows({ conversation, state: { sessionID: "draft_1", text: "  ", phase: DRAFT_PHASE_UNSENT } });
  assert.equal(rows.length, 1);
  assert.equal(rows[0].kind, MESSAGE_ROW_KIND);
  assert.equal(composerDraftRows({ state: { text: "只有草稿" } }).length, 1);
  assert.deepEqual(composerDraftRows(), []);
});

// ── 轮次结束 → 草稿收敛事件 ───────────────────────────────────

test("maps a finished round to materialize and a cancelled one to cancel", () => {
  assert.equal(draftRoundEvent({ wasRunning: true, isRunning: false }), "materialize");
  assert.equal(draftRoundEvent({ wasRunning: true, isRunning: false, cancelled: true }), "cancel");
  // 一轮还在跑 / 本来就空闲：没有收敛事件（不误清草稿）。
  assert.equal(draftRoundEvent({ wasRunning: true, isRunning: true }), "");
  assert.equal(draftRoundEvent({ wasRunning: false, isRunning: false }), "");
});

// ── 纯函数约束：不碰 DOM、不碰 Bridge ─────────────────────────

test("stays a pure module (no DOM, no Bridge, no storage)", () => {
  const forbidden = ["document.", "window.", "localStorage", "globalThis.", "invoke(", "getElementById", "addEventListener"];
  for (const needle of forbidden) {
    assert.equal(source.includes(needle), false, `draft-lifecycle.js 不得出现 ${needle}`);
  }
});
