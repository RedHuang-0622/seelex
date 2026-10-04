// 压缩进度面的累计逻辑（compaction-format.mergeCompactionProgress）与耗时文案。
//
// 为什么这些要在这里测而不是在 app.js 里：app.js 是"存 + 定时撤条"的薄层，
// DOM 之外动不了；累计逻辑一旦写进它就只能靠手点验证。抽成纯函数后，"新一轮是否
// 清空上一轮的清单""中途失败是否停在真正走过的格子"这类判据都能钉住——它们正是
// 用户读到"两轮混在一起"或"明明失败却满格"的原因。
import assert from "node:assert/strict";
import test from "node:test";

import {
  compactionFailureError,
  compactionFailureText,
  compactionFrontier,
  compactionGateDurationText,
  compactionOutcomeLabel,
  compactionRangeText,
  conversationCompactionAnchor,
  mergeCompactionProgress,
  messageOrdinal
} from "./compaction-format.js";

const mergeFrames = (...frames) => frames.reduce((acc, frame) => mergeCompactionProgress(acc, frame), null);

test("a begin frame opens a round with nothing counted and nothing timed", () => {
  const progress = mergeFrames({ state: "running", phase: "begin", gate: "judge", index: 0, total: 6, origin: "explicit" });
  assert.equal(progress.state, "running");
  assert.equal(progress.phase, "begin");
  assert.equal(progress.index, 0);
  assert.equal(progress.total, 6);
  assert.equal(progress.gates.length, 0);
  assert.equal(progress.elapsedMs, 0);
  assert.equal(progress.origin, "explicit");
});

test("gate frames accumulate in execution order with their own durations", () => {
  const progress = mergeFrames(
    { state: "running", phase: "begin", gate: "judge", index: 0, total: 6 },
    { state: "running", gate: "judge", index: 1, total: 6, elapsed_ms: 28, detail: "compared=163925" },
    { state: "running", gate: "assemble", index: 2, total: 6, elapsed_ms: 6, detail: "assembled=83887" },
    { state: "running", gate: "replace", index: 3, total: 6, elapsed_ms: 0, detail: "messages=4" }
  );
  assert.deepEqual(progress.gates.map(item => item.gate), ["judge", "assemble", "replace"]);
  assert.deepEqual(progress.gates.map(item => item.ms), [28, 6, 0]);
  assert.equal(progress.gates[0].detail, "compared=163925");
  assert.equal(progress.elapsedMs, 34);
  assert.equal(progress.index, 3);
});

test("a re-sent gate replaces its row instead of duplicating it", () => {
  const progress = mergeFrames(
    { state: "running", gate: "judge", index: 1, total: 6, elapsed_ms: 28 },
    { state: "running", gate: "judge", index: 1, total: 6, elapsed_ms: 31, detail: "compared=2" }
  );
  assert.equal(progress.gates.length, 1);
  assert.equal(progress.gates[0].ms, 31);
  assert.equal(progress.gates[0].detail, "compared=2");
});

test("the terminal frame carries the version, outcome and total elapsed", () => {
  const progress = mergeFrames(
    { state: "running", gate: "judge", index: 1, total: 6, elapsed_ms: 28 },
    { state: "running", gate: "record", index: 6, total: 6, elapsed_ms: 2, detail: "recorded=true version=2" },
    { state: "done", index: 6, total: 6, version: 2, origin: "explicit", outcome: "compacted", detail: "reached=6/6" }
  );
  assert.equal(progress.state, "done");
  assert.equal(progress.version, 2);
  assert.equal(progress.outcome, "compacted");
  assert.equal(progress.detail, "reached=6/6");
  assert.equal(progress.index, 6);
  assert.equal(progress.gates.length, 2);
  assert.equal(progress.elapsedMs, 30);
});

test("a failed round stops at the gate it really reached", () => {
  // 后端终局帧恒给 index=total（"本轮已收口"），但那不代表六关真走完：中途报错的
  // 那一轮必须停在它实际到过的格子上，否则进度条会被终局帧推成满格。
  const progress = mergeFrames(
    { state: "running", gate: "judge", index: 1, total: 6, elapsed_ms: 20 },
    { state: "running", gate: "assemble", index: 2, total: 6, elapsed_ms: 4 },
    { state: "failed", index: 6, total: 6, origin: "auto", outcome: "context: 结构性超限", detail: "reached=2/6" }
  );
  assert.equal(progress.state, "failed");
  assert.equal(progress.index, 2);
  assert.equal(progress.outcome, "context: 结构性超限");
  assert.equal(progress.gates.length, 2);
});

