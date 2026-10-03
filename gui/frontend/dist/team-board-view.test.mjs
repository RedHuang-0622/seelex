import test from "node:test";
import assert from "node:assert/strict";
import {
  TEAM_BOARD_CSS,
  formatEventTime,
  itemStatus,
  itemsOfMilestone,
  jobStageID,
  memberMessagesOf,
  memberQueueOf,
  milestoneStatus,
  orderMilestones,
  orderStages,
  orderWorkItems,
  ownStageStatus,
  renderTeamBoard,
  renderTeamGantt,
  renderTeamMilestones,
  renderTeamQueue,
  renderTeamRoster,
  renderWorkItemSessionPanel,
  stageStatuses,
  summarizeTeam,
  workItemsOf,
} from "./team-board-view.js";

// 夹具一：真实计划快照（team queue-strip-fix，2026-10-01 会话
// project-03b5b8d29af3f44b/session-b90ba1b5a3db5909/teamwork/events.jsonl 里的 plan 事件：
// stages=3 members=3 milestones=3）。作业 a1 是该计划 fix 阶段的真实在跑作业。
const PLAN = {
  team_id: "queue-strip-fix",
  version: 1,
  stages: [
    { id: "fix", roles: ["impl"], depends_on: [] },
    { id: "verify", roles: ["verify"], depends_on: ["fix"] },
    { id: "review", roles: ["review"], depends_on: ["verify"] },
  ],
  members: [
    { role: "impl", role_session_id: "draft_1-queue-strip-fix-impl", tools_policy: "readwrite" },
    { role: "verify", role_session_id: "draft_1-queue-strip-fix-verify", tools_policy: "readwrite" },
    { role: "review", role_session_id: "draft_1-queue-strip-fix-review", tools_policy: "readonly" },
  ],
  milestones: [
    { id: "m-fix", after: ["fix"] },
    { id: "m-verify", after: ["verify"] },
    { id: "m-review", after: ["review"] },
  ],
};

const RUNNING_JOB = {
  handle: "a1",
  seq: 1,
  kind: "worker",
  state: "running",
  node: "fix",
  bytes: 0,
  exit_code: 0,
  scope: { session: "draft_1", subject: "emp_impl" },
};

// 夹具二：八阶段骨架（左腿四阶段 + 右腿三阶段 + 一个 `review`/`tl` 收口阶段）。
//
// 注意它与 `$teamwork` 当前模板**不同**：模板已去掉 `tl` 角色与末尾 `review` 阶段（复核与
// 收口由 leader 本人做，2026-10-02）。这里刻意留着它——渲染件不认模板，**任何**计划形状
// 都要能画，而历史计划（带 review/tl）正是会被读到的那些。夹具不是模板的副本，是渲染件的
// 输入样本。
const V_PLAN = {
  team_id: "v-model",
  version: 4,
  stages: [
    { id: "req", roles: ["pm"] },
    { id: "design", roles: ["arch"], depends_on: ["req"] },
    { id: "contract_review", roles: ["contract_review"], depends_on: ["design"] },
    { id: "impl", roles: ["exec"], depends_on: ["contract_review"] },
    { id: "accept", roles: ["test_case"], depends_on: ["req", "impl"] },
    { id: "integration", roles: ["test_case"], depends_on: ["design", "impl"] },
    { id: "unit", roles: ["test_case"], depends_on: ["impl"] },
    { id: "review", roles: ["tl"], depends_on: ["accept", "integration", "unit"] },
  ],
  members: [
    { role: "pm", worktree: "seelex/pm" },
    { role: "arch", worktree: "seelex/arch" },
    { role: "contract_review", tools_policy: "readonly" },
    { role: "exec", worktree: "seelex/exec" },
    { role: "test_case", worktree: "seelex/test-case" },
    { role: "tl", tools_policy: "readonly" },
  ],
  milestones: [
    { id: "m-design", after: ["design"] },
    { id: "m-contract", after: ["contract_review"], status: "done", content: "契约冻结：team-board-view.js 的入参 = plan + jobs + events。" },
    { id: "m-impl", after: ["impl"] },
    { id: "m-verify", after: ["review"] },
  ],
};

