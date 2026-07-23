import { escapeHtml, hydrateIcons, icon, renderSources } from "./components.js";
import { createChatView } from "./chat-view.js";
import { createGUIClient } from "./client-state.js";
import { createConversationView } from "./conversation-view.js";

const state = {
  info: null,
  commandTrigger: "/",
  commandSuggestions: [],
  commandSelected: 0,
  inlineSuggestions: [],
  inlineSelected: 0,
  inlineRequest: 0
};

const elements = Object.fromEntries([
  "app-title", "app-version", "connection-dot", "provider-label", "token-label",
  "session-list", "session-count", "new-session",
  "plugin-list", "plugin-count", "account-list", "account-count", "conversation",
  "empty-state", "composer", "prompt", "composer-status", "stop-button", "send-button",
  "runtime-details", "effort-switch", "plan-view", "skill-list", "history-bar",
  "project-name", "project-root", "project-status", "project-overview", "project-sources", "source-count",
  "runtime-button", "runtime-modal", "runtime-close", "inline-suggestions",
  "command-button", "command-modal", "command-close", "command-triggers", "command-search", "command-results",
  "load-history", "interaction-modal", "interaction-risk", "interaction-title",
  "interaction-question", "interaction-preview", "interaction-options", "toast"
].map(id => [id, document.getElementById(id)]));

const conversationView = createConversationView(elements.conversation, {
  copyText: value => navigator.clipboard.writeText(value),
  notify: showToast
});
const chatView = createChatView(elements, conversationView);
const client = createGUIClient({
  loadSnapshot: () => invoke("Snapshot"),
  onSnapshot: (snapshot, options) => render(snapshot, options),
  onIncremental: renderIncremental,
  onError: showToast
});

function bridge() {
  return window.go?.gui?.Bridge;
}

async function invoke(method, ...args) {
  const api = bridge();
  if (!api || typeof api[method] !== "function") {
    throw new Error("GUI bridge 尚未就绪");
  }
  return api[method](...args);
}

function showToast(error) {
  elements.toast.textContent = error?.message || String(error);
  elements.toast.classList.remove("hidden");
  window.clearTimeout(showToast.timer);
  showToast.timer = window.setTimeout(() => elements.toast.classList.add("hidden"), 4200);
}

async function refresh(options = {}) {
  return client.refresh(options);
}

function render(snapshot, options = {}) {
  renderSessions(snapshot.sessions || [], snapshot.session || {}, snapshot.capabilities || {});
  renderProject(snapshot);
  renderRuntime(snapshot.runtime || {});
  renderPlugins(snapshot.runtime || {});
  renderAccounts(snapshot.runtime || {});
  chatView.render(snapshot, options.scrollMode);
  renderPlan(snapshot.runtime?.plan);
  renderSkills(snapshot.runtime?.skills || []);
  renderInteraction(snapshot.interaction);

}

function renderIncremental(snapshot, kind) {
  if (!snapshot) return;
  if (["message.added", "message.delta", "tool.started", "tool.completed"].includes(kind)) {
    chatView.renderConversation(snapshot.conversation || [], snapshot.chat || {}, "auto");
    chatView.renderControls(snapshot);
    if (kind !== "message.delta") renderProject(snapshot);
    return;
  }
  if (kind === "runtime.changed") {
    renderRuntime(snapshot.runtime || {});
    renderPlugins(snapshot.runtime || {});
    renderAccounts(snapshot.runtime || {});
    renderPlan(snapshot.runtime?.plan);
    renderSkills(snapshot.runtime?.skills || []);
    renderProject(snapshot);
    return;
  }
  if (kind === "interaction.opened" || kind === "interaction.closed") renderInteraction(snapshot.interaction);
}

