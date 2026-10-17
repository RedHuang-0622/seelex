package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// goalteam_test.go — 目标/团队只读面板（Alt+G / Alt+T）的验收：
//
//   - 目标面板读 Snapshot.Runtime.GoalGovernance（同源投影），无活跃 goal 时给
//     上线入口提示而不是空白；
//   - 团队面板走一次 tea.Cmd 异步读服务层读面（*application.Service.AgentTeamView
//     同签名），读面缺失 / 后端报错 / 未装配团队都有明确文案；
//   - 面板高度与渲染行数一致（convHeight 依赖它，错一行就撑破 viewport）；
//   - 面板只读：不提交输入、不改后端状态；有待批选择时不打开（键义不打架）。

// teamFakeApp 在 fakeApp 之上补团队读面（*application.Service.AgentTeamView 同形）。
type teamFakeApp struct {
	*fakeApp
	view      dto.TeamView
	err       error
	sessionID string
}

func (app *teamFakeApp) AgentTeamView(mainSessionID string) (dto.TeamView, error) {
	app.sessionID = mainSessionID
	return app.view, app.err
}

func goalSnapshot() application.Snapshot {
	snapshot := application.Snapshot{Runtime: application.RuntimeState{Model: "model"}}
	snapshot.Session.ID = "sess-main-1"
	snapshot.Runtime.GoalGovernance = &dto.GoalGovernanceView{
		Active: true,
		GoalID: "g-1",
		Title:  "给员工按权限开放工具",
		Status: dto.GoalActive,
	}
	return snapshot
}

func teamViewFixture() dto.TeamView {
	return dto.TeamView{
		SessionID:   "sess-main-1",
		TeamID:      "goal-a2a",
		TeamKind:    "goal-a2a",
		OrderPolicy: "goal_loop",
		OrderRoles:  []string{"user", "main", "tl"},
		Configured:  true,
		FloorRole:   "main",
		Members: []dto.TeamMember{
			{RoleName: "user", RoleKind: dto.RoleKindUser, JoinPolicy: "builtin", OrderIndex: 0, InOrder: true},
			{RoleName: "main", RoleKind: dto.RoleKindMain, JoinPolicy: "builtin", OrderIndex: 1, InOrder: true, RoleSessionID: "sess-main-1"},
			{RoleName: "tl", RoleKind: dto.RoleKindTechlead, JoinPolicy: "on_goal_create", OrderIndex: 2, InOrder: true, ToolsPolicy: "readonly", RoleSessionID: "goal-a2a-tl"},
		},
		Schedule: &dto.TeamSchedule{
			OrderPolicy:     "goal_loop",
			Order:           []string{"main", "tl"},
			NextRole:        "tl",
			Round:           2,
			RoundLimit:      6,
			NoProgress:      0,
			NoProgressLimit: 3,
			NoAutomaticTurn: []string{"tl"},
		},
	}
}

// altKey 造 Alt+<rune> 按键（tea 的 Alt+字母 = 带 Alt 修饰的 rune 键）。
func altRuneKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: true}
}

func escKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEsc} }

// press 走一次 Update，返回更新后的 Model（测试只关心面板状态与投影）。
func press(t *testing.T, model Model, message tea.KeyMsg) (Model, tea.Cmd) {
	t.Helper()
	updated, command := model.Update(message)
	result, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update 返回的不是 Model: %T", updated)
	}
	return result, command
}

func TestGoalPanelRendersGovernanceProjection(t *testing.T) {
	app := newFakeApp()
	app.snapshot = goalSnapshot()
	model := NewModel(app)
	model.showLogo = false
	model.width, model.height = 100, 40

	model, command := press(t, model, altRuneKey('g'))
	if command != nil {
		t.Fatal("目标面板不应发起 IO（数据源是 Snapshot 投影）")
	}
	if model.panel != panelGoal {
		t.Fatalf("Alt+G 后面板 = %q, want %q", model.panel, panelGoal)
	}
	panel := model.renderPanel()
	// 面板按治理投影渲染 goal 状态词：取值只可能来自 dto.GoalStatus 那一格
	// （夹具早先用过 "running"——那是**格外的词**，枚举化后写不出来了）。
	for _, want := range []string{"GOAL", "active", "g-1"} {
		if !strings.Contains(panel, want) {
			t.Fatalf("目标面板缺少 %q：\n%s", want, panel)
		}
	}

	// Esc 关闭：面板消失，会话区行数回到无面板时的水平。
	model, _ = press(t, model, escKey())
	if model.panel != panelNone || model.renderPanel() != "" {
		t.Fatalf("Esc 未关闭面板: panel=%q", model.panel)
	}
}

func TestGoalPanelWithoutActiveGoalPointsAtEntry(t *testing.T) {
	app := newFakeApp()
	model := NewModel(app)
	model.showLogo = false
	model.width = 80

	model, _ = press(t, model, altRuneKey('g'))
	panel := model.renderPanel()
	if !strings.Contains(panel, "没有活跃 goal") || !strings.Contains(panel, "goal_begin") {
		t.Fatalf("无活跃 goal 时面板未给出上线提示：\n%s", panel)
	}
}

