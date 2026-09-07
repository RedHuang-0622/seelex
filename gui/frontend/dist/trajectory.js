// 轨迹（Trajectory）——响应类型分类 + Network 风格轨迹渲染（纯函数，零依赖，可单测）。
//
// 数据源是 Snapshot.conversation（后端权威投影），本模块只做呈现层派生：
// 先按"响应类型"把会话消息分类，再把分类结果投影为类似浏览器 Network
// 面板的轨迹行（时间 / 类型 / 名称 / 状态 / 耗时 / 大小 + IN/OUT 详情）。
// 分类与渲染都是纯函数；DOM 交互（过滤、展开、result_ref 读回）在
// trajectory-view.js，本地 UI 状态不进入 Snapshot。

// 响应类型表：每条轨迹记录归属一个类型。顺序即过滤条展示顺序。
export const TRAJECTORY_KINDS = [
  { kind: "input",  label: "输入" },
  { kind: "llm",    label: "LLM" },
  { kind: "tool",   label: "工具" },
  { kind: "error",  label: "错误" },
  { kind: "notice", label: "通知" }
];

export function trajectoryKindLabel(kind) {
  return (TRAJECTORY_KINDS.find(entry => entry.kind === kind) || {}).label || String(kind || "?");
}

// 类型 → 状态着色 token（复用语义色：done/failed/running/info/idle）。
const KIND_STATUS = {
  input: "info",
  llm: "done",
  tool: "done",
  error: "failed",
  notice: "idle"
};

export function trajectoryKindStatus(kind) {
  return KIND_STATUS[kind] || "idle";
}

const KIND_ICONS = {
  input: '<path d="M4 5h16v12H8l-4 4z"/>',
  llm: '<path d="M4 7h10M18 7h2M4 17h2M10 17h10"/><circle cx="16" cy="7" r="2"/><circle cx="8" cy="17" r="2"/>',
  tool: '<path d="m5 7 4 4-4 4M11 17h8"/>',
  error: '<circle cx="12" cy="12" r="9"/><path d="M12 7v6M12 17h.01"/>',
  notice: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .34 1.88l.06.06-2.12 2.12-.06-.06a1.7 1.7 0 0 0-1.88-.34 1.7 1.7 0 0 0-1.04 1.56V20.3h-3v-.08A1.7 1.7 0 0 0 10.66 18.66a1.7 1.7 0 0 0-1.88.34l-.06.06-2.12-2.12.06-.06A1.7 1.7 0 0 0 7 15a1.7 1.7 0 0 0-1.56-1.04h-.08v-3h.08A1.7 1.7 0 0 0 7 9.92a1.7 1.7 0 0 0-.34-1.88L6.6 7.98l2.12-2.12.06.06a1.7 1.7 0 0 0 1.88.34A1.7 1.7 0 0 0 11.7 4.7v-.08h3v.08a1.7 1.7 0 0 0 1.04 1.56 1.7 1.7 0 0 0 1.88-.34l.06-.06 2.12 2.12-.06.06a1.7 1.7 0 0 0-.34 1.88 1.7 1.7 0 0 0 1.56 1.04h.08v3h-.08A1.7 1.7 0 0 0 19.4 15Z"/>'
};

export function trajectoryKindIcon(kind, size = 13) {
  const paths = KIND_ICONS[kind] || KIND_ICONS.notice;
  return `<svg class="icon" width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths}</svg>`;
}

