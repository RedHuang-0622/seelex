import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const embedURL = `data:text/javascript;base64,${Buffer.from(await readFile(new URL("./html-embed.js", import.meta.url), "utf8")).toString("base64")}`;
const markdownSource = (await readFile(new URL("./markdown.js", import.meta.url), "utf8"))
  .replace('"./html-embed.js"', `"${embedURL}"`);
const markdownURL = `data:text/javascript;base64,${Buffer.from(markdownSource).toString("base64")}`;
const componentsSource = (await readFile(new URL("./components.js", import.meta.url), "utf8"))
  .replace('"./markdown.js"', `"${markdownURL}"`);
const componentsURL = `data:text/javascript;base64,${Buffer.from(componentsSource).toString("base64")}`;
const compactionURL = `data:text/javascript;base64,${Buffer.from(await readFile(new URL("./compaction-format.js", import.meta.url), "utf8")).toString("base64")}`;
const summarySource = (await readFile(new URL("./context-summary.js", import.meta.url), "utf8"))
  .replace('"./components.js"', `"${componentsURL}"`)
  .replace('"./compaction-format.js"', `"${compactionURL}"`);
const { renderContextCompactions } = await import(`data:text/javascript;base64,${Buffer.from(summarySource).toString("base64")}`);
const { mergeCompactionProgress } = await import(compactionURL);

test("renders compaction records with range, origin and a frame entry", () => {
  const html = renderContextCompactions([{
    version: 2, reason: "context_budget_autonomous", origin: "explicit_after_turn",
    messages_before: 0, estimated_tokens: 12345, compacted_at: "2026-07-30T10:00:00Z",
    message_from: "message-1", message_to: "message-103", event_from: 1, event_to: 6,
    frame_ref: "tr-abc123", frame_bytes: 496, frame_tokens: 156
  }]);

  assert.match(html, /上下文压缩/);
  assert.match(html, /上下文预算（自主压缩）/);
  assert.match(html, /显式要求（回合后）/);
  assert.match(html, /消息 message-1\.\.message-103（事件 1\.\.6）/);
  assert.match(html, /12,345 tokens/);
  assert.match(html, /data-compact-open="0"/);
  assert.match(html, /tr-abc123/);
  // 旧占位句必须消失：它没有区间、没有来源、不可展开——用户说的"像占位符"就是它。
  assert.doesNotMatch(html, /Task checkpoint retained/);
  // messages_before 不得当成"压缩前 N 条消息"渲染。
  assert.doesNotMatch(html, /条消息/);
});

test("expanded record reads the folded frame body back by ref", () => {
  const compactions = [{ version: 1, reason: "context_budget", origin: "explicit", frame_ref: "tr-x", frame_bytes: 90, frame_tokens: 24 }];
  const html = renderContextCompactions(compactions, {
    detail: {
      index: 0, loading: false, error: "",
      text: "<!-- seelex:context-checkpoint-frame:v1 -->\n# Context checkpoint frame v1\n",
      hasMore: true, nextOffset: 30, totalBytes: 90
    }
  });

  assert.match(html, /折叠帧正文/);
  assert.match(html, /Context checkpoint frame v1/);
  assert.match(html, /data-compact-frame-load="more"/);
  assert.match(html, /剩余约 60 bytes/);
  assert.match(html, /收起帧正文/);
});

test("records without a frame ref say so instead of offering an empty viewer", () => {
  const html = renderContextCompactions([{ version: 1, reason: "context_budget" }]);
  assert.match(html, /本次没有可回读正文/);
  assert.doesNotMatch(html, /data-compact-open/);
});

