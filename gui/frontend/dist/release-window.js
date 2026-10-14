// 「会话驻留窗口」的取值面（设置弹窗 → 会话驻留）：取值区间、取值文案、以及把后端
// 读数归一成一个可显示取值的判据。
//
// 为什么单独成文件：`app.js` 只做 DOM 写入与 invoke 接线，口径放这里就能被
// `node --test` 直接钉住（同 `permission-tier.js` / `composer-knobs.js` 的姿势）。
//
// 后端权威是**进程级** limits 的 `session_release_window`（`seelexctx` 的
// `SessionReleaseWindowMin/Max` 是同一组边界的另一半，两边一起改）。界面上只暴露
// 1..16：不留 0（"谁都不留"= 每次切会话都冷加载）也不留"永不释放"格（那等于把内存
// 治理整块关掉）。

export const RELEASE_WINDOW_MIN = 1;
export const RELEASE_WINDOW_MAX = 16;
// 出厂口径：受保护集之外只保留上一个（受保护集 = 当前视图 / 待审批 / 运行·排队·在飞
// / 正在恢复）。
export const RELEASE_WINDOW_DEFAULT = 1;

// normalizeReleaseWindow 把一份**不可信**的读数归一成合法窗口，非法/缺省返回 null。
//
// 返回 null 而不是兜底成默认值：调用方据此区分"后端说了一个值"与"这一格现在没有
// 权威读数"（桥没就绪、字段跨版本缺失）。把 null 兜成 1 会让界面显示一个后端其实
// 没说的值——正是"控件显示的和真正生效的不是一回事"那类缺陷。
export function normalizeReleaseWindow(value) {
  const numeric = Number(value);
  if (!Number.isFinite(numeric)) return null;
  if (Math.round(numeric) !== numeric) return null;
  if (numeric < RELEASE_WINDOW_MIN || numeric > RELEASE_WINDOW_MAX) return null;
  return numeric;
}

// releaseWindowLabel 是选项/读数的文案；非法值给空串（调用方据此不写 DOM）。
export function releaseWindowLabel(window) {
  const value = normalizeReleaseWindow(window);
  if (value === null) return "";
  if (value === RELEASE_WINDOW_DEFAULT) return `${value}（默认：只保留上一个）`;
  return String(value);
}

// releaseWindowOptions 是下拉框的选项表（由 interface 生成，index.html 里不抄一份
// 16 行字面量）。
export function releaseWindowOptions() {
  const options = [];
  for (let value = RELEASE_WINDOW_MIN; value <= RELEASE_WINDOW_MAX; value += 1) {
    options.push({ value, label: releaseWindowLabel(value) });
  }
  return options;
}
