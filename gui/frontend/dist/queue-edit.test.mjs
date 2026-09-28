import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const embedURL = `data:text/javascript;base64,${Buffer.from(await readFile(new URL("./html-embed.js", import.meta.url), "utf8")).toString("base64")}`;
const markdownSource = (await readFile(new URL("./markdown.js", import.meta.url), "utf8"))
  .replace('"./html-embed.js"', `"${embedURL}"`);
const markdownURL = `data:text/javascript;base64,${Buffer.from(markdownSource).toString("base64")}`;
const componentSource = (await readFile(new URL("./components.js", import.meta.url), "utf8"))
  .replace('"./markdown.js"', `"${markdownURL}"`);
const { queueMoveTarget, queueDragTarget, renderChatActivity, renderMessageQueue } = await import(`data:text/javascript;base64,${Buffer.from(componentSource).toString("base64")}`);

test("renders reorder and recall controls on every queued message", () => {
  const html = renderMessageQueue({ running: true, input_queue: ["first", "second"] });

  assert.equal((html.match(/class="queued-message"/g) || []).length, 2);
  // 每条排队消息都有拖拽把手 + ↑ / ↓ / 撤回编辑四个入口（把手负责拖拽换序，
  // 箭头是键盘与无拖拽环境的等价入口），并带可访问名称。
  assert.equal((html.match(/data-queue-drag="/g) || []).length, 2);
  assert.equal((html.match(/data-queue-action="up"/g) || []).length, 2);
  assert.equal((html.match(/data-queue-action="down"/g) || []).length, 2);
  assert.equal((html.match(/data-queue-action="recall"/g) || []).length, 2);
  assert.match(html, /data-queue-index="0"/);
  assert.match(html, /data-queue-index="1"/);
  assert.match(html, /aria-label="撤回排队 01 到输入框"/);
  assert.match(html, /aria-label="撤回排队 02 到输入框"/);
  assert.match(html, /aria-label="上移排队 02"/);
  assert.match(html, /aria-label="下移排队 01"/);
  assert.match(html, /aria-label="拖拽调整排队 01 的顺序"/);
  // 边界禁用恰好两个：首行不能上移、末行不能下移；撤回始终可用。
  assert.equal((html.match(/disabled/g) || []).length, 2);
});

test("keeps queued labels and escaping after adding the edit controls", () => {
  const html = renderMessageQueue({ running: true, input_queue: ["**follow up**", "<script>alert(1)</script>"] });

  // 排队序号与正文一并在可访问名/提示里（单行条纸面上只有一行字）。
  assert.match(html, /aria-label="排队 01，等待发送"/);
  assert.match(html, /aria-label="排队 02，等待发送"/);
  // 纯文本正文：markdown 标记原样显示，标签一律逃逸。
  assert.match(html, /\*\*follow up\*\*/);
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

// 拖拽换序的落点换算：纸面落点是"插入缝"（第 hoverIndex 条的上半/下半），后端要的是
// "条目最终落在 to 位置"（service_queue.go 的 from→to 口径）。两套口径差一层：把条目
// 先抽出来会让缝之后的序号前移一位，所以 from 在缝前面时 to 要减 1。这组用例把账算清。
test("maps a drag drop onto the same from/to contract the arrow buttons use", () => {
  // [A,B,C,D] 的四种典型落点。
  assert.deepEqual(queueDragTarget(0, 1, 4, true), { from: 0, to: 1 });  // A 落到 B 之后 → [B,A,C,D]
  assert.deepEqual(queueDragTarget(0, 2, 4, true), { from: 0, to: 2 });  // A 落到 C 之后 → [B,C,A,D]
  assert.deepEqual(queueDragTarget(3, 1, 4, false), { from: 3, to: 1 }); // D 落到 B 之前 → [A,D,B,C]
  assert.deepEqual(queueDragTarget(3, 1, 4, true), { from: 3, to: 2 });  // D 落到 B 之后 → [A,B,D,C]
  assert.deepEqual(queueDragTarget(1, 3, 4, true), { from: 1, to: 3 });  // B 落到末位之后 → [A,C,D,B]
  assert.deepEqual(queueDragTarget(2, 0, 3, false), { from: 2, to: 0 }); // C 落到 A 之前 → [C,A,B]
  // 原地不动（含"落在自己上身/下身"与"落在紧邻的前一条上半"）：不必产生一次真实搬家。
  assert.equal(queueDragTarget(0, 1, 3, false), null);
  assert.equal(queueDragTarget(1, 1, 3, false), null);
  assert.equal(queueDragTarget(1, 1, 3, true), null);
  assert.equal(queueDragTarget(0, 0, 1, true), null);
  // 越界与非整数一律拒绝（后端会照单全收一次非法搬家，前端先挡掉）。
  assert.equal(queueDragTarget(-1, 0, 3, false), null);
  assert.equal(queueDragTarget(0, 3, 3, false), null);
  assert.equal(queueDragTarget(0.5, 1, 3, false), null);
  assert.equal(queueDragTarget(0, Number.NaN, 3, false), null);
});

test("queued rows live in their own strip, not in the conversation transcript", () => {
  // 活动带只剩"执行中"：排队条搬去输入框上沿的宿主（#message-queue），
  // 否则它会随对话滚动跑掉、与输入框之间还隔着一整段底部留白。
  assert.equal(renderChatActivity({ running: false }), "");
  const activity = renderChatActivity({ running: true, input_queue: ["next"] });
  assert.match(activity, /class="runtime-activity"/);
  assert.doesNotMatch(activity, /queued-message/);
  assert.match(renderMessageQueue({ running: true, input_queue: ["next"] }), /data-queue-action="recall"/);
  assert.equal(renderMessageQueue({ running: false }), "");
  assert.equal(renderMessageQueue({ running: true, input_queue: [] }), "");
});

test("renderer wires queue actions to the bridge invoke surface", async () => {
  const script = await readFile(new URL("./app.js", import.meta.url), "utf8");

  assert.ok(script.includes('invoke("ReorderQueuedInput"'), "app.js must call ReorderQueuedInput");
  assert.ok(script.includes('invoke("RecallQueuedInput"'), "app.js must call RecallQueuedInput");
  assert.ok(script.includes("[data-queue-action]"), "app.js must delegate queued message actions");
  assert.ok(script.includes("queueMoveTarget"), "app.js must reuse the shared move mapping");
  assert.ok(script.includes("elements.prompt.value"), "recall must write the original text back into the composer");
  // 拖拽换序：把手是拖动源，落点换算走 queueDragTarget，动作仍是同一条 ReorderQueuedInput。
  assert.ok(script.includes("[data-queue-drag]"), "app.js must delegate the queue drag handle");
  assert.ok(script.includes("queueDragTarget"), "app.js must reuse the shared drop mapping");
  // 队列拖拽必须用自己的 mime：写 text/plain 会被 file-drop 当成工作树路径读走。
  assert.ok(script.includes("application/x-seelex-queue"), "queue drag must carry its own mime");
  // 宿主是输入框正上方那一层（chat-view.js 渲染，#message-queue 在 index.html）。
  const chatView = await readFile(new URL("./chat-view.js", import.meta.url), "utf8");
  assert.ok(chatView.includes("renderMessageQueue"), "chat-view.js must render the queue strip");
  assert.ok(chatView.includes('elements["message-queue"]'), "chat-view.js must target the queue host");
  const markup = await readFile(new URL("./index.html", import.meta.url), "utf8");
  assert.match(markup, /id="message-queue"/);
});