test("escapes unknown reason text and hides an empty list", () => {
  assert.equal(renderContextCompactions([]), "");
  const html = renderContextCompactions([{ version: 1, reason: '<script>alert(1)</script>' }]);
  assert.doesNotMatch(html, /<script>/);
  assert.match(html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
  assert.match(html, /上下文压缩/);
});

// 进度条的可见性判据（app.js 的面板 hidden 由这里返回空串与否决定）：零记录时
// 必须仍然出内容。折叠发生在写记录之前，"没有记录就不画"会让第一次压缩看不到
// 任何进度——正是这次修复要消掉的"看不到压缩到哪一步"。
test("renders the gate progress bar with zero records, reusing the plan board track", () => {
  const html = renderContextCompactions([], {
    progress: { state: "running", gate: "store", index: 5, total: 6, version: 7, origin: "explicit", detail: "bytes=496 tokens=156" }
  });
  assert.match(html, /context-compaction-progress is-running/);
  // 复用 Plan 面板那一套轨道与填充，不自造第二套进度组件。
  assert.match(html, /class="plan-board-progress">.*class="plan-board-bar" style="width:83%"/s);
  assert.match(html, /帧正文落盘/);
  assert.match(html, /aria-valuemin="0" aria-valuemax="6" aria-valuenow="5"/);
  assert.match(html, /5\/6 · #7/);
  assert.match(html, /bytes=496 tokens=156/);
  assert.match(html, /显式要求（回合中）/);
});

test("a failed round stops at the gate it reached and shows the real error", () => {
  const html = renderContextCompactions([], {
    progress: {
      state: "failed", index: 2, total: 6, origin: "auto",
      detail: "reached=2/6", outcome: "context: system 指令自身超预算"
    }
  });
  assert.match(html, /is-failed/);
  assert.match(html, /压缩未完成/);
  assert.match(html, /style="width:33%"/);
  assert.match(html, /reached=2\/6/);
  // 错误原文照实显示，不替换成一句安慰话。
  assert.match(html, /system 指令自身超预算/);
  assert.doesNotMatch(html, /已折叠并写入压缩记录/);
});

test("progress alone never claims a record outcome", () => {
  assert.equal(renderContextCompactions([], {}), "");
  assert.equal(renderContextCompactions([], { progress: { state: "running" } }), "");
});

// 起手帧（后端 phase=begin）：显式压缩在判据估算之前就把"开始了"送到界面。判据
// 估算还没收口，因此清单上给这一关一行「进行中」——不编耗时，也不出现凭空累计的
// "共 …ms"。
test("begin frame renders the judge row as running without inventing a duration", () => {
  const html = renderContextCompactions([], {
    progress: { state: "running", phase: "begin", gate: "judge", index: 0, total: 6, origin: "explicit", gates: [] }
  });
  assert.match(html, /context-compaction-progress is-running/);
  assert.match(html, /class="plan-board-bar" style="width:0%"/);
  assert.match(html, /context-compaction-gate is-running/);
  assert.match(html, /判定是否需要折叠/);
  assert.match(html, /进行中/);
  assert.doesNotMatch(html, /共 /);
  assert.match(html, /0\/6/);
});

// 终局清单：一关一行、各带自己实测的耗时。压缩整轮只有几十毫秒，界面上不可能看到
// "慢慢走"的进度条，能回答"它到底干了什么、慢在哪一步"的只有这份逐关清单。
test("done frame lists every gate with its own measured duration", () => {
  const progress = [
    { state: "running", phase: "begin", gate: "judge", index: 0, total: 6 },
    { state: "running", gate: "judge", index: 1, total: 6, elapsed_ms: 28, detail: "compared=163925 all=160079 soft=125106 hard=150127" },
    { state: "running", gate: "assemble", index: 2, total: 6, elapsed_ms: 6, detail: "assembled=83887 target=112055 autonomous=false" },
    { state: "running", gate: "replace", index: 3, total: 6, elapsed_ms: 0, detail: "messages=4" },
    { state: "running", gate: "frame", index: 4, total: 6, elapsed_ms: 0, detail: "bytes=552 injected=false" },
    { state: "running", gate: "store", index: 5, total: 6, elapsed_ms: 0, detail: "bytes=552 tokens=174" },
    { state: "running", gate: "record", index: 6, total: 6, elapsed_ms: 0, detail: "recorded=true version=2" },
    { state: "done", index: 6, total: 6, version: 2, origin: "explicit", outcome: "compacted", detail: "reached=6/6" }
  ].reduce((acc, frame) => mergeCompactionProgress(acc, frame), null);
  const html = renderContextCompactions([], { progress });
  assert.equal(progress.gates.length, 6);
  assert.equal(progress.index, 6);
  assert.equal(progress.elapsedMs, 34);
  assert.match(html, /6\/6 · #2 · 共 34ms/);
  assert.match(html, /判定是否需要折叠<\/span>\s*<em class="context-compaction-gate-ms" title="这一关实测耗时">28ms<\/em>/);
  assert.match(html, /装配压缩上下文<\/span>\s*<em class="context-compaction-gate-ms" title="这一关实测耗时">6ms<\/em>/);
  // 不足一毫秒的关写 `&lt;1ms`：后端给的是截断毫秒，0 的含义就是"不到一毫秒"，
  // 写成 0ms 读起来像"没花时间"，凑成 1ms 是替后端编数字。
  assert.match(html, /替换 provider 历史<\/span>\s*<em class="context-compaction-gate-ms" title="这一关实测耗时">&lt;1ms<\/em>/);
  assert.match(html, /压缩完成/);
  assert.doesNotMatch(html, /进行中/);
  assert.equal((html.match(/context-compaction-gate is-done/g) || []).length, 6);
});
