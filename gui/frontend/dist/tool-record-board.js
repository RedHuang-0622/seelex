import { escapeHtml } from "./components.js";

// tool-record-board.js — **工具记录**看板的纯渲染件（柱形图 + 原因饼图 + 开关）。
//
// 生态位：数据源 = 后端 `Bridge.ToolRecordBoard()`（当前视图会话的会话级读数，落
// `<数据根>/tool-record.jsonl`）。app.js 只做搬运（拉数 + innerHTML + 事件委托 +
// 记住选了哪根柱子），排布、配色、比例、文案全在这里——渲染件可单测，面板不重算事实。
//
// 图形口径（用户指定，2026-10-15）：
//   - **只有一张柱形图**：一个工具一根柱子，**上半天 = 成功、下半天 = 失败**，
//     高度按该工具的**总调用次数**缩放（所以"用得多的工具"柱子最高）；
//   - 点一根柱子 → 它下面出现**原因饼图**：这个工具各**失败原因**的占比
//     （饼图回答的是"为什么坏"，不是"坏得多不多"）；
//   - 计数开关在表头：关掉之后后端不再记，看板整块退场。
//
// 颜色：成功/失败两格固定（绿/红），原因按词表序取色（同一原因每次同色）。

const SUCCESS_COLOR = "#10b981";
const FAILURE_COLOR = "#ef4444";
// 原因配色按**词表序**（与契约 dto.ToolReason* 的顺序一致）：同一原因每次同色，
// 图例顺序也稳定。顺序变了颜色跟着变，但那是"词表改了"的可视信号。
const REASON_COLORS = {
  permission: "#f59e0b",
  timeout: "#3b82f6",
  refused: "#8b5cf6",
  "not-found": "#0ea5e9",
  "invalid-args": "#ec4899",
  conflict: "#f97316",
  transport: "#14b8a6",
  "command-failed": "#64748b",
  other: "#94a3b8",
};
const REASON_LABELS = {
  permission: "权限",
  timeout: "超时",
  refused: "业务拒绝",
  "not-found": "目标不存在",
  "invalid-args": "入参不合法",
  conflict: "状态冲突",
  transport: "通道故障",
  "command-failed": "命令失败",
  other: "其它",
};

const BAR_WIDTH = 34; // 每根柱子占的横向格子（含间距）
const BAR_GAP = 8;
const CHART_HEIGHT = 132; // 基线到顶的高度（成功 + 失败两段共用）
const AXIS_HEIGHT = 26; // 柱子下方的工具名行
const MIN_BAR_WIDTH = 10;

export const TOOL_RECORD_CSS = `
.tool-record-view { display: flex; flex-direction: column; gap: 10px; }
.tool-record-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; font-size: 12px; color: var(--text-muted, #6b7280); }
.tool-record-toggle { display: inline-flex; align-items: center; gap: 6px; cursor: pointer; }
.tool-record-ledger-error { font-size: 12px; color: #ef4444; }
.tool-record-chart { overflow-x: auto; }
.tool-record-bar { cursor: pointer; }
.tool-record-bar:focus { outline: 2px solid var(--accent, #3b82f6); outline-offset: 1px; }
.tool-record-bar.is-selected rect { stroke: var(--accent, #3b82f6); stroke-width: 2; }
.tool-record-axis { font-size: 10px; fill: var(--text-muted, #6b7280); }
.tool-record-value { font-size: 10px; fill: var(--text-muted, #6b7280); }
.tool-record-reasons { display: flex; gap: 14px; align-items: flex-start; flex-wrap: wrap; border-top: 1px solid var(--border, #2a2f3a); padding-top: 8px; }
.tool-record-reason-row { display: grid; grid-template-columns: 10px 1fr auto auto; gap: 8px; align-items: center; font-size: 12px; }
.tool-record-swatch { width: 10px; height: 10px; border-radius: 2px; }
.tool-record-share { font-variant-numeric: tabular-nums; color: var(--text-muted, #6b7280); text-align: right; min-width: 42px; }
.tool-record-count { font-variant-numeric: tabular-nums; text-align: right; }
`;

