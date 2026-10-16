package assemble

import (
	"testing"

	goaldomain "github.com/RedHuang-0622/seelex/backend/multiagent/goal"
)

// 两条 ADVISOR 提示词用例（登记提示词优先 / 工具边界与实际执行面同源）随评估器本体搬进
// goal 域（`backend/multiagent/goal/advisor_evaluator_test.go`，P21-13）；本文件只留
// **宿主读面**（提示词优化与 `rolePromptFor`）的用例。

// 优化回合的 JSON 解析用例随实现搬进提示词域
// （`backend/engine/prompt/role_prompt_optimize_test.go`，P21-15）；本文件只留**读面**
// （`rolePromptFor`）的宿主用例。

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
