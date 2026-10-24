// 设置作用域必须能从结构上区分：应用设置、账户新增二级窗口、项目设置、会话 SSH。
// 这些断言钉住 DOM 归属，不替代 Go 侧的 scope/权限判据。
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const read = (name) => readFile(new URL(`./${name}`, import.meta.url), "utf8");

function modal(html, id) {
  return html.match(new RegExp(`<div id="${id}"[\\s\\S]*?\\n  </div>`))?.[0] || "";
}

test("应用设置有分类导航，账户新增不再平铺在首页", async () => {
  const html = await read("index.html");
  const settings = modal(html, "settings-modal");
  assert.match(settings, /id="settings-nav"/);
  for (const page of ["account", "roles", "session", "application"]) {
    assert.match(settings, new RegExp(`data-settings-page="${page}"`));
    assert.match(settings, new RegExp(`data-settings-page-panel="${page}"`));
  }
  assert.match(settings, /id="account-add-open"/, "账户页要有一个打开二级窗口的入口");
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
});

test("项目 SSH 连接表在项目设置，会话只保留限定与放行", async () => {
  const html = await read("index.html");
  const project = modal(html, "project-settings-modal");
  const settings = modal(html, "settings-modal");
  assert.ok(project, "必须有 project-settings-modal");
  assert.match(project, /id="ssh-env-list"/);
  assert.match(project, /id="ssh-env-host"/);
  assert.match(project, /id="ssh-env-save"/);
  assert.doesNotMatch(project, /id="session-ssh-allowed"/, "会话放权不属于项目连接表");
  assert.match(settings, /id="session-ssh-allowed"/);
  assert.match(settings, /id="session-ssh-env"/);
  assert.match(html, /id="project-settings-button"/, "项目标题区要有项目设置入口");
});

test("角色页把当前会话生效值与角色默认分开呈现", async () => {
  const html = await read("index.html");
  const app = await read("app.js");
  const settings = modal(html, "settings-modal");
  assert.match(settings, /id="role-account"/);
  assert.match(settings, /id="role-permission-vector"/);
  assert.match(settings, /id="permission-role-vector"/);
  assert.match(settings, /id="permission-role-account"/, "每个角色要有自己的账号选择");
  assert.match(settings, /id="permission-role-effective"/, "要能看见当前生效向量");
  assert.match(app, /row\.default_vector/, "默认编辑读取独立的 default_vector");
  assert.match(app, /row\.vector/, "生效读取来自生效向量");
  assert.match(app, /invoke\("SetRoleAccount"/, "角色账号要写回后端");
});
