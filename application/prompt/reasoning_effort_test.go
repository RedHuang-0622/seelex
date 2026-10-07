package prompt

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// TestReasoningEffortForMapsLevelsToWireVocabulary 钉住"档位 → provider 词表"的
// 唯一映射。档位名与 wire 值**不是同一套词**（lite 不等于 low 的名字，只是意
// 义上对应），所以这张表必须存在且只能有一份。
//
// 同时钉住：未知档位返回空串——空值不上线，provider 走自己的默认；
// 这里绝不替用户猜一个强度。
func TestReasoningEffortForMapsLevelsToWireVocabulary(t *testing.T) {
	cases := []struct {
		level string
		want  string
	}{
		{"lite", dto.ReasoningEffortLow},
		{"medium", dto.ReasoningEffortMedium},
		{"high", dto.ReasoningEffortHigh},
		{"max", dto.ReasoningEffortMax},
		{"HIGH", dto.ReasoningEffortHigh}, // 大小写与空白按既有口径归一
		{" high ", dto.ReasoningEffortHigh},
		{"nonsense", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := ReasoningEffortFor(c.level); got != c.want {
			t.Errorf("ReasoningEffortFor(%q) = %q, want %q", c.level, got, c.want)
		}
	}
}

// TestEffortProfilesCarryReasoningEffort 钉住"每一档都必须有思考强度"：
// reasoningEffort 是 effortProfile 的字段而不是散在外面的 if，漏填一档会在这里
// 变红，而不是等到线上"某一档怎么都不生效"才发现。
func TestEffortProfilesCarryReasoningEffort(t *testing.T) {
	for level, profile := range effortProfiles {
		if profile.reasoningEffort == "" {
			t.Errorf("effort profile %q has no reasoningEffort", level)
		}
		if !dto.ValidReasoningEffort(profile.reasoningEffort) {
			t.Errorf("effort profile %q has an unknown reasoningEffort %q", level, profile.reasoningEffort)
		}
	}
}
