// 压缩记录的展示口径（单一事实源）：右栏「上下文压缩」条目与轨迹「压缩」轨/详情
// 共用这几支格式化函数。两处各写一份必然漂移——同一条记录在右栏被读成
// `消息 message-1..message-103 / 事件 1..6`、在轨迹被读成另一种写法，用户就会
// 看到"同一个压缩两种说法"（2026-09-23 已发生过同类漂移：口径句里的数字与事实相反）。
//
// 口径（与后端 model.CompactionRangeLabel 一致）：
//   - 区间只取记录里**已有**的边界字段（message_from/to、event_from/to）；
//   - 绝不用 messages_before 顶替：那是装配前的引擎历史条数（含 system 行、
//     冷加载会话合法为 0），不是被压的消息条数；
//   - 未知原因/来源原样返回，不编造、不吞掉。

// compactionReasonLabel 渲染压缩原因（后端 reason 取值）。
export function compactionReasonLabel(reason) {
  switch (reason) {
  case "context_budget": return "上下文预算达峰";
  case "context_budget_autonomous": return "上下文预算（自主压缩）";
  case "large_tool_output": return "超大工具输出";
  default: return String(reason || "上下文压缩");
  }
}

// compactionOriginLabel 渲染压缩来源（auto/explicit/explicit_after_turn）：
// "谁要的这次压缩"与"为什么压"是两件事，回执与详情都要分开说。
export function compactionOriginLabel(origin) {
  switch (String(origin || "")) {
  case "auto": return "自动阈值";
  case "explicit": return "显式要求（回合中）";
  case "explicit_after_turn": return "显式要求（回合后）";
  default: return "";
  }
}

// compactionRangeText 渲染被压区间，例如 `消息 message-1..message-103（事件 1..6）`。
// 单号不写 `..`；只有一端就只写一端；没有边界返回 ""（调用方跳过这一段）。
export function compactionRangeText(compaction = {}) {
  const from = String(compaction.message_from || "");
  const to = String(compaction.message_to || "");
  const messageLabel = !from ? to : (to === "" || to === from ? from : `${from}..${to}`);
  const eventFrom = Number(compaction.event_from || 0);
  const eventTo = Number(compaction.event_to || 0);
  const parts = [];
  if (messageLabel) parts.push(`消息 ${messageLabel}`);
  if (eventFrom > 0 || eventTo > 0) parts.push(`事件 ${eventFrom}..${eventTo}`);
  if (!parts.length) return "";
  if (parts.length === 2) return `${parts[0]}（${parts[1]}）`;
  return parts[0];
}

// compactionCutLabel 渲染压缩分界虚线的说明：这条线以上（更早）的上下文已被折出
// provider 历史。区间未知时只说"更早的上下文"，不编造范围。
export function compactionCutLabel(compaction = {}) {
  const range = compactionRangeText(compaction);
  return range ? `以上 ${range}已被折叠` : "以上更早的上下文已被折叠";
}

// compactionGateLabels 是压缩门禁进度条的文案表，键序 = 后端
// context_runtime.CompactionGates 的执行顺序（判据估算→装配→替换 provider
// 历史→渲染帧→存帧→写记录）。两处顺序一旦漂移，进度条会把「存帧」画在
// 「写记录」之后——frontend 测试照着这份键序钉后端字面量。
export const compactionGateLabels = {
  judge: "判定是否需要折叠",
  assemble: "装配压缩上下文",
  replace: "替换 provider 历史",
  frame: "渲染 checkpoint 帧",
  store: "帧正文落盘",
  record: "写压缩记录"
};

// compactionGateLabel 渲染单关文案；未知门禁原样返回 id（后端新增门禁而前端
// 未跟随时，进度条要显示"有这么一关"，而不是留空白让人以为卡死）。
export function compactionGateLabel(gate) {
  const key = String(gate || "");
  return compactionGateLabels[key] || key;
}

