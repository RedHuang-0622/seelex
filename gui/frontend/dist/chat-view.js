import { renderConversationModel, renderMessageQueue } from "./components.js";
import { conversationCompactionAnchor } from "./compaction-format.js";
import { historyWindowed } from "./protocol.js";

export function createChatView(elements, conversationView) {
  // restoring：后端异步冷加载期间视图已切到目标空壳，对话区不渲染“暂无
  // 消息”空态，也不把用户当成就绪可发送（renderControls 处理输入区）。
  // switching：本端已发起切换、后端视图指针已动、但渲染层还没拿到目标快照的
  // 窗口——此时提交会落到看不见/没装载完的会话，输入区同样必须锁上（见
  // docs/devlog/2026-09-17-submit-during-cold-restore-repro.md）。两者都只
  // 影响呈现；权威判据在应用层（restoring 期间拒绝输入，ErrSessionRestoring）。
  // draft：本页未发送的草稿正文（页面 context 的第二半，判据在 draft-lifecycle.js
  // composerDraftRows）。有草稿时这一页就不是空态——草稿会话此前因此显示成空页。
  // compactions：会话的压缩记录（Snapshot.Task.ContextCompactions）。对话区只从
  // 它派生**一条**分界（会话单例，落点与文案由 conversationCompactionAnchor 判定），
  // 插在最新被折出的那条消息之后；记录逐条可查的地方是右栏「上下文压缩」与轨迹
  // 「压缩」轨。
  function renderConversation(messages, chat, scrollMode = "auto", hasMoreHistory = false, restoring = false, switching = false, draft = "", compactions = []) {
    const draftText = String(draft ?? "");
    const active = messages.length > 0 || draftText.trim() !== "" || chat.running || (chat.input_queue || []).length > 0 || restoring || switching;
    elements["empty-state"].classList.toggle("hidden", active);
    conversationView.render(renderConversationModel(messages, chat, draftText, conversationCompactionAnchor(messages, compactions)), { scrollMode, hasMoreHistory });
    renderMessageQueueHost(chat);
  }

  // renderMessageQueueHost 把排队条画到输入框正上方的 #message-queue（见
  // index.html 的宿主与 styles.css 的 .message-queue-host）。队列**不进对话流**：
  // 它是"还没发出去的那几条"，必须跟着输入框走，留在滚动区里会被对话内容推走、
  // 也随滚动跑掉（用户口径：与输入框间隔太宽，要贴着输入框、像抽屉一样叠着）。
  // --queue-h 写回给对话区底部留白：那一叠是绝对定位的浮层，滚动区得自己让出
  // 它的高度，否则对话尾部会被压在叠下面；空队列写 0。
  function renderMessageQueueHost(chat = {}) {
    const host = elements["message-queue"];
    if (!host) return;
    const html = renderMessageQueue(chat);
    if (host.innerHTML !== html) host.innerHTML = html;
    document.documentElement.style.setProperty("--queue-h", `${host.offsetHeight}px`);
  }

  // renderControls 是输入区锁的唯一落点：restoring（目标会话恢复中）= 整块
  // composer 只读壳；switching（切换在途）= 只锁输入，不动停止/历史栏（切换
  // 窗口里当前会话的停止按钮与历史回看必须继续可用）。
  function renderControls(snapshot, switching = false) {
    const restoring = snapshot.session?.status === "restoring";
    if (restoring) {
      elements["composer-status"].textContent = "正在恢复会话…";
      elements["send-button"].disabled = true;
      elements["send-button"].title = "会话恢复中";
      elements["send-button"].setAttribute("aria-label", "会话恢复中");
      elements.prompt.disabled = true;
      elements.prompt.placeholder = "会话内容恢复中…";
      elements.composer.classList.remove("is-running");
      elements["stop-button"].classList.add("hidden");
      elements["connection-dot"].classList.add("online");
      elements["history-bar"].classList.toggle("hidden", true);
      return;
    }
    const inputLocked = switching;
    const running = Boolean(snapshot.chat?.running);
    const queued = Number(snapshot.chat?.queued_count || 0);
    elements["composer-status"].textContent = inputLocked
      ? "正在切换会话…"
      : running ? queued ? `执行中 · ${queued} 排队` : "执行中" : "就绪";
    elements["send-button"].disabled = inputLocked;
    elements.prompt.disabled = inputLocked;
    elements["send-button"].title = inputLocked ? "正在切换会话" : running ? "加入队列" : "发送";
    elements["send-button"].setAttribute("aria-label", elements["send-button"].title);
    elements.prompt.placeholder = inputLocked
      ? "正在切换会话…"
      : running ? "继续输入，Enter 加入队列" : "描述任务，输入 / 查看命令，# 切换插件，$ 召回 Skill，@ 手动召唤团队";
    elements.composer.classList.toggle("is-running", running);
    elements["stop-button"].classList.toggle("hidden", !running);
    elements["connection-dot"].classList.add("online");
    // 历史栏：还有更早历史 / 正在回看更早历史（下方有更新的内容）时都常驻，
    // 用户因此既能继续向上翻，也能一键回到最新。
    const windowed = historyWindowed(snapshot);
    elements["history-bar"].classList.toggle("hidden", !snapshot.has_more_history && !windowed);
    elements["load-history"].classList.toggle("hidden", !snapshot.has_more_history);
    elements["latest-history"].classList.toggle("hidden", !windowed);
  }

  return {
    render(snapshot, scrollMode, switching = false, draft = "") {
      renderConversation(
        snapshot.conversation || [],
        snapshot.chat || {},
        scrollMode,
        snapshot.has_more_history,
        snapshot.session?.status === "restoring",
        switching,
        draft,
        snapshot.task?.context_compactions || []
      );
      renderControls(snapshot, switching);
    },
    renderConversation,
    renderControls
  };
}
