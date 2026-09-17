import { renderConversationModel } from "./components.js";
import { historyWindowed } from "./protocol.js";

export function createChatView(elements, conversationView) {
  // restoring：后端异步冷加载期间视图已切到目标空壳，对话区不渲染“暂无
  // 消息”空态，也不把用户当成就绪可发送（renderControls 处理输入区）。
  // switching：本端已发起切换、后端视图指针已动、但渲染层还没拿到目标快照的
  // 窗口——此时提交会落到看不见/没装载完的会话，输入区同样必须锁上（见
  // docs/devlog/2026-09-17-submit-during-cold-restore-repro.md）。两者都只
  // 影响呈现；权威判据在应用层（restoring 期间拒绝输入，ErrSessionRestoring）。
  function renderConversation(messages, chat, scrollMode = "auto", hasMoreHistory = false, restoring = false, switching = false) {
    const active = messages.length > 0 || chat.running || (chat.input_queue || []).length > 0 || restoring || switching;
    elements["empty-state"].classList.toggle("hidden", active);
    conversationView.render(renderConversationModel(messages, chat), { scrollMode, hasMoreHistory });
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
    render(snapshot, scrollMode, switching = false) {
      renderConversation(
        snapshot.conversation || [],
        snapshot.chat || {},
        scrollMode,
        snapshot.has_more_history,
        snapshot.session?.status === "restoring",
        switching
      );
      renderControls(snapshot, switching);
    },
    renderConversation,
    renderControls
  };
}
