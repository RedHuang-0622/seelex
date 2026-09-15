// tree-fork.js ── 树 / 分叉的统一渲染件（VS Code 观感）
//
// 纯函数（无 DOM、无 Bridge、无 localStorage），便于脱 DOM 单测：
//
//   1) 树轨 treeRowAttrs：文件树 / Plan 树 / 子代理树的层级连线不再用
//      「├─ / └─ / │」字符画，改为「祖先续行轨 + 末子圆角弯头」。实现刻意
//      做到**零额外 DOM**：续行轨 = 行内 1px linear-gradient 背景（每个续行
//      层一道），自身连接轨 = ::before 伪元素（末子为圆角弯头，非末子为整行
//      竖线）。样式在 styles.css 的「tree-fork」段，颜色只走语义 token。
//
//   2) 提交图分叉 layoutCommitGraph + commitGraphRowHTML：把 git 的 parents
//      拓扑算成泳道，逐行用 SVG 画直线/合并曲线 + 提交点，替代 `git --graph`
//      的 `* | \ /` 字符画。泳道色走 --fork-lane-N token（皮肤可换）。
//
// 两处的共同点：**拓扑事实只有一份**（后端下发的 depth/parents），渲染层
// 不再从字符画里反推结构。

const MAX_TREE_DEPTH = 24;
const MIN_LANE_WIDTH = 8;
const MAX_LANE_WIDTH = 32;

export const TREE_INDENT = 14;
export const TREE_ROW_HEIGHT = 22;
// 泳道数上限：超过就不再画线（记 dropped），避免畸形仓库把右栏撑爆。
export const MAX_FORK_LANES = 8;
// 泳道调色板长度（styles.css 里 --fork-lane-0..5 六档）。
export const FORK_LANE_TOKENS = 6;

// ── 树轨 ────────────────────────────────────────────────

// treeRowAttrs 把「层级 + 是否末子 + 祖先轨是否续行」折算成树行所需的
// class 与行内 style。
//   depth            0 = 根层（无轨道）
//   isLast           末子 → 圆角弯头；否则整行竖线
//   ancestorHasMore  第 level 层祖先是否还有后继兄弟（true = 该层竖线穿透本行）
export function treeRowAttrs(options = {}) {
  const depth = clampInt(options.depth, 0, MAX_TREE_DEPTH, 0);
  const indent = evenIndent(options.indent);
  const row = { depth, indent, className: "tf-row", style: "" };
  if (depth <= 0) return row;

  const isLast = options.isLast !== false;
  row.className = `tf-row tf-row--child tf-row--${isLast ? "elbow" : "line"}`;

  const flags = Array.isArray(options.ancestorHasMore) ? options.ancestorHasMore : [];
  const images = [];
  const sizes = [];
  const positions = [];
  const repeats = [];
  for (let level = 0; level < depth - 1; level++) {
    if (!flags[level]) continue;
    images.push("linear-gradient(var(--tree-rail), var(--tree-rail))");
    sizes.push("1px 100%");
    positions.push(`${railOffset(level, indent)}px 0`);
    repeats.push("no-repeat");
  }
  const decls = [`--tf-depth:${depth}`, `--tf-indent:${indent}px`];
  if (images.length) {
    decls.push(`background-image:${images.join(",")}`);
    decls.push(`background-size:${sizes.join(",")}`);
    decls.push(`background-position:${positions.join(",")}`);
    decls.push(`background-repeat:${repeats.join(",")}`);
  }
  row.style = decls.join(";");
  return row;
}

// railOffset 是第 level 层竖线的左边界（整数像素，与 styles.css 的
// ::before left 公式同源：level × indent + indent / 2 − 1）。
export function railOffset(level, indent = TREE_INDENT) {
  const step = evenIndent(indent);
  const value = clampInt(level, 0, MAX_TREE_DEPTH, 0);
  return value * step + step / 2 - 1;
}

// ── 提交图分叉 ───────────────────────────────────────────

