import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  TEAM_BOARD_CSS,
  formatEventTime,
  itemDepsOf,
  itemStatus,
  itemsOfMilestone,
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
  renderTeamGantt,
  renderTeamQueue,
  renderTeamWorkItem,
  renderWorkItemSessionPanel,
  summarizeTeam,
  workItemsOf,
} from "./team-board-view.js";

const SRC = readFileSync(new URL("./team-board-view.js", import.meta.url), "utf8");
const APP = readFileSync(new URL("./app.js", import.meta.url), "utf8");

// 夹具一：里程碑 + 工作项口径的计划（**没有 stages**，2026-10-03 Work Item 形状）。
// 一个 Work Item 一个 teammate 一套 Session + worktree。
//
// 覆盖：里程碑屏障（m-ship 依赖 m-build）+ 里程碑内 DAG（wi-impl←wi-req、wi-test←wi-impl）。
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
      worktree: "seelex/exec-wi-impl", live: true, interrupted: true, handle: "a7",
    },
    { id: "wi-test", milestone: "m-build", role: "test_case", name: "用例跟进", status: "pending", depends_on: ["wi-impl"] },
    { id: "wi-ship", milestone: "m-ship", role: "exec", name: "发布", status: "pending" },
  ],
};

// 夹具二：老口径的计划——**只有 stages、没有里程碑也没有工作项**。看板不得回退到阶段分组。
const STAGES_ONLY = {
  team_id: "legacy",
  version: 1,
  stages: [
    { id: "fix", roles: ["impl"], depends_on: [] },
    { id: "verify", roles: ["verify"], depends_on: ["fix"] },
  ],
  members: [{ role: "impl", role_session_id: "s-legacy-impl" }],
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
  assert.equal(memberStatusOf(MS_PLAN, "pm"), "idle");
  assert.equal(memberStatusOf({ members: [{ role: "x" }] }, "x"), "idle");
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

// ── ① 里程碑卡片列出它名下的 Work Item ───────────────────────────

test("① 每个里程碑卡片列出名下工作项（名称 / 描述 / 达成目标 / 执行 teammate / 依赖）", () => {
  const html = renderTeamGantt(MS_PLAN);
  assert.match(html, /data-team-gantt/);
  // m-build 名下列 3 件（含全部要求的字段），m-ship 列 1 件。
  assert.match(html, /data-milestone-id="m-build" data-status="active" data-depth="0"/);
  assert.match(html, /data-milestone-id="m-ship" data-status="pending" data-depth="1"/);
  assert.match(html, /data-item-id="wi-impl" data-status="running"/);
  assert.match(html, /data-item-id="wi-ship" data-status="pending"/);
  // 名称（带会话 = 可点子页面入口）。
  assert.match(html, /data-team-item-open="wi-impl" data-team-item-session="s-v-model-exec-wi-wi-impl"/);
  // 描述 + 达成目标（goal）。
  assert.match(html, /class="team-item-desc"[^>]*>改 team-board-view\.js 的渲染口径</);
  assert.match(html, /<span class="team-label">达成目标<\/span>看板能看见里程碑下的工作项</);
  // 执行的 teammate。
  assert.match(html, /<span class="chip team-role">exec<\/span>/);
  // work_items[].depends_on。
  assert.match(html, /<span class="team-label">依赖<\/span><span class="chip team-dep">wi-req<\/span>/);
  // 「前置没验收」显形。
  assert.match(html, /被依赖卡住/);
  // 没有里程碑 → 不留空壳。
  assert.equal(renderTeamGantt({ team_id: "t" }), "");
});

test("① renderTeamWorkItem 一个工作项就是一行：名称/描述/达成目标/teammate/依赖/状态", () => {
  const [entry] = orderWorkItems(itemsOfMilestone(MS_PLAN, "m-build")).filter(item => item.id === "wi-impl");
  const html = renderTeamWorkItem(entry);
  assert.match(html, /<li class="team-item is-running" data-item-id="wi-impl" data-status="running"/);
  assert.match(html, /team-role">exec</);
  assert.match(html, /team-dep">wi-req</);
  assert.match(html, /team-item-desc/);
  assert.match(html, /team-item-goal/);
  // 可重派 / 现场在都要显形（这是一个在跑但被中断的工作项）。
  assert.match(html, /可重派/);
  assert.match(html, /现场在/);
});

// ── ② 里程碑之间的屏障（甘特只表示依赖）──────────────────────────

test("② 里程碑卡片显示 depends_on 屏障；甘特不表示时间", () => {
  const html = renderTeamGantt(MS_PLAN);
  assert.match(html, /<span class="team-label">屏障<\/span><span class="chip team-dep">m-build<\/span>/);
  // L0 无屏障 → —。
  assert.match(html, /<span class="team-label">屏障<\/span><span class="muted">—<\/span>/);
  // 判据（after）与屏障分开。
  assert.match(html, /<span class="team-label">判据<\/span><span class="chip team-dep">accept<\/span>/);
  // 不表示时间。
  assert.doesNotMatch(html, /started_at|finished_at|datetime|duration|时长|耗时/);
});

test("② 里程碑屏障的缺失依赖与成环都显形", () => {
  const missing = renderTeamGantt({ milestones: [{ id: "a", depends_on: ["ghost"] }] });
  assert.match(missing, /依赖缺失：ghost/);
  const cyclic = renderTeamGantt({ milestones: [{ id: "a", depends_on: ["b"] }, { id: "b", depends_on: ["a"] }] });
  assert.match(cyclic, /依赖成环/);
});

// ── ③ teammate 区块：名称 / 状态 / 工作项名称队列 ────────────────

test("③ teammate 区块画名字 / 状态 / 负责的工作项名称队列 / 尾插回执", () => {
  const html = renderTeamQueue(MS_PLAN);
  assert.match(html, /data-team-queue/);
  assert.match(html, /data-role="exec" data-status="running"/);
  assert.match(html, /<span class="chip team-queue-item">实现<\/span>/);
  assert.match(html, /改动|等待 leader 评估|跑完/);
  // 名称 = 员工会话入口（与团队面板同一对钩子，缺会话号退化为纯文本）。
  assert.match(html, /class="team-member-role is-openable" data-team-role-open="exec" data-team-role-session="s-v-model-exec"/);
  assert.equal(renderTeamQueue({ members: [{ role: "a" }] }), "", "没有工作项就不画这一节（不留空壳）");
});

// ── ④ 没有 stages 也能渲染；不得回退到阶段分组 ───────────────────

test("④ 计划里没有 stages 也能渲染：里程碑 + 工作项 + teammate 都在", () => {
  const html = renderTeamBoard({ plan: MS_PLAN, jobs: [] });
  assert.match(html, /data-team-id="v-model"/);
  assert.match(html, /data-team-gantt/);
  assert.match(html, /data-team-queue/);
  assert.match(html, /data-milestone-id="m-build"/);
  assert.match(html, /data-item-id="wi-impl"/);
  assert.doesNotMatch(html, /data-stage-id/, "看板不得再按阶段分组");
  assert.doesNotMatch(html, /team-stage/, "阶段卡样式/类名不得再出现");
});

test("④ 只有 stages、没有里程碑与工作项 → 退场（不回退到阶段分组）", () => {
  assert.equal(renderTeamBoard({ plan: STAGES_ONLY, jobs: [] }), "");
  assert.equal(renderTeamBoard(), "");
  assert.equal(renderTeamBoard({ plan: null }), "");
  assert.equal(renderTeamBoard({ plan: { team_id: "t", version: 1 } }), "");
  assert.equal(renderTeamBoard({ plan: { team_id: "t", version: 1, stages: [] } }), "");
});

test("④ 渲染件不再导出/使用阶段口径（jobsByStage / orderStages / roleOwnerStage / stagesOf）", () => {
  for (const name of ["jobsByStage", "orderStages", "roleOwnerStage", "stagesOf", "renderTeamStageCard", "stageStatuses"]) {
    assert.doesNotMatch(SRC, new RegExp(`export function ${name}\\b`), `渲染件不得再导出 ${name}`);
  }
  assert.doesNotMatch(SRC, /class="team-stages"/);
  assert.doesNotMatch(SRC, /data-stage-id/);
  assert.doesNotMatch(SRC, /\.team-stage[\s,{[]/, "CSS 里不得再留一份 .team-stage 规则");
});

test("④ 接线把 work_items 搬给渲染件，且没有 stages 也能出图", () => {
  assert.match(APP, /work_items: workItems\.map/, "app.js 必须把 DTO 的 work_items 搬给渲染件");
  assert.match(APP, /stages\.length === 0 && milestones\.length === 0 && workItems\.length === 0/, "app.js 的退场判据必须是「无阶段且无里程碑且无工作项」");
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
  assert.doesNotMatch(html, /<svg|<em>n<\/em>/);
  assert.match(html, /&lt;img src=x onerror=&quot;boom&quot;&gt;/);
  assert.match(html, /<span class="chip team-role">&lt;i&gt;r&lt;\/i&gt;<\/span>/);
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

test("TEAM_BOARD_CSS 只吃语义 token，覆盖工作项状态且不再有阶段规则", () => {
  assert.match(TEAM_BOARD_CSS, /\.team-board\s*\{/);
  for (const status of ["running", "review", "done", "failed", "pending"]) {
    assert.match(TEAM_BOARD_CSS, new RegExp(`\\.team-item\\[data-status="${status}"\\]`));
  }
  assert.match(TEAM_BOARD_CSS, /\.team-item-name\.is-openable/);
  assert.match(TEAM_BOARD_CSS, /var\(--status-running\)/);
  assert.doesNotMatch(TEAM_BOARD_CSS, /#[0-9a-fA-F]{6}/);
  assert.doesNotMatch(TEAM_BOARD_CSS, /\.team-stage[\s,{[]/, "阶段口径已退场，CSS 里不得再有 .team-stage*");
  assert.doesNotMatch(TEAM_BOARD_CSS, /\.team-job[\s,{[]/);
});
