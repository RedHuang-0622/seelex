package model

// teamwork_board_member_assembly_clone_test.go — 团队看板成员行的**装配两格**进快照后的
// 深拷贝口径（2026-10-05）。
//
// 为什么单钉：`TeamworkMemberView` 新增的 `Assembly` 是**指针**（`*dto.PluginAssemblyView`），
// 而上一版 CloneTeamworkBoardView 只做了 `append([]dto.TeamworkMemberView(nil), members...)`
// ——浅拷贝下两个读者（GUI 与 TUI 各持一份快照）共享同一条读数，任何一方改一下就串到另一方。
// 声明面切片（`Plugins`）与读数里的两处切片（`plugins` / `plugin_face_missing`）同理。

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

func assemblyMemberFixture() RuntimeState {
	return RuntimeState{
		TeamworkBoard: &dto.TeamworkBoardView{
			TeamID: "team-board-assembly",
			Members: []dto.TeamworkMemberView{{
				Role: "exec", Status: "free",
				Plugins: []string{"docs"},
				Assembly: &dto.PluginAssemblyView{
					Role: "exec", Mode: "replace", Plugins: []string{"docs"}, PluginCount: 1,
					PluginFaceMissing: []string{"ghost"},
				},
			}},
		},
	}
}

func TestCloneRuntimeStateDeepCopiesMemberAssembly(t *testing.T) {
	runtime := assemblyMemberFixture()
	cloned := CloneRuntimeState(runtime)

	if cloned.TeamworkBoard.Members[0].Assembly == runtime.TeamworkBoard.Members[0].Assembly {
		t.Fatal("装配读数是指针：克隆必须换新值（否则两个读者共享同一条读数）")
	}

	// 声明面（内嵌切片）。
	cloned.TeamworkBoard.Members[0].Plugins[0] = "改了"
	if runtime.TeamworkBoard.Members[0].Plugins[0] != "docs" {
		t.Fatal("声明面（内嵌切片）必须独立")
	}
	// 读数里的两处切片。
	cloned.TeamworkBoard.Members[0].Assembly.Plugins[0] = "改了"
	if runtime.TeamworkBoard.Members[0].Assembly.Plugins[0] != "docs" {
		t.Fatal("读数里的声明面（内嵌切片）必须独立")
	}
	cloned.TeamworkBoard.Members[0].Assembly.PluginFaceMissing[0] = "改了"
	if runtime.TeamworkBoard.Members[0].Assembly.PluginFaceMissing[0] != "ghost" {
		t.Fatal("失灵名单（内嵌切片）必须独立")
	}
	// 读数本体（标量字段）。
	cloned.TeamworkBoard.Members[0].Assembly.Mode = "inherit-host"
	if runtime.TeamworkBoard.Members[0].Assembly.Mode != "replace" {
		t.Fatal("读数本体必须独立")
	}
}

// TestCloneTeamworkBoardViewKeepsNilAssembly 钉降温路径：没有读数（nil）的成员行克隆后
// 仍是 nil——前端据此不显示装配格，不能被克隆成一个零值读数（那会被读成"0 个工具"）。
func TestCloneTeamworkBoardViewKeepsNilAssembly(t *testing.T) {
	view := &dto.TeamworkBoardView{Members: []dto.TeamworkMemberView{{Role: "pm"}}}
	cloned := CloneTeamworkBoardView(view)
	if cloned.Members[0].Assembly != nil {
		t.Fatalf("没有读数的成员克隆后必须仍是 nil，得 %+v", cloned.Members[0].Assembly)
	}
	if cloned.Members[0].Plugins != nil {
		t.Fatalf("空声明克隆后必须仍是 nil（空 = 不覆盖），得 %v", cloned.Members[0].Plugins)
	}
}
