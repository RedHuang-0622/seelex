import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const embedURL = `data:text/javascript;base64,${Buffer.from(await readFile(new URL("./html-embed.js", import.meta.url), "utf8")).toString("base64")}`;
const markdownSource = (await readFile(new URL("./markdown.js", import.meta.url), "utf8"))
  .replace('"./html-embed.js"', `"${embedURL}"`);
const markdownURL = `data:text/javascript;base64,${Buffer.from(markdownSource).toString("base64")}`;
const componentsSource = (await readFile(new URL("./components.js", import.meta.url), "utf8"))
  .replace('"./markdown.js"', `"${markdownURL}"`);
const componentsURL = `data:text/javascript;base64,${Buffer.from(componentsSource).toString("base64")}`;
const source = (await readFile(new URL("./scheduled-tasks-view.js", import.meta.url), "utf8"))
  .replace('"./components.js"', `"${componentsURL}"`);
const { buildScheduledTaskSpec, renderScheduledTasks, renderScheduledTasksTable } = await import(`data:text/javascript;base64,${Buffer.from(source).toString("base64")}`);

const task = (overrides = {}) => ({
  id: "sched_1",
  name: "抓职位",
  kind: "command",
  interval_seconds: 3600,
  command: "auto_get_jobs",
  enabled: true,
  running: false,
  next_run_at: "2026-08-06T10:00:00+08:00",
  last_status: "ok",
  last_result: "采集完成，共 12 条",
  log_tail: ["[10:00:00] 运行开始", "[10:03:12] 运行完成"],
  run_count: 3,
  ...overrides
});

test("renders task list with name, interval, next run and cancel button", () => {
  const html = renderScheduledTasks([task()], [{ key: "auto_get_jobs", label: "BOSS直聘自动投简历" }]);
  assert.match(html, /抓职位/);
  assert.match(html, /每 1 小时/);
  assert.match(html, /下次/);
  assert.match(html, /共 3 次/);
  assert.match(html, /BOSS直聘自动投简历/);
  assert.match(html, /data-sched-cancel="sched_1"/);
  assert.match(html, /sched-chip-on/);
  assert.match(html, /上次成功/);
  assert.match(html, /采集完成，共 12 条/);
});

test("renders period units for recurring tasks", () => {
  const html = renderScheduledTasks([
    task({ id: "sched_4", name: "每月巡检", period_unit: "month", period_value: 1, interval_seconds: 2592000 }),
    task({ id: "sched_5", name: "每周同步", period_unit: "week", period_value: 2, interval_seconds: 1209600 })
  ], []);
  assert.match(html, /每 1 月/);
  assert.match(html, /每 2 周/);
});

test("renders prompt tasks with prompt content and session binding stays out of display", () => {
  const html = renderScheduledTasks([
    task({ id: "sched_2", name: "周期提醒", kind: "prompt", prompt: "每隔一小时检查发布状态", command: "", session_id: "sess_1" })
  ], []);
  assert.match(html, /提示词/);
  assert.match(html, /每隔一小时检查发布状态/);
  assert.doesNotMatch(html, /sess_1/);
});

test("renders one-shot scheduled tasks with fixed run time", () => {
  const html = renderScheduledTasks([
    task({
      id: "sched_6",
      name: "明天发布检查",
      one_shot: true,
      run_at: "2026-08-17T09:30:00+08:00",
      interval_seconds: 0,
      enabled: false,
      run_count: 1,
      last_status: "ok"
    })
  ], []);
  assert.match(html, /一次性/);
  assert.match(html, /定时/);
  assert.match(html, /09:30/);
  assert.match(html, /已停用/);
  assert.doesNotMatch(html, /每 /);
});

test("shows running and failed states from authoritative flags", () => {
  const running = renderScheduledTasks([task({ running: true, last_status: "running" })], []);
  assert.match(running, /运行中/);
  assert.match(running, /is-running/);

  const failed = renderScheduledTasks([
    task({ id: "sched_3", name: "失败任务", last_status: "failed", last_error: "命令退出失败: exit status 3" })
  ], []);
  assert.match(failed, /上次失败/);
  assert.match(failed, /is-failed/);
  assert.match(failed, /命令退出失败/);
});

