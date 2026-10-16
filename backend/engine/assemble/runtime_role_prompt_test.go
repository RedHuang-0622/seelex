package assemble

import (
	"testing"

	goaldomain "github.com/RedHuang-0622/seelex/backend/multiagent/goal"
)

// 两条 ADVISOR 提示词用例（登记提示词优先 / 工具边界与实际执行面同源）随评估器本体搬进
// goal 域（`backend/multiagent/goal/advisor_evaluator_test.go`，P21-13）；本文件只留
// **宿主读面**（提示词优化与 `rolePromptFor`）的用例。

// TestParseRolePromptOptimization：优化回合的 JSON 解析容忍围栏/前后噪声，缺
// optimized 显式报错，notes 去空且截断到 5 条。
func TestParseRolePromptOptimization(t *testing.T) {
	optimized, notes, err := parseRolePromptOptimization("```json\n{\"optimized\":\"新提示词\",\"notes\":[\"补了输出格式\",\"\",\"   \"]}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if optimized != "新提示词" || len(notes) != 1 || notes[0] != "补了输出格式" {
		t.Fatalf("解析结果 = %q %v", optimized, notes)
	}

	if _, _, err := parseRolePromptOptimization("抱歉，我无法完成"); err == nil {
		t.Fatal("缺 JSON 必须报错")
	}
	if _, _, err := parseRolePromptOptimization(`{"notes":["x"]}`); err == nil {
		t.Fatal("缺 optimized 必须报错")
	}

	_, notes, err = parseRolePromptOptimization(`{"optimized":"x","notes":["1","2","3","4","5","6","7"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 5 {
		t.Fatalf("notes 必须截断到 5 条，实际 %v", notes)
	}
}

// TestRolePromptProviderDefaultsToEmpty：未注入读面时 rolePromptFor 返回空串
// （调用方回退内置），不让 nil 读面 panic。
func TestRolePromptProviderDefaultsToEmpty(t *testing.T) {
	runtime := &Runtime{}
	if got := runtime.rolePromptFor(goaldomain.AdvisorRoleName); got != "" {
		t.Fatalf("未注入读面应返回空串，实际 %q", got)
	}
	runtime.SetRolePromptProvider(func(string) string { return "  登记的提示词  " })
	if got := runtime.rolePromptFor(goaldomain.AdvisorRoleName); got != "登记的提示词" {
		t.Fatalf("读面结果必须 trim，实际 %q", got)
	}
	// nil Runtime 不 panic（装配期容错）。
	var nilRuntime *Runtime
	if got := nilRuntime.rolePromptFor(goaldomain.AdvisorRoleName); got != "" {
		t.Fatalf("nil Runtime 应返回空串，实际 %q", got)
	}
}
