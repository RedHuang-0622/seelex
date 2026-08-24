import assert from "node:assert/strict";
import test from "node:test";

import { PIN_STORAGE_KEY, truncateTitle, readPinnedSessions, writePinnedSessions, isPinned, togglePinned } from "./sidebar.js";

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

test("truncateTitle tolerates null/undefined/non-string", () => {
  assert.equal(truncateTitle(null, 5), "");
  assert.equal(truncateTitle(undefined, 5), "");
  assert.equal(truncateTitle(12345, 3), "123…");
});

test("pinned helpers persist through a fake storage", () => {
  const storage = fakeStorage();
  assert.deepEqual(readPinnedSessions(storage), []);
  assert.equal(isPinned("s1", storage), false);
  togglePinned("s1", storage);
  assert.equal(isPinned("s1", storage), true);
  assert.deepEqual(readPinnedSessions(storage), ["s1"]);
  togglePinned("s1", storage);
  assert.equal(isPinned("s1", storage), false);
  assert.deepEqual(readPinnedSessions(storage), []);
});

test("pinned helpers tolerate malformed stored JSON", () => {
  const storage = fakeStorage({ [PIN_STORAGE_KEY]: "not-json" });
  assert.deepEqual(readPinnedSessions(storage), []);
  assert.equal(isPinned("s1", storage), false);
});

test("togglePinned appends new ids and keeps order", () => {
  const storage = fakeStorage();
  togglePinned("b", storage);
  togglePinned("a", storage);
  assert.deepEqual(readPinnedSessions(storage), ["b", "a"]);
});

test("writePinnedSessions normalizes stored value to JSON string", () => {
  const storage = fakeStorage();
  writePinnedSessions(["s1", "s2"], storage);
  assert.equal(storage.getItem(PIN_STORAGE_KEY), JSON.stringify(["s1", "s2"]));
});
