package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// goalteam.go — 目标治理与 AgentTeam 的 TUI 只读面（Alt+G / Alt+T）。
//
// 口径（tui/README.md 的边界）：
//   - 面板是**纯投影**：目标面板只读 Snapshot.Runtime.GoalGovernance（application
//     已经在状态迁移后投好影），团队面板走一次 tea.Cmd 异步读
//     AppController 的可选团队读面（*application.Service.AgentTeamView，与 GUI 经
//     Bridge 读的是同一个面）；
//   - 面板**只读**：不提交输入、不改后端状态、不新增只对终端生效的业务事实；
//   - 面板高度由 panelLines() 一处给出，View 渲染的行数必须与它一致（测试钉住），
//     否则 convHeight 会把 viewport 撑破。

const (
	panelNone = ""
	panelGoal = "goal"
	panelTeam = "team"
)

// 面板的展示上限：裁决正文按 rune 截断，行数上限防止长团队把对话区挤没。
const (
	panelLineLimit       = 12
	panelTextLimit       = 96
	panelDirectiveMax    = 120
	goalHistoryLineLimit = 4
)

// errTeamViewUnsupported 表示当前 AppController 没有团队读面：面板明确提示，
// 不留空白（TUI 与 GUI 不同装配时最容易出现这种缺失）。
var errTeamViewUnsupported = errors.New("当前前端没有团队读面")

// teamViewReader 是 TUI 的可选团队读面。*application.Service 已实现
// AgentTeamView(mainSessionID)（gui/bridge.go 调的是同一个方法），因此装配根
// 不需要为新面板改动任何东西；未实现时 Alt+T 给出明确提示。
type teamViewReader interface {
	AgentTeamView(mainSessionID string) (dto.TeamView, error)
}

// teamViewMsg 是异步取回的团队视图（含错误：读面缺失 / 后端未装配团队存储）。
type teamViewMsg struct {
	view dto.TeamView
	err  error
}

func fetchTeamView(app AppController, sessionID string) tea.Cmd {
	return func() tea.Msg {
		reader, ok := app.(teamViewReader)
		if !ok {
			return teamViewMsg{err: errTeamViewUnsupported}
		}
		view, err := reader.AgentTeamView(sessionID)
		return teamViewMsg{view: view, err: err}
	}
}

// handlePanelKey 处理目标/团队面板的按键：Alt+G 目标治理、Alt+T 团队、Esc 关闭。
// handled=false 表示该键不属于面板面（调用方按常规继续处理）。
//
// 与审批面板的互斥：有待批选择时不打开只读面板——两种面板同屏会让人分不清当前
// 该按什么键（选择面板的数字键/回车才是那一步的出口）。
func (model Model) handlePanelKey(message tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	key := message.String()
	if key != "alt+g" && key != "alt+t" {
		if key == "esc" && model.panel != panelNone {
			model.panel = panelNone
			model.teamErr = ""
			return true, model, nil
		}
		return false, model, nil
	}
	if model.snapshot.Interaction != nil {
		model.uiError = "先处理当前待批选择，再打开目标/团队面板"
		return true, model, nil
	}
	if (key == "alt+g" && model.panel == panelGoal) || (key == "alt+t" && model.panel == panelTeam) {
		model.panel = panelNone
		model.teamErr = ""
		return true, model, nil
	}
	model.uiError = ""
	if key == "alt+g" {
		model.panel = panelGoal
		return true, model, nil
	}
	// Alt+T：打开（已在团队面板上再按 = 重新取一次，面板底部写明这条）。
	model.panel = panelTeam
	model.teamLoading = true
	return true, model, fetchTeamView(model.app, model.snapshot.Session.ID)
}

// applyTeamView 收下团队读面的结果（Update 的 teamViewMsg 分支）。
func (model Model) applyTeamView(message teamViewMsg) Model {
	model.teamLoading = false
	if message.err != nil {
		model.teamView = nil
		model.teamErr = message.err.Error()
		return model
	}
	view := message.view
	model.teamView = &view
	model.teamErr = ""
	return model
}

// panelHeight 是面板占用的行数（0 = 未打开）；convHeight 用它扣减对话区高度。
func (model Model) panelHeight() int {
	return len(model.panelLines())
}

