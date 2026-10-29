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
