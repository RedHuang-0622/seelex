// plugin-source.js — 「按会话插件装配」在**提交侧**的规整（唯一一处）。
//
// 与展示 / 回读侧的规整（agent-team-view.js 的 normalizePluginNames）**只差一步**：
// 本文件**不去重**。口径是 leader 2026-10-05 的裁决：
//
//   提交侧只做 trim + 丢空项；重复项**原样提交**，由后端按既有口径裁决
//   （application/contract/dto/plugin_assembly.go 的 NormalizePlugins：
//   「重复声明显式拒绝，不静默去重」）并把错误回显；
//   回读 / 展示侧保持现状（去重后的清单更好读，且不改变已落盘的事实）。
//
// 为什么前端不"体贴地"去重：前端悄悄砍掉一个重复项 = 用户提交的名单与落盘的名单
// 不是同一份，而且后端那条"显式拒绝"从提交侧永远不可达（口径只剩一半生效）。
// leader 重复写名单是**错**（笔误 / 两条路各写一半），不是一个需要被默默成全的意图。
//
// 空值口径与权限格一致：空 / 清完为空 → **null**（调用方据此**不写该键**）——
// 空 = 不覆盖（工具面继承宿主当前装配、技能目录不注入），**不是**"装配了零个"。

// submitPluginNames 把表单输入（字符串：逗号 / 顿号 / 空格 / 分号分隔）或协议载荷
// （数组）规整成提交用清单：trim、丢空项、**保序、保重复**；空 → null。
export function submitPluginNames(value) {
  const items = typeof value === "string"
    ? value.split(/[,，、;；\s]+/)
    : (Array.isArray(value) ? value : null);
  if (!items) return null;
  const out = [];
  for (const raw of items) {
    const name = String(raw ?? "").trim();
    // 重复项**不在这里被吃掉**：后端显式拒绝，错误原样回显给用户。
    if (name) out.push(name);
  }
  return out.length ? out : null;
}

// withPluginAssembly 把装配字段并进一份提交载荷：非空才写 `plugins` 键，空则**不写**
// （不覆盖，而不是提交 `[]`）。返回同一份载荷，便于在 return 处就地接线。
export function withPluginAssembly(payload, value) {
  if (!payload || typeof payload !== "object") return payload;
  const names = submitPluginNames(value);
  if (names) payload.plugins = names;
  return payload;
}

// ── 「这个插件从哪来」的展示侧规整（来源读数 → 运行状态面板的一行）────────────────────
//
// 运行期视图里每个插件带三个键（后端 model.PluginInfo，commit 118f259）：
//   source_kind  来源类型：builtin / vendored / local；**未登记时整键缺席**（omitempty）
//   source_url   身份：kind=local 时是 `local:<那个根>`，否则是上游地址
//   source_root  这个插件**实际**从哪个根载入（多根 first-wins：同名只算链上先出现的那个）
//
// 三条口径（本模块是它们的唯一落点，app.js 只摆位）：
//   ① 缺失来源面 ⇒ **只显示名字**，不编来源：键缺席 / 空串都算缺失，绝不默认成 builtin
//      （"不知道谁给的"与"随发行包"是两回事，编个默认值等于伪造出处）；
//   ② local 必须**显式可见**（不能只靠 tooltip 里的路径才看得出是本机自建）；
//   ③ 本模块的输出会被直接拼进 innerHTML，所以转义在**这里**做（escapePluginSourceText）。

export const PLUGIN_SOURCE_LABELS = Object.freeze({
  builtin: "随发行包",
  vendored: "第三方移植",
  local: "本机自建",
});

const LOCAL_SOURCE_PREFIX = "local:";

// escapePluginSourceText 与 components.escapeHtml 同口径（含 &#039;），但**不 import
// components.js**：后者链到 markdown.js / DOM，而本模块要能被 node --test 直接 import
// （app.js 是巨型 DOM 脚本，纯函数只能钉在这种无依赖模块上）。
export function escapePluginSourceText(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

// shortenRootPath 只给**展示用**的短路径：长根路径保留末 maxSegments 段，前面用 `…`
// 占位（分隔符沿用原串——Windows 根不许被显示成 POSIX 根）。完整路径始终留在 note/title
// 里，所以这里丢掉的只是"面板上那一眼"，不是事实本身。
export function shortenRootPath(path, maxSegments = 2) {
  const text = String(path ?? "").trim();
  if (!text) return "";
  const separator = text.includes("\\") ? "\\" : "/";
  const segments = text.split(/[\\/]+/).filter(Boolean);
  if (segments.length <= maxSegments) return text;
  return "…" + separator + segments.slice(-maxSegments).join(separator);
}

// shortenSourceUrl 对 `local:<根>` 只收口那个根，前缀不动——`local:` 本身是"本机自建"的
// 机读标识，短掉就看不出这是本地根了。
export function shortenSourceUrl(url) {
  const text = String(url ?? "").trim();
  if (!text.startsWith(LOCAL_SOURCE_PREFIX)) return text;
  return LOCAL_SOURCE_PREFIX + shortenRootPath(text.slice(LOCAL_SOURCE_PREFIX.length));
}

// pluginSourceBadge 把一行的来源读数翻成 UI 要的四块**纯文本**（调用方负责摆位与转义）：
//   { hasSource, kind, label, note, hint }
//   label  可见短标签（"随发行包 / 第三方移植 / 本机自建"）；
//   note   完整旁注（多行）：来源地址 + 载入根 —— 进 title，路径不截断；
//   hint   有界单行旁注：给可见的第二行用，长路径收口。
// 缺失来源面 → 全空 + hasSource=false（判据①）；认不得的 kind **原样显形**，不塞进三档
// （那是另一套判定，前端不替后端猜）。
export function pluginSourceBadge(plugin) {
  const kind = typeof plugin?.source_kind === "string" ? plugin.source_kind.trim() : "";
  if (!kind) return { hasSource: false, kind: "", label: "", note: "", hint: "" };
  const url = typeof plugin.source_url === "string" ? plugin.source_url.trim() : "";
  const root = typeof plugin.source_root === "string" ? plugin.source_root.trim() : "";
  return {
    hasSource: true,
    kind,
    label: PLUGIN_SOURCE_LABELS[kind] || kind,
    note: [url ? `来源 ${url}` : "", root ? `载入根 ${root}` : ""].filter(Boolean).join("\n"),
    hint: [url ? `来源 ${shortenSourceUrl(url)}` : "", root ? `载入根 ${shortenRootPath(root)}` : ""]
      .filter(Boolean).join(" · "),
  };
}