// renderPanel 渲染面板正文（空串 = 未打开）。
func (model Model) renderPanel() string {
	return strings.Join(model.panelLines(), "\n")
}

// panelLines 是面板的唯一渲染源：高度与正文都由它给出，两者不可能不一致。
func (model Model) panelLines() []string {
	switch model.panel {
	case panelGoal:
		return model.goalPanelLines()
	case panelTeam:
		return model.teamPanelLines()
	default:
		return nil
	}
}

// goalPanelLines 渲染目标治理只读面（数据源 = Snapshot.Runtime.GoalGovernance，
// 与 GUI 「目标」面板同源投影；未上线 goal 时给出上线入口提示）。
//
// 面板的可见性口径与 GUI 一致：**归 goal 状态机**——栈上没有 active 帧（收口/归档）
// 就只给上线提示，不留空壳。收口账本（History）跟着看板一起出现：它是"已结束的
// 目标"那一节，与活动栈正交，但不单独撑起一块面板。
func (model Model) goalPanelLines() []string {
	goal := model.snapshot.Runtime.GoalGovernance
	if goal == nil || !goal.Active {
		lines := []string{StyleMuted.Render("  ◆ GOAL  当前会话没有活跃 goal（#goal 或 goal_begin 上线）")}
		if model.snapshot.Runtime.GoalSkillActive {
			lines = append(lines, StyleMuted.Render("  ·  goal skill 已激活，等待 goal_begin"))
		}
		lines = append(lines, StyleMuted.Render("  ·  Esc 关闭"))
		return lines
	}
	header := fmt.Sprintf("  ◆ GOAL  %s · %s",
		fallback(goal.Status, "active"), fallback(goal.GoalID, "—"))
	if goal.Recovered {
		// 这一帧来自会话存档快照（活体栈给不出时才兜底）：是个**痕迹**，不是结论——
		// 活体一恢复，后端下次投影自然撤掉它。
		header += " · 自快照恢复"
	}
	lines := []string{StyleTaskRunning.Render(header)}
	if title := oneLine(goal.Title, model.textLimit()); title != "" {
		lines = append(lines, StyleChoiceInactive.Render("  "+title))
	}
	if peer := oneLine(goal.PeerState, model.textLimit()); peer != "" {
		lines = append(lines, StyleMuted.Render("  peer: "+peer))
	}
	if directive := oneLine(goal.LastDirective, panelDirectiveMax); directive != "" {
		lines = append(lines, StyleChoiceInactive.Render("  TL: "+directive))
	} else {
		lines = append(lines, StyleMuted.Render("  TL: 暂无裁决（终态 gate 尚未评估）"))
	}
	lines = append(lines, model.goalHistoryLines()...)
	lines = append(lines, StyleMuted.Render("  ·  Alt+T 看团队 · Esc 关闭"))
	return lines
}

// goalHistoryLines 渲染「历史目标」一节：本会话已收口/中止目标的只读账本。
//
// 它是**另一件东西**，不是看板的一部分：看板主体写的是"当前治理中的那一帧"，
// 账本写的是"已经结束的目标"（口径见 docs/arch/session-board-metadata-lifecycle.md
// §3.1）。混在一行里就是把 finished 的目标读成当前目标。
//
// 只显示最近 goalHistoryLineLimit 条（终端面板会挤掉对话区），多出的折叠成一行——
// 折叠数字只说"还有几条"，不重排、不摘要。
func (model Model) goalHistoryLines() []string {
	goal := model.snapshot.Runtime.GoalGovernance
	if goal == nil || len(goal.History) == 0 {
		return nil
	}
	width := model.textLimit()
	entries := goal.History
	hidden := 0
	if len(entries) > goalHistoryLineLimit {
		hidden = len(entries) - goalHistoryLineLimit
		entries = entries[len(entries)-goalHistoryLineLimit:]
	}
	lines := []string{StyleMuted.Render(fmt.Sprintf("  ·  历史目标 %d", len(goal.History)))}
	for _, entry := range entries {
		parts := []string{fallback(strings.ToUpper(strings.TrimSpace(entry.Status)), "—")}
		if reason := oneLine(entry.ClosedReason, width/3); reason != "" {
			parts = append(parts, reason)
		}
		if entry.ProgressCount > 0 {
			parts = append(parts, fmt.Sprintf("打点 %d 条", entry.ProgressCount))
		}
		if at := formatGoalClosedAt(entry.ClosedAt); at != "" {
			parts = append(parts, "收口 "+at)
		}
		lines = append(lines, StyleMuted.Render(oneLine(fmt.Sprintf("     %s %s · %s",
			fallback(entry.GoalID, "—"), oneLine(entry.Title, width/2), strings.Join(parts, " · ")), width)))
	}
	if hidden > 0 {
		lines = append(lines, StyleMuted.Render(fmt.Sprintf("     ·  另有 %d 条更早的收口已折叠", hidden)))
	}
	return lines
}

