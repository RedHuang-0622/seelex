import { renderConversationModel } from "./components.js";
import { historyWindowed } from "./protocol.js";

export function createChatView(elements, conversationView) {
  function renderConversation(messages, chat, scrollMode = "auto", hasMoreHistory = false, restoring = false) {
    // restoring：后端异步冷加载期间视图已切到目标空壳，对话区不渲染“暂无
    // 消息”空态，也不把用户当成就绪可发送（renderControls 处理输入区）。
    const active = messages.length > 0 || chat.running || (chat.input_queue || []).length > 0 || restoring;
    elements["empty-state"].classList.toggle("hidden", active);
    conversationView.render(renderConversationModel(messages, chat), { scrollMode, hasMoreHistory });
  }

  function renderControls(snapshot) {
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
    const running = Boolean(snapshot.chat?.running);
    const queued = Number(snapshot.chat?.queued_count || 0);
    elements["composer-status"].textContent = running ? queued ? `执行中 · ${queued} 排队` : "执行中" : "就绪";
    elements["send-button"].disabled = false;
    elements.prompt.disabled = false;
    elements["send-button"].title = running ? "加入队列" : "发送";
    elements["send-button"].setAttribute("aria-label", running ? "加入队列" : "发送");
    elements.prompt.placeholder = running ? "继续输入，Enter 加入队列" : "描述任务，输入 / 查看命令，输入 # 加载 Skill";
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
    render(snapshot, scrollMode) {
      renderConversation(
        snapshot.conversation || [],
        snapshot.chat || {},
        scrollMode,
        snapshot.has_more_history,
        snapshot.session?.status === "restoring"
      );
      renderControls(snapshot);
    },
    renderConversation,
    renderControls
  };
}
