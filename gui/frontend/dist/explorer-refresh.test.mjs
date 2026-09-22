import test from "node:test";
import assert from "node:assert/strict";
import { REFRESH_STATUS, createExplorerRefresh } from "./explorer-refresh.js";

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

// harness 把 load 变成「可手动放行的请求」：每次 load 从队列里取一个 deferred，
// 测试按登记顺序决定哪一页、哪一代次的响应何时回来（load 调用顺序 = 批内顺序）。
function createHarness() {
  const queue = [];
  const calls = [];
  const commits = [];
  const errors = [];
  const refresher = createExplorerRefresh({
    load: page => {
      const gate = queue.shift();
      if (!gate) throw new Error(`测试未登记 deferred：${page}`);
      calls.push(page);
      return gate.promise;
    },
    commit: (entries, ticket) => commits.push({ entries, ticket }),
    onError: (error, info) => errors.push({ error, info })
  });
  return {
    refresher,
    calls,
    commits,
    errors,
    reserve(count) {
      return Array.from({ length: count }, () => {
        const gate = deferred();
        queue.push(gate);
        return gate;
      });
    },
    flush: () => new Promise(resolve => setTimeout(resolve, 0))
  };
}

test("refresh: 需要 load 与 commit 回调（装配错误立即暴露）", () => {
  assert.throws(() => createExplorerRefresh({}), TypeError);
  assert.throws(() => createExplorerRefresh({ load: () => {} }), TypeError);
});

test("refresh: 未绑定工作区 / 空请求不发请求", async () => {
  const h = createHarness();
  assert.equal((await h.refresher.refresh(["worktree"])).status, REFRESH_STATUS.NOOP);
  assert.equal((await h.refresher.refresh([])).status, REFRESH_STATUS.NOOP);
  assert.equal((await h.refresher.refresh(null)).status, REFRESH_STATUS.NOOP);
  assert.deepEqual(h.calls, []);
  assert.equal(h.refresher.setRoot("/w"), true);
  assert.equal(h.refresher.setRoot("/w"), false, "同根不算变化");
  assert.equal((await h.refresher.refresh([])).status, REFRESH_STATUS.NOOP);
  assert.equal(h.refresher.setRoot(""), true);
  assert.equal((await h.refresher.refresh(["worktree"])).status, REFRESH_STATUS.NOOP);
  assert.deepEqual(h.calls, []);
});

test("refresh: 重复触发复用同一 promise，不叠加请求", async () => {
  const h = createHarness();
  const [worktree] = h.reserve(1);
  h.refresher.setRoot("/w");
  const first = h.refresher.refresh(["worktree"]);
  const second = h.refresher.refresh(["worktree"]);
  assert.equal(first, second, "同一时刻只有一次在飞的刷新");
  assert.equal(h.refresher.inFlight(), true);
  await h.flush();
  assert.deepEqual(h.calls, ["worktree"], "重复触发不叠加请求");
  worktree.resolve({ files: 3 });
  const result = await first;
  assert.equal(result.status, REFRESH_STATUS.COMMITTED);
  assert.deepEqual(result.pages, ["worktree"]);
  assert.equal(h.refresher.inFlight(), false);
  assert.equal(h.commits.length, 1);
  assert.deepEqual(h.commits[0].entries, [{ page: "worktree", data: { files: 3 } }]);
  assert.deepEqual(h.commits[0].ticket, { root: "/w", generation: 2 });
});

test("refresh: 飞行期间更宽的请求在同一批内补齐，整批一次提交", async () => {
  const h = createHarness();
  const [worktree, gitlog] = h.reserve(2);
  h.refresher.setRoot("/w");
  const first = h.refresher.refresh(["worktree"]);
  await h.flush();
  const wider = h.refresher.refresh(["worktree", "gitlog"]);
  assert.equal(first, wider, "飞行期间的触发复用同一 promise");
  worktree.resolve({ files: 1 });
  await h.flush();
  assert.deepEqual(h.calls, ["worktree", "gitlog"], "同一代次内补齐新页，已加载的页不重拉");
  gitlog.resolve({ commits: ["c1"] });
  const result = await first;
  assert.equal(result.status, REFRESH_STATUS.COMMITTED);
  assert.deepEqual(result.pages, ["worktree", "gitlog"]);
  assert.equal(h.commits.length, 1, "三个面板要么一起换、要么都不换");
  assert.deepEqual(h.commits[0].entries, [
    { page: "worktree", data: { files: 1 } },
    { page: "gitlog", data: { commits: ["c1"] } }
  ]);
});

