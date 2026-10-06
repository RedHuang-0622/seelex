import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  TEAM_BOARD_CSS,
  effStatus,
  formatEventTime,
  itemDepsOf,
  itemStatus,
  itemsOfMilestone,
  memberCurrentSessionOf,
  memberMessagesOf,
  memberQueueOf,
  memberStatusOf,
  milestoneDepsOf,
  milestoneStatus,
  milestonesOf,
  orderMilestones,
  orderWorkItems,
  renderTeamAudit,
  renderTeamBoard,
  renderTeammateLiveSession,
  TEAMMATE_LIVE_PAGE_SIZE,
  renderTeamGantt,
  renderTeamQueue,
  renderTeamWorkItem,
  renderWorkItemSessionPanel,
  roleSlotOf,
  summarizeTeam,
  teammateSessionEntry,
  workItemsOf,
} from "./team-board-view.js";

const SRC = readFileSync(new URL("./team-board-view.js", import.meta.url), "utf8");
const APP = readFileSync(new URL("./app.js", import.meta.url), "utf8");

// 夹具一：里程碑 + 工作项口径的计划（**没有 stages**，2026-10-03 Work Item 形状）。
// 一个 Work Item 一个 teammate 一套 Session + worktree。
//
// 覆盖：里程碑屏障（m-ship 依赖 m-build）+ 里程碑内 DAG（wi-impl←wi-req、wi-test←wi-impl）
// + 一条 interrupted（抬红的线框色，仍是"可重派"）。
const MS_PLAN = {
  team_id: "v-model",
  version: 7,
  members: [
    { role: "pm", role_session_id: "s-v-model-pm", worktree: "seelex/pm", status: "idle" },
    {
      role: "exec",
      role_session_id: "s-v-model-exec",
      worktree: "seelex/exec",
      status: "running",
      queue: ["实现"],
      messages: [{ at: "2026-10-03T09:12:00", role: "exec", work_item: "wi-impl", text: "[wi-impl] 实现：跑完，等待 leader 评估" }],
    },
    { role: "test_case", role_session_id: "s-v-model-test", worktree: "seelex/test-case", tools_policy: "readonly", status: "idle" },
  ],
  milestones: [
    { id: "m-build", name: "构建", status: "active", content: "构建通过：四件工作项全部落地" },
    { id: "m-ship", name: "发布", depends_on: ["m-build"], status: "pending", after: ["accept"] },
  ],
  work_items: [
    {
      id: "wi-req", milestone: "m-build", role: "pm", name: "需求澄清",
      description: "把需求拆成可验收的条目", goal: "需求条目与验收用例逐条对应", status: "done",
    },
    {
      id: "wi-impl", milestone: "m-build", role: "exec", name: "实现",
      description: "改 team-board-view.js 的渲染口径", goal: "看板能看见里程碑下的工作项",
      status: "running", depends_on: ["wi-req"], session_id: "s-v-model-exec-wi-wi-impl",
      worktree: "seelex/exec-wi-impl", live: true, interrupted: true, handle: "a7", note: "实现：跑完，等 leader 评估",
    },
    { id: "wi-test", milestone: "m-build", role: "test_case", name: "用例跟进", status: "pending", depends_on: ["wi-impl"] },
    { id: "wi-ship", milestone: "m-ship", role: "exec", name: "发布", status: "pending" },
  ],
};

// 夹具二：老口径（阶段制时代）的计划——**只有 stages、没有里程碑也没有工作项**。
// 阶段口径已整条退场：这份计划在看板上就是"没有可看的编排"（整块退场）。
const STAGES_ONLY = {
  team_id: "legacy",
  version: 1,
  stages: [
    { id: "fix", roles: ["impl"], depends_on: [] },
    { id: "verify", roles: ["verify"], depends_on: ["fix"] },
  ],
  members: [{ role: "impl", role_session_id: "s-legacy-impl" }],
};

// 夹具三：**数组序不是拓扑序**的计划（里程碑与工作项都倒着声明）。
// 用它钉住口径 1：行序只能是依赖拓扑序，不许按数组序硬排。
const SHUFFLED = {
  team_id: "shuffled",
  version: 1,
  milestones: [{ id: "m2", depends_on: ["m1"] }, { id: "m1" }],
  work_items: [
    { id: "b2", milestone: "m2", role: "r2", status: "pending", depends_on: ["b1"] },
    { id: "b1", milestone: "m2", role: "r2", status: "pending" },
    { id: "a2", milestone: "m1", role: "r1", status: "pending", depends_on: ["a1"] },
    { id: "a1", milestone: "m1", role: "r1", status: "pending" },
  ],
};

// ── 纯函数：里程碑 / 工作项 ──────────────────────────────────────

test("milestonesOf / workItemsOf / itemStatus 只搬事实（空状态 = pending）", () => {
  assert.equal(milestonesOf(MS_PLAN).length, 2);
  assert.equal(workItemsOf(MS_PLAN).length, 4);
  assert.equal(itemsOfMilestone(MS_PLAN, "m-build").length, 3);
  assert.equal(itemStatus(MS_PLAN.work_items[1]), "running");
  assert.equal(itemStatus({ id: "x", name: "x" }), "pending");
  assert.equal(milestoneStatus({ id: "m" }), "pending");
  assert.equal(milestoneStatus({ id: "m", status: "DONE" }), "done");
  assert.deepEqual(milestoneDepsOf(MS_PLAN.milestones[1]), ["m-build"]);
  assert.deepEqual(itemDepsOf(MS_PLAN.work_items[1]), ["wi-req"]);
  assert.deepEqual(workItemsOf({}), []);
});

test("(c) effStatus 是线框色的**唯一判据**：四色映射 + interrupted 抬红 + killed 同一条红", () => {
  assert.equal(effStatus({ id: "x", status: "running" }), "running");
  assert.equal(effStatus({ id: "x", status: "review" }), "review");
  assert.equal(effStatus({ id: "x", status: "done" }), "done");
  assert.equal(effStatus({ id: "x", status: "failed" }), "failed");
  assert.equal(effStatus({ id: "x", status: "killed" }), "failed", "killed 与 failed 同一条红（不裂成两条）");
  assert.equal(effStatus({ id: "x", status: "pending" }), "pending");
  assert.equal(effStatus({ id: "x" }), "pending");
  assert.equal(effStatus({ id: "x", status: "running", interrupted: true }), "failed", "interrupted 抬成红");
  assert.equal(effStatus({ id: "x", status: "done", interrupted: true }), "failed", "interrupted 压过 status");
  assert.equal(effStatus({ id: "x", status: "weird" }), "pending", "认不出的状态退到 pending（不冒充 done）");
  // 渲染面：线框色的 data-eff 与 effStatus 同源（wi-impl 是 running+interrupted → 红）。
  const html = renderTeamGantt(MS_PLAN);
  assert.match(html, /data-item-id="wi-impl" data-status="running" data-eff="failed"/);
  assert.match(html, /<span class="team-status is-failed" data-eff="failed"[^>]*>running · 可重派</);
});

