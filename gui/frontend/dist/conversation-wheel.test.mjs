import test from "node:test";
import assert from "node:assert/strict";
import {
  activeRoundIndex,
  buildWheelRounds,
  normalizeWheelKind,
  roundAtOffset,
  scrollTopForFraction,
  wheelAnchors,
  wheelSignature
} from "./conversation-wheel.js";

test("wheel: kinds normalize to the canonical set", () => {
  assert.equal(normalizeWheelKind("user"), "user");
  assert.equal(normalizeWheelKind("assistant"), "agent");
  assert.equal(normalizeWheelKind("llm"), "agent");
  assert.equal(normalizeWheelKind("thinking"), "think");
  assert.equal(normalizeWheelKind("tool_result"), "tools");
  assert.equal(normalizeWheelKind("axis"), "tools");
  assert.equal(normalizeWheelKind("error"), "system");
  assert.equal(normalizeWheelKind(""), "other");
  assert.equal(normalizeWheelKind(undefined), "other");
});

test("wheel: anchors hang on user turns, one dash per question", () => {
  const entries = [
    { key: "message:1", kind: "user", label: "你 · 第一个问题" },
    { key: "roll:1", kind: "tools", label: "工具过程" },
    { key: "message:2", kind: "agent", label: "Seelex · 回答" },
    { key: "message:3", kind: "user", label: "你 · 第二个问题" },
    { key: "message:4", kind: "agent", label: "Seelex · 回答" }
  ];
  const anchors = wheelAnchors(entries);
  assert.equal(anchors.mode, "turn");
  assert.deepEqual(anchors.anchors.map(entry => entry.key), ["message:1", "message:3"]);
});

test("wheel: a window without user turns falls back to assistant steps", () => {
  // 长会话翻到中段：窗口里全是一轮里的工具与思考，没有 user 轮。
  const entries = [
    { key: "roll:9", kind: "tools", label: "工具过程 · 12 次" },
    { key: "message:30", kind: "think", label: "Seelex · 先读文件" },
    { key: "roll:10", kind: "tools", label: "工具过程 · 3 次" },
    { key: "message:31", kind: "agent", label: "Seelex · 这一段结论" }
  ];
  const anchors = wheelAnchors(entries);
  assert.equal(anchors.mode, "step");
  assert.deepEqual(anchors.anchors.map(entry => entry.key), ["message:30", "message:31"]);
  assert.equal(wheelAnchors([{ key: "roll:9", kind: "tools" }]).mode, "none");
});

test("wheel: one dash per question, positioned by real geometry", () => {
  const entries = [
    { key: "message:1", kind: "user", label: "你 · 目标", offsetTop: 0, height: 100 },
    { key: "roll:1", kind: "tools", offsetTop: 100, height: 600 },
    { key: "message:2", kind: "agent", offsetTop: 700, height: 300 },
    { key: "message:3", kind: "user", label: "你 · 追问", offsetTop: 1000, height: 80 },
    { key: "roll:2", kind: "tools", offsetTop: 1080, height: 920 }
  ];
  const result = buildWheelRounds(entries, { scrollHeight: 2000, trackHeight: 500 });
  assert.equal(result.empty, false);
  assert.equal(result.mode, "turn");
  assert.equal(result.rounds.length, 2);
  assert.equal(result.rounds[0].key, "message:1");
  assert.equal(result.rounds[1].key, "message:3");
  assert.equal(result.rounds[0].top, 0);
  // 1000/2000 = 一半 → 轨道 497 可用高度的一半。
  assert.equal(result.rounds[1].top, 248.5);
  // 回答区间归到所属问答：第一段一直铺到第二个问题。
  assert.equal(result.rounds[0].offsetBottom, 1000);
  assert.equal(result.rounds[1].offsetBottom, 2000);
});

test("wheel: dashes keep a minimum gap so short rounds stay clickable", () => {
  // 20 条同高相邻问答：按比例会全部叠在顶部，必须有最小间距。
  const entries = Array.from({ length: 20 }, (_, index) => ({
    key: `message:${index + 1}`, kind: "user", label: `问题 ${index + 1}`,
    offsetTop: index * 10, height: 10
  }));
  const { rounds } = buildWheelRounds(entries, { scrollHeight: 10000, trackHeight: 300, minGap: 11 });
  for (let index = 1; index < rounds.length; index += 1) {
    assert.ok(rounds[index].top - rounds[index - 1].top >= 11,
      `dash ${index} overlaps: ${rounds[index - 1].top} -> ${rounds[index].top}`);
  }
  // 整条轨道都排得下，不溢出、也不越界。
  assert.ok(rounds[rounds.length - 1].top + rounds[rounds.length - 1].height <= 300);
  for (const round of rounds) assert.ok(round.top >= 0);
});