// buildTrajectory 把 conversation 消息投影为轨迹记录数组（按消息顺序 =
// 时间顺序）。配对规则与 components.buildConversationItems 保持一致：
// role="tool" 消息记录请求（IN），后续 role="tool_result" 按 tool.id 命中
// 同一记录（name 回退），把响应（OUT）/状态/耗时/引用合并进去。
//
// 响应类型分类（先分类，再轨迹）：
//   role=user       → input  用户输入（请求发起）
//   role=assistant  → llm    模型响应（空占位跳过）
//   role=tool       → tool   工具调用（请求）
//   role=tool_result→ tool   工具响应（合并到配对记录）
//   role=error      → error  错误响应
//   role=system     → notice 系统通知
//   其它/未知       → notice 兜底
export function buildTrajectory(messages = []) {
  const records = [];
  // 配对键 = 框架 tool-call id：tool.started 与 tool_result 携带同一个 id（恢复
  // 历史同样取 toolCall.ID）。按名字回退会把同名并发工具错配成一行，属于前端
  // 自造的业务判断，不再实现。
  const pendingByID = new Map();

  const push = record => {
    records.push(record);
    if (record.kind === "tool" && record.toolID) pendingByID.set(record.toolID, record);
    return record;
  };

  for (const [index, message] of messages.entries()) {
    const role = message.role || "assistant";
    const createdAt = message.created_at || "";
    if (!message.tool) {
      // 显式类别优先：历史记录条目携带 kind 时不再依赖 role 启发式。
      if (message.kind) {
        if (message.kind === "user_input") {
          push({ kind: "input", key: `message:${message.id || index}`, name: "输入", output: message.content || "", status: "info", startedAt: createdAt, duration: 0 });
        } else if (message.kind === "llm") {
          if (!message.content) continue;
          push({ kind: "llm", key: `message:${message.id || index}`, name: "LLM", output: message.content, reasoning: message.reasoning_content || "", status: "success", startedAt: createdAt, duration: 0 });
        } else if (message.kind === "error") {
          push({ kind: "error", key: `message:${message.id || index}`, name: "错误", output: message.content || "", status: "error", startedAt: createdAt, duration: 0 });
        } else {
          push({ kind: "notice", key: `message:${message.id || index}`, name: "通知", output: message.content || "", status: "info", startedAt: createdAt, duration: 0 });
        }
        continue;
      }
      if (role === "user") {
        push({ kind: "input", key: `message:${message.id || index}`, name: "输入", output: message.content || "", status: "info", startedAt: createdAt, duration: 0 });
      } else if (role === "assistant") {
        // 空 assistant 是工具回合后的占位消息，对轨迹无信息量，跳过。
        if (!message.content) continue;
        push({ kind: "llm", key: `message:${message.id || index}`, name: "LLM", output: message.content, reasoning: message.reasoning_content || "", status: "success", startedAt: createdAt, duration: 0 });
      } else if (role === "error") {
        push({ kind: "error", key: `message:${message.id || index}`, name: "错误", output: message.content || "", status: "error", startedAt: createdAt, duration: 0 });
      } else {
        push({ kind: "notice", key: `message:${message.id || index}`, name: "通知", output: message.content || "", status: "info", startedAt: createdAt, duration: 0 });
      }
      continue;
    }

    const tool = message.tool;
    const isOutput = role === "tool_result";
    if (isOutput) {
      const target = tool.id ? pendingByID.get(tool.id) : null;
      if (target) {
        target.output = tool.error || tool.result || message.content || "";
        target.status = tool.error ? "error" : (tool.status === "error" || tool.status === "failed" ? "error" : "success");
        target.duration = tool.duration || target.duration;
        target.resultRef = tool.result_ref || "";
        target.truncated = Boolean(tool.truncated);
        target.totalChars = Number(tool.total_chars) || 0;
        continue;
      }
    }

    push({
      kind: "tool",
      key: `tool:${tool.id || message.id || index}`,
      toolID: tool.id || "",
      toolName: tool.name || "tool",
      name: tool.name || "tool",
      input: tool.arguments || "",
      output: tool.error || tool.result || (isOutput ? message.content || "" : ""),
      status: tool.error ? "error"
        : (tool.status === "running" || tool.status === "pending" ? "running" : "success"),
      duration: tool.duration || 0,
      startedAt: createdAt,
      resultRef: tool.result_ref || "",
      truncated: Boolean(tool.truncated),
      totalChars: Number(tool.total_chars) || 0
    });
  }
  return records;
}

// filterTrajectory 按类型过滤（"all" = 不过滤）。Network 面板的过滤语义。
export function filterTrajectory(records, kind) {
  if (!kind || kind === "all") return records;
  return records.filter(record => record.kind === kind);
}

// trajectoryStats 统计摘要（Network 面板的请求状态汇总）。
export function trajectoryStats(records = []) {
  const stats = { total: records.length, success: 0, error: 0, running: 0, info: 0, byKind: {} };
  for (const record of records) {
    stats.byKind[record.kind] = (stats.byKind[record.kind] || 0) + 1;
    if (record.status === "error") stats.error += 1;
    else if (record.status === "running" || record.status === "pending") stats.running += 1;
    else if (record.status === "success") stats.success += 1;
    else stats.info += 1;
  }
  return stats;
}

