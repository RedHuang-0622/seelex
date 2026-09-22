// 资源管理器数据面的「原子刷新」能力：single-flight + 代次 + 根一致性提交。
//
// 为什么需要它：三个子页（工作树 / 提交记录 / 工作区更改）是同一个工作区根
// 的三张只读元数据面，读盘与 git 都可能在回合结束时变。刷新必须满足三条：
//   1) 同一时刻只有一次在飞的刷新——重复触发复用同一 promise，不叠加请求；
//   2) 结果按「工作区根 + 代次(generation)」提交——根已变或已有更新代次时，
//      回来的响应必须丢弃；
//   3) 一次刷新是一批页：整批一起提交（要么都换成新一代次的数据，要么都不
//      动），失败整批保留旧数据只提示——绝不出现新旧混搭。
//
// 本文件与 DOM/Bridge 无关：拉取、提交与失败提示都从外部注入（load/commit/
// onError），因此可以脱离浏览器直接单测（见 explorer-refresh.test.mjs）。
export const REFRESH_STATUS = Object.freeze({
  COMMITTED: "committed", // 整批结果已提交
  STALE: "stale",         // 根已变或已有更新代次 → 结果丢弃
  ERROR: "error",         // 任一页失败 → 整批作废（旧数据保留）
  NOOP: "noop"            // 空请求或未绑定工作区 → 不发请求
});

const NOOP_RESULT = Object.freeze({ status: REFRESH_STATUS.NOOP, pages: Object.freeze([]) });

// normalizePages 收敛页集合：只保留非空字符串 id 的首次出现顺序。
function normalizePages(pages) {
  const list = Array.isArray(pages) ? pages : [pages];
  const unique = [];
  for (const page of list) {
    if (typeof page === "string" && page !== "" && !unique.includes(page)) unique.push(page);
  }
  return unique;
}

// createExplorerRefresh 创建一台刷新机：
//   load(page)              → Promise<data>，只读拉取某一页的数据面
//   commit(entries, ticket) → void，整批提交（entries = [{page, data}]，ticket = {root, generation}）
//   onError(error, info)    → void，整批失败时提示（info = {pages, root, generation}）
// 返回：
//   setRoot(root)      换工作区根：在飞请求立即作废并从单飞槽位摘下，返回是否有变化
//   refresh(pages)     触发刷新，返回 promise<{status, pages?, error?}>
//   currentRoot()      当前提交基准的工作区根
//   generation()       当前代次
//   inFlight()         是否有在飞的刷新
export function createExplorerRefresh({ load, commit, onError = () => {} } = {}) {
  if (typeof load !== "function" || typeof commit !== "function") {
    throw new TypeError("createExplorerRefresh 需要 load 与 commit 回调");
  }
  let root = "";
  let generation = 0;
  // pending 是「本次飞行期间新到的页请求」：同一代次内补齐后再一起提交，既不
  // 复用旧数据也不另起一次请求（重复页天然去重；已在本批加载过的页不再重拉）。
  const pending = new Set();
  let flight = null;

  function currentRoot() {
    return root;
  }

  function generationOf() {
    return generation;
  }

  function inFlight() {
    return Boolean(flight);
  }

  // setRoot 换根：代次 +1 让在飞响应回来时被判过期；在飞请求同时从单飞槽位
  // 摘掉，否则紧接着的刷新会被当成"复用在飞请求"而丢掉（那次结果注定作废）。
  function setRoot(next) {
    const value = String(next ?? "");
    if (value === root) return false;
    root = value;
    generation += 1;
    pending.clear();
    flight = null;
    return true;
  }

  async function run(ticket) {
    const loaded = new Map();
    const failed = [];
    let failure = null;
    for (;;) {
      // 每轮只拉「本批还没拿到的页」：飞行期间新到的请求在下一轮补齐，已经在
      // 本批里加载过的页请求直接算满足（single-flight：不叠加请求）。
      const batch = [...pending].filter(page => !loaded.has(page));
      pending.clear();
      if (batch.length === 0) break;
      // 一批页并行拉取：任一失败即整批作废（allSettled 保证其余请求不被取消，
      // 也不会因为一个失败丢掉其它页的结果）。
      const settled = await Promise.allSettled(
        batch.map(page => Promise.resolve().then(() => load(page)).then(data => ({ page, data })))
      );
      settled.forEach((entry, index) => {
        if (entry.status === "fulfilled") loaded.set(entry.value.page, entry.value.data);
        else {
          failed.push(batch[index]);
          failure = failure || entry.reason;
        }
      });
      if (failure) break;
    }
    if (failure) {
      // 整批不提交：已经拿到的页也不落地，否则三个面板会一半新一半旧。飞行期间
      // 新到的页请求一并丢弃——下一次刷新是新一代次，由调用方重新决定要刷哪些页。
      pending.clear();
      onError(failure, { pages: failed, root: ticket.root, generation: ticket.generation });
      return { status: REFRESH_STATUS.ERROR, pages: failed, error: failure };
    }
    const entries = [...loaded.entries()].map(([page, data]) => ({ page, data }));
    if (ticket.generation !== generation || ticket.root !== root) {
      return { status: REFRESH_STATUS.STALE, pages: entries.map(entry => entry.page) };
    }
    // 提交前先摘掉单飞标记：commit 回调若再次触发刷新，应当开新一代次，而不是
    // 被当成"复用在飞请求"丢掉。
    flight = null;
    commit(entries, { root: ticket.root, generation: ticket.generation });
    return { status: REFRESH_STATUS.COMMITTED, pages: entries.map(entry => entry.page) };
  }

  function refresh(pages) {
    const list = normalizePages(pages);
    if (list.length === 0) return flight ? flight.promise : Promise.resolve(NOOP_RESULT);
    if (root === "") return Promise.resolve(NOOP_RESULT);
    for (const page of list) pending.add(page);
    if (flight) return flight.promise;
    const ticket = { root, generation: generation + 1 };
    generation = ticket.generation;
    const promise = run(ticket).finally(() => {
      if (flight?.promise === promise) flight = null;
    });
    flight = { promise, ticket };
    return promise;
  }

  return { setRoot, refresh, currentRoot, generation: generationOf, inFlight };
}
