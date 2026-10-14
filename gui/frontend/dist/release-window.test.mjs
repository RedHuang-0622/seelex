// 「会话驻留窗口」取值面的口径测试（release-window.js）。
//
// 这里钉住的是**取值与文案**，不是接线：归一化必须把不可信读数判成 null（区分"后端
// 说了个数"与"这格没有权威读数"），而不是兜底成默认值——兜底会让界面显示一个后端
// 其实没说的值，正是"控件显示的和真正生效的不是一回事"那类缺陷。
import assert from "node:assert/strict";
import test from "node:test";

import {
  RELEASE_WINDOW_DEFAULT,
  RELEASE_WINDOW_MAX,
  RELEASE_WINDOW_MIN,
  normalizeReleaseWindow,
  releaseWindowLabel,
  releaseWindowOptions
} from "./release-window.js";

test("归一化：区间内的整数（含数字串）被接受", () => {
  for (let value = RELEASE_WINDOW_MIN; value <= RELEASE_WINDOW_MAX; value += 1) {
    assert.equal(normalizeReleaseWindow(value), value, `${value} 应当被接受`);
  }
  assert.equal(normalizeReleaseWindow("3"), 3, "后端/下拉框给的是字符串");
  assert.equal(normalizeReleaseWindow(RELEASE_WINDOW_MAX), RELEASE_WINDOW_MAX);
});

test("归一化：越界、非整数、非数值一律判 null（不兜底成默认值）", () => {
  for (const value of [0, -1, RELEASE_WINDOW_MAX + 1, 2.5, "abc", "", null, undefined, NaN, Infinity, {}]) {
    assert.equal(normalizeReleaseWindow(value), null, `${String(value)} 应当判 null`);
  }
});

test("文案：默认那一格说明语义，其余就是数字", () => {
  assert.equal(releaseWindowLabel(RELEASE_WINDOW_DEFAULT), "1（默认：只保留上一个）");
  assert.equal(releaseWindowLabel(2), "2");
  assert.equal(releaseWindowLabel(RELEASE_WINDOW_MAX), String(RELEASE_WINDOW_MAX));
  assert.equal(releaseWindowLabel(0), "", "非法值给空串（调用方据此不写 DOM）");
  assert.equal(releaseWindowLabel("x"), "");
});

test("选项表：1..16 各一条，值升序且与边界一致", () => {
  const options = releaseWindowOptions();
  assert.equal(options.length, RELEASE_WINDOW_MAX - RELEASE_WINDOW_MIN + 1);
  assert.deepEqual(options.map(option => option.value), Array.from({ length: options.length }, (_, index) => RELEASE_WINDOW_MIN + index));
  assert.equal(options[0].label, "1（默认：只保留上一个）");
  assert.equal(options[options.length - 1].value, RELEASE_WINDOW_MAX);
});
