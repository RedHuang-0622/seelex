import assert from "node:assert/strict";
import test from "node:test";

import {
  FORK_LANE_TOKENS,
  commitGraphRowHTML,
  forkGraphWidth,
  forkLaneToken,
  laneCenter,
  layoutCommitGraph,
  railOffset,
  renderCommitGraph,
  treeRowAttrs
} from "./tree-fork.js";

// ── 树轨 ────────────────────────────────────────────────

test("root rows carry no rail and no indent variables", () => {
  const row = treeRowAttrs({ depth: 0 });
  assert.equal(row.className, "tf-row");
  assert.equal(row.style, "");
});

test("last child draws an elbow, other children draw a through line", () => {
  const last = treeRowAttrs({ depth: 1, isLast: true });
  assert.equal(last.className, "tf-row tf-row--child tf-row--elbow");
  assert.equal(last.style, "--tf-depth:1;--tf-indent:14px");

  const more = treeRowAttrs({ depth: 1, isLast: false });
  assert.equal(more.className, "tf-row tf-row--child tf-row--line");
  assert.equal(more.style, "--tf-depth:1;--tf-indent:14px");
});

test("ancestor rails continue only for levels that still have siblings", () => {
  const row = treeRowAttrs({ depth: 3, isLast: true, ancestorHasMore: [true, false, true] });
  assert.match(row.style, /background-image:linear-gradient\(var\(--tree-rail\), var\(--tree-rail\)\)/);
  // level 0 续行、level 1 不续行（后代已收口）→ 只有一道背景轨。
  assert.equal(row.style.split("linear-gradient").length - 1, 1);
  assert.match(row.style, new RegExp(`background-position:${railOffset(0, 14)}px 0`));
  assert.match(row.style, /background-size:1px 100%/);
  assert.match(row.style, /background-repeat:no-repeat/);
});

test("each continuing ancestor level contributes one rail stripe", () => {
  const row = treeRowAttrs({ depth: 4, isLast: false, ancestorHasMore: [true, true, true] });
  assert.equal(row.style.split("linear-gradient").length - 1, 3);
  const positions = row.style.match(/background-position:([^;]+)/)?.[1] || "";
  assert.deepEqual(positions.split(","), [`${railOffset(0, 14)}px 0`, `${railOffset(1, 14)}px 0`, `${railOffset(2, 14)}px 0`]);
});

test("rail offsets stay on integer pixels and match the CSS formula", () => {
  assert.equal(railOffset(0, 14), 6); // 0 × 14 + 7 − 1
  assert.equal(railOffset(1, 14), 20);
  assert.equal(railOffset(2, 14), 34);
  // 奇数缩进归一到偶数（1px 轨线不会落在半像素上）。
  const odd = treeRowAttrs({ depth: 2, ancestorHasMore: [true], indent: 15 });
  assert.equal(odd.indent, 14);
  assert.match(odd.style, /--tf-indent:14px/);
});

test("depth is clamped so a malformed payload cannot explode the rail", () => {
  const deep = treeRowAttrs({ depth: 999, ancestorHasMore: new Array(999).fill(true) });
  assert.equal(deep.depth, 24);
  assert.match(deep.style, /--tf-depth:24/);
  const negative = treeRowAttrs({ depth: -3 });
  assert.equal(negative.depth, 0);
  assert.equal(negative.style, "");
});

// ── 提交图分叉 ───────────────────────────────────────────

test("linear history stays in one lane and ends at the root commit", () => {
  const graph = layoutCommitGraph([
    { id: "a", parents: ["b"] },
    { id: "b", parents: ["c"] },
    { id: "c", parents: [] }
  ]);
  assert.equal(graph.laneCount, 1);
  assert.equal(graph.dropped, 0);
  assert.deepEqual(graph.rows.map(row => row.lane), [0, 0, 0]);
  // 首行是新分支起点：只有向下的一段 + 提交点。
  assert.deepEqual(graph.rows[0].segments, [{ from: 0, fromY: 0.5, to: 0, toY: 1, lane: 0 }]);
  // 中段：上方来线 + 向下续线。
  assert.deepEqual(graph.rows[1].segments, [
    { from: 0, fromY: 0, to: 0, toY: 0.5, lane: 0 },
    { from: 0, fromY: 0.5, to: 0, toY: 1, lane: 0 }
  ]);
  // 根提交：只有上方来线，没有向下的边。
  assert.deepEqual(graph.rows[2].segments, [{ from: 0, fromY: 0, to: 0, toY: 0.5, lane: 0 }]);
  assert.deepEqual(graph.rows[2].parents, []);
});

