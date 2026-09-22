import test from "node:test";
import assert from "node:assert/strict";
import {
  EXPLORER_PAGES,
  EXPLORER_PAGE_META,
  EXPLORER_STORAGE_KEY,
  LEGACY_PANE_ORDER_KEY,
  isExplorerPage,
  legacyPaneOrder,
  normalizeExplorerState,
  resolveExplorerState,
  serializeExplorerState,
  withExplorerPage
} from "./explorer-pages.js";

test("explorer: 默认状态是三个平级子页的一次排列，激活首个子页", () => {
  assert.deepEqual(EXPLORER_PAGES, ["worktree", "gitlog", "changes"]);
  const state = normalizeExplorerState(null);
  assert.deepEqual(state, { order: ["worktree", "gitlog", "changes"], active: "worktree" });
  assert.deepEqual(Object.keys(EXPLORER_PAGE_META), [...EXPLORER_PAGES]);
  // 存储键是契约：新键 + 旧键常量各只有一份事实。
  assert.equal(EXPLORER_STORAGE_KEY, "seelex.right.explorer.v1");
  assert.equal(LEGACY_PANE_ORDER_KEY, "seelex.right.codePanes");
});

test("explorer: 脏存储收敛——未知项丢弃、缺失项补齐、重复项只留一次", () => {
  const state = normalizeExplorerState({
    order: ["gitlog", "nope", "gitlog", null, "changes"],
    active: "nope"
  });
  assert.deepEqual(state.order, ["gitlog", "changes", "worktree"]);
  // 激活页非法时回退到可见的第一个页签，而不是回退到默认 id。
  assert.equal(state.active, "gitlog");
  assert.deepEqual(normalizeExplorerState({ order: "worktree" }).order, EXPLORER_PAGES);
  assert.deepEqual(normalizeExplorerState("脏值").order, EXPLORER_PAGES);
  assert.equal(normalizeExplorerState({ order: ["changes"], active: "changes" }).active, "changes");
});

test("explorer: 切换激活子页只改 active，落盘载荷始终收敛", () => {
  const base = normalizeExplorerState({ order: ["changes", "gitlog", "worktree"], active: "changes" });
  const next = withExplorerPage(base, "gitlog");
  assert.deepEqual(next.order, ["changes", "gitlog", "worktree"]);
  assert.equal(next.active, "gitlog");
  assert.equal(base.active, "changes", "纯函数不改入参");
  assert.equal(serializeExplorerState(next), JSON.stringify({ order: ["changes", "gitlog", "worktree"], active: "gitlog" }));
  // 脏入参也不会把脏值写回。
  assert.equal(serializeExplorerState({ order: ["gitlog"], active: 42 }), JSON.stringify({ order: ["gitlog", "worktree", "changes"], active: "gitlog" }));
  assert.equal(isExplorerPage("worktree"), true);
  assert.equal(isExplorerPage("code"), false);
});

test("explorer: 旧版面板顺序只有三项排列才认，两项/脏值一律不迁移", () => {
  assert.deepEqual(legacyPaneOrder(JSON.stringify(["gitlog", "changes", "worktree"])), ["gitlog", "changes", "worktree"]);
  assert.deepEqual(legacyPaneOrder(["changes", "worktree", "gitlog"]), ["changes", "worktree", "gitlog"]);
  // 旧的两项存储（工作树/提交记录）+ 重复项 + 未知 id + 坏 JSON。
  assert.equal(legacyPaneOrder(JSON.stringify(["worktree", "gitlog"])), null);
  assert.equal(legacyPaneOrder(JSON.stringify(["worktree", "worktree", "changes"])), null);
  assert.equal(legacyPaneOrder(JSON.stringify(["worktree", "gitlog", "nope"])), null);
  assert.equal(legacyPaneOrder("{不是 JSON"), null);
  assert.equal(legacyPaneOrder(null), null);
  assert.equal(legacyPaneOrder(""), null);
});

test("explorer: 新键优先，旧键仅在缺新键时消费，脏旧键回退默认", () => {
  const stored = JSON.stringify({ order: ["changes", "worktree", "gitlog"], active: "worktree" });
  const fromStored = resolveExplorerState(stored, JSON.stringify(["gitlog", "worktree", "changes"]));
  assert.deepEqual(fromStored.state, { order: ["changes", "worktree", "gitlog"], active: "worktree" });
  assert.equal(fromStored.migrated, false, "有新键就不再消费旧键");
});

test("explorer: 迁移旧三项顺序（旧首块 = 激活页），旧两项顺序安全回退默认", () => {
  const migrated = resolveExplorerState(null, JSON.stringify(["changes", "worktree", "gitlog"]));
  assert.deepEqual(migrated.state, { order: ["changes", "worktree", "gitlog"], active: "changes" });
  assert.equal(migrated.migrated, true);

  const legacyTwo = resolveExplorerState(null, JSON.stringify(["worktree", "gitlog"]));
  assert.deepEqual(legacyTwo.state, normalizeExplorerState(null));
  assert.equal(legacyTwo.migrated, true, "旧键存在即视为已消费（可以安全删掉）");

  const empty = resolveExplorerState(null, null);
  assert.deepEqual(empty.state, normalizeExplorerState(null));
  assert.equal(empty.migrated, false, "没有旧键就不动旧键");

  const dirtyStored = resolveExplorerState("{坏 JSON", null);
  assert.deepEqual(dirtyStored.state, normalizeExplorerState(null));
  assert.equal(dirtyStored.migrated, false);
});