// compactionGateDurationText 渲染单关耗时。不足 1ms 时写 `<1ms`：后端给的是毫秒
// 整数（截断），0 的含义就是"不到一毫秒"，写成 `0ms` 读起来像"没花时间"，而四舍
// 五入成 1ms 是在替后端编数字。
export function compactionGateDurationText(ms) {
  const value = Number(ms || 0);
  if (!(value > 0)) return "<1ms";
  return `${value}ms`;
}

// mergeCompactionProgress 把一帧 compaction.progress 并入本轮进度面（纯函数，
// 视图层只负责存与重绘）。
//
// 为什么需要"轮次"概念：自动路径没有起手帧（要不要折叠正是判据估算的结果，估完
// 才知道），所以不能只靠 phase=begin 判新轮——否则上一轮留下的清单会被新一轮的
// 格子续写，用户读到的是两轮混在一起的耗时。判据是"上一轮的 running 已结束"。
//
// 逐关耗时（elapsed_ms）由后端逐段实测，这里只做累加：前端不猜、不插值、不把
// 累计值当成单关耗时。
export function mergeCompactionProgress(previous, payload) {
  if (!payload || typeof payload !== "object") return previous || null;
  const total = Number(payload.total || 0);
  if (!(total > 0)) return previous || null;
  const state = payload.state === "failed" ? "failed"
    : (payload.state === "done" ? "done" : "running");
  const phase = String(payload.phase || "");
  const gate = String(payload.gate || "");
  const index = Math.min(Math.max(Number(payload.index || 0), 0), total);
  const startsRound = phase === "begin" || (state === "running" && previous?.state !== "running");
  const base = startsRound ? null : previous;
  const gates = Array.isArray(base?.gates) ? base.gates.map(item => ({ ...item })) : [];
  if (state === "running" && gate && phase !== "begin") {
    const entry = { gate, ms: Number(payload.elapsed_ms || 0), detail: String(payload.detail || "") };
    const at = gates.findIndex(item => item.gate === gate);
    if (at < 0) gates.push(entry);
    else gates[at] = entry;
  }
  // 终局帧后端恒给 index=total（"本轮已收口"），但中途失败的那一轮应停在真正走过
  // 的格子上，不能被终局帧推成满格：沿用最后一条 running 的序号。
  const previousIndex = base?.state === "running" ? Number(base.index || 0) : 0;
  return {
    state,
    phase,
    gate,
    index: state === "running" ? Math.max(index, previousIndex) : (previousIndex || total),
    total,
    gates,
    elapsedMs: Number(base?.elapsedMs || 0) + Number(payload.elapsed_ms || 0),
    version: Number(payload.version || 0) || Number(base?.version || 0),
    origin: String(payload.origin || "") || String(base?.origin || ""),
    detail: String(payload.detail || ""),
    outcome: String(payload.outcome || "")
  };
}

// compactionOutcomeLabel 渲染终局结论。前两个取值是后端 CompactOutcome 字面量；
// 其余是失败轮的原始错误串（后端把 err.Error() 放在同一字段）——错误照原文显示，
// 不能替它编一句"压缩未完成"把真实原因盖掉。
export function compactionOutcomeLabel(outcome) {
  switch (String(outcome || "")) {
  case "compacted": return "已折叠并写入压缩记录";
  case "folded_without_record": return "已折叠，本轮纪元未到期不写记录";
  default: return String(outcome || "");
  }
}

// compactionCutTitle 是分界虚线的悬停说明：把"折叠了什么""原文在哪""谁要的"一次说清。
export function compactionCutTitle(compaction = {}) {
  const origin = compactionOriginLabel(compaction.origin);
  return [
    compactionCutLabel(compaction),
    "这段被折出 provider 历史；对话原文仍完整保留在时间线上，折叠帧正文可按 ref 回读",
    origin ? `来源 ${origin}` : ""
  ].filter(Boolean).join(" · ");
}
