package view

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

func TestRenderDiagReadsRuntimeReadings(t *testing.T) {
	snapshot := model.Snapshot{
		Runtime: model.RuntimeState{
			Provider: "provider-x",
			Model:    "model-y",
			Effort:   "high",
			Tokens:   "1.2k",
			Account:  "acct-1",
			Plugin:   "plug-a",
			Plan: &model.PlanState{
				Name:   "MyPlan",
				Status: model.PlanRunning,
				Nodes: []model.PlanNode{
					{Status: model.NodeCompleted},
					{Status: model.NodeSkipped},
					{Status: model.NodePending},
				},
			},
			Plugins: []model.PluginInfo{
				{Name: "plug-a", Description: "desc-a"},
				{Name: "plug-b"},
			},
			Accounts: []model.AccountInfo{
				{Name: "on", Provider: "p1", Model: "m1"},
				{Name: "off", Provider: "p2", Model: "m2", Disabled: true},
			},
			Skills: []model.SkillInfo{{Name: "skill-a"}, {Name: "skill-b"}},
		},
	}

	text := RenderDiag(snapshot)
	for _, want := range []string{
		"System Diagnostics",
		"Provider: provider-x  Model: model-y  Effort: high  Tokens: 1.2k",
		"Session: acct-1",
		// 完成计数口径 = completed + skipped（2/3），第三节点是 pending，不计入。
		"Plan: MyPlan (running, 2/3 nodes)",
		"Active: plug-a (2 total)",
		"★ plug-a",
		"—",
		"2 configured",
		"✓ on",
		"✗ off",
		"2 loaded",
		"skill-a  skill-b",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("RenderDiag output missing %q\n---\n%s", want, text)
		}
	}
}

func TestRenderDiagWithoutRuntimeSections(t *testing.T) {
	text := RenderDiag(model.Snapshot{})
	for _, want := range []string{
		"Active: none (0 total)",
		"0 configured",
		"0 loaded",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("RenderDiag output missing %q\n---\n%s", want, text)
		}
	}
	if strings.Contains(text, "Plan:") {
		t.Errorf("RenderDiag printed a plan section without a plan:\n%s", text)
	}
}

func TestPerfStatsCountsConversationAndArchive(t *testing.T) {
	snapshot := model.Snapshot{
		Revision: 7,
		Conversation: []model.Message{
			{Role: "user", Content: "hello"},
			{Role: "tool", Content: "x", Tool: &model.ToolCall{Arguments: "a", Result: "rr", Truncated: true}},
		},
	}
	refs := []model.ToolResultRef{{Size: 100}, {Size: 23}}

	stats := PerfStats(snapshot, 42, refs)
	if stats.ConversationMessages != 2 {
		t.Errorf("ConversationMessages = %d, want 2", stats.ConversationMessages)
	}
	// 5 ("hello") + 1 + 1 + 2 ("x" + 参数 + 结果)
	if stats.ConversationChars != 9 {
		t.Errorf("ConversationChars = %d, want 9", stats.ConversationChars)
	}
	if stats.LargestMessageChars != 5 {
		t.Errorf("LargestMessageChars = %d, want 5", stats.LargestMessageChars)
	}
	if stats.TruncatedOutputs != 1 {
		t.Errorf("TruncatedOutputs = %d, want 1", stats.TruncatedOutputs)
	}
	if stats.HistoryWindow != 42 {
		t.Errorf("HistoryWindow = %d, want 42 (host-supplied)", stats.HistoryWindow)
	}
	if stats.Revision != 7 {
		t.Errorf("Revision = %d, want 7", stats.Revision)
	}
	if stats.ToolResults != 2 || stats.ArchivedBytes != 123 {
		t.Errorf("archive readings = (%d, %d), want (2, 123)", stats.ToolResults, stats.ArchivedBytes)
	}
	if stats.SnapshotBytes <= 0 {
		t.Errorf("SnapshotBytes = %d, want > 0 (JSON estimate)", stats.SnapshotBytes)
	}
}

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
