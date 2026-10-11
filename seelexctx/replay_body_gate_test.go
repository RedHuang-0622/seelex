package seelexctx

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
)

// TestHasConversationBodyTable 钉住"素材里有没有可压的对话正文"这组形状。
// 规则与装配层 application/core/context_runtime 的 hasConversationBody 是**同一条**：
// 两侧各有这份表（消息类型不同：types.Message ↔ contract.EngineMessage），
// 规则本身不允许分叉。
func TestHasConversationBodyTable(t *testing.T) {
	systemPrompt := textMessage("system", "product system prompt")
	plan := textMessage("system", "plan context")
	blank := textMessage("user", "   ")
	cases := []struct {
		name     string
		messages []types.Message
		want     bool
		wantRole string
	}{
		{name: "nil", messages: nil, want: false, wantRole: "空"},
		{name: "只有产品指令", messages: []types.Message{systemPrompt}, want: false, wantRole: "system×1"},
		{name: "只有控制行", messages: []types.Message{systemPrompt, plan}, want: false, wantRole: "system×2"},
		{name: "空白 user 行", messages: []types.Message{systemPrompt, blank}, want: false, wantRole: "system×1 user×1"},
		{name: "user 正文", messages: []types.Message{systemPrompt, textMessage("user", "读一下 ports.go")}, want: true},
		{name: "assistant 正文", messages: []types.Message{systemPrompt, textMessage("assistant", "第一段正文")}, want: true},
		{name: "assistant 宣告工具调用", messages: []types.Message{systemPrompt, replayAssistantCall(replayToolCall("t1", "read_file"))}, want: true},
		{name: "工具结果也是可压载荷", messages: []types.Message{systemPrompt, replayToolResult("t1", "read_file", "body")}, want: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := HasConversationBody(testCase.messages); got != testCase.want {
				t.Fatalf("HasConversationBody = %t, want %t（%+v）", got, testCase.want, testCase.messages)
			}
			if testCase.wantRole != "" {
				if got := ReplayRolesTerse(testCase.messages); got != testCase.wantRole {
					t.Fatalf("ReplayRolesTerse = %q, want %q", got, testCase.wantRole)
				}
			}
		})
	}
}

// TestMeasureReplayCoverageTable 钉住"素材盖不盖得住本次要压的区间"这一事实：
// 区间为空时无此事实、逐字命中才算覆盖、一条都对不上就是没覆盖。
func TestMeasureReplayCoverageTable(t *testing.T) {
	systemPrompt := textMessage("system", "product system prompt")
	rounds := roundHistory(2)
	cases := []struct {
		name          string
		material      []types.Message
		overflow      []types.Message
		wantUnits     int
		wantCovered   int
		wantEvidence  bool
		wantRefSuffix string
	}{
		{
			name:     "区间为空 → 无此事实",
			material: []types.Message{systemPrompt},
			overflow: nil,
		},
		{
			name:          "素材只有控制行 → 一条都对不上",
			material:      []types.Message{systemPrompt},
			overflow:      rounds,
			wantUnits:     4,
			wantCovered:   0,
			wantEvidence:  true,
			wantRefSuffix: "uncovered:0/4",
		},
		{
			name:        "素材含要压的区间 → 覆盖，不留痕",
			material:    append([]types.Message{systemPrompt}, rounds...),
			overflow:    rounds,
			wantUnits:   4,
			wantCovered: 4,
		},
		{
			name:        "素材含区间的一部分 → 覆盖（部分也算对得上）",
			material:    []types.Message{systemPrompt, rounds[0]},
			overflow:    rounds,
			wantUnits:   4,
			wantCovered: 1,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			coverage := MeasureReplayCoverage(testCase.material, testCase.overflow)
			if coverage.OverflowUnits != testCase.wantUnits || coverage.CoveredUnits != testCase.wantCovered {
				t.Fatalf("coverage = %+v, want units=%d covered=%d",
					coverage, testCase.wantUnits, testCase.wantCovered)
			}
			evidence := ReplayCoverageEvidence(coverage)
			if !testCase.wantEvidence {
				if len(evidence) != 0 {
					t.Fatalf("覆盖（或无此事实）时不得留痕：%+v", evidence)
				}
				return
			}
			if len(evidence) != 1 {
				t.Fatalf("盖不住时必须留一条证据：%+v", evidence)
			}
			if !strings.HasSuffix(evidence[0].Ref, testCase.wantRefSuffix) {
				t.Fatalf("证据 ref = %q, want 后缀 %q", evidence[0].Ref, testCase.wantRefSuffix)
			}
			if !strings.HasPrefix(evidence[0].Ref, CompactReplayCoverageEvidenceRefPrefix) {
				t.Fatalf("证据 ref 必须以 %q 开头：%q", CompactReplayCoverageEvidenceRefPrefix, evidence[0].Ref)
			}
		})
	}
}
