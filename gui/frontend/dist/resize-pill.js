// 拖拽尺寸读条（拟物标尺）：拖动分区 / 终端 / 文件详情分隔条时，在指针旁贴
// 一枚小药丸显示当前像素尺寸（黄铜边、等宽数字），松手即消失。
//
// 这是 app.js 与 terminal-panel.js 共用的唯一来源——两侧的拖拽骨架都调这里，
// 不各自造一份 DOM 与计时器，避免样式/时序漂移。模块只在被调用时才碰 document，
// 因此 node --test 直接导入不会有副作用。

let pillEl = null;
let flashTimer = 0;

function ensurePill() {
  if (pillEl) return pillEl;
  if (typeof document === "undefined" || typeof document.createElement !== "function") return null;
  pillEl = document.createElement("div");
  pillEl.className = "resize-pill hidden";
  const host = document.body || document.documentElement;
  host?.appendChild?.(pillEl);
  return pillEl;
}

// showResizePill 显示读条并贴到 (x, y)；缺省只在已有节点上改文字，不新建。
export function showResizePill(text, x, y) {
  const pill = ensurePill();
  if (!pill) return;
  if (flashTimer) { clearTimeout(flashTimer); flashTimer = 0; }
  pill.textContent = String(text);
  pill.classList?.remove("hidden");
  if (Number.isFinite(x) && Number.isFinite(y)) {
    pill.style.left = `${x}px`;
    pill.style.top = `${y}px`;
  }
}

export function hideResizePill() {
  if (flashTimer) { clearTimeout(flashTimer); flashTimer = 0; }
  pillEl?.classList?.add("hidden");
}

// flashResizePill：给键盘微调用的短时读条（自动消失）。
export function flashResizePill(text, x, y, ms = 700) {
  showResizePill(text, x, y);
  if (flashTimer) clearTimeout(flashTimer);
  flashTimer = setTimeout(hideResizePill, ms);
}
