import assert from "node:assert/strict";
import test from "node:test";

import {
  employeeFieldRows,
  employeePool,
  hirePanel,
  isPinnedRole,
  nextAgentTeamOrder,
  normalizeAgentTeam,
  normalizePluginNames,
  normalizeTeamGlobal,
  normalizeTeamLibrary,
  pluginsLabel,
  renderAgentTeam,
  renderRoleLiveTools,
  renderRoleSessionDetail,
  renderRoleSessionSwitcher,
  renderTeamMemberList,
  ROLE_SPEC_ALIASES,
  roleDisplayName,
  roleKindLabel,
  TEAM_ROLE_SPEC_FIELDS,
  teamEditorPanel,
  teamEntryFromMembers,
  teamGlobalDrift,
  teamMemberNames,
  teamMemberSpecMap,
  teamRoleSpec,
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
  const html = renderAgentTeam({ configured: false, members: [], scheduled: [] }, null);
  assert.match(html, /当前会话未装配 AgentTeam/);
  // 没有内置形态可"就地装配"了：面板上不出现任何形态按钮，入口只剩「新建团队」与库条目。
  assert.doesNotMatch(html, /data-team-materialize/);
  assert.match(html, /data-team-open-team="1"/);
  // 「存当前会话」这条操作已移除（团队面板的保存取代了它）。
  assert.doesNotMatch(html, /data-team-save-current="1"/);
  assert.doesNotMatch(html, /data-team-form-members/);
});

