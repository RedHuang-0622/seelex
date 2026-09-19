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

// COMPOSER_STASH_LIMIT 是「输入框正文按会话留存」的上限（LRU）：正文只是未
// 发送的本地草稿，留最近用过的这些够用，不能让一张进程级表随会话数无限长。
const COMPOSER_STASH_LIMIT = 24;

// composerViewSwitch 决定「视图会话切换时输入框正文与脏位往哪走」——**输入框
// 正文按会话归属**：在 A 里写的未发送正文属于 A，不得跟着视图切到 B 并被当成
// B 的提交内容（`composerSubmitPlan` 只认「当前视图会话」，正文要是没跟着切，
// 一条会话运行中时写下的插话就会被发给另一个空闲会话）。
//
// 入参（都不被修改）：
//   fromSessionID / toSessionID：切换前 / 后的视图会话 ID；
//   current：切换前输入框里的正文（DOM 是正文的事实源）；
//   dirty：切换前是否「有本地未落盘输入」（挡住后端旧副本回填，见
//     shouldRestoreDraft）；
//   stash：Map<会话 ID, 未发送正文>，本地留存表（上一次调用的返回值）。
//
// 返回 { stash, text, dirty, switched }：
//   - ID 未变 / 目标为空 → 原样返回（switched=false，不动输入框）；
//   - 离开会话的正文按 ID 留存（空串不占位子；重复写入刷新 LRU 序）；
//   - 新会话有自己的留存 → 用它，并把脏位置真：那是本地内容，后端旧副本
//     （草稿正文）不得覆盖它；
//   - 新会话没有留存 → 正文置空、脏位**置假**：这条很关键，草稿会话的正文
//     随后由 restoreComposerDraft 回填，而 shouldRestoreDraft 要求「非脏、
//     未聚焦」——上一个会话遗留的脏位会把新会话自己的草稿正文挡在门外
//     （用户看到的是「切过去还是上一个会话的字」）。
export function composerViewSwitch({ fromSessionID, toSessionID, current, dirty = false, stash } = {}) {
  const source = String(fromSessionID ?? "");
  const target = String(toSessionID ?? "");
  const text = String(current ?? "");
  const next = trimComposerStash(new Map(stash instanceof Map ? stash : []));
  if (target === "" || target === source) return { stash: next, text, dirty: Boolean(dirty), switched: false };
  // 首次挂载（视图里还没有已知前置会话）：不动已经敲进去的内容，只把它记到
  // 该会话名下——启动竞态里宁可保留，也不能吞掉用户刚输入的字。
  if (source === "") {
    if (text !== "") next.set(target, text);
    return { stash: trimComposerStash(next), text, dirty: Boolean(dirty), switched: true };
  }
  // 离开的会话：正文按 ID 留存（空串不占位子；重写刷新 LRU 序 = 先删后插）。
  next.delete(source);
  if (text !== "") next.set(source, text);
  // 进入的会话：有本地留存就用它并置脏（本地内容，后端旧副本不得覆盖）；
  // 没有留存则清空正文、清脏位，把输入框交还给该会话自己（草稿正文由
  // shouldRestoreDraft 回填）。
  const restored = next.get(target);
  const hasLocal = typeof restored === "string" && restored !== "";
  return { stash: trimComposerStash(next), text: hasLocal ? restored : "", dirty: hasLocal, switched: true };
}

// trimComposerStash 把留存表收进上限（Map 迭代序 = 插入序，最旧在前）。
function trimComposerStash(stash) {
  while (stash.size > COMPOSER_STASH_LIMIT) stash.delete(stash.keys().next().value);
  return stash;
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
