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

export function createEffortControl({ root, input, output, selectEffort, onError = () => {} }) {
  if (!root || !input || typeof selectEffort !== "function") {
    throw new TypeError("Effort control requires root, input and selectEffort");
  }

  let committed = effortPresentation(EFFORT_LEVELS[Number(input.value)]).level;
  let pending = false;
  // enabled=false 用于「回合进行中」：此时后端一定会拒绝切换，前端先把入口
  // 关掉，而不是让用户拖完再看一个失败 toast（那正是"调节 effort 出问题"）。
  let enabled = true;

  function render(level) {
    const view = effortPresentation(level);
    input.value = String(view.index);
    input.setAttribute("aria-valuetext", view.label);
    if (output) {
      output.value = view.label;
      output.textContent = view.label;
    }
    root.dataset.effort = view.level;
    root.style.setProperty("--effort-progress", `${view.progress}%`);
    return view;
  }

  function applyEnabled() {
    input.disabled = !enabled;
    root.classList.toggle("is-locked", !enabled);
    if (output) output.title = enabled ? "" : EFFORT_RUNNING_HINT;
    if (enabled) input.removeAttribute("title");
    else input.setAttribute("title", EFFORT_RUNNING_HINT);
  }

  input.addEventListener("input", () => {
    if (!enabled || pending) return;
    render(EFFORT_LEVELS[Number(input.value)]);
  });

  input.addEventListener("change", async () => {
    if (!enabled || pending) return;
    const next = effortPresentation(EFFORT_LEVELS[Number(input.value)]).level;
    if (next === committed) {
      render(committed);
      return;
    }

    pending = true;
    input.disabled = true;
    root.classList.add("is-pending");
    try {
      const applied = await selectEffort(next);
      // selectEffort 返回后端"真正生效的值"时以它为准（后端可能拒绝或归一化）；
      // 返回 undefined（旧调用方）时沿用请求值。
      committed = isEffortLevel(applied) ? applied : next;
      render(committed);
    } catch (error) {
      render(committed);
      onError(error);
    } finally {
      pending = false;
      applyEnabled();
      root.classList.remove("is-pending");
    }
  });

  return Object.freeze({
    // setLevel 只在拿到已知等级时才改状态：快照缺 effort 字段（跨会话投影、
    // 冷加载中的空 runtime）时不要把用户刚选的档位打回 Lite。
    setLevel(level) {
      if (!isEffortLevel(level)) return;
      committed = level;
      if (!pending) render(committed);
    },
    // setEnabled(false) = 回合进行中：滑块置灰且保留当前档位显示。
    setEnabled(next) {
      enabled = next !== false;
      applyEnabled();
    },
    level() {
      return committed;
    },
    isEnabled() {
      return enabled;
    }
  });
}
