import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const markdownSource = await readFile(new URL("./markdown.js", import.meta.url), "utf8");
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

  assert.deepEqual(model.items.map(item => item.key), ["message:assistant-1", "tool:call-1", "chat:activity"]);
  assert.match(model.items[1].html, /data-conversation-key="tool:call-1"/);
  assert.equal(model.payloads.get("tool:call-1-out"), "done");
});

test("renders truncated tool output as collapsible preview with result_ref loader", () => {
  const bigOutput = "x".repeat(9000);
  const model = renderConversationModel([
    { id: "tool-start", role: "tool", tool: { id: "call-big", name: "bash", arguments: "{}" } },
    { id: "tool-end", role: "tool_result", tool: {
      id: "call-big", name: "bash", result: bigOutput.slice(0, 8000),
      result_ref: "tr-abc123", truncated: true, total_chars: 9000
    } }
  ]);

  const item = model.items.find(entry => entry.key === "tool:call-big");
  assert.ok(item, "tool item must be present");
  assert.match(item.html, /<details class="io-collapse"/);
  assert.match(item.html, /data-load-ref="tr-abc123"/);
  assert.match(item.html, /加载完整输出/);
  assert.match(item.html, /全文 9000 字符/);
  // 完整 payload 仍可复制（预览 + 引用），与展开按钮并存。
  assert.equal(model.payloads.get("tool:call-big-out").length, 8000);
  // 大块输出不直接进 DOM（预览被折叠在 <pre> 内且受 4KB 上限约束）。
  const previewMatch = item.html.match(/<pre>([\s\S]*?)<\/pre>/);
  assert.ok(previewMatch && previewMatch[1].length <= 4000 + 128, "rendered preview must stay under 4KB + slack");
});

test("passes non-truncated tool output through unchanged", () => {
  const model = renderConversationModel([
    { id: "tool-start", role: "tool", tool: { id: "call-small", name: "read_file", arguments: "{}" } },
    { id: "tool-end", role: "tool_result", tool: { id: "call-small", name: "read_file", result: "small" } }
  ]);
  const item = model.items.find(entry => entry.key === "tool:call-small");
  assert.ok(item);
  assert.doesNotMatch(item.html, /data-load-ref/);
  assert.doesNotMatch(item.html, /io-collapse/);
  assert.equal(model.payloads.get("tool:call-small-out"), "small");
});
