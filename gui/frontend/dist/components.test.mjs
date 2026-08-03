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
  assert.match(html, /WORKING/);
});

test("renders queued inputs as safe markdown cards", () => {
  const html = renderChatActivity({
    running: true,
    input_queue: ["**follow up**", "<script>alert(1)</script>"]
  });

  assert.match(html, /QUEUE 01/);
  assert.match(html, /<strong>follow up<\/strong>/);
  assert.match(html, /QUEUE 02/);
  assert.doesNotMatch(html, /<script>/);
  assert.match(html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
});

test("appends activity after conversation without changing tool payloads", () => {
  const rendered = renderConversationComponent(
    [{ role: "assistant", content: "answer" }],
    { running: true, input_queue: ["next"] }
  );

  assert.match(rendered.html, /class="message assistant"[\s\S]*runtime-activity[\s\S]*QUEUE 01/);
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