// renderToolRecordBoard 渲染整块看板：表头（开关 + 合计）+ 柱形图 + （选中时）原因饼图。
//
// selectedTool 是当前选中的工具名（""= 没选，只画柱形图）。
export function renderToolRecordBoard(board, options = {}) {
  const stats = normalizeStats(board);
  const enabled = Boolean(board?.enabled);
  const selected = String(options.selectedTool ?? "");
  const parts = [`<div class="tool-record-view">`];
  parts.push(renderToolRecordHead(board, enabled));
  const ledgerError = String(board?.ledger_error ?? "").trim();
  if (ledgerError) {
    parts.push(`<div class="tool-record-ledger-error">⚠ 台账落盘失败（计数只在内存里）：${escapeHtml(ledgerError)}</div>`);
  }
  if (stats.length === 0) {
    parts.push(`<div class="tool-record-axis">${enabled ? "还没有工具调用记录" : "计数已关闭（打开后成功与失败都会被记录）"}</div>`);
    parts.push(`</div>`);
    return parts.join("");
  }
  parts.push(`<div class="tool-record-chart">${renderToolRecordBars(stats, selected)}</div>`);
  const active = stats.find((stat) => stat.tool === selected);
  if (active) {
    parts.push(renderToolRecordReasons(active));
  } else {
    parts.push(`<div class="tool-record-axis">点一根柱子看它的失败原因占比</div>`);
  }
  parts.push(`</div>`);
  return parts.join("");
}

// renderToolRecordHead 表头：合计（**只读**——计数开关在设置弹窗里，与 TUI 面板同口径：
// 看板只显示读数，不承担设置动作）。
function renderToolRecordHead(board, enabled) {
  const total = Number(board?.total) || 0;
  const success = Number(board?.success) || 0;
  const failure = Number(board?.failure) || 0;
  const rate = total > 0 ? ((success / total) * 100).toFixed(1) : "—";
  return (
    `<div class="tool-record-head">` +
    `<span>共 <strong>${total}</strong> 次 · 成功 ${success} / 失败 ${failure} · 成功率 ${rate}%</span>` +
    (enabled ? "" : `<span>计数已关闭（设置 → 工具记录）</span>`) +
    `</div>`
  );
}

// renderToolRecordBars 画**唯一一张**柱形图：一工具一根柱，上成功下失败。
//
// 高度按"所有工具里最大的总调用次数"缩放（相对比较才有意义：绝对值随会话长短漂移）。
export function renderToolRecordBars(stats, selected = "") {
  const rows = normalizeStats(stats);
  if (rows.length === 0) return "";
  const maxTotal = Math.max(...rows.map((row) => row.total), 1);
  const slotWidth = BAR_WIDTH + BAR_GAP;
  const width = rows.length * slotWidth + BAR_GAP;
  const height = CHART_HEIGHT + AXIS_HEIGHT + 14;
  const baseline = CHART_HEIGHT + 14;
  const bars = rows.map((row, index) => {
    const x = BAR_GAP + index * slotWidth;
    const scale = row.total / maxTotal;
    const successHeight = Math.round((row.success / Math.max(row.total, 1)) * scale * CHART_HEIGHT);
    const failureHeight = Math.round((row.failure / Math.max(row.total, 1)) * scale * CHART_HEIGHT);
    const barWidth = Math.min(BAR_WIDTH, Math.max(MIN_BAR_WIDTH, slotWidth - BAR_GAP));
    const isSelected = row.tool === selected;
    const title = `${row.tool}：成功 ${row.success} / 失败 ${row.failure}（共 ${row.total}）`;
    const label = row.tool.length > 6 ? `${row.tool.slice(0, 6)}…` : row.tool;
    return (
      `<g class="tool-record-bar${isSelected ? " is-selected" : ""}" data-tool-record-tool="${escapeHtml(row.tool)}" role="button" tabindex="0" aria-label="${escapeHtml(title)}">` +
      `<title>${escapeHtml(title)}</title>` +
      // 失败段贴着基线（下半），成功段坐在它上面（上半）——"成功在上面、失败在下面"。
      `<rect x="${x}" y="${baseline - failureHeight}" width="${barWidth}" height="${failureHeight}" fill="${FAILURE_COLOR}"></rect>` +
      `<rect x="${x}" y="${baseline - failureHeight - successHeight}" width="${barWidth}" height="${successHeight}" fill="${SUCCESS_COLOR}"></rect>` +
      `<text class="tool-record-value" x="${x + barWidth / 2}" y="${baseline - failureHeight - successHeight - 3}" text-anchor="middle">${row.total}</text>` +
      `<text class="tool-record-axis" x="${x + barWidth / 2}" y="${baseline + 12}" text-anchor="middle">${escapeHtml(label)}</text>` +
      `</g>`
    );
  });
  return `<svg width="${width}" height="${height}" viewBox="0 0 ${width} ${height}" role="img" aria-label="工具调用成功与失败柱形图">${bars.join("")}</svg>`;
}

