import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const embedURL = `data:text/javascript;base64,${Buffer.from(await readFile(new URL("./html-embed.js", import.meta.url), "utf8")).toString("base64")}`;
// markdown.js 现在也 import ./file-reference.js（引用 chip 的渲染）：data: URL 模块
// 解析不了相对路径，两个 specifier 都要换成可解析的 URL。
const fileReferenceURL = new URL("./file-reference.js", import.meta.url).href;
const markdownSource = (await readFile(new URL("./markdown.js", import.meta.url), "utf8"))
  .replace('"./html-embed.js"', `"${embedURL}"`)
  .replace('"./file-reference.js"', `"${fileReferenceURL}"`);
const markdownURL = `data:text/javascript;base64,${Buffer.from(markdownSource).toString("base64")}`;
const componentSource = (await readFile(new URL("./components.js", import.meta.url), "utf8"))
  .replace('"./markdown.js"', `"${markdownURL}"`);
const { renderConversationModel } = await import(`data:text/javascript;base64,${Buffer.from(componentSource).toString("base64")}`);

import {
  buildTrajectory,
  renderTrajectoryRow
} from "./trajectory.js";

function toolPair(id, name, status = "success", duration = 1_200_000_000, totalChars = 9000) {
  return [
    { id: `${id}-start`, role: "tool", tool: { id, name, arguments: "{}", status: "running" }, created_at: "2026-08-25T10:00:02Z" },
    { id: `${id}-end`, role: "tool_result", content: "preview", tool: { id, name, result: "preview", status, duration, total_chars: totalChars, truncated: true }, created_at: "2026-08-25T10:00:03Z" }
  ];
}

function findConversationToolEntry(model, toolKey) {
  return model.items.find((entry) => entry.html.includes(`data-conversation-key="${toolKey}"`));
}

test("T3.7 conversation tool row and trajectory row share status/duration/size and key", () => {
  const messages = toolPair("call-1", "read_file");
  const model = renderConversationModel(messages);
  const chipEntry = findConversationToolEntry(model, "tool:call-1");
  assert.ok(chipEntry, "tool summary row must exist in the conversation");
  const chip = chipEntry.html;

  const records = buildTrajectory(messages);
  const tool = records.find((record) => record.kind === "tool");
  assert.ok(tool, "tool trajectory record must exist");
  const row = renderTrajectoryRow(tool, tool.key, new Map());

  // 同一 key：工具行 ↔ 轨迹行跳转命中。
  assert.match(chip, /data-trajectory-key="tool:call-1"/);
  assert.match(row, /data-trajectory-key="tool:call-1"/);

  // 同一 tool 在两视图 status/duration/size 一致（同一分类函数派生）。
  assert.match(chip, /class="tool-state">.*已完成/);
  assert.match(row, /trajectory-status is-neutral">OK/);
  assert.match(chip, /1\.2s/);
  assert.match(row, /1\.2s/);
  assert.match(chip, /8\.8 KB/);
  assert.match(row, /8\.8 KB/);
});

test("T3.7 error tool keeps consistent state across views", () => {
  const messages = toolPair("call-err", "bash", "error", 500_000_000, 128);
  const chipEntry = findConversationToolEntry(renderConversationModel(messages), "tool:call-err");
  assert.ok(chipEntry);
  const chip = chipEntry.html;
  const tool = buildTrajectory(messages).find((record) => record.kind === "tool");
  const row = renderTrajectoryRow(tool, tool.key, new Map());

  assert.match(chip, /class="tool-state">.*失败/);
  assert.match(row, /trajectory-status is-error">ERR/);
  assert.match(chip, /500ms/);
  assert.match(row, /500ms/);
  assert.match(chip, /128 B/);
  assert.match(row, /128 B/);
});

test("T3.8 empty conversation renders without tool rows", () => {
  const model = renderConversationModel([]);
  assert.ok(Array.isArray(model.items));
  assert.equal(model.items.some((entry) => entry.html.includes('data-trajectory-key="tool:')), false);
});

test("T3.8 many consecutive tool calls collapse to one summary with all entries inside", () => {
  const messages = [];
  for (let index = 0; index < 40; index++) {
    messages.push(...toolPair(`call-${index}`, "read_file", "success", 1_000_000_000, 2048));
  }
  const model = renderConversationModel(messages);
  const tools = model.items.filter(entry => entry.meta?.kind === "tool");
  assert.equal(tools.length, 1, "一个连续工具域只显示最新一条");
  assert.equal(tools[0].meta.toolCount, 40);
  const html = tools.map(entry => entry.html).join("");
  assert.equal((html.match(/class="tool-run is-success tool-call-row/g) || []).length, 41);
  assert.equal((html.match(/class="tool-call-summary"/g) || []).length, 41);
  assert.doesNotMatch(html, /conversation-axis/);
  assert.match(html, /data-conversation-key="tools:tool:call-0"/);
  assert.match(html, /data-trajectory-key="tool:call-39"/);
  assert.match(html, /data-tool-count="40"/);
  assert.match(html, /class="tool-domain-list"/);
  assert.match(html, /data-conversation-key="tool:call-0"/);
  assert.match(html, /data-conversation-key="tool:call-39"/);
  assert.equal((html.match(/data-tool-id="call-\d+"/g) || []).length, 41, "外层最新摘要 + 40 条域内条目");
  assert.match(html, /io-panel/);
  assert.match(html, /tool-call-drawer/);
});
