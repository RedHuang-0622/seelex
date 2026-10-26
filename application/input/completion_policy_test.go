package input

import (
	"strings"
	"testing"
)

func TestUnknownCommandNoticeUsesInputOwnedCatalog(t *testing.T) {
	application := New(Deps{
		CommandSuggestions: func() []Suggestion {
			return []Suggestion{
				{Text: "compact", Kind: SuggestionKindCommand},
				{Text: "help", Kind: SuggestionKindCommand},
			}
		},
		HasVisibleTool: func(name string) bool {
			return name == "compact_context" || name == "bash"
		},
	})

	cases := []struct {
		name string
		want []string
	}{
		{name: "compact_context", want: []string{"模型侧工具", "不能从输入框直接执行", "/compact"}},
		{name: "bash", want: []string{"模型侧工具", "不能从输入框直接执行"}},
		{name: "comapct", want: []string{"你是想用 /compact 吗"}},
		{name: "nonsense_xyz", want: []string{"未知命令: nonsense_xyz", "/help"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			notice := application.UnknownCommandNotice(testCase.name)
			for _, want := range testCase.want {
				if !strings.Contains(notice, want) {
					t.Fatalf("notice %q does not contain %q", notice, want)
				}
			}
			if testCase.name == "bash" && strings.Contains(notice, "你是想用") {
				t.Fatalf("tool-only input must not be mapped to a command: %q", notice)
			}
			if testCase.name == "nonsense_xyz" &&
				(strings.Contains(notice, "模型侧工具") || strings.Contains(notice, "你是想用")) {
				t.Fatalf("unrelated input must not invent a hint: %q", notice)
			}
		})
	}
}

func TestSigilMigrationHintUsesInputOwnedCatalog(t *testing.T) {
	application := New(Deps{
		HasPlugin: func(name string) bool { return name == "planner" },
		HasSkill:  func(name string) bool { return name == "review" },
	})
	if got := application.SigilMigrationHint(SigilSkill, "planner"); !strings.Contains(got, "#planner") {
		t.Fatalf("plugin migration hint = %q", got)
	}
	if got := application.SigilMigrationHint(SigilPlugin, "review"); !strings.Contains(got, "$review") {
		t.Fatalf("skill migration hint = %q", got)
	}
	if got := application.SigilMigrationHint(SigilSkill, "missing"); got != "" {
		t.Fatalf("unknown name hint = %q, want empty", got)
	}
}
