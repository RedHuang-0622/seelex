import assert from "node:assert/strict";
import test from "node:test";

import {
  SPRING_PRESETS,
  dampingRatio,
  springCharacter,
  springEasing,
  springStepResponse
} from "./spring-motion.js";

test("弹簧语义：按压/状态临界阻尼，进入欠阻尼，退出无回弹", () => {
  assert.equal(springCharacter(SPRING_PRESETS.press), "critical");
  assert.equal(springCharacter(SPRING_PRESETS.state), "critical");
  assert.equal(springCharacter(SPRING_PRESETS.enter), "underdamped");
  assert.equal(springCharacter(SPRING_PRESETS.exit), "overdamped");
  assert.equal(springCharacter(SPRING_PRESETS.settle), "underdamped");
  assert.ok(dampingRatio(SPRING_PRESETS.enter) > 0.65 && dampingRatio(SPRING_PRESETS.enter) < 0.85);
  assert.ok(dampingRatio(SPRING_PRESETS.exit) > 1);
});

test("阶跃响应有阻尼：进入轻微过冲后收敛，退出不越过终值", () => {
  const enter = SPRING_PRESETS.enter;
  let peak = 0;
  for (let index = 0; index <= enter.durationMs + 100; index += 1) {
    peak = Math.max(peak, springStepResponse(enter, index / 1000));
  }
  assert.ok(peak > 1, "欠阻尼进入必须有受控过冲");
  assert.ok(peak < 1.08, `进入过冲必须克制，实际峰值 ${peak}`);
  assert.ok(Math.abs(springStepResponse(enter, enter.durationMs / 1000) - 1) < 0.02);

  const exit = SPRING_PRESETS.exit;
  for (let index = 0; index <= 100; index += 1) {
    assert.ok(springStepResponse(exit, index / 1000) <= 1.0001, "过阻尼退出不得回弹越过终值");
  }
});

test("CSS linear() easing 由物理参数生成且终点固定为 1", () => {
  const easing = springEasing(SPRING_PRESETS.enter);
  assert.match(easing, /^linear\(/);
  assert.match(easing, /1 100%\)$/);
  assert.match(easing, /1\.[0-9]+ /, "欠阻尼曲线应保留可见的轻微过冲");
});
