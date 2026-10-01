package model

// teamwork_board_clone_test.go — 团队看板投影进快照后的**深拷贝**口径。
//
// 为什么值得单钉：快照是并发读者的共享值（GUI/TUI 各读一份），CloneRuntimeState
// 只要漏一层，前端就会读到一半的写。最容易被漏掉的正是**元素内嵌的切片**
// （stages[].roles / milestones[].after）——外层切片换新、内层仍指向同一底层数组。

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

func TestCloneRuntimeStateDeepCopiesTeamworkBoard(t *testing.T) {
	runtime := RuntimeState{
		TeamworkBoard: &dto.TeamworkBoardView{
			TeamID:  "team-board-gui-tui",
			Version: 2,
			Stages: []dto.TeamworkStageView{
				{ID: "design", Roles: []string{"arch"}, DependsOn: []string{"req"}},
			},
			Members: []dto.TeamworkMemberView{{Role: "arch", ToolsPolicy: "readwrite"}},
			Milestones: []dto.TeamworkMilestoneView{
				{ID: "m-1", After: []string{"design"}, Content: "契约定稿"},
			},
			Jobs:   []dto.TeamworkJobView{{Handle: "a7", Stage: "design"}},
			Events: []dto.TeamworkEventView{{Kind: "plan"}},
		},
	}
	cloned := CloneRuntimeState(runtime)
	if cloned.TeamworkBoard == runtime.TeamworkBoard {
		t.Fatal("看板必须是新值（不能两个读者共享同一份可变视图）")
	}

	// 外层切片独立。
	cloned.TeamworkBoard.Members[0].Role = "改了"
	if runtime.TeamworkBoard.Members[0].Role != "arch" {
		t.Fatal("成员切片必须独立")
	}
	cloned.TeamworkBoard.Jobs[0].Handle = "b1"
	if runtime.TeamworkBoard.Jobs[0].Handle != "a7" {
		t.Fatal("作业切片必须独立")
	}
	cloned.TeamworkBoard.Events[0].Kind = "retire"
	if runtime.TeamworkBoard.Events[0].Kind != "plan" {
		t.Fatal("审计切片必须独立")
	}

	// 内嵌切片独立（漏这一层就是"外层换了、内层还指着同一数组"）。
	cloned.TeamworkBoard.Stages[0].Roles[0] = "改了"
	if runtime.TeamworkBoard.Stages[0].Roles[0] != "arch" {
		t.Fatal("阶段角色（内嵌切片）必须独立")
	}
	cloned.TeamworkBoard.Stages[0].DependsOn[0] = "改了"
	if runtime.TeamworkBoard.Stages[0].DependsOn[0] != "req" {
		t.Fatal("阶段依赖（内嵌切片）必须独立")
	}
	cloned.TeamworkBoard.Milestones[0].After[0] = "改了"
	if runtime.TeamworkBoard.Milestones[0].After[0] != "design" {
		t.Fatal("里程碑判据（内嵌切片）必须独立")
	}
}

func TestCloneRuntimeStateKeepsNilTeamworkBoard(t *testing.T) {
	cloned := CloneRuntimeState(RuntimeState{})
	if cloned.TeamworkBoard != nil {
		t.Fatalf("没有计划时必须保持 nil（前端的整块退场判据），得到 %+v", cloned.TeamworkBoard)
	}
}