test("wheel: crowded rails compress the gap instead of overflowing", () => {
  const entries = Array.from({ length: 60 }, (_, index) => ({
    key: `message:${index + 1}`, kind: "user", offsetTop: index, height: 4
  }));
  const { rounds } = buildWheelRounds(entries, { scrollHeight: 6000, trackHeight: 200, minGap: 11 });
  assert.equal(rounds.length, 60);
  for (const round of rounds) assert.ok(round.top + round.height <= 200);
  assert.equal(rounds[0].top, 0);
  assert.ok(rounds[59].top > rounds[0].top);
});

test("wheel: empty geometry renders no dashes and hides the rail", () => {
  assert.equal(buildWheelRounds([], { scrollHeight: 100, trackHeight: 100 }).empty, true);
  assert.equal(buildWheelRounds([{ key: "a", kind: "user", offsetTop: 0, height: 10 }], { scrollHeight: 0, trackHeight: 100 }).empty, true);
  assert.equal(buildWheelRounds([{ key: "a", kind: "user", offsetTop: 0, height: 10 }], { scrollHeight: 100, trackHeight: 0 }).empty, true);
  assert.equal(
    buildWheelRounds([{ key: "a", kind: "tools", offsetTop: 0, height: 10 }], { scrollHeight: 100, trackHeight: 100 }).empty,
    true
  );
});

test("wheel: active dash follows the reading position", () => {
  const entries = [
    { key: "message:1", kind: "user", offsetTop: 0, height: 100 },
    { key: "message:2", kind: "user", offsetTop: 1000, height: 100 },
    { key: "message:3", kind: "user", offsetTop: 2000, height: 100 }
  ];
  const { rounds } = buildWheelRounds(entries, { scrollHeight: 3000, trackHeight: 400 });
  assert.equal(activeRoundIndex(rounds, { scrollTop: 0, clientHeight: 500 }), 0);
  // 参考线 = scrollTop + 40% 视口：800 + 200 = 1000 才越过第二个问题。
  assert.equal(activeRoundIndex(rounds, { scrollTop: 799, clientHeight: 500 }), 0);
  assert.equal(activeRoundIndex(rounds, { scrollTop: 800, clientHeight: 500 }), 1);
  assert.equal(activeRoundIndex(rounds, { scrollTop: 1800, clientHeight: 500 }), 2);
  assert.equal(activeRoundIndex([], { scrollTop: 0, clientHeight: 500 }), -1);
});

test("wheel: hover hit-tests the dash then the nearest one", () => {
  const { rounds } = buildWheelRounds([
    { key: "message:1", kind: "user", offsetTop: 0, height: 100 },
    { key: "message:2", kind: "user", offsetTop: 1000, height: 100 }
  ], { scrollHeight: 2000, trackHeight: 400 });
  assert.equal(roundAtOffset(rounds, rounds[0].top).key, "message:1");
  assert.equal(roundAtOffset(rounds, rounds[0].top + rounds[0].height).key, "message:1");
  // 紧贴刻度下方的空白仍算第一条（3px 的线要点击得中）。
  assert.equal(roundAtOffset(rounds, rounds[0].top + rounds[0].height + 1).key, "message:1");
  // 靠近第二条时命中第二条。
  assert.equal(roundAtOffset(rounds, rounds[1].top - 30).key, "message:2");
  assert.equal(roundAtOffset([], 10), null);
});

test("wheel: clicking the rail maps a fraction onto the scroll range", () => {
  const box = { scrollHeight: 1000, clientHeight: 200 };
  assert.equal(scrollTopForFraction(0, box), 0);
  assert.equal(scrollTopForFraction(0.5, box), 400);
  assert.equal(scrollTopForFraction(1, box), 800);
  assert.equal(scrollTopForFraction(2, box), 800);
  assert.equal(scrollTopForFraction(-1, box), 0);
  assert.equal(scrollTopForFraction(0.5, { scrollHeight: 100, clientHeight: 100 }), 0);
});

test("wheel: signature changes only when the dash geometry changes", () => {
  const base = [
    { key: "message:1", kind: "user", top: 0 },
    { key: "message:2", kind: "user", top: 120 }
  ];
  assert.equal(wheelSignature(base), wheelSignature([...base.map(round => ({ ...round }))]));
  assert.notEqual(wheelSignature(base), wheelSignature([base[0], { ...base[1], top: 130 }]));
  assert.notEqual(wheelSignature(base), wheelSignature([base[0]]));
  assert.equal(wheelSignature(undefined), "");
});
