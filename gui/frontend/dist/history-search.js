import { escapeHtml } from "./components.js";

// ── 历史检索面板（长上下文历史记录检索）──────────────────────────
// 数据源：invoke("SearchHistory", query, limit) 的权威返回
// （seelexctx/search.Result：压缩栈帧命中 → 真实聊天记录，token 预算截断）。
// 渲染只读展示：帧命中（摘要 + 范围 + 分数）经 <details> 展开聊天记录；
// 所有文本 escape，状态全部来自权威 JSON，不维护本地猜测。

const ROLE_LABELS = { user: "用户", assistant: "助手", tool: "工具" };

// historySearchView 归一化检索结果（防御畸形载荷：非对象 → 空；
// hits 非数组 → []；hit 非对象或 segment_id 非字符串的条目丢弃）。
export function historySearchView(result) {
  if (!result || typeof result !== "object" || Array.isArray(result)) {
    return { hits: [], note: "", totalUnits: 0, indexedFrames: 0, truncated: false };
  }
  const hits = Array.isArray(result.hits)
    ? result.hits.filter(hit => Boolean(hit) && typeof hit === "object" && typeof hit.segment_id === "string")
    : [];
  return {
    hits,
    note: typeof result.note === "string" ? result.note : "",
    totalUnits: Number.isFinite(result.total_units) ? result.total_units : 0,
    indexedFrames: Number.isFinite(result.indexed_frames) ? result.indexed_frames : 0,
    truncated: Boolean(result.truncated)
  };
}

// renderHistorySearchResults 渲染检索结果 HTML：无命中 → 提示（含未压缩
// 兜底说明）；命中 → 每帧一个 <details>（summary = 段标识 + 范围 + 分数 +
// 摘要），展开后为聊天记录列表；首个命中默认展开。
export function renderHistorySearchResults(result) {
  const view = historySearchView(result);
  if (!view.hits.length) {
    const hint = view.note
      ? escapeHtml(view.note)
      : "没有匹配的历史记录";
    return `<div class="history-search-empty">${hint}</div>`;
  }
  const meta = `索引 ${view.indexedFrames} 帧 · 事件流 ${view.totalUnits} 轮` + (view.truncated ? " · 预算截断" : "");
  return `<div class="history-search-meta">${escapeHtml(meta)}</div>` +
    view.hits.map((hit, index) => `
      <details class="history-search-hit"${index === 0 ? " open" : ""}>
        <summary>
          <span class="history-search-head"><strong>${escapeHtml(hit.segment_id)}</strong><small>${escapeHtml(rangeText(hit))} · 命中分 ${escapeHtml(scoreText(hit.score))}</small></span>
          <span class="history-search-summary">${escapeHtml(hit.summary || "(无摘要)")}</span>
        </summary>
        <ol class="history-search-records">${renderRecords(hit)}</ol>
      </details>`).join("");
}

function rangeText(hit) {
  const from = Number.isFinite(hit.from) ? hit.from : 0;
  const to = Number.isFinite(hit.to) ? hit.to : 0;
  return `[${from}..${to}]`;
}

function scoreText(score) {
  return Number.isFinite(score) ? score.toFixed(2) : "—";
}

function renderRecords(hit) {
  const records = Array.isArray(hit.records) ? hit.records.filter(record => Boolean(record) && typeof record === "object") : [];
  if (!records.length) {
    return `<li class="history-search-record is-empty">（该帧范围内无事件记录）</li>`;
  }
  return records.map(record => {
    const role = ROLE_LABELS[record.role] || record.role || "消息";
    const tool = record.tool_name ? `<span class="history-search-tool">${escapeHtml(record.tool_name)}</span>` : "";
    const ref = record.result_ref ? `<span class="history-search-ref" title="${escapeHtml(record.result_ref)}">${escapeHtml(record.result_ref)}</span>` : "";
    const cut = record.truncated ? `<span class="history-search-cut">已截断</span>` : "";
    return `<li class="history-search-record">
      <span class="history-search-role">${escapeHtml(role)}</span>${tool}
      <span class="history-search-content">${escapeHtml(record.content || "")}</span>${ref}${cut}
    </li>`;
  }).join("");
}
