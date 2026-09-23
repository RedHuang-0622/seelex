// 压缩进度面的累计逻辑（compaction-format.mergeCompactionProgress）与耗时文案。
//
// 为什么这些要在这里测而不是在 app.js 里：app.js 是"存 + 定时撤条"的薄层，
// DOM 之外动不了；累计逻辑一旦写进它就只能靠手点验证。抽成纯函数后，"新一轮是否
// 清空上一轮的清单""中途失败是否停在真正走过的格子"这类判据都能钉住——它们正是
// 用户读到"两轮混在一起"或"明明失败却满格"的原因。
import assert from "node:assert/strict";
import test from "node:test";

import { compactionGateDurationText, mergeCompactionProgress } from "./compaction-format.js";

const fold = (...frames) => frames.reduce((acc, frame) => mergeCompactionProgress(acc, frame), null);

test("a begin frame opens a round with nothing counted and nothing timed", () => {
  const progress = fold({ state: "running", phase: "begin", gate: "judge", index: 0, total: 6, origin: "explicit" });
  assert.equal(progress.state, "running");
  assert.equal(progress.phase, "begin");
  assert.equal(progress.index, 0);
  assert.equal(progress.total, 6);
  assert.equal(progress.gates.length, 0);
  assert.equal(progress.elapsedMs, 0);
  assert.equal(progress.origin, "explicit");
});

test("gate frames accumulate in execution order with their own durations", () => {
  const progress = fold(
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
  const progress = fold(
    { state: "running", gate: "judge", index: 1, total: 6, elapsed_ms: 28 },
    { state: "running", gate: "judge", index: 1, total: 6, elapsed_ms: 31, detail: "compared=2" }
  );
  assert.equal(progress.gates.length, 1);
  assert.equal(progress.gates[0].ms, 31);
  assert.equal(progress.gates[0].detail, "compared=2");
});

test("the terminal frame carries the version, outcome and total elapsed", () => {
  const progress = fold(
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
  const progress = fold(
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
  // 自动路径没有起手帧（要不要折叠正是判据估算的结果），新轮的第一帧就是判据关
  // 收口。判新轮的判据只能是"上一轮已收口"——否则两轮的耗时会被读成一轮。
  const first = fold(
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
  const progress = fold({ state: "running", gate: "judge", index: 1, total: 6, elapsed_ms: 28 });
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
  const running = fold({ state: "running", phase: "begin", gate: "judge", index: 0, total: 6, origin: "explicit" });
  assert.deepEqual(running.gates, []);
  assert.equal(running.elapsedMs, 0);
  assert.equal(running.index, 0);
  const failed = mergeCompactionProgress(running, { state: "failed", total: 6, origin: "explicit", outcome: "assembly exploded" });
  assert.deepEqual(failed.gates, []);
  assert.equal(failed.state, "failed");
  assert.equal(failed.outcome, "assembly exploded");
});
