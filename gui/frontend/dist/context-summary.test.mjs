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
const { renderContextCompactions, renderCompactionFrameModal } = await import(`data:text/javascript;base64,${Buffer.from(summarySource).toString("base64")}`);
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

test("expanded record reads the compacted frame body back by ref", () => {
  const compactions = [{ version: 1, reason: "context_budget", origin: "explicit", frame_ref: "tr-x", frame_bytes: 90, frame_tokens: 24 }];
  const html = renderContextCompactions(compactions, {
    sessionID: "session-a",
    detail: {
      ref: "tr-x", sessionID: "session-a", loading: false, error: "",
      text: "<!-- seelex:context-checkpoint-frame:v1 -->\n# Context checkpoint frame v1\n",
      hasMore: true, nextOffset: 30, totalBytes: 90
    }
  });

  assert.match(html, /压缩帧正文/);
  assert.match(html, /Context checkpoint frame v1/);
  assert.match(html, /data-compact-frame-load="more"/);
  assert.match(html, /剩余约 60 bytes/);
  assert.match(html, /收起/);
});

// 展开身份 = (会话, ref)：上一个会话读回来的正文不许挂在当前会话的记录行上。
// 旧实现按**记录数组下标**判定展开行，于是切到会话 B 后，B 的同一序号行会直接显示
// 会话 A 的正文（正文本身还是 A 的字节）——正是用户报的"跨会话留存压缩帧的污染"。
test("ref 对不上不认（记录数组重排后下标会漂）", () => {
  const html = renderContextCompactions([{ version: 1, reason: "context_budget", frame_ref: "tr-other" }], {
    sessionID: "session-a",
    detail: { ref: "tr-x", sessionID: "session-a", text: "旧正文", hasMore: false, nextOffset: 0, totalBytes: 9 }
  });
  assert.doesNotMatch(html, /旧正文/);
  assert.doesNotMatch(html, /收起/);
  assert.match(html, /data-compact-open="0"/);
});

test("会话对不上不认：切到别的会话后旧正文既不上屏也不给收起", () => {
  const records = [{ version: 1, reason: "context_budget", frame_ref: "tr-shared" }];
  const stale = { ref: "tr-shared", sessionID: "session-a", loading: false, error: "", text: "会话 A 的正文", hasMore: false, nextOffset: 9, totalBytes: 9 };

  const crossSession = renderContextCompactions(records, { detail: stale, sessionID: "session-b" });
  assert.doesNotMatch(crossSession, /会话 A 的正文/);
  assert.doesNotMatch(crossSession, /收起/);
  assert.match(crossSession, /data-compact-open="0"/);

  // 同一会话 + 同一 ref 才认（视图侧清空前也不许把别的会话的正文画出来）。
  const sameSession = renderContextCompactions(records, {
    detail: { ...stale, sessionID: "session-b" }, sessionID: "session-b"
  });
  assert.match(sameSession, /会话 A 的正文/);
  assert.match(sameSession, /收起/);
});

test("records without a frame ref say so instead of offering an empty viewer", () => {
  const html = renderContextCompactions([{ version: 1, reason: "context_budget" }]);
  assert.match(html, /无帧正文/);
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
// 必须仍然出内容。压缩发生在写记录之前，"没有记录就不画"会让第一次压缩看不到
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
  assert.doesNotMatch(html, /已压缩并写入压缩记录/);
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
  assert.match(html, /判定是否需要压缩/);
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
  assert.match(html, /判定是否需要压缩<\/span>\s*<em class="context-compaction-gate-ms" title="这一关实测耗时">28ms<\/em>/);
  assert.match(html, /装配压缩上下文<\/span>\s*<em class="context-compaction-gate-ms" title="这一关实测耗时">6ms<\/em>/);
  // 不足一毫秒的关写 `&lt;1ms`：后端给的是截断毫秒，0 的含义就是"不到一毫秒"，
  // 写成 0ms 读起来像"没花时间"，凑成 1ms 是替后端编数字。
  assert.match(html, /替换 provider 历史<\/span>\s*<em class="context-compaction-gate-ms" title="这一关实测耗时">&lt;1ms<\/em>/);
  assert.match(html, /压缩完成/);
  assert.doesNotMatch(html, /进行中/);
  assert.equal((html.match(/context-compaction-gate is-done/g) || []).length, 6);
});

