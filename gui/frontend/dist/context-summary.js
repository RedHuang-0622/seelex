import { escapeHtml, renderSeqBadge } from "./components.js";
// 压缩记录的展示口径与轨迹「压缩」轨共用同一份纯函数（compaction-format.js）：
// 两处各写一份就会出现同一条记录两种读法的漂移。
import { compactionFailureText, compactionFrontier, compactionGateDurationText, compactionGateLabel, compactionOriginLabel, compactionOutcomeLabel, compactionRangeText, compactionReasonLabel, compactionStackOrder } from "./compaction-format.js";

// 压缩栈表格（右栏 状态/概要 里唯一的内容块）。
//
// 展示口径与轨迹「压缩」轨共用同一份纯函数（compaction-format.js）：两处各写一份就
// 会出现同一条记录两种读法的漂移。
//
// renderContextCompactions 渲染门禁进度条（瞬态，一轮压缩结束即撤）+ 压缩条目表：
// 一行一次压缩，栈顶 = 当前前沿（深灰），更早的压缩往下排（浅灰）；每次成功压缩都
// 带一枚 **seq 徽标**（复用 goal 看板 active seq 的同一件组件，见 components.js
// renderSeqBadge）——用户口径：压缩成功之后状态里要有一条带 seq 的压缩条目。
// 压缩失败也留一条痕（`Failed=true`，无区间无帧），排在栈下方并用失败色标明它不是
// 一次压缩（用户口径：压缩失败 → 留下失败记录 → 原始上下文继续存在）。
// 点开某行读该帧正文（detail 由视图侧按 frame_ref 分页读回来），没有帧引用的行不给
// 展开入口。
//
// 这里此前是一列卡片 + 一句硬编码英文占位句（"Task checkpoint retained; details can
// be re-read when needed."）——用户看到的"压缩帧像占位符"就是它：没有区间、没有来源、
// 不可展开。卡片改表格的原因同状态表：右栏窄，自由布局会错位，表格行始终对齐。
//
// options.progress 是一轮压缩的门禁进度（compaction.progress 载荷）。进度不进
// 快照，因此「零记录 + 有进度」也要出内容：压缩发生在写记录之前，只有记录时
// 才显示就会让进度条在唯一的"还没有记录"那一轮里彻底不出现。
const STACK_COLUMNS = ["seq", "压缩", "帧"];

export function renderContextCompactions(compactions = [], options = {}) {
  const records = Array.isArray(compactions) ? compactions : [];
  const progress = renderCompactionProgress(options.progress);
  if (records.length === 0) return progress;
  const detail = options.detail && typeof options.detail === "object" ? options.detail : null;
  const frontier = compactionFrontier(records);
  const title = '<div class="context-summary-title">上下文压缩</div>';
  const head = `<div class="compaction-stack-row is-head" role="row">${
    STACK_COLUMNS.map(label => `<span role="columnheader">${escapeHtml(label)}</span>`).join("")}</div>`;
  // 成功记录按栈序（栈顶在前），失败痕接在末尾：失败不是一次压缩，它没有栈位
  // （区间恒空），硬塞进栈序只会让读者以为"压缩栈上多了一格"。栈序判定本身也要
  // 先滤掉失败痕——它们的区间恒空（end=-1），混进栈序会让同一条失败被画两次。
  const failures = records
    .map((record, index) => ({ record, index }))
    .filter(entry => entry.record && entry.record.failed);
  const rows = [
    ...compactionStackOrder(records)
      .filter(index => !records[index]?.failed)
      .map(index => renderRow(records[index] || {}, index, frontier, detail, options)),
    ...failures.map(entry => renderFailureRow(entry.record, entry.index))
  ].join("");
  return `${progress ? `${progress}${title}` : title}<div class="compaction-stack" role="table" aria-label="压缩栈：一行一次压缩，栈顶是当前前沿；压缩失败的行排在栈下方（它没有区间）">${head}${rows}</div>`;
}