// renderToolRecordReasons 画选中工具的**失败原因饼图** + 图例。
//
// 没有失败（或没有原因记录）→ 一行说明（不是空壳：用户点了柱子必须得到反馈）。
export function renderToolRecordReasons(stat) {
  const reasons = normalizeReasons(stat?.reasons);
  const header = `<div class="tool-record-head"><span><strong>${escapeHtml(String(stat?.tool ?? ""))}</strong> 的失败原因</span>` +
    `<span>失败 ${Number(stat?.failure) || 0} 次</span></div>`;
  if (reasons.length === 0) {
    return `<div class="tool-record-reasons"><div>${header}<div class="tool-record-axis">这个工具还没失败过</div></div></div>`;
  }
  const total = reasons.reduce((sum, row) => sum + row.count, 0);
  const pie = renderReasonsPie(reasons, total);
  const legend = reasons
    .map((row) => {
      const share = total > 0 ? ((row.count / total) * 100).toFixed(1) : "0.0";
      return (
        `<div class="tool-record-reason-row" title="${escapeHtml(reasonLabel(row.reason))}">` +
        `<span class="tool-record-swatch" style="background:${reasonColor(row.reason)}"></span>` +
        `<span>${escapeHtml(reasonLabel(row.reason))}</span>` +
        `<span class="tool-record-count">${row.count}</span>` +
        `<span class="tool-record-share">${share}%</span>` +
        `</div>`
      );
    })
    .join("");
  return `<div class="tool-record-reasons"><div>${header}${pie}</div><div>${legend}</div></div>`;
}

// renderReasonsPie 画原因占比（一片 = 一类原因）。
function renderReasonsPie(reasons, total) {
  const size = 132;
  const center = size / 2;
  const radius = 54;
  const title = reasons.map((row) => `${reasonLabel(row.reason)} ${row.count}`).join("、");
  const open = `<svg width="${size}" height="${size}" viewBox="0 0 ${size} ${size}" role="img" aria-label="${escapeHtml(title)}"><title>${escapeHtml(title)}</title>`;
  if (reasons.length === 1 || total === 0) {
    // 单原因（一片占满）时圆弧退化（起止点重合），改画整圆——否则看起来像"饼图坏了"。
    return `${open}<circle cx="${center}" cy="${center}" r="${radius}" fill="${reasonColor(reasons[0].reason)}"></circle></svg>`;
  }
  let angle = 0;
  const slices = reasons.map((row) => {
    const start = angle;
    angle += (row.count / total) * 360;
    return `<path d="${slicePath(center, center, radius, start, angle)}" fill="${reasonColor(row.reason)}"></path>`;
  });
  return `${open}${slices.join("")}</svg>`;
}

// normalizeStats 归一入参：只认正整数的成功/失败，按总次数降序（并列保持后端顺序）。
// 非数组 / 非法行一律忽略——渲染件不替后端判"这份数据对不对"，只判"能不能画"。
export function normalizeStats(board) {
  const rows = Array.isArray(board) ? board : Array.isArray(board?.tools) ? board.tools : [];
  return rows
    .map((row) => {
      const success = Math.max(0, Math.trunc(Number(row?.success ?? 0)) || 0);
      const failure = Math.max(0, Math.trunc(Number(row?.failure ?? 0)) || 0);
      return {
        tool: String(row?.tool ?? "").trim(),
        success,
        failure,
        total: success + failure,
        lastAt: row?.last_at ?? "",
        reasons: normalizeReasons(row?.reasons),
      };
    })
    .filter((row) => row.tool && row.total > 0)
    .sort((a, b) => b.total - a.total);
}

// normalizeReasons 归一原因表（词表外的标识照原样留：原因标识是后端事实，渲染件不吞）。
export function normalizeReasons(reasons) {
  if (!Array.isArray(reasons)) return [];
  return reasons
    .map((row) => ({ reason: String(row?.reason ?? "").trim(), count: Math.max(0, Math.trunc(Number(row?.count ?? 0)) || 0) }))
    .filter((row) => row.reason && row.count > 0)
    .sort((a, b) => b.count - a.count);
}

export function reasonLabel(reason) {
  return REASON_LABELS[reason] ?? reason;
}

export function reasonColor(reason) {
  return REASON_COLORS[reason] ?? REASON_COLORS.other;
}

// slicePath 画一片扇形（角度从 12 点方向顺时针）。
function slicePath(cx, cy, radius, startDeg, endDeg) {
  const [x0, y0] = polar(cx, cy, radius, startDeg);
  const [x1, y1] = polar(cx, cy, radius, endDeg);
  const largeArc = endDeg - startDeg > 180 ? 1 : 0;
  return `M ${round(cx)} ${round(cy)} L ${round(x0)} ${round(y0)} A ${radius} ${radius} 0 ${largeArc} 1 ${round(x1)} ${round(y1)} Z`;
}

function polar(cx, cy, radius, angleDeg) {
  const radians = ((angleDeg - 90) * Math.PI) / 180;
  return [cx + radius * Math.cos(radians), cy + radius * Math.sin(radians)];
}

function round(value) {
  return Math.round(value * 1000) / 1000;
}
