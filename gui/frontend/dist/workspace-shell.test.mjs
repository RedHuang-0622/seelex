import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const html = await readFile(new URL("./index.html", import.meta.url), "utf8");
const css = await readFile(new URL("./styles.css", import.meta.url), "utf8");
const glass = await readFile(new URL("./themes/glass.css", import.meta.url), "utf8");
const app = await readFile(new URL("./app.js", import.meta.url), "utf8");
const summary = await readFile(new URL("./workbench-summary.js", import.meta.url), "utf8");

test("shell removes reserved divider gutters and the content-paper radius", () => {
  const shell = css.match(/\.app-shell\s*\{([\s\S]*?)\}/)?.[1] || "";
  assert.match(
    shell,
    /grid-template-columns:\s*var\(--nav-w,\s*112px\)\s+var\(--left-w,\s*268px\)\s+minmax\(0,\s*1fr\)\s+var\(--right-w,\s*300px\)/,
    "一级导航、任务列表、会话区、右栏必须是四个独立列"
  );
  assert.doesNotMatch(shell, /6px/, "主布局不得继续预留 6px 沟槽");

  const divider = css.match(/#left-divider\s*\{([\s\S]*?)\}/)?.[1] || "";
  assert.match(divider, /grid-column:\s*2/, "任务列表分隔条应覆盖在第二列边界");

  const workspace = [...css.matchAll(/\.workspace\s*\{([\s\S]*?)\}/g)]
    .map(match => match[1])
    .find(body => /border-radius/.test(body)) || "";
  assert.match(workspace, /border-radius:\s*0/, "对话区不应再有圆角纸面");
});

test("glass material uses a left-to-right opacity ramp", () => {
  assert.match(glass, /--glass-left-alpha:\s*38%/, "左栏玻璃透明度最低");
  assert.match(glass, /--glass-center-alpha:\s*66%/, "对话区玻璃透明度居中");
  assert.match(glass, /--glass-right-alpha:\s*88%/, "右栏玻璃透明度最高");
  assert.match(glass, /\.left-panel\s*\{[\s\S]*?var\(--glass-left-alpha\)/, "左栏必须消费左档透明度");
  assert.match(glass, /\.workspace\s*\{[\s\S]*?var\(--glass-center-alpha\)/, "对话区必须消费中档透明度");
  assert.match(glass, /\.right-panel\s*\{[\s\S]*?var\(--glass-right-alpha\)/, "右栏必须消费右档透明度");
});

test("left navigation is a standalone rail beside the task list", () => {
  const nav = html.match(/<nav[^>]+id="left-nav"[\s\S]*?<\/nav>/)?.[0] || "";
  assert.ok(nav, "左侧顶部导航必须存在");
  for (const page of ["tasks", "scheduled", "plugins"]) {
    assert.match(nav, new RegExp(`data-left-page="${page}"`), `缺少 ${page} 导航项`);
  }
  assert.match(nav, />聊天</);
  assert.match(nav, />定时任务</);
  assert.match(nav, />插件管理</);
  assert.match(html, /id="toggle-left-nav"/, "主导航必须可收缩");
  assert.match(html, /<aside class="nav-panel"/, "一级导航必须独立成一列");
  assert.match(html, /<aside class="left-panel task-panel panel"/, "任务列表必须是第二列");
  const navStart = html.indexOf('<aside class="nav-panel"');
  const taskStart = html.indexOf('<aside class="left-panel task-panel panel"');
  assert.ok(navStart >= 0 && taskStart > navStart, "导航列必须在任务列表左侧");
  assert.match(html, /id="left-page-tasks"/);
  assert.match(html, /id="left-page-scheduled"/);
  assert.match(html, /id="left-page-plugins"/);
  assert.doesNotMatch(html, /id="left-page-skills"/, "Skill 不再占第四个一级页面");
  assert.match(app, /function showLeftPage\(/);
  assert.match(app, /seelex\.left-nav\.collapsed/);
});

test("left navigation stays mode-owned black or white, not skin-owned", () => {
  assert.match(css, /--left-nav-bg:\s*#ffffff/, "浅色导航必须是纯白");
  assert.match(
    css,
    /:root\[data-theme="dark"\]\s*\{[\s\S]*?--left-nav-bg:\s*#000000/,
    "深色导航必须是纯黑"
  );
  const rule = [...css.matchAll(/\.nav-panel\s*\{([\s\S]*?)\}/g)]
    .map(match => match[1])
    .find(body => /background/.test(body)) || "";
  assert.match(rule, /background:\s*var\(--left-nav-bg\)/);
  assert.doesNotMatch(rule, /--accent|--skin-|gradient/, "导航底色不得被皮肤或渐变接管");
});

test("plugins and skills leave the runtime modal for the task-list plugin page", () => {
  const runtime = html.slice(html.indexOf('id="runtime-modal"'));
  assert.doesNotMatch(runtime, /id="plugin-list"/, "运行状态不再显示插件");
  assert.doesNotMatch(runtime, /id="skill-list"/, "运行状态不再显示 Skill");
  const pluginPage = html.slice(html.indexOf('id="left-page-plugins"'));
  assert.match(pluginPage, /id="plugin-list"/);
  assert.match(pluginPage, /id="skill-list"/);
  assert.match(pluginPage, /plugin-skills-block/);
});

test("scheduled task editing lives inside the middle session workspace", () => {
  assert.doesNotMatch(html, /id="scheduled-task-modal"/, "定时任务不再使用弹窗");
  assert.doesNotMatch(html, /id="scheduled-table-modal"/, "定时任务不再使用表格弹窗");
  const workspaceStart = html.indexOf('<main class="workspace">');
  const editor = html.indexOf('id="scheduled-editor"');
  const workspaceEnd = html.indexOf('</main>', workspaceStart);
  assert.ok(editor > workspaceStart && editor < workspaceEnd, "编辑器必须在会话主区内部");
  assert.match(html, /class="scheduled-editor-body"/);
  assert.match(html, /id="scheduled-task-cancel"/);
  assert.match(app, /classList\.add\("is-task-schedule"\)/);
  assert.match(app, /classList\.remove\("is-task-schedule"\)/);
});

test("workbench renders inline and separates todo from task and subagent", () => {
  assert.doesNotMatch(html, /id="work-table-open"/, "工作台不再保留打开表格按钮");
  assert.doesNotMatch(html, /id="work-table-modal"/, "工作台不再保留表格弹窗");
  assert.match(html, /id="workbench-summary-view"/, "工作台必须有内联摘要宿主");
  assert.match(app, /createWorkbenchSummaryView\(/);
  assert.match(summary, /renderWorkbenchSummary\(/);
  assert.match(summary, /暂时没有 todo 项/);
  assert.doesNotMatch(app, /function openWorkTable\(/);
  assert.doesNotMatch(app, /setModal\("work-table-modal"/);
});
