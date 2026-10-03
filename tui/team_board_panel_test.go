package tui

// team_board_panel_test.go — 钉住 TUI 侧的**团队看板**（Alt+T 面板里的一节）。
//
// 口径（契约 docs/arch/team-board-gui-tui-contract.md §6）：TUI 与 GUI「团队看板」子页
// **同源**——两边都只读 Snapshot.Runtime.TeamworkBoard，不另起一套取值；没有计划
// （nil 或无里程碑也无工作项）→ 不追加任何行（不留空壳）。**没有阶段**（2026-10-04
// 阶段口径整条退场）：终端与前端都只认里程碑 + 工作项。

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// boardSnapshot 在 goal 快照上挂一份团队看板投影（与后端下发的形状一致）。
func boardSnapshot() application.Snapshot {
	snapshot := goalSnapshot()
	snapshot.Runtime.TeamworkBoard = &dto.TeamworkBoardView{
		TeamID:     "team-board-gui-tui",
		Version:    2,
		MaxMembers: 6,
		Members: []dto.TeamworkMemberView{
			{Role: "impl_ui", RoleSessionID: "sess-impl", ToolsPolicy: "readwrite", Status: "running"},
		},
		Milestones: []dto.TeamworkMilestoneView{
			{ID: "m-impl", Name: "实现", Content: "契约定稿", Status: "active"},
			{ID: "m-ship", Name: "发布", DependsOn: []string{"m-impl"}, Status: "pending"},
		},
		WorkItems: []dto.TeamworkWorkItemView{
			{ID: "wi-impl", Milestone: "m-impl", Role: "impl_ui", Name: "实现 UI", Status: "running", SessionID: "sess-impl"},
			{ID: "wi-ship", Milestone: "m-ship", Role: "impl_ui", Name: "发布", Status: "pending", DependsOn: []string{"wi-impl"}},
		},
		Jobs: []dto.TeamworkJobView{
			{Handle: "a7", State: "running", Bytes: 2048, Node: "wi-impl", Role: "impl_ui"},
		},
	}
	return snapshot
}

func TestTeamPanelShowsTeamBoardProjection(t *testing.T) {
	base := newFakeApp()
	base.snapshot = boardSnapshot()
	app := &teamFakeApp{fakeApp: base, view: teamViewFixture()}
	model := NewModel(app)
	model.showLogo = false
	model.width, model.height = 120, 60

	model, command := press(t, model, altRuneKey('t'))
	if command == nil {
		t.Fatal("团队面板必须经 tea.Cmd 异步读取")
	}
	updated, _ := model.Update(command())
	model = updated.(Model)

	panel := model.renderPanel()
	// 看板头：team_id / 版本 / 里程碑数 / 在编 / 作业计数。
	// 面板只有 12 行（panelLineLimit），所以断言只覆盖**前两节**能排下的东西。
	for _, want := range []string{
		"团队看板", "team-board-gui-tui", "v2", "里程碑 2", "在编 1/6", "作业 1 跑/0 完/0 败",
		// 里程碑：id / 状态 / 屏障 / leader 撰写的内容。
		"m-impl", "active", "屏障:—", "契约定稿",
		// 里程碑下的工作项行 + 它的作业行（权威归属只有一格：job.node = 工作项 id）。
		"wi-impl", "实现 UI", "impl_ui", "a7", "running", "2.0KiB",
	} {
		if !strings.Contains(panel, want) {
			t.Fatalf("团队面板缺少 %q：\n%s", want, panel)
		}
	}
	// 成员表仍在（新增一节不改既有口径）。
	if !strings.Contains(panel, "goal-a2a") {
		t.Fatalf("团队面板丢了成员表：\n%s", panel)
	}
}

func TestTeamPanelOmitsTeamBoardWithoutPlan(t *testing.T) {
	base := newFakeApp()
	base.snapshot = goalSnapshot() // 没有 TeamworkBoard
	app := &teamFakeApp{fakeApp: base, view: teamViewFixture()}
	model := NewModel(app)
	model.showLogo = false
	model.width, model.height = 120, 60

	model, command := press(t, model, altRuneKey('t'))
	updated, _ := model.Update(command())
	model = updated.(Model)
	if panel := model.renderPanel(); strings.Contains(panel, "团队看板") {
		t.Fatalf("没有计划时不得渲染团队看板空壳：\n%s", panel)
	}
}

func TestTeamBoardLinesReturnNilWithoutPlan(t *testing.T) {
	app := newFakeApp()
	app.snapshot = goalSnapshot()
	model := NewModel(app)
	if lines := model.teamBoardLines(); lines != nil {
		t.Fatalf("无计划时必须返回 nil（调用方不追加任何行），得到 %#v", lines)
	}
	// 有计划但阶段为空：同样不留空壳（口径与渲染件的空计划一致）。
	app.snapshot.Runtime.TeamworkBoard = &dto.TeamworkBoardView{TeamID: "t", Version: 1}
	if lines := model.teamBoardLines(); lines != nil {
		t.Fatalf("阶段为空时必须返回 nil，得到 %#v", lines)
	}
}