test("a new round never inherits the previous round's checklist", () => {
  // 自动路径没有起手帧（要不要压缩正是判据估算的结果），新轮的第一帧就是判据关
  // 收口。判新轮的判据只能是"上一轮已收口"——否则两轮的耗时会被读成一轮。
  const first = mergeFrames(
    { state: "running", phase: "begin", gate: "judge", index: 0, total: 6 },
    { state: "running", gate: "judge", index: 1, total: 6, elapsed_ms: 28 },
    { state: "done", index: 6, total: 6, version: 2, outcome: "compacted" }
  );
  assert.equal(first.gates.length, 1);
  assert.equal(first.elapsedMs, 28);
  const second = mergeCompactionProgress(first, { state: "running", gate: "judge", index: 1, total: 6, elapsed_ms: 11, origin: "auto" });
  assert.equal(second.gates.length, 1);
  assert.equal(second.gates[0].ms, 11);
  assert.equal(second.elapsedMs, 11);
  assert.equal(second.state, "running");
  assert.equal(second.outcome, "");
  // 上一轮拿到的版本号不该留在新一轮上（那会把进度条对到上一条记录）。
  assert.equal(second.version, 0);
  assert.equal(second.origin, "auto");
});

test("an unusable frame leaves the round untouched", () => {
  const progress = mergeFrames({ state: "running", gate: "judge", index: 1, total: 6, elapsed_ms: 28 });
  for (const payload of [null, undefined, 7, "x", {}, { state: "running", gate: "judge" }, { state: "running", total: 0 }]) {
    assert.equal(mergeCompactionProgress(progress, payload), progress);
  }
});

test("durations below a millisecond say so instead of claiming zero", () => {
  assert.equal(compactionGateDurationText(28), "28ms");
  assert.equal(compactionGateDurationText(1), "1ms");
  assert.equal(compactionGateDurationText(0), "<1ms");
  assert.equal(compactionGateDurationText(undefined), "<1ms");
  assert.equal(compactionGateDurationText(-3), "<1ms");
});

// 起手帧不占清单格子：它说的是"这一关开始了"，不是"这一关花了 X 毫秒"。若让它入清单，
// 起手帧这一屏会在判据关上出现两行（清单一行凭空 <1ms + 起手帧自己的"进行中"一行），
// 而在判据关收口之前就失败的那一轮，会把一个并不存在的耗时写进清单。
test("the begin frame never becomes a checklist row of its own", () => {
  const running = mergeFrames({ state: "running", phase: "begin", gate: "judge", index: 0, total: 6, origin: "explicit" });
  assert.deepEqual(running.gates, []);
  assert.equal(running.elapsedMs, 0);
  assert.equal(running.index, 0);
  const failed = mergeCompactionProgress(running, { state: "failed", total: 6, origin: "explicit", outcome: "assembly exploded" });
  assert.deepEqual(failed.gates, []);
  assert.equal(failed.state, "failed");
  assert.equal(failed.outcome, "assembly exploded");
});

// ── 压缩分界（会话单例）的判定与落点 ─────────────────────────────
// 分界不是"每条压缩记录一条"：它说的是会话当前的一个事实（以上这些已经不发给模型），
// 因此判定只返回一个前沿，区间取整段已折出的上下文（起点最早、终点最新）——只报最后
// 一次压缩，会让更早折掉的那段看起来还发给模型。

test("messageOrdinal reads the event number, tool rows included", () => {
  assert.equal(messageOrdinal("message-663"), 663);
  assert.equal(messageOrdinal("message-663-2"), 663);
  assert.equal(messageOrdinal("tool-call-1"), null);
  assert.equal(messageOrdinal(""), null);
  assert.equal(messageOrdinal(undefined), null);
});

test("compactionFrontier 不把失败痕读成已压出窗口的上下文", () => {
  // 失败痕的 message_to / event_to 恒空（这次什么都没动）。它一旦参与前沿判定，
  // 对话区就会凭空画出一条"以上已被压缩"的假线——而那段上下文明明还发给模型。
  const frontier = compactionFrontier([
    { version: 1, message_from: "message-1", message_to: "message-9", event_from: 1, event_to: 3, frame_ref: "tr-1" },
    { version: 2, failed: true, note: "no_model_summary" }
  ]);
  assert.equal(frontier.message_to, "message-9");
  assert.equal(frontier.index, 0);
  assert.equal(frontier.count, 2, "失败痕仍在记录列表里（要查得到），只是不参与分界");
});

test("压缩失败：回执文案说「压缩失败」并把原因字面量翻成人话", () => {
  assert.equal(compactionOutcomeLabel("compact_failed"), "压缩失败：上下文原样继续");
  assert.match(compactionFailureText({ note: "no_model_summary estimated=281424 budget=163616" }), /拿不到模型读后感/);
  assert.match(compactionFailureText({ note: "no_model_summary estimated=281424 budget=163616" }), /estimated=281424 budget=163616/);
  assert.match(compactionFailureText({ note: "ineffective_compact landing=1 soft=2" }), /换不来余量/);
  // 未留原因的失败也不许装成"没有失败"。
  assert.match(compactionFailureText({}), /未留下原因/);
  // 后端新增失败种类而前端没跟时：原样显示，不吞成一句笼统的"压缩失败"。
  assert.equal(compactionFailureText({ note: "brand_new_reason why=x" }), "brand_new_reason；why=x");
});

