import { createKnobSlider, discreteScale } from "./knob-slider.js";

// Effort 档位控制器 = 共享滑杆组件（knob-slider.js）+ 一张四档停位表。
//
// 行为一个字都没有在这里重写：拖动只预览、松手才提交、待定守护、失败回滚、
// 快照覆盖、运行中上锁全在组件里。本文件只声明"档位是什么"。
export const EFFORT_LEVELS = Object.freeze(["lite", "medium", "high", "max"]);

const EFFORT_LABELS = Object.freeze({
  lite: "Lite",
  medium: "Medium",
  high: "High",
  max: "Max"
});

// 每档对应的视觉进度百分比（lite 也要有一小段可见，否则像"没生效"）。
const PROGRESS_MAP = Object.freeze([10, 38, 66, 100]);

// EFFORT_HINT 是滑块不可用时的说明（回合进行中禁止切换 effort：G0b/INV-G7）。
export const EFFORT_RUNNING_HINT = "回合进行中不能切换强度：等本回合结束后再调（当前值不会排队生效）";

export function isEffortLevel(level) {
  return typeof level === "string" && EFFORT_LEVELS.includes(level);
}

export function effortPresentation(level) {
  const normalized = isEffortLevel(level) ? level : EFFORT_LEVELS[0];
  const index = EFFORT_LEVELS.indexOf(normalized);
  return Object.freeze({
    level: normalized,
    label: EFFORT_LABELS[normalized],
    index,
    progress: PROGRESS_MAP[index],
    isMax: normalized === "max"
  });
}

export const EFFORT_SCALE = discreteScale(EFFORT_LEVELS, { labels: EFFORT_LABELS, progress: PROGRESS_MAP });

export function createEffortControl({ root, input, output, selectEffort, onError = () => {} }) {
  if (!root || !input || typeof selectEffort !== "function") {
    throw new TypeError("Effort control requires root, input and selectEffort");
  }

  const slider = createKnobSlider({
    root,
    input,
    output,
    scale: EFFORT_SCALE,
    dataKey: "effort",
    progressVar: "--effort-progress",
    runningHint: EFFORT_RUNNING_HINT,
    commit: selectEffort,
    onError
  });

  return Object.freeze({
    // setLevel 只在拿到已知等级时才改状态：快照缺 effort 字段（跨会话投影、
    // 冷加载中的空 runtime）时不要把用户刚选的档位打回 Lite。
    setLevel(level) {
      if (!isEffortLevel(level)) return;
      slider.set(level);
    },
    // setEnabled(false) = 回合进行中：滑块置灰且保留当前档位显示。
    setEnabled(next) {
      slider.setEnabled(next);
    },
    level() {
      return slider.value();
    },
    isEnabled() {
      return slider.isEnabled();
    }
  });
}