// 过滤条（全部/输入/LLM/工具/错误/通知 + 计数徽标）。
export function renderTrajectoryFilters(records, active) {
  const counts = trajectoryStats(records).byKind;
  const buttons = [
    { kind: "all", label: "全部", count: records.length }
  ].concat(TRAJECTORY_KINDS.map(entry => ({ kind: entry.kind, label: entry.label, count: counts[entry.kind] || 0 })));
  return `<div class="trajectory-filters" role="tablist" aria-label="按响应类型过滤轨迹">
    ${buttons.map(button => `
      <button type="button" class="trajectory-filter-btn${button.kind === active ? " is-active" : ""}" data-trajectory-filter="${button.kind}" role="tab" aria-selected="${button.kind === active ? "true" : "false"}">
        <span class="trajectory-filter-label">${escapeHtml(button.label)}</span><span class="trajectory-filter-count">${button.count}</span>
      </button>`).join("")}
  </div>`;
}

// 摘要条（Network 面板顶部的统计行）。
export function renderTrajectorySummary(stats) {
  const statuses = [
    stats.success ? `<span class="is-success">成功 ${stats.success}</span>` : "",
    stats.error ? `<span class="is-failed">失败 ${stats.error}</span>` : "",
    stats.running ? `<span class="is-running">运行 ${stats.running}</span>` : ""
  ].filter(Boolean).join(" · ");
  return `<div class="trajectory-summary">共 ${stats.total} 条${statuses ? ` · ${statuses}` : ""}</div>`;
}

// ── 上下文轴元数据（US-2：压缩标记 + 前缀注入并入轴，替代独立面板）────────
//
// 数据源：prefixLayers 来自 Bridge.PromptLayers（会话级当前栈，不进 Snapshot）；
// compactions 来自 Snapshot.Task.ContextCompactions（后端每次压缩发布的公开
// 元数据，snapshot.changed 全量刷新带回）。两层都只消费公开字段，轴不携带
// checkpoint 正文 / 工具结果 / 原始对话。

// promptKindLabel 层种类中文标签（identity/base/effort/instructions/skill）。
export function promptKindLabel(kind) {
  switch (kind) {
  case "identity": return "身份";
  case "base": return "基础/系统提示";
  case "effort": return "力度";
  case "instructions": return "指令";
  case "skill": return "技能";
  default: return kind || "层";
  }
}

function promptKindOrder(kind) {
  switch (kind) {
  case "identity": return 0;
  case "base": return 1;
  case "effort": return 2;
  case "instructions": return 3;
  case "skill": return 4;
  default: return 5;
  }
}

// prefixLayerSegments 把当前会话的 prompt 前缀层投影为轴「前缀注入」轨的段：
// 按装配顺序排列，段宽=层文本占比、横跨整条轴。语义：system 前缀在每一轮
// 请求都先于对话注入且字节稳定，因此它在对话坐标上是一整条常量带——轴只
// 表达它的组成占比，不假装它有逐消息坐标。可 trace 粒度 = 会话级当前层
// （PromptStack 不持久化每请求的层增量历史，这是当前实现的边界）。
export function prefixLayerSegments(layers = []) {
  const list = (Array.isArray(layers) ? layers : []).filter(layer => layer && typeof layer === "object");
  const ordered = [...list].sort((a, b) => promptKindOrder(a.kind) - promptKindOrder(b.kind));
  const weights = ordered.map(layer => Math.max(String(layer.text || "").length, 1));
  const total = weights.reduce((sum, weight) => sum + weight, 0) || 1;
  let cursor = 0;
  return ordered.map((layer, index) => {
    const x = (cursor / total) * 100;
    const width = (weights[index] / total) * 100;
    cursor += weights[index];
    const kind = String(layer.kind || "layer");
    return {
      key: `prefix:${kind}:${index}`,
      kind,
      name: layer.name ? String(layer.name) : "",
      label: layer.name ? String(layer.name) : promptKindLabel(kind),
      text: layer.text != null ? String(layer.text) : "",
      x,
      width,
      chars: String(layer.text || "").length
    };
  });
}

function compactionReasonLabel(reason) {
  switch (reason) {
  case "context_budget": return "上下文预算达峰";
  case "large_tool_output": return "超大工具输出";
  default: return "上下文压缩";
  }
}

function formatNumber(value) {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(Number(value) || 0);
}

function recordTime(record) {
  if (!record || !record.startedAt) return NaN;
  const time = new Date(record.startedAt);
  return Number.isNaN(time.getTime()) ? NaN : time.getTime();
}

// compactionAnchorIndex 定位"压缩发生时会话已推进到的最后一条记录"：用最后
// 一条 startedAt <= compacted_at 的记录锚定（压缩发生在该记录之后）。找不到
// （压缩点早于已加载窗口）返回 -1，调用方把刻度钳到轴起点并注明。
function compactionAnchorIndex(records, compactedAt) {
  const target = compactedAt ? new Date(compactedAt).getTime() : NaN;
  if (Number.isNaN(target)) return records.length - 1;
  let anchor = -1;
  for (let index = 0; index < records.length; index++) {
    const time = recordTime(records[index]);
    if (Number.isNaN(time)) continue;
    if (time <= target) anchor = index;
  }
  return anchor;
}

