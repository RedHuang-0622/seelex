import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const html = await readFile(new URL("./index.html", import.meta.url), "utf8");
const app = await readFile(new URL("./app.js", import.meta.url), "utf8");

test("new project dialog keeps a temporary draft list before creating", () => {
  assert.match(html, /id="new-project-drafts"/, "必须有待创建项目列表");
  assert.match(html, /id="new-project-create"/, "必须有批量创建动作");
  assert.match(app, /let newProjectDrafts\s*=\s*\[\]/, "前端必须持有新建项目 temp list");
  assert.match(app, /function renderNewProjectDrafts\(/);
  assert.match(app, /function addNewProjectDraft\(/);
});

test("project creation is sent to one backend batch call", () => {
  assert.match(app, /invoke\("CreateWorkspaces",/);
  assert.doesNotMatch(
    app.match(/elements\["new-session-pick-folder"\][\s\S]*?\n\}\);/)?.[0] || "",
    /invoke\("CreateWorkspace",/,
    "选择文件夹只能进入 temp list，不能在每次选择时立即创建"
  );
});

test("new project dialog uses the Codex-like stacked project rows", () => {
  assert.match(html, /新建任务或项目/);
  assert.match(html, /新建项目/);
  assert.match(app, /class="new-project-draft-row"/);
  assert.match(app, /class="new-project-draft-path"/);
  assert.match(app, /class="icon-button subtle new-project-draft-remove"/);
});
