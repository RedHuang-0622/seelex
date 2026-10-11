import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import test from "node:test";

// Agent Team 面板退场的**守卫**（2026-10-12 J-1a，用户裁决：`@<团队>` 手动召唤直接删除）。
//
// 为什么要有这一条：退场是"删面"，而删面最容易留下的不是报错，是**半条通道**——
// 少了监听器的入口、作废一个不存在的缓存、指回已删模块的 import。它们全都不报错，
// 只是永远不生效。所以这里把"退场后的形状"钉成断言：谁把面板的任一片搬回来，
// 或谁把团队看板（teamwork，保留面）顺手带走，立刻就红。
//
// 保留面（本文件的阴性断言同样重要）：
//   - 「工作台」页的团队看板 `#team-board-section` / `#team-board-view`（数据源
//     runtime.teamwork_board）——团队面只剩这一块，绝不删、绝不改 id；
//   - 角色会话只读观察面（`role-session-view.js` + `AgentTeamRoleSnapshot` 调用点）。

const read = (name) => readFile(new URL(`./${name}`, import.meta.url), "utf8");

test("退场的资产不再存在：agent-team-view.js 与它的两条测试都已下架", async () => {
  const files = await readdir(new URL(".", import.meta.url));
  assert.equal(files.includes("agent-team-view.js"), false, "面板渲染件必须已删（不留空壳文件）");
  assert.equal(files.includes("agent-team-view.test.mjs"), false, "面板渲染件的测试必须已删");
  assert.equal(files.includes("agent-team-refresh.test.mjs"), false, "面板刷新接线的测试必须已删");
  // 只读那一面的新房客在场：删面不等于把读面也删掉。
  assert.ok(files.includes("role-session-view.js"), "角色会话只读观察面必须有落点");
  assert.ok(files.includes("role-session-view.test.mjs"), "读面的用例要跟着搬，不是删掉");
});

test("app.js 里不留半条退场通道：没有 AgentTeam* 面板 RPC、没有面板状态与刷新钩子", async () => {
  const app = await read("./app.js");
  for (const token of [
    "./agent-team-view.js",
    "invalidateAgentTeam", "refreshAgentTeam", "renderAgentTeamPanel", "scheduleAgentTeamRefresh",
    "runAgentTeamAction", "closeAgentTeamEditors", "openAgentTeamHire", "openAgentTeamTeamPanel",
    "optimizeAgentTeamPrompt", "agentTeamView", "agentTeamLibrary", "agentTeamGlobal",
    "agentTeamDirty", "agentTeamError", "agentTeamLoading", "agentTeamSessionID",
    "[data-team-refresh]", "[data-team-hire-plugins]", "team-section", "team-view",
  ]) {
    assert.equal(app.includes(token), false, `app.js 不得再出现退场面标记：${token}`);
  }
  // 面板动作整族：一个都不许留（留一个就是"点不动的按钮/永不生效的调用"）。
  // 只许剩 AgentTeamRoleSnapshot 的**两处**调用点（角色会话只读观察面：打开 + 刷新）。
  const rpcs = app.match(/invoke\("AgentTeam[A-Za-z]*"/g) || [];
  assert.deepEqual(rpcs, [
    'invoke("AgentTeamRoleSnapshot"',
    'invoke("AgentTeamRoleSnapshot"',
  ], "只许留 AgentTeamRoleSnapshot（角色会话只读观察面：open + refresh 两处）");
  // 面板整族 RPC 名一个不留（上面那条已覆盖，这里再按名字钉一遍，防止改了写法）。
  for (const rpc of ["AgentTeamView", "AgentTeamLibrary", "AgentTeamGlobalConfig", "AgentTeamMaterializeTeam", "AgentTeamSaveTeam", "AgentTeamDeleteTeam", "AgentTeamSetOrder", "AgentTeamDeleteRole", "AgentTeamInstantiateRole", "AgentTeamSaveEmployee", "AgentTeamDeleteEmployee", "AgentTeamOptimizePrompt"]) {
    assert.equal(app.includes(rpc), false, `面板 RPC 不得再被调用：${rpc}`);
  }
});

test("index.html：Agent Team 折叠块整块退场，团队看板原样保留", async () => {
  const html = await read("./index.html");
  for (const id of ["team-section", "team-view", "team-count"]) {
    assert.equal(html.includes(`id="${id}"`), false, `「状态」页的 Agent Team 块必须整块删除：${id}`);
  }
  // 保留面：团队看板的三件套（id 与数据源都不许动）。
  for (const id of ["team-board-section", "team-board-view", "team-board-badge"]) {
    assert.ok(html.includes(`id="${id}"`), `团队看板挂载点必须保留：${id}`);
  }
  assert.ok(html.includes('id="team-board-section" class="hidden"'), "团队看板仍必须默认 hidden（无计划不留空壳）");
});

test("空态与 placeholder 不再提「@ 手动召唤团队」", async () => {
  const [html, chat] = await Promise.all([read("./index.html"), read("./chat-view.js")]);
  for (const [name, source] of [["index.html", html], ["chat-view.js", chat]]) {
    assert.equal(source.includes("手动召唤团队"), false, `${name} 不得再写团队召唤口径`);
    assert.equal(source.includes("召唤团队"), false, `${name} 不得再写「召唤团队」`);
  }
  // 空态提示与 composer placeholder 都不许留一个孤立的 `@` 提示位。J-1b 落点之后
  // （2026-10-12）输入框可以（且应当）提 `@`，但口径只能是**文件引用**：`@ 引用工作区
  // 文件`，不许写成召唤团队，也不许只留一个光秃秃的 `@`。
  assert.doesNotMatch(html, /<span class="kbd" aria-hidden="true">@<\/span>/);
  assert.match(html, /id="prompt"[^>]*placeholder="[^"]*@ 引用工作区文件/, "@ 的口径必须是文件引用");
  // 前缀表里其余三条照旧（别把同伴一起带走）。
  assert.match(html, /<span class="kbd" aria-hidden="true">\/<\/span> 命令与工具/);
  assert.match(html, /<span class="kbd" aria-hidden="true">\$<\/span> 召回 Skill/);
});

test("团队看板的渲染件与接线不许被退场改动带走", async () => {
  const app = await read("./app.js");
  assert.ok(app.includes('import { TEAM_BOARD_CSS, TEAMMATE_LIVE_PAGE_SIZE') && app.includes('from "./team-board-view.js"'), "app.js 仍必须引团队看板渲染件");
  assert.ok(app.includes("function bindTeamBoardActions()") && app.includes('elements["team-board-view"]'), "看板入口的委托仍必须绑在 #team-board-view 上");
  assert.ok(app.includes("renderTeamBoard("), "看板渲染调用不得被删");
  assert.ok(app.includes("teammateSessionEntry"), "看板的 teammate 会话判定面必须保留（角色会话读面的唯一判定）");
});
