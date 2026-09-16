import assert from "node:assert/strict";
import test from "node:test";

import {
  agentTeamOrderForDrag,
  employeePool,
  hirePanel,
  isPinnedRole,
  nextAgentTeamOrder,
  normalizeAgentTeam,
  normalizeTeamGlobal,
  normalizeTeamLibrary,
  renderAgentTeam,
  renderRoleSessionDetail,
  renderTeamMemberList,
  roleDisplayName,
  teamEditorPanel,
  teamGlobalDrift,
  teamMemberNames,
  toolsPolicyLabel
} from "./agent-team-view.js";

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
    { role_name: "tl", role_kind: "techlead", role_session_id: "goal-a2a-tl", in_order: true, order_index: 2, tools_policy: "readonly", system_prompt: "你是评审官" },
    { role_name: "digest", role_kind: "timer", join_policy: "scheduled", in_order: false, order_index: -1 }
  ],
  scheduled: [
    { role_name: "digest", role_kind: "timer", join_policy: "scheduled", in_order: false, order_index: -1 }
  ]
};

const presets = [
  { team_kind: "goal-a2a", order_policy: "goal_loop", order_roles: ["user", "main", "tl"], roles: [{ role_name: "tl", role_kind: "techlead" }] },
  { team_kind: "review-team", order_policy: "user_main_decided", order_roles: ["user", "main", "reviewer"], roles: [{ role_name: "reviewer", role_kind: "agent" }] }
];

const library = {
  configured: true,
  teams: [
    {
      team_id: "my-team",
      team_kind: "my-team",
      name: "我的审计队",
      order_policy: "user_main_decided",
      order_roles: ["user", "main", "auditor"],
      origin: "current-session",
      roles: [{ role_name: "auditor", role_kind: "agent", system_prompt: "审计员", tools_policy: "readonly" }]
    }
  ]
};

test("unconfigured session renders the empty state and team entry points", () => {
  const html = renderAgentTeam({ configured: false, members: [], scheduled: [] }, presets, null);
  assert.match(html, /当前会话未装配 AgentTeam/);
  // 内置形态只留一行 chip（就地装配），库条目给「点团队名打开面板 / 装配 / 删除」。
  assert.match(html, /data-team-materialize="goal-a2a"/);
  assert.match(html, /data-team-materialize="review-team"/);
  assert.match(html, /data-team-open-team="1"/);
  // 「存当前会话」这条操作已移除（团队面板的保存取代了它）。
  assert.doesNotMatch(html, /data-team-save-current="1"/);
  assert.doesNotMatch(html, /data-team-form-members/);
});

test("团队库 lists the user's own teams; built-in presets are chips, not library rows", () => {
  const html = renderAgentTeam(goalView, presets, library);
  assert.match(html, /team-rail-head[\s\S]*?<span>团队库<\/span>/);
  assert.match(html, /role="table" aria-label="团队库"/);
  assert.match(html, /我的审计队/);
  assert.match(html, /data-team-materialize-team="my-team"/);
  // 点团队名 = 打开这支团队的团队面板（唯一入口，不再另设「编辑」）。
  assert.match(html, /data-team-edit-team="my-team"/);
  assert.match(html, /data-team-delete-team="my-team"/);
  // 内置形态不再是"库条目"：没有 data-team-library-template / 存入库，
  // 当前形态的 chip 是禁用态（重复装配幂等，但不是可点的动作）。
  assert.doesNotMatch(html, /data-team-library-template/);
  assert.doesNotMatch(html, /data-team-save-template/);
  assert.match(html, /class="team-preset-chip is-active" data-team-materialize="goal-a2a"[^>]*disabled/);
  // 「默认顺序 vs 本会话」那张 项/值 表已删除：默认顺序不再单独摆一张表。
  assert.doesNotMatch(html, /默认顺序/);
  assert.doesNotMatch(html, /aria-label="发言顺序（全局默认 vs 本会话）"/);
});