// TestGoalPanelShowsRecoveredFrameAndHistoryLedger：目标面板的两条新痕迹
//   - recovered：这一帧来自会话存档快照（活体栈给不出时才兜底）；
//   - 「历史目标」：已收口/中止目标的只读账本，与看板主体分开显示——不收口的目标
//     只在账本里，看板主体写的仍是 active seq。
//
// 同时钉住"面板可见性归 goal 状态机"：没有活跃帧时面板只给上线提示，账本不单独
// 撑起一块面板（与 GUI 同口径；收口后想常驻看账本要单独裁决）。
func TestGoalPanelShowsRecoveredFrameAndHistoryLedger(t *testing.T) {
	app := newFakeApp()
	snapshot := goalSnapshot()
	snapshot.Runtime.GoalGovernance.Recovered = true
	snapshot.Runtime.GoalGovernance.History = []dto.GoalHistoryView{
		{GoalID: "g-2", Title: "上一轮收口的目标", Status: "completed", ClosedAt: 1760000000, ClosedReason: "goal.finish", ProgressCount: 5},
		{GoalID: "g-3", Title: "<i>没人做过的目标</i>", Status: "aborted", ClosedAt: 1760003600, ClosedReason: "goal.abort"},
	}
	app.snapshot = snapshot
	model := NewModel(app)
	model.showLogo = false
	model.width, model.height = 120, 40

	model, _ = press(t, model, altRuneKey('g'))
	panel := model.renderPanel()
	for _, want := range []string{"自快照恢复", "历史目标 2", "g-2", "上一轮收口的目标", "COMPLETED", "goal.finish", "打点 5 条",
		"g-3", "ABORTED", "goal.abort"} {
		if !strings.Contains(panel, want) {
			t.Fatalf("目标面板缺少 %q：\n%s", want, panel)
		}
	}
	// 账本与看板主体分开：看板头写的是活跃那一帧（g-1），账本行写自己的编号。
	if !strings.Contains(panel, "g-1") {
		t.Fatalf("看板主体应写 active 的编号：\n%s", panel)
	}
	if got, want := model.panelHeight(), strings.Count(panel, "\n")+1; got != want {
		t.Fatalf("面板高度 = %d, 渲染行数 = %d", got, want)
	}

	// 折叠：账本很长时只显示最近 goalHistoryLineLimit 条，其余折成一行（不挤掉对话区）。
	app.snapshot.Runtime.GoalGovernance.History = []dto.GoalHistoryView{
		{GoalID: "g-1", Status: "completed"}, {GoalID: "g-2", Status: "completed"},
		{GoalID: "g-3", Status: "completed"}, {GoalID: "g-4", Status: "completed"},
		{GoalID: "g-5", Status: "completed"}, {GoalID: "g-6", Status: "completed"},
	}
	folded := NewModel(app)
	folded.showLogo = false
	folded.width, folded.height = 120, 40
	folded, _ = press(t, folded, altRuneKey('g'))
	text := folded.renderPanel()
	if !strings.Contains(text, "另有 2 条更早的收口已折叠") {
		t.Fatalf("长账本没有折叠提示：\n%s", text)
	}
	if rows := strings.Count(text, "COMPLETED"); rows != goalHistoryLineLimit {
		t.Fatalf("折叠后应只显示最近 %d 条，实际 %d：\n%s", goalHistoryLineLimit, rows, text)
	}
	// 没有活跃帧时账本不单独露面（面板可见性归 goal 状态机）。
	app.snapshot.Runtime.GoalGovernance = &dto.GoalGovernanceView{
		Active:  false,
		History: []dto.GoalHistoryView{{GoalID: "g-1", Status: "completed"}},
	}
	closed := NewModel(app)
	closed.showLogo = false
	closed.width = 80
	closed, _ = press(t, closed, altRuneKey('g'))
	if text := closed.renderPanel(); strings.Contains(text, "历史目标") {
		t.Fatalf("没有活跃帧时不应单独撑起账本面板：\n%s", text)
	}
}

// TestTeamPanelFetchesServiceViewOnce / TestTeamPanelRefetchesOnTeamChanged /
// TestTeamPanelReportsMissingReaderAndUnconfiguredTeam 已随 Agent Team 只读面退场
// （2026-10-12 J-1a）删除：它们钉的是 Alt+T 面板上半段（成员表 + 发言调度运行态）的
// 异步读面（*application.Service.AgentTeamView）与 team.changed 重取。那个读面与它的
// 发布点都已整条退场，面板现在只渲染团队看板（数据在会话快照里，不需要异步取数）。
//
// 面板本身仍在，且有一条仍然成立的断言：打开面板**不排任何异步命令**
// （TeamworkBoard 已在快照里，IO 不落 Update）。

