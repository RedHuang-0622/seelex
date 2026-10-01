import test from "node:test";
import assert from "node:assert/strict";
import {
  TEAM_BOARD_CSS,
  formatEventTime,
  jobStageID,
  milestoneStatus,
  orderStages,
  ownStageStatus,
  renderTeamBoard,
  renderTeamMilestones,
  renderTeamRoster,
  stageStatuses,
  summarizeTeam,
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

// 夹具二：V 模型模板骨架（$teamwork 的「V 模型阶段模板」）——左腿四阶段 + 右腿三阶段 + 收口。
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
