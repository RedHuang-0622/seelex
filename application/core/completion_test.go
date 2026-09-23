package core

import (
	"testing"
)

// TestSuggestionsExcludeModelSideTools：`/` 只列"能从输入框直接执行"的入口——
// 命令与 Skill。工具即使对模型可见也不进建议面（2026-09-23 口径修订）：一个能力
// 要让用户打 `/名字` 显式调用，前提是它已注册成命令，否则它就只有模型那一个调用方。
// 因此这里同时钉两面：工具名不在候选里（`compact_context`、`read`），同能力的命令
// 入口必须在（`compact`）——只删不补会让用户彻底找不到压缩入口。
// 打错到工具名时的补救说明由 unknownCommandNotice 负责（见
// input_unknown_command_notice_test.go），不靠把工具列进面板来"提醒"。
func TestSuggestionsExcludeModelSideTools(t *testing.T) {
	runtime := &fakeRuntime{visibleTools: []Tool{
		{Name: "compact_context", Description: "fold the transcript now"},
		{Name: "read", Description: "读文件"},
		{Name: "todolist_init", Description: "初始化待办（旧名）"},
	}}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))
	defer service.Shutdown()

	suggestions := service.Suggestions("/")
	if len(suggestions) == 0 {
		t.Fatal("/ 面板不该为空")
	}
	kinds := make(map[string]string, len(suggestions))
	for _, suggestion := range suggestions {
		kinds[suggestion.Text] = suggestion.Kind
	}
	for _, name := range []string{"compact_context", "read", "todolist_init"} {
		if kind, listed := kinds[name]; listed {
			t.Fatalf("工具 %q 不该出现在 / 面板（kind=%q）：%v", name, kind, kinds)
		}
	}
	for name, want := range map[string]string{
		"compact": SuggestionKindCommand,
		"help":    SuggestionKindCommand,
		"review":  SuggestionKindSkill,
	} {
		if kinds[name] != want {
			t.Fatalf("/ 面板里 %q 的 kind = %q, want %q：%v", name, kinds[name], want, kinds)
		}
	}
	for _, suggestion := range suggestions {
		if suggestion.Kind != SuggestionKindCommand && suggestion.Kind != SuggestionKindSkill {
			t.Fatalf("/ 面板混进了不可执行域 %q（kind=%q）", suggestion.Text, suggestion.Kind)
		}
	}
}