// formatGoalClosedAt 把存档里的 unix 秒压成 "YYYY-MM-DD HH:MM"（0/负数 → ""，
// 不把渲染层的猜测当成事实：面板只报它有的东西）。
func formatGoalClosedAt(at int64) string {
	if at <= 0 {
		return ""
	}
	return time.Unix(at, 0).Format("2006-01-02 15:04")
}

// teamPanelLines 渲染团队只读面：成员与发言顺序（TeamView.OrderRoles/InOrder）、
// 定时 agent、以及调度运行态（TeamView.Schedule，与 GUI 「发言调度」串珠条同源）。
func (model Model) teamPanelLines() []string {
	if model.teamLoading {
		return []string{StyleMuted.Render("  ◆ TEAM  读取中…")}
	}
	if model.teamErr != "" {
		return []string{
			StyleError.Render("  ◆ TEAM  读取失败: " + oneLine(model.teamErr, model.textLimit())),
			StyleMuted.Render("  ·  Alt+T 重试 · Esc 关闭"),
		}
	}
	view := model.teamView
	if view == nil {
		return []string{StyleMuted.Render("  ◆ TEAM  尚未读取（Alt+T 打开）")}
	}
	if !view.Configured {
		lines := []string{
			StyleMuted.Render("  ◆ TEAM  当前会话未装配 AgentTeam（GUI 团队面板可一键装配 preset；goal_begin 也会隐式装配 goal-a2a）"),
		}
		lines = append(lines, model.teamBoardLines()...)
		lines = append(lines, StyleMuted.Render("  ·  Alt+T 重试 · Esc 关闭"))
		return lines
	}
	header := fmt.Sprintf("  ◆ TEAM  %s · order_policy=%s",
		fallback(view.TeamKind, view.TeamID), fallback(view.OrderPolicy, "—"))
	if view.FloorRole != "" {
		header += " · 发言中 " + view.FloorRole
	}
	lines := []string{StyleTaskRunning.Render(header)}
	for _, member := range view.Members {
		lines = append(lines, StyleChoiceInactive.Render("  "+memberLine(member, model.textLimit())))
	}
	for _, member := range view.Scheduled {
		lines = append(lines, StyleMuted.Render("  定时 "+memberLine(member, model.textLimit())))
	}
	if schedule := view.Schedule; schedule != nil {
		parts := []string{fmt.Sprintf("轮次 %d/%d", schedule.Round, schedule.RoundLimit)}
		if schedule.NextRole != "" {
			parts = append(parts, "下一个 "+schedule.NextRole)
		}
		parts = append(parts, fmt.Sprintf("无进展 %d/%d", schedule.NoProgress, schedule.NoProgressLimit))
		if schedule.Stopped {
			parts = append(parts, "已收束("+fallback(schedule.StopReason, "—")+")")
		}
		lines = append(lines, StyleMuted.Render("  调度 "+strings.Join(parts, " · ")))
		if len(schedule.Unexecuted) > 0 {
			lines = append(lines, StyleMuted.Render("  无执行者 "+oneLine(strings.Join(schedule.Unexecuted, ","), model.textLimit())))
		}
	} else {
		lines = append(lines, StyleMuted.Render("  调度 无运行态（本会话没有发言调度运行时）"))
	}
	lines = append(lines, model.teamBoardLines()...)
	lines = append(lines, StyleMuted.Render("  ·  Alt+T 刷新 · Alt+G 看目标 · Esc 关闭"))
	return clampLines(lines, panelLineLimit)
}

