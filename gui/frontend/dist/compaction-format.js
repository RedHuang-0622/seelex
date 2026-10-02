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

// messageOrdinal 从消息 id 取**事件序号**（`message-663` → 663；工具行
// `message-663-1` 属于同一事件 → 663）。取不到（空、其它形状）返回 null。
//
// 分界要按序号与对话行对位，不能按字符串比大小（`message-9` > `message-663`）。
export function messageOrdinal(messageID) {
  const match = /^message-(\d+)(?:-\d+)?$/.exec(String(messageID ?? "").trim());
  return match ? Number(match[1]) : null;
}

// compactionFrontier 返回会话**单例**的压缩分界（前沿）：历次记录里被折出保留
// 窗口的最后一条消息（message_to 序号最大者），区间起点取全部记录里最早的一端。
//
// 为什么是单例：分界想说的是"以上这些已经不发给模型了"，那是会话当前的**一个**
// 事实。历次压缩各画一条线，读者会以为两条线之间那段还给模型（其实早折掉了）。
// 记录本身仍逐条可查（右栏「上下文压缩」列表、轨迹「压缩」轨刻度）。
//
// 返回值沿用记录字段名（message_from/message_to/event_from/event_to），因此可直接
// 喂给 compactionRangeText / compactionCutLabel / compactionCutTitle——分界文案与
// 条目文案因此不可能分叉。index 是前沿记录在入参数组里的下标（轨迹据此给刻度打
// 标记）。没有可比记录（空列表、记录里没有消息号）返回 null：不画线，不用别的量
// 顶替。
export function compactionFrontier(compactions = []) {
  const list = (Array.isArray(compactions) ? compactions : []).filter(record => record && typeof record === "object");
  let frontierIndex = -1;
  let frontierOrdinal = null;
  let startOrdinal = null;
  let messageFrom = "";
  let eventFrom = 0;
  let eventTo = 0;
  list.forEach((compaction, index) => {
    const end = messageOrdinal(compaction.message_to);
    if (end !== null && (frontierOrdinal === null || end > frontierOrdinal)) {
      frontierOrdinal = end;
      frontierIndex = index;
    }
    const start = messageOrdinal(compaction.message_from);
    if (start !== null && (startOrdinal === null || start < startOrdinal)) {
      startOrdinal = start;
      messageFrom = String(compaction.message_from || "");
    }
    const from = Number(compaction.event_from || 0);
    if (from > 0 && (eventFrom === 0 || from < eventFrom)) eventFrom = from;
    const to = Number(compaction.event_to || 0);
    if (to > eventTo) eventTo = to;
  });
  if (frontierIndex < 0) return null;
  const frontier = list[frontierIndex];
  return {
    ...frontier,
    index: frontierIndex,
    count: list.length,
    messageToOrdinal: frontierOrdinal,
    // 分界以上指的是"整段已折出的上下文"，不是最后一次压缩的那一段：起点取最早、
    // 终点取最新。只报最后一段会让更早的压缩看起来还发给模型。
    message_from: messageFrom || String(frontier.message_to || ""),
    event_from: eventFrom,
    event_to: eventTo
  };
}

// compactionStackOrder 把压缩记录按「栈」的顺序排出**下标序列**：栈顶 = 最新一次
// 压缩（被压区间终点序号最大者，与 compactionFrontier 同一把尺），往下依次更早。
//
// 返回下标而不是重排后的记录：右栏的展开态（data-compact-open）与帧正文都按下标记账
// （app.js 用 compactions[index] 取记录），排序若换了数组，点开第 1 行就会读到第 2 行
// 的正文。同一侧记录少（会话级），不做分页。
export function compactionStackOrder(compactions = []) {
  const list = Array.isArray(compactions) ? compactions : [];
  return list
    .map((record, index) => ({
      index,
      end: messageOrdinal(record?.message_to) ?? -1,
      time: Number(Date.parse(String(record?.compacted_at || ""))) || 0,
      version: Number(record?.version || 0)
    }))
    .sort((left, right) => right.end - left.end || right.time - left.time || right.version - left.version)
    .map(entry => entry.index);
}