// ── 顺序：depends_on 是唯一事实 ──────────────────────────────────

test("orderStages 按 depends_on 拓扑排序并给层号（L0 依赖已满足，可并行）", () => {
  const plain = orderStages(PLAN).map(entry => [entry.id, entry.depth]);
  assert.deepEqual(plain, [["fix", 0], ["verify", 1], ["review", 2]]);
  const v = orderStages(V_PLAN).map(entry => [entry.id, entry.depth]);
  assert.deepEqual(v, [
    ["req", 0],
    ["design", 1],
    ["contract_review", 2],
    ["impl", 3],
    ["accept", 4],
    ["integration", 4],
    ["unit", 4],
    ["review", 5],
  ]);
});

test("orderStages 层号取最长依赖链：accept 依赖 req(0)+impl(3) → L4，不是 L1", () => {
  const entries = new Map(orderStages(V_PLAN).map(entry => [entry.id, entry]));
  assert.equal(entries.get("accept").depth, 4);
  assert.deepEqual(entries.get("accept").depends_on, ["req", "impl"]);
});

test("orderStages 依赖指向不存在的阶段时显形为 missing_deps，不静默丢阶段", () => {
  const plan = { team_id: "t", version: 1, stages: [{ id: "solo", roles: ["r"], depends_on: ["ghost"] }] };
  const [entry] = orderStages(plan);
  assert.equal(entry.id, "solo");
  assert.equal(entry.depth, 0);
  assert.deepEqual(entry.missing_deps, ["ghost"]);
  assert.equal(entry.cyclic, false);
});

test("orderStages 依赖成环时把剩下的阶段接在末尾并标 cyclic", () => {
  const plan = {
    team_id: "t",
    version: 1,
    stages: [
      { id: "a", roles: ["r"], depends_on: ["b"] },
      { id: "b", roles: ["r"], depends_on: ["a"] },
      { id: "c", roles: ["r"] },
    ],
  };
  const entries = orderStages(plan);
  assert.deepEqual(entries.map(entry => entry.id), ["c", "a", "b"]);
  assert.deepEqual(entries.map(entry => entry.cyclic), [false, true, true]);
});

// ── 归属：作业 → 阶段 ───────────────────────────────────────────

test("jobStageID 依次回落 stage → node → emp_<role>（角色首次出现的阶段）", () => {
  assert.equal(jobStageID(PLAN, { stage: "verify", node: "fix" }), "verify");
  assert.equal(jobStageID(PLAN, { node: "verify" }), "verify");
  assert.equal(jobStageID(PLAN, { scope: { subject: "emp_verify" } }), "verify");
  assert.equal(jobStageID(PLAN, { role: "review" }), "review");
  assert.equal(jobStageID(PLAN, { scope: { subject: "emp_nobody" } }), "");
  assert.equal(jobStageID(PLAN, {}), "");
});

test("jobStageID 对同一角色多层验证（V 模板的 test_case）只认首次出现的阶段", () => {
  const job = { scope: { subject: "emp_test_case" } };
  assert.equal(jobStageID(V_PLAN, job), "accept");
});

// ── 阶段状态：依赖边 + 作业终态 ──────────────────────────────────

test("ownStageStatus 只认终态事实：失败 > 全完 > 其余算在途", () => {
  assert.equal(ownStageStatus([]), "");
  assert.equal(ownStageStatus([{ state: "done" }]), "done");
  assert.equal(ownStageStatus([{ state: "done" }, { state: "done" }]), "done");
  assert.equal(ownStageStatus([{ state: "running" }]), "running");
  assert.equal(ownStageStatus([{ state: "queued" }]), "running");
  assert.equal(ownStageStatus([{ state: "" }]), "running");
  assert.equal(ownStageStatus([{ state: "done" }, { state: "running" }]), "running");
  assert.equal(ownStageStatus([{ state: "failed" }, { state: "running" }]), "failed");
  assert.equal(ownStageStatus([{ state: "killed" }]), "failed");
});

