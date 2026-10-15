// 「每账号上下文窗口」的取值面（设置 → 账号）。
//
// 挡位表由**后端下发**（bridge 的 AccountWindowPresets，源头是 seelexctx/window.go）：
// 前端不写死 200k/400k/600k/1M 那份清单，否则改挡位要改两处、必然漂移。
// 本模块只做三件事：把 token 数渲染成短标签、把挡位表拼成 <option> 清单、把用户输入
// 归一成"要提交给后端的整数"或 null（null = 这次不改，调用方给出提示）。

// ACCOUNT_WINDOW_CUSTOM 是"自定义"选项的值（不是合法窗口，只是控件层的哨兵）。
export const ACCOUNT_WINDOW_CUSTOM = "custom";

// accountWindowLabel 把 token 数渲染成 200k / 1M 这样的短标签；非法值说"未设置"。
export function accountWindowLabel(window) {
  const value = Number(window);
  if (!Number.isFinite(value) || value <= 0) return "未设置";
  if (value % 1000000 === 0) return `${value / 1000000}M`;
  if (value % 1000 === 0) return `${value / 1000}k`;
  return String(value);
}

// accountWindowOptions 返回挡位选项（后端挡位 + 「自定义…」）。
export function accountWindowOptions(presets) {
  const list = Array.isArray(presets) ? presets : [];
  const options = list
    .map(preset => Number(preset))
    .filter(preset => Number.isSafeInteger(preset) && preset > 0)
    .map(preset => ({ value: String(preset), label: accountWindowLabel(preset) }));
  options.push({ value: ACCOUNT_WINDOW_CUSTOM, label: "自定义…" });
  return options;
}

// normalizeAccountWindowInput 把自定义输入框里的文本归一成整数；不合法返回 null。
// 只收纯数字（不带 k/M 后缀）：谁来解释"1M 等于多少"必须只有一处（后端），输入框里
// 再放一套单位解析就等于两处口径。
export function normalizeAccountWindowInput(raw) {
  const text = String(raw ?? "").trim();
  if (!text) return null;
  if (!/^[0-9]+$/.test(text)) return null;
  const value = Number(text);
  if (!Number.isSafeInteger(value) || value <= 0) return null;
  return value;
}
