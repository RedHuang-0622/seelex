import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const embedURL = `data:text/javascript;base64,${Buffer.from(await readFile(new URL("./html-embed.js", import.meta.url), "utf8")).toString("base64")}`;
const markdownSource = (await readFile(new URL("./markdown.js", import.meta.url), "utf8"))
  .replace('"./html-embed.js"', `"${embedURL}"`);
const markdownURL = `data:text/javascript;base64,${Buffer.from(markdownSource).toString("base64")}`;
const componentSource = (await readFile(new URL("./components.js", import.meta.url), "utf8"))
  .replace('"./markdown.js"', `"${markdownURL}"`);
const { queueMoveTarget, renderChatActivity } = await import(`data:text/javascript;base64,${Buffer.from(componentSource).toString("base64")}`);

test("renders reorder and recall controls on every queued message", () => {
  const html = renderChatActivity({ running: true, input_queue: ["first", "second"] });

  assert.equal((html.match(/class="queued-message"/g) || []).length, 2);
  // 每条排队消息都有 ↑ / ↓ / 撤回编辑三个动作，并带可访问名称。
  assert.equal((html.match(/data-queue-action="up"/g) || []).length, 2);
  assert.equal((html.match(/data-queue-action="down"/g) || []).length, 2);
  assert.equal((html.match(/data-queue-action="recall"/g) || []).length, 2);
  assert.match(html, /data-queue-index="0"/);
  assert.match(html, /data-queue-index="1"/);
  assert.match(html, /aria-label="撤回排队 01 到输入框"/);
  assert.match(html, /aria-label="撤回排队 02 到输入框"/);
  assert.match(html, /aria-label="上移排队 02"/);
  assert.match(html, /aria-label="下移排队 01"/);
  // 边界禁用恰好两个：首行不能上移、末行不能下移；撤回始终可用。
  assert.equal((html.match(/disabled/g) || []).length, 2);
});

test("keeps queued labels and escaping after adding the edit controls", () => {
  const html = renderChatActivity({ running: true, input_queue: ["**follow up**", "<script>alert(1)</script>"] });

  assert.match(html, /排队 01/);
  assert.match(html, /排队 02/);
  assert.match(html, /<strong>follow up<\/strong>/);
  assert.doesNotMatch(html, /<script>/);
  assert.match(html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
});

test("maps queue move actions to reorder targets and rejects boundaries", () => {
  assert.deepEqual(queueMoveTarget("up", 1, 3), { from: 1, to: 0 });
  assert.deepEqual(queueMoveTarget("down", 0, 3), { from: 0, to: 1 });
  assert.deepEqual(queueMoveTarget("up", 2, 3), { from: 2, to: 1 });
  assert.equal(queueMoveTarget("up", 0, 3), null);
  assert.equal(queueMoveTarget("down", 2, 3), null);
  assert.equal(queueMoveTarget("recall", 0, 3), null);
  assert.equal(queueMoveTarget("up", 3, 3), null);
  assert.equal(queueMoveTarget("up", -1, 3), null);
  assert.equal(queueMoveTarget("up", 0, 0), null);
  assert.equal(queueMoveTarget("up", 0.5, 3), null);
});

test("queued edit controls are only rendered for running chat", () => {
  assert.equal(renderChatActivity({ running: false }), "");
  const html = renderChatActivity({ running: true, input_queue: ["next"] });
  assert.match(html, /data-queue-action="recall"/);
});

test("renderer wires queue actions to the bridge invoke surface", async () => {
  const script = await readFile(new URL("./app.js", import.meta.url), "utf8");

  assert.ok(script.includes('invoke("ReorderQueuedInput"'), "app.js must call ReorderQueuedInput");
  assert.ok(script.includes('invoke("RecallQueuedInput"'), "app.js must call RecallQueuedInput");
  assert.ok(script.includes("[data-queue-action]"), "app.js must delegate queued message actions");
  assert.ok(script.includes("queueMoveTarget"), "app.js must reuse the shared move mapping");
  assert.ok(script.includes("elements.prompt.value"), "recall must write the original text back into the composer");
});