// compactionMarks 把 Snapshot.Task.ContextCompactions 投影为轴「压缩」轨刻度：
// x = 压缩发生时会话推进到的体量位置（锚定记录结束处）。只含公开元数据
// （version/reason/messages_before/estimated_tokens/compacted_at）。
export function compactionMarks(records = [], compactions = []) {
  const list = (Array.isArray(compactions) ? compactions : []).filter(c => c && typeof c === "object");
  const weights = records.map(contextAxisWeight);
  const total = weights.reduce((sum, weight) => sum + weight, 0) || 1;
  let cursor = 0;
  const starts = [];
  for (const weight of weights) {
    starts.push((cursor / total) * 100);
    cursor += weight;
  }
  return list.map((compaction, index) => {
    const anchor = compactionAnchorIndex(records, compaction.compacted_at);
    const segmentStart = anchor >= 0 && anchor < starts.length ? starts[anchor] : null;
    const end = segmentStart !== null
      ? segmentStart + (weights[anchor] / total) * 100
      : 0;
    return {
      key: `compact:${index}`,
      version: Number(compaction.version) || index + 1,
      reason: String(compaction.reason || ""),
      reasonLabel: compactionReasonLabel(compaction.reason),
      messagesBefore: Number(compaction.messages_before) || 0,
      tokens: Number(compaction.estimated_tokens) || 0,
      timeLabel: compaction.compacted_at ? formatTime(compaction.compacted_at) : "—",
      x: Math.min(Math.max(end, 0), 100),
      anchored: anchor >= 0
    };
  });
}

// renderAxisDetail 渲染被点中的元数据块详情（全部 escape，无未受控注入）。
// selection = { type: "prefix", layer } 或 { type: "compression", mark }。
export function renderAxisDetail(selection = {}) {
  if (!selection || typeof selection !== "object") return "";
  if (selection.type === "prefix" && selection.layer) return renderPrefixDetail(selection.layer);
  if (selection.type === "compression" && selection.mark) return renderCompressionDetail(selection.mark);
  return "";
}

function renderPrefixDetail(layer) {
  const kind = String(layer.kind || "layer");
  const name = layer.name ? escapeHtml(String(layer.name)) : "";
  return `<section class="axis-detail is-prefix" data-axis-detail>
    <header>
      <strong>前缀注入层 · ${escapeHtml(promptKindLabel(kind))}</strong>
      <button class="axis-detail-close" type="button" data-axis-detail-close title="收起详情">收起</button>
    </header>
    <div class="axis-detail-meta">
      <span>${escapeHtml(promptKindLabel(kind))}</span>
      ${name ? `<span>${name}</span>` : ""}
      <span>${layer.chars} chars</span>
      <span>每请求前置（system 前缀）</span>
    </div>
    <pre class="axis-detail-text">${escapeHtml(layer.text) || '<span class="muted">（空层）</span>'}</pre>
  </section>`;
}

function renderCompressionDetail(mark) {
  const meta = [
    mark.timeLabel && mark.timeLabel !== "—" ? `时间 ${mark.timeLabel}` : "",
    mark.messagesBefore > 0 ? `${formatNumber(mark.messagesBefore)} 条消息` : "",
    mark.tokens > 0 ? `约 ${formatNumber(mark.tokens)} tokens` : ""
  ].filter(Boolean).join(" · ");
  const where = mark.anchored ? "刻度=压缩发生时会话推进到的位置" : "刻度钳在轴起点：压缩点早于当前已加载的对话窗口";
  return `<section class="axis-detail is-compression" data-axis-detail>
    <header>
      <strong>上下文压缩 #${escapeHtml(String(mark.version))}</strong>
      <button class="axis-detail-close" type="button" data-axis-detail-close title="收起详情">收起</button>
    </header>
    <div class="axis-detail-meta">
      <span>${escapeHtml(mark.reasonLabel)}</span>
      ${meta ? `<span>${escapeHtml(meta)}</span>` : ""}
      <span>${escapeHtml(where)}</span>
    </div>
    <p>该时刻窗口外的旧轮次被折叠为栈顶压缩摘要，随后装配保留一个有界的新鲜
       窗口继续执行。对话原文仍完整保留在时间线上（呈现层不丢消息）；折叠原文
       可经 read_compressed_turn(segment_id) / search_history 读回。</p>
  </section>`;
}

