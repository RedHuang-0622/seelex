// ── 草稿（composer draft）的生命周期与页面 context ─────────────
// app.js 是壳层的 composition/orchestration（DOM 事件、Bridge 调用），规则本身
// 住在这里：草稿在一轮里怎么走、页面要同时显示什么。可被 node --test 直接覆盖，
// 不碰 DOM、不碰 Bridge。
//
// 存在理由（两个用户可见的缺口，规则集中一处避免各处各写一遍）：
//  1) 页面 context 缺"草稿"这一半：会话页此前只渲染既定（已落盘/已投影）的
//     message，本会话当前未发送的草稿只活在输入框里——切走后回看这一页（或重启
//     后）既看不到"这一轮还没发出去的是什么"，也无法把它与既定消息区分开。
//     `composerDraftRows` 给出页面要渲染的行：既定的 message + 一条标成草稿的
//     未发送行，二者按 `kind`/`draft` 可区分。
//  2) 草稿与"本轮取消/完成"没有同步：一轮完成（物化提交）后已发送的正文归消息、
//     未发送的剩余部分仍是草稿；一轮被取消时正文按未发送保留（不吞字）。这两个
//     终点由 `draftRoundEvent` + `draftLifecycle` 的 materialize/cancel 事件表达，
//     后端侧的清空落在 application/core/composer_draft.go（clearComposerDraft）。
//
// 与 composer-input.js 的分工：那里是"输入框正文的编辑/归属规则"（提交只切掉
// 已发送那段、正文按会话留存、后端旧副本不回填），这里是"这份草稿走到了哪一步、
// 页面怎么看见它"。提交事件携带的 `remaining` 由 app.js 用
// `clearSubmittedText(current, sent)` 算好传进来（不在这里复制那条规则）。

// phase 是草稿的进度：
//   "idle"    —— 没有未发送正文；
//   "unsent"  —— 有未发送正文，且没有一轮在途（本轮结束/被取消后都回到这里）；
//   "sending" —— 有正文已被本轮带走（提交在途），本地剩余正文等权威收敛。
export const DRAFT_PHASE_IDLE = "idle";
export const DRAFT_PHASE_UNSENT = "unsent";
export const DRAFT_PHASE_SENDING = "sending";

// DRAFT_ROW_KIND / MESSAGE_ROW_KIND 是页面行的种类标记：既定的 message 与
// 未发送的草稿行靠它区分（渲染层据此给草稿行加草稿样式，不要靠文本猜）。
export const MESSAGE_ROW_KIND = "message";
export const DRAFT_ROW_KIND = "draft";

// draftLifecycleFromSnapshot 从后端权威快照派生本地草稿状态。
//   sessionID：视图会话 ID（草稿归属的键）；
//   draft：该会话是不是尚未物化的草稿会话（只有这种会话的正文允许落盘草稿）；
//   composer：后端持有的草稿正文（草稿会话跨重启恢复的那一份）。
// 注意：本地未落盘的输入优先于后端副本由调用方决定（composer-input.js
// shouldRestoreDraft / composerDirty），这里只给出权威起点。
export function draftLifecycleFromSnapshot({ sessionID, draft, composer } = {}) {
  const attached = Boolean(draft);
  const text = attached ? String(composer ?? "") : "";
  return {
    sessionID: String(sessionID ?? ""),
    attached,
    text,
    phase: text === "" ? DRAFT_PHASE_IDLE : DRAFT_PHASE_UNSENT
  };
}

