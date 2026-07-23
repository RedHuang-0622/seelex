import { renderConversationModel } from "./components.js";

export function createChatView(elements, conversationView) {
  function renderConversation(messages, chat, scrollMode = "auto") {
    const active = messages.length > 0 || chat.running || (chat.input_queue || []).length > 0;
    elements["empty-state"].classList.toggle("hidden", active);
    conversationView.render(renderConversationModel(messages, chat), { scrollMode });
  }

  function renderControls(snapshot) {
    const running = Boolean(snapshot.chat?.running);
    const queued = Number(snapshot.chat?.queued_count || 0);
    elements["composer-status"].textContent = running ? queued ? `RUN · ${queued} QUEUED` : "RUN" : "READY";
    elements["send-button"].disabled = false;
    elements["send-button"].title = running ? "加入队列" : "发送";
    elements["send-button"].setAttribute("aria-label", running ? "加入队列" : "发送");
    elements.prompt.placeholder = running ? "继续输入，Enter 加入队列" : "描述任务，输入 / 查看命令，输入 # 加载 Skill";
    elements.composer.classList.toggle("is-running", running);
    elements["stop-button"].classList.toggle("hidden", !running);
    elements["connection-dot"].classList.add("online");
    elements["history-bar"].classList.toggle("hidden", !snapshot.has_more_history);
  }

  return {
    render(snapshot, scrollMode) {
      renderConversation(snapshot.conversation || [], snapshot.chat || {}, scrollMode);
      renderControls(snapshot);
    },
    renderConversation,
    renderControls
  };
}