test("orderMilestones 按屏障依赖排层号；缺依赖与成环都显形（不静默丢里程碑）", () => {
  const ordered = orderMilestones(MS_PLAN);
  assert.deepEqual(ordered.map(entry => [entry.id, entry.depth]), [["m-build", 0], ["m-ship", 1]]);
  const missing = orderMilestones({ milestones: [{ id: "a", depends_on: ["ghost"] }, { id: "b" }] });
  assert.deepEqual(missing[0].missing_deps, ["ghost"]);
  const cyclic = orderMilestones({ milestones: [{ id: "a", depends_on: ["b"] }, { id: "b", depends_on: ["a"] }] });
  assert.equal(cyclic.filter(entry => entry.cyclic).length, 2);
});

test("orderWorkItems 只在里程碑内排 DAG，并把「前置没验收」标成被卡住", () => {
  const entries = orderWorkItems(itemsOfMilestone(MS_PLAN, "m-build"));
  assert.deepEqual(entries.map(entry => entry.id), ["wi-req", "wi-impl", "wi-test"]);
  assert.equal(entries.find(entry => entry.id === "wi-impl").blocked, false, "前置 wi-req 已 done");
  assert.equal(entries.find(entry => entry.id === "wi-test").blocked, true, "前置 wi-impl 还在跑");
});

test("memberQueueOf / memberStatusOf / memberMessagesOf：队列 = 没完成的工作项（销项即出队）", () => {
  assert.deepEqual(memberQueueOf(MS_PLAN, "exec"), ["实现"]);
  assert.equal(memberStatusOf(MS_PLAN, "exec"), "running");
  // teammate 状态只有 running / free 两值（2026-10-04）：没在跑的人不写 idle/review/done。
  assert.equal(memberStatusOf(MS_PLAN, "pm"), "free");
  assert.equal(memberStatusOf({ members: [{ role: "x" }] }, "x"), "free");
  assert.equal(memberStatusOf({ members: [{ role: "x", status: "review" }] }, "x"), "free", "待验收不是 running：人没在干活");
  assert.equal(memberStatusOf({ members: [{ role: "x", status: "done" }] }, "x"), "free", "teammate 没有 done 这个状态");
  assert.equal(memberMessagesOf(MS_PLAN, "exec").length, 1);
  // 后端没给 queue 时按工作项自行派生（同一条判据：未完成才占队列）。
  const derived = { ...MS_PLAN, members: [{ role: "pm" }, { role: "exec" }] };
  assert.deepEqual(memberQueueOf(derived, "exec"), ["实现", "发布"]);
  assert.deepEqual(memberQueueOf(derived, "pm"), [], "wi-req 已 done → 已销项，不占队列");
});

test("summarizeTeam 只报事实计数（里程碑 / 工作项 / 在编 / 作业；不报阶段）", () => {
  const summary = summarizeTeam(MS_PLAN, [{ handle: "a7", state: "running" }]);
  assert.deepEqual(summary, {
    members: 3,
    milestones_total: 2,
    milestones_done: 0,
    items_total: 4,
    items_done: 1,
    items_running: 1,
    items_review: 0,
    items_failed: 0,
    jobs_running: 1,
    jobs_failed: 0,
    jobs_done: 0,
  });
  assert.equal(hasKey(summary, "stages"), false, "阶段口径已退场，汇总里不得再有 stages");
});

function hasKey(object, key) {
  return Object.prototype.hasOwnProperty.call(object, key);
}

// ── ① 里程碑 = 一个大框，罩住它名下的工作项 ───────────────────────

