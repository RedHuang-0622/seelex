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
