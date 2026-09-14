package core

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// ② 的应用层接线：会话端口实现可选 floor 读面时，Agent Team 成员表带上
// floor_role（前端「floor 高亮」的数据源）；未实现时留空而不报错。

// floorRecordingSessions 在 A2A 装配桩上补出可选 floor 读面。
type floorRecordingSessions struct {
	teamRecordingSessions
	floorRole string
}

func (s *floorRecordingSessions) ReadFloorRole(string) (string, error) {
	return s.floorRole, nil
}

func TestAgentTeamViewCarriesFloorRole(t *testing.T) {
	sessions := &floorRecordingSessions{floorRole: "tl"}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))

	if _, err := service.MaterializeAgentTeamPreset("sess-floor", dto.TeamKindGoalA2A, 0); err != nil {
		t.Fatalf("MaterializeAgentTeamPreset: %v", err)
	}
	view, err := service.AgentTeamView("sess-floor")
	if err != nil {
		t.Fatalf("AgentTeamView: %v", err)
	}
	if view.FloorRole != "tl" {
		t.Fatalf("floor_role = %q, want tl（成员表读面必须带上当前发言角色）", view.FloorRole)
	}
}

// TestAgentTeamViewWithoutFloorPortStaysEmpty 钉住降级：宿主没有 floor 读面
// （旧端口实现）时成员表照常返回，floor 留空。
func TestAgentTeamViewWithoutFloorPortStaysEmpty(t *testing.T) {
	sessions := &teamRecordingSessions{}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))

	if _, err := service.MaterializeAgentTeamPreset("sess-no-floor", dto.TeamKindGoalA2A, 0); err != nil {
		t.Fatalf("MaterializeAgentTeamPreset: %v", err)
	}
	view, err := service.AgentTeamView("sess-no-floor")
	if err != nil {
		t.Fatalf("AgentTeamView 不该因缺 floor 读面失败: %v", err)
	}
	if view.FloorRole != "" {
		t.Fatalf("无 floor 读面时 floor_role 应留空, 得 %q", view.FloorRole)
	}
	if !view.Configured {
		t.Fatalf("成员表其余字段不受影响: %+v", view)
	}
}
