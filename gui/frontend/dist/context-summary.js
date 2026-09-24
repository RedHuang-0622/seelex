import { escapeHtml } from "./components.js";
// 压缩记录的展示口径与轨迹「压缩」轨共用同一份纯函数（compaction-format.js）：
// 两处各写一份就会出现同一条记录两种读法的漂移。
import { compactionGateDurationText, compactionGateLabel, compactionOriginLabel, compactionOutcomeLabel, compactionRangeText, compactionReasonLabel } from "./compaction-format.js";

// renderContextCompactions 渲染右栏「上下文压缩」：门禁进度条（瞬态，一轮压缩
// 结束即撤）+ 记录条目（公开元数据：版本/原因/来源/被压区间/估算/时间）+ 按
// ref 展开的折叠帧正文。
//
// 这里此前只有一句硬编码英文占位句（"Task checkpoint retained; details can be
// re-read when needed."）——用户看到的"压缩帧像占位符"就是它：没有区间、没有来源、
// 不可展开。正文不进快照，options.detail 携带视图侧按 frame_ref 读回来的那一页
// （{ index, loading, error, text, hasMore, nextOffset, totalBytes }；index 是本
// 次展开的条目下标，-1 = 未展开）。
//
// options.progress 是一轮压缩的门禁进度（compaction.progress 载荷）。进度不进
// 快照，因此「零记录 + 有进度」也要出内容：折叠发生在写记录之前，只有记录时
// 才显示就会让进度条在唯一的"还没有记录"那一轮里彻底不出现。
export function renderContextCompactions(compactions = [], options = {}) {
  const records = Array.isArray(compactions) ? compactions : [];
  const progress = renderCompactionProgress(options.progress);
  if (records.length === 0) return progress;
  const detail = options.detail && typeof options.detail === "object" ? options.detail : null;
  const title = '<div class="context-summary-title">上下文压缩</div>';
  return `${progress ? `${progress}${title}` : title}${records.map((compaction, index) => {
    const version = Number(compaction?.version || 0);
    const reason = compactionReasonLabel(compaction?.reason);
    const origin = compactionOriginLabel(compaction?.origin);
    const tokens = Number(compaction?.estimated_tokens || 0);
    const range = compactionRangeText(compaction);
    const time = formatTime(compaction?.compacted_at);
    const facts = [
      range ? `被压区间 ${range}` : "无区间边界",
      tokens ? `约 ${formatNumber(tokens)} tokens` : "",
      time
    ].filter(Boolean).join(" · ");
    const frameRef = String(compaction?.frame_ref || "");
    const open = Boolean(detail) && detail.index === index;
    const actions = frameRef
      ? `<button type="button" class="context-summary-open" data-compact-open="${index}" aria-expanded="${open}" title="按 ref ${escapeHtml(frameRef)} 读取折叠帧正文">${open ? "收起帧正文" : "查看帧正文"}</button>
        <button type="button" class="context-summary-open" data-compact-frame-ref="${escapeHtml(frameRef)}" title="在可调大小的弹框里查看帧正文（ref ${escapeHtml(frameRef)}）">弹框查看</button>`
      : `<span class="context-summary-noframe">本次没有可回读正文</span>`;
    return `<article class="context-summary-item">
      <header><strong>#${escapeHtml(String(version || "?"))}</strong><span>${escapeHtml(reason)}</span>${origin ? `<em class="context-summary-origin">${escapeHtml(origin)}</em>` : ""}</header>
      <small>${escapeHtml(facts)}</small>
      <div class="context-summary-actions">${actions}${frameRef ? `<span class="context-summary-ref" title="会话内容存储里的引用（ref）">${escapeHtml(frameRef)}</span>` : ""}</div>
      ${open ? renderFrameDetail(compaction, detail, frameRef, tokens) : ""}
    </article>`;
  }).join("")}`;
}

