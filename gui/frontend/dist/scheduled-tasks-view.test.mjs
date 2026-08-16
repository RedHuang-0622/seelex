import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const markdownSource = await readFile(new URL("./markdown.js", import.meta.url), "utf8");
const markdownURL = `data:text/javascript;base64,${Buffer.from(markdownSource).toString("base64")}`;
const componentsSource = (await readFile(new URL("./components.js", import.meta.url), "utf8"))
  .replace('"./markdown.js"', `"${markdownURL}"`);
const componentsURL = `data:text/javascript;base64,${Buffer.from(componentsSource).toString("base64")}`;
const source = (await readFile(new URL("./scheduled-tasks-view.js", import.meta.url), "utf8"))
  .replace('"./components.js"', `"${componentsURL}"`);
const { renderScheduledTasks } = await import(`data:text/javascript;base64,${Buffer.from(source).toString("base64")}`);

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
