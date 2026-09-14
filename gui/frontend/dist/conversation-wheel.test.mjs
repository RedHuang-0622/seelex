import test from "node:test";
import assert from "node:assert/strict";
import {
  activeInputIndex,
  inputAtOffset,
  inputDashes,
  inWindowInput,
  isUserWheelKind,
  linkInputRows,
  locateInput,
  normalizeDomUserRows,
  normalizeInputIndex,
  planInputLocate,
  scrollTopForFraction,
  wheelSignature
} from "./conversation-wheel.js";

// indexItems 构造后端 SessionInputIndex 载荷（只含用户输入刻度）。
function indexItems(rounds, options = {}) {
  return {
    session_id: "session-a",
    total: options.total ?? 40,
    input_count: rounds.length,
    window: {
      offset: options.windowOffset ?? 0,
      count: options.windowCount ?? 0,
      total: options.total ?? 40,
      has_more: Boolean(options.windowOffset),
      window_size: options.windowSize ?? 10
    },
    items: rounds.map((round, position) => ({
      round,
      message_id: `m${round}`,
      offset: options.offsets ? options.offsets[position] : round * 2,
      round_id: round,
      seq: round * 3,
      created_at: "2026-09-11T10:00:00Z",
      summary: `问题 ${round}`,
      chars: 40,
      loaded: false
    }))
  };
}

// domRow 构造一行 DOM 测量结果（kind = components.js 的 data-wheel-kind）。
function domRow(key, kind, offsetTop, height = 100, label = "") {
  return { key, kind, offsetTop, height, label };
}

test("wheel index: only user inputs are recognized (multi-kind dashes are gone)", () => {
  assert.equal(isUserWheelKind("user"), true);
  assert.equal(isUserWheelKind(" USER "), true);
  // 助手步骤 / 思考 / 工具 / 系统行都不再是刻度类别。
  assert.equal(isUserWheelKind("agent"), false);
  assert.equal(isUserWheelKind("think"), false);
  assert.equal(isUserWheelKind("tools"), false);
  assert.equal(isUserWheelKind("system"), false);
  assert.equal(isUserWheelKind(""), false);
  assert.equal(isUserWheelKind(undefined), false);

  const rows = normalizeDomUserRows([
    domRow("message:m1", "user", 0),
    domRow("roll:1", "tools", 100),
    domRow("message:m2", "agent", 400),
    domRow("message:m3", "think", 700),
    domRow("chat:activity", "system", 900)
  ]);
  assert.deepEqual(rows.map(row => row.key), ["message:m1"]);
});

test("wheel index: a window with no user row renders nothing (no assistant fallback)", () => {
  // 长会话翻到中段：窗口里全是思考/工具/助手步骤，没有任何 user 轮。
  const rows = [
    domRow("roll:9", "tools", 0),
    domRow("message:30", "think", 200),
    domRow("message:31", "agent", 400)
  ];
  const result = inputDashes([], rows, { scrollHeight: 1000, trackHeight: 400 });
  assert.equal(result.empty, true);
  assert.deepEqual(result.rounds, []);
});

test("wheel index: the full index yields one dash per user input, early rounds included", () => {
  const payload = indexItems([1, 2, 3, 4, 5], { windowOffset: 6, windowCount: 4, total: 12, windowSize: 4, offsets: [0, 2, 4, 6, 8] });
  const { items, window } = normalizeInputIndex(payload);
  assert.equal(items.length, 5);
  assert.equal(window.windowSize, 4);
  // 只有第 4/5 条落在已加载窗口里（窗口 = 偏移 [6,10)）。
  assert.deepEqual(items.map(item => inWindowInput(item, window)), [false, false, false, true, true]);

  const result = inputDashes(items, [domRow("message:m4", "user", 100), domRow("message:m5", "user", 900)], {
    scrollHeight: 1000, trackHeight: 500, window
  });
  assert.equal(result.empty, false);
  assert.equal(result.rounds.length, 5);
  assert.equal(result.loaded, 2);
  assert.equal(result.rounds[3].loaded, true);
  assert.equal(result.rounds[3].key, "message:m4");
  // 已加载刻度 = 真实几何：100/1000 与 900/1000 的内容比例 × 497 可用高度。
  assert.equal(result.rounds[3].top, 49.7);
  assert.equal(result.rounds[4].top, 447.3);
  // 未加载的早期轮次按确定性比例落在窗口之上，且顺序不变。
  assert.deepEqual(result.rounds.slice(0, 3).map(round => round.loaded), [false, false, false]);
  assert.deepEqual(result.rounds.slice(0, 3).map(round => round.top), [12.43, 24.85, 37.28]);
  for (let index = 1; index < result.rounds.length; index += 1) {
    assert.ok(result.rounds[index].top > result.rounds[index - 1].top, `dash ${index} not ordered`);
  }
});