// teamBoardLines 渲染**团队看板**（数据源 = Snapshot.Runtime.TeamworkBoard，与 GUI
// 「团队看板」子页**同源投影**：同一份后端只读投影，不另起一套取值）。
// 无计划（nil 或没有阶段）→ nil：不追加空壳，口径同 GUI。
//
// 这一节刻意**不重算**阶段状态与拓扑层号——那些是纯渲染件
// （gui/frontend/dist/team-board-view.js）的职责，终端里再折一遍就是第二份事实。
// 这里只把投影里已有的东西逐行说清楚：阶段（角色 / 依赖边）、该阶段的作业行、里程碑。
func (model Model) teamBoardLines() []string {
	board := model.snapshot.Runtime.TeamworkBoard
	if board == nil || (len(board.Stages) == 0 && len(board.WorkItems) == 0) {
		return nil
	}
	width := model.textLimit()
	running, done, failed := 0, 0, 0
	for _, job := range board.Jobs {
		switch job.State {
		case "running":
			running++
		case "done":
			done++
		case "failed", "killed":
			failed++
		}
	}
	header := fmt.Sprintf("  ◆ 团队看板  %s · v%d · 阶段 %d · 工作项 %d · %s · 作业 %d 跑/%d 完/%d 败",
		fallback(board.TeamID, "—"), board.Version, len(board.Stages), len(board.WorkItems), rosterText(board),
		running, done, failed)
	lines := []string{StyleTaskRunning.Render(oneLine(header, width))}
	for _, stage := range board.Stages {
		roles := "—"
		if len(stage.Roles) > 0 {
			roles = strings.Join(stage.Roles, ",")
		}
		deps := "—"
		if len(stage.DependsOn) > 0 {
			deps = strings.Join(stage.DependsOn, ",")
		}
		lines = append(lines, StyleChoiceInactive.Render(oneLine(
			fmt.Sprintf("  %s  %s  deps:%s", stage.ID, roles, deps), width)))
		for _, job := range board.Jobs {
			if job.Stage != stage.ID && job.Node != stage.ID {
				continue
			}
			lines = append(lines, StyleMuted.Render(oneLine(
				fmt.Sprintf("    作业 %s %s %s", job.Handle, fallback(job.State, "—"), formatByteSize(job.Bytes)), width)))
		}
	}
	// 里程碑甘特：里程碑之间串行（屏障），里程碑内部是工作项依赖 DAG。终端里同样
	// **不重算**依赖层号与状态折算（那是渲染件的职责），只把投影里的东西逐行说清楚。
	for _, milestone := range board.Milestones {
		deps := "—"
		if len(milestone.DependsOn) > 0 {
			deps = strings.Join(milestone.DependsOn, ",")
		}
		line := fmt.Sprintf("  里程碑 %s %s  屏障:%s", milestone.ID, fallback(milestone.Status, "pending"), deps)
		if content := oneLine(milestone.Content, width/2); content != "" {
			line += " · " + content
		}
		lines = append(lines, StyleMuted.Render(oneLine(line, width)))
		for _, item := range board.WorkItems {
			if item.Milestone != milestone.ID {
				continue
			}
			detail := fmt.Sprintf("    %s %s · %s · %s", item.ID, item.Name, fallback(item.Status, "pending"), fallback(item.Role, "—"))
			if item.Interrupted {
				detail += " · 可重派"
			}
			if len(item.DependsOn) > 0 {
				detail += " · deps:" + strings.Join(item.DependsOn, ",")
			}
			lines = append(lines, StyleMuted.Render(oneLine(detail, width)))
		}
	}
	// teammate 段：名字 / 状态 / 负责的工作项名称队列（看板要回答"这个人手上还有什么"）。
	for _, member := range board.Members {
		if len(member.Queue) == 0 && member.Status == "" {
			continue
		}
		queue := "空"
		if len(member.Queue) > 0 {
			queue = strings.Join(member.Queue, " → ")
		}
		lines = append(lines, StyleMuted.Render(oneLine(
			fmt.Sprintf("  %s %s  队列:%s", member.Role, fallback(member.Status, "idle"), queue), width)))
	}
	if board.Recovered {
		// 来自会话存档快照（活体投影给不出时才兜底）：与 GUI 同形的痕迹标记。
		lines = append(lines, StyleMuted.Render("  ·  自快照恢复（活体投影给不出，这份看板来自会话存档）"))
	}
	if board.Stale {
		lines = append(lines, StyleMuted.Render("  ·  句柄投影可能过期（jobs I-4：句柄只在内存，进程重启后作废）"))
	}
	return lines
}

