import assert from "node:assert/strict";
import test from "node:test";

import { TITLE_TAILS_KEY, truncateTitle, duplicateSuffix, titleSuffix, readTitleTails, writeTitleTails } from "./sidebar.js";

function fakeStorage(initial = {}) {
  const map = new Map(Object.entries(initial));
  return {
    getItem: key => (map.has(key) ? map.get(key) : null),
    setItem: (key, value) => map.set(key, String(value))
  };
}

test("truncateTitle keeps short titles unchanged", () => {
  assert.equal(truncateTitle("你好", 5), "你好");
  assert.equal(truncateTitle("abcde", 5), "abcde");
});

test("truncateTitle cuts long titles to 5 code points plus ellipsis", () => {
  assert.equal(truncateTitle("abcdefgh", 5), "abcde…");
  assert.equal(truncateTitle("修复首页样式崩溃问题", 5), "修复首页样…");
});

test("truncateTitle handles surrogate pairs (emoji) without splitting", () => {
  assert.equal(truncateTitle("😀😀😀😀😀😀", 5), "😀😀😀😀😀…");
  assert.equal(truncateTitle("a😀b😀c😀d", 5), "a😀b😀c…");
});

test("duplicateSuffix appends ordinal only for repeated names", () => {
  assert.equal(duplicateSuffix(1, 1), "");
  assert.equal(duplicateSuffix(2, 1), "");
  assert.equal(duplicateSuffix(1, 3), " (1)");
  assert.equal(duplicateSuffix(2, 3), " (2)");
  assert.equal(duplicateSuffix(3, 3), " (3)");
  // 非法输入不崩溃。
  assert.equal(duplicateSuffix(0, 2), "");
  assert.equal(duplicateSuffix(null, 2), "");
});

test("titleSuffix only numbers duplicates (1 stays plain)", () => {
  assert.equal(titleSuffix(1), "");
  assert.equal(titleSuffix(2), " (2)");
  assert.equal(titleSuffix(3), " (3)");
  assert.equal(titleSuffix(0), "");
  assert.equal(titleSuffix(null), "");
});

test("title tails persist as name -> tail number key-value pair", () => {
  const storage = fakeStorage();
  assert.deepEqual(readTitleTails(storage), {});
  writeTitleTails({ "你好": 3, "审查": 2 }, storage);
  assert.equal(storage.getItem(TITLE_TAILS_KEY), JSON.stringify({ "你好": 3, "审查": 2 }));
  assert.deepEqual(readTitleTails(storage), { "你好": 3, "审查": 2 });
  // 损坏存档回退空。
  const broken = fakeStorage({ [TITLE_TAILS_KEY]: "not-json" });
  assert.deepEqual(readTitleTails(broken), {});
});

test("truncateTitle tolerates null/undefined/non-string", () => {
  assert.equal(truncateTitle(null, 5), "");
  assert.equal(truncateTitle(undefined, 5), "");
  assert.equal(truncateTitle(12345, 3), "123…");
});

// 置顶/别名不再是本模块职责：它们作为会话展示元数据由后端持久化，侧栏只读快照
// 下发的 session.meta（写入经 Bridge.SetSessionMeta），因此 localStorage 辅助
// 函数已整体删除。