test("wheel index: dashes in a window without any loaded user input use deterministic interpolation", () => {
  const { items, window } = normalizeInputIndex(indexItems([1, 2, 3], { windowOffset: 12, windowCount: 4, total: 16, windowSize: 4 }));
  const rows = [domRow("message:x9", "agent", 0, 300), domRow("roll:9", "tools", 300, 700)];
  const first = inputDashes(items, rows, { scrollHeight: 1000, trackHeight: 400, window });
  const second = inputDashes(items, rows, { scrollHeight: 1000, trackHeight: 400, window });
  assert.equal(first.rounds.length, 3);
  assert.equal(first.loaded, 0);
  // 没有任何已加载锚点：在 (0, 0) → (N+1, 1) 之间等距插值（0.25 / 0.5 / 0.75）。
  assert.deepEqual(first.rounds.map(round => round.progress), [0.25, 0.5, 0.75]);
  assert.deepEqual(first, second, "same inputs must give the same layout");
});

test("wheel index: a stale loaded flag cannot move a dash that the window offsets exclude", () => {
  const payload = indexItems([1, 2, 3], { windowOffset: 10, windowCount: 4, total: 20, windowSize: 4, offsets: [0, 2, 12] });
  payload.items[0].loaded = true; // 后端逐项标志过期：窗口偏移区间才是准的
  const { items, window } = normalizeInputIndex(payload);
  assert.equal(inWindowInput(items[0], window), false);
  assert.equal(inWindowInput(items[2], window), true);
  // 没有窗口信息时退回逐项标志。
  assert.equal(inWindowInput(items[0], null), true);
});

test("wheel index: linking prefers message id, then falls back to ordered pairing", () => {
  const { items, window } = normalizeInputIndex(indexItems([1, 2, 3], { windowOffset: 0, windowCount: 3, total: 3, windowSize: 3, offsets: [0, 1, 2] }));
  const rows = normalizeDomUserRows([
    domRow("message:m1", "user", 0),
    domRow("message:8", "user", 300),
    domRow("message:9", "user", 600)
  ]);
  const links = linkInputRows(items, rows, window);
  // 第 1 条按 message_id 精确命中；剩余两条按顺序补配（旧数据 key 是窗口序号）。
  assert.equal(links.get(1).key, "message:m1");
  assert.equal(links.get(2).key, "message:8");
  assert.equal(links.get(3).key, "message:9");

  // 剩余条目数与剩余行数不等时不做位置配对：宁可不标（点击时回读），也不错位。
  const partial = linkInputRows(items, normalizeDomUserRows([domRow("message:8", "user", 0)]), window);
  assert.equal(partial.size, 0);
});

test("wheel index: minimum gap keeps short rounds clickable without overflowing", () => {
  const rounds = indexItems(Array.from({ length: 20 }, (_, index) => index + 1), { total: 40 }).items;
  const { rounds: dashes } = inputDashes(normalizeInputIndex({ items: rounds }).items, [], {
    scrollHeight: 10000, trackHeight: 300, minGap: 11
  });
  assert.equal(dashes.length, 20);
  for (let index = 1; index < dashes.length; index += 1) {
    assert.ok(dashes[index].top - dashes[index - 1].top >= 11,
      `dash ${index} overlaps: ${dashes[index - 1].top} -> ${dashes[index].top}`);
  }
  assert.ok(dashes[dashes.length - 1].top + dashes[dashes.length - 1].height <= 300);
  for (const dash of dashes) assert.ok(dash.top >= 0);
});

test("wheel index: crowded rails compress the gap instead of overflowing", () => {
  const payload = normalizeInputIndex({
    items: Array.from({ length: 60 }, (_, index) => ({ round: index + 1, offset: index, summary: `q${index}` }))
  });
  const { rounds } = inputDashes(payload.items, [], { scrollHeight: 6000, trackHeight: 200, minGap: 11 });
  assert.equal(rounds.length, 60);
  for (const round of rounds) assert.ok(round.top + round.height <= 200);
  // 第一条输入在 (0,0) 锚点之后：位置为正但贴近轨道顶端。
  assert.ok(rounds[0].top > 0 && rounds[0].top < 10);
  assert.ok(rounds[59].top > rounds[0].top);
});

