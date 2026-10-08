import { createKnobSlider, discreteScale, linearScale } from "./knob-slider.js";

// composer 的运行参数分成两处（各自只改自己那一项，另一项一个字都不动）：
//
//   · 思考强度（reasoning effort）——"这一问想多深"，贴发送键；
//   · 循环次数（loop limit）——"这一趟跑多少轮"，是运行期预算，落在运行状态弹窗里，
//     外加一个「无限循环」勾选（勾上就按进程级上限 ≈ 无上限跑）。
//
// 一次同时设置两条的 Effort 档位滑杆已从 composer 下线（档位只经 `/effort` 命令切换），
// 所以界面上不存在"一个滑杆同时控制两项"的控件。
//
// 行为（预览 / 提交 / 待定守护 / 失败回滚 / 快照覆盖 / 运行中上锁）全部来自共享组件
// knob-slider.js，本文件只声明两条拉条各自的"停位怎么映射成值"，以及循环次数那条的
// 「无限循环」语义。

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
// 有限循环次数的上界 = max 档默认 loops（application/prompt/effort.go 的
// effortProfiles["max"].maxLoops = 768）。拉条只在"有限"这一段里走。
export const LOOP_LIMIT_MAX = 768;
// 「无限循环」没有另造后端字段，而是复用既有哨兵：limits.input_loop_limit 的默认值
// （goal skill 的"≈无上限"）。勾上就以它下发。
export const LOOP_LIMIT_INFINITE = 9999;
export const LOOP_LIMIT_STEP = 1;
// 无限循环时读数落点显示这个（不是把 9999 摆出来当"数值"）。
export const LOOP_INFINITE_LABEL = "∞";

export const KNOB_RUNNING_HINT = "回合进行中不能调整：等本回合结束后再调（当前值不会排队生效）";

export const REASONING_EFFORT_SCALE = discreteScale(REASONING_EFFORT_LEVELS, {
  labels: REASONING_EFFORT_LABELS,
  progress: REASONING_EFFORT_PROGRESS
});

// loopLimitScale 是有限循环次数的线性刻度：增长量恒为 1（用户口径里明确要求的"线性变化"）。
export function loopLimitScale({ min = LOOP_LIMIT_MIN, max = LOOP_LIMIT_MAX, step = LOOP_LIMIT_STEP } = {}) {
  return linearScale({ min, max, step });
}

// isInfiniteLoop 判定一份权威读数是否代表"无限循环"：只有既有哨兵（>= 进程级上限的
// 默认值）算；有限区间里的任何读数都不算。
export function isInfiniteLoop(limit) {
  const numeric = Number(limit);
  return Number.isFinite(numeric) && numeric >= LOOP_LIMIT_INFINITE;
}

export function createComposerKnobs({
  reasoning,          // { root, input, output } —— 推理强度
  loop,               // { root, input, output } —— 循环次数
  infinite = null,    // { checkbox, valueText } —— 「无限循环」勾选与读数落点（可选）
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
      const settled = applied === undefined || applied === null ? value : Number(applied);
      // 记住最后一份**有限**权威值：取消「无限循环」时回到它。
      if (Number.isFinite(settled) && settled < LOOP_LIMIT_INFINITE) finiteLimit = settled;
      return settled;
    },
    onError
  });

  const checkbox = infinite?.checkbox || null;
  const valueText = infinite?.valueText || loop.output || null;

  let enabled = true;
  let infiniteOn = false;
  let finiteLimit = loopSlider.value();
  let infinitePending = false;

  // paintInfinite 把「无限循环」的当前态铺到勾选、滑杆可用性与读数落点上。滑杆在无限
  // 态下不可用（9999 不在有限刻度里），读数改显 LOOP_INFINITE_LABEL。
  function paintInfinite() {
    if (checkbox) {
      checkbox.checked = infiniteOn;
      checkbox.disabled = !enabled;
    }
    loopSlider.setEnabled(enabled && !infiniteOn);
    if (valueText) {
      valueText.textContent = infiniteOn ? LOOP_INFINITE_LABEL : String(loopSlider.value());
      valueText.title = enabled ? "" : KNOB_RUNNING_HINT;
    }
  }

  async function commitInfinite(next) {
    infinitePending = true;
    if (checkbox) checkbox.disabled = true;
    try {
      const target = next ? LOOP_LIMIT_INFINITE : finiteLimit;
      const applied = typeof setLoopLimit === "function" ? await setLoopLimit(target) : undefined;
      const settled = applied === undefined || applied === null ? target : Number(applied);
      if (isInfiniteLoop(settled)) {
        infiniteOn = true;
      } else {
        infiniteOn = false;
        finiteLimit = settled;
        loopSlider.set(settled);
      }
    } catch (error) {
      onError(error);
    } finally {
      infinitePending = false;
      paintInfinite();
    }
  }

  if (checkbox) {
    checkbox.addEventListener("change", () => {
      if (!enabled || infinitePending) {
        paintInfinite();
        return;
      }
      commitInfinite(Boolean(checkbox.checked));
    });
  }

  paintInfinite();

  return Object.freeze({
    setReasoningEffort(value) {
      reasoningSlider.set(value);
    },
    setLoopLimit(limit) {
      const numeric = Number(limit);
      if (!Number.isFinite(numeric)) {
        paintInfinite();
        return;
      }
      if (isInfiniteLoop(numeric)) {
        infiniteOn = true;
      } else {
        infiniteOn = false;
        finiteLimit = numeric;
        loopSlider.set(numeric);
      }
      paintInfinite();
    },
    setEnabled(next) {
      enabled = next !== false;
      reasoningSlider.setEnabled(enabled);
      loopSlider.setEnabled(enabled && !infiniteOn);
      if (checkbox) checkbox.disabled = !enabled;
      if (valueText) valueText.title = enabled ? "" : KNOB_RUNNING_HINT;
    },
    reasoningEffort() {
      return reasoningSlider.value();
    },
    loopLimit() {
      return infiniteOn ? LOOP_LIMIT_INFINITE : loopSlider.value();
    },
    isInfinite() {
      return infiniteOn;
    },
    isEnabled() {
      return enabled && reasoningSlider.isEnabled();
    }
  });
}