test("escapes all rendered text and tolerates malformed items", () => {
  const html = renderScheduledTasks([
    task({
      id: '"><img src=x onerror="boom">',
      name: '<script>alert(1)</script>',
      prompt: '<b onmouseover="x()">任务</b>',
      last_result: "<img src=y>"
    }),
    null,
    { id: "no-name" },
    "plain string",
    { name: "no-id" }
  ], [{ key: '"><svg onload="z()">', label: "<i>label</i>" }]);
  assert.doesNotMatch(html, /<script>/);
  assert.doesNotMatch(html, /<img src=x/);
  assert.doesNotMatch(html, /onmouseover/);
  assert.doesNotMatch(html, /<i>label<\/i>/);
  assert.match(html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
  // 畸形条目被过滤：仅剩 1 条合法任务
  const items = (html.match(/class="sched-item"/g) || []).length;
  assert.equal(items, 1);
});

test("renders empty state for empty or non-array input", () => {
  assert.match(renderScheduledTasks([], []), /暂无定时任务/);
  assert.match(renderScheduledTasks(null, null), /暂无定时任务/);
  assert.match(renderScheduledTasks(undefined, undefined), /暂无定时任务/);
  assert.match(renderScheduledTasks("nope", []), /暂无定时任务/);
});

test("renders disabled task with off chip and pending status", () => {
  const html = renderScheduledTasks([task({ enabled: false, last_status: "pending" })], []);
  assert.match(html, /sched-chip-off/);
  assert.match(html, /已停用/);
  assert.match(html, /待运行/);
});

test("renders scheduled tasks as Excel table with columns and cancel buttons", () => {
  const html = renderScheduledTasksTable([task()], [{ key: "auto_get_jobs", label: "BOSS直聘自动投简历" }]);
  assert.match(html, /excel-grid scheduled-table/);
  assert.match(html, />名称</);
  assert.match(html, />类型</);
  assert.match(html, />周期</);
  assert.match(html, />下次运行</);
  assert.match(html, />状态</);
  assert.match(html, />操作</);
  assert.match(html, /抓职位/);
  assert.match(html, /每 1 小时/);
  assert.match(html, /BOSS直聘自动投简历/);
  assert.match(html, /data-sched-cancel="sched_1"/);
  assert.match(html, /已启用/);
  assert.match(html, /上次成功/);
  assert.match(html, /data-sched-id="sched_1"/);
});

// 表格整块的名字只剩头带那一条（弹窗头不再重复标题），且表体只隔一个滚动容器：
// 弹窗纵向只此一层可滚（口径同工作表格弹窗，见 styles.css 的 .scheduled-table-card）。
test("keeps one name band and one scroll container around the scheduled table", () => {
  const html = renderScheduledTasksTable([task(), task({ id: "sched_2", name: "第二个" })], []);
  assert.match(html, /<header class="sched-table-band">\s*<strong>定时任务<\/strong>/);
  assert.match(html, /sched-table-total">2 项</);
  assert.equal((html.match(/class="sched-table-scroll"/g) || []).length, 1);
  assert.equal((html.match(/<table/g) || []).length, 1);
  // 头带在滚动容器之外：滚动时块名不跟着走。
  assert.ok(html.indexOf("sched-table-band") < html.indexOf("sched-table-scroll"));
});

test("renders empty scheduled table state for empty or non-array input", () => {
  for (const items of [[], null, undefined, "nope"]) {
    const html = renderScheduledTasksTable(items, []);
    assert.match(html, /暂无定时任务/);
    // 空表也有头带：标题只剩这一条，空态不能连名字一起丢。
    assert.match(html, /<strong>定时任务<\/strong>/);
    assert.match(html, /sched-table-total">0 项</);
  }
});

// 周期锚点在面板上的读数：给了开始时间要显示出来，没给要写明"按创建时间"——
// 用户看到的必须是他勾的那件事（2026-10-07 口径：没明说就得勾"每个周期按当前时间"）。
test("renders the period anchor next to the interval", () => {
  const html = renderScheduledTasks([
    task({ id: "sched_a", name: "每天九点", kind: "prompt", period_unit: "day", period_value: 1, interval_seconds: 86400, start_clock: "09:00" }),
    task({ id: "sched_b", name: "周一同步", kind: "prompt", period_unit: "week", period_value: 1, interval_seconds: 604800, start_clock: "09:00", start_weekday: 1 }),
    task({ id: "sched_c", name: "随创建滚动", kind: "prompt", period_unit: "day", period_value: 2, interval_seconds: 172800 })
  ], []);
  assert.match(html, /每 1 天 09:00/);
  assert.match(html, /每 1 周 周一 09:00/);
  assert.match(html, /每 2 天（按创建时间）/);
});

// ── 弹窗载荷契约（buildScheduledTaskSpec）──────────────────────────────
//
// 现场（2026-10-07）：周期模式提交时前端把 runAt 写成空串，Wails 绑定层用
// encoding/json 反序列化参数，DTO 里 runAt 是 time.Time —— 空串当场报
// "error parsing arguments: parsing time \"\" as \"2006-01-02T15:04:05Z07:00\""，
// Go 侧一行都执行不到。这里的钉子就是"周期模式 runAt 必须是 null"。

test("builds a daily prompt spec with an explicit start clock", () => {
  const { spec, error } = buildScheduledTaskSpec({
    name: " 每天九点巡检 ", prompt: " 巡检 ", mode: "period",
    periodValue: "1", periodUnit: "day", startClock: "09:00", anchorNow: false, enabled: true
  });
  assert.equal(error, undefined);
  assert.equal(spec.name, "每天九点巡检");
  assert.equal(spec.kind, "prompt");
  assert.equal(spec.prompt, "巡检");
  assert.equal(spec.periodUnit, "day");
  assert.equal(spec.periodValue, 1);
  assert.equal(spec.startClock, "09:00");
  assert.equal(spec.startWeekday, 0);
  assert.equal(spec.interval, 86400e9);
  assert.equal(spec.enabled, true);
  // 关键：周期模式下 runAt 是 null，不是 ""（空串会让 Wails 的参数反序列化失败）。
  assert.equal(spec.runAt, null);
  assert.doesNotMatch(JSON.stringify(spec), /"runAt":""/);
});

test("builds a weekly spec with the ISO weekday and a minute period", () => {
  const weekly = buildScheduledTaskSpec({
    name: "周一同步", prompt: "同步", mode: "period",
    periodValue: "1", periodUnit: "week", startClock: "09:00", startWeekday: "5", enabled: true
  }).spec;
  assert.equal(weekly.startClock, "09:00");
  assert.equal(weekly.startWeekday, 5);
  assert.equal(weekly.interval, 604800e9);

  const minutely = buildScheduledTaskSpec({
    name: "每分钟", prompt: "看一眼", mode: "period",
    periodValue: "1", periodUnit: "minute", startClock: "09:00", startWeekday: "3", enabled: true
  }).spec;
  assert.equal(minutely.periodUnit, "minute");
  assert.equal(minutely.interval, 60e9);
  // 子日周期没有"几点开始"：开始时间/星期一律不带（后端也拒收）。
  assert.equal(minutely.startClock, "");
  assert.equal(minutely.startWeekday, 0);
});

test("requires a start clock or the current-time checkbox for anchored units", () => {
  const missing = buildScheduledTaskSpec({
    name: "巡检", prompt: "巡检", mode: "period",
    periodValue: "1", periodUnit: "day", startClock: "", anchorNow: false, enabled: true
  });
  assert.match(missing.error, /勾选/);
  assert.equal(missing.spec, undefined);

  const acknowledged = buildScheduledTaskSpec({
    name: "巡检", prompt: "巡检", mode: "period",
    periodValue: "2", periodUnit: "week", startClock: "", anchorNow: true, enabled: false
  });
  assert.equal(acknowledged.error, undefined);
  assert.equal(acknowledged.spec.startClock, "");
  assert.equal(acknowledged.spec.startWeekday, 0);
  assert.equal(acknowledged.spec.enabled, false);
});

test("rejects malformed dialog input instead of sending it", () => {
  const cases = [
    [{ name: "  ", prompt: "p" }, /任务名称/],
    [{ name: "n", prompt: "  " }, /提示词内容/],
    [{ name: "n", prompt: "p", mode: "period", periodValue: "0", periodUnit: "day", anchorNow: true }, /周期数值/],
    [{ name: "n", prompt: "p", mode: "period", periodValue: "1", periodUnit: "year", anchorNow: true }, /周期单位/],
    [{ name: "n", prompt: "p", mode: "period", periodValue: "1", periodUnit: "day", startClock: "9:00" }, /HH:MM/],
    [{ name: "n", prompt: "p", mode: "at", runAtValue: "" }, /定时执行时间/],
    [{ name: "n", prompt: "p", mode: "at", runAtValue: "2026-10-07T10:00", now: Date.parse("2026-10-07T12:00:00") }, /晚于当前时间/]
  ];
  for (const [fields, pattern] of cases) {
    const built = buildScheduledTaskSpec(fields);
    assert.equal(built.spec, undefined, `不该发出载荷：${JSON.stringify(fields)}`);
    assert.match(built.error, pattern);
  }
});

test("builds a one-shot spec with an absolute RFC3339 run time", () => {
  const now = Date.parse("2026-10-07T10:00:00");
  const { spec, error } = buildScheduledTaskSpec({
    name: "明天发布检查", prompt: "检查发布", mode: "at",
    runAtValue: "2026-10-08T09:30", now, enabled: false
  });
  assert.equal(error, undefined);
  assert.equal(spec.runAt, new Date("2026-10-08T09:30").toISOString());
  assert.equal(spec.interval, 0);
  assert.equal(spec.periodUnit, "");
  // 一次性任务由后端强制启用（创建即启用，执行后自动停用）。
  assert.equal(spec.enabled, true);
});

