// ── 输入框（composer）编辑规则 ──────────────────────────────
// app.js 是壳层的 composition/orchestration：它负责把 DOM 事件接到这些纯函数
// 上，规则本身住在这里，可被 node --test 直接覆盖。
//
// 存在理由（三个"吞输入"的坑 + 一个"投错会话"的坑，规则集中一处避免各处各写
// 一遍）：
//  1) 提交是异步 RPC：`Submit` 往返期间用户还在打字，回来后整框清空会把这段
//     新输入一起吞掉 → clearSubmittedText 只移除已发送的那段；
//  2) 草稿会话的整份快照会把后端草稿正文回填输入框：后端正文比本地输入旧时
//     回填会覆盖用户刚敲的字 → shouldRestoreDraft 拒绝覆盖未落盘的本地输入；
//  3) 中文输入法确认候选词的 Enter 与"要发送的 Enter"要分开：WebView2 偶发
//     不在该次 Enter 上带 isComposing，需要 composition 跟踪 + keyCode 229
//     兜底 → isComposingEnter；
//  4) 提交路由不能重读后端"当前会话"：一个会话运行中、用户切到另一个空闲会话
//     发消息时，ambient `Submit` 走的是后端视图指针（切换 TOCTOU 窗口里可能仍
//     是运行中的那个会话），输入就被投进运行中会话的队列
//     （application/core/session_scope.go `SubmitToSession` 注释：
//     TestStressConcurrentSessionsDoNotPollute 抓到 queued-2 进 sess-4 视图）
//     → composerSubmitPlan 把"发到哪个会话"显式钉死为**当前视图会话**。
//
// 会话级 API 的口径见 docs/gui/modules/multi-session-pages.md §6：
// 「旧 Submit/Cancel/Resolve/Snapshot 在迁移期委托 active session；新前端
// 一律使用显式 session ID」。

// clearSubmittedText 返回"提交成功后输入框应剩什么"：只移除本次已发送的那段
// 原文。提交往返期间用户可能已继续输入（追加在末尾），此时保留追加部分；
// 若用户中途把内容改成与提交原文不同（整段改写/清空后另写），则不动它——
// 宁可留下已发送的原文，也不能吞掉用户的新输入。
export function clearSubmittedText(current, sent) {
  const value = String(current ?? "");
  const submitted = String(sent ?? "");
  if (submitted === "") return value;
  if (value === submitted) return "";
  if (value.startsWith(submitted)) return value.slice(submitted.length);
  return value;
}

// SIGIL_PREFIXES 是后端输入路由器（input_router）拥有的前缀：/ 命令、# 插件、
// $ 技能、@ 团队。命中即由路由器按"当前视图会话"分派，前端不复制这套映射。
const SIGIL_PREFIXES = ["/", "#", "$", "@"];

// isSigilInput 判定输入是否交给后端路由器（前缀与 router 的 `HasPrefix` 同一
// 判据：只看首字符，不管后面有没有内容——`#` 空名在 router 里是 no-op，
// `@` 空名有语义，都不该被当成普通对话发出去）。
export function isSigilInput(text) {
  const value = String(text ?? "").trimStart();
  return value !== "" && SIGIL_PREFIXES.includes(value[0]);
}

// composerSubmitPlan 决定一次 composer 提交走哪条 RPC：
//   - sigil 输入（/ # $ @）→ `Submit`：前缀→用例的映射是后端路由器的职责；
//     它按当前视图会话分派，切换在途的窗口由输入区锁覆盖（chat-view.js）。
//   - 普通对话输入 → `SubmitToSession` + 显式 viewedSessionID：**绝不重读后端
//     "当前会话"**。运行中会话不会再把本该发给空闲会话的输入吸进自己的队列。
//   - 拿不到视图会话 ID（快照未就绪）→ 退回 `Submit`（宁可走老路，也不能把
//     输入投到一个空 ID 上）。
// 返回 { rpc, args }（args 直接展开给 invoke）。
export function composerSubmitPlan({ text, viewedSessionID }) {
  const value = String(text ?? "");
  const sessionID = String(viewedSessionID ?? "").trim();
  if (isSigilInput(value) || sessionID === "") return { rpc: "Submit", args: [value] };
  return { rpc: "SubmitToSession", args: [sessionID, value] };
}


// shouldRestoreDraft 判定整份快照渲染时要不要把后端草稿正文回填输入框。
//   - 非草稿会话 / 后端没有草稿正文 → 不回填（无草稿归属）；
//   - 输入框正聚焦 → 不回填（用户正在编辑，绝不动它）；
//   - 有未落盘的本地输入（dirty）→ 不回填（后端正文可能是更旧的副本，回填
//     等于吞掉用户刚敲的字）；
//   - 本地内容已等于后端正文 → 无需回填。
export function shouldRestoreDraft({ draft, snapshotComposer, current, focused, dirty }) {
  if (!draft || !snapshotComposer) return false;
  if (focused) return false;
  if (dirty) return false;
  return String(current ?? "") !== String(snapshotComposer);
}

// isComposingEnter 判定这次 Enter 是否属于输入法合成（确认候选词），不能当
// 发送。三重判据：本地 composition 跟踪（compositionstart/end）、事件自带的
// isComposing、以及 IME 处理标记 keyCode 229（WebView2 上偶发 isComposing
// 缺失时仍能拦住）。
export function isComposingEnter(event, composing = false) {
  if (composing) return true;
  if (!event) return false;
  if (event.isComposing) return true;
  return event.keyCode === 229;
}