// layoutCommitGraph 把「提交 + 父提交」拓扑算成逐行泳道（纯函数）。
// commits 需按 git 的拓扑序（新 → 旧）传入，每项 {id, parents: [id]}。
// 返回 {rows, laneCount, dropped}：
//   rows[i].lane      该提交落在哪条泳道（提交点）
//   rows[i].segments  本行要画的线段（fromY/toY 归一化 0/0.5/1 = 上/中/下）
//   rows[i].merge     是否合并提交
//   laneCount         全表泳道数（列宽取全表最大值，不随行抖动）
//   dropped           因泳道打满而未画线的提交/父边数量
export function layoutCommitGraph(commits, options = {}) {
  const maxLanes = clampInt(options.maxLanes, 1, 16, MAX_FORK_LANES);
  const list = Array.isArray(commits) ? commits : [];
  const waiting = []; // waiting[lane] = 该泳道正在等待的父提交 id（null = 空闲）
  const rows = [];
  let laneCount = 1;
  let dropped = 0;

  const firstFreeLane = () => {
    for (let lane = 0; lane < waiting.length; lane++) {
      if (waiting[lane] == null) return lane;
    }
    if (waiting.length >= maxLanes) return -1;
    waiting.push(null);
    return waiting.length - 1;
  };

  for (const commit of list) {
    const id = textValue(commit?.id);
    if (!id) continue;

    // 有子提交在本行收口（可能多条：merge 的汇入）→ 取最左的那条作为落点。
    const incoming = [];
    for (let lane = 0; lane < waiting.length; lane++) {
      if (waiting[lane] === id) incoming.push(lane);
    }
    const lane = incoming.length ? incoming[0] : firstFreeLane();
    if (lane < 0) {
      dropped += 1;
      continue;
    }
    for (const from of incoming) {
      if (from !== lane) waiting[from] = null; // 汇入的旁路泳道本行收口，释放
    }

    const segments = [];
    // 汇入边：从上方落到本行提交点。
    for (const from of incoming) {
      segments.push({ from, fromY: 0, to: lane, toY: 0.5 });
    }
    // 无关泳道：整行穿透（分支继续往下）。
    for (let other = 0; other < waiting.length; other++) {
      if (other === lane || waiting[other] == null || waiting[other] === id) continue;
      segments.push({ from: other, fromY: 0, to: other, toY: 1 });
    }

    const parents = Array.isArray(commit.parents)
      ? commit.parents.map(textValue).filter(Boolean)
      : [];
    waiting[lane] = parents.length ? parents[0] : null;
    if (parents.length) {
      // 首父留在同泳道；其余父各自占一条空闲泳道（分叉）。
      segments.push({ from: lane, fromY: 0.5, to: lane, toY: 1 });
      for (const parent of parents.slice(1)) {
        const target = firstFreeLane();
        if (target < 0) {
          dropped += 1;
          continue;
        }
        waiting[target] = parent;
        segments.push({ from: lane, fromY: 0.5, to: target, toY: 1 });
      }
    }

    for (const segment of segments) {
      // 线段取色：同泳道取该泳道，跨泳道（汇入 / 分叉）取目标泳道。
      segment.lane = segment.from === segment.to ? segment.from : segment.to;
    }
    laneCount = Math.max(laneCount, lane + 1, waiting.length);
    rows.push({ id, lane, segments, merge: parents.length > 1, parents });
  }

  for (const row of rows) row.lanes = laneCount;
  return { rows, laneCount, dropped };
}

// commitGraphRowHTML 渲染一行提交图（一条 SVG：线段 + 提交点）。
export function commitGraphRowHTML(row, options = {}) {
  const laneWidth = evenLaneWidth(options.laneWidth);
  const rowHeight = clampInt(options.rowHeight, 12, 48, TREE_ROW_HEIGHT);
  const lanes = Math.max(1, Number(row?.lanes) || 1, (Number(row?.lane) || 0) + 1);
  const width = lanes * laneWidth;
  const mid = rowHeight / 2;
  const paths = [];
  for (const segment of Array.isArray(row?.segments) ? row.segments : []) {
    const x1 = laneCenter(segment?.from, laneWidth);
    const x2 = laneCenter(segment?.to, laneWidth);
    const y1 = clampUnit(segment?.fromY) * rowHeight;
    const y2 = clampUnit(segment?.toY) * rowHeight;
    const d = x1 === x2
      ? `M${x1} ${y1}V${y2}`
      : `M${x1} ${y1}C${x1} ${(y1 + y2) / 2} ${x2} ${(y1 + y2) / 2} ${x2} ${y2}`;
    paths.push(`<path class="tf-fork-line" data-tf-lane="${forkLaneToken(segment?.lane)}" d="${d}"/>`);
  }
  const dot = Number.isFinite(Number(row?.lane))
    ? `<circle class="tf-fork-dot" data-tf-lane="${forkLaneToken(row.lane)}" cx="${laneCenter(row.lane, laneWidth)}" cy="${mid}" r="3.2"/>`
    : "";
  return `<svg class="tf-fork-svg" width="${width}" height="${rowHeight}" viewBox="0 0 ${width} ${rowHeight}" aria-hidden="true" focusable="false">${paths.join("")}${dot}</svg>`;
}

// renderCommitGraph 渲染整张提交图（逐行 SVG 串联）。
export function renderCommitGraph(rows, options = {}) {
  return (Array.isArray(rows) ? rows : []).map(row => commitGraphRowHTML(row, options)).join("");
}

// forkGraphWidth 是提交图列的像素宽度（列宽固定，行与行对齐）。
export function forkGraphWidth(lanes, options = {}) {
  const laneWidth = evenLaneWidth(options.laneWidth);
  return Math.max(1, clampInt(lanes, 1, 64, 1)) * laneWidth;
}

// laneCenter 是某条泳道竖线的 x 中轴。
export function laneCenter(lane, laneWidth = 16) {
  const width = evenLaneWidth(laneWidth);
  const value = clampInt(lane, 0, 63, 0);
  return value * width + width / 2;
}

// forkLaneToken 把泳道号压到调色板档位（0..FORK_LANE_TOKENS-1）。
export function forkLaneToken(lane) {
  const value = clampInt(lane, 0, 4096, 0);
  return value % FORK_LANE_TOKENS;
}

function evenIndent(value) {
  const step = clampInt(value, 8, 32, TREE_INDENT);
  return step - (step % 2);
}

function evenLaneWidth(value) {
  const width = clampInt(value, MIN_LANE_WIDTH, MAX_LANE_WIDTH, 16);
  return width - (width % 2);
}

function clampUnit(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return 0;
  return Math.min(1, Math.max(0, number));
}

function clampInt(value, min, max, fallback) {
  const number = Number(value);
  if (!Number.isFinite(number)) return fallback;
  return Math.min(max, Math.max(min, Math.trunc(number)));
}

function textValue(value) {
  return typeof value === "string" ? value.trim() : "";
}
