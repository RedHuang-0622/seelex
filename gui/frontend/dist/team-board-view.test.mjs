import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import {
  TEAM_BOARD_CSS,
  TEAMMATE_LIVE_PAGE_SIZE,
  effStatus,
  formatEventTime,
  ganttModel,
  itemDepsOf,
  itemStatus,
  itemsOfMilestone,
  memberCurrentSessionOf,
  memberMessagesOf,
  memberOf,
  memberQueueOf,
  memberStatusOf,
  memberWorkOf,
  milestoneDepsOf,
  milestoneEff,
  milestoneStatus,
  milestonesOf,
  orderMilestones,
  orderWorkItems,
  parseTeamPageRef,
  renderMilestoneDetail,
  renderTeamAudit,
  renderTeamBoard,
  renderTeamGantt,
  renderTeammateDetail,
  renderTeammateLiveSession,
  renderTeamPage,
  renderTeamQueue,
  renderWorkItemDetail,
  roleSlotOf,
  summarizeTeam,
  teamPageRef,
  teamPageTitle,
  teammateSessionEntry,
  workItemsOf,
} from "./team-board-view.js";

const SRC = readFileSync(new URL("./team-board-view.js", import.meta.url), "utf8");
const APP = readFileSync(new URL("./app.js", import.meta.url), "utf8");
const INDEX = readFileSync(new URL("./index.html", import.meta.url), "utf8");
const STYLES = readFileSync(new URL("./styles.css", import.meta.url), "utf8");

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

// 夹具四：空里程碑（名下没有工作项）——它不画空条，画一枚零宽菱形，落在屏障前驱的右端那一格。
const MS_EMPTY = {
  team_id: "empty-ms",
  milestones: [{ id: "m-a", name: "有活的一块" }, { id: "m-b", name: "还没排活的一块", depends_on: ["m-a"] }],
  work_items: [{ id: "a1", milestone: "m-a", role: "r", name: "甲", status: "done" }],
};

// panelsOf 只取详情页**面板**那一段（返回键之外的全部内容）。用它证明"Work Item 页被两种父页
// 复用的是同一个渲染件"：从里程碑进来与从 teammate 进来，面板逐字节相同。
function panelsOf(html) {
  const at = html.indexOf('<div class="team-page-panels">');
  assert.ok(at > 0, "详情页必须有面板容器");
  return html.slice(at);
}

// tilesOf 取出详情页里的页签 key（顺序 = 渲染顺序）。
function tilesOf(html) {
  return [...html.matchAll(/data-team-tab="([^"]+)"/g)].map(match => match[1]);
}

// styleAttrs 取出渲染结果里所有 style="…"（用于钉"渲染件不写像素"）。
function styleAttrs(html) {
  return [...html.matchAll(/style="([^"]*)"/g)].map(match => match[1]);
}

// ── 纯函数：里程碑 / 工作项 ──────────────────────────────────────

test("milestonesOf / workItemsOf / itemStatus 只搬事实（空状态 = pending）", () => {
  assert.equal(milestonesOf(MS_PLAN).length, 2);
  assert.equal(workItemsOf(MS_PLAN).length, 4);
  assert.equal(itemsOfMilestone(MS_PLAN, "m-build").length, 3);
  assert.equal(itemStatus({}), "pending");
  assert.equal(itemStatus({ status: "RUNNING" }), "running");
  assert.equal(milestoneStatus({}), "pending");
  assert.equal(milestoneStatus({ status: "ACTIVE" }), "active");
  assert.deepEqual(milestoneDepsOf({ depends_on: [" m1 ", "", "m2"] }), ["m1", "m2"]);
  assert.deepEqual(itemDepsOf({ depends_on: [" a ", null] }), ["a"]);
});

test("(c) effStatus 是**工作项**线框色的唯一判据：四色映射 + interrupted 抬红 + killed 同一条红", () => {
  assert.equal(effStatus({ status: "running" }), "running");
  assert.equal(effStatus({ status: "done" }), "done");
  assert.equal(effStatus({ status: "review" }), "review");
  assert.equal(effStatus({ status: "pending" }), "pending");
  assert.equal(effStatus({ status: "failed" }), "failed");
  assert.equal(effStatus({ status: "killed" }), "failed");
  assert.equal(effStatus({ status: "running", interrupted: true }), "failed", "中断先抬红");
  assert.equal(effStatus({}), "pending");
});

test("(c2) milestoneEff 是**里程碑**线框色的唯一判据：active 不能落成 pending", () => {
  // 里程碑的状态字面量只有 pending / active / done（sessionstore 的白名单）——比工作项少一个
  // review、也没有 interrupted。复用 effStatus 会把 active 读成"还没开始"，所以两者分开。
  assert.equal(milestoneEff({ status: "active" }), "running");
  assert.equal(milestoneEff({ status: "done" }), "done");
  assert.equal(milestoneEff({ status: "pending" }), "pending");
  assert.equal(milestoneEff({}), "pending");
  assert.equal(milestoneEff({ status: "failed" }), "failed");
});

test("orderMilestones 按屏障依赖排层号；缺依赖与成环都显形（不静默丢里程碑）", () => {
  const ordered = orderMilestones({
    milestones: [
      { id: "m3", depends_on: ["m2"] },
      { id: "m2", depends_on: ["m1"] },
      { id: "m1" },
      { id: "m-missing", depends_on: ["nope"] },
      { id: "m-loop-a", depends_on: ["m-loop-b"] },
      { id: "m-loop-b", depends_on: ["m-loop-a"] },
    ],
  });
  // 指向不存在的里程碑那条边**被忽略**（它在计划里画不出来）：m-missing 因此与 m1 同层，
  // 按声明序排在同一批里 —— 缺依赖只在这里显形成 missing_deps，不改排序。
  assert.deepEqual(ordered.filter(item => !item.cyclic).map(item => item.id), ["m1", "m-missing", "m2", "m3"]);
  assert.deepEqual(ordered.map(item => item.depth), [0, 0, 1, 2, 0, 0]);
  assert.deepEqual(ordered.find(item => item.id === "m-missing").missing_deps, ["nope"]);
  assert.equal(ordered.filter(item => item.cyclic).length, 2, "成环的两个接在末尾（层号归 0），不静默丢");
});

test("orderWorkItems 只在里程碑内排 DAG，并把「前置没验收」标成被卡住", () => {
  const ordered = orderWorkItems(itemsOfMilestone(MS_PLAN, "m-build"), new Map(workItemsOf(MS_PLAN).map(item => [item.id, item])));
  assert.deepEqual(ordered.map(item => item.id), ["wi-req", "wi-impl", "wi-test"]);
  assert.equal(ordered.find(item => item.id === "wi-impl").blocked, false, "wi-req 已 done：不被卡住");
  assert.equal(ordered.find(item => item.id === "wi-test").blocked, true, "wi-impl 还在跑：被卡住");
});

test("memberQueueOf / memberStatusOf / memberMessagesOf / memberWorkOf：队列 = 没完成的工作项（销项即出队）", () => {
  assert.deepEqual(memberQueueOf(MS_PLAN, "exec"), ["实现"]);
  assert.deepEqual(memberQueueOf(MS_PLAN, "pm"), []);
  assert.equal(memberStatusOf(MS_PLAN, "exec"), "running");
  assert.equal(memberStatusOf(MS_PLAN, "pm"), "free", "idle 不是第二个词：收敛到 free");
  assert.equal(memberStatusOf(MS_PLAN, "nobody"), "free");
  assert.equal(memberMessagesOf(MS_PLAN, "exec").length, 1);
  assert.deepEqual(memberMessagesOf(MS_PLAN, "pm"), []);
  // memberWorkOf 是**全部**名下工作项（含已销项）：侧边栏报的是"负责几件事、做完几件"。
  assert.deepEqual(memberWorkOf(MS_PLAN, "exec").map(item => item.id), ["wi-impl", "wi-ship"]);
  assert.deepEqual(memberWorkOf(MS_PLAN, "pm").map(item => item.id), ["wi-req"]);
  assert.equal(memberOf(MS_PLAN, "exec").worktree, "seelex/exec");
  assert.equal(memberOf(MS_PLAN, "nobody"), null);
});

test("summarizeTeam 只报事实计数（里程碑 / 工作项 / 在编 / 作业；不报阶段）", () => {
  const summary = summarizeTeam(MS_PLAN, [
    { handle: "a1", state: "running" },
    { handle: "a2", state: "done" },
    { handle: "a3", state: "failed" },
    { handle: "a4", state: "killed" },
    { handle: "a5" },
  ]);
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
    jobs_failed: 2,
    jobs_done: 1,
  });
  assert.equal(Object.keys(summary).some(key => key.includes("stage")), false, "阶段口径不得回流");
});

// ── 侧边栏 ①：里程碑格栅（只画里程碑；WI 编号 / 内容摘要 / 一格一层）────────────

