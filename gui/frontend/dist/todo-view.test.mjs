import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const markdownSource = await readFile(new URL("./markdown.js", import.meta.url), "utf8");
const markdownURL = `data:text/javascript;base64,${Buffer.from(markdownSource).toString("base64")}`;
const componentsSource = (await readFile(new URL("./components.js", import.meta.url), "utf8"))
  .replace('"./markdown.js"', `"${markdownURL}"`);
const componentsURL = `data:text/javascript;base64,${Buffer.from(componentsSource).toString("base64")}`;
const source = (await readFile(new URL("./todo-view.js", import.meta.url), "utf8"))
  .replace('"./components.js"', `"${componentsURL}"`);
const { renderTodoList } = await import(`data:text/javascript;base64,${Buffer.from(source).toString("base64")}`);

test("renders todo list with authoritative done flags and progress", () => {
  const html = renderTodoList([
    { text: "inspect module", done: true },
    { text: "implement fix", done: false },
    { text: "run tests", done: false }
  ]);
  assert.match(html, /1 \/ 3 完成/);
  assert.match(html, /aria-valuenow="33"/);
  assert.match(html, /todo-item is-done/);
  assert.match(html, /todo-item/);
  assert.match(html, /inspect module/);
  assert.match(html, /data-todo-index="2"/);
});

test("escapes todo text and tolerates malformed items", () => {
  const html = renderTodoList([
    { text: '<img src=x onerror="boom">', done: false },
    null,
    { text: 42, done: true },
    "plain string"
  ]);
  assert.match(html, /&lt;img src=x onerror=&quot;boom&quot;&gt;/);
  assert.doesNotMatch(html, /<img src=x/);
  assert.doesNotMatch(html, /<script>/);
  // 畸形条目被过滤：null / 非对象 / 非字符串 text 丢弃，仅剩 1 条合法项
  assert.match(html, /0 \/ 1 完成/);
  assert.doesNotMatch(html, /暂无待办项/);
});

test("hides list for empty input and normalizes non-array", () => {
  assert.match(renderTodoList([]), /0 \/ 0 完成/);
  assert.match(renderTodoList(null), /0 \/ 0 完成/);
  assert.match(renderTodoList(undefined), /0 \/ 0 完成/);
  assert.match(renderTodoList("nope"), /0 \/ 0 完成/);
});

test("renders done items struck through only from authoritative done flag", () => {
  const html = renderTodoList([{ text: "done task", done: true }, { text: "open task", done: false }]);
  assert.match(html, /is-done/);
  assert.match(html, /✓/);
  assert.match(html, /○/);
  const doneCount = (html.match(/is-done/g) || []).length;
  assert.equal(doneCount, 1);
});