test("stageStatuses：fix 在跑 → 下游 verify/review 只能是 pending（依赖边没满足）", () => {
  const status = stageStatuses(PLAN, [RUNNING_JOB]);
  assert.equal(status.get("fix"), "running");
  assert.equal(status.get("verify"), "pending");
  assert.equal(status.get("review"), "pending");
});

test("stageStatuses：fix 完成 → verify 变 ready（可派活），review 仍 pending", () => {
  const status = stageStatuses(PLAN, [{ ...RUNNING_JOB, state: "done", bytes: 2048, exit_code: 0 }]);
  assert.equal(status.get("fix"), "done");
  assert.equal(status.get("verify"), "ready");
  assert.equal(status.get("review"), "pending");
});

test("stageStatuses：失败沿依赖边挡住下游，且不把下游读成 ready", () => {
  const status = stageStatuses(PLAN, [{ ...RUNNING_JOB, state: "failed", exit_code: 1 }]);
  assert.equal(status.get("fix"), "failed");
  assert.equal(status.get("verify"), "pending");
});

test("stageStatuses：无依赖且未派发的阶段是 ready（计划已写 = 可以派）", () => {
  const status = stageStatuses(PLAN, []);
  assert.equal(status.get("fix"), "ready");
  assert.equal(status.get("verify"), "pending");
});

// ── 汇总 ────────────────────────────────────────────────────────

test("summarizeTeam 只报事实计数（阶段 / 在编 / 作业 / 里程碑）", () => {
  const summary = summarizeTeam(PLAN, [RUNNING_JOB]);
  assert.deepEqual(summary, {
    stages: 3,
    done: 0,
    running: 1,
    failed: 0,
    members: 3,
    milestones_total: 3,
    milestones_done: 0,
    jobs_running: 1,
    jobs_failed: 0,
    jobs_done: 0,
  });
  const vSummary = summarizeTeam(V_PLAN, []);
  assert.equal(vSummary.members, 6);
  assert.equal(vSummary.milestones_total, 4);
  assert.equal(vSummary.milestones_done, 1);
});

test("milestoneStatus：空状态是 pending（sessionstore 口径），未知取值原样返回", () => {
  assert.equal(milestoneStatus({ id: "m" }), "pending");
  assert.equal(milestoneStatus({ id: "m", status: "DONE" }), "done");
  assert.equal(milestoneStatus({ id: "m", status: "weird" }), "weird");
});

// ── 渲染 ────────────────────────────────────────────────────────

test("renderTeamBoard：没有计划 / 计划里没有阶段 → 不留空壳", () => {
  assert.equal(renderTeamBoard(), "");
  assert.equal(renderTeamBoard({ plan: null }), "");
  assert.equal(renderTeamBoard({ plan: { team_id: "t", version: 1, stages: [] } }), "");
  assert.equal(renderTeamBoard({ plan: { team_id: "t", version: 1 } }), "");
});

test("renderTeamBoard：真实计划 + 真实在跑作业渲染出阶段卡、在编表、里程碑与审计行", () => {
  const html = renderTeamBoard({
    plan: PLAN,
    jobs: [RUNNING_JOB],
    maxMembers: 6,
    events: [
      { at: "2026-10-01T13:34:07.8545913Z", kind: "plan", team_id: "queue-strip-fix", detail: "stages=3 members=3 milestones=3" },
      { at: "2026-10-01T13:34:12.8618115Z", kind: "dispatch", stage: "fix", role: "impl", handle: "a1", detail: "阶段 fix：修排队条动作按钮" },
      { at: "2026-10-01T13:37:00.4202564Z", kind: "join", handle: "a1", detail: "汇合窗口用尽，仍在跑 1 条：a1=running" },
    ],
  });
  assert.match(html, /data-team-id="queue-strip-fix"/);
  assert.match(html, /阶段 0\/3 完成 · 1 在跑 · 在编 3\/6/);
  assert.match(html, /data-stage-id="fix" data-status="running" data-depth="0"/);
  assert.match(html, /data-stage-id="verify" data-status="pending" data-depth="1"/);
  assert.match(html, /data-handle="a1"/);
  assert.match(html, /emp_impl · running · 0B 输出/);
  assert.match(html, /data-team-roster/);
  assert.match(html, /data-team-milestones/);
  assert.match(html, /data-milestone-id="m-fix" data-status="pending"/);
  assert.match(html, /data-kind="dispatch"/);
});