function normalizeAxisExtras(extras) {
  const options = extras && typeof extras === "object" ? extras : {};
  return {
    prefixLayers: Array.isArray(options.prefixLayers) ? options.prefixLayers : [],
    compactions: Array.isArray(options.compactions) ? options.compactions : []
  };
}

// renderPrefixLane 渲染顶部「前缀注入」轨：每次请求前置的 system 前缀层，
// 横跨整轴（段宽=层文本占比，与对话坐标无关）；点击段开轴下方详情。
function renderPrefixLane(segments) {
  const bar = segments.map((segment, index) => {
    const title = `${segment.label} · ${segment.chars} chars · ${promptKindLabel(segment.kind)} · 点击查看注入文本`;
    return `<button type="button" class="axis-segment is-prefix is-${escapeHtml(segment.kind || "layer")}" style="--x:${segment.x.toFixed(3)}%;--w:${segment.width.toFixed(3)}%" data-prefix-layer="${index}" title="${escapeHtml(title)}" aria-label="${escapeHtml(segment.label)}"><span>${escapeHtml(segment.label)}</span></button>`;
  }).join("");
  return `<div class="context-axis-lane is-prefix" aria-label="前缀注入（每次请求前置的 system 前缀层；段宽=层文本占比，横跨整轴）">
    <span class="axis-lane-label" title="每次请求都会在对话之前注入的 system 前缀层（identity/base/effort/instructions/skill）；横轴=各层文本占比，不随对话推进变化"><span>前缀注入</span><span class="axis-lane-count">${segments.length}</span></span>
    <div class="axis-lane-bar is-meta">${bar}</div>
  </div>`;
}

// renderCompressionLane 渲染底部「压缩」轨：在会话推进位置标记历次压缩
// （刻度点击开详情）；压缩点早于已加载窗口时钳到轴起点。
function renderCompressionLane(marks) {
  const bar = marks.map((mark, index) => {
    const where = mark.anchored ? `会话推进至 ${mark.x.toFixed(0)}% 处` : "压缩点早于已加载窗口（刻度置于起点）";
    const title = `压缩 #${mark.version} · ${mark.reasonLabel}${mark.messagesBefore > 0 ? ` · ${formatNumber(mark.messagesBefore)} 条` : ""}${mark.tokens > 0 ? ` · 约 ${formatNumber(mark.tokens)} tokens` : ""} · ${mark.timeLabel} · ${where} · 点击查看详情`;
    return `<button type="button" class="axis-segment is-compress" style="--x:${mark.x.toFixed(3)}%" data-compact-idx="${index}" title="${escapeHtml(title)}" aria-label="${escapeHtml(`压缩 #${mark.version}`)}"><span>#${escapeHtml(String(mark.version))}</span></button>`;
  }).join("");
  return `<div class="context-axis-lane is-compress" aria-label="上下文压缩刻度（模型侧把旧段折叠为摘要的位置，对话原文仍保留）">
    <span class="axis-lane-label" title="软阈值达峰时把窗口外旧轮次折叠为栈顶摘要；刻度标记压缩发生时会话推进位置"><span>压缩</span><span class="axis-lane-count">×${marks.length}</span></span>
    <div class="axis-lane-bar is-meta">${bar}</div>
  </div>`;
}

// contextAxisWeight 返回上下文轴上该记录占据的相对体量：工具记录按输出/输入
// 字符数，其余按内容与推理字符数；最小为 1，保证记录都有可见落点。
export function contextAxisWeight(record) {
  if (record.kind === "tool") {
    return Math.max(Number(record.totalChars) || String(record.output || "").length || String(record.input || "").length, 1);
  }
  return Math.max(String(record.output || "").length + String(record.reasoning || "").length, 1);
}