test("① 格栅：一块里程碑 = 抬头一行 + 名下工作项**各占一行**（没有边、没有箭头）", () => {
  const html = renderTeamGantt(MS_PLAN);
  assert.equal((html.match(/<section class="team-dag-ms"/g) || []).length, 2, "两个里程碑两块");
  assert.equal((html.match(/class="team-dag-wi-label"/g) || []).length, 4, "四件工作项 = 四行");
  assert.equal((html.match(/class="team-dag-cell/g) || []).length, 4, "一行一件事（4 件工作项 = 4 格）");
  assert.doesNotMatch(html, /team-dag-frame|team-dag-bar|team-dag-gate/, "大框 / 条 / 闸门带都随旧结构退场");
  assert.doesNotMatch(html, /team-dag-edge|-arrow/, "不画箭头：依赖不在几何里表达");
  assert.match(renderTeamGantt(SHUFFLED), /data-milestone-id="m1"/);
  assert.equal(renderTeamGantt({ milestones: [] }), "");
});

test("① 一行一件事：一行恒有且只有一个格子，宽度恒等于一层（不切、不叠、不挤）", () => {
  // 口径校正（2026-10-08）：上一版把同槽并行的几件**横切成 n 小格**（--n / --k），一件工作项
  // 在屏幕上只剩半格宽——"两件事"与"一件事被切开"就分不出来了。工作项在工作项这一级是不可
  // 再分的单位：一件一行、一行一格。
  const html = renderTeamGantt(MS_PLAN);
  const rows = html.split('<div class="team-dag-wi-plot').slice(1);
  assert.equal(rows.length, 4, "每一件工作项一行");
  for (const row of rows) {
    assert.equal((row.split("</div>")[0].match(/class="team-dag-cell/g) || []).length, 1, "一行里只有一个格子");
  }
  assert.doesNotMatch(html, /--n:|--k:|data-same-slot/, "槽内等分那一套已退场");
  assert.doesNotMatch(TEAM_BOARD_CSS, /var\(--n,|var\(--k,/, "CSS 里也不再有 --n / --k");
  assert.doesNotMatch(html, /team-dag-ms-span/, "无名色带（汇总条）退场：跨度改用文字说");
  assert.doesNotMatch(TEAM_BOARD_CSS, /\.team-dag-ms-span\s*\{/, "它也没有留下任何规则");
});

test("① 里程碑行读得到 id / 名字 / 状态 / 进度 / 跨度 / 内容摘要", () => {
  const html = renderTeamGantt(MS_PLAN);
  assert.match(html, /data-milestone-id="m-build"[^>]*data-status="active"[^>]*data-eff="running"[^>]*data-locked="false"[^>]*data-layer="0"[^>]*data-sum-start="0"[^>]*data-sum-end="3"[^>]*data-empty="false"[^>]*data-items="3"/);
  assert.match(html, /data-milestone-id="m-ship"[^>]*data-status="pending"[^>]*data-eff="pending"[^>]*data-locked="true"/);
  assert.match(html, /data-team-ms-open="m-build"[^>]*>m-build · 构建</);
  assert.match(html, /data-team-ms-open="m-ship"[^>]*>m-ship · 发布</);
  // 进度 = 名下工作项的 done/total（不是"这个人几件事"）。
  assert.match(html, /title="名下工作项：1 已完成 \/ 共 3">1\/3</);
  assert.match(html, /title="名下工作项：0 已完成 \/ 共 1">0\/1</);
  // 名下工作项**一件一行**（行序 = 里程碑内拓扑序），每行读得到编号 / 名字 / 状态 / 归属；
  // 抬头行不再有 WI 编号串（那正是"所有工作项挤在一行"的旧址）。
  const build = html.slice(html.indexOf('data-milestone-id="m-build"'), html.indexOf('data-milestone-id="m-ship"'));
  assert.deepEqual([...build.matchAll(/<button type="button" class="team-dag-wi-open is-openable" data-team-page-open="item:([^"]+)"/g)].map(m => m[1]),
    ["wi-req", "wi-impl", "wi-test"]);
  assert.match(build, /team-dag-wi-name" title="实现">实现</, "行里读得到工作项名字");
  assert.match(build, /title="wi-impl 实现 · running · @exec · 槽 1"/, "行 title 读得到名字 / 状态 / 负责人 / 槽位");
  assert.doesNotMatch(build, /team-dag-ms-wis/, "抬头行的编号串已退场");
  // 内容摘要是里程碑自己的 content（截断也留全文在 title 里）。
  assert.match(html, /title="构建通过：四件工作项全部落地">构建通过：四件工作项全部落地</);
});

test("① 屏障：没放行的里程碑带 🔒 + 等谁；没声明屏障的那一块不写「等」", () => {
  const html = renderTeamGantt(MS_PLAN);
  const build = html.slice(html.indexOf('data-milestone-id="m-build"'), html.indexOf('data-milestone-id="m-ship"'));
  const ship = html.slice(html.indexOf('data-milestone-id="m-ship"'));
  assert.match(build, /🔓 已解锁/);
  assert.doesNotMatch(build, /等 /, "没有屏障的那一块不写「等」");
  assert.match(ship, /🔒 待解锁/);
  assert.match(ship, /等 m-build/);
  assert.match(ship, /屏障：这些里程碑全 done 才放行/);
});

test("① 里程碑行序 = 屏障拓扑序（数组序倒着声明也一样）", () => {
  const html = renderTeamGantt(SHUFFLED);
  assert.ok(html.indexOf('data-milestone-id="m1"') < html.indexOf('data-milestone-id="m2"'), "m1 必须排在 m2 之前");
  const drawn = html.slice(html.indexOf('data-milestone-id="m1"'), html.indexOf('data-milestone-id="m2"'));
  assert.match(drawn, /data-layer="0"/);
  assert.match(html, /data-milestone-id="m2"[^>]*data-layer="1"/);
});

test("① 刻度尺：横轴写的是依赖槽位（不是时间），刻度 0..slots", () => {
  const html = renderTeamGantt(MS_PLAN);
  assert.match(html, /横轴 = 依赖槽位（非时间）· 1 格 = 1 层依赖 · 一行一件事/);
  const ticks = [...html.matchAll(/data-tick="(\d+)"/g)].map(match => Number(match[1]));
  assert.deepEqual(ticks, [0, 1, 2, 3], "slots = 3（m-build 名下三件，槽 0/1/2 → 末尾 3）");
  assert.match(html, /style="--team-dag-slots:3"/);
  assert.doesNotMatch(html, /工期|开始时间|结束时间|日期/, "格栅不许被读成工期：一个时间词都不出现");
});

test("② 格子的 x 只有槽位算式，宽度由 CSS 给（渲染件不写像素）", () => {
  const html = renderTeamGantt(MS_PLAN);
  const styles = styleAttrs(html);
  assert.ok(styles.length > 0);
  for (const style of styles) {
    assert.doesNotMatch(style, /px/, `渲染件不写像素（几何只有 CSS 一份）：${style}`);
    assert.doesNotMatch(style, /width|height|left|top/, `渲染件不写定位：${style}`);
  }
  // 格子的左端与宽度都是槽位变量（--i），由 CSS 乘 slot-w。
  assert.match(html, /<i class="team-dag-cell" data-eff="done" data-item="wi-req" data-slot="0" style="--i:0;--team-dag-role-color:var\(--team-dag-role-\d\)"/);
  assert.match(TEAM_BOARD_CSS, /\.team-dag-cell\s*\{[^}]*left:\s*calc\(var\(--i,\s*0\)\s*\*\s*var\(--team-dag-slot-w\)/);
  assert.match(TEAM_BOARD_CSS, /\.team-dag-cell\s*\{[^}]*width:\s*calc\(var\(--team-dag-slot-w\)\s*-\s*2px\)/);
});

test("(a) 槽位 = max(end(deps))（finish→start 紧贴前驱右端）；倒序声明也一样", () => {
  const model = ganttModel(SHUFFLED);
  const byID = new Map(model.rows.map(row => [row.id, row]));
  assert.equal(byID.get("a1").slot, 0);
  assert.equal(byID.get("a2").slot, 1);
  assert.equal(byID.get("b1").slot, 0);
  assert.equal(byID.get("b2").slot, 1);
  for (const row of model.rows) {
    for (const dep of row.depends_on) {
      const source = byID.get(dep);
      if (source) assert.ok(row.slot >= source.end, `${row.id} 必须紧贴在 ${dep} 右端之后`);
    }
  }
});

test("(b) 跨度 = 名下工作项的 min(slot)..max(end)：事实在 model 里，抬头行用**文字**说", () => {
  const model = ganttModel(MS_PLAN);
  const build = model.frames.find(frame => frame.id === "m-build");
  assert.deepEqual(build.sum, { s: 0, e: 3, empty: false });
  assert.equal(build.total, 3);
  assert.equal(build.done, 1);
  const ship = model.frames.find(frame => frame.id === "m-ship");
  assert.deepEqual(ship.sum, { s: 0, e: 1, empty: false });
  const html = renderTeamGantt(MS_PLAN);
  assert.match(html, /class="team-dag-ms-broad"[^>]*>跨槽 0–3</, "跨度写在抬头行里");
  assert.doesNotMatch(html, /team-dag-ms-span/, "不再用一条无名色带去说它");
});

test("(c) 空里程碑：只写「尚未排活」，不画任何几何", () => {
  const model = ganttModel(MS_EMPTY);
  const empty = model.frames.find(frame => frame.id === "m-b");
  assert.deepEqual(empty.sum, { s: 1, e: 1, empty: true }, "事实照旧算得出（接在屏障前驱的右端）");
  const html = renderTeamGantt(MS_EMPTY);
  assert.match(html, /data-milestone-id="m-b"[^>]*data-empty="true"/);
  assert.match(html, /尚未排活/);
  assert.doesNotMatch(html, /team-dag-ms-span/, "空里程碑也不画零宽菱形");
});

test("(d) 两条颜色通道正交：格子描边只吃状态令牌，teammate 色只走格子填充 + 色点", () => {
  const html = renderTeamGantt(MS_PLAN);
  const cell = html.match(/<i class="team-dag-cell[^"]*" data-eff="([^"]+)" data-item="([^"]+)"[^>]*style="([^"]*)"/);
  assert.ok(cell);
  assert.doesNotMatch(cell[3], /status-/, "填充只写归属色变量，状态色由 data-eff 交给 CSS");
  assert.match(TEAM_BOARD_CSS, /\.team-board \[data-eff="running"\]\s*\{\s*--row-line:\s*var\(--status-running\)/);
  assert.match(TEAM_BOARD_CSS, /\.team-dag-cell\s*\{[^}]*background:\s*color-mix\(in srgb,\s*var\(--team-dag-role-color/);
  // 五状态各一条（描边只引既有令牌）；pending 另加虚线。
  assert.match(TEAM_BOARD_CSS, /\.team-board \[data-eff="done"\]\s*\{\s*--row-line:\s*var\(--status-done\)/);
  assert.match(TEAM_BOARD_CSS, /\.team-board \[data-eff="review"\]\s*\{\s*--row-line:\s*var\(--status-info\)/);
  assert.match(TEAM_BOARD_CSS, /\.team-board \[data-eff="failed"\]\s*\{\s*--row-line:\s*var\(--status-failed\)/);
  assert.match(TEAM_BOARD_CSS, /\.team-board \[data-eff="pending"\]\s*\{\s*--row-line:\s*var\(--status-idle\)/);
  assert.match(TEAM_BOARD_CSS, /\.team-dag-cell\[data-eff="pending"\],\s*\.team-wi\[data-eff="pending"\]\s*\{\s*border-style:\s*dashed/);
});

test("(e) 同一位 teammate 的颜色跨格一致、跨重渲染不变；不同位不同色", () => {
  const plan = {
    milestones: [{ id: "m", name: "M" }],
    work_items: [
      { id: "x1", milestone: "m", role: "colour-alpha", name: "甲" },
      { id: "x2", milestone: "m", role: "colour-beta", name: "乙" },
      { id: "x3", milestone: "m", role: "colour-alpha", name: "丙" },
    ],
  };
  const first = renderTeamGantt(plan);
  const second = renderTeamGantt(plan);
  assert.equal(first, second, "同一份输入连渲两次逐字节相同");
  const skin = id => first.match(new RegExp(`data-item="${id}"[^>]*style="([^"]*)"`))[1];
  assert.equal(skin("x1"), skin("x3"), "同一位 teammate 的格色相同");
  assert.notEqual(skin("x1"), skin("x2"), "不同位不同色");
});

test("(f) role 超过 6 个 → 色板回绕，slot≥6 的格子叠斜纹第二通道（防撞色）", () => {
  // 夹具刻意混两族 role：`pm` 是本文件**最先**登记的那一位（slot 0，见上面的 ① 用例先渲染
  // MS_PLAN），后面 8 位是全新 role（必然落到色板尾部）——这样"回绕"与"未回绕"两支都被覆盖。
  // 颜色登记的次序是进程内的既有事实（口径 3），所以这里直接读 roleSlotOf 判期望，不写死数字。
  assert.ok(roleSlotOf("pm") < 6, "pm 必须是色板里靠前的一位，否则这条用例覆盖不到未回绕那一支");
  const items = [{ id: "c0", milestone: "m", role: "pm", name: "第一位" }]
    .concat(Array.from({ length: 8 }, (_, index) => ({ id: `c${index + 1}`, milestone: "m", role: `colour-wrap-${index}`, name: `第 ${index + 2} 位` })));
  const html = renderTeamGantt({ milestones: [{ id: "m", name: "M" }], work_items: items });
  const wrapped = [...html.matchAll(/team-dag-cell is-wrapped"[^>]*data-item="([^"]+)"/g)].map(match => match[1]);
  const plain = [...html.matchAll(/<i class="team-dag-cell" data-eff="[^"]*" data-item="([^"]+)"/g)].map(match => match[1]);
  const expectWrapped = items.filter(item => roleSlotOf(item.role) >= 6).map(item => item.id);
  const expectPlain = items.filter(item => roleSlotOf(item.role) < 6).map(item => item.id);
  assert.deepEqual(wrapped, expectWrapped, "叠斜纹的正好是色板回绕之后的那些");
  assert.deepEqual(plain, expectPlain, "没回绕的格子不带斜纹");
  assert.ok(expectWrapped.length > 0 && expectPlain.length > 0, "夹具必须同时覆盖回绕与未回绕");
  assert.match(TEAM_BOARD_CSS, /\.team-dag-cell\.is-wrapped\s*\{[^}]*repeating-linear-gradient/);
});

test("(f2) 同槽并行 = 两行同一列（工作项不被切开，也不会互相盖住）", () => {
  // 上一版为了"后画的盖住先画的"把一格横切成 n 小格（--n / --k）。口径校正（2026-10-08）：
  // 工作项是**行**这个轴上的单位——一件一行，同槽的两件就是两行同一列（x 相同、y 不同），
  // 既不互相遮挡，也不会把"一件事"画成半格。
  const plan = {
    milestones: [{ id: "m", name: "M" }],
    work_items: [
      { id: "z1", milestone: "m", role: "pm", name: "甲", status: "done" },
      { id: "z2", milestone: "m", role: "pm", name: "乙", status: "failed", depends_on: ["z1"] },
      { id: "z3", milestone: "m", role: "pm", name: "丙", status: "running", depends_on: ["z1"] },
    ],
  };
  const html = renderTeamGantt(plan);
  const rows = html.split('<div class="team-dag-wi-plot').slice(1);
  assert.equal(rows.length, 3, "三件 = 三行");
  assert.deepEqual(rows.map(row => Number((row.match(/data-slot="(\d)"/) || [])[1])), [0, 1, 1], "z2 / z3 同槽：两行同一列");
  for (const row of rows) {
    assert.equal((row.split("</div>")[0].match(/class="team-dag-cell/g) || []).length, 1, "一行恒一格");
  }
  const cell = id => html.match(new RegExp(`<i class="team-dag-cell" data-eff="[^"]*" data-item="${id}" data-slot="(\\d)" style="([^"]*)"`));
  assert.deepEqual(cell("z1").slice(1), ["0", "--i:0;--team-dag-role-color:var(--team-dag-role-0)"]);
  assert.deepEqual(cell("z2").slice(1), ["1", "--i:1;--team-dag-role-color:var(--team-dag-role-0)"]);
  assert.deepEqual(cell("z3").slice(1), ["1", "--i:1;--team-dag-role-color:var(--team-dag-role-0)"]);
  assert.match(html, /title="z3 丙 · running · @pm · 槽 1 · 同槽 2 件并行"/, "同槽只报在悬停里，不进几何");
  assert.doesNotMatch(html, /--k:|--n:/);
});

// ── 侧边栏 ②：teammate 条目（一行一位）+ 会话入口 ─────────────────────────

test("③ teammate 条目一行一位：名字 / 状态 / 负责几件事 / 工作区 / 权责", () => {
  const html = renderTeamQueue(MS_PLAN);
  assert.equal((html.match(/<li class="team-member"/g) || []).length, 3, "一行一位");
  assert.match(html, /data-team-member-open="exec"[^>]*data-tip="打开 exec 的详情页/);
  assert.match(html, /<span class="team-member-count" title="负责 2 件事：已完成 0 件 · 1 件在跑">2 件事<\/span>/);
  assert.match(html, /<span class="team-member-count" title="负责 1 件事：已完成 1 件">1 件事 · 1 完成<\/span>/);
  assert.match(html, /class="team-member-wt" title="seelex\/exec">seelex\/exec</);
  assert.match(html, /<span class="chip team-policy is-readonly">readonly<\/span>/);
  assert.match(html, /在编 3 位/);
  // 侧边栏不再铺开装配 / 队列明细 / 回执：那些都在详情页里。
  assert.doesNotMatch(html, /team-member-assembly|team-queue-item|team-member-message/);
  assert.equal(renderTeamQueue({ members: [] }), "");
  assert.equal(renderTeamQueue({ plan: MS_PLAN }), "", "没有在编成员 = 这一节退场");
});

test("③ teammate 会话入口只指向「这件事自己的会话」，绝不回退到员工的历史角色会话", () => {
  const html = renderTeamQueue(MS_PLAN);
  // exec 有在跑的工作项（带 session_id）→ 挂入口；pm / test_case 没有 → 不挂。
  assert.match(html, /data-team-role-open="exec" data-team-role-session="s-v-model-exec-wi-wi-impl" data-team-item="wi-impl"/);
  assert.match(html, />当前会话<\/button>/);
  assert.doesNotMatch(html, /data-team-role-open="pm"/);
  assert.doesNotMatch(html, /s-v-model-pm/, "没有自己的会话时连长期角色会话号都不写进去");
  assert.doesNotMatch(html, /s-v-model-exec"/, "入口不是长期角色会话号");
  assert.match(html, /is-openable/, "入口必须与纯文本区分（否则用户看不出这一格可以点）");
});

test("memberCurrentSessionOf：在跑 > 等验收 > 最近开过工；一个都没开过 → 空", () => {
  const plan = {
    members: [{ role: "r" }],
    work_items: [
      { id: "w-old", role: "r", status: "done", session_id: "s-old" },
      { id: "w-review", role: "r", status: "review", session_id: "s-review" },
      { id: "w-run", role: "r", status: "running", session_id: "s-run" },
    ],
  };
  assert.equal(memberCurrentSessionOf(plan, "r").session_id, "s-run");
  const noRunning = {
    members: [{ role: "r" }],
    work_items: [
      { id: "w-old", role: "r", status: "done", session_id: "s-old" },
      { id: "w-review", role: "r", status: "review", session_id: "s-review" },
    ],
  };
  assert.equal(memberCurrentSessionOf(noRunning, "r").session_id, "s-review");
  assert.equal(memberCurrentSessionOf({ members: [{ role: "r" }], work_items: [] }, "r").session_id, "");
  // 后端给了权威字段就认后端（前端只是降级兜底）。
  const provided = { members: [{ role: "r", current_session_id: "s-authority", current_work_item: "w-x" }], work_items: [] };
  assert.deepEqual(memberCurrentSessionOf(provided, "r"), { session_id: "s-authority", work_item: "w-x", name: "" });
});

test("teammateSessionEntry：teammate 只开「这件事自己的会话」，没有就不开；非 teammate 才走角色会话", () => {
  const live = teammateSessionEntry(MS_PLAN, "exec");
  assert.equal(live.kind, "live");
  assert.equal(live.session_id, "s-v-model-exec-wi-wi-impl");
  assert.equal(live.work_item, "wi-impl");
  const none = teammateSessionEntry(MS_PLAN, "pm");
  assert.equal(none.kind, "none");
  assert.match(none.reason, /pm 这一位此刻没有自己的会话/);
  assert.equal(teammateSessionEntry(MS_PLAN, "outsider").kind, "role", "不是 teammate：照旧走角色会话");
  assert.equal(teammateSessionEntry(MS_PLAN, "").kind, "none");
});

// ── 侧边栏 ③：看板整体 / 头部 / 审计 / 转义 / 痕迹 ────────────────────

test("④ 里程碑口径的计划照常出图：里程碑格栅 + teammate 条目都在（装配不在这里）", () => {
  const html = renderTeamBoard({ plan: MS_PLAN, jobs: [], events: [], maxMembers: 6 });
  assert.match(html, /data-team-board data-team-id="v-model"/);
  assert.match(html, /data-team-gantt/);
  assert.match(html, /data-team-queue/);
  assert.doesNotMatch(html, /team-member-assembly/, "装配在 teammate 详情页里");
});

test("④ 老口径（只有 stages）的计划 → 退场：阶段不再是可看的编排", () => {
  assert.equal(renderTeamBoard({ plan: STAGES_ONLY, jobs: [] }), "");
  assert.equal(renderTeamBoard({ plan: null, jobs: [] }), "");
  assert.equal(renderTeamBoard({ plan: { milestones: [], work_items: [] }, jobs: [] }), "");
  assert.equal(renderTeamBoard({}), "");
});

test("④ 渲染件没有阶段口径的残留（jobsByStage / orderStages / roleOwnerStage / stagesOf）", () => {
  for (const ghost of ["jobsByStage", "orderStages", "roleOwnerStage", "stagesOf", "team-stage"]) {
    assert.equal(SRC.includes(ghost), false, `${ghost} 必须随阶段口径一起退场`);
  }
});

test("④ 上一版真甘特的几何随「不画箭头」一起退场（不留没人调的死代码）", () => {
  for (const ghost of [
    "export function layoutTeamGanttEdges", "export function scheduleTeamGanttLayout",
    "export function renderTeamWorkItem", "export function renderWorkItemSessionPanel",
    "edgeSegs", "patchEdgeY", "observeTeamGanttContents",
  ]) {
    assert.equal(SRC.includes(ghost), false, `${ghost} 必须删除（边已不在几何里表达，留着就是第二套几何）`);
  }
  assert.equal(APP.includes("layoutTeamGanttEdges"), false);
  assert.equal(APP.includes("scheduleTeamGanttLayout"), false);
  assert.equal(APP.includes("data-team-item-open"), false, "旧的工作项行入口随行一起退场");
  for (const ghost of ["team-dag-edge", "team-dag-frame", "team-dag-gate", ".team-dag-row ", ".team-dag-bar", "team-dag-grid"]) {
    assert.equal(TEAM_BOARD_CSS.includes(ghost), false, `样式里的 ${ghost} 必须退场`);
  }
});

test("看板头报工作项口径的计数，不再写「阶段」", () => {
  const html = renderTeamBoard({ plan: MS_PLAN, jobs: [{ handle: "a7", state: "running" }], maxMembers: 6 });
  assert.match(html, /工作项 1\/4 完成/);
  assert.match(html, /在编 3\/6/);
  assert.match(html, /里程碑 0\/2/);
  assert.doesNotMatch(html, /阶段/);
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

test("审计只显示最近 4 条且最新在最上面，时间取 HH:MM", () => {
  const events = Array.from({ length: 8 }, (_, index) => ({ at: `2026-10-03T13:0${index}:00`, kind: "dispatch", work_item: `wi-${index}` }));
  const html = renderTeamAudit(events);
  const shown = [...html.matchAll(/data-kind="dispatch"/g)].length;
  assert.equal(shown, 4, "侧边栏是尾巴，不是台账");
  assert.doesNotMatch(html, /wi-0</);
  assert.match(html, /wi-7/);
  assert.match(html, /<time class="team-event-at">13:07<\/time>/);
  assert.equal(formatEventTime("not-a-time"), "not-a-time");
  assert.equal(formatEventTime(""), "—");
  assert.equal(formatEventTime("2026-10-03T09:05:00Z"), "09:05");
});

test("转义所有外部文本（team_id / 里程碑名 / 工作项名 / 描述都不能注入）", () => {
  const plan = {
    team_id: '<img src=x onerror="boom">',
    version: 1,
    members: [{ role: '<i>r</i>', role_session_id: "s", worktree: '<b>w</b>' }],
    milestones: [{ id: '<b>m</b>', name: '<em>n</em>', content: '<script>c</script>' }],
    work_items: [{
      id: "wi-1", milestone: "<b>m</b>", role: '<i>r</i>', name: "<u>n</u>", description: "<script>x</script>",
      goal: "<svg/>", session_id: "s", worktree: "<b>w</b>", note: "<hr>",
    }],
  };
  const html = renderTeamBoard({ plan, jobs: [] });
  assert.doesNotMatch(html, /<img/);
  assert.doesNotMatch(html, /<script|<\/script>/);
  assert.doesNotMatch(html, /<u>n<\/u>|<em>n<\/em>/);
  assert.doesNotMatch(html, /<svg\/>/, "工作项里的 <svg/> 必须转义");
  assert.match(html, /&lt;u&gt;n&lt;\/u&gt;/, "工作项名要转义");
  assert.match(html, /&lt;em&gt;n&lt;\/em&gt;/, "里程碑名要转义");
  assert.match(html, /&lt;b&gt;m&lt;\/b&gt;/, "里程碑 id 里的标签要转义");
  assert.match(html, /&lt;img src=x onerror=&quot;boom&quot;&gt;/);
  // 详情页读的是同一份外部文本，转义一条都不能少。
  const page = renderTeamPage({ plan, ref: "milestone:<b>m</b>" });
  assert.doesNotMatch(page, /<script|<\/script>|<img/);
  const item = renderTeamPage({ plan, ref: "item:wi-1" });
  assert.doesNotMatch(item, /<svg\/>|<u>n<\/u>|<hr>/);
  assert.match(item, /&lt;svg\/&gt;/);
});

// ── 详情页 ①：ref 解析 / 标题 / 分发 ────────────────────────────────

test("(g) parseTeamPageRef / teamPageRef / teamPageTitle：只有三种页，认不出来就不猜", () => {
  assert.deepEqual(parseTeamPageRef("milestone:M1"), { kind: "milestone", id: "M1", ref: "milestone:M1" });
  assert.deepEqual(parseTeamPageRef("item:WI-3"), { kind: "item", id: "WI-3", ref: "item:WI-3" });
  assert.deepEqual(parseTeamPageRef("teammate:impl"), { kind: "teammate", id: "impl", ref: "teammate:impl" });
  for (const bad of ["", "item:", ":x", "stage:s1", "item", "milestone", null, undefined]) {
    assert.equal(parseTeamPageRef(bad), null, `${String(bad)} 不是一页`);
  }
  assert.equal(parseTeamPageRef("item:x:y").id, "x:y", "id 里可以有冒号（只有第一个冒号是分隔符）");
  assert.equal(teamPageRef("item", "WI-3"), "item:WI-3");
  assert.equal(teamPageRef("milestone", "M1"), "milestone:M1");
  assert.equal(teamPageTitle({ plan: MS_PLAN, ref: "milestone:m-build" }), "m-build · 构建");
  assert.equal(teamPageTitle({ plan: MS_PLAN, ref: "item:wi-impl" }), "wi-impl · 实现");
  assert.equal(teamPageTitle({ plan: MS_PLAN, ref: "teammate:exec" }), "exec");
  assert.equal(teamPageTitle({ plan: MS_PLAN, ref: "milestone:gone" }), "gone", "取不到名字就退回 id");
  assert.match(renderTeamPage({ plan: MS_PLAN, ref: "stage:s1" }), /这一页认不出来/);
  assert.match(renderTeamPage({}), /这一页认不出来/);
});

// ── 详情页 ②：里程碑 ──────────────────────────────────────────────

test("(h) 里程碑详情页：五个页签，基本信息的字段与 Work Item 表都在", () => {
  const html = renderTeamPage({ plan: MS_PLAN, ref: "milestone:m-build" });
  assert.deepEqual(tilesOf(html), ["basic", "items", "members", "deps", "events"]);
  assert.match(html, /data-team-page="milestone:m-build"/);
  assert.match(html, /<span class="team-page-kind">里程碑<\/span>/);
  assert.match(html, /data-team-tab="items"[^>]*><span class="team-page-tab-label">Work Item<\/span><span class="team-page-tab-count">3<\/span>/);
  assert.match(html, /data-team-tab="members"[^>]*><span class="team-page-tab-label">成员<\/span><span class="team-page-tab-count">3<\/span>/);
  for (const key of ["里程碑名称", "状态", "计划进度", "依赖槽位", "屏障层号", "屏障", "判据", "工作项编号", "内容"]) {
    assert.match(html, new RegExp(`team-kv-key">${key}<`), `基本信息要读得到「${key}」`);
  }
  assert.match(html, /1\/3 已完成/);
  assert.match(html, /槽 0–3（1 格 = 1 层依赖；\*\*不是工期\*\*）/, "槽位口径要在详情页里写明");
  assert.match(html, /L0/);
  assert.match(html, /wi-req wi-impl wi-test/, "工作项编号串按拓扑序");
  // Work Item 表：ID / 名称 / 负责人 / 依赖 / 状态，ID 与名称都是下钻入口。
  assert.match(html, /<th>ID<\/th><th>名称<\/th><th>负责人<\/th><th>依赖<\/th><th>状态<\/th>/);
  assert.match(html, /data-team-page-open="item:wi-impl"[^>]*>wi-impl</);
  assert.match(html, /data-team-page-open="teammate:exec"[^>]*>exec</);
});

test("(i) 里程碑详情页：依赖页签逐条列全（含状态与反向边），不画箭头", () => {
  const build = renderTeamPage({ plan: MS_PLAN, ref: "milestone:m-build" });
  assert.match(build, /无屏障：这一块不挡在任何里程碑后面/);
  assert.match(build, /data-team-page="milestone:m-build"/);
  assert.match(build, /被依赖（谁在等它）[\s\S]*?m-ship · pending/, "反向依赖带状态");
  assert.match(build, /wi-req → wi-impl/, "里程碑内的工作项依赖写成一行文字（不画成折线）");
  assert.match(build, /wi-impl → wi-test/);
  assert.doesNotMatch(build, /team-dag-edge|-arrow/);
  const ship = renderTeamPage({ plan: MS_PLAN, ref: "milestone:m-ship" });
  assert.match(ship, /屏障（这些里程碑全 done 才放行）[\s\S]*?m-build · active/, "屏障也带那头的状态");
  assert.match(ship, /判据 after[\s\S]*?accept/, "判据 after 单独一栏");
  assert.match(ship, /还没有与这块里程碑相关的审计行/, "空动态不画空壳");
});

test("(j) 里程碑详情页：动态只收与这一块相关的审计行", () => {
  const events = [
    { at: "2026-10-03T09:00:00", kind: "plan", detail: "全量" },
    { at: "2026-10-03T09:10:00", kind: "milestone", milestone: "m-build", detail: "构建开工" },
    { at: "2026-10-03T09:20:00", kind: "dispatch", work_item: "wi-impl", detail: "派活" },
    { at: "2026-10-03T09:30:00", kind: "dispatch", work_item: "wi-ship", detail: "另一块" },
  ];
  const html = renderTeamPage({ plan: MS_PLAN, ref: "milestone:m-build", events });
  assert.match(html, /构建开工/);
  assert.match(html, /派活/);
  assert.doesNotMatch(html, /另一块/);
  assert.doesNotMatch(html, /全量/, "整份计划的流水（plan，没有里程碑归属）不属于这一页：动态只收这一页自己的行");
});

// ── 详情页 ③：Work Item（被两种父页复用的那一页）─────────────────────

test("(k) Work Item 详情页：五个页签，字段带下钻链接与两种标记", () => {
  const html = renderTeamPage({ plan: MS_PLAN, ref: "item:wi-impl" });
  assert.deepEqual(tilesOf(html), ["basic", "deps", "session", "jobs", "events"]);
  assert.match(html, /data-team-page="item:wi-impl"/);
  assert.match(html, /<span class="team-page-kind">Work Item<\/span>/);
  assert.match(html, /可重派/, "interrupted 要显形（可重派）");
  assert.match(html, /现场在/, "live 要显形");
  const basic = html.slice(html.indexOf('data-team-panel="basic"'), html.indexOf('data-team-panel="deps"'));
  assert.match(basic, /data-team-page-open="milestone:m-build"/);
  assert.match(basic, /data-team-page-open="teammate:exec"/);
  assert.match(basic, /槽 1（1 格 = 1 层依赖；\*\*不是工期\*\*）/);
  assert.match(basic, /s-v-model-exec-wi-wi-impl/);
  assert.match(basic, /看板能看见里程碑下的工作项/, "达成目标");
  assert.match(basic, /改 team-board-view.js 的渲染口径/, "描述");
  assert.match(basic, /实现：跑完，等 leader 评估/, "结论");
});

test("(l) Work Item 详情页：依赖页签把三条判据分开写（缺失 / 跨里程碑 / 被卡住）", () => {
  const plan = {
    milestones: [{ id: "m1" }, { id: "m2", depends_on: ["m1"] }],
    work_items: [
      { id: "a", milestone: "m1", status: "running" },
      { id: "b", milestone: "m2", status: "pending", depends_on: ["a"], name: "跨里程碑" },
      { id: "c", milestone: "m2", status: "pending", depends_on: ["nope"], name: "写错 id" },
      { id: "d", milestone: "m2", status: "pending", name: "被等的人" },
      { id: "e", milestone: "m2", status: "pending", depends_on: ["d"], name: "等人的人" },
    ],
  };
  const cross = renderTeamPage({ plan, ref: "item:b" });
  assert.match(cross, /a · running · 跨里程碑/, "跨里程碑是「存在但在别的块里」，不是「缺失」");
  assert.doesNotMatch(cross, /依赖缺失/);
  assert.match(cross, /跨里程碑依赖：a（编排口径只允许同里程碑内/, "两句话都摆出来");
  assert.match(cross, /前置还没验收通过/, "被卡住也要说");
  const missing = renderTeamPage({ plan, ref: "item:c" });
  assert.match(missing, /依赖缺失：nope/);
  assert.doesNotMatch(missing, /跨里程碑依赖：/);
  const waited = renderTeamPage({ plan, ref: "item:d" });
  assert.match(waited, /被依赖（谁在等它）[\s\S]*?e · pending/, "反向依赖读得到");
  assert.match(waited, /无依赖：可以立刻派/);
});

test("(m) Work Item 详情页：执行会话页签给三种诚实读数（读到 / 读取中 / 没有自己的会话）", () => {
  const none = renderTeamPage({ plan: MS_PLAN, ref: "item:wi-test" });
  assert.match(none, /这件事还没有自己的会话（未派发 \/ 已销项）/);
  const loading = renderTeamPage({ plan: MS_PLAN, ref: "item:wi-impl", tab: "session", liveLoading: true });
  assert.match(loading, /正在读这一轮的执行面/);
  const live = renderTeamPage({
    plan: MS_PLAN, ref: "item:wi-impl", tab: "session",
    live: { running: true, live: true, messages: [{ role: "user", text: "开始" }, { role: "assistant", text: "好" }] },
  });
  assert.match(live, /data-teammate-live/);
  assert.match(live, />输入</);
  assert.match(live, />好</);
});

test("(n) Work Item 详情页：作业与回执行只收属于这件事的（作业行 / 尾插回执）", () => {
  const jobs = [
    { handle: "a7", state: "running", node: "wi-impl", bytes: 2048, exit_code: 0, role: "exec" },
    { handle: "a9", state: "done", node: "wi-ship", bytes: 10, exit_code: 0 },
  ];
  const html = renderTeamPage({ plan: MS_PLAN, ref: "item:wi-impl", jobs });
  assert.match(html, /<th>句柄<\/th><th>状态<\/th><th>字节<\/th><th>退出码<\/th><th>归属<\/th>/);
  assert.match(html, /<code class="team-dag-id">a7<\/code>/);
  assert.match(html, /2048 B/);
  assert.doesNotMatch(html, />a9</, "别的节点的作业行不许串进来");
  assert.match(html, /尾插回执[\s\S]*?\[wi-impl\] 实现：跑完，等待 leader 评估/);
  const bare = renderTeamPage({ plan: MS_PLAN, ref: "item:wi-ship" });
  assert.match(bare, /wi-ship 名下没有作业行/, "没有作业行就直说，不画空表");
});

test("(o) Work Item 详情页被两种父页复用**同一个渲染件**：面板逐字节相同，只有返回键不同", () => {
  const fromMilestone = renderTeamPage({ plan: MS_PLAN, ref: "item:wi-impl", parent: { ref: "milestone:m-build", label: "m-build · 构建" } });
  const fromTeammate = renderTeamPage({ plan: MS_PLAN, ref: "item:wi-impl", parent: { ref: "teammate:exec", label: "exec" } });
  const bare = renderTeamPage({ plan: MS_PLAN, ref: "item:wi-impl" });
  assert.equal(panelsOf(fromMilestone), panelsOf(fromTeammate), "两种父页进来的是同一页");
  assert.equal(panelsOf(fromTeammate), panelsOf(bare));
  assert.match(fromMilestone, /data-team-page-back="milestone:m-build"[^>]*>← m-build · 构建</);
  assert.match(fromTeammate, /data-team-page-back="teammate:exec"[^>]*>← exec</);
  assert.doesNotMatch(bare, /data-team-page-back/, "没有上一层就不画返回键（不画一个点了没用的键）");
  assert.match(bare, /class="team-page-root"[^>]*>团队看板</);
  // 直接调三个子渲染件与走分发得到的是同一份（分层不是第二份实现）。
  assert.equal(renderWorkItemDetail({ plan: MS_PLAN, spec: { kind: "item", id: "wi-impl", ref: "item:wi-impl" } }), bare);
  assert.equal(renderMilestoneDetail({ plan: MS_PLAN, spec: { kind: "milestone", id: "m-build", ref: "milestone:m-build" } }),
    renderTeamPage({ plan: MS_PLAN, ref: "milestone:m-build" }));
  assert.equal(renderTeammateDetail({ plan: MS_PLAN, spec: { kind: "teammate", id: "exec", ref: "teammate:exec" } }),
    renderTeamPage({ plan: MS_PLAN, ref: "teammate:exec" }));
});

test("(o2) 子页上栏统一：三页共用同一套抬头 + 切换行（类种 / 标题 / chips / 返回 / 页签）", () => {
  // 口径（2026-10-08）：子页的上栏只有**一种形状**，三种页逐字同形 ——
  //   抬头两行：类种 chip + 标题（等宽 id · 名字）／状态与标记 chips；
  //   切换行一行：返回（长了截断）+ 页签（横向滚动、不换行）。
  // 统一之前三页各拼一串（teammate 页连标题元素都没有），"标题在哪、状态在哪"每换一页都要重找。
  const pages = [
    renderTeamPage({ plan: MS_PLAN, ref: "milestone:m-build", parent: { ref: "teammate:exec", label: "exec" } }),
    renderTeamPage({ plan: MS_PLAN, ref: "item:wi-impl", parent: { ref: "milestone:m-build", label: "m-build · 构建" } }),
    renderTeamPage({ plan: MS_PLAN, ref: "teammate:exec" }),
  ];
  for (const html of pages) {
    assert.equal((html.match(/class="team-page-top"/g) || []).length, 1, "上栏只有一处");
    assert.equal((html.match(/class="team-page-head-line"/g) || []).length, 1);
    assert.equal((html.match(/class="team-page-head-chips"/g) || []).length, 1);
    assert.equal((html.match(/role="tablist"/g) || []).length, 1, "切换行只有一处");
    // 顺序固定：抬头标题行 → 抬头 chips 行 → 切换行 → 页签 → 面板。
    const marks = ["team-page-head-line", "team-page-head-chips", "team-page-bar", "team-page-tabs", "team-page-panels"].map(mark => html.indexOf(mark));
    assert.deepEqual(marks.slice().sort((a, b) => a - b), marks, "上栏各段的先后顺序三页一致");
    assert.match(html, /team-page-head-chips"><span class="team-status/, "状态 chip 永远是 chips 行第一枚");
    assert.equal((html.match(/<h3 class="team-page-name">/g) || []).length, 1, "标题只有一个");
    assert.equal((html.match(/<h2>/g) || []).length, 0, "上栏不写第二份标题（弹窗头只写「团队详情」）");
  }
  // 返回键是同一枚控件（同一个类、同一个钩子），只有标签不同；没有上一层就画「团队看板」。
  assert.match(pages[0], /class="team-page-back is-openable" data-team-page-back="teammate:exec"[^>]*>← exec</);
  assert.match(pages[1], /class="team-page-back is-openable" data-team-page-back="milestone:m-build"[^>]*>← m-build · 构建</);
  assert.match(pages[2], /class="team-page-root"/);
  // 页签是同一个写法：标签一定包在 .team-page-tab-label 里，计数一定走 .team-page-tab-count。
  for (const html of pages) {
    const tabs = (html.match(/class="team-page-tab(?: is-active)?"/g) || []).length;
    assert.equal((html.match(/class="team-page-tab-label"/g) || []).length, tabs, "每一枚页签都有标签元素");
    assert.doesNotMatch(html, /data-team-tab="[^"]*"[^>]*>\s*[^<]*\(\d+\)/, "计数不再拼进标签文本");
  }
  // CSS 也只此一份：上栏钉住、页签横向滚动、返回可截断。
  assert.match(TEAM_BOARD_CSS, /\.team-page-top\s*\{[^}]*position:\s*sticky/);
  assert.match(TEAM_BOARD_CSS, /\.team-page-tabs\s*\{[^}]*overflow-x:\s*auto/);
  assert.match(TEAM_BOARD_CSS, /\.team-page-back\s*\{[^}]*text-overflow:\s*ellipsis/);
  assert.match(TEAM_BOARD_CSS, /\.team-page-tab-count\s*\{/);
  // app.js 的弹窗头只写「团队详情」，标题归页自己的抬头（之前两处都写 = 同一句话出现两遍）。
  assert.match(APP, /const title = '<span class="eyebrow">团队详情<\/span>'/);
  assert.doesNotMatch(APP, /eyebrow">团队详情<\/span><h2>/);
});

test("(p) 页签：tab 原样带回（刷新不把用户拽回第一页），认不出来的 tab 退回第一页", () => {
  const deps = renderTeamPage({ plan: MS_PLAN, ref: "item:wi-impl", tab: "deps" });
  assert.match(deps, /data-team-tab="deps"[^>]*is-active|is-active"[^>]*data-team-tab="deps"/);
  assert.match(deps, /<section class="team-page-panel is-active" data-team-panel="deps"/);
  assert.doesNotMatch(deps, /<section class="team-page-panel is-active" data-team-panel="basic"/);
  assert.equal((deps.match(/is-active/g) || []).length, 2, "只有一个页签 + 一块面板是 active");
  const weird = renderTeamPage({ plan: MS_PLAN, ref: "item:wi-impl", tab: "nope" });
  assert.match(weird, /<section class="team-page-panel is-active" data-team-panel="basic"/);
  // 面板全部渲染出来（切页签只加类，不重算）——所以每块面板的内容在任何 tab 下都在。
  for (const tab of ["basic", "deps", "session", "jobs", "events"]) {
    assert.match(deps, new RegExp(`data-team-panel="${tab}"`));
  }
});

// ── 详情页 ④：teammate（含插件装配）──────────────────────────────

test("(q) teammate 详情页：四个页签，基本信息 / 负责的 Work Item 表 / 装配 / 动态", () => {
  const html = renderTeamPage({ plan: MS_PLAN, ref: "teammate:exec" });
  assert.deepEqual(tilesOf(html), ["basic", "items", "assembly", "events"]);
  assert.match(html, /data-team-page="teammate:exec"/);
  assert.match(html, /<span class="team-page-kind">teammate<\/span>/);
  assert.match(html, /data-team-tab="items"[^>]*><span class="team-page-tab-label">负责的 Work Item<\/span><span class="team-page-tab-count">2<\/span>/);
  for (const key of ["角色", "状态", "权责", "工作区", "长期角色会话", "此刻那件事", "此刻那件事的会话", "负责"]) {
    assert.match(html, new RegExp(`team-kv-key">${key}<`), `基本信息要读得到「${key}」`);
  }
  assert.match(html, /s-v-model-exec/, "长期角色会话号照写（那是员工的历史会话，与「此刻那件事」是两码事）");
  assert.match(html, /s-v-model-exec-wi-wi-impl/, "此刻那件事的会话");
  // 负责的 Work Item 表带「所属里程碑」一列，每一行都是同一个 Work Item 详情页的入口。
  assert.match(html, /<th>ID<\/th><th>名称<\/th><th>所属里程碑<\/th><th>依赖<\/th><th>状态<\/th>/);
  assert.match(html, /data-team-page-open="item:wi-impl"/);
  assert.match(html, /data-team-page-open="item:wi-ship"/);
  assert.match(html, /data-team-page-open="milestone:m-ship"/);
  assert.match(html, /data-team-role-open="exec" data-team-role-session="s-v-model-exec-wi-wi-impl"/, "「当前会话」是同一个入口");
  const noSession = renderTeamPage({ plan: MS_PLAN, ref: "teammate:pm" });
  assert.doesNotMatch(noSession, /data-team-role-open/, "没有自己的会话就不挂入口");
  assert.match(noSession, /pm 这一位此刻没有自己的会话/);
  assert.match(renderTeamPage({ plan: MS_PLAN, ref: "teammate:nobody" }), /不在编/);
});

test("(r) teammate 详情页：装配页签 = 同一个 renderMemberAssembly + teammate 级口径", () => {
  const plan = {
    members: [{
      role: "exec", status: "running", tools_policy: "readwrite", worktree: "seelex/exec",
      plugins: ["impeccable"],
      assembly: { mode: "replace", plugin_count: 1, skill_count: 2, skill_catalog_runes: 900, skill_catalog_tokens_est: 300, plugin_face_tools: 4, total_tools: 12 },
    }],
    milestones: [{ id: "m" }],
    work_items: [
      { id: "wi-1", milestone: "m", role: "exec", name: "实现", status: "running" },
      { id: "wi-2", milestone: "m", role: "exec", name: "另一件", status: "pending" },
    ],
  };
  const html = renderTeamPage({ plan, ref: "teammate:exec", jobs: [{ handle: "a1", state: "running", node: "wi-1", bytes: 5, exit_code: 0 }] });
  assert.match(html, /data-assembly-mode="replace"/);
  assert.match(html, /<span class="chip team-assembly-plugin" title="impeccable">impeccable<\/span>/);
  assert.match(html, /技能 2 \/ 目录 900B · ≈300 tok \/ 工具面 4\/12/);
  assert.match(html, /teammate 级 · 对这位每个工作项会话都生效/);
  assert.match(html, /装配是 <b>teammate 级<\/b>/);
  // 名下每一件有作业行的都列出来（装配对哪些会话生效 = 这一串工作项）。
  assert.match(html, /wi-1 · 实现[\s\S]*?<code class="team-dag-id">a1<\/code>/);
  assert.doesNotMatch(html, /wi-2 · 另一件/, "没有作业行的工作项不画空表");
});

// ── 样式与接线 ───────────────────────────────────────────────────

test("(s) TEAM_BOARD_CSS：定值滚动上限 + 刻度尺 sticky + 窄栏容器查询 + 详情页卡片", () => {
  assert.match(TEAM_BOARD_CSS, /--team-dag-scroll-max-h:\s*380px/);
  assert.match(TEAM_BOARD_CSS, /\.team-dag-scroll\s*\{[^}]*max-height:\s*var\(--team-dag-scroll-max-h,\s*380px\)/);
  assert.match(TEAM_BOARD_CSS, /\.team-dag-ruler\s*\{[^}]*position:\s*sticky/);
  assert.match(TEAM_BOARD_CSS, /\.team-board\s*\{[^}]*container-type:\s*inline-size/);
  assert.match(TEAM_BOARD_CSS, /@container \(max-width:\s*520px\)/);
  assert.match(TEAM_BOARD_CSS, /@container \(max-width:\s*520px\)\s*\{[\s\S]*?--team-dag-slot-w:\s*38px/);
  assert.match(TEAM_BOARD_CSS, /\.team-page-card\s*\{[^}]*max-height:\s*min\(82vh,\s*760px\)/);
  assert.match(TEAM_BOARD_CSS, /\.team-page-panel\s*\{\s*display:\s*none/);
  assert.match(TEAM_BOARD_CSS, /\.team-page-panel\.is-active\s*\{\s*display:\s*block/);
  const maxHeightLine = TEAM_BOARD_CSS.split("\n").filter(line => /--team-dag-scroll-max-h/.test(line));
  assert.ok(maxHeightLine.length > 0);
  for (const line of maxHeightLine) assert.doesNotMatch(line, /vh/, "滚动上限是定值 px，不跟 vh");
});

test("(t) 几何只有一份：CSS 定义 --team-dag-*，渲染件只读变量名、不写第二份数字", () => {
  const code = SRC.slice(0, SRC.indexOf("export const TEAM_BOARD_CSS"));
  for (const name of ["--team-dag-slot-w", "--team-dag-label-w", "--team-dag-bar-h", "--team-dag-row-h", "--team-dag-ruler-h"]) {
    assert.match(TEAM_BOARD_CSS, new RegExp(`${name}:\\s*\\d+px`), `CSS 必须定义 ${name}`);
    assert.equal(code.includes(`${name}:`), false, `渲染件不得复写 ${name}（第二份数字 = 改一处漏一处）`);
  }
  assert.equal(code.includes("X_VARS"), false, "上一版的 x 系数表随边一起退场");
  assert.equal(code.includes("calc("), false, "渲染件里不再有任何几何算式（左端只有槽位变量）");
});

test("(u) TEAM_BOARD_CSS 只吃仓库既有令牌：自造色值只许出现在作用域内的归属色 / 里程碑色板里", () => {
  const offenders = TEAM_BOARD_CSS.split("\n").filter(line => /#[0-9a-fA-F]{3,8}\b/.test(line)).filter(line => !/--team-dag-(role|ms-tone)-\d:/.test(line));
  assert.deepEqual(offenders, [], "自造 hex 只许出现在 --team-dag-role-* / --team-dag-ms-tone-* 两族里");
  assert.match(TEAM_BOARD_CSS, /\[data-theme="dark"\] \.team-board, \[data-theme="dark"\] \.team-page/);
});

test("(v) styles.css 里没有第二份看板 CSS（唯一来源是 TEAM_BOARD_CSS）", () => {
  for (const token of ["team-dag-", "team-board-head", "team-member-assembly", "team-page-card", "team-stage {"]) {
    assert.equal(STYLES.includes(token), false, `styles.css 不得出现 ${token}（两份口径 = 改一处漏一处）`);
  }
  assert.match(TEAM_BOARD_CSS, /\.team-page-card\s*\{/);
  assert.match(TEAM_BOARD_CSS, /\.team-dag-cell\s*\{/);
});

test("(v2) TEAM_BOARD_CSS 自身结构完整：模板没有被反引号提前截断、花括号配平", () => {
  // 现场（2026-10-08）：CSS 注释里写 `` `b→a` `` 这种行内代码，第一个反引号就把模板字符串
  // 截断了——`node --check` 因为后面那个反引号又开了一个新模板而**照样通过**，只有浏览器
  // 加载时才炸（Unexpected identifier 'b'）。所以这条守卫看的是**值本身的结构**：
  // 被截断的值不会以 `}` 收尾，花括号也不配平。
  const css = TEAM_BOARD_CSS.trim();
  assert.ok(css.startsWith("/*"), "CSS 必须以注释开头（被截断的值不会）");
  assert.ok(css.endsWith("}"), "CSS 必须以规则收尾：模板被提前截断时结尾是一段注释或半句中文");
  const opens = (css.match(/\{/g) || []).length;
  const closes = (css.match(/\}/g) || []).length;
  assert.equal(opens, closes, `花括号必须配平（{ ${opens} 个 / } ${closes} 个）`);
  assert.equal(css.includes("*/"), true);
});

test("(w) 接线：三个入口都有钩子，且 app.js 真的挂上了监听", () => {
  // 渲染件写钩子。
  assert.match(SRC, /data-team-ms-open=/);
  assert.match(SRC, /data-team-member-open=/);
  assert.match(SRC, /data-team-page-open=/);
  assert.match(SRC, /data-team-tab=/);
  assert.match(SRC, /data-team-page-back=/);
  assert.match(SRC, /is-openable/);
  // app.js 绑监听（写在渲染件上却不绑 = 点不动的按钮，比没有入口更坏）。
  for (const hook of ["data-team-ms-open", "data-team-member-open", "data-team-page-open", "data-team-tab", "data-team-page-back", "data-team-role-open"]) {
    assert.ok(APP.includes(hook), `app.js 必须处理 ${hook}`);
  }
  assert.match(APP, /elements\["team-board-view"\]/);
  assert.match(APP, /bindTeamBoardActions\(/);
  assert.match(APP, /elements\["team-page-view"\]\?\.addEventListener/);
  assert.match(APP, /renderTeamPage\(/);
  assert.match(APP, /openRoleSessionDetail\(openRole\.dataset\.teamRoleOpen/, "会话入口复用同一个 openRoleSessionDetail");
  assert.match(APP, /currentTeamBoardInput = input/, "详情页读的是同一份只读投影");
  assert.match(APP, /\["team-page-modal", closeTeamPage\]/, "点遮罩要能关");
  assert.match(APP, /"team-page-modal", "team-page-close", "team-page-modal-title", "team-page-view"/, "新弹窗的 id 必须登记进 elements");
  for (const id of ["team-page-modal", "team-page-close", "team-page-modal-title", "team-page-view"]) {
    assert.ok(INDEX.includes(`id="${id}"`), `index.html 缺少详情页挂载点 ${id}`);
  }
});

test("(w2) 工作项行可点：整行（左列编号 + 右列格子）都是热区，走详情页同一条下钻钩子", () => {
  const html = renderTeamGantt(MS_PLAN);
  assert.match(html, /<button type="button" class="team-dag-wi-open is-openable" data-team-page-open="item:wi-req"/, "编号可点");
  assert.match(html, /<div class="team-dag-wi-plot is-openable" data-item="wi-req" data-slot="0" data-team-page-open="item:wi-req"/, "整行可点");
  assert.equal((html.match(/is-openable/g) || []).length, 10, "两块抬头 + 四行两处热区");
  // 「该开哪一页」只有 parseTeamPageRef 一处判据：看板行与详情页里的下钻链走同一个钩子、同一个函数。
  assert.match(SRC, /data-team-page-open="\$\{escapeHtml\(teamPageRef\("item", id\)\)\}"/);
  assert.match(APP, /\[data-team-page-open\]/);
  assert.match(APP, /parseTeamPageRef\(openItem\.dataset\.teamPageOpen\)/);
});

// ── ⑧ 装配契约（2026-10-05 冻结）：声明面 chips + 生效读数 + 黄牌 / 失灵 ──────────
//
// 装配从侧边栏那一行搬到 **teammate 详情页**的「插件装配」页签。断言一条不删：契约没变，
// 变的只是它显示在哪儿。

function assemblyPage(member, jobs = []) {
  return renderTeamPage({
    plan: { members: [{ role: "exec", ...member }], milestones: [{ id: "m" }], work_items: [{ id: "wi-1", milestone: "m", role: "exec", name: "实现", status: "running" }] },
    ref: "teammate:exec",
    jobs,
  });
}

test("⑤ replace 装配：每个插件一枚 chip + 技能/目录/token/工具面一行读数 + teammate 级文案", () => {
  const html = assemblyPage({
    plugins: ["impeccable", "board-kit"],
    assembly: {
      mode: "replace", plugin_count: 2,
      skill_count: 3, skill_catalog_runes: 1200, skill_catalog_tokens_est: 400,
      plugin_face_tools: 5, total_tools: 12,
    },
  });
  assert.match(html, /data-assembly-mode="replace"/);
  assert.match(html, /<span class="chip team-assembly-plugin" title="impeccable">impeccable<\/span>/);
  assert.match(html, /<span class="chip team-assembly-plugin" title="board-kit">board-kit<\/span>/);
  assert.match(html, /技能 3 \/ 目录 1200B · ≈400 tok \/ 工具面 5\/12/);
  // 窄栏会被省略号吃掉（审查 Hypothesis）：title 里放同一句话的全文，悬停可读全。
  assert.match(html, /title="技能 3 \/ 目录 1200B · ≈400 tok \/ 工具面 5\/12（生效读数：插件面工具数是上界/);
  assert.match(html, /teammate 级 · 对这位每个工作项会话都生效/);
});

test("⑤ inherit-host：显式写「继承宿主」，不靠字段缺失暗示，也不给读数行", () => {
  const html = assemblyPage({ plugins: [], assembly: { mode: "inherit-host" } });
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
  const html = assemblyPage({ plugins: [] });
  assert.doesNotMatch(html, /team-member-assembly/, "读数给不出：整格退场");
  assert.doesNotMatch(html, /继承宿主/, "缺失 ≠ 空集，不许自称继承宿主");
  assert.doesNotMatch(html, /装配读数缺失|按不覆盖处理/, "降级文案连同降级分支一起删掉");
  assert.doesNotMatch(html, /data-assembly-mode/, "没有读数就没有 mode 结论");
  assert.doesNotMatch(html, /技能 \d+ \/ 目录/);
  assert.match(html, /既没有声明面，也没有生效读数/, "整格退场时给一句说明（不是空面板）");
});

test("⑤ 声明非空 + assembly 为 nil → 只列声明 chips，不写任何 mode 结论（不同屏自相矛盾）", () => {
  const html = assemblyPage({ plugins: ["impeccable", "board-kit"] });
  assert.match(html, /<span class="chip team-assembly-plugin" title="impeccable">impeccable<\/span>/);
  assert.match(html, /<span class="chip team-assembly-plugin" title="board-kit">board-kit<\/span>/);
  assert.doesNotMatch(html, /继承宿主/, "声明了插件就不许再自称继承宿主（两句互斥，同屏即自相矛盾）");
  assert.doesNotMatch(html, /data-assembly-mode/, "读数缺失：不下 mode 结论");
  assert.doesNotMatch(html, /team-assembly-readout|team-assembly-inherit/);
});

test("⑤ 读数在但没写明 mode → 同样不下 mode 结论（不靠字段缺失暗示）", () => {
  const declared = assemblyPage({ plugins: ["impeccable"], assembly: {} });
  assert.match(declared, /team-assembly-plugin/);
  assert.doesNotMatch(declared, /继承宿主/);
  assert.doesNotMatch(declared, /data-assembly-mode/);
  assert.doesNotMatch(assemblyPage({ plugins: [], assembly: {} }), /team-member-assembly/);
});

test("⑤ 黄牌：显式标记且 title = yellow_reason；只报不拒，与读数/插件 chips 同屏", () => {
  const html = assemblyPage({
    plugins: ["big"],
    assembly: { mode: "replace", skill_count: 9, yellow: true, yellow_reason: "技能目录超阈值：≈8200 tok > 6k" },
  });
  assert.match(html, /<span class="chip team-assembly-yellow" title="技能目录超阈值：≈8200 tok &gt; 6k">黄牌<\/span>/);
  assert.match(html, /team-assembly-plugin/, "黄牌只报不拒：插件 chips 仍在");
  assert.match(html, /技能 9 \/ 目录 0B · ≈0 tok/, "黄牌不替换读数");
});

test("⑤ 失灵 / 已撤：显式标记，且**不得**被渲染成「没装配」（继承宿主）", () => {
  const html = assemblyPage({
    plugins: ["gone", "moved"],
    assembly: {
      mode: "replace", plugin_face_tools: 0, total_tools: 12,
      plugin_face_faulted: true, plugin_face_missing: ["gone"],
      plugin_face_note: "声明 gone / moved，现已失灵，工具面为空",
    },
  });
  assert.match(html, /data-assembly-mode="replace"/, "失灵仍是 replace：不许落进继承宿主那一支");
  assert.match(html, /<span class="chip team-assembly-faulted"[^>]*>失灵<\/span>/);
  assert.match(html, /<span class="chip team-assembly-missing"[^>]*>已撤 1<\/span>/);
  assert.match(html, /声明 gone \/ moved，现已失灵，工具面为空/);
  assert.doesNotMatch(html, /继承宿主/, "失灵与没装配语义相反，不许混渲染");
});

test("⑤ 长插件名不撑破行：chips 容器换行 + 单枚限宽截断（title 留全名）", () => {
  const long = "a-very-long-plugin-name-that-would-overflow-the-member-row-".repeat(2);
  const html = assemblyPage({ plugins: [long], assembly: { mode: "replace" } });
  assert.match(html, new RegExp(`title="${long}"`), "title 里保留全名");
  const visible = html.match(/<span class="chip team-assembly-plugin"[^>]*>([^<]*)<\/span>/)[1];
  assert.ok(visible.length < long.length, "行内可见文本要截断");
  assert.match(TEAM_BOARD_CSS, /\.team-assembly-plugin\s*\{[^}]*max-width:\s*160px/);
  assert.match(TEAM_BOARD_CSS, /\.team-assembly-plugin\s*\{[^}]*text-overflow:\s*ellipsis/);
  assert.match(TEAM_BOARD_CSS, /\.team-member-assembly\s*\{[^}]*flex-wrap:\s*wrap/);
});

test("⑤ 装配读数转义（注入 <img onerror=...> 不许出裸标签）", () => {
  const html = assemblyPage({
    plugins: ['<img src=x onerror="boom">'],
    assembly: { mode: "replace", yellow: true, yellow_reason: '<img src=x onerror="boom">' },
  });
  assert.doesNotMatch(html, /<img/);
  assert.match(html, /&lt;img src=x onerror=&quot;boom&quot;&gt;/);
});

// ── 实时会话子页面（细节：读得到就读，读不到就如实说）──────────────────

test("renderTeammateLiveSession：读得到就读这一轮的对话，读不到就如实说「不在本进程」", () => {
  const offline = renderTeammateLiveSession({ running: false, session_id: "s1" }, { role: "exec", work_item: "wi-impl" });
  assert.match(offline, /data-teammate-live="s1"/);
  assert.match(offline, /执行面不在本进程/);
  assert.match(offline, /正文不落盘/, "如实说：读不到 ≠ 会话是空的");
  assert.doesNotMatch(offline, /role-kv-row/, "读不到就不画空壳");

  const live = renderTeammateLiveSession({
    running: true, live: true, session_id: "s1", offset: 0, limit: 40, total: 2, has_more: false,
    messages: [
      { role: "user", text: "开工" },
      { role: "tool", text: "原始载荷（不画）" },
      { role: "assistant", text: "好" },
    ],
  }, { role: "exec", work_item: "wi-impl" });
  assert.match(live, />开工</);
  assert.match(live, />好</);
  assert.doesNotMatch(live, /原始载荷/, "工具行的原始载荷不画（渲染件不画它不理解的东西）");
  assert.match(live, /正在跑/);
});

test("renderTeammateLiveSession：翻页条搬后端的 offset/total/has_more，不自己推算", () => {
  const html = renderTeammateLiveSession({
    running: true, live: true, session_id: "s1",
    offset: TEAMMATE_LIVE_PAGE_SIZE, limit: TEAMMATE_LIVE_PAGE_SIZE, total: 95, has_more: true,
    messages: [{ role: "user", text: "一" }],
  });
  assert.match(html, /data-teammate-live-page="0"[^>]*>上一页</);
  assert.match(html, new RegExp(`data-teammate-live-page="${TEAMMATE_LIVE_PAGE_SIZE * 2}"[^>]*>下一页<`));
  assert.match(html, new RegExp(`第 ${TEAMMATE_LIVE_PAGE_SIZE + 1}–${TEAMMATE_LIVE_PAGE_SIZE + 1} 条 / 共 95 条`));
  // 第一页 + 没有更多：两个键都在，但都是 disabled（不画假的翻页键）。
  const first = renderTeammateLiveSession({ running: true, session_id: "s2", messages: [{ role: "user", text: "一" }], offset: 0, total: 1, has_more: false });
  assert.match(first, /data-teammate-live-page="0" disabled>上一页</);
  assert.match(first, /disabled>下一页</);
  // 读数缺失（老载荷/夹具）→ 退化成"只有这一页"，一个翻页键都不画。
  assert.doesNotMatch(renderTeammateLiveSession({ running: true, session_id: "s3", messages: [] }), /team-live-pager/);
});

// ── 幂等（口径 8）───────────────────────────────────────────────

test("(x) 幂等：同一份夹具连渲两次逐字节相同（无时间戳/随机数/自增 id）", () => {
  assert.equal(renderTeamGantt(MS_PLAN), renderTeamGantt(MS_PLAN));
  assert.equal(renderTeamGantt(SHUFFLED), renderTeamGantt(SHUFFLED));
  assert.equal(renderTeamBoard({ plan: MS_PLAN, jobs: [] }), renderTeamBoard({ plan: MS_PLAN, jobs: [] }));
  for (const ref of ["milestone:m-build", "item:wi-impl", "teammate:exec"]) {
    assert.equal(renderTeamPage({ plan: MS_PLAN, ref }), renderTeamPage({ plan: MS_PLAN, ref }), `${ref} 幂等`);
  }
});

test("(y) 恶意名字/依赖名零注入（格子、机读属性、title、详情页都不例外）", () => {
  const bad = '"><img src=x onerror="alert(1)">';
  const plan = {
    team_id: bad,
    milestones: [{ id: bad, name: bad, content: bad }],
    work_items: [{ id: "w1", milestone: bad, role: bad, name: bad, status: "pending", depends_on: [bad], session_id: bad, worktree: bad, goal: bad, description: bad, note: bad }],
  };
  const html = renderTeamGantt(plan);
  assert.doesNotMatch(html, /<img/);
  assert.match(html, /&lt;img src=x onerror=&quot;alert\(1\)&quot;&gt;/, "名字要转义（进 title 也不许出裸标签）");
  const page = renderTeamPage({ plan, ref: `item:w1` });
  assert.doesNotMatch(page, /<img/);
  const msPage = renderTeamPage({ plan, ref: `milestone:${bad}` });
  assert.doesNotMatch(msPage, /<img/);
  // raw 通道只有两处（teamLink 造出来的下钻链接）：坏角色名走同一条路，也不许出裸标签。
  const teamPage = renderTeamPage({ plan, ref: `teammate:${bad}` });
  assert.doesNotMatch(teamPage, /<img/);
  assert.doesNotMatch(teamPage, /<script/);
  assert.match(teamPage, /&lt;img src=x onerror=&quot;alert\(1\)&quot;&gt;/);
});