test("renderTeamBoard 转义所有外部文本（team_id / 阶段 id / 描述都不能注入）", () => {
  const plan = {
    team_id: '<img src=x onerror="boom">',
    version: 1,
    stages: [{ id: '<b>s</b>', roles: ['<i>r</i>'], depends_on: [] }],
  };
  const html = renderTeamBoard({ plan, jobs: [{ handle: "<u>a1</u>", state: "running", node: "<b>s</b>" }] });
  assert.doesNotMatch(html, /<img/);
  assert.doesNotMatch(html, /<b>s<\/b>/);
  assert.doesNotMatch(html, /<i>r<\/i>/);
  assert.doesNotMatch(html, /<u>a1<\/u>/);
  assert.match(html, /&lt;img src=x onerror=&quot;boom&quot;&gt;/);
  assert.match(html, /<span class="chip team-role">&lt;i&gt;r&lt;\/i&gt;<\/span>/);
});

test("renderTeamBoard：句柄投影过期时显形（jobs I-4），不假装是最新事实", () => {
  const html = renderTeamBoard({ plan: PLAN, jobs: [], stale: true });
  assert.match(html, /句柄投影可能过期/);
  assert.doesNotMatch(renderTeamBoard({ plan: PLAN, jobs: [] }), /句柄投影可能过期/);
});

test("renderTeamBoard：从存档恢复的看板显形（recovered），与 stale 同屏而不互斥", () => {
  // recovered = 这份看板来自会话存档快照（活体投影给不出时才兜底）。
  const recoveredOnly = renderTeamBoard({ plan: PLAN, jobs: [], recovered: true });
  assert.match(recoveredOnly, /自快照恢复/);
  assert.doesNotMatch(recoveredOnly, /句柄投影可能过期/);
  // 恢复出来的看板两者同时为真：存档里的作业行也一律是上一个进程的句柄。
  const both = renderTeamBoard({ plan: PLAN, jobs: [], recovered: true, stale: true });
  assert.match(both, /自快照恢复/);
  assert.match(both, /句柄投影可能过期/);
  // 活体投影：两个标记都不出现（默认就是没有痕迹）。
  assert.doesNotMatch(renderTeamBoard({ plan: PLAN, jobs: [] }), /自快照恢复/);
});

test("renderTeamStageCard 把无依赖写成 —，把缺失依赖/成环写成告警行", () => {
  const html = renderTeamBoard({ plan: PLAN, jobs: [] });
  assert.match(html, /<span class="team-label">依赖<\/span><span class="muted">—<\/span>/);
  const broken = renderTeamBoard({
    plan: {
      team_id: "t",
      version: 1,
      stages: [
        { id: "a", roles: ["r"], depends_on: ["ghost"] },
        { id: "b", roles: ["r"], depends_on: ["b"] },
      ],
    },
  });
  assert.match(broken, /依赖缺失：ghost/);
  assert.match(broken, /依赖成环/);
});

test("renderTeamRoster：只读权责显形、未指派工作区写主工作区、角色会话号进 title", () => {
  const html = renderTeamRoster(PLAN, [RUNNING_JOB]);
  assert.match(html, /class="chip team-policy is-readonly">readonly<\/span>/);
  assert.match(html, /class="chip team-policy is-readwrite">readwrite<\/span>/);
  assert.match(html, /title="未指派工作区（回退主工作区）">主工作区</);
  assert.match(html, /title="draft_1-queue-strip-fix-review"/);
  assert.match(html, /data-role="impl"/);
  assert.equal(renderTeamRoster({ team_id: "t", members: [] }, []), "");
});