test("wheel index: empty geometry or an empty index hides the rail", () => {
  assert.equal(inputDashes([], [], { scrollHeight: 100, trackHeight: 100 }).empty, true);
  assert.equal(inputDashes([{ round: 1, offset: 0 }], [], { scrollHeight: 0, trackHeight: 100 }).empty, true);
  assert.equal(inputDashes([{ round: 1, offset: 0 }], [], { scrollHeight: 100, trackHeight: 0 }).empty, true);
  const empty = inputDashes([], [], { scrollHeight: 100, trackHeight: 100 });
  assert.equal(empty.rounds.length, 0);
  assert.equal(empty.empty, true);
});

test("wheel index: hover hit-tests the dash then the nearest one", () => {
  const { items } = normalizeInputIndex(indexItems([1, 2], { offsets: [0, 2] }));
  const { rounds } = inputDashes(items, [domRow("message:m1", "user", 0), domRow("message:m2", "user", 1000)], {
    scrollHeight: 2000, trackHeight: 400
  });
  assert.equal(inputAtOffset(rounds, rounds[0].top).round, 1);
  assert.equal(inputAtOffset(rounds, rounds[0].top + rounds[0].height).round, 1);
  // 紧贴刻度下方的空白仍算第一条（3px 的线要点击得中）。
  assert.equal(inputAtOffset(rounds, rounds[0].top + rounds[0].height + 1).round, 1);
  assert.equal(inputAtOffset(rounds, rounds[1].top - 30).round, 2);
  assert.equal(inputAtOffset([], 10), null);
});

test("wheel index: active dash follows the reading position", () => {
  const { items } = normalizeInputIndex(indexItems([1, 2, 3], { offsets: [0, 1, 2] }));
  const { rounds } = inputDashes(items, [
    domRow("message:m1", "user", 0), domRow("message:m2", "user", 1000), domRow("message:m3", "user", 2000)
  ], { scrollHeight: 3000, trackHeight: 400 });
  assert.equal(activeInputIndex(rounds, { scrollTop: 0, clientHeight: 500 }), 0);
  // 参考线 = scrollTop + 40% 视口：800 + 200 = 1000 才越过第二条输入。
  assert.equal(activeInputIndex(rounds, { scrollTop: 799, clientHeight: 500 }), 0);
  assert.equal(activeInputIndex(rounds, { scrollTop: 800, clientHeight: 500 }), 1);
  assert.equal(activeInputIndex(rounds, { scrollTop: 1800, clientHeight: 500 }), 2);
  assert.equal(activeInputIndex([], { scrollTop: 0, clientHeight: 500 }), -1);
});

test("wheel index: locate decision picks scroll inside the window, read-back outside", () => {
  const { items } = normalizeInputIndex(indexItems([1, 2, 3], { windowOffset: 40, windowCount: 10, total: 60, windowSize: 10, offsets: [1, 21, 45] }));
  const { rounds } = inputDashes(items, [domRow("message:m3", "user", 100)], { scrollHeight: 1000, trackHeight: 400 });
  // 第 3 条在窗口里 → 直接滚动。
  const inside = planInputLocate(rounds, 3, { windowOffset: 40, windowSize: 10 });
  assert.equal(inside.action, "scroll");
  assert.equal(inside.key, "message:m3");
  // 第 2 条在窗口外（偏移 21，窗口从 40 开始）→ 回读 ceil((40-21)/10) = 2 页。
  const outside = planInputLocate(rounds, 2, { windowOffset: 40, windowSize: 10 });
  assert.equal(outside.action, "read-back");
  assert.equal(outside.pages, 2);
  assert.equal(outside.messageID, "m2");
  assert.equal(outside.offset, 21);
  // 第 1 条同样在窗口外，且页数被上限截断。
  assert.equal(planInputLocate(rounds, 1, { windowOffset: 40, windowSize: 10 }).pages, 4);
  assert.equal(planInputLocate(rounds, 1, { windowOffset: 40, windowSize: 10, maxPages: 3 }).pages, 3);
  // 没有窗口信息 → 至少回读一页（而不是干脆不定位）。
  assert.equal(planInputLocate(rounds, 1, {}).pages, 1);
  assert.equal(planInputLocate(rounds, 99, {}).action, "missing");
});

