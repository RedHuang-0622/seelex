import assert from "node:assert/strict";
import test from "node:test";
import { readFile } from "node:fs/promises";

// 渲染件测试：与其它 view 测试同一手法（data: URL 替掉相对 import）。
const dataURL = (source) => `data:text/javascript;base64,${Buffer.from(source).toString("base64")}`;
const componentsSource = await readFile(new URL("./components.js", import.meta.url), "utf8");
const markdownSource = (await readFile(new URL("./markdown.js", import.meta.url), "utf8"))
  .replace('"./html-embed.js"', `"${new URL("./html-embed.js", import.meta.url).href}"`)
  .replace('"./file-reference.js"', `"${new URL("./file-reference.js", import.meta.url).href}"`);
const componentsURL = dataURL(componentsSource.replace('"./markdown.js"', `"${dataURL(markdownSource)}"`));
const boardSource = (await readFile(new URL("./tool-record-board.js", import.meta.url), "utf8")).replace(
  '"./components.js"',
  `"${componentsURL}"`,
);
const {
  renderToolRecordBoard,
  renderToolRecordBars,
  renderToolRecordReasons,
  normalizeStats,
  reasonLabel,
} = await import(dataURL(boardSource));

const board = () => ({
  enabled: true,
  total: 12,
  success: 9,
  failure: 3,
  tools: [
    { tool: "bash", success: 8, failure: 2, total: 10, last_at: "2026-10-15T21:10:00Z", reasons: [{ reason: "timeout", count: 2 }] },
    { tool: "read_file", success: 1, failure: 1, total: 2, reasons: [{ reason: "not-found", count: 1 }] },
  ],
});

test("柱形图：一工具一根柱子，成功在上、失败在下", () => {
  const html = renderToolRecordBars(normalizeStats(board()), "");
  // 两根柱子 → 每个 <g> 里两个 rect（成功 + 失败）
  assert.equal((html.match(/class="tool-record-bar/g) || []).length, 2);
  assert.equal((html.match(/<rect /g) || []).length, 4);
  // 只取 bash 那一根柱子的 <g>：不切到组尾会把后面那根也算进来。
  const bash = html.match(/data-tool-record-tool="bash"[\s\S]*?<\/g>/)[0];
  const rects = [...bash.matchAll(/<rect x="[-\d.]+" y="([-\d.]+)" width="[\d.]+" height="([\d.]+)" fill="(#\w+)"/g)];
  assert.equal(rects.length, 2);
  const [, failureY, failureH, failureColor] = rects[0];
  const [, successY, successH, successColor] = rects[1];
  assert.equal(failureColor, "#ef4444");
  assert.equal(successColor, "#10b981");
  // 成功段坐在失败段上方：successY < failureY，且两段相接。
  assert.ok(Number(successY) < Number(failureY), `成功段必须在失败段上方：${successY} vs ${failureY}`);
  assert.ok(Math.abs(Number(successY) + Number(successH) - Number(failureY)) < 0.01);
  // 次数标注在柱顶
  assert.match(html, />10</);
});

test("点柱子 → 该工具的原因饼图（无选中时给提示）", () => {
  const noSelection = renderToolRecordBoard(board());
  assert.match(noSelection, /点一根柱子/);
  assert.doesNotMatch(noSelection, /<path /);

  const selected = renderToolRecordBoard(board(), { selectedTool: "bash" });
  assert.match(selected, /bash<\/strong> 的失败原因/);
  assert.match(selected, /超时/);
  assert.match(selected, /100\.0%/);
  // 单原因 → 整圆（圆弧退化会画不出来）
  assert.match(selected, /<circle /);

  const both = renderToolRecordBoard(
    { ...board(), tools: [{ tool: "bash", success: 1, failure: 3, reasons: [{ reason: "timeout", count: 2 }, { reason: "refused", count: 1 }] }] },
    { selectedTool: "bash" },
  );
  assert.equal((both.match(/<path /g) || []).length, 2);
  assert.match(both, /66\.7%/);
  assert.match(both, /33\.3%/);
});

test("空态与关态：关着时说明开关在哪（看板本身只读，不带开关控件）", () => {
  const off = renderToolRecordBoard({ enabled: false, total: 0, tools: [] });
  assert.match(off, /计数已关闭/);
  assert.doesNotMatch(off, /<svg/);
  const on = renderToolRecordBoard({ enabled: true, total: 0, tools: [] });
  assert.match(on, /还没有工具调用记录/);
  // 开关在设置弹窗里：看板**不再**渲染任何开关控件（看板只读读数）。
  assert.doesNotMatch(renderToolRecordBoard(board()), /type="checkbox"/);
  assert.match(renderToolRecordBoard({ ...board(), enabled: false }), /设置 → 工具记录/);
});

test("台账落盘失败显示告警且被转义", () => {
  const html = renderToolRecordBoard({
    enabled: true, total: 1, success: 1,
    tools: [{ tool: "bash", success: 1 }],
    ledger_error: 'open <tool-record.jsonl>: access "denied"',
  });
  assert.match(html, /台账落盘失败/);
  assert.match(html, /&lt;tool-record\.jsonl&gt;/);
  assert.doesNotMatch(html, /<tool-record/);
});

test("归一：忽略零次/无名行，按总次数降序；原因词表外的标识照原样显示", () => {
  const stats = normalizeStats({
    tools: [
      { tool: "a", success: 1, failure: 0 },
      { tool: "b", success: 9, failure: 1 },
      { tool: "c", success: 0, failure: 0 },
      { tool: "", success: 5 },
    ],
  });
  assert.deepEqual(stats.map((row) => row.tool), ["b", "a"]);
  assert.equal(reasonLabel("timeout"), "超时");
  assert.equal(reasonLabel("weird-reason"), "weird-reason");
  const html = renderToolRecordReasons({ tool: "x", success: 0, failure: 1, reasons: [{ reason: "weird-reason", count: 1 }] });
  assert.match(html, /weird-reason/);
});