function renderProject(snapshot) {
  const project = state.info?.project || {};
  const runtime = snapshot.runtime || {};
  const running = Boolean(snapshot.chat?.running);
  const sources = project.sources || [];
  elements["project-name"].textContent = project.name || "当前工作区";
  elements["project-root"].textContent = project.root || "";
  elements["project-status"].innerHTML = [
    ["状态", running ? "Agent 执行中" : "Ready"],
    ["会话", shortSessionID(snapshot.session?.id || "—")],
    ["消息", String(snapshot.conversation?.length || 0)],
    ["资料源", String(sources.length)]
  ].map(([label, value]) => `<div class="status-item"><span>${escapeHtml(label)}</span><strong title="${escapeHtml(value)}">${escapeHtml(value)}</strong></div>`).join("");
  elements["project-overview"].textContent = running
    ? `当前任务正在执行，会话使用 ${runtime.plugin || "default"} 能力形态。`
    : `工作区已就绪。当前会话包含 ${snapshot.conversation?.length || 0} 条界面消息，可从左侧继续管理会话。`;
  elements["source-count"].textContent = String(sources.length);
  elements["project-sources"].innerHTML = renderSources(sources);
}

function renderSessions(sessions, current, capabilities) {
  const currentID = current.id || "";
  const items = [...sessions];
  if (currentID && !items.some(session => session.id === currentID)) {
    items.unshift({ id: currentID, current: true });
  }
  elements["session-count"].textContent = String(items.length);
  elements["session-list"].innerHTML = items.length
    ? items.map(session => {
      const active = session.id === currentID;
      const updated = session.updated_at
        ? new Date(session.updated_at).toLocaleString([], { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" })
        : "当前会话";
      const detail = session.token_count ? `${updated} · ${session.token_count} tokens` : updated;
      return `<button class="stack-button session-button ${active ? "active" : ""}" data-session="${escapeHtml(session.id)}">
        <span class="session-name">${icon("message", 13)} ${escapeHtml(shortSessionID(session.id))}</span><small>${escapeHtml(detail)}</small>
      </button>`;
    }).join("")
    : '<span class="muted list-empty">暂无会话</span>';

  elements["session-list"].querySelectorAll("button").forEach(button => {
    button.addEventListener("click", async () => {
      if (button.dataset.session === currentID) return;
      if (!capabilities.session_resume) {
        showToast(capabilities.session_resume_reason || "当前版本暂不支持恢复历史会话");
        return;
      }
      try { await invoke("Submit", `/resume ${button.dataset.session}`); await refresh({ scroll: "bottom" }); }
      catch (error) { showToast(error); }
    });
  });
}

function shortSessionID(id) {
  const value = String(id || "");
  return value.length > 18 ? `${value.slice(0, 8)}…${value.slice(-6)}` : value;
}

function renderRuntime(runtime) {
  elements["provider-label"].textContent = [runtime.provider, runtime.model].filter(Boolean).join(" · ") || "ready";
  elements["token-label"].textContent = runtime.tokens || "";
  elements["runtime-details"].innerHTML = [
    ["Model", runtime.model || "—"],
    ["Provider", runtime.provider || "—"],
    ["Plugin", runtime.plugin || "—"],
    ["Effort", runtime.effort || "—"],
    ["Prompt", runtime.prompt_stack || "—"],
    ["Tools", String(runtime.visible_tools?.length || 0)]
  ].map(([key, value]) => `<dt>${escapeHtml(key)}</dt><dd>${escapeHtml(value)}</dd>`).join("");

  const levels = ["lite", "medium", "high", "max"];
  elements["effort-switch"].innerHTML = levels.map(level =>
    `<button class="segment ${runtime.effort === level ? "active" : ""}" data-effort="${level}">${level}</button>`
  ).join("");
  elements["effort-switch"].querySelectorAll("button").forEach(button => {
    button.addEventListener("click", async () => {
      try { await invoke("SwitchEffort", button.dataset.effort); await refresh({ scroll: false }); }
      catch (error) { showToast(error); }
    });
  });
}

function renderPlugins(runtime) {
  const plugins = runtime.plugins || [];
  elements["plugin-count"].textContent = String(plugins.length);
  elements["plugin-list"].innerHTML = plugins.map(plugin => `
    <button class="stack-button ${runtime.plugin === plugin.name ? "active" : ""}" data-plugin="${escapeHtml(plugin.name)}">
      ${escapeHtml(plugin.name)}<small>${escapeHtml(plugin.description || "")}</small>
    </button>`).join("");
  elements["plugin-list"].querySelectorAll("button").forEach(button => {
    button.addEventListener("click", async () => {
      try { await invoke("SwitchPlugin", button.dataset.plugin); await refresh({ scroll: false }); }
      catch (error) { showToast(error); }
    });
  });
}

function renderAccounts(runtime) {
  const accounts = runtime.accounts || [];
  elements["account-count"].textContent = String(accounts.length);
  elements["account-list"].innerHTML = accounts.map(account => `
    <button class="stack-button ${runtime.account === account.name ? "active" : ""}" data-account="${escapeHtml(account.name)}" ${account.disabled ? "disabled" : ""}>
      ${escapeHtml(account.name)}<small>${escapeHtml(`${account.provider || ""} ${account.model || ""}`.trim())}</small>
    </button>`).join("");
  elements["account-list"].querySelectorAll("button").forEach(button => {
    button.addEventListener("click", async () => {
      try { await invoke("SelectAccount", button.dataset.account); await refresh({ scroll: false }); }
      catch (error) { showToast(error); }
    });
  });
}

function renderSkills(skills) {
  elements["skill-list"].innerHTML = skills.length
    ? skills.map(skill => `<span class="chip" title="${escapeHtml(skill.description || "")}">#${escapeHtml(skill.name)}</span>`).join("")
    : '<span class="muted">当前 Plugin 无 Skill</span>';
}

function renderPlan(plan) {
  if (!plan) {
    elements["plan-view"].className = "plan-view muted";
    elements["plan-view"].textContent = "暂无执行计划";
    return;
  }
  const nodes = flattenNodes(plan.nodes || []);
  elements["plan-view"].className = "plan-view";
  elements["plan-view"].innerHTML = `
    <div class="plan-header"><strong>${escapeHtml(plan.name || "Plan")}</strong><span>${Math.round((plan.progress || 0) * 100)}%</span></div>
    ${nodes.map(node => `<div class="plan-node ${escapeHtml(node.status || "")}" style="margin-left:${Math.min(node.depth || 0, 5) * 10}px">${escapeHtml(node.label || node.id || "node")}</div>`).join("")}`;
}

function flattenNodes(nodes, result = []) {
  for (const node of nodes) {
    result.push(node);
    flattenNodes(node.children || [], result);
  }
  return result;
}

function renderInteraction(interaction) {
  elements["interaction-modal"].classList.toggle("hidden", !interaction);
  if (!interaction) return;
  elements["interaction-risk"].textContent = interaction.risk || interaction.kind || "approval";
  elements["interaction-title"].textContent = interaction.title || "需要确认";
  elements["interaction-question"].textContent = interaction.question || interaction.tool_name || "是否继续？";
  elements["interaction-preview"].textContent = interaction.preview || "";
  elements["interaction-preview"].classList.toggle("hidden", !interaction.preview);
  elements["interaction-options"].innerHTML = (interaction.options || []).map(option =>
    `<button data-option="${escapeHtml(option.id)}" class="${escapeHtml(option.style || "")}">${escapeHtml(option.label)}</button>`
  ).join("");
  elements["interaction-options"].querySelectorAll("button").forEach(button => {
    button.addEventListener("click", async () => {
      try { await invoke("ResolveInteraction", interaction.id, button.dataset.option); await refresh({ scroll: false }); }
      catch (error) { showToast(error); }
    });
  });
}

function setModal(id, open) {
  elements[id].classList.toggle("hidden", !open);
}

function openRuntime() {
  setModal("runtime-modal", true);
}

function closeRuntime() {
  setModal("runtime-modal", false);
}

async function openCommandPalette(trigger = "/") {
  state.commandTrigger = ["/", "#", "@"].includes(trigger) ? trigger : "/";
  state.commandSelected = 0;
  elements["command-search"].value = state.commandTrigger;
  syncCommandTriggers();
  setModal("command-modal", true);
  await updateCommandResults();
  elements["command-search"].focus();
  elements["command-search"].setSelectionRange(1, 1);
}

function closeCommandPalette() {
  setModal("command-modal", false);
}

function syncCommandTriggers() {
  elements["command-triggers"].querySelectorAll("button").forEach(button => {
    button.classList.toggle("active", button.dataset.trigger === state.commandTrigger);
  });
}

async function updateCommandResults() {
  let input = elements["command-search"].value.trimStart();
  if (!["/", "#", "@"].includes(input[0])) {
    input = state.commandTrigger + input;
    elements["command-search"].value = input;
  } else {
    state.commandTrigger = input[0];
    syncCommandTriggers();
  }
  try {
    state.commandSuggestions = await invoke("Suggestions", input) || [];
    state.commandSelected = Math.min(state.commandSelected, Math.max(state.commandSuggestions.length - 1, 0));
    renderSuggestionList(elements["command-results"], state.commandSuggestions, state.commandSelected, state.commandTrigger);
  } catch (error) {
    showToast(error);
  }
}

function renderSuggestionList(container, suggestions, selected, trigger, limit = suggestions.length) {
  const visible = suggestions.slice(0, limit);
  container.innerHTML = visible.length
    ? visible.map((suggestion, index) => `<button class="command-result ${index === selected ? "selected" : ""}" type="button" data-index="${index}">
      <span class="command-result-icon">${icon(suggestionIcon(suggestion.kind), 14)}</span>
      <span class="command-prefix">${escapeHtml(trigger)}${escapeHtml(suggestion.text)}</span>
      <span class="command-description">${escapeHtml(suggestion.description || "")}</span>
      <span class="command-kind">${escapeHtml(suggestion.kind || "command")}</span>
    </button>`).join("")
    : '<span class="muted list-empty">没有匹配的指令</span>';
  container.querySelectorAll("button").forEach(button => {
    button.addEventListener("click", () => acceptSuggestion(visible[Number(button.dataset.index)], trigger));
  });
}

function suggestionIcon(kind) {
  return ({ skill: "skill", plugin: "plugin", tool: "terminal", command: "command" })[kind] || "command";
}

function acceptSuggestion(suggestion, trigger) {
  if (!suggestion) return;
  elements.prompt.value = `${trigger}${suggestion.text} `;
  resizePrompt();
  closeCommandPalette();
  hideInlineSuggestions();
  elements.prompt.focus();
  elements.prompt.setSelectionRange(elements.prompt.value.length, elements.prompt.value.length);
}

async function updateInlineSuggestions() {
  const input = elements.prompt.value.trimStart();
  if (!/^[\/#@][^\s]*$/.test(input)) {
    hideInlineSuggestions();
    return;
  }
  const request = ++state.inlineRequest;
  try {
    const suggestions = await invoke("Suggestions", input) || [];
    if (request !== state.inlineRequest) return;
    state.inlineSuggestions = suggestions.slice(0, 8);
    state.inlineSelected = Math.min(state.inlineSelected, Math.max(state.inlineSuggestions.length - 1, 0));
    elements["inline-suggestions"].classList.toggle("hidden", state.inlineSuggestions.length === 0);
    renderSuggestionList(elements["inline-suggestions"], state.inlineSuggestions, state.inlineSelected, input[0], 8);
  } catch (error) {
    hideInlineSuggestions();
    showToast(error);
  }
}

function hideInlineSuggestions() {
  state.inlineRequest++;
  state.inlineSuggestions = [];
  state.inlineSelected = 0;
  elements["inline-suggestions"].classList.add("hidden");
}

elements.composer.addEventListener("submit", async event => {
  event.preventDefault();
  const text = elements.prompt.value.trim();
  if (!text) return;
  try {
    await invoke("Submit", text);
    elements.prompt.value = "";
    hideInlineSuggestions();
    resizePrompt();
    await refresh({ scroll: "bottom" });
  } catch (error) { showToast(error); }
});

elements.prompt.addEventListener("keydown", event => {
  if (!elements["inline-suggestions"].classList.contains("hidden") && state.inlineSuggestions.length) {
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const direction = event.key === "ArrowDown" ? 1 : -1;
      state.inlineSelected = (state.inlineSelected + direction + state.inlineSuggestions.length) % state.inlineSuggestions.length;
      renderSuggestionList(elements["inline-suggestions"], state.inlineSuggestions, state.inlineSelected, elements.prompt.value.trimStart()[0], 8);
      return;
    }
    if (event.key === "Tab") {
      event.preventDefault();
      acceptSuggestion(state.inlineSuggestions[state.inlineSelected], elements.prompt.value.trimStart()[0]);
      return;
    }
    if (event.key === "Escape") {
      event.preventDefault();
      hideInlineSuggestions();
      return;
    }
  }
  if (event.key === "Enter" && !event.shiftKey) {
    event.preventDefault();
    elements.composer.requestSubmit();
  }
});
elements.prompt.addEventListener("input", () => {
  resizePrompt();
  state.inlineSelected = 0;
  updateInlineSuggestions();
});

elements["stop-button"].addEventListener("click", async () => {
  const requestID = client.current()?.chat?.request_id || "";
  try { await invoke("CancelChat", requestID); await refresh({ scroll: false }); }
  catch (error) { showToast(error); }
});

elements["load-history"].addEventListener("click", async () => {
  try { await invoke("LoadMoreHistory", 50); await refresh({ scroll: "anchor" }); }
  catch (error) { showToast(error); }
});

elements["new-session"].addEventListener("click", async () => {
  try { await invoke("Submit", "/new"); await refresh({ scroll: "bottom" }); }
  catch (error) { showToast(error); }
});

elements["runtime-button"].addEventListener("click", openRuntime);
elements["runtime-close"].addEventListener("click", closeRuntime);
elements["command-button"].addEventListener("click", () => openCommandPalette("/"));
elements["command-close"].addEventListener("click", closeCommandPalette);

elements["command-triggers"].querySelectorAll("button").forEach(button => {
  button.addEventListener("click", () => openCommandPalette(button.dataset.trigger));
});

elements["command-search"].addEventListener("input", () => {
  state.commandSelected = 0;
  updateCommandResults();
});

elements["command-search"].addEventListener("keydown", event => {
  if (event.key === "Escape") {
    event.preventDefault();
    closeCommandPalette();
    elements.prompt.focus();
    return;
  }
  if ((event.key === "ArrowDown" || event.key === "ArrowUp") && state.commandSuggestions.length) {
    event.preventDefault();
    const direction = event.key === "ArrowDown" ? 1 : -1;
    state.commandSelected = (state.commandSelected + direction + state.commandSuggestions.length) % state.commandSuggestions.length;
    renderSuggestionList(elements["command-results"], state.commandSuggestions, state.commandSelected, state.commandTrigger);
    elements["command-results"].querySelector(".selected")?.scrollIntoView({ block: "nearest" });
    return;
  }
  if ((event.key === "Enter" || event.key === "Tab") && state.commandSuggestions.length) {
    event.preventDefault();
    acceptSuggestion(state.commandSuggestions[state.commandSelected], state.commandTrigger);
  }
});

for (const [modalID, close] of [["runtime-modal", closeRuntime], ["command-modal", closeCommandPalette]]) {
  elements[modalID].addEventListener("click", event => {
    if (event.target === elements[modalID]) close();
  });
}

document.addEventListener("keydown", event => {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
    event.preventDefault();
    openCommandPalette("/");
  }
  if (event.key === "Escape") {
    closeRuntime();
    closeCommandPalette();
  }
});

function resizePrompt() {
  elements.prompt.style.height = "auto";
  elements.prompt.style.height = `${Math.min(elements.prompt.scrollHeight, 180)}px`;
}

async function initialise() {
  try {
    hydrateIcons();
    const info = await invoke("Info");
    state.info = info;
    elements["app-title"].textContent = info.title || "Seelex";
    elements["app-version"].textContent = info.version || "dev";
    await refresh({ scroll: "bottom" });
    if (window.runtime?.EventsOn) {
      window.runtime.EventsOn("seelex:event", event => client.handleEvent(event));
      window.runtime.EventsOn("seelex:ready", snapshot => {
        try { client.acceptSnapshot(snapshot, "bottom"); }
        catch (error) { showToast(error); }
      });
    }
  } catch (error) {
    showToast(error);
    window.setTimeout(initialise, 600);
  }
}

initialise();
