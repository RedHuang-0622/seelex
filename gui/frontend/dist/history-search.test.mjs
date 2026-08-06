import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const markdownSource = await readFile(new URL("./markdown.js", import.meta.url), "utf8");
const markdownURL = `data:text/javascript;base64,${Buffer.from(markdownSource).toString("base64")}`;
const componentsSource = (await readFile(new URL("./components.js", import.meta.url), "utf8"))
  .replace('"./markdown.js"', `"${markdownURL}"`);
const componentsURL = `data:text/javascript;base64,${Buffer.from(componentsSource).toString("base64")}`;
const source = (await readFile(new URL("./history-search.js", import.meta.url), "utf8"))
  .replace('"./components.js"', `"${componentsURL}"`);
const { historySearchView, renderHistorySearchResults } = await import(`data:text/javascript;base64,${Buffer.from(source).toString("base64")}`);

test("renders hit frames with range, score and expandable records", () => {
  const html = renderHistorySearchResults({
    query: "数据库优化",
    total_units: 5,
    indexed_frames: 2,
    hits: [{
      segment_id: "compact-a",
      from: 0,
      to: 1,
      score: 2.5,
      summary: "数据库索引优化讨论",
      units: 2,
      records: [
        { role: "user", content: "聊聊数据库索引" },
        { role: "assistant", tool_name: "grep_search" },
        { role: "tool", tool_name: "bash", content: "工具结果内容", result_ref: "result:call-1" }
      ]
    }]
  });
  assert.match(html, /compact-a/);
  assert.match(html, /\[0\.\.1\]/);
  assert.match(html, /2\.50/);
  assert.match(html, /数据库索引优化讨论/);
  assert.match(html, /索引 2 帧 · 事件流 5 轮/);
  assert.match(html, /<details class="history-search-hit" open>/);
  assert.match(html, /用户/);
  assert.match(html, /聊聊数据库索引/);
  assert.match(html, /grep_search/);
  assert.match(html, /result:call-1/);
});

test("escapes all hit and record text", () => {
  const html = renderHistorySearchResults({
    hits: [{
      segment_id: '<img src=x onerror="boom">',
      from: 0,
      to: 0,
      score: 1,
      summary: "<script>alert(1)</script>",
      records: [{ role: "user", content: '<b onclick="x()">raw</b>' }]
    }]
  });
  assert.match(html, /&lt;img src=x onerror=&quot;boom&quot;&gt;/);
  assert.match(html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
  assert.match(html, /&lt;b onclick=&quot;x\(\)&quot;&gt;raw&lt;\/b&gt;/);
  assert.doesNotMatch(html, /<img src=x/);
  assert.doesNotMatch(html, /<script>/);
  assert.doesNotMatch(html, /<b onclick/);
});

test("shows note when no hits and normalizes malformed payloads", () => {
  assert.match(renderHistorySearchResults({ note: "历史未压缩：按最近轮次全量扫描检索（可检索性有限）", hits: [] }), /历史未压缩/);
  assert.match(renderHistorySearchResults(null), /没有匹配的历史记录/);
  assert.match(renderHistorySearchResults(undefined), /没有匹配的历史记录/);
  assert.match(renderHistorySearchResults("nope"), /没有匹配的历史记录/);
  assert.match(renderHistorySearchResults({ hits: "not-an-array" }), /没有匹配的历史记录/);
});

test("normalizer drops malformed hits and keeps valid ones", () => {
  const view = historySearchView({
    hits: [
      null,
      { segment_id: 42, from: 0, to: 0 },
      { segment_id: "compact-b", from: 3, to: 4, summary: "有效命中", records: [{ role: "user", content: "x" }] }
    ],
    total_units: 6,
    indexed_frames: 2
  });
  assert.equal(view.hits.length, 1);
  assert.equal(view.hits[0].segment_id, "compact-b");
  assert.equal(view.totalUnits, 6);
  assert.equal(view.indexedFrames, 2);
});

test("renders truncated flags from authoritative payload", () => {
  const html = renderHistorySearchResults({
    truncated: true,
    hits: [{
      segment_id: "compact-a",
      from: 0,
      to: 0,
      score: 1,
      summary: "s",
      truncated: true,
      records: [{ role: "tool", tool_name: "bash", content: "…", truncated: true }]
    }]
  });
  assert.match(html, /预算截断/);
  assert.match(html, /已截断/);
});

test("renders empty records placeholder and missing summary", () => {
  const html = renderHistorySearchResults({
    hits: [{ segment_id: "compact-a", from: 5, to: 5, score: 0.5, records: [] }]
  });
  assert.match(html, /\(无摘要\)/);
  assert.match(html, /该帧范围内无事件记录/);
});
