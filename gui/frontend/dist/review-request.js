// review-request.js ── 「agent 驱动 GUI 打开待审阅文件」的前端合同与纯逻辑（J-2）。
//
// 生态位：这是 app.js「apply review request」路径的**可测部分**——不碰 DOM / Bridge，
// 只做三件可判定的事：解析快照里的 `review_request`、判定"这是新的一次召唤还是同一份
// 快照的补放"、把每份文件映射成抽屉条目。开 chip / 读盘 / 展开抽屉由 app.js 与
// file-preview.js 承担（见那里的 openDeferred）。
//
// 契约（唯一事实）在后端：application/contract/dto/review_request.go（dto.ReviewRequest）
// 与 docs/gui/schemas/session-snapshot.schema.json。快照顶层键 `review_request`
// （缺省 / null = 该会话当前没有待审阅请求）：
//   { request_id, files: [{ path, note?, start_line?, end_line? }], skipped?, requested_at? }
//
// **清单只带路径、不带内容**：字节仍由前端按路径走既有的 Bridge.WorkspaceFileContent
// 读回（containment / 敏感过滤 / 二进制 / 上限在 workspace 域统一裁决）。所以这条新入口
// **不可能**绕过任何既有边界——前端这一侧只是"按路径开 chip"。
//
// 为什么是**状态**而不是事件：GUI 可能没开 / 正在重连，或用户此刻在别的会话上。一次性
// 事件在"当下没有接收者"时就丢了；落在这份**会话作用域**的快照里，前端起来 / 切回该会话
// 时按快照补放（补放口径见 decideReviewApply）。

// MAX_REVIEW_FILES_PER_REQUEST 是合同的批处理硬上限（与后端 dto.MaxReviewFilesPerRequest
// 同数）。它**不是**前端要再判一遍的门（超限由后端显式拒绝、不静默截断），而是 chip 条的
// 宽度预算基准：可见枚数（file-preview.PREVIEW_CHIP_VISIBLE_LIMIT = 5）+ 溢出计数足以容纳
// 满额 10 份。
export const MAX_REVIEW_FILES_PER_REQUEST = 10;

// REVIEW_SKIP_REASONS 是跳过原因词表（与 dto.ReviewSkip.Reason 同字面量）：只用于把
// "哪几份没进来、为什么"如实转述（工具结果 / chip 悬停里的同一句话）。
export const REVIEW_SKIP_REASONS = Object.freeze([
  "sandbox", "sensitive", "binary", "missing", "duplicate"
]);

// normalizeReviewRequest 解析并校验快照里的 review_request；形状不合法（缺 request_id、
// files 不是非空数组）时返回 null——宁可不弹，也不按半份契约开 chip。
// 返回 { requestID, files: [{path, note?, startLine?, endLine?}], skipped: [{path, reason}] }。
export function normalizeReviewRequest(snapshot) {
  const raw = snapshot?.review_request;
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return null;
  const requestID = typeof raw.request_id === "string" ? raw.request_id.trim() : "";
  if (!requestID) return null;
  const files = [];
  for (const item of Array.isArray(raw.files) ? raw.files : []) {
    if (!item || typeof item !== "object" || Array.isArray(item)) continue;
    const path = typeof item.path === "string" ? item.path : "";
    if (!path) continue;
    const ref = { path };
    if (typeof item.note === "string" && item.note) ref.note = item.note;
    // 行区间只在两端都成区间时带上（单端 / 倒序视为"不指定"，与后端同口径）。
    const start = Number(item.start_line);
    const end = Number(item.end_line);
    if (Number.isInteger(start) && Number.isInteger(end) && start >= 1 && end >= start) {
      ref.startLine = start;
      ref.endLine = end;
    }
    files.push(ref);
  }
  if (!files.length) return null;
  return { requestID, files, skipped: normalizeSkipped(raw.skipped) };
}

function normalizeSkipped(list) {
  const out = [];
  for (const item of Array.isArray(list) ? list : []) {
    const path = typeof item?.path === "string" ? item.path : "";
    const reason = typeof item?.reason === "string" ? item.reason : "";
    if (path && reason) out.push({ path, reason });
  }
  return out;
}

// reviewRequestKey 是"这一次召唤"的身份：会话 ID + request_id。两段都带上，切到别的
// 会话再切回来时同一个 request_id 不会被当成"已应用"而漏补放（请求是会话作用域的）。
export function reviewRequestKey(snapshot, request = normalizeReviewRequest(snapshot)) {
  if (!request) return "";
  const sessionID = typeof snapshot?.session?.id === "string" ? snapshot.session.id : "";
  return `${sessionID}::${request.requestID}`;
}

// decideReviewApply 是"到底要不要动界面"的唯一判据（纯函数，好测）：
//   - 快照没有合法请求 → 不动（absent）；
//   - 这一次召唤已被应用过（同一份快照的补放）→ 不动（replay）：不重复开 tab、不抢当前文件；
//   - 用户正在编辑 → 本轮**不抢**（editing），**不推进已应用水位**，下一次快照再试（补放）；
//   - 其余 → 应用（new）。
// 返回 { apply, reason, key, request }。apply=true 时应用方负责把 key 记为已应用水位。
export function decideReviewApply(snapshot, { appliedKey = "", editing = false } = {}) {
  const request = normalizeReviewRequest(snapshot);
  if (!request) return { apply: false, reason: "absent", key: appliedKey, request: null };
  const key = reviewRequestKey(snapshot, request);
  if (key && key === appliedKey) return { apply: false, reason: "replay", key: appliedKey, request };
  if (editing) return { apply: false, reason: "editing", key: appliedKey, request };
  return { apply: true, reason: "new", key, request };
}

// reviewRequestEntries 把清单映射成抽屉条目（file-preview 的 openPreviewTab 形状）：
// 只带 path（name 由抽屉按路径末段归一）+ note（写进 chip 的悬停提示）。这样"同路径
// 同一条目"由抽屉的去重语义负责——重复召唤同一份文件只是激活，不重复读盘。
export function reviewRequestEntries(request) {
  return (request?.files || []).map(file => (file.note ? { path: file.path, note: file.note } : { path: file.path }));
}
