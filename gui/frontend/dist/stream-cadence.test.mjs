import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

import {
  STREAM_ENTER_HYSTERESIS_MS,
  STREAM_EXIT_HYSTERESIS_MS,
  STREAM_MAX_COALESCE_MS,
  STREAM_TICK_MS,
  createStreamCadence
} from "./stream-cadence.js";

function fakeClock() {
  let time = 0;
  let sequence = 0;
  const tasks = new Map();
  function schedule(callback, delay = 0) {
    const id = ++sequence;
    tasks.set(id, { at: time + Math.max(0, Number(delay) || 0), callback });
    return id;
  }
  function cancel(id) {
    tasks.delete(id);
  }
  function advance(ms) {
    const target = time + Math.max(0, Number(ms) || 0);
    while (true) {
      const due = [...tasks.entries()]
        .filter(([, task]) => task.at <= target)
        .sort((a, b) => a[1].at - b[1].at || a[0] - b[0])[0];
      if (!due) break;
      const [id, task] = due;
      tasks.delete(id);
      time = task.at;
      task.callback();
    }
    time = target;
  }
  return { now: () => time, schedule, cancel, frame: callback => schedule(callback, 0), advance, pending: () => tasks.size };
}

function makeScheduler() {
  const clock = fakeClock();
  const renders = [];
  const terminal = [];
  const phases = [];
  const cadence = createStreamCadence({
    now: clock.now,
    setTimeout: clock.schedule,
    clearTimeout: clock.cancel,
    requestAnimationFrame: clock.frame,
    onRender: (frame, phase) => renders.push({ frame, phase, at: clock.now() }),
    onPhaseChange: phase => phases.push({ phase, at: clock.now() }),
    onTerminal: frame => terminal.push({ frame, at: clock.now() })
  });
  return { cadence, clock, renders, terminal, phases };
}

test("cadence 常量与宪法一致，且 50ms 内多次 chunk 只合并刷新一次", () => {
  assert.equal(STREAM_TICK_MS, 50);
  assert.equal(STREAM_ENTER_HYSTERESIS_MS, 100);
  assert.equal(STREAM_EXIT_HYSTERESIS_MS, 150);
  assert.equal(STREAM_MAX_COALESCE_MS, 50);

  const { cadence, clock, renders } = makeScheduler();
  cadence.push({ text: "a" });
  assert.equal(renders.length, 0, "首个 chunk 必须等下一帧");
  clock.advance(0);
  assert.equal(renders.length, 1);
  assert.equal(renders[0].frame.text, "a");

  cadence.push({ text: "ab" });
  cadence.push({ text: "abc" });
  cadence.push({ text: "abcd" });
  clock.advance(49);
  assert.equal(renders.length, 1, "50ms 内不得逐 chunk 刷新");
  clock.advance(1);
  assert.equal(renders.length, 2);
  assert.equal(renders[1].frame.text, "abcd", "合并必须 latest-wins");
});

test("进入流态需 100ms 滞回，退出需 150ms 滞回", () => {
  const { cadence, clock, phases } = makeScheduler();
  cadence.push({ text: "a" });
  clock.advance(99);
  assert.equal(phases.length, 0, "不足 100ms 不得切接收中");
  clock.advance(1);
  assert.deepEqual(phases, [{ phase: "receiving", at: 100 }]);

  cadence.push({ text: "ab" });
  clock.advance(149);
  assert.equal(phases.at(-1).phase, "receiving", "不足 150ms 不得退出接收中");
  clock.advance(1);
  assert.deepEqual(phases.at(-1), { phase: "idle", at: 250 });
});

test("terminal 先 flush 尾包，再进入稳定态；之后不再排渲染", () => {
  const { cadence, clock, renders, terminal, phases } = makeScheduler();
  cadence.push({ text: "a" });
  clock.advance(0);
  clock.advance(100);

  cadence.push({ text: "ab" });
  cadence.terminal({ text: "abc", done: true });
  assert.equal(renders.at(-1).frame.text, "ab", "terminal 前必须先 flush 尚未刷新的尾包");
  assert.equal(terminal.at(-1).frame.text, "abc", "terminal 帧随后独立提交");
  assert.equal(phases.at(-1).phase, "idle", "terminal 立即退出接收中");
  const renderCount = renders.length;
  clock.advance(200);
  assert.equal(renders.length, renderCount, "terminal 后不得再排 tick");
});

test("app 接线：delta 走 cadence，terminal/快照边界先收尾", async () => {
  const source = await readFile(new URL("./app.js", import.meta.url), "utf8");
  assert.match(source, /onIncremental:\s*handleIncremental/);
  assert.match(source, /kind === "message\.delta"[\s\S]{0,120}streamCadence\.push/);
  assert.match(source, /kind === "chat\.changed" && !snapshot\.chat\?\.running[\s\S]{0,120}streamCadence\.terminal/);
  assert.match(source, /onTerminal:\s*frame =>[\s\S]{0,160}streamPhase:\s*"idle"/);
  assert.match(source, /onSnapshot:[\s\S]{0,160}streamCadence\.reset\(\)/);
  assert.match(source, /streamCadence\.flush\(\);\s*renderIncrementalBuffered\(snapshot, kind, payload/);
});
