import assert from "node:assert/strict";
import test from "node:test";

// 角色会话只读观察面（原 agent-team-view.test.mjs 里与 Agent Team 面板无关的那一半）。
//
// 2026-10-12 J-1a：Agent Team 面板本体退场（`agent-team-view.js` 已删），但「看这位
// 员工此刻那件事的会话」这条读面不属于面板——它的入口在团队看板（`team-board-view.js`
// 写 `data-team-role-open` 钩子，`app.js` 绑委托），因此随渲染件一起搬到这里，用例原样
// 保留（一条都没删：草稿区、竖排记录表、实时工具区、刷新键、切员工都在）。
import {
  renderRoleLiveTools,
  renderRoleSessionDetail,
  renderRoleSessionSwitcher
} from "./role-session-view.js";

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

// ── 刷新键与切员工（接线口径）────────────────────────────────

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
