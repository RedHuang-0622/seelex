import assert from "node:assert/strict";
import test from "node:test";

import { isPinnedRole, nextAgentTeamOrder, normalizeAgentTeam, renderAgentTeam } from "./agent-team-view.js";

const goalView = {
  session_id: "main-1",
  team_kind: "goal-a2a",
  order_policy: "goal_loop",
  order_roles: ["user", "main", "tl"],
  configured: true,
  floor_role: "tl",
  members: [
    { role_name: "user", role_kind: "user", in_order: true, order_index: 0 },
    { role_name: "main", role_kind: "main", in_order: true, order_index: 1 },
    { role_name: "tl", role_kind: "techlead", role_session_id: "goal-a2a-tl", in_order: true, order_index: 2 },
    { role_name: "digest", role_kind: "timer", join_policy: "scheduled", in_order: false, order_index: -1 }
  ],
  scheduled: [
    { role_name: "digest", role_kind: "timer", join_policy: "scheduled", in_order: false, order_index: -1 }
  ]
};

const presets = [{ team_kind: "goal-a2a" }, { team_kind: "review-team" }];

test("unconfigured session renders the empty state and preset entry points", () => {
  const html = renderAgentTeam({ configured: false, members: [], scheduled: [] }, presets);
  assert.match(html, /当前会话未装配 AgentTeam/);
  assert.match(html, /data-team-materialize="goal-a2a"/);
  assert.match(html, /data-team-materialize="review-team"/);
  assert.doesNotMatch(html, /team-order-item/);
});

test("configured team renders order, members and the scheduled partition", () => {
  const html = renderAgentTeam(goalView, presets);
  assert.match(html, /工作顺序/);
  assert.match(html, /data-team-materialize="goal-a2a"/);
  assert.match(html, /goal-a2a 已装配/);
  assert.match(html, /team-order-role">user</);
  assert.match(html, /team-order-role">tl</);
  assert.match(html, /floor/);
  assert.match(html, /发言中/);
  assert.match(html, /定时插话 · 不参与工作顺序/);
  // 定时 agent 出现在独立分区，不在工作顺序列表里。
  const orderSection = html.slice(html.indexOf("工作顺序"), html.indexOf("</ol>"));
  assert.doesNotMatch(orderSection, /digest/);
});

test("role names and notices are escaped, never interpolated raw", () => {
  const html = renderAgentTeam({
    configured: true,
    order_policy: "user_main_decided",
    order_roles: ["user", "main", "<img src=x onerror=alert(1)>"],
    members: [{ role_name: "<img src=x onerror=alert(1)>", role_kind: "agent", in_order: true, order_index: 2 }],
    scheduled: [],
    design_notice: ["floor.role_name 不在 lifecycle.order_roles"]
  }, presets);
  assert.doesNotMatch(html, /<img src=x/);
  assert.match(html, /&lt;img src=x onerror=alert\(1\)&gt;/);
  assert.match(html, /floor\.role_name 不在 lifecycle\.order_roles/);
});

test("pinned roles cannot be removed from the working order", () => {
  assert.equal(isPinnedRole("user"), true);
  assert.equal(isPinnedRole("main"), true);
  assert.equal(isPinnedRole("tl"), false);
  const html = renderAgentTeam(goalView, presets);
  assert.match(html, /data-team-action="remove" data-team-role="user"[^>]*disabled/);
  assert.match(html, /data-team-action="remove" data-team-role="tl"/);
});

test("nextAgentTeamOrder only rewrites the working order", () => {
  assert.deepEqual(nextAgentTeamOrder(goalView, "up", "tl"), {
    policy: "goal_loop", orderRoles: ["user", "tl", "main"]
  });
  assert.deepEqual(nextAgentTeamOrder(goalView, "down", "user"), {
    policy: "goal_loop", orderRoles: ["main", "user", "tl"]
  });
  assert.deepEqual(nextAgentTeamOrder(goalView, "remove", "tl"), {
    policy: "goal_loop", orderRoles: ["user", "main"]
  });
  assert.deepEqual(nextAgentTeamOrder(goalView, "restore", "digest"), {
    policy: "goal_loop", orderRoles: ["user", "main", "tl", "digest"]
  });
  // 非法动作：pin 角色摘除、边界移动、重复恢复、角色不在表内。
  assert.equal(nextAgentTeamOrder(goalView, "remove", "main"), null);
  assert.equal(nextAgentTeamOrder(goalView, "up", "user"), null);
  assert.equal(nextAgentTeamOrder(goalView, "down", "tl"), null);
  assert.equal(nextAgentTeamOrder(goalView, "restore", "tl"), null);
  assert.equal(nextAgentTeamOrder(goalView, "up", "digest"), null);
});

test("normalizeAgentTeam tolerates malformed payloads", () => {
  assert.deepEqual(normalizeAgentTeam(null), {
    sessionID: "", teamID: "", teamKind: "", orderPolicy: "", orderRoles: [],
    members: [], scheduled: [], configured: false, floorRole: "", designNotice: []
  });
  const partial = normalizeAgentTeam({
    order_roles: ["user", 7, ""],
    members: [{ role_name: "tl" }, { role_name: "" }, null],
    scheduled: "nope"
  });
  assert.deepEqual(partial.orderRoles, ["user"]);
  assert.equal(partial.members.length, 1);
  assert.equal(partial.members[0].roleName, "tl");
  assert.deepEqual(partial.scheduled, []);
  assert.equal(partial.configured, false);
});