// renderContextAxis 渲染轨迹视图顶部的上下文轴——多线谱（分轨）布局，
// 类似 DevTools Network 面板的时间轴：五种响应类型各占一条横轨，共用同一
// 横轴（对话顺序 + 内容体量，非时间轴）。每个轨迹记录是一个可点击块，按
// 它在全局序列中的位置落在本类型轨道上；某类型在某段对话里没有记录时该轨
// 留空。extras 附加两条元数据轨：
//  - prefixLayers（Bridge.PromptLayers）：顶部「前缀注入」轨——每次请求前置
//    的 system 前缀层，横跨整轴（段宽=层文本占比，与对话坐标无关）；
//  - compactions（Snapshot.Task.ContextCompactions）：底部「压缩」轨——在
//    会话推进位置标记历次上下文压缩。
// 普通块点击由视图定位到对应轨迹行；元数据块点击开轴下方详情（view 侧）。
export function renderContextAxis(records = [], extras) {
  const options = normalizeAxisExtras(extras);
  const prefixSegments = prefixLayerSegments(options.prefixLayers);
  const marks = compactionMarks(records, options.compactions);
  if (!records.length && !prefixSegments.length && !marks.length) {
    return '<div class="context-axis-empty">暂无上下文轴数据</div>';
  }
  const weights = records.map(contextAxisWeight);
  const total = weights.reduce((sum, weight) => sum + weight, 0) || 1;
  // 共享横轴上的游标：块的起点 = 此前所有记录的体量占比累计，块宽 = 自身体量占比。
  let cursor = 0;
  const blocksByKind = new Map(TRAJECTORY_KINDS.map(entry => [entry.kind, []]));
  for (const [index, record] of records.entries()) {
    const x = (cursor / total) * 100;
    const width = (weights[index] / total) * 100;
    cursor += weights[index];
    const label = record.kind === "tool" ? (record.toolName || record.name) : trajectoryKindLabel(record.kind);
    const size = trajectorySize(record);
    const statusClass = statusClassName(record.status);
    const block = `<button type="button" class="axis-segment is-${escapeHtml(record.kind)} ${statusClass}" style="--x:${x.toFixed(3)}%;--w:${width.toFixed(3)}%" data-trajectory-key="${escapeHtml(record.key)}" title="${escapeHtml(`${label} · ${size} · 点击定位轨迹行`)}" aria-label="${escapeHtml(label)}"><span>${escapeHtml(label)}</span></button>`;
    blocksByKind.get(record.kind)?.push(block);
  }
  const lanes = TRAJECTORY_KINDS.map(entry => {
    const blocks = blocksByKind.get(entry.kind) || [];
    const empty = blocks.length === 0;
    const laneHint = empty ? "（无上下文块）" : "";
    return `<div class="context-axis-lane is-${entry.kind}${empty ? " is-empty" : ""}" aria-label="${escapeHtml(entry.label)}${laneHint}">
      <span class="axis-lane-label" title="${escapeHtml(entry.label + (empty ? "：本类型无上下文块" : ""))}">${trajectoryKindIcon(entry.kind, 10)}<span>${escapeHtml(entry.label)}</span></span>
      <div class="axis-lane-bar">${blocks.join("")}</div>
    </div>`;
  }).join("");
  const hints = ["横轴=对话顺序+体量"];
  if (prefixSegments.length) hints.push("前缀注入=层文本占比");
  if (marks.length) hints.push(`压缩 ×${marks.length}（刻度可点击）`);
  hints.push("点击块定位轨迹行");
  return `<div class="context-axis" role="group" aria-label="上下文轴：按响应类型分轨，横轴为对话顺序与内容体量（非时间轴）">
    <div class="context-axis-head"><strong>上下文轴</strong><span>${escapeHtml(hints.join(" · "))}</span></div>
    <div class="context-axis-track">${prefixSegments.length ? renderPrefixLane(prefixSegments) : ""}${lanes}${marks.length ? renderCompressionLane(marks) : ""}</div>
  </div>`;
}

// 表头行（Network 面板的列定义）。
const TRAJECTORY_COLUMNS = ["时间", "类型", "名称", "状态", "耗时", "大小"];

// renderTrajectoryTable 渲染轨迹表格（Network 风格）：表头 + 记录行。
// 返回 { html, items, payloads }：
//  - html     整表 HTML（不含过滤条/摘要）
//  - items    [{ key, html }]（keyed reconcile 用）
//  - payloads 完整 IN/OUT payload Map（复制/展开用）
export function renderTrajectoryTable(records) {
  const payloads = new Map();
  if (!records.length) {
    const html = `<div class="trajectory-empty">${escapeHtml(emptyTrajectoryText())}</div>`;
    return { html, items: [], payloads };
  }
  const head = `<div class="trajectory-row is-head" role="row">
    ${TRAJECTORY_COLUMNS.map(label => `<span>${escapeHtml(label)}</span>`).join("")}
  </div>`;
  const items = records.map(record => {
    const key = record.key;
    const html = renderTrajectoryRow(record, key, payloads);
    return { key, html };
  });
  return { html: head + items.map(item => item.html).join(""), items, payloads };
}

