package seelebridge

import (
	"strings"
	"testing"
)

// TestAdvisorSystemPromptPrefersRegisteredPrompt：登记了员工提示词就用它，未登记
// 回退内置角色设定；**两种情况都追加输出契约**（否则 goal 域解析不出指令）。
func TestAdvisorSystemPromptPrefersRegisteredPrompt(t *testing.T) {
	builtin := (&goalLLMEvaluator{}).advisorSystemPrompt(false)
	if !strings.Contains(builtin, goalAdvisorRolePrompt) || !strings.Contains(builtin, goalAdvisorOutputContract) {
		t.Fatalf("未登记提示词时必须用内置角色设定 + 输出契约：%q", builtin)
	}

	evaluator := &goalLLMEvaluator{promptProvider: func(roleName string) string {
		if roleName != advisorRoleName {
			t.Fatalf("提示词读面只应按 ADVISOR 角色名查询，实际 %q", roleName)
		}
		return "你是我司的评审官：先看测试证据，再看实现。"
	}}
	registered := evaluator.advisorSystemPrompt(true)
	if !strings.Contains(registered, "你是我司的评审官") {
		t.Fatalf("登记的提示词必须生效：%q", registered)
	}
	if strings.Contains(registered, goalAdvisorRolePrompt) {
		t.Fatalf("登记提示词必须替换内置角色设定：%q", registered)
	}
	if !strings.Contains(registered, goalAdvisorOutputContract) {
		t.Fatalf("输出契约必须永远追加：%q", registered)
	}

	// 读面返回空白 = 未登记（用内置兜底），不能让空提示词顶掉角色设定。
	blank := &goalLLMEvaluator{promptProvider: func(string) string { return "   " }}
	if !strings.Contains(blank.advisorSystemPrompt(false), goalAdvisorRolePrompt) {
		t.Fatalf("空白登记提示词应回退内置角色设定：%q", blank.advisorSystemPrompt(false))
	}
}

// TestAdvisorSystemPromptReportsActualToolBoundary：工具边界必须与实际执行面**同源**
// ——提示词说"你有只读工具"而实际没有（或反之），模型的行为就会与实际能力错位，
// 比没有工具更糟（它会声称自己核对过文件）。
func TestAdvisorSystemPromptReportsActualToolBoundary(t *testing.T) {
	evaluator := &goalLLMEvaluator{}
	withTools := evaluator.advisorSystemPrompt(true)
	withoutTools := evaluator.advisorSystemPrompt(false)
	if !strings.Contains(withTools, goalAdvisorToolingWithTools) || strings.Contains(withTools, goalAdvisorToolingWithoutTools) {
		t.Fatalf("有工具时只应出现「有工具」段：%q", withTools)
	}
	if !strings.Contains(withoutTools, goalAdvisorToolingWithoutTools) || strings.Contains(withoutTools, goalAdvisorToolingWithTools) {
		t.Fatalf("无工具时只应出现「无工具」段：%q", withoutTools)
	}
	if !strings.Contains(withTools, goalAdvisorOutputContract) || !strings.Contains(withoutTools, goalAdvisorOutputContract) {
		t.Fatal("两种口径都必须带输出契约（否则 goal 域解析不出指令）")
	}
}

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
	if got := runtime.rolePromptFor(advisorRoleName); got != "" {
		t.Fatalf("未注入读面应返回空串，实际 %q", got)
	}
	runtime.SetRolePromptProvider(func(string) string { return "  登记的提示词  " })
	if got := runtime.rolePromptFor(advisorRoleName); got != "登记的提示词" {
		t.Fatalf("读面结果必须 trim，实际 %q", got)
	}
	// nil Runtime 不 panic（装配期容错）。
	var nilRuntime *Runtime
	if got := nilRuntime.rolePromptFor(advisorRoleName); got != "" {
		t.Fatalf("nil Runtime 应返回空串，实际 %q", got)
	}
}
