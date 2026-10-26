package dto_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

func TestTeamworkPlanContractWireShapeAndFreeFunctions(t *testing.T) {
	plan := dto.TeamworkPlan{
		TeamID:  "team-1",
		Version: 2,
		Members: []dto.TeamworkMember{{
			Role:          "exec",
			RoleSessionID: "session-exec",
			Plugins:       []string{"docs"},
		}},
		Milestones: []dto.TeamworkMilestone{{
			ID:        "m1",
			DependsOn: []string{"m0"},
			Items: []dto.TeamworkWorkItem{{
				ID:        "wi1",
				Milestone: "m1",
				Role:      "exec",
				Name:      "build",
				DependsOn: []string{"wi0"},
			}},
		}},
		State: dto.TeamworkState{
			State:        dto.TeamworkStateClosed,
			ClosedReason: "done",
		},
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	text := string(raw)
	for _, field := range []string{
		`"team_id"`,
		`"role_session_id"`,
		`"plugins"`,
		`"milestones"`,
		`"depends_on"`,
		`"closed_reason"`,
	} {
		if !strings.Contains(text, field) {
			t.Fatalf("JSON 缺少持久化字段 %s: %s", field, text)
		}
	}

	if !dto.TeamworkPlanIsEmpty(dto.TeamworkPlan{}) {
		t.Fatal("空 TeamID 必须被识别为空计划")
	}
	if dto.TeamworkPlanIsEmpty(plan) {
		t.Fatal("已声明 TeamID 的计划不得被识别为空计划")
	}
	item := dto.TeamworkWorkItem{}
	if got := dto.TeamworkWorkItemStatusOrPending(item); got != dto.TeamworkItemPending {
		t.Fatalf("空工作项状态 = %q，want %q", got, dto.TeamworkItemPending)
	}
	item.Status = dto.TeamworkItemDone
	if !dto.TeamworkWorkItemTerminal(item) {
		t.Fatal("done 工作项必须被识别为终态")
	}
}
