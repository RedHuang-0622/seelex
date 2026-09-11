import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const embedURL = `data:text/javascript;base64,${Buffer.from(await readFile(new URL("./html-embed.js", import.meta.url), "utf8")).toString("base64")}`;
const markdownSource = (await readFile(new URL("./markdown.js", import.meta.url), "utf8"))
  .replace('"./html-embed.js"', `"${embedURL}"`);
const markdownURL = `data:text/javascript;base64,${Buffer.from(markdownSource).toString("base64")}`;
const componentSource = (await readFile(new URL("./components.js", import.meta.url), "utf8"))
  .replace('"./markdown.js"', `"${markdownURL}"`);
const { renderChatActivity, renderConversationComponent, renderConversationModel } = await import(`data:text/javascript;base64,${Buffer.from(componentSource).toString("base64")}`);

test("renders runtime activity only from active chat state", () => {
  assert.equal(renderChatActivity({ running: false }), "");
  const html = renderChatActivity({ running: true });
  assert.match(html, /class="runtime-activity"/);
  assert.match(html, /执行中/);
});

test("renders queued inputs as safe markdown cards", () => {
  const html = renderChatActivity({
    running: true,
    input_queue: ["**follow up**", "<script>alert(1)</script>"]
  });

  assert.match(html, /排队 01/);
  assert.match(html, /<strong>follow up<\/strong>/);
  assert.match(html, /排队 02/);
  assert.doesNotMatch(html, /<script>/);
  assert.match(html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
});

test("appends activity after conversation without changing tool payloads", () => {
  const rendered = renderConversationComponent(
    [{ role: "assistant", content: "answer" }],
    { running: true, input_queue: ["next"] }
  );

  assert.match(rendered.html, /class="message assistant"[\s\S]*runtime-activity[\s\S]*排队 01/);
  assert.equal(rendered.payloads.size, 0);
});

test("uses stable message and tool keys for incremental rendering", () => {
  const model = renderConversationModel([
    { id: "assistant-1", role: "assistant", content: "answer" },
    { id: "tool-start", role: "tool", tool: { id: "call-1", name: "read", arguments: "{}" } },
    { id: "tool-end", role: "tool_result", tool: { id: "call-1", name: "read", result: "done" } }
  ], { running: true });

  assert.deepEqual(model.items.map(item => item.key), ["message:assistant-1", "roll:tool:call-1", "chat:activity"]);
  assert.match(model.items[1].html, /class="conversation-axis is-tools"/);
  assert.match(model.items[1].html, /data-conversation-key="tool:call-1"/);
  assert.match(model.items[1].html, /data-trajectory-key="tool:call-1"/);
  assert.match(model.items[1].html, /工具过程/);
});

test("renders tool calls as one-line chips without IN/OUT panels", () => {
  const model = renderConversationModel([
    { id: "tool-start", role: "tool", tool: { id: "call-big", name: "bash", arguments: "{}" } },
    { id: "tool-end", role: "tool_result", tool: {
      id: "call-big", name: "bash", result: "preview",
      result_ref: "tr-abc123", truncated: true, total_chars: 9000
    } }
  ]);

  const item = model.items.find(entry => entry.html.includes('data-conversation-key="tool:call-big"'));
  assert.ok(item, "tool item must be present");
  assert.match(item.html, /class="chat-chip is-tool"/);
  assert.match(item.html, /data-trajectory-key="tool:call-big"/);
  assert.match(item.html, /8\.8 KB/);
  assert.doesNotMatch(item.html, /data-load-ref/);
  assert.doesNotMatch(item.html, /io-collapse/);
  assert.doesNotMatch(item.html, /io-panel/);
  assert.match(item.html, /class="item-id">call-big</);
});

test("keeps chat tool rows to a single line with status", () => {
  const model = renderConversationModel([
    { id: "tool-start", role: "tool", tool: { id: "call-small", name: "read_file", arguments: "{}" } },
    { id: "tool-end", role: "tool_result", tool: { id: "call-small", name: "read_file", result: "small" } }
  ]);
  const item = model.items.find(entry => entry.html.includes('data-conversation-key="tool:call-small"'));
  assert.ok(item);
  assert.match(item.html, /class="tool-state">.*OK/);
  assert.doesNotMatch(item.html, /data-copy/);
  assert.doesNotMatch(item.html, /io-collapse/);
});

test("keeps restored tool steps in message order instead of piling tools then replies", () => {
  // 恢复路径（sessionstore 派生 conversation）产出的事实形状：助手步骤只有
  // 思考，工具调用与结果各自成条，回合末尾才是正文。
  const model = renderConversationModel([
    { id: "message-1", role: "user", content: "长会话问题" },
    { id: "seq-2", role: "assistant", reasoning_content: "先读文件" },
    { id: "seq-2#tool-1", role: "tool", tool: { id: "call-1", name: "read", arguments: "{}", status: "success" } },
    { id: "message-3", role: "tool_result", content: "package a", tool: { id: "call-1", name: "read", result: "package a", status: "success" } },
    { id: "message-4", role: "assistant", content: "结论" }
  ]);

  // 顺序 = 问题 → 助手步骤（含思考）→ 这一步的工具过程 → 正文。
  assert.deepEqual(model.items.map(item => item.meta.kind), ["message", "message", "axis", "message"]);
  assert.match(model.items[1].html, /先读文件/);
  assert.match(model.items[2].html, /工具过程/);
  assert.match(model.items[2].html, /call-1/);
  assert.match(model.items[3].html, /结论/);
  // 工具过程夹在步骤与正文之间，而不是被堆到整段对话末尾。
  const html = model.items.map(item => item.html).join("");
  assert.ok(html.indexOf("先读文件") < html.indexOf("工具过程"));
  assert.ok(html.indexOf("工具过程") < html.indexOf("结论"));
});

test("renders thinking in its own scroll axis and keeps the reply content inline", () => {
  const model = renderConversationModel([
    { id: "a1", role: "assistant", content: "reply", reasoning_content: "thinking steps" }
  ]);
  const item = model.items[0];
  assert.ok(item);
  assert.match(item.html, /class="reasoning-block is-thinking-axis"/);
  assert.match(item.html, /data-trajectory-key="message:a1"/);
  assert.match(item.html, /thinking steps/);
  assert.match(item.html, /class="item-id">a1</);
  assert.match(item.html, />reply<\/p>/);
  assert.match(item.html, /思考/);
});