// renderTrajectoryRow 渲染一条轨迹记录行：主行（时间/类型/名称/状态/耗时/
// 大小）+ 可展开详情（IN/OUT 面板）。记录数据全部 escape 或进入 payload
// Map，不做未受控 HTML 注入。
export function renderTrajectoryRow(record, key, payloads) {
  const statusClass = statusClassName(record.status);
  const statusLabel = statusLabelOf(record.status);
  const duration = record.duration ? formatDuration(record.duration) : (record.status === "running" ? "运行中" : "");
  const size = trajectorySize(record);
  const input = prettyValue(record.input);
  const output = prettyValue(record.output);
  const inputKey = `${key}-in`;
  const outputKey = `${key}-out`;
  payloads.set(inputKey, input);
  payloads.set(outputKey, output);
  if (record.reasoning) {
    payloads.set(`${outputKey}-think`, String(record.reasoning));
  }

  const detail = record.kind === "tool"
    ? renderTrajectoryDetail({ input, output, inputKey, outputKey, record, statusClass })
    : renderTrajectoryTextDetail({ output, outputKey, record });

  return `<details class="trajectory-row ${statusClass}" data-trajectory-key="${escapeHtml(key)}" data-trajectory-kind="${escapeHtml(record.kind)}">
    <summary class="trajectory-row-main" role="row">
      <time datetime="${escapeHtml(record.startedAt || "")}">${escapeHtml(formatTime(record.startedAt))}</time>
      <span class="trajectory-kind is-${escapeHtml(record.kind)}">${trajectoryKindIcon(record.kind)}${escapeHtml(trajectoryKindLabel(record.kind))}</span>
      <strong class="trajectory-name" title="${escapeHtml(record.name)}">${escapeHtml(record.name)}</strong>
      <span class="trajectory-status ${statusClass}">${escapeHtml(statusLabel)}</span>
      <span class="trajectory-duration">${escapeHtml(duration)}</span>
      <span class="trajectory-size">${escapeHtml(size)}</span>
    </summary>
    <div class="trajectory-detail">${detail}</div>
  </details>`;
}

// renderTrajectoryDetail 工具记录详情：IN/OUT 双栏（复用 io-panel 类名，
// 与对话工具卡片同一套复制/展开/result_ref 交互契约）。
function renderTrajectoryDetail({ input, output, inputKey, outputKey, record, statusClass }) {
  const inputView = limitText(input, 1400, 28);
  const outputView = limitText(output, 4000, 40);
  const ref = record.resultRef || "";
  const note = record.truncated && ref
    ? `<span class="io-note">预览 ${outputView.total} 字符 · 全文 ${record.totalChars} 字符（默认折叠，点击加载）</span>`
    : outputView.truncated
      ? `<span class="io-note">预览 ${outputView.total} 字符</span>`
      : "";
  const fullButton = ref && record.truncated
    ? `<button class="io-expand" type="button" data-load-ref="${escapeHtml(ref)}" title="加载完整输出（按需拉取，默认折叠）">${svgExpand()} <span>加载完整输出</span></button>`
    : outputView.truncated
      ? `<button class="io-expand" type="button" data-expand="${outputKey}" title="展开完整内容">${svgExpand()} <span>+${outputView.hidden} chars</span></button>`
      : "";
  const outBody = outputView.truncated
    ? `<details class="io-collapse"><summary><span>查看输出</span><span class="io-collapse-meta">${escapeHtml(String(outputView.total))} chars</span></summary><pre>${escapeHtml(outputView.preview)}</pre></details>`
    : `<pre>${escapeHtml(outputView.preview)}</pre>`;
  return `<div class="trajectory-io-grid">
    <section class="io-panel" data-payload="${inputKey}">
      <header><span class="io-label">IN</span><span class="io-meta">${inputView.total} chars</span><button class="icon-button subtle" type="button" data-copy="${inputKey}" title="复制输入" aria-label="复制输入">${svgCopy()}</button></header>
      <pre>${escapeHtml(inputView.preview)}</pre>
    </section>
    <section class="io-panel ${statusClass === "is-error" ? "io-error" : ""}" data-payload="${outputKey}">
      <header><span class="io-label">OUT</span><span class="io-meta">${outputView.total} chars</span><button class="icon-button subtle" type="button" data-copy="${outputKey}" title="复制输出" aria-label="复制输出">${svgCopy()}</button></header>
      ${outBody}
      ${note}
      ${fullButton}
    </section>
  </div>`;
}

