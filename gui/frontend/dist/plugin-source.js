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
