import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const markdownSource = await readFile(new URL("./markdown.js", import.meta.url), "utf8");
const markdownURL = `data:text/javascript;base64,${Buffer.from(markdownSource).toString("base64")}`;
const componentsSource = (await readFile(new URL("./components.js", import.meta.url), "utf8"))
  .replace('"./markdown.js"', `"${markdownURL}"`);
const componentsURL = `data:text/javascript;base64,${Buffer.from(componentsSource).toString("base64")}`;
const summarySource = (await readFile(new URL("./context-summary.js", import.meta.url), "utf8"))
  .replace('"./components.js"', `"${componentsURL}"`);
const { renderContextCompactions } = await import(`data:text/javascript;base64,${Buffer.from(summarySource).toString("base64")}`);

test("renders public context compaction metadata without checkpoint content", () => {
  const html = renderContextCompactions([{
    version: 2, reason: "large_tool_output", messages_before: 7,
    estimated_tokens: 12345, compacted_at: "2026-07-30T10:00:00Z"
  }]);

  assert.match(html, /Context compression/);
  assert.match(html, /Large tool output/);
  assert.match(html, /7 messages/);
  assert.match(html, /12,345 tokens/);
  assert.match(html, /Task checkpoint retained/);
  assert.doesNotMatch(html, /seelex:context-checkpoint/);
});

test("escapes unknown reason text and hides an empty list", () => {
  assert.equal(renderContextCompactions([]), "");
  const html = renderContextCompactions([{ version: 1, reason: '<script>alert(1)</script>' }]);
  assert.doesNotMatch(html, /<script>/);
  assert.match(html, /Context compression/);
});