test("renderTeamRoster：在编行的角色名是成员会话入口（缺会话号退化为纯文本）", () => {
  // 入口 = 「这位此刻在干什么」的唯一入口：钩子与团队面板成员行同款
  // （app.js 的看板委托把它们接到同一个 openRoleSessionDetail 上）。
  const html = renderTeamRoster(PLAN, []);
  assert.match(html, /<button type="button" class="team-member-role is-openable" data-team-role-open="impl" data-team-role-session="draft_1-queue-strip-fix-impl"/);
  assert.match(html, /aria-label="打开 review 的员工会话"/);
  // 降级输入：没有 role_session_id 就不渲染按钮——点不动的入口比没有入口更坏。
  const degraded = renderTeamRoster({ team_id: "t", members: [{ role: "pm" }, { role: "exec", role_session_id: "  " }] }, []);
  assert.match(degraded, /<span class="team-member-role">pm<\/span>/);
  assert.doesNotMatch(degraded, /data-team-role-open/);
  // 角色名进属性前仍然过转义（与文本同一口径）。
  const escaped = renderTeamRoster({ team_id: "t", members: [{ role: '<img src=x>', role_session_id: "s1" }] }, []);
  assert.doesNotMatch(escaped, /<img src=x>/);
  assert.match(escaped, /data-team-role-open="&lt;img src=x&gt;"/);
});

test("renderTeamMilestones：无内容写「尚无内容」，有内容按截断展示并留全文 title", () => {
  const html = renderTeamMilestones(V_PLAN);
  assert.match(html, /data-milestone-id="m-design" data-status="pending"/);
  assert.match(html, /data-milestone-id="m-contract" data-status="done"/);
  assert.match(html, /<span class="team-label">判据<\/span><span class="chip team-dep">design<\/span>/);
  assert.match(html, /class="team-milestone-content is-empty" title="">尚无内容</);
  assert.match(html, /契约冻结/);
  assert.equal(renderTeamMilestones({ team_id: "t" }), "");
});

test("审计只显示最近 6 条且最新在最上面，时间取 HH:MM", () => {
  const events = Array.from({ length: 8 }, (_, index) => ({
    at: `2026-10-01T13:0${index}:00.0000000Z`,
    kind: "dispatch",
    stage: `s${index}`,
  }));
  const html = renderTeamBoard({ plan: PLAN, jobs: [], events });
  const shown = [...html.matchAll(/data-kind="dispatch"/g)].length;
  assert.equal(shown, 6);
  assert.doesNotMatch(html, /s0</);
  assert.match(html, /s7/);
  assert.match(html, /<time class="team-event-at">13:07<\/time>/);
  assert.equal(formatEventTime("not-a-time"), "not-a-time");
  assert.equal(formatEventTime(""), "—");
});

