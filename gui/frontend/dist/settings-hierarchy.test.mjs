// 设置作用域必须能从结构上区分：应用设置、账户新增二级窗口、项目设置、会话 SSH。
// 这些断言钉住 DOM 归属，不替代 Go 侧的 scope/权限判据。
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const read = (name) => readFile(new URL(`./${name}`, import.meta.url), "utf8");

function modal(html, id) {
  return html.match(new RegExp(`<div id="${id}"[\\s\\S]*?\\n  </div>`))?.[0] || "";
}

test("设置只管理全局角色权限，会话覆盖移回会话界面", async () => {
  const html = await read("index.html");
  const settings = modal(html, "settings-modal");
  assert.match(settings, /id="settings-nav"/);
  for (const page of ["roles", "application"]) {
    assert.match(settings, new RegExp(`data-settings-page="${page}"`));
    assert.match(settings, new RegExp(`data-settings-page-panel="${page}"`));
  }
  assert.doesNotMatch(settings, /data-settings-page="session"/, "会话权限不再是独立一级分类");
  assert.match(settings, /id="permission-role-board"/, "角色页要有角色权限板");
  assert.doesNotMatch(settings, /id="session-role-board"/, "全局设置不出现会话覆盖板");
  assert.match(html, /id="session-permission-modal"/, "会话权限使用独立会话弹窗");
  assert.match(html, /id="session-permission-board"/, "会话弹窗承载按 agent role 覆盖");
  assert.doesNotMatch(settings, /id="account-add-api-key"/, "API Key 表单不得平铺在一级设置");
  assert.doesNotMatch(settings, /id="ssh-env-host"/, "项目 SSH 连接表单不得平铺在应用设置");
});

test("添加账户是独立二级弹窗，凭据字段只在弹窗内", async () => {
  const html = await read("index.html");
  const add = modal(html, "account-add-modal");
  assert.ok(add, "必须有 account-add-modal");
  for (const id of ["account-add-role", "account-add-model", "account-add-api-key", "account-add-submit"]) {
    assert.match(add, new RegExp(`id="${id}"`));
  }
  assert.match(add, /aria-modal="true"/);
  const remove = modal(html, "account-delete-modal");
  for (const id of ["account-delete-close", "account-delete-cancel", "account-delete-confirm"]) {
    assert.match(remove, new RegExp(`id="${id}"`));
  }
});

test("项目 SSH 连接表只在项目设置，全局应用设置不含 SSH", async () => {
  const html = await read("index.html");
  const project = modal(html, "project-settings-modal");
  const settings = modal(html, "settings-modal");
  assert.ok(project, "必须有 project-settings-modal");
  assert.match(project, /主机留空即本地环境/, "项目环境表要说明 local/ssh 判据");
  assert.match(project, /id="ssh-env-list"/);
  assert.match(project, /id="ssh-env-host"/);
  assert.match(project, /id="ssh-env-save"/);
  assert.doesNotMatch(project, /id="session-ssh-allowed"/, "会话放权不属于项目连接表");
  assert.doesNotMatch(settings, /SSH 环境|ssh-env-list|session-ssh-allowed/, "全局设置不得包含 SSH 设置");
  assert.match(html, /id="project-settings-button"/, "项目标题区要有项目设置入口");
  assert.match(html, /id="project-settings-button"[^>]*>管理环境<\/button>/, "项目入口要明确管理工作环境");
});

test("角色页使用角色卡、候选/确认权限栏，并支持继承 chip", async () => {
  const html = await read("index.html");
  const app = await read("app.js");
  const settings = modal(html, "settings-modal");
  assert.match(settings, /id="permission-role-board"/);
  assert.doesNotMatch(settings, /id="session-role-board"/);
  assert.doesNotMatch(settings, /id="session-permission-board"/, "会话权限不得回流全局设置");
  assert.match(html, /id="session-permission-button"/);
  assert.doesNotMatch(settings.match(/data-settings-page-panel="roles"[\s\S]*?data-settings-page-panel="application"/)?.[0] || "", /id="account-windows"|id="role-default-plugin"/, "角色页只保留角色层级；账户运行参数落在角色卡内，应用参数落在应用页");
  assert.match(app, /rolePermissionBoardHTML/);
  assert.match(app, /data-role-inherit/);
  assert.match(app, /data-role-perm-add/);
  assert.match(app, /SetRoleSessionPermission/, "会话权限必须按 agent role 更新");
  assert.match(app, /invoke\("SetRoleAccount"/, "角色账号要写回后端");
  assert.match(app, /add\.value === "ssh"[\s\S]*openSSHUserSpaceDialog/, "勾选 SSH 要打开路径弹窗");
});

test("弹窗关闭按钮固定在弹窗上沿，不随内容滚动", async () => {
  const css = await read("styles.css");
  assert.match(css, /\.settings-card > \.modal-header,[\s\S]*?position: sticky;[\s\S]*?top: 0;/);
});
