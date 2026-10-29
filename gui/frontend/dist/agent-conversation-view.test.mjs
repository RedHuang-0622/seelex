import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import {
  agentConversationKey,
  agentConversationMessages,
  normalizeAgentConversationRef,
  renderAgentConversationPanel
} from "./agent-conversation-view.js";

const here = path.dirname(fileURLToPath(import.meta.url));

function read(name) {
  return fs.readFileSync(path.join(here, name), "utf8");
}

const SUBAGENT_REF = {
  kind: "subagent",
  session_id: "node-freeze",
  main_session_id: "session-main"
};

test("agent conversation refs are fixed windows and use stable keys", () => {
  assert.deepEqual(normalizeAgentConversationRef(SUBAGENT_REF), {
    kind: "subagent",
    session_id: "node-freeze",
    main_session_id: "session-main",
    team_id: "",
    role: "",
    work_item_id: ""
  });
  assert.notEqual(
    agentConversationKey(SUBAGENT_REF),
    agentConversationKey({ ...SUBAGENT_REF, session_id: "node-other" })
  );
  assert.match(agentConversationKey({
    kind: "teammate",
    session_id: "wi-session",
    main_session_id: "session-main",
    work_item_id: "wi-1"
  }), /teammate.*wi-1/);
});

test("agent records use the main conversation model, including paired tools", () => {
  const messages = agentConversationMessages({
    records: [
      { id: "m1", kind: "system", content: "internal prompt must stay out of chat" },
      { id: "m2", kind: "user", role: "user", content: "检查这个模块" },
      { id: "m3", kind: "assistant", role: "assistant", content: "先读取文件。" },
      {
        id: "m4", kind: "tool_call", role: "assistant",
        tool_name: "read_file", tool_call_id: "call-1",
        content: "{\"path\":\"README.md\"}", status: "requested"
      },
      {
        id: "m5", kind: "tool_result", role: "tool",
        tool_name: "read_file", tool_call_id: "call-1",
        content: "README body", status: "success"
      }
    ]
  });
  assert.equal(messages.some(message => String(message.content).includes("internal prompt")), false);
  assert.equal(messages.find(message => message.id === "m2").role, "user");
  assert.equal(messages.find(message => message.id === "m4").tool.id, "call-1");
  assert.equal(messages.find(message => message.id === "m5").role, "tool_result");
});

test("one shared panel renders the conversation body and a per-session composer", () => {
  const html = renderAgentConversationPanel({
    status: "live",
    recovery: "live",
    total: 2,
    records: [
      { id: "m1", kind: "user", role: "user", content: "继续完成目标" },
      { id: "m2", kind: "assistant", role: "assistant", content: "正在继续。" }
    ]
  }, { ref: SUBAGENT_REF });
  assert.match(html, /class="conversation agent-conversation-stream"/);
  assert.match(html, /继续完成目标/);
  assert.match(html, /正在继续/);
  assert.match(html, /data-agent-chat-form/);
  assert.match(html, /data-agent-kind="subagent"/);
  assert.match(html, /data-agent-session-id="node-freeze"/);
  assert.match(html, /消息只发送到这个 subagent 会话/);
  assert.match(html, /class="composer agent-conversation-composer"/);
  assert.match(renderAgentConversationPanel(null, { ref: SUBAGENT_REF, loading: true }), /conversation-switch-loading/);
});

test("main and child composers come from one template rather than copied markup", () => {
  const index = read("index.html");
  const app = read("app.js");
  assert.match(index, /<template id="composer-template">/);
  assert.match(index, /<form id="composer" class="composer"><\/form>/);
  assert.match(app, /mountComposerTemplate\(document\.getElementById\("composer"\)\)/);
  assert.match(app, /mountAgentConversationPanel/);
});

test("Work Item and subagent details mount the shared conversation view and motion", () => {
  const conversation = read("agent-conversation-view.js");
  const team = read("team-board-view.js");
  const plan = read("plan-dsl.js");
  const app = read("app.js");
  assert.match(team, /renderAgentConversationPanel/);
  assert.match(plan, /data-agent-conversation-embed/);
  assert.match(app, /mountActiveNodeAgentConversationPanel/);
  assert.match(app, /invoke\("ConversationChat"/);
  assert.match(conversation, /createConversationView/);
  assert.match(conversation, /refreshAgentConversationPanel/);
  assert.match(conversation, /renderConversationLoading\("restoring"\)/);
  assert.doesNotMatch(team, /team-execution-records/);
});

test("空 page 不得抛 null 读取：teammate 首次打开「当前会话」时 page 就是 null", () => {
  // 回归（2026-10-29 用户实测）：默认参数只兜 undefined、不兜显式 null，而
  // mountAgentConversationPanel 与 controller.render 都显式传 null，于是首次打开
  // teammate 的会话弹窗抛 "Cannot read properties of null (reading 'records')"。
  assert.deepEqual(agentConversationMessages(null), []);
  assert.deepEqual(agentConversationMessages(), []);
  assert.deepEqual(agentConversationMessages({}), []);
  // 有记录时仍照常映射（role 才是消息判据，kind 只决定工具/系统行）。
  const mapped = agentConversationMessages({
    records: [{ id: "r1", role: "user", content: "hi" }]
  });
  assert.equal(mapped.length, 1);
  assert.equal(mapped[0].role, "user");
});

test("乐观回显：pending 正文立刻出现在面板里，且空记录时不算空态", () => {
  // 回归（2026-10-29 用户实测「发消息他不收、没有及时反馈」）：按下发送到后端写下这条
  // 记录之间可能有几十秒，这段时间必须让用户看见自己那句话已经出去了。
  const ref = { kind: "subagent", session_id: "node-x", main_session_id: "main" };
  const blank = renderAgentConversationPanel(null, { ref, pending: "看下这个报错" });
  assert.match(blank, /看下这个报错/);
  // 有 pending 时不能落到 loading 占位（首屏还在读记录就要能看见自己刚发的那句）。
  assert.doesNotMatch(blank, /conversation-switch-loading/);
  // 没有 pending、page 还是 null → 仍是 loading 占位。
  assert.match(renderAgentConversationPanel(null, { ref }), /conversation-switch-loading/);
  // 记录为空（page 已到但没记录）且无 pending → 空态文案。
  assert.match(renderAgentConversationPanel({ records: [] }, { ref }), /还没有对话记录/);
});

test("agent 输入框必须真的绑上提交：发送键是 type=submit，没有监听就什么都不发生", () => {
  const app = read("app.js");
  // submitAgentConversation 必须既有定义、也被委托调用（2026-10-29 之前它是死代码：
  // 全仓只出现一次，就是定义本身，于是按下发送只剩浏览器原生提交）。
  const occurrences = (app.match(/submitAgentConversation/g) || []).length;
  assert.ok(occurrences >= 2, `submitAgentConversation 必须有调用点，实际出现 ${occurrences} 次`);
  assert.match(app, /addEventListener\("submit"[\s\S]{0,200}\[data-agent-chat-form\]/);
  // Enter 必须能发送（否则 textarea 只会换行），且组字中的 Enter 不算发送。
  assert.match(app, /addEventListener\("keydown"[\s\S]{0,300}\[data-agent-chat-form\]/);
  assert.match(app, /isComposingEnter\(event, composerComposing\)/);
  // 反馈链路：状态行要说出"发送中/已发出"，且超时后进轮询而不是干等。
  assert.match(app, /state\.sending \? "发送中…"/);
  assert.match(app, /state\.awaiting \? "已发出，等待回复…"/);
  assert.match(app, /startAgentConversationPoll\(state\)/);
});