test("wheel index: locate runs the read-back loop then scrolls and highlights", async () => {
  const timeline = [];
  const classes = new Set();
  const node = {
    classList: {
      add: value => classes.add(value),
      remove: value => classes.delete(value)
    },
    scrollIntoView: options => timeline.push(options)
  };
  const decision = { action: "read-back", round: 2, index: 1, key: "", messageID: "m2", offset: 21, pages: 2 };
  let pages = 0;
  let available = null;
  const ok = await locateInput(decision, {
    findNode: () => available,
    loadPage: () => { pages += 1; available = pages >= 2 ? node : null; return true; },
    schedule: () => {},
    scrollTo: (target, behavior) => timeline.push(`${behavior}:${target === node}`)
  });
  assert.equal(ok, true);
  assert.equal(pages, 2);
  assert.deepEqual(timeline, ["smooth:true"]);
  assert.equal(classes.has("is-wheel-target"), true);

  // DOM 里已经有目标：不回读，直接滚动。
  let directPages = 0;
  const direct = await locateInput({ action: "scroll", round: 1, messageID: "m1", pages: 0 }, {
    findNode: () => node,
    loadPage: () => { directPages += 1; return true; },
    schedule: () => {}
  });
  assert.equal(direct, true);
  assert.equal(directPages, 0);

  // 没有回读通道 / 页数用尽：返回 false（视图不动），不抛错。
  const blocked = await locateInput({ action: "read-back", round: 1, pages: 3 }, { findNode: () => null });
  assert.equal(blocked, false);
  let exhausted = 0;
  const gone = await locateInput({ action: "read-back", round: 1, pages: 5 }, {
    findNode: () => null,
    loadPage: () => { exhausted += 1; return false; },
    schedule: () => {}
  });
  assert.equal(gone, false);
  assert.equal(exhausted, 1);
  assert.equal(await locateInput({ action: "missing", round: 9 }, { findNode: () => node }), false);
});

test("wheel index: rail fractions map onto the scroll range", () => {
  const box = { scrollHeight: 1000, clientHeight: 200 };
  assert.equal(scrollTopForFraction(0, box), 0);
  assert.equal(scrollTopForFraction(0.5, box), 400);
  assert.equal(scrollTopForFraction(1, box), 800);
  assert.equal(scrollTopForFraction(2, box), 800);
  assert.equal(scrollTopForFraction(-1, box), 0);
  assert.equal(scrollTopForFraction(0.5, { scrollHeight: 100, clientHeight: 100 }), 0);
});

test("wheel index: signature changes only when the dash geometry changes", () => {
  const base = [
    { round: 1, top: 0, loaded: true, summary: "第一问" },
    { round: 2, top: 120, loaded: false, summary: "第二问" }
  ];
  assert.equal(wheelSignature(base), wheelSignature([...base.map(round => ({ ...round }))]));
  assert.notEqual(wheelSignature(base), wheelSignature([base[0], { ...base[1], top: 130 }]));
  assert.notEqual(wheelSignature(base), wheelSignature([base[0], { ...base[1], loaded: true }]));
  assert.notEqual(wheelSignature(base), wheelSignature([base[0], { ...base[1], summary: "改了摘要" }]));
  assert.notEqual(wheelSignature(base), wheelSignature([base[0]]));
  assert.equal(wheelSignature(undefined), "");
});

test("wheel index: payload normalization tolerates the bridge JSON shape", () => {
  const { items, window } = normalizeInputIndex({
    session_id: "s", total: 9, input_count: 2, window: { offset: 3, count: 3, total: 9, has_more: true, window_size: 6 },
    items: [
      { round: 2, message_id: null, offset: "5", summary: " 多   空白\n摘要 ", chars: 12, loaded: 1 },
      { round: 1, message_id: "m1", offset: 2, summary: "第一问", loaded: false },
      null
    ]
  });
  assert.equal(items.length, 2);
  assert.deepEqual(items.map(item => item.round), [1, 2]);
  assert.equal(items[1].messageID, "");
  assert.equal(items[1].offset, 5);
  assert.equal(items[1].summary, "多 空白 摘要");
  assert.equal(items[1].loaded, true);
  assert.equal(window.hasMore, true);
  assert.equal(window.windowSize, 6);
  assert.deepEqual(normalizeInputIndex(null), { items: [], window: null });
});
