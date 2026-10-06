package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
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
		Active:        true,
		GoalID:        "g-1",
		Title:         "给员工按权限开放工具",
		Status:        dto.GoalActive,
		PeerState:     dto.PeerAdvisoryPending,
		LastDirective: "[verdict_done] 两条验收证据在本次 ADVISOR 输入中均可核对",
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
	for _, want := range []string{"GOAL", "active", "advisory_pending", "verdict_done", "g-1"} {
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

func TestTeamPanelFetchesServiceViewOnce(t *testing.T) {
	base := newFakeApp()
	base.snapshot = goalSnapshot()
	app := &teamFakeApp{fakeApp: base, view: teamViewFixture()}
	model := NewModel(app)
	model.showLogo = false
	model.width, model.height = 120, 40

	model, command := press(t, model, altRuneKey('t'))
	if command == nil {
		t.Fatal("团队面板必须经 tea.Cmd 异步读取（IO 不阻塞 Update）")
	}
	if !model.teamLoading {
		t.Fatal("团队面板未标记加载态")
	}
	// 命令跑完（阻塞点在这里，不在 Update）。
	updated, _ := model.Update(command())
	model = updated.(Model)

	if app.sessionID != "sess-main-1" {
		t.Fatalf("团队读面的会话 = %q, want 主会话 ID", app.sessionID)
	}
	panel := model.renderPanel()
	for _, want := range []string{"TEAM", "goal-a2a", "goal_loop", "tl", "readonly", "goal-a2a-tl", "下一个 tl", "轮次 2/6", "下一个 tl"} {
		if !strings.Contains(panel, want) {
			t.Fatalf("团队面板缺少 %q：\n%s", want, panel)
		}
	}
	if model.teamLoading || model.teamErr != "" {
		t.Fatalf("团队面板加载态未收敛: loading=%v err=%q", model.teamLoading, model.teamErr)
	}
}

// TestTeamPanelRefetchesOnTeamChanged 钉住"装配后面板自动更新"：面板打开时收到
// team.changed 就地重取成员表；面板收起时不产生请求。装配/顺序/入职改的是后端
// 事实（数据不在会话快照里），此前只能靠用户再按一次 Alt+T 才看得到。
func TestTeamPanelRefetchesOnTeamChanged(t *testing.T) {
	base := newFakeApp()
	base.snapshot = goalSnapshot()
	app := &teamFakeApp{fakeApp: base, view: teamViewFixture()}
	model := NewModel(app)
	model.showLogo = false
	model.width, model.height = 120, 40

	// 打开面板：一次异步读。
	model, command := press(t, model, altRuneKey('t'))
	updated, _ := model.Update(command())
	model = updated.(Model)
	if model.teamErr != "" {
		t.Fatalf("打开面板读失败：%q", model.teamErr)
	}

	// 面板收起：team.changed 不产生请求（不可见的面板不该发 RPC）。
	model.panel = panelNone
	updated, _ = model.Update(applicationEventMsg{event: application.Event{Kind: application.EventTeamChanged}})
	if updated.(Model).teamLoading {
		t.Fatal("面板收起时不该重取团队成员表")
	}

	// 面板打开：team.changed 就地重取。
	model.panel = panelTeam
	app.sessionID = ""
	updated, command = model.Update(applicationEventMsg{event: application.Event{Kind: application.EventTeamChanged}})
	model = updated.(Model)
	if !model.teamLoading {
		t.Fatal("团队事实变了，面板必须重取（否则停在旧成员表）")
	}
	// 命令是 Batch（重取 + 继续等事件）：关掉订阅让等待命令立即返回，只取重取那条。
	model.subscription.Close()
	batch, ok := command().(tea.BatchMsg)
	if !ok {
		t.Fatalf("team.changed 应同时排入重取与继续等待，实际命令 = %T", command())
	}
	fetched := false
	for _, inner := range batch {
		if message, ok := inner().(teamViewMsg); ok {
			fetched = true
			model = model.applyTeamView(message)
		}
	}
	if !fetched {
		t.Fatal("batch 里没有团队读面命令")
	}
	if app.sessionID != "sess-main-1" {
		t.Fatalf("重取用了错误的会话 ID：%q", app.sessionID)
	}
	if model.teamLoading || model.teamErr != "" {
		t.Fatalf("重取后加载态未收敛: loading=%v err=%q", model.teamLoading, model.teamErr)
	}
}

func TestTeamPanelReportsMissingReaderAndUnconfiguredTeam(t *testing.T) {
	// 1) 装配根没给团队读面（TUI/GUI 装配不同的宿主）。
	app := newFakeApp()
	model := NewModel(app)
	model.showLogo = false
	model.width = 90
	model, command := press(t, model, altRuneKey('t'))
	updated, _ := model.Update(command())
	model = updated.(Model)
	if !strings.Contains(model.renderPanel(), "没有团队读面") {
		t.Fatalf("读面缺失时面板未提示：\n%s", model.renderPanel())
	}

	// 2) 读面在，但本会话还没装配团队（未配置）。
	base := newFakeApp()
	base.snapshot = goalSnapshot()
	failing := &teamFakeApp{fakeApp: base, err: errors.New("团队存储未装配")}
	model = NewModel(failing)
	model.showLogo = false
	model.width = 90
	model, command = press(t, model, altRuneKey('t'))
	updated, _ = model.Update(command())
	model = updated.(Model)
	if !strings.Contains(model.renderPanel(), "读取失败") {
		t.Fatalf("读面报错时面板未提示：\n%s", model.renderPanel())
	}

	unconfigured := &teamFakeApp{fakeApp: base, view: dto.TeamView{SessionID: "sess-main-1"}}
	model = NewModel(unconfigured)
	model.showLogo = false
	model.width = 90
	model, command = press(t, model, altRuneKey('t'))
	updated, _ = model.Update(command())
	model = updated.(Model)
	if !strings.Contains(model.renderPanel(), "未装配 AgentTeam") {
		t.Fatalf("未配置团队时面板未提示：\n%s", model.renderPanel())
	}
}

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

	model, _ = press(t, model, altRuneKey('g'))
	if got, want := model.panelHeight(), strings.Count(model.renderPanel(), "\n")+1; got != want {
		t.Fatalf("目标面板高度 = %d, 渲染行数 = %d", got, want)
	}
	before := model.convHeight()

	model, command := press(t, model, altRuneKey('t'))
	if got, want := model.panelHeight(), strings.Count(model.renderPanel(), "\n")+1; got != want {
		t.Fatalf("加载态面板高度 = %d, 渲染行数 = %d", got, want)
	}
	updated, _ := model.Update(command())
	model = updated.(Model)
	if got, want := model.panelHeight(), strings.Count(model.renderPanel(), "\n")+1; got != want {
		t.Fatalf("团队面板高度 = %d, 渲染行数 = %d", got, want)
	}
	// 对话区高度必须扣掉面板占用的行数。
	if want := max(model.height-model.topPanelH()-model.planPanelH()-model.panelHeight()-model.midPanelH()-model.bottomPanelH(), 4); model.convHeight() != want {
		t.Fatalf("convHeight = %d, want %d", model.convHeight(), want)
	}
	if model.convHeight() >= before {
		t.Fatalf("面板打开后对话区没有变矮：before=%d after=%d", before, model.convHeight())
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