// ── 压缩帧正文：内联展开 + 可调大小弹框（两种读法，同一份正文区）────────────

test("每条记录同时给内联展开与弹框两个入口（同一 ref）", () => {
  const html = renderContextCompactions([{
    version: 3, reason: "context_budget", message_from: "message-1", message_to: "message-9",
    frame_ref: "tr-z", frame_bytes: 120, frame_tokens: 30
  }]);
  assert.match(html, /data-compact-open="0"/);
  assert.match(html, /data-compact-frame-ref="tr-z"/);
  assert.match(html, /弹框/);
});

// 压缩栈表格的读法（用户口径 2026-09-26）：栈顶在前、按新旧下沉，栈顶那一行才是
// 当前前沿（深灰 + 「栈顶」标记），更早的压缩降成浅灰但仍逐条可点开读正文。
// 展开入口仍带**原数组下标**，但视图侧只用它取这一行的 frame_ref（点击那一刻的
// 权威记录）；此后收起/续读只认 ref——下标会随记录重排与会话切换而漂。
test("压缩栈按新旧下沉，只有栈顶标前沿，入口仍按原下标记账", () => {
  const records = [
    { version: 1, reason: "context_budget", message_from: "message-1", message_to: "message-9", frame_ref: "tr-old" },
    { version: 2, reason: "large_tool_output", message_from: "message-1", message_to: "message-40", frame_ref: "tr-new" }
  ];
  const html = renderContextCompactions(records);
  const rows = [...html.matchAll(/<div class="compaction-stack-row (is-frontier|is-stale)[^"]*"[^>]*data-compact-index="(\d+)"/g)]
    .map(match => ({ state: match[1], index: Number(match[2]) }));
  assert.deepEqual(rows, [{ state: "is-frontier", index: 1 }, { state: "is-stale", index: 0 }]);
  assert.equal((html.match(/class="compaction-stack-flag"/g) || []).length, 1, "前沿标记只能有一个");
  // 栈顶在前：按行序读 data-compact-index（下标 1 = 更晚那次压缩）。
  assert.ok(html.indexOf('data-compact-index="1"') < html.indexOf('data-compact-index="0"'), "栈顶没排在最前");
  // 每次成功压缩都带一枚 seq 徽标（用户口径 2026-10-02：压缩成功之后状态里要出
  // 一条带 seq 的压缩条目）。组件是组件库件 renderSeqBadge，色调走压缩那一档。
  assert.equal((html.match(/class="seq-badge is-compaction"/g) || []).length, 2, "两条记录各一枚 seq 徽标");
  assert.match(html, /seq-badge-num">2</);
  assert.match(html, /seq-badge-unit">seq</);
  // 表头存在且只有一行表头（表格不是卡片列表）。
  assert.match(html, /class="compaction-stack-row is-head"[^>]*>.*<span role="columnheader">seq<\/span>/s);
});

test("弹框正文区与右栏内联展开是同一段 HTML", () => {
  const record = { version: 3, frame_ref: "tr-z", frame_bytes: 120, frame_tokens: 30, estimated_tokens: 4200 };
  const detail = { loading: false, error: "", text: "frame body", hasMore: true, nextOffset: 10, totalBytes: 25 };
  const modal = renderCompactionFrameModal({ record, detail });
  assert.match(modal, /压缩帧正文/);
  assert.match(modal, /tr-z/);
  assert.match(modal, /frame body/);
  assert.match(modal, /data-compact-frame-load="more"/);
  assert.match(modal, /剩余约 15 bytes/);
  // 右栏展开时用的是同一个渲染器：两种读法若各写一套，正文/分页迟早漂移。
  const inline = renderContextCompactions([record], {
    sessionID: "session-x",
    detail: { ref: "tr-z", sessionID: "session-x", ...detail }
  });
  assert.ok(inline.includes(modal), "弹框正文应当是右栏展开正文的同一段 HTML");
});

test("没有 ref 的弹框明说没有正文，不给一个空壳 viewer", () => {
  const html = renderCompactionFrameModal({ record: { version: 1 }, detail: { text: "x" } });
  assert.match(html, /没有可回读的正文/);
  assert.doesNotMatch(html, /axis-detail-text/);
});

// ── 压缩失败痕（用户口径 2026-10-02）────────────────────────────────
// 「压缩失败 → 留下失败记录 → 原始上下文继续存在 → 模型仍然直接看到原来的上下文」：
// 失败不是一次压缩，但它必须**在状态页上查得到**（此前只活在 6 秒的瞬态进度条里）。
// 渲染上因此与成功条目同表不同行：带失败色、写清原因、没有区间/帧/动作列。
test("压缩失败痕渲染成表里一条失败行，不带区间与帧入口", () => {
  const html = renderContextCompactions([{
    version: 3, reason: "context_budget", origin: "explicit", failed: true,
    note: "no_model_summary estimated=281424 budget=163616 window=200000 overhead=9123",
    estimated_tokens: 281424, compacted_at: "2026-07-30T10:00:00Z"
  }]);
  assert.match(html, /class="compaction-stack-row is-failed"/);
  assert.match(html, /data-compact-failed="1"/);
  assert.match(html, /<strong>压缩失败<\/strong>/);
  assert.match(html, /拿不到模型读后感/);
  // 原因里的数字事实原样带出：它是这次失败的证据，不是可改写的叙述。
  assert.match(html, /estimated=281424 budget=163616 window=200000 overhead=9123/);
  // 失败痕不带区间、不给帧入口、也不占"栈顶"。
  assert.doesNotMatch(html, /data-compact-open/);
  assert.doesNotMatch(html, /data-compact-frame-ref/);
  assert.doesNotMatch(html, /compaction-stack-flag">栈顶/);
  // seq 徽标照给（它是第几次压缩尝试），只是色调走失败色。
  assert.match(html, /seq-badge is-compaction is-failed/);
  assert.match(html, /上下文原样/);
});

test("失败痕与成功记录同表：失败排在栈下方，前沿仍只认成功那一条", () => {
  // 失败痕没有区间（EventTo/message_to 恒空），因此既不能当栈顶、也不能画对话区分界
  // ——把它读成"已压出窗口的上下文"就是造假。
  const records = [
    { version: 1, reason: "context_budget", message_from: "message-1", message_to: "message-9", frame_ref: "tr-old" },
    { version: 2, reason: "context_budget", failed: true, note: "no_model_summary" }
  ];
  const html = renderContextCompactions(records);
  const rows = [...html.matchAll(/data-compact-index="(\d+)"/g)].map(match => Number(match[1]));
  assert.deepEqual(rows, [0, 1], "成功在前（栈）、失败接在末尾");
  assert.equal((html.match(/compaction-stack-flag">栈顶/g) || []).length, 1);
  assert.equal((html.match(/compaction-stack-row is-failed/g) || []).length, 1);
});

// 失败的**报错原文**要落在失败条目的可视化**下面**（用户口径 2026-10-04）：原因行
// 说的是"这次没压成"，报错行说的是"读数闸这次报了什么"——两句都在，读者才能自答
// 下一步（查配置 / 查素材 / 查那次调用）。此前报错只活在 6 秒的瞬态进度条里。
test("失败痕把报错原文渲染在原因下面，后端没给就不给这一行", () => {
  const note = "no_model_summary estimated=197421 budget=166808 window=200000 overhead=4986"
    + " error=compact-local:replay-failed 前缀重放两次调用均失败，已回退本地确定性压缩：connect: connection refused";
  const html = renderContextCompactions([{ version: 3, reason: "context_budget", failed: true, note }]);
  assert.match(html, /class="compaction-stack-error"[^>]*>报错：compact-local:replay-failed/);
  assert.match(html, /connect: connection refused/);
  // 位置：原因行在上、报错行在下（"下面"是字面要求，不是修辞）。
  assert.ok(html.indexOf("compaction-stack-note") < html.indexOf("compaction-stack-error"), "报错行应在原因行下面");

  // 报错是外部文本，照样要转义。
  const escaped = renderContextCompactions([{
    version: 3, failed: true, note: "no_model_summary error=<script>alert(1)</script>"
  }]);
  assert.doesNotMatch(escaped, /<script>/);
  assert.match(escaped, /&lt;script&gt;/);

  // 后端没给报错段（旧痕 / 这次没走到读数闸）→ 不给报错行，也不拿数字事实冒充报错。
  const plain = renderContextCompactions([{ version: 3, failed: true, note: "no_model_summary estimated=1 budget=2" }]);
  assert.doesNotMatch(plain, /compaction-stack-error/);
  assert.doesNotMatch(plain, /报错：/);
});
