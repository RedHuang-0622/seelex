import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

// app.js 是 DOM 绑定的入口脚本（node 里跑不起来），这里钉的是**接线口径**：
//
//   2026-10-12 J-1a：Agent Team 面板本体退场（`agent-team-view.js` 已删、`#team-view`
//   与面板刷新键随之消失），这条文件只留**仍然活着的那一半**接线——「角色会话只读
//   观察面」（团队看板「当前会话」入口 → openRoleSessionDetail → renderRoleSessionDetail）。
//   原 `agent-team-refresh.test.mjs` 里关于面板缓存（invalidateAgentTeam / agentTeamDirty）
//   与面板刷新键（`[data-team-refresh]`）的用例随面板一起删除：那些断言指向的对象已经
//   不存在，留着只会变成"改空壳凑绿"。
//
// 为什么仍要钉 team.changed：角色会话读面是**拉取**面（权威读数走
// Bridge.AgentTeamRoleSnapshot），它的热更新只能由后端这条事件送达——视图开着时重取
// 当前目标，关着时什么都不做（没有"最后心跳时间"这类推着走的东西）。
const appSource = await readFile(new URL("./app.js", import.meta.url), "utf8");

test("app.js still consumes the backend team.changed event (now only for the role-session view)", () => {
  assert.ok(
    appSource.includes('kind === "team.changed"'),
    "app.js must dispatch the backend team.changed event"
  );
  assert.ok(
    appSource.includes("if (roleSessionDetail) refreshRoleSessionDetail()"),
    "team.changed must hot-update the open role-session view (event-driven, no heartbeat)"
  );
  // 面板缓存那套（invalidateAgentTeam / agentTeamDirty）必须随面板一起消失：留一半
  // 就是"作废一个不存在的缓存"——永远不生效的死路径。
  assert.ok(
    !appSource.includes("invalidateAgentTeam") && !appSource.includes("agentTeamDirty"),
    "the retired panel cache must not linger in app.js"
  );
  assert.ok(
    !appSource.includes('"[data-team-refresh]"') && !appSource.includes("refreshAgentTeam"),
    "the retired panel refresh key must not linger in app.js"
  );
});

test("app.js never guesses team facts from the composer text", () => {
  assert.ok(
    !appSource.includes("text.startsWith(SIGIL_TEAM)"),
    "team facts must not be derived from the submitted text (goal auto-assembly and other surfaces would be missed)"
  );
});

test("app.js wires the role-session detail refresh key", () => {
  assert.ok(
    appSource.includes('"[data-role-session-refresh]"'),
    "the role-session detail must wire its own refresh key"
  );
  assert.ok(
    appSource.includes("refreshRoleSessionDetail"),
    "the role-session refresh must be a real function, not a fire-and-forget call"
  );
});

// 2026-10-03（口径修正）：员工侧不只是"事件驱动 + 手动刷新"——**它自己那一轮的实时工具
// 活动**走 teammate.tool.started/completed 的推送路（subagent.tool.* 的员工侧对称面）。
// 这里钉的是接线：消费这两个 kind、把载荷并进按角色会话分组的缓存、开着的正是这一位时
// 立刻重绘（不跑 RPC）、并且**不**把这一帧当快照重绘。
test("app.js hot-updates the role-session detail from teammate.tool.* frames", () => {
  assert.ok(
    appSource.includes('kind === "teammate.tool.started"') && appSource.includes('kind === "teammate.tool.completed"'),
    "app.js must consume the teammate tool-activity frames (they are the employee-side twin of subagent.tool.*)"
  );
  assert.ok(
    appSource.includes("applyTeammateToolActivity(payload)"),
    "the frames must be applied through one function (no ad-hoc DOM writes in the event switch)"
  );
  assert.ok(
    appSource.includes("roleSessionLiveTools.set(roleSessionID, steps)"),
    "live frames must be buffered per role session, so switching to another member shows their own activity"
  );
  assert.ok(
    appSource.includes("roleSessionLiveTools.set(roleSessionID, steps)") && appSource.includes("ROLE_LIVE_TOOL_LIMIT"),
    "the live buffer must be bounded (a ReAct round can take many steps)"
  );
  assert.ok(
    appSource.includes("detail.roleSessionID !== roleSessionID") && appSource.includes("renderRoleSessionView(detail.snapshot)"),
    "an arriving frame must only repaint when the open detail is that teammate, and it must repaint from the last snapshot + the new frame (no RPC)"
  );
  assert.ok(
    appSource.includes("liveTools"),
    "the live frames must reach the renderer (otherwise they are collected and never shown)"
  );
});

// 面板退场后，「切员工」的名册不再来自 TeamView（那支 RPC 随面板走）：它改读**团队看板
// 的在编名单**（runtime.teamwork_board 的 members[]，与看板行同一份投影）——同一条事实
// 只有一个来源，不在前端另编一份名册。
test("app.js switches the role-session view to another member, from the board roster", () => {
  assert.ok(
    appSource.includes('"[data-role-session-switch]"'),
    "the switcher must be wired so the view can move to another employee's session"
  );
  assert.ok(
    appSource.includes("roleSessionTargets"),
    "the switcher's members must come from the session roster, not be invented in the DOM"
  );
  assert.ok(
    appSource.includes("teammateSessionEntry(plan, roleName)") && appSource.includes("currentTeamPlan"),
    "the roster must be read from the teamwork board projection (one source of truth), not a second roster"
  );
  assert.ok(
    !appSource.includes("AgentTeamView") && !appSource.includes("agentTeamView"),
    "the retired TeamView RPC must not be called any more (the board projection is the source)"
  );
});