test("refresh: 根已变 → 旧响应丢弃；新根的结果按新代次提交", async () => {
  const h = createHarness();
  const [oldWorktree] = h.reserve(1);
  h.refresher.setRoot("/w1");
  const oldFlight = h.refresher.refresh(["worktree"]);
  await h.flush();
  assert.equal(h.refresher.generation(), 2, "开一次刷新即新一代次");

  assert.equal(h.refresher.setRoot("/w2"), true);
  assert.equal(h.refresher.inFlight(), false, "换根后旧飞行从单飞槽位摘下（其结果必然作废）");
  assert.equal(h.refresher.generation(), 3, "换根作废旧数据面，代次继续前进");

  const [worktree, gitlog] = h.reserve(2);
  const newFlight = h.refresher.refresh(["worktree", "gitlog"]);
  assert.notEqual(newFlight, oldFlight);
  await h.flush();
  worktree.resolve({ files: 2 });
  gitlog.resolve({ commits: [] });
  const fresh = await newFlight;
  assert.equal(fresh.status, REFRESH_STATUS.COMMITTED);
  assert.deepEqual(h.commits[0].ticket, { root: "/w2", generation: 4 });

  oldWorktree.resolve({ files: 9 });
  const stale = await oldFlight;
  assert.equal(stale.status, REFRESH_STATUS.STALE, "根已变，过期响应必须丢弃");
  assert.equal(h.commits.length, 1, "过期响应不提交：三个面板不会新旧混搭");
});

test("refresh: 任一页失败 → 整批作废（旧数据保留）只提示", async () => {
  const h = createHarness();
  const [worktree, gitlog, changes] = h.reserve(3);
  h.refresher.setRoot("/w");
  const flight = h.refresher.refresh(["worktree", "gitlog", "changes"]);
  await h.flush();
  assert.deepEqual(h.calls, ["worktree", "gitlog", "changes"]);
  worktree.resolve({ files: 1 });
  gitlog.reject(new Error("提交记录暂不可用"));
  changes.resolve({ total: 0, entries: [] });
  const result = await flight;
  assert.equal(result.status, REFRESH_STATUS.ERROR);
  assert.deepEqual(result.pages, ["gitlog"]);
  assert.equal(h.commits.length, 0, "已经拿到的页也不落地：不许一半新一半旧");
  assert.equal(h.errors.length, 1);
  assert.equal(h.errors[0].error.message, "提交记录暂不可用");
  assert.deepEqual(h.errors[0].info, { pages: ["gitlog"], root: "/w", generation: 2 });
  assert.equal(h.refresher.inFlight(), false, "失败后不残留在飞状态");
});

test("refresh: 提交后再次刷新开新一代次并重拉（不是复读旧结果）", async () => {
  const h = createHarness();
  const [first] = h.reserve(1);
  h.refresher.setRoot("/w");
  const flightA = h.refresher.refresh(["changes"]);
  await h.flush();
  first.resolve({ total: 2, entries: [] });
  assert.equal((await flightA).status, REFRESH_STATUS.COMMITTED);

  const [second] = h.reserve(1);
  const flightB = h.refresher.refresh(["changes"]);
  await h.flush();
  assert.deepEqual(h.calls, ["changes", "changes"]);
  second.resolve({ total: 0, entries: [] });
  assert.equal((await flightB).status, REFRESH_STATUS.COMMITTED);
  assert.deepEqual(h.commits.map(entry => entry.ticket), [
    { root: "/w", generation: 2 },
    { root: "/w", generation: 3 }
  ]);
});
