import { createKnobSlider, discreteScale, linearScale } from "./knob-slider.js";

// composer 底部的两条**独立**拉条：推理强度（reasoning effort）与循环次数（loop times）。
//
// 口径（写在 application/core/service_knobs.go，这里只是它的界面）：拖一条拉条**只**改
// 它那一项，另一项一个字都不动。一次同时设置两条的 Effort 档位滑杆已从 composer 下线
// （档位改成只经 `/effort` 命令切换），所以界面上不存在"一个滑杆同时控制两项"的控件。
//
// 行为（预览 / 提交 / 待定守护 / 失败回滚 / 快照覆盖 / 运行中上锁）全部来自共享组件
// knob-slider.js，本文件只声明两条拉条各自的"停位怎么映射成值"。

// 推理强度用 provider 词表（low/medium/high/max）。
//
// 刻意**不含** minimal：它只在部分模型上可用（GPT-5 系），摆到界面上等于给大多数
// 模型一个会被 provider 拒绝的合法按钮。也不含 "session"——那是"跟随档位"的同义词，
// 是**没有覆盖**，不是一条拉条能停的位置（要回到跟随，跑一次 `/effort` 命令即可）。
export const REASONING_EFFORT_LEVELS = Object.freeze(["low", "medium", "high", "max"]);

const REASONING_EFFORT_LABELS = Object.freeze({
  low: "Low",
  medium: "Medium",
  high: "High",
  max: "Max"
});

// 标签导出给"定时任务弹窗里那一格思考强度"用：同一份停位与文案，别处不再抄一遍
// （拉条与任务声明停的是同一组 wire 值）。
export { REASONING_EFFORT_LABELS };

// 视觉进度与四档停位对齐（低档也留一小段，避免看起来"没生效"）。
const REASONING_EFFORT_PROGRESS = Object.freeze([15, 45, 75, 100]);

export const LOOP_LIMIT_MIN = 1;
// 上界 = limits.input_loop_limit 的默认值（9999 就是 goal skill 用的"≈无上限"哨兵）。
// 超过它就不再表达"无上限"，所以拉条不允许推得更远；后端也是同一判据（会拒绝）。
export const LOOP_LIMIT_MAX = 9999;
export const LOOP_LIMIT_STEP = 1;

export const KNOB_RUNNING_HINT = "回合进行中不能调整：等本回合结束后再调（当前值不会排队生效）";

export const REASONING_EFFORT_SCALE = discreteScale(REASONING_EFFORT_LEVELS, {
  labels: REASONING_EFFORT_LABELS,
  progress: REASONING_EFFORT_PROGRESS
});

// loopLimitScale 是循环次数的线性刻度：增长量恒为 1（用户口径里明确要求的"线性变化"）。
export function loopLimitScale({ min = LOOP_LIMIT_MIN, max = LOOP_LIMIT_MAX, step = LOOP_LIMIT_STEP } = {}) {
  return linearScale({ min, max, step });
}

export function createComposerKnobs({
  reasoning,          // { root, input, output } —— 推理强度
  loop,               // { root, input, output } —— 循环次数
  setReasoningEffort, // async (value: string) => 后端真正生效的值 | undefined
  setLoopLimit,       // async (limit: number) => 后端真正生效的值 | undefined
  loopScale = loopLimitScale(),
  onError = () => {}
}) {
  if (!reasoning || !loop) throw new TypeError("Composer knobs require reasoning and loop targets");

  const reasoningSlider = createKnobSlider({
    root: reasoning.root,
    input: reasoning.input,
    output: reasoning.output,
    scale: REASONING_EFFORT_SCALE,
    dataKey: "effort",
    progressVar: "--effort-progress",
    runningHint: KNOB_RUNNING_HINT,
    commit: async value => {
      if (typeof setReasoningEffort !== "function") return undefined;
      const applied = await setReasoningEffort(value);
      // 后端返回值可能是被归一化过的串；无法识别时沿用请求值（组件已处理）。
      return applied === undefined || applied === null ? value : String(applied);
    },
    onError
  });

  const loopSlider = createKnobSlider({
    root: loop.root,
    input: loop.input,
    output: loop.output,
    scale: loopScale,
    dataKey: "effort",
    progressVar: "--effort-progress",
    runningHint: KNOB_RUNNING_HINT,
    commit: async value => {
      if (typeof setLoopLimit !== "function") return undefined;
      const applied = await setLoopLimit(value);
      return applied === undefined || applied === null ? value : Number(applied);
    },
    onError
  });

  return Object.freeze({
    setReasoningEffort(value) {
      reasoningSlider.set(value);
    },
    setLoopLimit(limit) {
      loopSlider.set(limit);
    },
    // 运行中两条一起上锁（后端对两者是同一条守卫：视图会话 running 时拒绝）。
    setEnabled(next) {
      reasoningSlider.setEnabled(next);
      loopSlider.setEnabled(next);
    },
    reasoningEffort() {
      return reasoningSlider.value();
    },
    loopLimit() {
      return loopSlider.value();
    },
    isEnabled() {
      return reasoningSlider.isEnabled() && loopSlider.isEnabled();
    }
  });
}