test("团队库 head carries no annotation text", () => {
  const html = renderAgentTeam(goalView, presets, library, globalConfig);
  for (const annotation of ["全局·跨会话", "不依赖团队", "3 个内置形态", "拖拽行首手柄", "装配与编排", "尚未装配团队"]) {
    assert.doesNotMatch(html, new RegExp(annotation));
  }
  assert.doesNotMatch(html, /team-rail-hint/);
});

test("员工栏 holds the working order and exposes drag handles + permissions", () => {
  const html = renderAgentTeam(goalView, presets, library);
  assert.match(html, /team-rail-head[\s\S]*?<span>员工栏<\/span>/);
  assert.match(html, /role="table" aria-label="员工栏"/);
  assert.match(html, /role="columnheader">员工（拖拽调序 · 权限）<\/span>/);
  assert.match(html, /role="columnheader">操作<\/span>/);
  // 发言顺序就在员工栏里：拖拽条 + 逻辑角色名 + 位置 + 权限 chip。
  assert.match(html, /data-team-drag="tl" draggable="true"/);
  assert.match(html, /data-team-staff-role="tl"[^>]*data-team-order-role="tl"[^>]*data-team-in-order="1"/);
  assert.match(html, /team-member-role" title="逻辑角色名（metadata，不是 provider role）">tl</);
  assert.match(html, /team-member-pos" title="工作顺序位置">#3</);
  assert.match(html, /team-perm-chip"[^>]*>只读</);
  assert.match(html, /data-team-order-drop="end"/);
  // 定时 agent 不在发言顺序里（它没有拖拽条，位置标"定时"）。
  const staffSection = html.slice(html.indexOf('aria-label="员工栏"'), html.indexOf('data-team-order-drop="end"'));
  assert.match(staffSection, /data-team-staff-role="digest"[^>]*data-team-in-order="0"/);
  assert.doesNotMatch(staffSection, /data-team-drag="digest"/);
  // 冷加载槽位默认空（表单不常驻）。
  assert.match(html, /data-team-hire-slot hidden/);
  assert.doesNotMatch(html, /data-team-hire-form/);
});

test("team members expose distinct agent identities and role-session entry points", () => {
  assert.equal(roleDisplayName("main", "main"), "EXEC");
  assert.equal(roleDisplayName("tl", "techlead"), "ADVISOR");
  assert.equal(roleDisplayName("user", "user"), "USER");
  assert.equal(roleDisplayName("reviewer", "agent"), "reviewer");
  const html = renderAgentTeam(goalView, presets, library);
  // 两个 agent 不再是同一个 AGENT 文案：EXEC（main）与 ADVISOR（tl）分开。
  assert.match(html, /data-team-role-open="main"/);
  assert.match(html, /data-team-role-open="tl" data-team-role-session="goal-a2a-tl"/);
  assert.match(html, />EXEC</);
  assert.match(html, />ADVISOR</);
});

// ── 冷加载面板 ────────────────────────────────────────────────

test("hirePanel carries prompt + permission fields and closes itself", () => {
  const team = normalizeAgentTeam(goalView);
  const member = team.members.find(item => item.roleName === "tl");
  const html = hirePanel(team, member);
  assert.match(html, /data-team-editor="hire"/);
  assert.match(html, /data-team-editor-close="1"/);
  assert.match(html, /修改员工 · ADVISOR/);
  assert.match(html, /data-team-hire-name[^>]*value="tl"[^>]*readonly/);
  // 提示词与权限必须回填（否则一次编辑会把用户登记的东西清空）。
  assert.match(html, /data-team-hire-prompt[^>]*>你是评审官</);
  assert.match(html, /data-team-hire-tools[\s\S]*?<option value="readonly" selected>/);
  assert.match(html, /data-team-hire-model/);
  assert.match(html, /data-team-optimize="1"/);
  assert.match(html, /data-team-prompt-result hidden/);
  // 新增态：空表单 + 入职按钮，角色名可写。
  const fresh = hirePanel(team, null);
  assert.match(fresh, /入职员工/);
  assert.match(fresh, /data-team-hire-name[^>]*placeholder="reviewer \/ auditor…"[^>]*value=""[^>]*required>/);
  assert.doesNotMatch(fresh, /data-team-hire-name[^>]*readonly/);
});

// 用户口径：员工面板（入职 / 新建员工 / 修改员工）里不摆小字备注——字段说明与
// 底部作用域说明都撤掉，说明改走控件 hover 提示（title），选项标签自己把话说完。
test("员工面板不摆小字备注（说明只在 title 里）", () => {
  const team = normalizeAgentTeam(goalView);
  const member = team.members.find(item => item.roleName === "tl");
  const panels = [
    hirePanel(team, member),                      // 修改员工 · ADVISOR（会话作用域）
    hirePanel(team, null),                        // 入职员工（会话作用域新增）
    hirePanel(team, member, "library"),           // 修改员工 · ADVISOR（员工库作用域）
    hirePanel(team, null, "library")              // 新建员工 · 员工库
  ];
  for (const html of panels) {
    assert.doesNotMatch(html, /team-field-hint/);
    assert.doesNotMatch(html, /team-editor-hint/);
    // 去掉 title / data-tip / placeholder（hover 与空态提示）后，可见正文里不该再出现
    // 这些原本当小字备注的句子。
    const visible = html.replace(/(?:title|data-tip|placeholder)="[^"]*"/g, "");
    assert.doesNotMatch(visible, /同名角色|已装配的会话|只写员工库|小写英文|这一位用哪个模型|worker 干活|什么时候把这个员工/);
  }
  // 说明没丢，只是进 title：关键口径仍挂在控件/提交键上。
  assert.match(panels[0], /data-team-hire-name[^>]*title="[^"]*同名即就地覆盖/);
  assert.match(panels[0], /data-team-hire-tools[^>]*title="[^"]*readonly/);
  assert.match(panels[0], /data-team-hire-submit title="[^"]*同名角色就地覆盖/);
  assert.match(panels[2], /data-team-hire-submit title="[^"]*员工库/);
  // 「优化提示词」的运行态回执照旧小字显示（空态不占位），它自己的类与备注分开。
  assert.match(panels[0], /class="team-prompt-state" data-team-optimize-state/);
  // 面板骨架与字段数不变（7 个字段一条流水线，编号还在）。
  assert.equal((panels[0].match(/class="team-field-no"/g) || []).length, 8);
  assert.match(panels[0], /data-team-hire-prompt/);
});

