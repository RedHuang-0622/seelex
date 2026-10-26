package admin

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestLatestVisibleUserGoal(t *testing.T) {
	long := strings.Repeat("字", 250)
	tests := []struct {
		name     string
		messages []model.Message
		want     string
	}{
		{name: "empty", messages: nil, want: ""},
		{
			name: "last user wins",
			messages: []model.Message{
				{Role: "user", Content: "first"},
				{Role: "assistant", Content: "answer"},
				{Role: "user", Content: "second"},
			},
			want: "second",
		},
		{
			name: "blank and subagent lines are skipped",
			messages: []model.Message{
				{Role: "user", Content: "real goal"},
				{Role: "user", Content: "   "},
				{Role: "user", Content: "[子代理产出] inner note"},
			},
			want: "real goal",
		},
		{
			name:     "truncated at 200 runes",
			messages: []model.Message{{Role: "user", Content: long}},
			want:     strings.Repeat("字", 200) + "…",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := LatestVisibleUserGoal(test.messages); got != test.want {
				t.Errorf("LatestVisibleUserGoal = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRuntimeProjectionConstructors(t *testing.T) {
	governance := &dto.GoalGovernanceView{Active: true, GoalID: "g1"}
	visibility := RuntimeVisibility(true, governance)
	if !visibility.GoalSkillActive || visibility.GoalGovernance != governance {
		t.Errorf("RuntimeVisibility = %+v, want goal skill active with the same governance pointer", visibility)
	}

	evidence := ParentEvidence("sess-1", []model.Message{{Role: "user", Content: "do it"}}, 9)
	if evidence.SessionID != "sess-1" || evidence.Goal != "do it" || evidence.ConversationCount != 9 {
		t.Errorf("ParentEvidence = %+v, want sess-1 / do it / 9", evidence)
	}
}