test("TEAM_BOARD_CSS 只吃语义 token（不写死色值），并覆盖五种阶段状态", () => {
  assert.match(TEAM_BOARD_CSS, /\.team-board\s*\{/);
  for (const status of ["running", "done", "failed", "ready", "pending"]) {
    assert.match(TEAM_BOARD_CSS, new RegExp(`\\.team-stage\\[data-status="${status}"\\]`));
  }
  assert.match(TEAM_BOARD_CSS, /var\(--status-running\)/);
  assert.doesNotMatch(TEAM_BOARD_CSS, /#[0-9a-fA-F]{6}/);
});

// ── Work Item 口径（2026-10-03 重构）────────────────────────────────
//
// 计划里有工作项时，看板画的是**里程碑甘特 + teammate 队列**：里程碑之间串行
// （depends_on 屏障），里程碑内部按工作项依赖 DAG 并行；一个 Work Item 一个 teammate
// 一套 Session + worktree。

const ITEM_PLAN = {
  team_id: "v-model",
  version: 1,
  members: [
    { role: "pm", role_session_id: "s-v-model-pm", status: "idle", queue: ["需求澄清"] },
    { role: "exec", role_session_id: "s-v-model-exec", status: "running", queue: ["实现"], messages: [{ at: 1, role: "exec", work_item: "wi-impl", text: "[wi-impl] 实现：跑完，改动已合并回主工作区，等待 leader 评估" }] },
    { role: "test_case", role_session_id: "s-v-model-test", status: "idle", queue: ["用例跟进"] },
  ],
  milestones: [
    { id: "m-build", name: "构建", status: "active" },
    { id: "m-ship", name: "发布", depends_on: ["m-build"], status: "pending" },
  ],
  work_items: [
    { id: "wi-req", milestone: "m-build", role: "pm", name: "需求澄清", status: "done", session_id: "", worktree: "" },
    { id: "wi-impl", milestone: "m-build", role: "exec", name: "实现", status: "running", depends_on: ["wi-req"], session_id: "s-v-model-exec-wi-wi-impl", worktree: "seelex/exec-wi-impl", live: true, interrupted: true, handle: "a7" },
    { id: "wi-test", milestone: "m-build", role: "test_case", name: "用例跟进", status: "pending", depends_on: ["wi-impl"] },
    { id: "wi-ship", milestone: "m-ship", role: "exec", name: "发布", status: "pending", depends_on: [] },
  ],
};

test("workItemsOf / itemsOfMilestone / itemStatus 只搬事实（空状态 = pending）", () => {
  assert.equal(workItemsOf(ITEM_PLAN).length, 4);
  assert.equal(itemsOfMilestone(ITEM_PLAN, "m-build").length, 3);
  assert.equal(itemStatus(ITEM_PLAN.work_items[1]), "running");
  assert.equal(itemStatus({ id: "x", name: "x" }), "pending");
  assert.deepEqual(workItemsOf({}), []);
});

test("orderMilestones 按屏障依赖排层号；缺依赖与成环都显形", () => {
  const ordered = orderMilestones(ITEM_PLAN);
  assert.deepEqual(ordered.map(entry => [entry.id, entry.depth]), [["m-build", 0], ["m-ship", 1]]);
  const missing = orderMilestones({ milestones: [{ id: "a", depends_on: ["ghost"] }, { id: "b" }] });
  assert.deepEqual(missing[0].missing_deps, ["ghost"]);
  const cyclic = orderMilestones({ milestones: [{ id: "a", depends_on: ["b"] }, { id: "b", depends_on: ["a"] }] });
  assert.equal(cyclic.filter(entry => entry.cyclic).length, 2, "成环的两个里程碑都要显形（都不静默丢）");
});

test("orderWorkItems 只在里程碑内排 DAG，并把「前置没验收」标成被卡住", () => {
  const entries = orderWorkItems(itemsOfMilestone(ITEM_PLAN, "m-build"));
  assert.deepEqual(entries.map(entry => entry.id), ["wi-req", "wi-impl", "wi-test"]);
  const impl = entries.find(entry => entry.id === "wi-impl");
  const test_ = entries.find(entry => entry.id === "wi-test");
  assert.equal(impl.blocked, false, "前置 wi-req 已 done → 不该被卡住");
  assert.equal(test_.blocked, true, "前置 wi-impl 还在跑 → 被依赖卡住（与后端的依赖闸门同源）");
});

test("memberQueueOf / memberMessagesOf：队列是没完成的工作项，消息是尾插回执", () => {
  assert.deepEqual(memberQueueOf(ITEM_PLAN, "exec"), ["实现"]);
  assert.equal(memberMessagesOf(ITEM_PLAN, "exec").length, 1);
  // 后端没给 queue 时按工作项自行派生（同一条判据：未完成才占队列）。
  const derived = { ...ITEM_PLAN, members: [{ role: "pm" }, { role: "exec" }] };
  assert.deepEqual(memberQueueOf(derived, "exec"), ["实现", "发布"], "exec 名下两件未完成的事都在队列里");
  assert.deepEqual(memberQueueOf(derived, "pm"), [], "wi-req 已 done → 已销项，不占队列");
  assert.deepEqual(memberMessagesOf(derived, "exec"), []);
});

test("renderTeamGantt 画里程碑屏障 + 里程碑内工作项，并给出子页面入口", () => {
  const html = renderTeamGantt(ITEM_PLAN);
  assert.match(html, /data-team-gantt/);
  assert.match(html, /data-milestone-id="m-build" data-status="active" data-depth="0"/);
  assert.match(html, /data-milestone-id="m-ship" data-status="pending" data-depth="1"/);
  assert.match(html, /<span class="team-label">屏障<\/span><span class="chip team-dep">m-build<\/span>/);
  assert.match(html, /data-item-id="wi-impl" data-status="running"/);
  // 子页面入口：带会话的工作项才可点（点不动的入口比没有入口更坏）。
  assert.match(html, /data-team-item-open="wi-impl" data-team-item-session="s-v-model-exec-wi-wi-impl"/);
  assert.doesNotMatch(html, /data-team-item-open="wi-test"/);
  // 被依赖卡住 + 可重派 + 现场在，都要显形。
  assert.match(html, /被依赖卡住/);
  assert.match(html, /可重派/);
  assert.match(html, /现场在/);
  assert.equal(renderTeamGantt({ team_id: "t" }), "");
});

test("renderTeamGantt 不用时间表达任何东西（甘特只表示依赖）", () => {
  const html = renderTeamGantt(ITEM_PLAN);
  assert.doesNotMatch(html, /started_at|finished_at|datetime|duration|时长|耗时/);
});

test("renderTeamQueue 画 teammate 的名字 / 状态 / 工作项队列 / 尾插回执", () => {
  const html = renderTeamQueue(ITEM_PLAN);
  assert.match(html, /data-team-queue/);
  assert.match(html, /data-role="exec" data-status="running"/);
  assert.match(html, /<span class="chip team-queue-item">实现<\/span>/);
  assert.match(html, /改动已合并回主工作区/);
  assert.equal(renderTeamQueue({ members: [{ role: "a" }] }), "", "没有工作项就不画这一节（不留空壳）");
});

test("renderTeamBoard：只有工作项、没有阶段也要画出来（迁移期两块数据面并存）", () => {
  const html = renderTeamBoard({ plan: ITEM_PLAN, jobs: [], events: [] });
  assert.match(html, /data-team-gantt/);
  assert.match(html, /data-team-queue/);
  assert.match(html, /data-team-id="v-model"/);
  assert.equal(renderTeamBoard({ plan: { team_id: "t" } }), "");
});

test("renderWorkItemSessionPanel 是执行进度子页面：只有上面的条目，没有下面的表格", () => {
  const html = renderWorkItemSessionPanel(ITEM_PLAN, "wi-impl");
  assert.match(html, /data-team-item-panel="wi-impl"/);
  for (const key of ["工作项", "名称", "执行 teammate", "状态", "会话", "工作区", "结论"]) {
    assert.match(html, new RegExp(`role-kv-key">${key}<`));
  }
  assert.match(html, /s-v-model-exec-wi-wi-impl/);
  assert.match(html, /seelex\/exec-wi-impl/);
  // 「不要下面的表格」：子页面里没有表格元素。
  assert.doesNotMatch(html, /<table|<thead|<tbody|role-row/);
  const gone = renderWorkItemSessionPanel(ITEM_PLAN, "wi-nope");
  assert.match(gone, /已不在计划里/);
  assert.equal(renderWorkItemSessionPanel({}, "x"), '<div class="role-session-view is-empty">这件事已不在计划里（可能已收口）</div>');
});

test("TEAM_BOARD_CSS 覆盖 Work Item 口径的四种状态与子页面条目", () => {
  for (const status of ["running", "review", "done", "failed", "pending"]) {
    assert.match(TEAM_BOARD_CSS, new RegExp(`\\.team-item\\[data-status="${status}"\\]`));
  }
  assert.match(TEAM_BOARD_CSS, /\.team-item-name\.is-openable/);
  assert.match(TEAM_BOARD_CSS, /\.team-item-panel \.role-kv-value/);
  assert.doesNotMatch(TEAM_BOARD_CSS, /#[0-9a-fA-F]{6}/);
});