test("teamEditorPanel fills from the library entry and closes itself", () => {
  const html = teamEditorPanel(normalizeAgentTeam(goalView), normalizeTeamLibrary(library).teams[0], presets);
  assert.match(html, /data-team-editor="team"/);
  assert.match(html, /团队 · 我的审计队/);
  assert.match(html, /data-team-editor-close="1"/);
  assert.match(html, /data-team-form-name[^>]*value="我的审计队"/);
  assert.match(html, /data-team-form-id[^>]*value="my-team"[^>]*readonly/);
  // 成员表：行序即发言顺序，每行可拖拽、可 ✕。
  assert.match(html, /data-team-member-list/);
  assert.match(html, /data-team-member-item="auditor" data-team-member-drag="auditor" draggable="true"/);
  assert.match(html, /data-team-member-remove="auditor"/);
  assert.match(html, /data-team-form-policy[\s\S]*?<option value="user_main_decided" selected>/);
  assert.match(html, /data-team-template="review-team"/);
  assert.match(html, /data-team-form-fill-current="1"/);
  assert.match(html, /当前会话：tl/);
  // 新建态：空条目、可写团队 ID。
  const fresh = teamEditorPanel(normalizeAgentTeam(goalView), null, presets);
  assert.match(fresh, />新建团队</);
  assert.doesNotMatch(fresh, /data-team-form-id[^>]*readonly/);
});

