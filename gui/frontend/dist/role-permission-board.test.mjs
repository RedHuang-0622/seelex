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
  const html = rolePermissionBoardHTML(rows, {
    mode: "session",
    expanded: new Set(["teammate"]),
    accounts: [
      { name: "teammate-1", role: "teammate", provider: "deepseek", model: "v3", api_key_set: true },
    ],
  });
  assert.match(html, /data-role-perm-cycle="read"/, "会话覆盖必须仍可编辑");
  assert.doesNotMatch(html, /data-role-perm-add="write"[^>]*disabled/, "会话覆盖不受角色默认继承开关影响");
  assert.doesNotMatch(html, /data-role-inherit="teammate"/, "继承 chip 只属于角色默认页");
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
  assert.match(sessionHTML, /路径 2/, "会话板必须显示会话覆盖的路径数量");
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
});