// conversationCompactionAnchor 求对话区那条「以上已压缩」分界该落在哪一条消息之后
// （纯函数；null = 本页不画线）。渲染层只拿到"落在哪条消息之后 + 已格式化的文案"，
// 不必再懂压缩口径——口径只在 compaction-format.js 一处。
//
// 落点：本页最后一条"消息号 <= 前沿消息号"的消息。前沿消息通常就在本页；往回翻页时
// 整页都可能更早，分界落在本页末尾（读作"这一页以上都被折了"）。整页都在前沿之后
// （前沿在更早的那一页）返回 null：分界不在这一页，这里没有任何已压缩的内容，凭空插
// 一行就是假线。
export function conversationCompactionAnchor(messages = [], compactions = []) {
  const frontier = compactionFrontier(compactions);
  if (!frontier) return null;
  const rows = Array.isArray(messages) ? messages : [];
  const target = Number(frontier.messageToOrdinal);
  let messageID = "";
  for (const message of rows) {
    const ordinal = messageOrdinal(message?.id);
    if (ordinal !== null && ordinal <= target) messageID = String(message.id || "");
  }
  if (!messageID) return null;
  const count = Number(frontier.count || 0);
  return {
    messageID,
    label: compactionCutLabel(frontier),
    title: `${compactionCutTitle(frontier)} · 分界是会话单例：历次压缩记录见右栏「上下文压缩」与轨迹「压缩」轨`,
    note: count > 1 ? `会话共压缩 ${count} 次，这里只标最新一次的终点` : "",
    frameRef: String(frontier.frame_ref || "")
  };
}

// compactionCutLabel 渲染压缩分界虚线的说明：这条线以上（更早）的上下文已被压出
// provider 历史。区间未知时只说"更早的上下文"，不编造范围。
export function compactionCutLabel(compaction = {}) {
  const range = compactionRangeText(compaction);
  return range ? `以上 ${range}已被压缩` : "以上更早的上下文已被压缩";
}

// compactionGateLabels 是压缩门禁进度条的文案表，键序 = 后端
// context_runtime.CompactionGates 的执行顺序（判据估算→装配→替换 provider
// 历史→推帧进压缩栈→渲染帧→存帧→写记录）。两处顺序一旦漂移，进度条会把
// 「存帧」画在「写记录」之后——frontend 测试照着这份键序钉后端字面量。
export const compactionGateLabels = {
  judge: "判定是否需要压缩",
  assemble: "装配压缩上下文",
  replace: "替换 provider 历史",
  index: "推帧进压缩栈",
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
// 为什么需要"轮次"概念：自动路径没有起手帧（要不要压缩正是判据估算的结果，估完
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
  case "compacted": return "已压缩并写入压缩记录";
  case "compacted_without_record": return "已压缩，本轮纪元未到期不写记录";
  // 判据命中了，但这次**压不下去**（拿不到模型读后感 / 压缩换不来余量）：按口径不折
  // 上下文、不推压缩栈顶、上下文版本不推进，上下文原样继续 append。与上一行是两种
  // 终局——前者压了上下文只是没记，后者什么都没动。"压缩"这套说法已废止（它曾同时
  // 指"压了"与"没压成"，见下一条注释）。
  case "compact_failed": return "压缩失败：上下文原样继续";
  default: return String(outcome || "");
  }
}

// compactionFailureText 渲染一条**压缩失败痕**的原因（记录里的 Failed=true 那一条）。
//
// 后端把原因写成「字面量 + 数字事实」（如
// `no_model_summary estimated=281424 budget=163616 window=200000 overhead=9123`）：
// 字面量说明成因（下一步查什么），数字是这次失败的证据。这里只翻字面量那一段，
// 数字原样带出——它是判据本身，改写就成了二次叙述。
//
// 未知字面量原样返回：后端新增失败种类而前端没跟时，条目要显示"有这么一种失败"，
// 而不是吞成一句笼统的"压缩失败"。
export function compactionFailureText(compaction = {}) {
  const note = String(compaction?.note || "").trim();
  if (!note) return "压缩失败（未留下原因）";
  const [literal, facts] = splitLiteral(note);
  const label = FAILURE_LABELS[literal] || literal;
  return facts ? `${label}；${facts}` : label;
}

// FAILURE_LABELS 是压缩失败原因的文案表，键 = 后端协议字面量
// （context_runtime.CompactionFailure*，只此两处定义，两侧由配对口径互钉）。
const FAILURE_LABELS = {
  no_model_summary: "拿不到模型读后感（摘要器未装配或重放调用失败）",
  ineffective_compact: "压缩换不来余量（压缩落点仍够不到判据线）"
};

// splitLiteral 把「字面量 + 空格 + 数字事实」拆开；没有数字事实时第二段为空。
function splitLiteral(note) {
  const at = note.indexOf(" ");
  if (at < 0) return [note, ""];
  return [note.slice(0, at), note.slice(at + 1).trim()];
}

// compactionCutTitle 是分界虚线的悬停说明：把"压缩了什么""原文在哪""谁要的"一次说清。
export function compactionCutTitle(compaction = {}) {
  const origin = compactionOriginLabel(compaction.origin);
  return [
    compactionCutLabel(compaction),
    "这段被折出 provider 历史；对话原文仍完整保留在时间线上，压缩帧正文可按 ref 回读",
    origin ? `来源 ${origin}` : ""
  ].filter(Boolean).join(" · ");
}
