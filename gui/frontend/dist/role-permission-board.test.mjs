import assert from "node:assert/strict";
import test from "node:test";

import {
  orderedPermissionRoles,
  permissionCandidateTags,
  permissionRoleLabel,
  rolePermissionBoardHTML,
} from "./role-permission-board.js";

const rows = [
  { role: "teammate", accounts: ["teammate-1"], account: "teammate-1", default_permission_vector: ["read"], inherit_main_agent: true, explicit: true, permission_vector: ["read", "ssh"] },
  { role: "agent", accounts: ["agent-1"], account: "agent-1", default_permission_vector: ["read", "write"], permission_vector: ["read", "write"], explicit: true },
  { role: "goalplan", accounts: ["goalplan-1"], default_permission_vector: [], permission_vector: [], explicit: false },
  { role: "subagent", accounts: ["subagent-1"], default_permission_vector: ["read"], inherit_main_agent: false, permission_vector: ["read"], explicit: true },
];

test("角色按 goalplan / mainagent / subagent / teammate 展示", () => {
  assert.equal(permissionRoleLabel("agent"), "mainagent");
  assert.deepEqual(orderedPermissionRoles(rows).map(row => row.role), ["goalplan", "agent", "subagent", "teammate"]);
});

test("候选栏只列未选择权限，确认栏保留 allow / deny 三态", () => {
  assert.deepEqual(permissionCandidateTags(["read", "!write"]).map(tag => tag.id).includes("read"), false);
  assert.deepEqual(permissionCandidateTags(["read", "!write"]).map(tag => tag.id).includes("write"), false);
});

test("subagent / teammate 默认继承 mainagent，账户只读继承", () => {
  const html = rolePermissionBoardHTML(rows, {
    mode: "default",
    expanded: new Set(["teammate"]),
    accounts: [
      { name: "agent-1", role: "agent", provider: "openai", model: "gpt-4o", api_key_set: true },
      { name: "teammate-1", role: "teammate", provider: "deepseek", model: "v3", api_key_set: true },
    ],
  });
  assert.match(html, /继承 mainagent 权限/);
  assert.match(html, /data-role-inherit="teammate" aria-pressed="true"/);
  assert.match(html, /role-permission-name">mainagent/);
  assert.match(html, /data-role-account-pick="agent-1"/);
  assert.match(html, /data-role-account-edit="agent-1"/);
  assert.match(html, /data-role-account-delete="agent-1"/);
  assert.match(html, /teammate-1/);
  assert.doesNotMatch(html, /账户权限|账户设置权限/, "账户没有独立权限编辑入口");
  assert.match(html, /data-role-perm-cycle="read"[^>]*disabled/, "继承态的角色默认不得改已选权限");
  assert.match(html, /data-role-perm-add="ssh"[^>]*disabled|data-role-perm-add="computeruse"[^>]*disabled/, "继承态的角色默认不得追加权限");
});

test("继承只锁角色默认，当前会话仍可按 agent role 覆盖", () => {
  const defaultHTML = rolePermissionBoardHTML(rows, {
    mode: "default",
    expanded: new Set(["teammate"]),
    accounts: [
      { name: "teammate-1", role: "teammate", provider: "deepseek", model: "v3", api_key_set: true },
    ],
  });
  const sessionHTML = rolePermissionBoardHTML(rows, {
    mode: "session",
    expanded: new Set(["teammate"]),
  });
  assert.match(defaultHTML, /data-role-perm-cycle="read"[^>]*disabled/, "角色默认继承锁只锁默认栏");
  assert.match(sessionHTML, /data-role-perm-cycle="ssh"/, "本会话覆盖仍可编辑");
  assert.doesNotMatch(sessionHTML, /data-role-perm-cycle="ssh"[^>]*disabled/, "会话覆盖不受继承锁影响");
  assert.doesNotMatch(sessionHTML, /data-role-inherit="teammate"/, "会话权限弹窗不重复继承设置");
});

test("SSH 是单一能力，选中后显示路径范围入口", () => {
  const html = rolePermissionBoardHTML([{
    role: "agent",
    accounts: [],
    permission_vector: ["read", "ssh"],
    default_permission_vector: ["read", "ssh"],
    default_ssh_user_spaces: ["/home/seelex/.cache"],
    session_ssh_user_spaces: ["/home/seelex/.cache", "/srv/shared"],
  }], { mode: "default", expanded: new Set(["agent"]) });
  assert.match(html, /data-role-user-spaces="agent"/);
  assert.match(html, /路径 1/);
  assert.doesNotMatch(html, /ssh\.userspace/);

  const sessionHTML = rolePermissionBoardHTML([{
    role: "agent",
    accounts: [],
    permission_vector: ["read", "ssh"],
    default_permission_vector: ["read", "ssh"],
    default_ssh_user_spaces: ["/home/seelex/.cache"],
    session_ssh_user_spaces: ["/home/seelex/.cache", "/srv/shared"],
  }], { mode: "session", expanded: new Set(["agent"]) });
  assert.match(sessionHTML, /路径 2/, "会话权限面显示会话覆盖的路径数量");
});

test("账号行在所属角色内展示上下文窗口，不把账户平铺回全局", () => {
  const html = rolePermissionBoardHTML(rows, {
    mode: "default",
    expanded: new Set(["agent"]),
    windowPresets: [200000, 400000],
    accounts: [
      { name: "agent-1", role: "agent", provider: "openai", model: "gpt-4o", context_window: 200000 },
    ],
  });
  assert.match(html, /data-role-account-window-select="agent-1"/);
  assert.match(html, /<option value="200000" selected>200k<\/option>/);
  const row = html.match(/<div class="role-account-row[\s\S]*?<\/div>\s*<\/div>/)?.[0] || "";
  assert.match(row, /data-role-account-pick="agent-1"[\s\S]*role-account-window[\s\S]*data-role-account-window-select="agent-1"/, "上下文窗口必须嵌在账户条目内部");
  assert.match(row, /甜点上下文/, "账户内的窗口要说明它是该模型的甜点上下文");
});

test("全局角色板不渲染会话权限控件", () => {
  const html = rolePermissionBoardHTML(rows, { mode: "default", expanded: new Set(["agent"]) });
  assert.doesNotMatch(html, /data-board="session"/);
  assert.match(html, /data-board="default"/);
});
