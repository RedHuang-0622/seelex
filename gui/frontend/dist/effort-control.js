export const EFFORT_LEVELS = Object.freeze(["lite", "medium", "high", "max"]);

const EFFORT_LABELS = Object.freeze({
  lite: "Lite",
  medium: "Medium",
  high: "High",
  max: "Max"
});

// 每档对应的液柱宽度百分比（lite 也要有一小段可见）
const PROGRESS_MAP = Object.freeze([10, 38, 66, 100]);

export function effortPresentation(level) {
  const normalized = EFFORT_LEVELS.includes(level) ? level : EFFORT_LEVELS[0];
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

  input.addEventListener("input", () => {
    if (!pending) render(EFFORT_LEVELS[Number(input.value)]);
  });

  input.addEventListener("change", async () => {
    if (pending) return;
    const next = effortPresentation(EFFORT_LEVELS[Number(input.value)]).level;
    if (next === committed) {
      render(committed);
      return;
    }

    pending = true;
    input.disabled = true;
    root.classList.add("is-pending");
    try {
      await selectEffort(next);
      committed = next;
      render(committed);
    } catch (error) {
      render(committed);
      onError(error);
    } finally {
      pending = false;
      input.disabled = false;
      root.classList.remove("is-pending");
    }
  });

  return Object.freeze({
    setLevel(level) {
      committed = effortPresentation(level).level;
      if (!pending) render(committed);
    }
  });
}