test("① 每个里程碑是一个大框，罩住它名下相邻的若干行（行数正确、行不跨框）", () => {
  const html = renderTeamGantt(MS_PLAN);
  assert.match(html, /data-team-gantt/);
  assert.match(html, /class="team-dag-scroll"/);
  assert.match(html, /class="team-dag-content"/);
  // 框自带 里程碑 id / status / 屏障层号 / 解锁态。
  assert.match(html, /class="team-dag-frame" data-milestone-id="m-build" data-status="active" data-layer="0" data-locked="false"/);
  assert.match(html, /class="team-dag-frame" data-milestone-id="m-ship" data-status="pending" data-layer="1" data-locked="true"/);

  // (b) 框真的包住自己名下的行，且框内行数正确 —— 不是"看起来在一起"。
  const chunks = html.split('<section class="team-dag-frame"').slice(1);
  assert.equal(chunks.length, 2, "两个里程碑 = 两个大框");
  const idsOf = chunk => [...chunk.matchAll(/data-item-id="([^"]+)"/g)].map(match => match[1]);
  assert.deepEqual(idsOf(chunks[0]), ["wi-req", "wi-impl", "wi-test"], "m-build 的三个行都在它自己的框里，且是拓扑序");
  assert.deepEqual(idsOf(chunks[1]), ["wi-ship"], "m-ship 的行没有漏到别的框里");
  assert.match(chunks[0], /team-dag-ms-count">3 items</);
  assert.match(chunks[1], /team-dag-ms-count">1 items</);
  // 没有里程碑 → 不留空壳。
  assert.equal(renderTeamGantt({ team_id: "t" }), "");
});

test("① renderTeamWorkItem 一个工作项就是一行：名称/描述/达成目标/teammate/依赖/状态", () => {
  const [entry] = orderWorkItems(itemsOfMilestone(MS_PLAN, "m-build")).filter(item => item.id === "wi-impl");
  const html = renderTeamWorkItem(entry);
  assert.match(html, /<article class="team-dag-row" data-item-id="wi-impl" data-status="running" data-eff="failed"/);
  assert.match(html, /class="chip team-role" title="teammate"><i class="team-dag-dot"><\/i>exec</);
  assert.match(html, /class="chip team-dep">wi-req</);
  // 名称（带会话 = 可点子页面入口）。
  assert.match(html, /class="team-dag-name is-openable" data-team-item-open="wi-impl" data-team-item-session="s-v-model-exec-wi-wi-impl"/);
  // 口径 9：一行里 名称 / role / status / 依赖 / 会话 / 工作区 / 达成目标 / 描述 / 结论 都在。
  for (const text of ["实现", "exec", "running", "wi-req", "s-v-model-exec-wi-wi-impl", "seelex/exec-wi-impl", "看板能看见里程碑下的工作项", "改 team-board-view.js 的渲染口径", "等 leader 评估"]) {
    assert.ok(html.includes(text) || html.includes(text.replace(/&/g, "&amp;")), `行里读不到 ${text}`);
  }
  // 三个标记：可重派 / 现场在（这一行是 running + interrupted + live）。
  assert.match(html, /可重派/);
  assert.match(html, /现场在/);
});

test("(a) 每一行都排在其全部依赖之后（拓扑不变量），且不按数组序硬排", () => {
  const order = [...renderTeamGantt(MS_PLAN).matchAll(/<article class="team-dag-row" data-item-id="([^"]+)"/g)].map(match => match[1]);
  assert.deepEqual(order, ["wi-req", "wi-impl", "wi-test", "wi-ship"]);
  const position = new Map(order.map((id, index) => [id, index]));
  for (const item of MS_PLAN.work_items) {
    for (const dep of itemDepsOf(item)) {
      if (!position.has(dep)) continue;
      assert.ok(position.get(dep) < position.get(item.id), `${item.id} 必须排在其依赖 ${dep} 之后`);
    }
  }
  // 数组序**不是**拓扑序的夹具：行序必须被重排成拓扑序（里程碑之间也重排）。
  const shuffled = [...renderTeamGantt(SHUFFLED).matchAll(/<article class="team-dag-row" data-item-id="([^"]+)"/g)].map(match => match[1]);
  assert.deepEqual(shuffled, ["a1", "a2", "b1", "b2"], "倒着声明的计划也必须排成拓扑序");
});

// ── ② 里程碑框头 / 屏障 / 闸门 ───────────────────────────────────

test("② 框头读得到 id · name · L<n> · status chip · 屏障 deps · 判据 · 🔒/🔓 · n items · content", () => {
  const html = renderTeamGantt(MS_PLAN);
  assert.match(html, /<span class="team-dag-ms-id">m-build<\/span>[\s\S]{0,80}<span class="team-dag-ms-name">构建<\/span>/);
  assert.match(html, /title="屏障层号 L0（同层 = 依赖边允许并行）">L0</);
  assert.match(html, /<span class="team-status is-active">active<\/span>/);
  assert.match(html, /<span class="team-label">屏障<\/span><span class="chip team-dep">m-build<\/span>/);
  assert.match(html, /<span class="team-label">判据<\/span><span class="chip team-dep">accept<\/span>/);
  assert.match(html, /🔒 待解锁/);
  assert.match(html, /🔓 已解锁/);
  assert.match(html, /team-dag-ms-count">3 items</);
  assert.match(html, /team-dag-ms-content" title="构建通过：四件工作项全部落地">构建通过：四件工作项全部落地</);
  // L0 无屏障 → —。
  assert.match(html, /<span class="team-label">屏障<\/span><span class="muted">—<\/span>/);
  // 甘特不表示时间。
  assert.doesNotMatch(html, /started_at|finished_at|datetime|duration|时长|耗时/);
});

test("(5) 屏障 = 完成上一个才能进下一个：未解锁框虚线 + 降透明度；闸门带写明在等谁/已放行", () => {
  const html = renderTeamGantt(MS_PLAN);
  assert.match(html, /class="team-dag-gate" data-gate="m-build-&gt;m-ship" data-open="false"/);
  assert.match(html, /闸门 → 上一层全部 done 才放行（等 m-build）/);
  assert.match(TEAM_BOARD_CSS, /\.team-dag-frame\[data-locked="true"\]\s*\{[^}]*border-style:\s*dashed/);
  assert.match(TEAM_BOARD_CSS, /\.team-dag-frame\[data-locked="true"\]\s*\{[^}]*opacity:\s*\.55/);
  assert.match(TEAM_BOARD_CSS, /\.team-dag-gate\[data-open="true"\]\s*\{[^}]*--team-dag-gate-tone:\s*var\(--status-done\)/);
  // 上一层全 done → 框解锁、闸门转绿并写「闸门已放行」。
  const opened = renderTeamGantt({
    milestones: [{ id: "m-build", name: "构建", status: "done" }, { id: "m-ship", name: "发布", depends_on: ["m-build"], status: "pending" }],
    work_items: [{ id: "wi-ship", milestone: "m-ship", role: "exec", name: "发布", status: "pending" }],
  });
  assert.match(opened, /data-gate="m-build-&gt;m-ship" data-open="true"/);
  assert.match(opened, /闸门已放行 → m-build 全部 done/);
  assert.match(opened, /data-milestone-id="m-ship"[^>]*data-locked="false"/);
});

test("② 里程碑屏障的缺失依赖与成环都显形", () => {
  const missing = renderTeamGantt({ milestones: [{ id: "a", depends_on: ["ghost"] }] });
  assert.match(missing, /依赖缺失：ghost/);
  const cyclic = renderTeamGantt({ milestones: [{ id: "a", depends_on: ["b"] }, { id: "b", depends_on: ["a"] }] });
  assert.match(cyclic, /依赖成环/);
});

test("② 工作项级的缺失依赖也显形（不许静默丢依赖）", () => {
  const html = renderTeamGantt({
    milestones: [{ id: "m", name: "M" }],
    work_items: [{ id: "wi-1", milestone: "m", role: "r", name: "甲", status: "pending", depends_on: ["wi-ghost"] }],
  });
  assert.match(html, /依赖缺失：wi-ghost/);
});

// ── ③ teammate 区块：名称 / 状态 / 工作项名称队列 ────────────────

test("③ teammate 区块画名字 / 状态 / 负责的工作项名称队列 / 尾插回执", () => {
  const html = renderTeamQueue(MS_PLAN);
  assert.match(html, /data-team-queue/);
  assert.match(html, /data-role="exec" data-status="running"/);
  assert.match(html, /<span class="chip team-queue-item">实现<\/span>/);
  assert.match(html, /改动|等待 leader 评估|跑完/);
  // 名称 = 员工会话入口（与团队面板同一对钩子，缺会话号退化为纯文本）。
  // 入口开的是**这位此刻那件事的会话**（2026-10-04 用户口径）：exec 正在跑 wi-impl，
  // 所以入口指向 wi-impl 自己的会话号，而不是 exec 的角色会话 s-v-model-exec。
  assert.match(html, /class="team-member-role is-openable" data-team-role-open="exec" data-team-role-session="s-v-model-exec-wi-wi-impl" data-team-item="wi-impl"/);
  assert.doesNotMatch(html, /data-team-role-session="s-v-model-exec"/, "不得再指向员工的长期角色会话");
  // 没有"这件事自己的会话"的成员**不是入口**（2026-10-04 用户口径修正：查看 teammate
  // 的会话看到的总是主代理的会话——因为那位员工在存储里的角色会话从不写 teammate 自己的行，
  // RoleSnapshot 的 main 车道就是主会话整段）。回退到角色会话 = 把主代理的会话冒充成
  // teammate 的会话，所以这里退化成纯文本 + 说清为什么。
  assert.doesNotMatch(html, /data-team-role-open="pm"/, "没有自己的会话就不许挂入口");
  assert.doesNotMatch(html, /data-team-role-session="s-v-model-pm"/, "不得回退到员工的长期角色会话");
  assert.match(html, /<span class="team-member-role" title="[^"]*这一位此刻没有自己的会话/);
  assert.equal(renderTeamQueue({ members: [{ role: "a" }] }), "", "没有工作项就不画这一节（不留空壳）");
});

test("③ teammate 会话入口只指向「这件事自己的会话」，绝不回退到员工的历史角色会话", () => {
  // 只给角色会话号（员工长期会话）而没有工作项会话号：这一位此刻没有自己的会话——
  // 点开它会读到主代理的会话（角色会话读面恒带 main_rows，而 teammate 自己那条车道为空），
  // 所以入口必须退场，而不是"先给一个看起来能点的入口"。
  const plan = {
    members: [{ role: "exec", role_session_id: "s-v-model-exec", status: "free" }],
    work_items: [{ id: "wi-impl", role: "exec", name: "实现", status: "pending" }],
  };
  const html = renderTeamQueue(plan);
  assert.match(html, /<span class="team-member-role"/, "没有自己的会话：名字是纯文本");
  assert.doesNotMatch(html, /data-team-role-open/, "不许挂入口");
  assert.doesNotMatch(html, /s-v-model-exec/, "角色会话号不得出现在入口上");
  assert.match(html, /这一位此刻没有自己的会话/);
  // 有在跑的工作项时才是入口（同一份计划，补上这件事自己的会话号）。
  const withSession = renderTeamQueue({
    members: [{ role: "exec", role_session_id: "s-v-model-exec", status: "running" }],
    work_items: [{ id: "wi-impl", role: "exec", name: "实现", status: "running", session_id: "s-v-model-exec-wi-wi-impl" }],
  });
  assert.match(withSession, /data-team-role-session="s-v-model-exec-wi-wi-impl" data-team-item="wi-impl"/);
  assert.doesNotMatch(withSession, /s-v-model-exec"/, "指向的是这件事自己的会话，不是角色会话");
});

test("memberCurrentSessionOf：在跑 > 等验收 > 最近开过工；一个都没开过 → 空", () => {
  const plan = {
    members: [{ role: "exec" }, { role: "verify" }, { role: "fresh" }],
    work_items: [
      { id: "wi-done", role: "verify", status: "done", session_id: "s-done", name: "旧活" },
      { id: "wi-review", role: "verify", status: "review", session_id: "s-review", name: "等验收" },
      { id: "wi-two", role: "verify", status: "review", session_id: "s-review-2", name: "第二件等验收" },
      { id: "wi-run", role: "exec", status: "running", session_id: "s-run", name: "在跑" },
      { id: "wi-pending", role: "exec", status: "pending", name: "还没派发（没有会话号）" },
    ],
  };
  assert.deepEqual(memberCurrentSessionOf(plan, "exec"), { session_id: "s-run", work_item: "wi-run", name: "在跑" });
  // 等验收优先于更早的"等验收"（先到先得），也优先于 done 的兜底。
  assert.equal(memberCurrentSessionOf(plan, "verify").session_id, "s-review");
  assert.deepEqual(memberCurrentSessionOf(plan, "fresh"), { session_id: "", work_item: "", name: "" });
  // 后端给了权威字段时以它为准（前端那份只是老快照的降级兜底）。
  const provided = { members: [{ role: "exec", current_session_id: "s-auth", current_work_item: "wi-auth" }] };
  assert.deepEqual(memberCurrentSessionOf(provided, "exec"), { session_id: "s-auth", work_item: "wi-auth", name: "" });
});

test("teammateSessionEntry：teammate 只开「这件事自己的会话」，没有就不开；非 teammate 才走角色会话", () => {
  const plan = {
    members: [{ role: "exec", role_session_id: "s-v-model-exec" }, { role: "pm", role_session_id: "s-v-model-pm" }],
    work_items: [
      { id: "wi-impl", role: "exec", name: "实现", status: "running", session_id: "s-v-model-exec-wi-wi-impl" },
      { id: "wi-req", role: "pm", name: "需求", status: "done" },
    ],
  };
  // teammate + 有"这件事自己的会话" → 实时读面（工作项上下文一起带上）。
  assert.deepEqual(teammateSessionEntry(plan, "exec"), {
    kind: "live", session_id: "s-v-model-exec-wi-wi-impl", work_item: "wi-impl", name: "实现"
  });
  // teammate + 没有 → **不开**（退回角色会话只会拿到主代理的 main 行）。
  const none = teammateSessionEntry(plan, "pm");
  assert.equal(none.kind, "none");
  assert.match(none.reason, /这一位此刻没有自己的会话/);
  // 不是 teammate（goal-a2a 的 tl 等真有自己角色的会话）→ 角色会话，行为不变。
  assert.deepEqual(teammateSessionEntry(plan, "tl"), { kind: "role" });
  // 没有计划（没有团队看板）→ 判不了 teammate 身份，按老路径走角色会话。
  assert.deepEqual(teammateSessionEntry(null, "tl"), { kind: "role" });
});

// ── ④ 里程碑口径：阶段不再是计划的形状 ─────────────────────────────

test("④ 里程碑口径的计划照常出图：里程碑 + 工作项 + teammate 都在", () => {
  const html = renderTeamBoard({ plan: MS_PLAN, jobs: [] });
  assert.match(html, /data-team-id="v-model"/);
  assert.match(html, /data-team-gantt/);
  assert.match(html, /data-team-queue/);
  assert.match(html, /data-milestone-id="m-build"/);
  assert.match(html, /data-item-id="wi-impl"/);
  assert.doesNotMatch(html, /data-stage-id/, "看板不得再按阶段分组");
  assert.doesNotMatch(html, /team-stage/, "阶段卡样式/类名不得再出现");
});

test("④ 老口径（只有 stages）的计划 → 退场：阶段不再是可看的编排", () => {
  assert.equal(renderTeamBoard({ plan: STAGES_ONLY, jobs: [] }), "");
  assert.equal(renderTeamBoard(), "");
  assert.equal(renderTeamBoard({ plan: null }), "");
  assert.equal(renderTeamBoard({ plan: { team_id: "t", version: 1 } }), "");
  assert.equal(renderTeamBoard({ plan: { team_id: "t", version: 1, stages: [] } }), "");
});

test("④ 渲染件没有阶段口径的残留（jobsByStage / orderStages / roleOwnerStage / stagesOf）", () => {
  for (const name of ["jobsByStage", "orderStages", "roleOwnerStage", "stagesOf", "renderTeamStageCard", "stageStatuses"]) {
    assert.doesNotMatch(SRC, new RegExp(`export function ${name}\\b`), `渲染件不得再导出 ${name}`);
  }
  assert.doesNotMatch(SRC, /class="team-stages"/);
  assert.doesNotMatch(SRC, /data-stage-id/);
  assert.doesNotMatch(SRC, /\.team-stage[\s,{[]/, "CSS 里不得再留一份 .team-stage 规则");
});

test("④ 接线把 work_items 搬给渲染件；退场判据只看里程碑与工作项", () => {
  assert.match(APP, /work_items: workItems\.map/, "app.js 必须把 DTO 的 work_items 搬给渲染件");
  assert.match(APP, /milestones\.length === 0 && workItems\.length === 0/, "app.js 的退场判据必须是「无里程碑且无工作项」（阶段口径已退场，不再参与判据）");
  assert.doesNotMatch(APP, /board\?\.stages/, "app.js 不得再从投影里搬 stages（阶段口径已退场）");
});

// ── 当前 teammate 会话（实时执行面）子页面 ──────────────────────

test("renderTeammateLiveSession：读得到就读这一轮的对话，读不到就如实说「不在本进程」", () => {
  const live = renderTeammateLiveSession(
    { session_id: "s-wi", role: "exec", running: true, live: true, messages: [
      { role: "user", text: "实现渲染件" },
      { role: "assistant", text: "改好了，用例全绿" },
      { role: "tool", text: "（工具行不进面板）" },
    ] },
    { work_item: "wi-impl" }
  );
  assert.match(live, /data-teammate-live="s-wi"/);
  assert.match(live, /wi-impl/);
  assert.match(live, /实现渲染件/);
  assert.match(live, /用例全绿/);
  assert.match(live, /正在跑/);
  assert.doesNotMatch(live, /工具行不进面板/, "工具行是执行细节，不进这个面板");
  assert.doesNotMatch(live, /<table|role-row/, "子页面不画状态台账（只要条目）");

  // 执行面不在本进程（重启过/已收口）：不许画空壳假装"当前会话是空的"。
  const missing = renderTeammateLiveSession(
    { session_id: "s-wi", role: "exec", running: false, live: false, messages: [] },
    { work_item: "wi-impl" }
  );
  assert.match(missing, /不在本进程/);
  assert.match(missing, /正文不落盘/);
  assert.doesNotMatch(missing, /role-kv-row/, "读不到就不画对话区");

  // 截断痕迹如实显示。
  const truncated = renderTeammateLiveSession(
    { session_id: "s-wi", role: "exec", running: true, live: false, truncated: true,
      messages: [{ role: "assistant", text: "只留最近几条" }] },
    { work_item: "wi-impl" }
  );
  assert.match(truncated, /已跑完/);
  assert.match(truncated, /只显示最近/);
});

test("renderTeammateLiveSession：翻页条搬后端的 offset/total/has_more，不自己推算", () => {
  // 中间页：前后都翻得动（上一页回到 offset-limit，下一页往后走）。
  const middle = renderTeammateLiveSession(
    { session_id: "s-wi", role: "exec", running: true, live: false,
      offset: 40, limit: TEAMMATE_LIVE_PAGE_SIZE, total: 100, has_more: true,
      messages: [{ role: "assistant", text: "中间那一页" }] },
    { work_item: "wi-impl" }
  );
  assert.match(middle, /中间那一页/);
  assert.match(middle, /第 41–41 条 \/ 共 100 条/);
  assert.match(middle, /data-teammate-live-page="0"/, "上一页回到 offset-limit（后端页大小）");
  assert.match(middle, /data-teammate-live-page="80"/, "下一页往后走一个页大小");
  assert.doesNotMatch(middle, /disabled/, "中间页两个方向都翻得动");

  // 尾巴页（has_more=false）：下一页必须禁用——不许画一个按不动的键假装还有。
  const tail = renderTeammateLiveSession(
    { session_id: "s-wi", role: "exec", running: true, live: true,
      offset: 60, limit: TEAMMATE_LIVE_PAGE_SIZE, total: 61, has_more: false,
      messages: [{ role: "assistant", text: "最新一条" }] },
    { work_item: "wi-impl" }
  );
  assert.match(tail, /第 61–61 条 \/ 共 61 条/);
  assert.match(tail, /data-teammate-live-page="100" disabled/, "到头了：下一页禁用");

  // 空会话：没有条目就不画翻页条（不画空壳）。
  const empty = renderTeammateLiveSession(
    { session_id: "s-wi", role: "exec", running: true, live: true, offset: 0, limit: 40, total: 0, has_more: false, messages: [] },
    { work_item: "wi-impl" }
  );
  assert.doesNotMatch(empty, /team-live-pager/, "没有条目就没有翻页条");

  // 前端只搬读数、不另算：翻页动作经唯一的读法（后端的 TeammateSessionLivePage）。
  assert.match(APP, /invoke\("TeammateSessionLivePage"/, "翻页必须走后端的分页读法");
  assert.match(APP, /data-teammate-live-page/, "翻页键要有事件接线（画出来点不动等于没画）");
});

// ── 头部 / 转义 / 痕迹 / 子页面 / 样式 ───────────────────────────

test("看板头报工作项口径的计数，不再写「阶段」", () => {
  const html = renderTeamBoard({ plan: MS_PLAN, jobs: [{ handle: "a7", state: "running" }], maxMembers: 6 });
  assert.match(html, /工作项 1\/4 完成/);
  assert.match(html, /在编 3\/6/);
  assert.match(html, /里程碑 0\/2/);
  assert.doesNotMatch(html, /阶段/);
});

test("转义所有外部文本（team_id / 工作项名 / 描述都不能注入）", () => {
  const plan = {
    team_id: '<img src=x onerror="boom">',
    version: 1,
    members: [{ role: '<i>r</i>', role_session_id: "s" }],
    milestones: [{ id: '<b>m</b>', name: '<em>n</em>' }],
    work_items: [{ id: "wi-1", milestone: "<b>m</b>", role: '<i>r</i>', name: "<u>n</u>", description: "<script>x</script>", goal: "<svg/>" }],
  };
  const html = renderTeamBoard({ plan, jobs: [] });
  assert.doesNotMatch(html, /<img/);
  assert.doesNotMatch(html, /<script|<\/script>/);
  assert.doesNotMatch(html, /<u>n<\/u>|<em>n<\/em>/);
  assert.doesNotMatch(html, /<svg\/>/, "工作项里的 <svg/> 必须转义（画布上那张 SVG 是渲染件自己的）");
  assert.match(html, /&lt;u&gt;n&lt;\/u&gt;/, "工作项名要转义");
  assert.match(html, /&lt;em&gt;n&lt;\/em&gt;/, "里程碑名要转义");
  assert.match(html, /&lt;img src=x onerror=&quot;boom&quot;&gt;/);
  assert.match(html, /<span class="chip team-role" title="teammate"><i class="team-dag-dot"><\/i>&lt;i&gt;r&lt;\/i&gt;<\/span>/);
  assert.match(html, /&lt;b&gt;m&lt;\/b&gt;/, "里程碑 id 里的标签要转义");
  assert.match(html, /&lt;svg\/&gt;/, "达成目标里的 <svg/> 要转义");
});

test("句柄投影过期 / 自快照恢复都显形，且与 stale 同屏而不互斥", () => {
  const stale = renderTeamBoard({ plan: MS_PLAN, jobs: [], stale: true });
  assert.match(stale, /句柄投影可能过期/);
  assert.doesNotMatch(renderTeamBoard({ plan: MS_PLAN, jobs: [] }), /句柄投影可能过期/);
  const recovered = renderTeamBoard({ plan: MS_PLAN, jobs: [], recovered: true });
  assert.match(recovered, /自快照恢复/);
  const both = renderTeamBoard({ plan: MS_PLAN, jobs: [], recovered: true, stale: true });
  assert.match(both, /自快照恢复/);
  assert.match(both, /句柄投影可能过期/);
});

test("审计只显示最近 6 条且最新在最上面，时间取 HH:MM", () => {
  const events = Array.from({ length: 8 }, (_, index) => ({ at: `2026-10-03T13:0${index}:00`, kind: "dispatch", work_item: `wi-${index}` }));
  const html = renderTeamAudit(events);
  const shown = [...html.matchAll(/data-kind="dispatch"/g)].length;
  assert.equal(shown, 6);
  assert.doesNotMatch(html, /wi-0</);
  assert.match(html, /wi-7/);
  assert.match(html, /<time class="team-event-at">13:07<\/time>/);
  assert.equal(formatEventTime("not-a-time"), "not-a-time");
  assert.equal(formatEventTime(""), "—");
});

test("renderWorkItemSessionPanel 是执行进度子页面：只有条目，没有下面的表格", () => {
  const html = renderWorkItemSessionPanel(MS_PLAN, "wi-impl");
  assert.match(html, /data-team-item-panel="wi-impl"/);
  for (const key of ["工作项", "名称", "执行 teammate", "状态", "达成目标", "描述", "会话", "工作区", "结论"]) {
    assert.match(html, new RegExp(`role-kv-key">${key}<`));
  }
  assert.match(html, /s-v-model-exec-wi-wi-impl/);
  assert.doesNotMatch(html, /<table|<thead|<tbody|role-row/);
  assert.match(renderWorkItemSessionPanel(MS_PLAN, "wi-nope"), /已不在计划里/);
  assert.equal(renderWorkItemSessionPanel({}, "x"), '<div class="role-session-view is-empty">这件事已不在计划里（可能已收口）</div>');
});

// ── ⑥ 依赖边（口径 6）与两通道正交（口径 3/13）────────────────────

test("(6) 依赖边真的画出来：覆盖全内容的 SVG 正交折线 + 末端箭头；边一律中性灰", () => {
  const html = renderTeamGantt(MS_PLAN);
  assert.match(html, /<svg class="team-dag-edges" aria-hidden="true" preserveAspectRatio="none" viewBox="0 0 100 \d+"/);
  const paths = [...html.matchAll(/class="team-dag-edge" d="M (\d+) (\d+) H (\d+) V (\d+) H (\d+)"/g)];
  // wi-impl←wi-req 与 wi-test←wi-impl 两条（wi-ship 没有依赖）。
  assert.equal(paths.length, 2, "有几条依赖边就画几条");
  for (const [, xs, ys, lane, yd, xd] of paths) {
    assert.equal(xs, xd, "起点与终点都在节点锚点上（同一条 lane 折回去）");
    assert.ok(Number(lane) > Number(xs), "lane 在节点右侧（左 gutter 里）");
    assert.ok(Number(ys) < Number(yd), "被依赖的行在上，依赖它的行在下");
  }
  assert.equal([...html.matchAll(/class="team-dag-edge-arrow"/g)].length, 2, "每条边一个箭头");
  // 边不吃状态色、不吃 teammate 色：唯一来源是 --team-dag-edge（= --faint）。
  assert.match(TEAM_BOARD_CSS, /--team-dag-edge:\s*var\(--faint\)/);
  assert.match(TEAM_BOARD_CSS, /\.team-dag-edge\s*\{[^}]*stroke:\s*var\(--team-dag-edge\)/);
  assert.match(TEAM_BOARD_CSS, /\.team-dag-edges\s*\{[^}]*position:\s*absolute/);
  assert.match(TEAM_BOARD_CSS, /\.team-dag-edges\s*\{[^}]*width:\s*var\(--team-dag-gutter\)/);
});

test("(3/13) 两条颜色通道正交：边框/底色只吃状态令牌；teammate 色只走色带 + 色点", () => {
  const [entry] = orderWorkItems(itemsOfMilestone(MS_PLAN, "m-build")).filter(item => item.id === "wi-impl");
  const html = renderTeamWorkItem(entry);
  assert.match(html, /data-eff="failed"/, "线框色由 data-eff 决定");
  assert.match(html, /style="--team-dag-role-color:var\(--team-dag-role-\d\)"/, "归属色只以变量形式挂在行上");
  assert.match(html, /<span class="team-dag-band"/);
  assert.match(html, /<i class="team-dag-dot"><\/i>/, "role chip 里的色点");
  // CSS：状态色只给 --row-line/--row-tint；role 色只给色带与色点 —— 谁也不越界。
  assert.match(TEAM_BOARD_CSS, /\.team-dag-row\[data-eff="failed"\]\s*\{[^}]*--row-line:\s*var\(--status-failed\)[^}]*--row-tint:\s*var\(--tint-failed\)/);
  assert.doesNotMatch(TEAM_BOARD_CSS, /\.team-dag-(card|row)[^{]*\{[^}]*--team-dag-role-color/);
  assert.doesNotMatch(TEAM_BOARD_CSS, /\.team-dag-(band|dot)[^{]*\{[^}]*--row-line/);
  // 色板必须定义在 .team-board 作用域里（不泄露到全局）。
  assert.match(TEAM_BOARD_CSS, /\.team-board\s*\{[^}]*--team-dag-role-0:/);
  assert.doesNotMatch(TEAM_BOARD_CSS, /:root\s*\{[^}]*--team-dag-role-0:/);
});

test("(3) 同一位 teammate 的颜色跨行一致、跨重渲染不变；不同位不同色", () => {
  const plan = {
    milestones: [{ id: "m", status: "pending" }],
    work_items: [
      { id: "i1", milestone: "m", role: "zz-same", status: "done" },
      { id: "i2", milestone: "m", role: "zz-other", status: "done" },
      { id: "i3", milestone: "m", role: "zz-same", status: "done" },
    ],
  };
  const varOf = (html, id) => html.match(new RegExp(`data-item-id="${id}"[^>]*style="--team-dag-role-color:var\\(([^)]+)\\)"`))[1];
  const first = renderTeamGantt(plan);
  assert.equal(varOf(first, "i1"), varOf(first, "i3"), "同一位 teammate 的每一行同一个色带");
  assert.notEqual(varOf(first, "i1"), varOf(first, "i2"), "不同位 teammate 不同色");
  assert.equal(renderTeamGantt(plan), first, "同一输入连渲两次逐字相同（无时间戳/随机数/自增 id）");
  assert.equal(varOf(renderTeamGantt(plan), "i1"), varOf(first, "i1"), "重渲染不改色");
});

test("(11) role 超过 6 个 → 色板回绕，slot≥6 的色带叠斜纹第二通道", () => {
  const roles = Array.from({ length: 10 }, (_, index) => `zz-wrap-${index}`);
  const plan = { milestones: [{ id: "m", status: "done" }], work_items: roles.map((role, index) => ({ id: `w${index}`, milestone: "m", role, status: "done" })) };
  const rows = [...renderTeamGantt(plan).matchAll(/<article class="team-dag-row" data-item-id="w\d+"[\s\S]*?<\/article>/g)].map(match => match[0]);
  assert.equal(rows.length, 10);
  let wrapped = 0;
  roles.forEach((role, index) => {
    const slot = roleSlotOf(role);
    const expectWrapped = slot >= 6;
    if (expectWrapped) wrapped += 1;
    const band = rows[index].match(/<span class="team-dag-band([^"]*)"/)[1];
    assert.equal(band.includes("is-wrapped"), expectWrapped, `${role}（slot ${slot}）的斜纹标记必须与「是否回绕」一致`);
    assert.match(rows[index], new RegExp(`--team-dag-role-${slot % 6}\\)`), "回绕时复用 6 色里对应的那一格");
  });
  assert.ok(wrapped > 0, "10 位 teammate 必然有人落到回绕区（否则这条用例没验到东西）");
  assert.match(TEAM_BOARD_CSS, /\.team-dag-band\.is-wrapped\s*\{[^}]*repeating-linear-gradient/);
});

// ── ⑦ 样式与几何（口径 7/10/12/13）───────────────────────────────

test("(d) TEAM_BOARD_CSS：滚动容器 max-height + 框头 sticky + 四状态行描边各一条", () => {
  assert.match(TEAM_BOARD_CSS, /\.team-dag-scroll\s*\{[^}]*max-height:\s*var\(--team-dag-scroll-max-h,\s*380px\)/);
  assert.match(TEAM_BOARD_CSS, /\.team-dag-scroll\s*\{[^}]*overflow-y:\s*auto/);
  assert.match(TEAM_BOARD_CSS, /\.team-dag-scroll\s*\{[^}]*overscroll-behavior:\s*contain/);
  assert.match(TEAM_BOARD_CSS, /\.team-dag-frame-head\s*\{[^}]*position:\s*sticky/);
  assert.match(TEAM_BOARD_CSS, /\.team-dag-frame-head\s*\{[^}]*top:\s*0/);
  for (const eff of ["running", "done", "review", "failed"]) {
    assert.match(TEAM_BOARD_CSS, new RegExp(`\\.team-dag-row\\[data-eff="${eff}"\\]\\s*\\{[^}]*--row-line:`), `缺 ${eff} 的行描边`);
  }
  // pending 也有一条（灰 + 虚线）。
  assert.match(TEAM_BOARD_CSS, /\.team-dag-row\[data-eff="pending"\]\s*\{[^}]*--row-line:\s*var\(--status-idle\)/);
  assert.match(TEAM_BOARD_CSS, /\[data-eff="pending"\]\s+\.team-dag-card\s*\{[^}]*border-style:\s*dashed/);
  // 细滚动条随主题 + 框头吸附的承载。
  assert.match(TEAM_BOARD_CSS, /\.team-dag-scroll::-webkit-scrollbar-thumb\s*\{[^}]*var\(--border-strong\)/);
});

test("(12/13) 滚动上限是定值 380px（在 .team-board 作用域里），不跟 vh", () => {
  assert.match(TEAM_BOARD_CSS, /\.team-board\s*\{[^}]*--team-dag-scroll-max-h:\s*380px/);
  assert.doesNotMatch(TEAM_BOARD_CSS, /--team-dag-scroll-max-h:[^;]*vh/);
});

test("(10) 窄栏自适应用 container query：收缩 gutter + 折叠次级信息，纯 CSS", () => {
  assert.match(TEAM_BOARD_CSS, /\.team-board\s*\{[^}]*container-type:\s*inline-size/);
  assert.match(TEAM_BOARD_CSS, /@container\s*\(max-width:\s*520px\)/);
  const block = TEAM_BOARD_CSS.slice(TEAM_BOARD_CSS.indexOf("@container (max-width"));
  assert.match(block, /--team-dag-gutter:\s*44px/, "窄栏收缩 gutter");
  assert.match(block, /\.team-dag-ms-content[^{]*\{[^}]*display:\s*none/, "折叠次级信息");
  // 折叠不许动到几何常量：行高不变，依赖边才不会错位。
  assert.doesNotMatch(block, /--team-dag-row-h:/);
  assert.doesNotMatch(block, /--team-dag-head-h:/);
});

test("几何只在两处写：CSS 的 --team-dag-* 与渲染件的 DAG 常量必须逐字相同", () => {
  const pairs = [
    ["--team-dag-content-pad-top", 4],
    ["--team-dag-frame-pad", 4],
    ["--team-dag-head-h", 26],
    ["--team-dag-row-h", 72],
    ["--team-dag-row-gap", 4],
    ["--team-dag-gate-h", 18],
    ["--team-dag-gate-margin", 2],
    ["--team-dag-frame-border", 1],
  ];
  for (const [name, value] of pairs) {
    assert.match(TEAM_BOARD_CSS, new RegExp(`${name}:\\s*${value}px`), `${name} 必须与渲染件的 DAG 常量一致（差一线，线就落不到节点上）`);
  }
  // 渲染件那边的同一组数字（改一处必须改两处，这条用例就是那条绳子）。
  assert.match(SRC, /rowH:\s*72,/);
  assert.match(SRC, /headH:\s*26,/);
  assert.match(SRC, /gateH:\s*18,/);
});

test("TEAM_BOARD_CSS 只吃仓库既有令牌：自造色值只许出现在作用域内的归属色/框描边色板里", () => {
  assert.match(TEAM_BOARD_CSS, /\.team-board\s*\{/);
  assert.match(TEAM_BOARD_CSS, /var\(--status-running\)/);
  assert.match(TEAM_BOARD_CSS, /var\(--tint-done\)/);
  assert.match(TEAM_BOARD_CSS, /var\(--border-failed\)/);
  for (const line of TEAM_BOARD_CSS.split("\n")) {
    if (/#[0-9a-fA-F]{3,8}/.test(line)) {
      assert.match(line.trim(), /^--team-dag-(role|ms-tone)-[0-9]+:/, `字面色值只许出现在自造色板里，实际：${line.trim()}`);
    }
  }
  assert.doesNotMatch(TEAM_BOARD_CSS, /\.team-stage[\s,{[]/, "阶段口径已退场，CSS 里不得再有 .team-stage*");
  assert.doesNotMatch(TEAM_BOARD_CSS, /\.team-job[\s,{[]/);
  assert.doesNotMatch(TEAM_BOARD_CSS, /\.team-milestone-gantt/, "老里程碑卡片样式必须随旧结构一起退场");
  assert.doesNotMatch(TEAM_BOARD_CSS, /\.team-gantt[\s,{[]/, "老甘特容器样式必须退场");
});

// ── ⑤ 成员行的插件装配（2026-10-05）：声明面 chips + 生效读数 + 黄牌 / 失灵 ──
//
// 数据两格（后端 commit 39a8b63，app.js 原样透传 members）：member.plugins = 声明面，
// member.assembly = 生效读数。装配是**teammate 级**，所以夹具里配一个工作项，行才出得来。

function assemblyPlan(member) {
  return {
    members: [{ role: "exec", ...member }],
    work_items: [{ id: "wi-1", role: "exec", name: "实现", status: "running" }],
  };
}

test("⑤ replace 装配：每个插件一枚 chip + 技能/目录/token/工具面一行读数 + teammate 级文案", () => {
  const html = renderTeamQueue(assemblyPlan({
    plugins: ["impeccable", "board-kit"],
    assembly: {
      mode: "replace", plugin_count: 2,
      skill_count: 3, skill_catalog_runes: 1200, skill_catalog_tokens_est: 400,
      plugin_face_tools: 5, total_tools: 12,
    },
  }));
  assert.match(html, /data-assembly-mode="replace"/);
  assert.match(html, /<span class="chip team-assembly-plugin" title="impeccable">impeccable<\/span>/);
  assert.match(html, /<span class="chip team-assembly-plugin" title="board-kit">board-kit<\/span>/);
  assert.match(html, /技能 3 \/ 目录 1200B · ≈400 tok \/ 工具面 5\/12/);
  // 窄栏会被省略号吃掉（审查 Hypothesis）：title 里放同一句话的全文，悬停可读全。
  assert.match(html, /title="技能 3 \/ 目录 1200B · ≈400 tok \/ 工具面 5\/12（生效读数：插件面工具数是上界/);
  // 文案说清这是 teammate 级的：对这位手上的每个工作项会话都生效。
  assert.match(html, /teammate 级 · 对这位每个工作项会话都生效/);
});

test("⑤ inherit-host：显式写「继承宿主」，不靠字段缺失暗示，也不给读数行", () => {
  const html = renderTeamQueue(assemblyPlan({ plugins: [], assembly: { mode: "inherit-host" } }));
  assert.match(html, /data-assembly-mode="inherit-host"/);
  assert.match(html, /team-assembly-inherit[^>]*>继承宿主</);
  assert.doesNotMatch(html, /技能 \d+ \/ 目录/, "继承宿主没有目录段读数");
  assert.doesNotMatch(html, /team-assembly-plugin/, "空集不装插件：没有声明 chips");
});

// 2026-10-05 审查 P0 修正：`dto/teamwork_board.go` 的 Assembly 注释写的是
// "Assembly 为 nil 表示**桥这一侧给不出读数**（未装配插件域 / 成员行不在读数里）：
//  前端据此**不显示装配格**，而不是把缺失读成'0 个工具、0 份技能'"。
// 所以"缺失"既不许写成「按不覆盖处理」（那是把缺失当读数），也不许写成「继承宿主」
// （那是替后端断言前端无从知道的语义——"空集 = 不覆盖"只能由 mode=inherit-host 回答）。
test("⑤ assembly 为 nil → 不显示装配格（钉住「没有装配格」，不留空壳也不自称继承宿主）", () => {
  const html = renderTeamQueue(assemblyPlan({ plugins: [] }));
  assert.doesNotMatch(html, /team-member-assembly/, "读数给不出：整格退场");
  assert.doesNotMatch(html, /继承宿主/, "缺失 ≠ 空集，不许自称继承宿主");
  assert.doesNotMatch(html, /装配读数缺失|按不覆盖处理/, "降级文案连同降级分支一起删掉");
  assert.doesNotMatch(html, /data-assembly-mode/, "没有读数就没有 mode 结论");
  assert.doesNotMatch(html, /技能 \d+ \/ 目录/);
});

test("⑤ 声明非空 + assembly 为 nil → 只列声明 chips，不写任何 mode 结论（不同屏自相矛盾）", () => {
  const html = renderTeamQueue(assemblyPlan({ plugins: ["impeccable", "board-kit"] }));
  // 声明面是**计划**的事实：照列（title 留全名），这一格不假装自己知道生效面。
  assert.match(html, /<span class="chip team-assembly-plugin" title="impeccable">impeccable<\/span>/);
  assert.match(html, /<span class="chip team-assembly-plugin" title="board-kit">board-kit<\/span>/);
  assert.doesNotMatch(html, /继承宿主/, "声明了插件就不许再自称继承宿主（两句互斥，同屏即自相矛盾）");
  assert.doesNotMatch(html, /data-assembly-mode/, "读数缺失：不下 mode 结论");
  assert.doesNotMatch(html, /team-assembly-readout|team-assembly-inherit/);
  assert.doesNotMatch(html, /装配读数缺失|按不覆盖处理/);
});

test("⑤ 读数在但没写明 mode → 同样不下 mode 结论（不靠字段缺失暗示）", () => {
  const declared = renderTeamQueue(assemblyPlan({ plugins: ["impeccable"], assembly: {} }));
  assert.match(declared, /team-assembly-plugin/);
  assert.doesNotMatch(declared, /继承宿主/);
  assert.doesNotMatch(declared, /data-assembly-mode/);
  const bare = renderTeamQueue(assemblyPlan({ plugins: [], assembly: {} }));
  assert.doesNotMatch(bare, /team-member-assembly/);
});

test("⑤ 黄牌：显式标记且 title = yellow_reason；只报不拒，与读数/插件 chips 同屏", () => {
  const html = renderTeamQueue(assemblyPlan({
    plugins: ["big"],
    assembly: { mode: "replace", skill_count: 9, yellow: true, yellow_reason: "技能目录超阈值：≈8200 tok > 6k" },
  }));
  assert.match(html, /<span class="chip team-assembly-yellow" title="技能目录超阈值：≈8200 tok &gt; 6k">黄牌<\/span>/);
  assert.match(html, /team-assembly-plugin/, "黄牌只报不拒：插件 chips 仍在");
  assert.match(html, /技能 9 \/ 目录 0B · ≈0 tok/, "黄牌不替换读数");
});

test("⑤ 失灵 / 已撤：显式标记，且**不得**被渲染成「没装配」（继承宿主）", () => {
  const html = renderTeamQueue(assemblyPlan({
    plugins: ["gone", "moved"],
    assembly: {
      mode: "replace", plugin_face_tools: 0, total_tools: 12,
      plugin_face_faulted: true, plugin_face_missing: ["gone"],
      plugin_face_note: "声明 gone / moved，现已失灵，工具面为空",
    },
  }));
  assert.match(html, /data-assembly-mode="replace"/, "失灵仍是 replace：不许落进继承宿主那一支");
  assert.match(html, /<span class="chip team-assembly-faulted"[^>]*>失灵<\/span>/);
  assert.match(html, /<span class="chip team-assembly-missing"[^>]*>已撤 1<\/span>/);
  assert.match(html, /声明 gone \/ moved，现已失灵，工具面为空/);
  assert.doesNotMatch(html, /继承宿主/, "失灵与没装配语义相反，不许混渲染");
});

test("⑤ 长插件名不撑破行：chips 容器换行 + 单枚限宽截断（title 留全名）", () => {
  const long = "a-very-long-plugin-name-that-would-overflow-the-member-row-".repeat(2);
  const html = renderTeamQueue(assemblyPlan({ plugins: [long], assembly: { mode: "replace" } }));
  assert.match(html, new RegExp(`title="${long}"`), "title 里保留全名");
  const visible = html.match(/<span class="chip team-assembly-plugin"[^>]*>([^<]*)<\/span>/)[1];
  assert.ok(visible.length < long.length, "行内可见文本要截断");
  assert.match(TEAM_BOARD_CSS, /\.team-assembly-plugin\s*\{[^}]*max-width:\s*160px/);
  assert.match(TEAM_BOARD_CSS, /\.team-assembly-plugin\s*\{[^}]*text-overflow:\s*ellipsis/);
  assert.match(TEAM_BOARD_CSS, /\.team-member-assembly\s*\{[^}]*flex-wrap:\s*wrap/);
});

test("⑤ 装配读数转义（注入 <img onerror=...> 不许出裸标签）", () => {
  const html = renderTeamQueue(assemblyPlan({
    plugins: ['<img src=x onerror="boom">'],
    assembly: { mode: "replace", yellow: true, yellow_reason: '<img src=x onerror="boom">' },
  }));
  assert.doesNotMatch(html, /<img/);
  assert.doesNotMatch(html, /<script|<\/script>/);
  assert.match(html, /&lt;img src=x onerror=&quot;boom&quot;&gt;/);
});