// renderTrajectoryTextDetail 非工具记录（input/llm/error/notice）详情：
// 单栏全文（同样走 payload Map + 折叠预览）。
function renderTrajectoryTextDetail({ output, outputKey, record }) {
  const view = limitText(output, 4000, 40);
  const note = view.truncated ? `<span class="io-note">预览 ${view.total} 字符</span>` : "";
  const fullButton = view.truncated
    ? `<button class="io-expand" type="button" data-expand="${outputKey}" title="展开完整内容">${svgExpand()} <span>+${view.hidden} chars</span></button>`
    : "";
  const think = String(record.reasoning || "").trim();
  const thinkKey = `${outputKey}-think`;
  const thinkView = think ? limitText(think, 4000, 40) : null;
  const thinkPanel = thinkView
    ? `<section class="io-panel trajectory-think" data-payload="${thinkKey}">
        <header><span class="io-label">THINK</span><span class="io-meta">${thinkView.total} chars</span><button class="icon-button subtle" type="button" data-copy="${thinkKey}" title="复制思考内容" aria-label="复制思考内容">${svgCopy()}</button></header>
        <pre>${escapeHtml(thinkView.preview)}</pre>
        ${thinkView.truncated ? `<button class="io-expand" type="button" data-expand="${thinkKey}" title="展开完整内容">${svgExpand()} <span>+${thinkView.hidden} chars</span></button>` : ""}
      </section>`
    : "";
  const body = view.truncated
    ? `<details class="io-collapse"><summary><span>查看内容</span><span class="io-collapse-meta">${escapeHtml(String(view.total))} chars</span></summary><pre>${escapeHtml(view.preview)}</pre></details>`
    : `<pre>${escapeHtml(view.preview)}</pre>`;
  return `<div class="trajectory-text-panel">
    ${thinkPanel}
    <section class="io-panel ${record.status === "error" ? "io-error" : ""}" data-payload="${outputKey}">
      <header><span class="io-label">BODY</span><span class="io-meta">${view.total} chars</span><button class="icon-button subtle" type="button" data-copy="${outputKey}" title="复制内容" aria-label="复制内容">${svgCopy()}</button></header>
      ${body}
      ${note}
      ${fullButton}
    </section>
  </div>`;
}

// 轨迹大小列：截断记录用归档总字符，其余用可见输出长度（KB 展示）。
function trajectorySize(record) {
  const chars = record.totalChars || String(record.output || "").length;
  if (chars <= 0) return "—";
  if (chars < 1024) return `${chars} B`;
  return `${(chars / 1024).toFixed(chars < 10240 ? 1 : 0)} KB`;
}

function emptyTrajectoryText() {
  return "暂无轨迹记录。轨迹来自当前会话可见窗口（对话区消息），工具调用、LLM 回复、错误与系统通知会按响应类型列在这里。";
}

// ── 本地工具（与 components.js 保持同一呈现契约；纯函数，无模块依赖）──

export function escapeHtml(value = "") {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

function prettyValue(value) {
  const text = String(value || "").trim();
  if (!text) return "";
  try {
    return JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    return text;
  }
}

function limitText(value, maxChars, maxLines) {
  const text = String(value || "");
  const lines = text.split("\n");
  let preview = lines.slice(0, maxLines).join("\n");
  if (preview.length > maxChars) preview = preview.slice(0, maxChars);
  const truncated = preview.length < text.length;
  return { preview, truncated, hidden: Math.max(text.length - preview.length, 0), total: text.length };
}

function formatDuration(duration) {
  const milliseconds = Number(duration) / 1e6;
  if (!Number.isFinite(milliseconds) || milliseconds <= 0) return "";
  return milliseconds >= 1000 ? `${(milliseconds / 1000).toFixed(1)}s` : `${Math.round(milliseconds)}ms`;
}

function formatTime(iso) {
  if (!iso) return "—";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "—";
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false });
}

function statusClassName(status) {
  if (status === "error" || status === "failed") return "is-error";
  if (status === "running" || status === "pending") return "is-running";
  if (status === "success") return "is-success";
  return "is-info";
}

function statusLabelOf(status) {
  if (status === "error" || status === "failed") return "ERR";
  if (status === "running" || status === "pending") return "RUN";
  if (status === "success") return "OK";
  return "—";
}

function svgCopy() {
  return '<svg class="icon" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="8" y="8" width="11" height="11" rx="2"/><path d="M16 8V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h3"/></svg>';
}

function svgExpand() {
  return '<svg class="icon" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m8 3-5 5M3 3v5h5M16 3l5 5M21 3v5h-5M8 21l-5-5M3 21v-5h5M16 21l5-5M21 21v-5h-5"/></svg>';
}