// rosterText 是在编一行：有产品级上限时写「在编 n/max」（看板要能区分"正常"与"顶到上限"），
// 没有上限时只写「在编 n」——不编造一个不存在的上限。
//
// 成员**不在这里逐行列出**：成员表归 Alt+T 面板上半段的 AgentTeam 那一节。同一块面板里
// 再列一遍"在编"，只会让人以为有两份互相矛盾的名册（AgentTeam 是发言调度面，
// 团队看板是 leader 的硬编排面，两者本来就可能是不同的集合）。
func rosterText(board *dto.TeamworkBoardView) string {
	if board.MaxMembers > 0 {
		return fmt.Sprintf("在编 %d/%d", len(board.Members), board.MaxMembers)
	}
	return fmt.Sprintf("在编 %d", len(board.Members))
}

// formatByteSize 把字节数压成终端友好的短形态（0 → "0B"，不写"0.0KiB"）。
func formatByteSize(bytes int64) string {
	switch {
	case bytes <= 0:
		return "0B"
	case bytes < 1024:
		return fmt.Sprintf("%dB", bytes)
	case bytes < 1024*1024:
		return fmt.Sprintf("%.1fKiB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%.1fMiB", float64(bytes)/(1024*1024))
	}
}

// memberLine 渲染一行成员：位置 · 角色（kind/tools_policy）· 角色会话 ID 尾号 ·
// 入职/在位口径。
func memberLine(member dto.TeamMember, width int) string {
	position := "未排入"
	if member.InOrder || member.OrderIndex >= 0 {
		position = fmt.Sprintf("#%d", member.OrderIndex+1)
	}
	meta := string(member.RoleKind)
	if member.ToolsPolicy != "" {
		meta += "/" + member.ToolsPolicy
	}
	session := "—"
	if member.RoleSessionID != "" {
		session = shortID(member.RoleSessionID)
	}
	line := fmt.Sprintf("%-4s %-10s %-18s %s", position, member.RoleName, meta, session)
	if member.JoinPolicy != "" {
		line += " " + member.JoinPolicy
	}
	return oneLine(line, width)
}

// clampLines 截断超长面板：多出的内容折叠成一行提示，避免挤掉对话区。
func clampLines(lines []string, limit int) []string {
	if limit <= 0 || len(lines) <= limit {
		return lines
	}
	clamped := append([]string(nil), lines[:limit-1]...)
	clamped = append(clamped, StyleMuted.Render(fmt.Sprintf("  ·  另有 %d 行已折叠（GUI 团队面板可看全）", len(lines)-limit+1)))
	return clamped
}

// textLimit 是面板正文按终端宽度换算的展示上限（中文按 1 rune 计，宽度安全余量 6）。
func (model Model) textLimit() int {
	width := model.width - 6
	if width < 20 {
		width = 20
	}
	if width > panelTextLimit {
		width = panelTextLimit
	}
	return width
}

// oneLine 把多行文本压成一行并按 rune 截断（面板是单行块的堆叠，不能有裸换行）。
func oneLine(text string, limit int) string {
	flat := strings.Join(strings.Fields(strings.ReplaceAll(text, "\n", " ")), " ")
	runes := []rune(flat)
	if limit <= 0 || len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "…"
}

// fallback 取非空值（面板上的空字段显示为占位符而不是空白）。
func fallback(value, placeholder string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return placeholder
	}
	return value
}

// shortID 显示会话/角色会话 ID 的尾号（长 ID 在终端里挤掉其他列）。
func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return "…" + id[len(id)-11:]
}

// goalBadge 是状态行的 goal 短标记（与 GUI 的 GOAL badge 同口径：有活跃治理
// 或 skill 激活都显示 goal）。
func goalBadge(snapshot application.Snapshot) string {
	if goal := snapshot.Runtime.GoalGovernance; goal != nil && goal.Active {
		return "goal"
	}
	if snapshot.Runtime.GoalSkillActive {
		return "goal"
	}
	return ""
}