test("merge commit forks into a second lane and the join collects both lanes", () => {
  const graph = layoutCommitGraph([
    { id: "a", parents: ["b", "c"] },
    { id: "b", parents: ["d"] },
    { id: "c", parents: ["d"] },
    { id: "d", parents: [] }
  ]);
  assert.equal(graph.laneCount, 2);
  assert.equal(graph.rows[0].merge, true);
  // merge 行：首父留在本泳道，第二父占用新泳道。
  assert.deepEqual(graph.rows[0].segments, [
    { from: 0, fromY: 0.5, to: 0, toY: 1, lane: 0 },
    { from: 0, fromY: 0.5, to: 1, toY: 1, lane: 1 }
  ]);
  // 旁支行：另一条泳道整行穿透。
  assert.ok(graph.rows[1].segments.some(seg => seg.from === 1 && seg.fromY === 0 && seg.to === 1 && seg.toY === 1));
  // join 行：两条泳道汇入同一提交点，旁路泳道释放。
  const join = graph.rows[3];
  assert.equal(join.lane, 0);
  const merges = join.segments.filter(seg => seg.from === 1 && seg.to === 0 && seg.toY === 0.5);
  assert.equal(merges.length, 1);
  assert.equal(merges[0].lane, 0);
});

test("a branch tip without an incoming edge opens a free lane", () => {
  const graph = layoutCommitGraph([
    { id: "main2", parents: ["base"] },
    { id: "side2", parents: ["side1"] },
    { id: "base", parents: [] },
    { id: "side1", parents: ["base2"] },
    { id: "base2", parents: [] }
  ]);
  assert.equal(graph.laneCount, 2);
  assert.equal(graph.rows[0].lane, 0);
  assert.equal(graph.rows[1].lane, 1);
  // base 由 main2 与 side1 两条边先后等待 → 先到先得（lane 0）。
  assert.equal(graph.rows[2].lane, 0);
  assert.equal(graph.rows[3].lane, 1);
  // base2 只被 side1 等待 → 落回同一条泳道（泳道号对续行稳定，不复用空位）。
  assert.equal(graph.rows[4].lane, 1);
});

test("lane cap drops surplus edges instead of overflowing the panel", () => {
  const commits = [];
  for (let index = 0; index < 12; index++) commits.push({ id: `tip${index}`, parents: [`p${index}a`, `p${index}b`] });
  const graph = layoutCommitGraph(commits, { maxLanes: 3 });
  assert.ok(graph.laneCount <= 3);
  assert.ok(graph.dropped > 0);
  assert.ok(graph.rows.every(row => row.lanes === graph.laneCount));
});

test("malformed commit payloads are ignored, never rendered as NaN", () => {
  const graph = layoutCommitGraph([
    null,
    "junk",
    { id: "", parents: ["x"] },
    { id: "a", parents: "not-an-array" },
    { id: "b", parents: [null, "c"] }
  ]);
  assert.deepEqual(graph.rows.map(row => row.id), ["a", "b"]);
  assert.deepEqual(graph.rows[0].parents, []);
  assert.deepEqual(graph.rows[1].parents, ["c"]);
  const html = renderCommitGraph(graph.rows);
  assert.ok(!html.includes("NaN"));
  assert.ok(!html.includes("undefined"));
});

test("empty input yields an empty graph with a single lane column", () => {
  const graph = layoutCommitGraph(null);
  assert.deepEqual(graph.rows, []);
  assert.equal(graph.laneCount, 1);
  assert.equal(graph.dropped, 0);
  assert.equal(renderCommitGraph(graph.rows), "");
});

// ── 提交图 SVG ──────────────────────────────────────────

test("row SVG draws straight edges, curves and a commit dot", () => {
  const graph = layoutCommitGraph([
    { id: "a", parents: ["b", "c"] },
    { id: "b", parents: [] },
    { id: "c", parents: [] }
  ]);
  const first = commitGraphRowHTML(graph.rows[0], { laneWidth: 16, rowHeight: 22 });
  assert.match(first, /<svg class="tf-fork-svg" width="32" height="22" viewBox="0 0 32 22"/);
  assert.match(first, /<path class="tf-fork-line" data-tf-lane="0" d="M8 11V22"\/>/);
  assert.match(first, /<path class="tf-fork-line" data-tf-lane="1" d="M8 11C8 16\.5 24 16\.5 24 22"\/>/);
  assert.match(first, /<circle class="tf-fork-dot" data-tf-lane="0" cx="8" cy="11" r="3\.2"\/>/);
});

test("lane colors cycle through the palette tokens", () => {
  assert.equal(forkLaneToken(0), 0);
  assert.equal(forkLaneToken(FORK_LANE_TOKENS), 0);
  assert.equal(forkLaneToken(FORK_LANE_TOKENS + 2), 2);
  assert.equal(forkLaneToken(-5), 0);
  const row = { lane: 7, lanes: 8, segments: [{ from: 7, fromY: 0, to: 7, toY: 1, lane: 7 }] };
  assert.match(commitGraphRowHTML(row), /data-tf-lane="1"/);
});

test("row geometry helpers agree with the rendered column width", () => {
  assert.equal(laneCenter(0), 8);
  assert.equal(laneCenter(2), 40);
  assert.equal(forkGraphWidth(3), 48);
  assert.equal(forkGraphWidth(3, { laneWidth: 20 }), 60);
  assert.equal(forkGraphWidth(0), 16);
});

test("junk row payloads render a bare SVG instead of throwing", () => {
  const html = commitGraphRowHTML({ lane: "x", lanes: "y", segments: [null, { from: "bad", to: undefined, fromY: "?", toY: 9 }] });
  assert.ok(html.startsWith("<svg class=\"tf-fork-svg\""));
  assert.ok(!html.includes("NaN"));
  assert.ok(!html.includes("Infinity"));
});
