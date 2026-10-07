// ── 滑杆组件的**唯一实现**（composer 底下三条拉条共用）────────────────────
//
// 三条拉条（Effort 档位 / 推理强度 / 循环次数）的行为是同一条，此前 effort-control.js
// 自己写了一份；现在行为只写在这里，effort-control.js 退化成"这份实现 + 一张四档停位表"。
//
// 行为契约（全套都一样）：
//
//   拖动(input)   → 只预览，不碰后端（拖动期间连发请求会把引擎与 prompt 栈打成
//                   一串中间态）
//   松手(change)  → 提交一次；pending 期间的重复 change 被守护忽略
//   成功          → 采纳后端**真正生效的值**（不是假定请求值生效：后端可能拒绝或
//                   归一化，UI 必须显示真的那个）
//   失败          → 回滚到最后一份权威值，错误交 onError
//   快照覆盖      → set(...)：命令行 /effort、TUI、远端控制改的值也能同步到拉条
//   运行中        → setEnabled(false)：置灰 + 拖动既不预览也不提交（后端一定会拒，
//                   前端不该先答应再弹一个失败 toast）
//
// 三条拉条的差异只有两样：**停位怎么映射成值**（scale）与**写哪个 data-*/CSS 变量**
// （CSS 侧同构，于是复用同一套 .effort-control / .syringe-* 规则）。差异由参数表达。

// discreteScale 是离散停位表：values 是**有序**的取值，index 就是 range 的位置。
export function discreteScale(values, { labels = {}, progress = [] } = {}) {
  const list = Object.freeze([...values]);
  return Object.freeze({
    kind: "discrete",
    values: list,
    length: list.length,
    valueAt: index => list[index],
    labelAt: index => labels[list[index]] ?? String(list[index]),
    indexOf: value => list.indexOf(value),
    progressAt: index => (progress.length ? progress[index] : ((index + 1) / list.length) * 100)
  });
}

// linearScale 是**线性整数**停位表：value = min + index * step，增长量恒为 step。
//
// 为什么 range 的 value 仍是 index 而不是值本身：三条拉条共用的命中区逻辑要求
// "位置 0..N 均匀"，把它们统一成 index 才能用同一份 render；值↔index 的双向映射
// 由本表负责（indexOf 越界/非整数/不在步长上返回 -1，调用方据此忽略不可信读数）。
export function linearScale({ min, max, step = 1, unit = "" } = {}) {
  const span = Math.max(0, Math.floor((max - min) / step));
  const length = span + 1;
  const valueAt = index => min + index * step;
  return Object.freeze({
    kind: "linear",
    min,
    max,
    step,
    length,
    valueAt,
    labelAt: index => `${valueAt(index)}${unit}`,
    indexOf: value => {
      const numeric = Number(value);
      if (!Number.isFinite(numeric)) return -1;
      if (numeric < min || numeric > max) return -1;
      if (Math.round(numeric) !== numeric) return -1;
      if ((numeric - min) % step !== 0) return -1;
      return Math.round((numeric - min) / step);
    },
    progressAt: index => (length <= 1 ? 100 : (index / (length - 1)) * 100)
  });
}

export function createKnobSlider({
  root,
  input,
  output = null,
  scale,
  dataKey = "effort",
  progressVar = "--effort-progress",
  runningHint = "回合进行中不能调整：等本回合结束后再调",
  commit,
  onError = () => {}
}) {
  if (!root || !input || !scale || typeof commit !== "function") {
    throw new TypeError("Knob slider requires root, input, scale and commit");
  }

  input.min = "0";
  input.max = String(scale.length - 1);
  input.step = "1";

  function normalizeIndex(raw) {
    const index = Math.round(Number(raw));
    if (!Number.isFinite(index) || index < 0) return 0;
    if (index > scale.length - 1) return scale.length - 1;
    return index;
  }

  let committed = normalizeIndex(input.value);
  let pending = false;
  let enabled = true;

  function render(index) {
    const position = normalizeIndex(index);
    input.value = String(position);
    const label = scale.labelAt(position);
    input.setAttribute("aria-valuetext", label);
    if (output) {
      output.value = label;
      output.textContent = label;
    }
    root.dataset[dataKey] = String(scale.valueAt(position));
    root.style.setProperty(progressVar, `${scale.progressAt(position)}%`);
    return position;
  }

  function applyEnabled() {
    input.disabled = !enabled;
    root.classList.toggle("is-locked", !enabled);
    if (output) output.title = enabled ? "" : runningHint;
    if (enabled) input.removeAttribute("title");
    else input.setAttribute("title", runningHint);
  }

  input.addEventListener("input", () => {
    if (!enabled || pending) return;
    render(Number(input.value));
  });

  input.addEventListener("change", async () => {
    if (!enabled || pending) return;
    const index = normalizeIndex(Number(input.value));
    if (index === committed) {
      render(committed);
      return;
    }
    pending = true;
    input.disabled = true;
    root.classList.add("is-pending");
    try {
      const applied = await commit(scale.valueAt(index));
      // 后端返回"真正生效的值"时以它为准；返回 undefined（旧调用方）或不可识别
      // 的值时沿用请求值。
      const adopted = scale.indexOf(applied);
      committed = adopted >= 0 ? adopted : index;
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

  render(committed);
  applyEnabled();

  return Object.freeze({
    // set 只在拿到本表认识的值时才改状态：快照缺字段（跨会话投影、冷加载中的空
    // runtime）时不要把用户刚选的值打回 0。
    set(value) {
      const index = scale.indexOf(value);
      if (index < 0) return;
      committed = index;
      if (!pending) render(committed);
    },
    setIndex(index) {
      committed = normalizeIndex(index);
      if (!pending) render(committed);
    },
    // setEnabled(false) = 运行中：置灰且保留当前值显示。
    setEnabled(next) {
      enabled = next !== false;
      applyEnabled();
    },
    value() {
      return scale.valueAt(committed);
    },
    index() {
      return committed;
    },
    isEnabled() {
      return enabled;
    }
  });
}
