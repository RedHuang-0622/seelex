package tui

// team_board_panel_test.go — 钉住 TUI 侧的**团队看板**（Alt+T 面板里的一节）。
//
// 口径（契约 docs/arch/team-board-gui-tui-contract.md §6）：TUI 与 GUI「团队看板」子页
// **同源**——两边都只读 Snapshot.Runtime.TeamworkBoard，不另起一套取值；没有计划
// （nil 或无阶段）→ 不追加任何行（不留空壳）。

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
		Stages: []dto.TeamworkStageView{
			{ID: "design", Roles: []string{"arch"}},
			{ID: "impl_ui", Roles: []string{"impl_ui"}, DependsOn: []string{"design"}},
		},
		Members: []dto.TeamworkMemberView{
			{Role: "arch", RoleSessionID: "sess-arch", ToolsPolicy: "readwrite"},
		},
		Milestones: []dto.TeamworkMilestoneView{
			{ID: "m-design", After: []string{"design"}, Content: "契约定稿", Status: "done"},
		},
		Jobs: []dto.TeamworkJobView{
			{Handle: "a7", State: "running", Bytes: 2048, Stage: "impl_ui", Role: "impl_ui"},
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
	// 看板头：team_id / 版本 / 阶段数 / 在编 / 作业计数。
	for _, want := range []string{
		"团队看板", "team-board-gui-tui", "v2", "阶段 2", "在编 1/6", "作业 1 跑/0 完/0 败",
		// 阶段行：id / 角色 / 依赖边（顺序的唯一事实）。
		"design", "arch", "deps:—", "impl_ui", "deps:design",
		// 该阶段的作业行（权威归属由桥给：job.stage）。
		"a7", "running", "2.0KiB",
		// 里程碑：id / 状态 / leader 撰写的内容。
		"m-design", "done", "契约定稿",
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