// renderRow 渲染一次**成功压缩**的条目：seq 徽标（= 压缩版本号）+ 触发/来源/区间
// + 帧正文入口。
//
// 展开哪一行按 **(会话, frame_ref)** 判定，不按记录数组下标：
//   - 下标会随记录数组重排、更会随会话切换指到另一条记录——上一个会话读回来的
//     正文挂到当前会话的同一序号行上，就是把别的会话的压缩说成当前会话的；
//   - ref 是内容存储里的引用，跨会话不会撞，且与读取侧（data-compact-frame-load
//     按 ref 分页）同一身份，不会出现"展开的是这条、续读的是那条"；
//   - options.sessionID 是当前视图会话：详情里写的会话对不上就不认这份正文
//     （即使 ref 撞了，也不许跨会话显示）。
function renderRow(compaction, index, frontier, detail, options) {
  const version = Number(compaction.version || 0);
  const reason = compactionReasonLabel(compaction.reason);
  const origin = compactionOriginLabel(compaction.origin);
  const tokens = Number(compaction.estimated_tokens || 0);
  const range = compactionRangeText(compaction);
  const time = formatTime(compaction.compacted_at);
  const isFrontier = Boolean(frontier) && frontier.index === index;
  const frameRef = String(compaction.frame_ref || "");
  const detailSession = detail ? String(detail.sessionID || "") : "";
  const detailRef = detail ? String(detail.ref || "") : "";
  const sessionID = String(options.sessionID || "");
  const detailApplies = detailRef !== "" && (sessionID === "" || detailSession === "" || detailSession === sessionID);
  const open = detailApplies && detailRef === frameRef;
  // 一行两排：右栏实测量级只有 ~280px，四列会把中文按字符切碎（浏览器核对里
  // "message-1..message-663" 被断成 mes/sage-）。触发与来源同排，区间/估算/
  // 时间另起一排等宽小字。
  const facts = [range || "无区间边界", tokens ? `${formatNumber(tokens)} tokens` : "", time].filter(Boolean).join(" · ");
  // 动作列两个短按钮：右栏实测量级只有 ~280px，"查看帧正文"这种五字按钮会把
  // 触发与区间挤成四行（浏览器核对实测），而两个入口都得一次点击到位。
  const actions = frameRef
    ? `<button type="button" class="context-summary-open" data-compact-open="${index}" aria-expanded="${open}" title="按 ref ${escapeHtml(frameRef)} 读取压缩帧正文${open ? "（收起）" : "（展开在本行下方）"}">${open ? "收起" : "帧正文"}</button>
        <button type="button" class="context-summary-open" data-compact-frame-ref="${escapeHtml(frameRef)}" title="在可调大小的弹框里查看帧正文（ref ${escapeHtml(frameRef)}）">弹框</button>`
    : `<span class="context-summary-noframe">无帧正文</span>`;
  const row = [
    `<div class="compaction-stack-row${isFrontier ? " is-frontier" : " is-stale"}${open ? " is-open" : ""}" role="row" data-compact-index="${index}" title="${escapeHtml([reason, origin, facts].filter(Boolean).join(" · "))}">`,
    `<span role="cell" class="compaction-stack-cell is-version">${renderSeqBadge({ seq: version || "?", unit: "seq", tone: "compaction", title: `压缩 seq：第 ${version} 次压缩（栈顶 = 当前前沿）` })}${isFrontier ? '<em class="compaction-stack-flag">栈顶</em>' : ""}</span>`,
    `<span role="cell" class="compaction-stack-cell is-body">`,
    `<strong>${escapeHtml(reason)}</strong>${origin ? `<em class="compaction-stack-origin">${escapeHtml(origin)}</em>` : ""}`,
    `<small>${escapeHtml(facts)}</small></span>`,
    `<span role="cell" class="compaction-stack-cell is-actions">${actions}</span>`,
    "</div>"
  ].join("");
  return open ? `${row}<div class="compaction-stack-detail" role="row">${renderFrameDetail(compaction, detail, frameRef, tokens)}</div>` : row;
}

// renderFailureRow 渲染一条**压缩失败**的痕：seq 徽标（= 失败时的上下文版本）+ 原因
// + 一句"上下文原样继续"。它没有区间、没有帧、也没有动作列——不是一次压缩，没有正文
// 可读，也没有"栈顶"可言。
//
// 为什么失败要进这张表（而不仅是 6 秒的瞬态进度条）：用户口径是「压缩失败 → **留下
// 失败记录** → 原始上下文继续存在 → 模型仍然直接看到原来的上下文」。瞬态进度条撤了
// 之后，用户再想确认"刚才那次到底压没压"就查无实据。
function renderFailureRow(compaction, index) {
  const version = Number(compaction.version || 0);
  const origin = compactionOriginLabel(compaction.origin);
  const time = formatTime(compaction.compacted_at);
  const reason = compactionFailureText(compaction);
  const facts = [time, origin].filter(Boolean).join(" · ");
  return [
    `<div class="compaction-stack-row is-failed" role="row" data-compact-index="${index}" data-compact-failed="1" title="${escapeHtml(reason)}">`,
    `<span role="cell" class="compaction-stack-cell is-version">${renderSeqBadge({ seq: version || "?", unit: "seq", tone: "compaction", failed: true, title: "压缩失败：这一次没有压缩，上下文原样保留" })}<em class="compaction-stack-flag">失败</em></span>`,
    `<span role="cell" class="compaction-stack-cell is-body">`,
    `<strong>压缩失败</strong>`,
    facts ? `<small>${escapeHtml(facts)}</small>` : "",
    `</span>`,
    `<span role="cell" class="compaction-stack-cell is-actions"><span class="context-summary-noframe">上下文原样</span></span>`,
    // 原因跨整行：右栏实测量级只有 ~360px，把它塞进中列会被压成一条竖着的碎字
    // （而原因里恰好有一串不能断行的数字事实 estimated=…/budget=…/window=…）。
    `<span class="compaction-stack-note" role="cell">${escapeHtml(reason)}</span>`,
    "</div>"
  ].join("");
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

// renderCompactionFrameModal 渲染弹框里的压缩帧正文：复用右栏展开时的同一份正文区
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

// renderFrameDetail 渲染展开的压缩帧正文区：复用轨迹详情同一容器与分页组件
// （.axis-detail / .axis-detail-text / data-compact-frame-load），不自造第二套
// 面板与第二套分页交互。
function renderFrameDetail(compaction, detail, frameRef, tokens) {
  const size = [
    Number(compaction?.frame_bytes || 0) > 0 ? `${formatNumber(compaction.frame_bytes)} bytes` : "",
    Number(compaction?.frame_tokens || 0) > 0 ? `约 ${formatNumber(compaction.frame_tokens)} tokens` : ""
  ].filter(Boolean).join(" · ");
  const head = `<div class="axis-detail-frame-head">
      <span class="axis-detail-frame-title">压缩帧正文</span>
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