test("团队库 lists the user's own teams, with no built-in shape rows or chips", () => {
  const html = renderAgentTeam(goalView, library);
  assert.match(html, /team-rail-head[\s\S]*?<span>团队库<\/span>/);
  assert.match(html, /role="table" aria-label="团队库"/);
  assert.match(html, /我的审计队/);
  assert.match(html, /data-team-materialize-team="my-team"/);
  // 点团队名 = 打开这支团队的团队面板（唯一入口，不再另设「编辑」）。
  assert.match(html, /data-team-edit-team="my-team"/);
  assert.match(html, /data-team-delete-team="my-team"/);
  // 内置形态目录已删除：面板上既没有形态 chip 行，也没有任何"按形态装配"的按钮
  //（"存入库"那两条旧动作同样不留）。
  assert.doesNotMatch(html, /team-preset/);
  assert.doesNotMatch(html, /data-team-materialize="/);
  assert.doesNotMatch(html, /data-team-library-template/);
  assert.doesNotMatch(html, /data-team-save-template/);
  // 「默认顺序 vs 本会话」那张 项/值 表已删除：默认顺序不再单独摆一张表。
  assert.doesNotMatch(html, /默认顺序/);
  assert.doesNotMatch(html, /aria-label="发言顺序（全局默认 vs 本会话）"/);
});

test("团队库 head carries no annotation text", () => {
  const html = renderAgentTeam(goalView, library, globalConfig);
  for (const annotation of ["全局·跨会话", "不依赖团队", "3 个内置形态", "拖拽行首手柄", "装配与编排", "尚未装配团队"]) {
    assert.doesNotMatch(html, new RegExp(annotation));
  }
  assert.doesNotMatch(html, /team-rail-hint/);
});

test("员工栏只写本会话：档案编辑只在员工库，不再两处同名按钮写两份事实", () => {
  const html = renderAgentTeam(goalView, library, globalConfig);
  const staffSection = html.slice(html.indexOf('aria-label="员工栏"'), html.indexOf('data-team-hire-slot'));

  // 本会话的两件事还在：摘除（出工作顺序、留角色）与移出本会话（连注册表一起删）。
  assert.match(staffSection, /data-team-action="remove"/);
  assert.match(staffSection, /data-team-delete="tl"/);
  // 档案编辑（提示词 / 权限 / 类型）从员工栏下线：唯一入口是员工库。
  assert.doesNotMatch(staffSection, /data-team-edit="/);
  assert.doesNotMatch(staffSection, />编辑</);
  assert.match(html, /data-team-employee-edit="auditor"/);
  // 回显的 chip 说清它是"本会话在编副本"，不再与员工库同句混读。
  assert.match(staffSection, /title="工具权限（本会话在编副本）"/);
  // 栏头 title 说明各自的事实域（不占版面：见上面 "head carries no annotation text"）。
  assert.match(html, /team-rail-head" title="员工库（全局事实）[^"]*"/);
  assert.match(html, /team-rail-head" title="员工栏（本会话）[^"]*"/);
});

test("员工栏 holds the working order, with no drag handles and no position column", () => {
  const html = renderAgentTeam(goalView, library);
  assert.match(html, /team-rail-head[\s\S]*?<span>员工栏<\/span>/);
  assert.match(html, /role="table" aria-label="员工栏"/);
  // 窄栏只留"员工（权限）/ 操作"两列：没有"位置"列（顺序不由人编排），也没有拖拽条。
  assert.match(html, /role="columnheader">员工（权限）<\/span>/);
  assert.match(html, /role="columnheader">操作<\/span>/);
  assert.match(html, /data-team-staff-role="tl" data-team-order-role="tl"/);
  assert.match(html, /team-member-role" title="逻辑角色名（metadata，不是 provider role）">tl</);
  assert.match(html, /team-perm-chip"[^>]*>只读</);
  // 定时 agent 不在发言顺序里（那一行标"定时"），栏内没有任何编排手势。
  const staffSection = html.slice(html.indexOf('aria-label="员工栏"'), html.indexOf('data-team-hire-slot'));
  assert.match(staffSection, /team-outside-row"[^>]*data-team-staff-role="digest"/);
  assert.match(staffSection, /title="定时 agent 不参与发言顺序">定时</);
  assert.doesNotMatch(staffSection, /data-team-drag/);
  assert.doesNotMatch(staffSection, /draggable/);
  assert.doesNotMatch(staffSection, /team-member-pos/);
  // 冷加载槽位默认空（表单不常驻）。
  assert.match(html, /data-team-hire-slot hidden/);
  assert.doesNotMatch(html, /data-team-hire-form/);
});

test("team members expose distinct agent identities and role-session entry points", () => {
  assert.equal(roleDisplayName("main", "main"), "EXEC");
  assert.equal(roleDisplayName("tl", "techlead"), "ADVISOR");
  assert.equal(roleDisplayName("user", "user"), "USER");
  assert.equal(roleDisplayName("reviewer", "agent"), "reviewer");
  const html = renderAgentTeam(goalView, library);
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
  // 面板骨架与字段数随装配入口 +1（9 个字段一条流水线，编号还在）。
  assert.equal((panels[0].match(/class="team-field-no"/g) || []).length, 9);
  assert.match(panels[0], /data-team-hire-prompt/);
});

test("teamEditorPanel fills from the library entry and closes itself", () => {
  const html = teamEditorPanel(normalizeAgentTeam(goalView), normalizeTeamLibrary(library).teams[0]);
  assert.match(html, /data-team-editor="team"/);
  assert.match(html, /团队 · 我的审计队/);
  assert.match(html, /data-team-editor-close="1"/);
  assert.match(html, /data-team-form-name[^>]*value="我的审计队"/);
  assert.match(html, /data-team-form-id[^>]*value="my-team"[^>]*readonly/);
  // 成员表：行序 = 登记先后（没有拖拽），行上只剩 ✕。
  assert.match(html, /data-team-member-list/);
  assert.match(html, /data-team-member-item="auditor" data-team-member-kind="agent"/);
  assert.doesNotMatch(html, /draggable/);
  assert.match(html, /data-team-member-remove="auditor"/);
  // 顺序策略 / 团队形态（team_kind）/ 门禁 / 压缩都只走隐藏字段：面板不给输入框，
  // 也不给只读展示（要核对时看落盘文件）。
  assert.match(html, /data-team-form-policy[^>]*value="user_main_decided"/);
  assert.match(html, /data-team-form-kind[^>]*value="my-team"/);
  assert.doesNotMatch(html, /<select[^>]*data-team-form-policy/);
  assert.doesNotMatch(html, /data-team-form-policy-label/);
  assert.doesNotMatch(html, /data-team-template/);
  assert.match(html, /data-team-form-fill-current="1"/);
  assert.match(html, /当前会话：tl/);
  // 新建态：空条目、可写团队 ID。
  const fresh = teamEditorPanel(normalizeAgentTeam(goalView), null);
  assert.match(fresh, />新建团队</);
  assert.doesNotMatch(fresh, /data-team-form-id[^>]*readonly/);
});

test("teamMemberNames / renderTeamMemberList keep the team's own order without pinned roles", () => {
  assert.deepEqual(teamMemberNames({ roles: [{ roleName: "auditor" }, { roleName: "main" }], orderRoles: ["user", "main", "auditor"] }), ["auditor"]);
  assert.deepEqual(teamMemberNames({ roles: [{ roleName: "b" }, { roleName: "a" }], orderRoles: ["a", "b"] }), ["a", "b"]);
  const list = renderTeamMemberList(["auditor", "tl"], [{ roleName: "tl", roleKind: "techlead" }]);
  // 次序由数组本身表达（登记先后）：行上不再有位置序号，生态位 chip 只在登记过时出现。
  assert.doesNotMatch(list, /team-member-idx/);
  assert.match(list, /data-team-member-item="auditor" data-team-member-kind=""/);
  assert.match(list, /data-team-member-item="tl" data-team-member-kind="techlead"/);
  assert.match(list, /team-member-label">ADVISOR</);
  assert.match(list, /class="chip team-member-kind"[^>]*>TL</);
  assert.match(list, /data-team-member-remove="tl"/);
  assert.match(renderTeamMemberList([], []), /team-member-empty/);
  assert.match(renderTeamMemberList(null, null), /team-member-empty/);
});

// ── 团队成员的 RoleSpec：生态位（条目）/ 人的档案（员工库·本会话）────────
//
// 回归背景（真事）：团队面板只搬 role_name/role_kind/tools_policy/system_prompt，
// 保存一支 goal-a2a 就把 tl 的 techlead 规格静默降级成 agent —— 角色的生态位这份事实
// 当场丢了。下面这几条把"不许再丢字段"钉住。

// 一支条目的生态位事实：形态目录删掉之后，条目自己就是生态位的唯一来源
//（没有"哪个内置形态规定 tl 必须 techlead"这回事了）。
const teamEntryWithTL = {
  team_id: "goal-a2a",
  team_kind: "goal-a2a",
  order_policy: "goal_loop",
  gate_policy: "goal_finish_gate",
  compact_policy: "main_authoritative",
  roles: [{
    role_name: "tl",
    role_kind: "techlead",
    join_policy: "on_goal_create",
    presence_policy: "online_when_goal_active",
    tools_policy: "readonly",
    directive_schema: ["verdict_done", "verdict_not_done", "escalate_human"]
  }]
};

test("teamMemberSpecMap：生态位取自条目，人的档案取自员工库 / 本会话", () => {
  const pool = [
    { roleName: "tl", roleKind: "agent", toolsPolicy: "readwrite", permissionGroups: { ro: 4, rw: 2 }, systemPrompt: "你是技术负责人" },
    { roleName: "worker", roleKind: "agent", toolsPolicy: "readwrite" }
  ];
  const specs = teamMemberSpecMap(["tl", "worker"], { entry: teamEntryWithTL, pool });
  // 条目录了 tl 的生态位：techlead + goal 上线入顺序 + verdict 指令集。
  assert.equal(specs.tl.role_kind, "techlead");
  assert.equal(specs.tl.join_policy, "on_goal_create");
  assert.equal(specs.tl.presence_policy, "online_when_goal_active");
  assert.deepEqual(specs.tl.directive_schema, ["verdict_done", "verdict_not_done", "escalate_human"]);
  // 人的档案以员工库那一份为准：权限是用户给这个人登记的，不被条目的 readonly 覆盖。
  assert.equal(specs.tl.tools_policy, "readwrite");
  assert.deepEqual(specs.tl.permission_groups, { ro: 4, rw: 2 });
  assert.equal(specs.tl.system_prompt, "你是技术负责人");
  // 条目里没有的角色整体走员工库那一份，并且不编造没登记的字段。
  assert.equal(specs.worker.role_kind, "agent");
  assert.equal(specs.worker.join_policy, undefined);
});

test("teamMemberSpecMap：条目登记的生态位不被员工库那一份盖掉（反之也不编造）", () => {
  // 条目怎么写，装配后的生态位就怎么传递——形态目录删掉之后没有第二份"权威生态位"，
  // 所以条目里的 role_kind 必须原样进草稿（这是"tl 被降级成 agent"那条事故的守门人：
  // 面板不许把条目已登记的 techlead 洗成员工库里的 agent）。
  const entry = {
    team_id: "goal-a2a",
    team_kind: "goal-a2a",
    order_roles: ["user", "main", "tl"],
    roles: [{ role_name: "tl", role_kind: "techlead", join_policy: "on_goal_create", tools_policy: "readwrite" }]
  };
  const specs = teamMemberSpecMap(["tl"], {
    entry,
    pool: [{ roleName: "tl", roleKind: "agent", toolsPolicy: "full" }]
  });
  assert.equal(specs.tl.role_kind, "techlead");
  assert.equal(specs.tl.join_policy, "on_goal_create");
  // 人的档案（权限档）照旧以员工库那一份为准：条目的 readwrite 被 full 覆盖。
  assert.equal(specs.tl.tools_policy, "full");
});

test("teamEntryFromMembers 带上整份 RoleSpec 与形态级策略（丢一个字段就是丢一份生态位）", () => {
  const entry = teamEntryFromMembers({
    teamID: "goal-a2a",
    teamKind: "goal-a2a",
    name: "goal-a2a",
    orderPolicy: "goal_loop",
    gatePolicy: "goal_finish_gate",
    compactPolicy: "main_authoritative",
    members: [
      { roleName: "tl", spec: { role_kind: "techlead", join_policy: "on_goal_create", tools_policy: "readwrite", directive_schema: ["verdict_done"] } },
      { roleName: "worker", spec: { role_kind: "agent", tools_policy: "readwrite", system_prompt: "干活" } }
    ]
  });
  assert.equal(entry.team_id, "goal-a2a");
  assert.deepEqual(entry.order_roles, ["user", "main", "tl", "worker"]);
  assert.deepEqual(entry.roles[0], {
    role_name: "tl",
    role_kind: "techlead",
    join_policy: "on_goal_create",
    tools_policy: "readwrite",
    directive_schema: ["verdict_done"]
  });
  assert.deepEqual(entry.roles[1], { role_name: "worker", role_kind: "agent", tools_policy: "readwrite", system_prompt: "干活" });
  assert.equal(entry.gate_policy, "goal_finish_gate");
  assert.equal(entry.compact_policy, "main_authoritative");
});

test("teamRoleSpec 只收登记过的值：空 / 空数组不算事实", () => {
  assert.deepEqual(teamRoleSpec({ role_kind: "agent", join_policy: "", directive_schema: [], order_priority: 0, presence_policy: "online" }), { role_kind: "agent", presence_policy: "online" });
  assert.deepEqual(teamRoleSpec(null), {});
});

test("roleKindLabel 未知生态位原样显示，不折成 agent", () => {
  assert.equal(roleKindLabel("techlead"), "TL");
  assert.equal(roleKindLabel("agent"), "agent");
  assert.equal(roleKindLabel("leader"), "leader");
  assert.equal(roleKindLabel(""), "");
});

test("团队编辑器把成员生态位与条目的隐藏字段带进草稿", () => {
  const html = teamEditorPanel(normalizeAgentTeam(goalView), normalizeTeamLibrary(library).teams[0]);
  // 成员行带生态位（可见 chip + 可回读的载荷），行序 = 登记先后。
  assert.match(html, /data-team-member-kind="agent"/);
  assert.match(html, /class="chip team-member-kind"[^>]*>agent</);
  assert.match(html, /data-team-member-spec="[^"]*&quot;role_kind&quot;:&quot;agent&quot;/);
  // 条目上的既有字段进表单（没有编辑入口，但保存时要原样送回后端）。
  assert.match(html, /data-team-form-gate/);
  assert.match(html, /data-team-form-compact/);
  assert.match(html, /data-team-form-kind/);
  // 面板不给 team_kind / 门禁 / 压缩 的任何可见展示（旧的回显小字已删）。
  assert.doesNotMatch(html, /data-team-form-shape/);
});

test("团队库行只说规模、成员名进 title（不再承诺写死的班底，也不用「循环」说话）", () => {
  const html = renderAgentTeam(goalView, library);
  assert.doesNotMatch(html, /固定循环/);
  assert.match(html, /class="team-library-meta" title="成员：auditor">1 人</);
  // 顺序策略在面板上不再展示（历史字段：只回读、不驱动轮次）。
  assert.doesNotMatch(html, /实际顺序：user → main → auditor/);
  assert.doesNotMatch(html, /由 user \/ main 编排/);
});

test("顺序策略是历史字段：面板既不显示也不给入口", () => {
  const html = renderAgentTeam(goalView, library);
  // 不再有可编辑入口：一个改了不驱动任何轮次的旋钮比没有旋钮更容易误导。
  assert.doesNotMatch(html, /<select[^>]*data-team-policy/);
  // 连只读展示也不给：取值仍在会话 lifecycle 里（要核对时看落盘文件或 TUI），
  // 面板不复述它，也不再用"固定座次/循环"这类措辞。
  assert.doesNotMatch(html, /data-team-policy/);
  assert.doesNotMatch(html, /team-policy-static/);
  assert.doesNotMatch(html, /固定座次/);
  assert.doesNotMatch(html, /顺序策略 · 历史字段/);
});

test("发言调度的收束文案不再说「环」：顺序里没有执行者 / 发言顺序为空", () => {
  const stopped = {
    ...goalView,
    schedule: { order: ["tl"], next_role: "", stopped: true, stop_reason: "no_executor", no_automatic_turn: ["tl"] }
  };
  const html = renderAgentTeam(stopped, library);
  assert.match(html, /顺序里没有执行者/);
  assert.doesNotMatch(html, /环内无执行者/);
  assert.doesNotMatch(html, /空环/);
});

// ── 排序纯函数 ────────────────────────────────────────────────

test("pinned roles cannot be removed from the working order", () => {
  assert.equal(isPinnedRole("user"), true);
  assert.equal(isPinnedRole("main"), true);
  assert.equal(isPinnedRole("tl"), false);
  const html = renderAgentTeam(goalView, library);
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
  // 位置类动作一律被拒：面板不提供任何调序通道（顺序归 leader 的 team plan）。
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
  // 未同步草稿不再混进已发布行：它带 is-draft 标记与待同步文案（独立成区）。
  assert.match(html, /class="role-session-row is-draft"[^>]*data-draft-kind=""/);
  assert.match(html, /class="chip is-draft">待同步</);
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

test("role record table is vertical: one round per row, lane per column", () => {
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
  // 竖排：回合当行、车道当列——不再有 excel-grid 横向网格，也不再有"车道行"。
  assert.match(html, /<table class="role-record-table" data-role-record-table>/);
  assert.doesNotMatch(html, /excel-grid/);
  assert.doesNotMatch(html, /role-record-lane[^\-]/);
  assert.match(html, /<th class="role-record-seq" scope="col">回合<\/th>/);
  assert.match(html, /<th class="role-record-lane-head" scope="col">main<\/th>/);
  assert.match(html, /<th class="role-record-lane-head is-own" scope="col" title="ADVISOR">tl<\/th>/);
  assert.match(html, /data-record-cut="5"/);
  assert.match(html, /data-record-outside="5"/);
  assert.match(html, /data-record-visible="2"/);
  // 一条回合一行：行号 = seq，时间自上而下（6 在 7 前，7 在 8 前）。
  assert.match(html, /<tr class="role-record-round" data-seq="1">\s*<th class="role-record-seq" scope="row">1<\/th>/);
  assert.match(html, /<tr class="role-record-round" data-seq="8">/);
  assert.ok(html.indexOf('data-seq="6"') < html.indexOf('data-seq="7"'));
  assert.ok(html.indexOf('data-seq="7"') < html.indexOf('data-seq="8"'));
  // 入伙前的两个回合在它那一栏只是占位，不冒充它记得的上下文。
  assert.match(html, /class="role-record-cell is-outside" data-seq="1"/);
  assert.match(html, /class="role-record-cell is-outside" data-seq="5"/);
  assert.doesNotMatch(html, /class="role-record-cell is-shared" data-seq="(?:1|5)"/);
  // 入伙后的共享回合 + 它自己的回合都在；它自己那一回合 main 栏是空位。
  assert.match(html, /class="role-record-cell is-shared" data-seq="6"[^>]*>goal 这一回合</);
  assert.match(html, /class="role-record-cell is-shared" data-seq="7"[^>]*>exec 这一回合</);
  assert.match(html, /class="role-record-cell is-own" data-seq="8"[^>]*>tl 这一回合</);
  assert.match(html, /class="role-record-cell is-empty" data-seq="8"/);
  assert.match(html, /前缀匹配自 seq 6 起/);
});

test("role session detail renders the teammate's live tool activity (实时区)", () => {
  // 数据来源是 teammate.tool.* 事件的载荷（不进快照的瞬态）：一条一步、最新在下。
  const live = [
    { id: "read_file#1#0a1b2c3d", name: "read_file", status: "success", result: "package a" },
    { id: "bash_git_status#2#0f0f0f0f", name: "bash", status: "running", arguments: "git status --short" }
  ];
  const html = renderRoleSessionDetail(
    { role_name: "tl", role_rows: [{ seq: 8, role: "assistant", content: "已发布的裁决" }] },
    { roleName: "tl", roleSessionID: "goal-a2a-tl", liveTools: live }
  );
  assert.match(html, /class="team-block role-live-tools" data-role-live-tools="2"/);
  assert.match(html, /正在做（实时）<\/span><span class="badge">2<\/span>/);
  assert.match(html, /data-live-id="bash_git_status#2#0f0f0f0f"/);
  // 状态未知时原样显示，不折成 running（后端加新状态时前端不该把事实说错）。
  assert.match(html, /role-live-status is-running">进行中</);
  assert.match(html, /role-live-status is-success">完成</);
  assert.ok(html.indexOf("read_file#1#0a1b2c3d") < html.indexOf("bash_git_status#2#0f0f0f0f"));
  // 实时区排在记录表之前：先看它此刻在动什么，再看它的记录。
  assert.ok(html.indexOf("role-live-tools") < html.indexOf("role-record-table"));
  // 没有活动 → 不留空壳（这是"没有正在做的事"，不是"加载失败"）。
  assert.equal(renderRoleLiveTools([]), "");
  assert.equal(renderRoleLiveTools(null), "");
  assert.doesNotMatch(renderRoleSessionDetail({ role_name: "tl", role_rows: [] }), /role-live-tools/);
});

test("live tool activity is escaped like any other payload", () => {
  const html = renderRoleLiveTools([{ id: "<b>1</b>", name: "<img src=x>", status: "<svg>", result: "<script>alert(1)</script>" }]);
  assert.doesNotMatch(html, /<img src=x/);
  assert.doesNotMatch(html, /<script>alert\(1\)/);
  assert.match(html, /&lt;img src=x&gt;/);
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
  }, {
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

// ── 未同步草稿（生命周期：本轮结束才同步）──────────────────────
//
// 形状取自后端真实生产者：application/core/goal_team_recorder.go:RecordTLRound
// 一次 AppendRoleDraft 落两行（role_context 上下文 + tl_directive 裁决），
// sessionstore/role_session.go:readRoleSnapshot 把它们投影成 draft_rows。
// 关键：草稿行**还没有发布 seq**（seq 由 sequencer 在 sync 时分配），它的身份是
// round_id / role_name / unit_seq / kind——所以表格列不能拿 seq 当键。
function unsyncedDraftSnapshot() {
  return {
    main_session_id: "main-1",
    role_name: "tl",
    role_session_id: "goal-a2a-tl",
    join_seq_id: 7,
    role_rows: [{ seq: 8, role: "assistant", content: "已发布的裁决" }],
    draft_rows: [
      {
        role_name: "tl", role_session_id: "goal-a2a-tl", unit_seq: 1,
        event: {
          kind: "role_context", role: "system", content: "本轮送给 ADVISOR 的原文",
          role_name: "tl", role_session_id: "goal-a2a-tl", unit_seq: 1
        }
      },
      {
        role_name: "tl", role_session_id: "goal-a2a-tl", unit_seq: 2,
        event: {
          kind: "tl_directive", role: "assistant", content: "本轮裁决原文",
          role_name: "tl", role_session_id: "goal-a2a-tl", unit_seq: 2
        }
      }
    ]
  };
}

test("unsynced role drafts get their own region with a count badge and pending wording", () => {
  const html = renderRoleSessionDetail(unsyncedDraftSnapshot());
  // 草稿独立成区：带条数徽标与「待同步」口径，不混进已发布行区域。
  assert.match(html, /class="team-block role-session-drafts" data-role-drafts="2"/);
  assert.match(html, /未同步草稿<\/span><span class="badge">2<\/span>/);
  assert.match(html, /待同步/);
  // 每条草稿一行，is-draft + 索引 + 类别可区分（草稿还没有 seq）。
  assert.equal((html.match(/class="role-session-row is-draft"/g) || []).length, 2);
  assert.match(html, /data-draft-index="1"[^>]*data-draft-kind="role_context"/);
  assert.match(html, /data-draft-index="2"[^>]*data-draft-kind="tl_directive"/);
  assert.match(html, /本轮送给 ADVISOR 的原文/);
  assert.match(html, /本轮裁决原文/);
  // 既定的 message 行仍是普通行：is-draft 只出现在草稿行上。
  assert.match(html, /<article class="role-session-row">/);
  assert.equal((html.match(/role-session-row is-draft/g) || []).length, 2);
});

test("role record table keeps draft rows but marks them is-draft, never as seq 0", () => {
  const html = renderRoleSessionDetail(unsyncedDraftSnapshot());
  // 行号：已发布回合用 seq，草稿行用「草稿N」——草稿没有 seq，不能冒充第 0 回合。
  assert.match(html, /<th class="role-record-seq is-draft" scope="row"[^>]*>草稿1<\/th>/);
  assert.match(html, /<th class="role-record-seq is-draft" scope="row"[^>]*>草稿2<\/th>/);
  assert.doesNotMatch(html, /data-seq="0"/);
  assert.match(html, /data-record-own="1"/);
  assert.match(html, /data-record-draft="2"/);
  // 两条草稿各自一行，不因 seq 相同互相吞掉（旧实现按 seq 建 map → 只剩最后一条）。
  assert.equal((html.match(/class="role-record-cell is-own is-draft"/g) || []).length, 2);
  assert.match(html, /class="role-record-cell is-own is-draft" data-draft="1"[^>]*title="[^"]*未同步草稿[^"]*"[^>]*>本轮送给 ADVISOR 的原文</);
  assert.match(html, /class="role-record-cell is-own is-draft" data-draft="2"[^>]*>本轮裁决原文</);
  // main 车道在草稿行只是空位，不冒充它记得的上下文。
  assert.match(html, /class="role-record-cell is-empty" data-draft="1"[^>]*>·</);
  // 草稿行排在已发布回合行之后（时间自上而下 = 最后发生的最下面）。
  assert.ok(html.indexOf('data-seq="8"') < html.indexOf('data-draft="1"'));
});

test("draft content, kind and identity are escaped like any other row", () => {
  const html = renderRoleSessionDetail({
    role_name: "tl",
    draft_rows: [{
      role_name: "<b>tl</b>", unit_seq: 1,
      event: { kind: "<svg onload=alert(1)>", role: "system", content: "<img src=x onerror=alert(1)>" }
    }]
  });
  assert.doesNotMatch(html, /<img src=x/);
  assert.doesNotMatch(html, /<svg onload/);
  assert.match(html, /&lt;img src=x onerror=alert\(1\)&gt;/);
  assert.match(html, /&lt;svg onload=alert\(1\)&gt;/);
});

test("a synced round leaves no draft region (本轮完成后草稿收敛)", () => {
  const html = renderRoleSessionDetail({
    role_name: "tl",
    role_rows: [{ seq: 8, role: "assistant", content: "已发布的裁决" }],
    draft_rows: []
  });
  assert.doesNotMatch(html, /role-session-drafts/);
  assert.doesNotMatch(html, /is-draft/);
  assert.match(html, /class="role-record-cell is-own" data-seq="8"[^>]*>已发布的裁决</);
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
  const html = renderAgentTeam(goalView, library, globalConfig);
  assert.match(html, /员工库/);
  assert.doesNotMatch(html, /team-rail-hint/);
  assert.match(html, /data-team-employee-new="1"/);
  assert.match(html, /data-team-employee-delete="auditor"/);
  assert.match(html, /data-team-employee-edit="auditor"/);
  // 库里的行有"库"来源 chip；拖拽手柄已删（面板不提供任何编排手势）。
  assert.match(html, /team-source-chip"[^>]*>库</);
  assert.doesNotMatch(html, /data-team-employee-drag/);
  assert.doesNotMatch(html, /draggable/);
  // 只在本会话没在编的库行给「入职」（接住原先"从员工库拖到员工栏"那条手势）；
  // 已在编的行不给，避免同名重复装配。
  const libraryOnly = {
    ...globalConfig,
    employees: { configured: true, employees: [...globalConfig.employees.employees, { role_name: "reviewer", role_kind: "agent" }] }
  };
  const withOnlyLibrary = renderAgentTeam(goalView, library, libraryOnly);
  assert.match(withOnlyLibrary, /data-team-employee-hire="reviewer"/);
  assert.doesNotMatch(withOnlyLibrary, /data-team-employee-hire="auditor"/);
  // 员工库与团队解耦：会话没装配团队，员工库照样在、照样能增删改。
  const unconfigured = renderAgentTeam({ configured: false, members: [], scheduled: [] }, library, globalConfig);
  assert.match(unconfigured, /data-team-employee-edit="auditor"/);
  // 旧宿主不下发员工库时不伪造写按钮（员工栏仍可增删当前会话的在编角色）。
  const withoutMaster = renderAgentTeam(goalView, library);
  assert.doesNotMatch(withoutMaster, /data-team-employee-new/);
  assert.doesNotMatch(withoutMaster, /data-team-employee-edit/);
  // 母本为空也不会是 0 人空壳：本会话在编的 tl 进池（来源标"本会话"）。
  const noMaster = renderAgentTeam(goalView, library, { employees: { employees: [] }, order: {}, composition: { employees: [] } });
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
  const html = renderAgentTeam(goalView, library, drifted);
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
  const html = renderAgentTeam(goalView, library, hostile);
  assert.doesNotMatch(html, /<img src=x/);
  assert.match(html, /&lt;img src=x/);
});

// ── E：员工运行详情（刷新键 + 切员工）与员工行 k→v 表格化 ─────────────
//
// 用户口径：Agent Team 没有心跳（不搞"最后心跳时间"），员工侧的两件事靠
// **事件驱动 + 手动刷新键**——面板一枚刷新键、运行详情视图一枚刷新键；员工行
// 不再只回显 chip，而是能展开一张 k→v 全字段表；运行详情里能直接「切员工」。

test("员工行表格化 k→v：全字段一行一栏，空栏照列（不藏起来）", () => {
  const rows = employeeFieldRows({ roleName: "tl", roleKind: "techlead", toolsPolicy: "readonly", systemPrompt: "你是评审官" });
  const byKey = Object.fromEntries(rows.map(row => [row.key, row.value]));
  assert.equal(byKey.role_kind, "TL");
  assert.equal(byKey.tools_policy, "只读");
  assert.equal(byKey.permission_groups, "未装配");
  // 装配（插件能力轴）也进 k→v：没登记就照列"继承宿主"，不藏起来。
  assert.equal(byKey.plugins, "继承宿主");
  assert.equal(byKey.presence_policy, "继承");
  assert.equal(byKey.system_prompt, "5 字符");
  // 空载荷也不抛：全字段仍逐栏列出（值退化成"继承/未登记"）。
  assert.equal(employeeFieldRows(null).length, 8);

  const html = renderAgentTeam(goalView, library, globalConfig);
  assert.match(html, /data-team-employee-kv="auditor"/);
  assert.match(html, /data-team-employee-kv="tl"/);
  assert.match(html, /data-team-kv="tools_policy"/);
  assert.match(html, /data-team-kv="plugins"/);
  assert.match(html, /data-team-kv="system_prompt"/);
});

// ── 按会话插件装配（能力轴）：档案回读 + 编辑回填 + 保存保真 ─────────────
//
// 回归背景（真事）：字段表 / 别名表里没有 plugins，于是「保存团队 / 修改员工」这一趟
// 就把已登记的装配静默清空——dto.TeamMember.Plugins 与 dto.RoleSpec.Plugins 的注释
// 把理由写死了："前端要回填原值，否则一次编辑就会把装配清空"。下面几条把它钉住。

test("档案面回读「装配」：插件名照列，空 = 继承宿主（不伪造成空清单）", () => {
  const view = {
    ...goalView,
    members: goalView.members.map(member => member.role_name === "tl"
      ? { ...member, plugins: ["repo-guard", "doc-tools"] }
      : member)
  };
  const master = {
    ...globalConfig,
    employees: {
      configured: true,
      employees: [
        { role_name: "auditor", role_kind: "agent", tools_policy: "readonly", plugins: ["doc-tools"] },
        { role_name: "plain", role_kind: "agent" }
      ]
    }
  };
  const html = renderAgentTeam(view, library, master);
  // 本会话在编那一份回读得到
  assert.match(html, /data-team-employee-kv="tl"[\s\S]*?data-team-kv="plugins"[\s\S]*?repo-guard、doc-tools/);
  // 员工库那一份也随行合并（合并时丢装配 = 编辑面板回填不到原值）
  assert.match(html, /data-team-employee-kv="auditor"[\s\S]*?data-team-kv="plugins"[\s\S]*?doc-tools/);
  // 没登记的行写「继承宿主」——空本身是一条事实。
  assert.match(html, /data-team-employee-kv="plain"[\s\S]*?data-team-kv="plugins"[\s\S]*?>继承宿主</);
  assert.equal(pluginsLabel(["repo-guard", "doc-tools"]), "repo-guard、doc-tools");
  assert.equal(pluginsLabel([]), "继承宿主");
});

test("保存映射保真装配：teamRoleSpec / teamMemberSpecMap / teamEntryFromMembers 都不丢 plugins", () => {
  // 条目角色（协议拼法）与员工库那一份（归一化 camelCase）都带装配 → 草稿两处都在
  const entry = {
    team_id: "goal-a2a",
    team_kind: "goal-a2a",
    order_roles: ["user", "main", "tl"],
    roles: [{ role_name: "tl", role_kind: "techlead", plugins: ["repo-guard"] }]
  };
  const specs = teamMemberSpecMap(["tl", "worker"], {
    entry,
    pool: [{ roleName: "worker", roleKind: "agent", plugins: ["doc-tools", "doc-tools"] }]
  });
  assert.deepEqual(specs.tl.plugins, ["repo-guard"]);
  assert.deepEqual(specs.worker.plugins, ["doc-tools"]);

  // 团队库条目载荷按协议拼法写回：保存团队这一趟不丢装配
  const payload = teamEntryFromMembers({
    teamID: "goal-a2a",
    members: [{ roleName: "tl", spec: { role_kind: "techlead", plugins: ["repo-guard", "doc-tools"] } }]
  });
  assert.deepEqual(payload.roles[0].plugins, ["repo-guard", "doc-tools"]);
  assert.deepEqual(teamRoleSpec({ plugins: ["repo-guard"] }), { plugins: ["repo-guard"] });
});

test("两拼法一致：字段表 ↔ 别名表一一对应，plugins 与 permission_groups 同位置", () => {
  assert.ok(TEAM_ROLE_SPEC_FIELDS.includes("plugins"));
  for (const field of TEAM_ROLE_SPEC_FIELDS) {
    assert.equal(typeof ROLE_SPEC_ALIASES[field], "string", `${field} 没登记别名`);
  }
  assert.equal(ROLE_SPEC_ALIASES.permission_groups, "permissionGroups");
  // plugins 的两种拼法同形（json 名就是 plugins）：登记在表里，与 permission_groups 同一处口径。
  assert.equal(ROLE_SPEC_ALIASES.plugins, "plugins");
  // 同一份事实的两种来源 → 同一份输出（一律协议拼法 snake_case）
  const snake = teamRoleSpec({ role_name: "tl", permission_groups: { ro: 4 }, plugins: ["repo-guard"] });
  const camel = teamRoleSpec({ roleName: "tl", permissionGroups: { ro: 4 }, plugins: ["repo-guard"] });
  assert.deepEqual(snake, { permission_groups: { ro: 4 }, plugins: ["repo-guard"] });
  assert.deepEqual(snake, camel);
});

test("空装配不落盘：空 = 不覆盖（不伪造成「装配了零个」）", () => {
  // 归一：空字符串 / 空数组 / 纯空白项 → null（= 不覆盖）
  assert.equal(normalizePluginNames(""), null);
  assert.equal(normalizePluginNames([]), null);
  assert.equal(normalizePluginNames(" , ，、 "), null);
  assert.equal(normalizePluginNames(undefined), null);
  assert.equal(normalizePluginNames({ nope: 1 }), null);
  // 规整：trim / 丢空 / 保序去重；表单文本（逗号 / 空格 / 顿号）与协议数组同一条路
  assert.deepEqual(normalizePluginNames(" repo-guard ,, doc-tools  repo-guard "), ["repo-guard", "doc-tools"]);
  assert.deepEqual(normalizePluginNames(["  doc-tools ", "", "repo-guard"]), ["doc-tools", "repo-guard"]);
  // 落盘：空清单不进条目（键都不出现，不是一个空数组）
  assert.deepEqual(teamRoleSpec({ plugins: [] }), {});
  assert.deepEqual(teamRoleSpec({ plugins: ["", "  "] }), {});
  const payload = teamEntryFromMembers({
    teamID: "t1",
    members: [{ roleName: "worker", spec: { role_kind: "agent", plugins: [] } }]
  });
  assert.equal("plugins" in payload.roles[0], false);
  // 回读归一：空 = null（编辑面板据此留空文本框）
  assert.equal(normalizeAgentTeam({
    order_roles: ["user", "main", "w"],
    members: [{ role_name: "w", role_kind: "agent", plugins: [] }]
  }).members.find(m => m.roleName === "w").plugins, null);
});

test("员工面板给装配编辑入口：回填原值 + 上限提示 + 留空即不覆盖", () => {
  const team = normalizeAgentTeam(goalView);
  const member = team.members.find(item => item.roleName === "tl");
  const filled = hirePanel(team, { ...member, plugins: ["repo-guard", "doc-tools"] });
  assert.match(filled, /name="plugins" data-team-hire-plugins/);
  assert.match(filled, /data-team-hire-plugins[^>]*value="repo-guard, doc-tools"/);
  // 上限提示：读不到生效值就不复述写死的数字，可见标签只留口径，配置键挂在 title 上
  assert.match(filled, /装配（上限以配置为准）/);
  assert.doesNotMatch(filled, /装配（上限 3 个）/);
  assert.match(filled, /data-team-hire-plugins[^>]*title="[^"]*limits\.plugins\.per_teammate/);
  assert.match(filled, /data-team-hire-plugins[^>]*title="[^"]*留空 = 不覆盖/);
  // 没登记装配时文本框是空的（空 = 不提交该键，不是"提交空数组"）
  const empty = hirePanel(team, member);
  assert.match(empty, /data-team-hire-plugins[^>]*value=""/);
  // 上限可注入（配置面可调），只改提示文案
  assert.match(hirePanel(team, member, "session", { pluginLimit: 5 }), /装配（上限 5 个）/);
  // 面板不摆小字备注的老口径不破：说明只在 title 里（无 team-field-hint）
  assert.doesNotMatch(filled, /team-field-hint/);
});

test("Agent Team 面板带常驻手动刷新键（无心跳，事件驱动之外的兜底）", () => {
  const html = renderAgentTeam(goalView, library);
  assert.match(html, /class="team-panel-toolbar"/);
  assert.match(html, /data-team-refresh="1"/);
  // 未装配的会话也有刷新键：面板数据按需 RPC 拉，刷新不分装配与否。
  const unconfigured = renderAgentTeam({ configured: false, members: [], scheduled: [] }, library);
  assert.match(unconfigured, /data-team-refresh="1"/);
});

test("员工运行详情带刷新键，并把身份带在键上（刷新原样重放）", () => {
  const html = renderRoleSessionDetail({ role_name: "tl", role_session_id: "goal-a2a-tl", role_rows: [] }, { roleName: "tl", roleSessionID: "goal-a2a-tl" });
  assert.match(html, /class="role-session-toolbar"/);
  assert.match(html, /data-role-session-refresh="1"[^>]*data-role-session-role="tl"[^>]*data-role-session-sid="goal-a2a-tl"/);
});

test("运行详情「切员工」：给多员工名单摆一排 chip，当前位高亮", () => {
  const members = [
    { roleName: "tl", roleKind: "techlead", roleSessionID: "goal-a2a-tl" },
    { roleName: "auditor", roleKind: "agent", roleSessionID: "role-auditor" }
  ];
  const bar = renderRoleSessionSwitcher(members, "tl");
  assert.match(bar, /role-session-switcher/);
  assert.match(bar, /data-role-session-switch="tl"[^>]*aria-current="true"/);
  assert.match(bar, /data-role-session-switch="auditor"[^>]*data-role-session-switch-sid="role-auditor"/);
  // 只有一位（或没有名单）不摆一条只有一个 chip 的条。
  assert.equal(renderRoleSessionSwitcher([members[0]], "tl"), "");
  assert.equal(renderRoleSessionSwitcher(null, "tl"), "");

  const html = renderRoleSessionDetail({ role_name: "tl", role_rows: [] }, { roleName: "tl", roleSessionID: "goal-a2a-tl", members });
  assert.match(html, /data-role-session-switch="auditor"/);
});