// TestPanelHeightMatchesRenderedLines：convHeight 用 panelHeight 扣高度，两者
// 一旦不一致就会撑破 viewport（或留下空白行）。
func TestPanelHeightMatchesRenderedLines(t *testing.T) {
	base := newFakeApp()
	base.snapshot = goalSnapshot()
	app := &teamFakeApp{fakeApp: base, view: teamViewFixture()}
	model := NewModel(app)
	model.showLogo = false
	model.width, model.height = 120, 40
	model.ready = true

	// 基线：面板未打开时的对话区高度（"面板占了多少行"只能相对它说）。
	before := model.convHeight()

	model, _ = press(t, model, altRuneKey('g'))
	if got, want := model.panelHeight(), strings.Count(model.renderPanel(), "\n")+1; got != want {
		t.Fatalf("目标面板高度 = %d, 渲染行数 = %d", got, want)
	}
	if model.convHeight() >= before {
		t.Fatalf("目标面板打开后对话区没有变矮：before=%d after=%d", before, model.convHeight())
	}

	// Alt+T：打开团队看板面板。J-1a（2026-10-12）之后它**不再排异步命令**——
	// 面板内容全在会话快照里（Runtime.TeamworkBoard），IO 不落 Update。
	model, command := press(t, model, altRuneKey('t'))
	if command != nil {
		t.Fatal("团队看板面板只读快照，不该排任何异步命令（成员表读面已随 J-1a 退场）")
	}
	if got, want := model.panelHeight(), strings.Count(model.renderPanel(), "\n")+1; got != want {
		t.Fatalf("团队面板高度 = %d, 渲染行数 = %d", got, want)
	}
	// 对话区高度必须扣掉面板占用的行数。
	if want := max(model.height-model.topPanelH()-model.planPanelH()-model.panelHeight()-model.midPanelH()-model.bottomPanelH(), 4); model.convHeight() != want {
		t.Fatalf("convHeight = %d, want %d", model.convHeight(), want)
	}
	if model.convHeight() >= before {
		t.Fatalf("团队看板面板打开后对话区没有变矮：before=%d after=%d", before, model.convHeight())
	}
}

// TestPanelKeysDoNotHijackInteractionOrInput：待批选择优先；普通按键进入输入框
// （面板不吞键）；面板键在任何时候都不提交输入。
func TestPanelKeysDoNotHijackInteractionOrInput(t *testing.T) {
	app := newFakeApp()
	app.snapshot = goalSnapshot()
	app.snapshot.Interaction = &application.Interaction{
		ID:      "ix-1",
		Title:   "需要批准",
		Options: []application.InteractionOption{{Label: "允许"}, {Label: "拒绝"}},
	}
	model := NewModel(app)
	model.showLogo = false
	model.width = 90

	model, _ = press(t, model, altRuneKey('g'))
	if model.panel != panelNone {
		t.Fatalf("有待批选择时仍打开了面板: %q", model.panel)
	}
	if !strings.Contains(model.uiError, "待批") {
		t.Fatalf("未给出面板与选择互斥的提示: %q", model.uiError)
	}

	// 无选择时：普通字符进入输入框，Alt+G 打开面板，且不提交任何输入。
	app2 := newFakeApp()
	app2.snapshot = goalSnapshot()
	model = NewModel(app2)
	model.showLogo = false
	model.width = 90
	model, _ = press(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if app2.submitted != "" {
		t.Fatalf("按键被误提交: %q", app2.submitted)
	}
	model, _ = press(t, model, altRuneKey('g'))
	if model.panel != panelGoal {
		t.Fatalf("Alt+G 未打开目标面板: %q", model.panel)
	}
	if app2.submitted != "" {
		t.Fatalf("打开面板不该提交输入: %q", app2.submitted)
	}
}

func TestStatusBarShowsGoalBadge(t *testing.T) {
	app := newFakeApp()
	app.snapshot = goalSnapshot()
	model := NewModel(app)
	model.showLogo = false
	model.width = 120
	if bar := model.renderStatusBar(); !strings.Contains(bar, "goal") {
		t.Fatalf("状态行缺少 goal 标记：%q", bar)
	}

	// 只有 skill 激活（治理未上线）时显示裸 badge。
	app.snapshot.Runtime.GoalGovernance = nil
	app.snapshot.Runtime.GoalSkillActive = true
	model = NewModel(app)
	model.showLogo = false
	model.width = 120
	if bar := model.renderStatusBar(); !strings.Contains(bar, "goal") {
		t.Fatalf("状态行缺少 goal skill 标记：%q", bar)
	}
}

// TestAltGAndAltTAreNotGlobalShortcutNoise：面板键在 running 回合里同样可用
// （只读面不该被运行态挡住），且不影响 Ctrl+C 取消。
func TestPanelShortcutAvailableWhileRunning(t *testing.T) {
	app := newFakeApp()
	app.snapshot = goalSnapshot()
	app.snapshot.Chat.Running = true
	app.snapshot.Chat.RequestID = "req-1"
	model := NewModel(app)
	model.showLogo = false
	model.width = 100

	model, _ = press(t, model, altRuneKey('g'))
	if model.panel != panelGoal {
		t.Fatalf("运行态下 Alt+G 未打开目标面板: %q", model.panel)
	}
	model, _ = press(t, model, escKey())
	model, _ = press(t, model, tea.KeyMsg{Type: tea.KeyCtrlC})
	if app.cancelled != "req-1" {
		t.Fatalf("Ctrl+C 取消失效: %q", app.cancelled)
	}
}