// renderCompactionProgress 渲染一轮压缩的门禁进度条：复用 Plan 面板同款轨道与
// 填充（.plan-board-progress / .plan-board-bar），不自造第二套进度组件。进度是
// 瞬态（后端每收一关发一条 compaction.progress，终局后生命周期结束），因此
// 它不进快照、不进记录列表，只由视图侧按事件暂存后传进来。
//
// 轨道下面挂**逐关耗时清单**：整轮压缩只有几十毫秒（一次全量 token 估算 + 一次
// 装配 + 一次落帧），读者不可能看到"慢慢走"的进度条；能回答"它到底干了什么、卡在
// 哪一步"的只有每一关自己花掉的时间。这份清单因此不是装饰——没有它，界面上就只剩
// 一条瞬时满格的绿条，读者合理地认为什么都没发生。
export function renderCompactionProgress(progress) {
  if (!progress || typeof progress !== "object") return "";
  const total = Number(progress.total || 0);
  if (!(total > 0)) return "";
  const state = progress.state === "failed" ? "failed"
    : (progress.state === "done" ? "done" : "running");
  const index = Math.min(Math.max(Number(progress.index || 0), 0), total);
  const version = Number(progress.version || 0);
  const begin = state === "running" && String(progress.phase || "") === "begin";
  const label = state === "running" ? compactionGateLabel(progress.gate)
    : (state === "failed" ? "压缩未完成" : "压缩完成");
  // 轨道填充 = 真正收口过的门禁数（视图侧在终局事件上沿用最后一条 running 的
  // 序号），所以中途失败的那一轮停在半路，不会靠"终局 index=total"画成满格；
  // detail 在运行关是该关的判据（如 compared/all/soft/hard），在终局是
  // reached=N/M，两个数字口径一致。
  const facts = [
    compactionOriginLabel(progress.origin),
    state === "running" ? "" : compactionOutcomeLabel(progress.outcome),
    String(progress.detail || "")
  ].filter(Boolean).join(" · ");
  const elapsed = Number(progress.elapsedMs || 0);
  return `<div class="context-compaction-progress is-${state}" role="progressbar" aria-label="上下文压缩进度" aria-valuemin="0" aria-valuemax="${total}" aria-valuenow="${index}">
    <div class="context-compaction-progress-head">
      <span class="context-compaction-progress-label">${escapeHtml(label)}</span>
      <span class="context-compaction-progress-count">${index}/${total}${version > 0 ? ` · #${version}` : ""}${elapsed > 0 ? ` · 共 ${escapeHtml(compactionGateDurationText(elapsed))}` : ""}</span>
    </div>
    <div class="plan-board-progress"><div class="plan-board-bar" style="width:${Math.round((index / total) * 100)}%"></div></div>
    ${facts ? `<small class="context-compaction-progress-detail">${escapeHtml(facts)}</small>` : ""}
    ${renderCompactionGateTimeline(progress, begin)}
  </div>`;
}

// renderCompactionGateTimeline 渲染逐关耗时清单（<ol>，顺序 = 后端执行顺序）。
//
// 起手帧（phase=begin）时判据估算还没收口：给它一行"进行中"而不是编一个耗时——
// 这一行正是"按下回车后立刻有反馈"的落点，也是显式压缩最长的一段等待。
function renderCompactionGateTimeline(progress, begin) {
  const gates = Array.isArray(progress.gates) ? progress.gates : [];
  const rows = gates.map((item) => `<li class="context-compaction-gate is-done">
      <span class="context-compaction-gate-name">${escapeHtml(compactionGateLabel(item?.gate))}</span>
      <em class="context-compaction-gate-ms" title="这一关实测耗时">${escapeHtml(compactionGateDurationText(item?.ms))}</em>
    </li>`);
  if (begin) {
    rows.unshift(`<li class="context-compaction-gate is-running">
      <span class="context-compaction-gate-name">${escapeHtml(compactionGateLabel(progress.gate))}</span>
      <em class="context-compaction-gate-ms">进行中</em>
    </li>`);
  }
  if (rows.length === 0) return "";
  return `<ol class="context-compaction-gates">${rows.join("")}</ol>`;
}

// renderCompactionFrameModal 渲染弹框里的折叠帧正文：复用右栏展开时的同一份正文区
// （renderFrameDetail）与同一套分页标记（data-compact-frame-load），因此弹框与右栏
// 读的是同一个 ref、同一段正文、同一套"加载更多"行为，不会出现两种读法。
//
// 为什么要有弹框：右栏在 状态/账户/团队 的折叠区之间，正文又最长，面板里只能看到
// 几行；弹框可调大小（data-resizable），读长正文才是可用的。
export function renderCompactionFrameModal(state = {}) {
  const record = state.record && typeof state.record === "object" ? state.record : {};
  const frameRef = String(record.frame_ref || "");
  if (!frameRef) {
    return '<span class="muted">这条压缩记录没有帧正文引用（frame_ref 为空），本次没有可回读的正文。</span>';
  }
  return renderFrameDetail(record, state.detail && typeof state.detail === "object" ? state.detail : {}, frameRef, Number(record.estimated_tokens || 0));
}

// renderFrameDetail 渲染展开的折叠帧正文区：复用轨迹详情同一容器与分页组件
// （.axis-detail / .axis-detail-text / data-compact-frame-load），不自造第二套
// 面板与第二套分页交互。
function renderFrameDetail(compaction, detail, frameRef, tokens) {
  const size = [
    Number(compaction?.frame_bytes || 0) > 0 ? `${formatNumber(compaction.frame_bytes)} bytes` : "",
    Number(compaction?.frame_tokens || 0) > 0 ? `约 ${formatNumber(compaction.frame_tokens)} tokens` : ""
  ].filter(Boolean).join(" · ");
  const head = `<div class="axis-detail-frame-head">
      <span class="axis-detail-frame-title">折叠帧正文</span>
      <span class="axis-detail-frame-ref" title="会话内容存储里的引用（ref），前端按 ref 分页读取">${escapeHtml(frameRef)}</span>
      ${size ? `<span class="axis-detail-frame-size">${escapeHtml(size)}</span>` : ""}
    </div>`;
  if (detail.loading) {
    return `<section class="axis-detail is-compression">${head}<pre class="axis-detail-text">读取中…</pre></section>`;
  }
  if (detail.error) {
    return `<section class="axis-detail is-compression">${head}
      <pre class="axis-detail-text">读取失败：${escapeHtml(String(detail.error))}</pre>
      <div class="axis-detail-frame-actions"><button type="button" class="axis-detail-frame-load" data-compact-frame-load="first">重试</button></div>
    </section>`;
  }
  const remaining = Math.max(Number(detail.totalBytes || 0) - Number(detail.nextOffset || 0), 0);
  const actions = detail.hasMore
    ? `<button type="button" class="axis-detail-frame-load" data-compact-frame-load="more">加载更多${remaining > 0 ? `（剩余约 ${formatNumber(remaining)} bytes）` : ""}</button>`
    : `<span class="axis-detail-frame-done">已加载完${Number(detail.totalBytes || 0) > 0 ? `（${formatNumber(detail.totalBytes)} bytes）` : ""}</span>`;
  return `<section class="axis-detail is-compression">${head}
    <pre class="axis-detail-text">${escapeHtml(String(detail.text || "")) || '<span class="muted">（空正文）</span>'}</pre>
    <div class="axis-detail-frame-actions">${actions}</div>
  </section>`;
}

function formatNumber(value) {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(Number(value) || 0);
}

function formatTime(value) {
  const time = new Date(value);
  if (Number.isNaN(time.getTime())) return "";
  return time.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}