// draftLifecycle 是草稿状态迁移的唯一入口（纯函数，返回新状态，不改入参）。
// 事件：
//   { type: "append", text }       —— 输入框正文变化（用户敲字/程序化写入/
//                                      接受建议）。text = 当前框内正文。
//   { type: "submit", remaining }  —— 一次提交（正文被本轮带走）。remaining =
//                                      提交往返后框里剩下的本地正文（调用方按
//                                      clearSubmittedText 算好）。
//   { type: "materialize" }        —— 一轮完成（物化提交）：草稿会话已被消费，
//                                      已发送的正文归消息；未发送的剩余部分仍是草稿。
//   { type: "cancel" }             —— 一轮被取消：正文按未发送状态保留（不吞字）。
//   { type: "clear" }              —— 权威清空（快照说这份草稿没了：物化后清空、
//                                      用户清空归属、切到没有草稿的会话）。
export function draftLifecycle(state, event) {
  const base = normalizeDraftState(state);
  const type = event?.type;
  switch (type) {
    case "append":
      return withText(base, event.text);
    case "submit":
      return { ...base, text: String(event.remaining ?? base.text), phase: DRAFT_PHASE_SENDING };
    case "materialize":
      // 物化：这份会话不再是"草稿会话"，但用户后来敲进框里的字还没发出去。
      return settle({ ...base, attached: false });
    case "cancel":
      // 取消不回收正文：它本来就没发出去，按未发送状态留着。
      return settle(base);
    case "clear":
      return { ...base, text: "", phase: DRAFT_PHASE_IDLE };
    default:
      return base;
  }
}

// composerDraftRows 给出会话页要渲染的行：既定的 message + 本会话当前未发送的
// 草稿内容（末尾一条，标成 draft）。空草稿不出行——页面不会凭空多出一条空白行；
// 没有草稿归属（切到别的会话）也不会把上一个会话的字留在这里。
export function composerDraftRows({ conversation, state } = {}) {
  const messages = Array.isArray(conversation) ? conversation : [];
  const rows = messages.map(message => ({
    kind: MESSAGE_ROW_KIND,
    draft: false,
    id: message?.id ?? "",
    role: message?.role ?? "",
    content: message?.content ?? "",
    tool: message?.tool ?? null
  }));
  const current = normalizeDraftState(state);
  if (current.text.trim() === "") return rows;
  rows.push({
    kind: DRAFT_ROW_KIND,
    draft: true,
    sessionID: current.sessionID,
    text: current.text,
    // 还是"未发送"：sending 只表示这一轮在途，正文本身没被提交过。
    unsent: true
  });
  return rows;
}

// draftRoundEvent 把"一轮开始/结束"翻译成草稿事件：一轮完成（运行态收尾）=
// 物化提交的终点，草稿在后端已清空（clearComposerDraft），本地按 materialize
// 收敛；一轮被取消时正文按未发送保留，发 cancel。cancelled 由调用方给出（用户
// 点了停止/取消，壳层知道这次结束不是正常收尾），不猜。
export function draftRoundEvent({ wasRunning, isRunning, cancelled = false } = {}) {
  if (!wasRunning) return "";
  if (cancelled) return "cancel";
  if (!isRunning) return "materialize";
  return "";
}

// withText 把正文写进状态并同步 phase：正文为空 = 没有草稿可展示（idle），否则
// 是未发送（unsent）。**不打断在途轮次**——sending 期间继续敲字仍是"本轮在途"，
// 收尾（materialize/cancel）才回到 unsent/idle。
function withText(state, text) {
  const value = String(text ?? "");
  const phase = state.phase === DRAFT_PHASE_SENDING
    ? DRAFT_PHASE_SENDING
    : (value.trim() === "" ? DRAFT_PHASE_IDLE : DRAFT_PHASE_UNSENT);
  return { ...state, text: value, phase };
}

// settle 是一次轮次收尾（完成/取消）后的草稿归位：在途标记结束，剩下的本地正文
// 重新算作"未发送"。
function settle(state) {
  const value = String(state.text ?? "");
  return { ...state, text: value, phase: value.trim() === "" ? DRAFT_PHASE_IDLE : DRAFT_PHASE_UNSENT };
}

// normalizeDraftState 让缺省/半成品入参也得到完整状态（宿主首次渲染时只有快照）。
function normalizeDraftState(state) {
  if (!state || typeof state !== "object") {
    return { sessionID: "", attached: false, text: "", phase: DRAFT_PHASE_IDLE };
  }
  const text = String(state.text ?? "");
  const phase = [DRAFT_PHASE_IDLE, DRAFT_PHASE_UNSENT, DRAFT_PHASE_SENDING].includes(state.phase)
    ? state.phase
    : (text === "" ? DRAFT_PHASE_IDLE : DRAFT_PHASE_UNSENT);
  return {
    sessionID: String(state.sessionID ?? ""),
    attached: Boolean(state.attached),
    text,
    phase
  };
}