test("teamMemberNames / renderTeamMemberList keep the team's own order without pinned roles", () => {
  assert.deepEqual(teamMemberNames({ roles: [{ roleName: "auditor" }, { roleName: "main" }], orderRoles: ["user", "main", "auditor"] }), ["auditor"]);
  assert.deepEqual(teamMemberNames({ roles: [{ roleName: "b" }, { roleName: "a" }], orderRoles: ["a", "b"] }), ["a", "b"]);
  const list = renderTeamMemberList(["auditor", "tl"], [{ roleName: "tl", roleKind: "techlead" }]);
  assert.match(list, /team-member-idx">1<\/span>[\s\S]*?team-member-label">auditor</);
  assert.match(list, /team-member-label">ADVISOR</);
  assert.match(list, /data-team-member-remove="tl"/);
  assert.match(renderTeamMemberList([], []), /team-member-empty/);
  assert.match(renderTeamMemberList(null, null), /team-member-empty/);
});

// ── 排序纯函数 ────────────────────────────────────────────────

test("agentTeamOrderForDrag moves a member before the drop target", () => {
  // tl 拖到 main 之前：main 与 tl 交换。
  assert.deepEqual(agentTeamOrderForDrag(goalView, "tl", "main"), {
    policy: "goal_loop", orderRoles: ["user", "tl", "main"]
  });
  // 未排入的员工拖进顺序 = 插到落点之前（这里落点是 main）。
  assert.deepEqual(agentTeamOrderForDrag(goalView, "digest", "main"), {
    policy: "goal_loop", orderRoles: ["user", "digest", "main", "tl"]
  });
  // 拖到"顺序末尾"落区（target 空）= 排到最后。
  assert.deepEqual(agentTeamOrderForDrag(goalView, "main", ""), {
    policy: "goal_loop", orderRoles: ["user", "tl", "main"]
  });
  // 非法/无变化：拖到自己、目标不在顺序里、空源。
  assert.equal(agentTeamOrderForDrag(goalView, "tl", "tl"), null);
  assert.equal(agentTeamOrderForDrag(goalView, "tl", "ghost"), null);
  assert.equal(agentTeamOrderForDrag(goalView, "", "main"), null);
});

test("pinned roles cannot be removed from the working order", () => {
  assert.equal(isPinnedRole("user"), true);
  assert.equal(isPinnedRole("main"), true);
  assert.equal(isPinnedRole("tl"), false);
  const html = renderAgentTeam(goalView, presets, library);
  assert.match(html, /data-team-action="remove" data-team-role="user"[^>]*disabled/);
  assert.match(html, /data-team-action="remove" data-team-role="tl"/);
});

test("nextAgentTeamOrder only rewrites the working order (remove / restore)", () => {
  assert.deepEqual(nextAgentTeamOrder(goalView, "remove", "tl"), {
    policy: "goal_loop", orderRoles: ["user", "main"]
  });
  assert.deepEqual(nextAgentTeamOrder(goalView, "restore", "digest"), {
    policy: "goal_loop", orderRoles: ["user", "main", "tl", "digest"]
  });
  // ↑/↓ 随按钮一起移除：拖拽是唯一的调序通道，位置类动作必须被拒。
  assert.equal(nextAgentTeamOrder(goalView, "up", "tl"), null);
  assert.equal(nextAgentTeamOrder(goalView, "down", "tl"), null);
  // 非法动作：pin 角色摘除、重复恢复、不在顺序里的角色摘除。
  assert.equal(nextAgentTeamOrder(goalView, "remove", "main"), null);
  assert.equal(nextAgentTeamOrder(goalView, "remove", "digest"), null);
  assert.equal(nextAgentTeamOrder(goalView, "restore", "tl"), null);
});

// ── 角色会话详情 ──────────────────────────────────────────────

test("role session detail renders the agent identity, meta and its own rows", () => {
  const html = renderRoleSessionDetail({
    main_session_id: "main-1",
    role_name: "tl",
    role_session_id: "goal-a2a-tl",
    join_seq_id: 7,
    order_policy: "goal_loop",
    order_roles: ["user", "main", "tl"],
    floor: { role_name: "tl" },
    role_rows: [{ seq: 3, role: "assistant", content: "verdict: not_done" }],
    draft_rows: [{ event: { seq: 4, role: "assistant", content: "draft row" } }]
  });
  assert.match(html, /role-session-identity">ADVISOR</);
  assert.match(html, /role-session-sid[^>]*>goal-a2a-tl</);
  assert.match(html, /发言中 · tl/);
  assert.match(html, /join_seq 7/);
  assert.match(html, /verdict: not_done/);
  assert.match(html, /draft row/);
});

test("role session detail escapes content and tolerates an empty session", () => {
  const escaped = renderRoleSessionDetail({
    role_name: "tl",
    role_rows: [{ seq: 1, role: "assistant", content: "<img src=x onerror=alert(1)>" }]
  });
  assert.doesNotMatch(escaped, /<img src=x/);
  assert.match(escaped, /&lt;img src=x onerror=alert\(1\)&gt;/);
  const empty = renderRoleSessionDetail({ role_name: "tl", role_rows: [], draft_rows: [] });
  assert.match(empty, /还没有独立会话行/);
});

test("role record table lanes mark main turns outside the teammate prefix match", () => {
  const html = renderRoleSessionDetail({
    role_name: "tl",
    prefix_cut_seq: 5,
    visible_main_rows: 2,
    outside_prefix_main_rows: 5,
    main_rows: [
      { seq: 1, role: "assistant", content: "goal 之前 1" },
      { seq: 5, role: "assistant", content: "goal 之前 5" },
      { seq: 6, role: "assistant", content: "goal 这一回合" },
      { seq: 7, role: "assistant", content: "exec 这一回合" }
    ],
    role_rows: [{ seq: 8, role: "assistant", content: "tl 这一回合" }]
  });
  assert.match(html, /<table class="excel-grid role-record-table" data-role-record-table>/);
  assert.match(html, /role-record-lane-head">车道</);
  assert.match(html, /data-record-cut="5"/);
  assert.match(html, /data-record-outside="5"/);
  assert.match(html, /data-record-visible="2"/);
  // 两条车道各一行，列头 = 回合号
  assert.match(html, /<tr class="role-record-lane is-main" data-lane="main">/);
  assert.match(html, /<tr class="role-record-lane is-own" data-lane="tl">/);
  assert.match(html, /<th>8<\/th>/);
  // 入伙前的两个回合在它那一行只是占位，不冒充它记得的上下文
  assert.match(html, /class="role-record-cell is-outside" data-seq="1"/);
  assert.match(html, /class="role-record-cell is-outside" data-seq="5"/);
  // 占位、不冒充：它那一行的这两列只显示 —（主会话自己那一行仍显示自己的回合）
  assert.doesNotMatch(html, /class="role-record-cell is-shared" data-seq="(?:1|5)"/);
  // 入伙后的共享回合 + 它自己的回合都在
  assert.match(html, /class="role-record-cell is-shared" data-seq="6"[^>]*>goal 这一回合</);
  assert.match(html, /class="role-record-cell is-shared" data-seq="7"[^>]*>exec 这一回合</);
  assert.match(html, /class="role-record-cell is-own" data-seq="8"[^>]*>tl 这一回合</);
  assert.match(html, /前缀匹配自 seq 6 起/);
});

test("main role record table carries the whole session without placeholders", () => {
  const html = renderRoleSessionDetail({
    role_name: "main",
    prefix_cut_seq: 0,
    visible_main_rows: 2,
    outside_prefix_main_rows: 0,
    main_rows: [
      { seq: 1, role: "user", content: "起点这一句" },
      { seq: 2, role: "assistant", content: "当前位置这一句" }
    ]
  });
  assert.doesNotMatch(html, /is-outside/);
  assert.match(html, /该会话整段都在它的记录里/);
  assert.match(html, /class="role-record-cell is-main" data-seq="1"[^>]*>起点这一句</);
  assert.match(html, /class="role-record-cell is-main" data-seq="2"[^>]*>当前位置这一句</);
});

test("role names, notices and prompts are escaped, never interpolated raw", () => {
  const html = renderAgentTeam({
    configured: true,
    order_policy: "user_main_decided",
    order_roles: ["user", "main", "<img src=x onerror=alert(1)>"],
    members: [{ role_name: "<img src=x onerror=alert(1)>", role_kind: "agent", in_order: true, order_index: 2 }],
    scheduled: [],
    design_notice: ["floor.role_name 不在 lifecycle.order_roles"]
  }, presets, {
    teams: [{
      team_id: "evil",
      name: "<script>alert(1)</script>",
      roles: [{ role_name: "x", system_prompt: "<img src=x>" }]
    }]
  });
  assert.doesNotMatch(html, /<img src=x/);
  assert.doesNotMatch(html, /<script>alert\(1\)<\/script>/);
  assert.match(html, /&lt;img src=x onerror=alert\(1\)&gt;/);
  assert.match(html, /floor\.role_name 不在 lifecycle\.order_roles/);
  // 提示词只以"已登记/未登记"chip 出现，正文不回显到列表里。
  const promptPanel = hirePanel(normalizeAgentTeam({
    order_roles: ["user", "main", "x"],
    members: [{ role_name: "x", role_kind: "agent", in_order: true, order_index: 2, system_prompt: "</textarea><script>alert(1)</script>" }]
  }), { roleName: "x", roleKind: "agent", systemPrompt: "</textarea><script>alert(1)</script>", toolsPolicy: "" });
  assert.doesNotMatch(promptPanel, /<script>/);
  assert.match(promptPanel, /&lt;\/textarea&gt;&lt;script&gt;/);
});

// ── 归一化 ────────────────────────────────────────────────────

test("normalizeAgentTeam tolerates malformed payloads", () => {
  assert.deepEqual(normalizeAgentTeam(null), {
    sessionID: "", teamID: "", teamKind: "", orderPolicy: "", orderRoles: [],
    members: [], scheduled: [], configured: false, floorRole: "", schedule: null, designNotice: []
  });
  const partial = normalizeAgentTeam({
    order_roles: ["user", 7, ""],
    members: [{ role_name: "tl" }, { role_name: "" }, null],
    scheduled: "nope"
  });
  assert.deepEqual(partial.orderRoles, ["user"]);
  assert.equal(partial.members.length, 1);
  assert.equal(partial.members[0].roleName, "tl");
  assert.equal(partial.members[0].systemPrompt, "");
  assert.deepEqual(partial.scheduled, []);
  assert.equal(partial.configured, false);
});

test("normalizeTeamLibrary tolerates malformed payloads", () => {
  assert.deepEqual(normalizeTeamLibrary(null), { configured: false, teams: [] });
  const parsed = normalizeTeamLibrary({ configured: true, teams: [{ team_id: "a" }, { name: "no id" }, null] });
  assert.equal(parsed.configured, true);
  assert.equal(parsed.teams.length, 1);
  assert.equal(parsed.teams[0].teamID, "a");
  assert.deepEqual(parsed.teams[0].roles, []);
});

test("toolsPolicyLabel maps registered permissions to short chips", () => {
  assert.equal(toolsPolicyLabel("readonly"), "只读");
  assert.equal(toolsPolicyLabel("readwrite"), "读写");
  assert.equal(toolsPolicyLabel("full"), "完全");
  assert.equal(toolsPolicyLabel(""), "继承");
  assert.equal(toolsPolicyLabel("weird"), "weird");
});

const globalConfig = {
  employees: { configured: true, employees: [{ role_name: "auditor", role_kind: "agent", tools_policy: "readonly" }] },
  order: { configured: true, order_policy: "user_main_decided", order_roles: ["user", "main", "auditor"] },
  composition: {
    session_id: "main-1",
    order_policy: "user_main_decided",
    order_roles: ["user", "main", "auditor"],
    employees: [{ role_name: "auditor", role_kind: "agent", tools_policy: "readonly" }]
  }
};

test("employee library block lists the merged pool and the library write actions", () => {
  const html = renderAgentTeam(goalView, presets, library, globalConfig);
  assert.match(html, /员工库/);
  assert.doesNotMatch(html, /team-rail-hint/);
  assert.match(html, /data-team-employee-new="1"/);
  assert.match(html, /data-team-employee-delete="auditor"/);
  assert.match(html, /data-team-employee-edit="auditor"/);
  // 库里的行有 ≡（能拖进顺序）与"库"来源 chip。
  assert.match(html, /data-team-employee-drag="auditor"[^>]*draggable="true"/);
  assert.match(html, /team-source-chip"[^>]*>库</);
  // 员工库与团队解耦：会话没装配团队，员工库照样在、照样能增删改。
  const unconfigured = renderAgentTeam({ configured: false, members: [], scheduled: [] }, presets, library, globalConfig);
  assert.match(unconfigured, /data-team-employee-edit="auditor"/);
  // 旧宿主不下发员工库时不伪造写按钮（员工栏仍可增删当前会话的在编角色）。
  const withoutMaster = renderAgentTeam(goalView, presets, library);
  assert.doesNotMatch(withoutMaster, /data-team-employee-new/);
  assert.doesNotMatch(withoutMaster, /data-team-employee-edit/);
  // 母本为空也不会是 0 人空壳：本会话在编的 tl 进池（来源标"本会话"）。
  const noMaster = renderAgentTeam(goalView, presets, library, { employees: { employees: [] }, order: {}, composition: { employees: [] } });
  assert.match(noMaster, /team-rail-head[\s\S]*?<span>员工库<\/span>[\s\S]*?<span class="badge">1<\/span>/);
  assert.match(noMaster, /data-team-employee-save="tl"/);
  assert.match(noMaster, /team-source-chip is-session/);
});

test("employeePool merges the global library with the session roster", () => {
  const pool = employeePool(normalizeTeamGlobal(globalConfig), normalizeAgentTeam(goalView));
  assert.deepEqual(pool.map(role => [role.roleName, role.inLibrary, role.inSession]), [["auditor", true, true], ["tl", false, true]]);
  // 同一角色在两份事实里都出现时合并成一行，母本先出。
  const both = employeePool(
    normalizeTeamGlobal({ employees: { employees: [{ role_name: "tl", role_kind: "techlead", tools_policy: "full" }] }, order: {}, composition: { employees: [{ role_name: "tl", role_kind: "techlead" }] } }),
    normalizeAgentTeam(goalView)
  );
  assert.equal(both.length, 1);
  assert.equal(both[0].inLibrary, true);
  assert.equal(both[0].inSession, true);
  assert.equal(both[0].toolsPolicy, "full");
  // user / main / timer 不进员工库清单。
  assert.equal(pool.some(role => role.roleName === "main" || role.roleName === "user" || role.roleName === "digest"), false);
  // 畸形载荷 → 空池（不抛）。
  assert.deepEqual(employeePool(null, null), []);
});

test("employee library marks drift between the session copy and the library", () => {
  const drifted = {
    ...globalConfig,
    composition: { ...globalConfig.composition, employees: [{ role_name: "reviewer", role_kind: "agent" }] }
  };
  const html = renderAgentTeam(goalView, presets, library, drifted);
  assert.match(html, /本会话在编名单与员工库有差异/);
  assert.equal(teamGlobalDrift(normalizeTeamGlobal(drifted)), true);
  assert.equal(teamGlobalDrift(normalizeTeamGlobal({ ...globalConfig, composition: { ...globalConfig.composition, employees: globalConfig.employees.employees } })), false);
});

test("normalizeTeamGlobal tolerates malformed payloads", () => {
  const empty = normalizeTeamGlobal(null);
  assert.deepEqual(empty.employees, []);
  assert.deepEqual(empty.orderRoles, []);
  assert.deepEqual(empty.composition.employees, []);
  assert.equal(empty.employeesConfigured, false);

  const parsed = normalizeTeamGlobal({
    employees: { employees: [{ role_name: "a" }, null, { name: "no role_name" }] },
    order: { order_roles: ["user", "", "main"] },
    composition: { order_roles: "nope" }
  });
  assert.equal(parsed.employees.length, 1);
  assert.equal(parsed.employees[0].roleName, "a");
  assert.deepEqual(parsed.orderRoles, ["user", "main"]);
  assert.deepEqual(parsed.composition.orderRoles, []);
});

test("global master escapes employee names, never interpolates raw", () => {
  const hostile = {
    employees: { employees: [{ role_name: '<img src=x onerror="1">', role_kind: "agent" }] },
    order: { order_roles: ["user", "main"] },
    composition: { order_roles: ["user", "main"], employees: [] }
  };
  const html = renderAgentTeam(goalView, presets, library, hostile);
  assert.doesNotMatch(html, /<img src=x/);
  assert.match(html, /&lt;img src=x/);
});