// 失败痕的报错原文（note 末段 ` error=`）：后端把它接在末尾，因为报错是自由文本，
// 只有放在末尾才不必引号转义。这里钉三个判据：取得到原文、原文里的空格与引号不被
// 截断、以及**没有这一段就不给报错行**（不拿数字事实冒充报错）。
test("压缩失败痕的报错原文从 note 末段取回，取不到就不编", () => {
  const note = "no_model_summary estimated=197421 budget=166808 window=200000 overhead=4986"
    + " error=compact-local:replay-failed 前缀重放两次调用均失败，已回退本地确定性压缩：connect: connection refused";
  assert.equal(
    compactionFailureError({ note }),
    "compact-local:replay-failed 前缀重放两次调用均失败，已回退本地确定性压缩：connect: connection refused"
  );
  // 报错里带引号/等号也不影响：切分点是**最后一个** ` error=` 标记。
  assert.equal(
    compactionFailureError({ note: "no_model_summary err=inner error=readback: \"prefix replay requires history bytes\"" }),
    "readback: \"prefix replay requires history bytes\""
  );
  // 没有这一段（旧痕、或本条压缩失败没走到读数闸）→ 空串：条目上少一行，不编报错。
  assert.equal(compactionFailureError({ note: "ineffective_compact landing=1 soft=2" }), "");
  assert.equal(compactionFailureError({}), "");
  assert.equal(compactionFailureError({ note: "no_model_summary" }), "");
});

test("compactionFrontier takes the last compacted message and the earliest start", () => {
  const frontier = compactionFrontier([
    { version: 1, message_from: "message-1", message_to: "message-20", event_from: 1, event_to: 3, frame_ref: "tr-1" },
    { version: 2, message_from: "message-21", message_to: "message-103", event_from: 4, event_to: 9, frame_ref: "tr-2" }
  ]);
  assert.equal(frontier.index, 1);
  assert.equal(frontier.count, 2);
  assert.equal(frontier.messageToOrdinal, 103);
  assert.equal(frontier.message_from, "message-1");
  assert.equal(frontier.message_to, "message-103");
  assert.equal(frontier.event_from, 1);
  assert.equal(frontier.event_to, 9);
  assert.equal(frontier.frame_ref, "tr-2");
  assert.equal(compactionRangeText(frontier), "消息 message-1..message-103（事件 1..9）");
});

test("compactionFrontier declines to guess when no record carries a message id", () => {
  assert.equal(compactionFrontier([]), null);
  assert.equal(compactionFrontier([{ version: 1, reason: "context_budget", messages_before: 88 }]), null);
});

test("conversationCompactionAnchor places one divider after the last compacted message", () => {
  const messages = [
    { id: "message-1", role: "user" }, { id: "message-2", role: "assistant" },
    { id: "message-3", role: "user" }, { id: "message-4", role: "assistant" }
  ];
  const anchor = conversationCompactionAnchor(messages, [
    { version: 1, message_from: "message-1", message_to: "message-2", compacted_at: "t1" },
    { version: 2, message_from: "message-3", message_to: "message-4", compacted_at: "t2" }
  ]);
  assert.equal(anchor.messageID, "message-4");
  assert.equal(anchor.label, "以上 消息 message-1..message-4已被压缩");
  assert.match(anchor.note, /会话共压缩 2 次/);
});

test("conversationCompactionAnchor keeps the divider off pages that hold nothing compacted", () => {
  // 前沿在更早的那一页：本页消息都在分界之后 —— 这里没有任何已压缩的内容，凭空插一行
  // 虚线就是假线。
  const anchor = conversationCompactionAnchor([{ id: "message-9" }, { id: "message-10" }], [
    { version: 1, message_from: "message-1", message_to: "message-4", compacted_at: "t1" }
  ]);
  assert.equal(anchor, null);
  assert.equal(conversationCompactionAnchor([{ id: "message-1" }], []), null);
});

test("conversationCompactionAnchor clamps to the page end when the whole page is older", () => {
  // 往回翻页：整页都比前沿更早 → 分界落在本页末尾（读作"这一页以上都被折了"）。
  const anchor = conversationCompactionAnchor([{ id: "message-1" }, { id: "message-2" }], [
    { version: 1, message_from: "message-1", message_to: "message-103", compacted_at: "t1" }
  ]);
  assert.equal(anchor.messageID, "message-2");
  assert.equal(anchor.note, "");
  assert.equal(anchor.frameRef, "");
});
