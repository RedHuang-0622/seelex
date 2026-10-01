import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

// app.js 是 DOM 绑定的入口脚本（node 里跑不起来），这里钉的是**接线口径**：
//
//   会话团队事实（装配/工作顺序/入职/编辑成员）变了之后，Agent Team 面板必须收到
//   状态更新。面板数据不在会话快照里（按需 RPC 拉 AgentTeamView），而面板把它们按
//   会话键缓存，所以"事实变了"只能由后端的 team.changed 事件送达；前端不得从自己的
//   composer 文本里猜（那样会漏掉 goal 自动装配、面板收起时发起的召唤、以及非本
//   composer 的来路）。
const appSource = await readFile(new URL("./app.js", import.meta.url), "utf8");

test("app.js consumes the backend team.changed event for the Agent Team panel", () => {
  assert.ok(
    appSource.includes('kind === "team.changed"'),
    "app.js must dispatch the backend team.changed event"
  );
  assert.ok(
    appSource.includes("invalidateAgentTeam()"),
    "team.changed must invalidate the Agent Team panel cache"
  );
  assert.ok(
    appSource.includes("agentTeamDirty"),
    "the panel cache must carry a dirty flag so a later render/expand refetches instead of reusing stale members"
  );
});

test("app.js no longer guesses the panel refresh from the composer text", () => {
  assert.ok(
    !appSource.includes("text.startsWith(SIGIL_TEAM)"),
    "the panel refresh must not be derived from the submitted text (goal auto-assembly and other surfaces would be missed)"
  );
});

// E（前端）：员工侧的两件事也走**事件驱动 + 手动刷新键**（没有心跳）。这里钉的是
// app.js 的**接线口径**：面板刷新键、运行详情刷新键、切员工、以及 team.changed
// 对已打开的运行详情的热更新，都不能只靠 npm 里的渲染单测（那是 DOM 外的纯函数）。

test("app.js wires the Agent Team panel's manual refresh key", () => {
  assert.ok(
    appSource.includes('"[data-team-refresh]"'),
    "the panel must wire data-team-refresh to a forced refetch"
  );
  assert.ok(
    appSource.includes("refreshAgentTeam({ force: true })"),
    "the manual refresh key must force the refetch (no cache short-circuit)"
  );
});

test("app.js wires the employee-session detail refresh key and hot-updates it on team.changed", () => {
  assert.ok(
    appSource.includes('"[data-role-session-refresh]"'),
    "the role-session detail must wire its own refresh key"
  );
  assert.ok(
    appSource.includes("refreshRoleSessionDetail"),
    "the role-session detail refresh must be a real function, not a fire-and-forget call"
  );
  assert.ok(
    appSource.includes("if (roleSessionDetail) refreshRoleSessionDetail()"),
    "team.changed must refresh the open employee-session view (event-driven, no heartbeat)"
  );
});

test("app.js switches the employee-session view to another member (对话视图切员工)", () => {
  assert.ok(
    appSource.includes('"[data-role-session-switch]"'),
    "the switcher must be wired so the view can move to another employee's session"
  );
  assert.ok(
    appSource.includes("roleSessionTargets"),
    "the switcher's members must come from the session roster, not be invented in the DOM"
  );
});
