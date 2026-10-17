package registry

import (
	"context"
	"testing"
)

// TestProjectScopedToolNameCoversExactlyTheRegisteredSet 是**跨实现一致性**用例：注册面
// （`Router.Register` 实际登记的名字）与判据面（`ProjectScopedToolName`）必须指向同一份名单——
// 名单两处各写一遍就会漂移（新加的项目作用域工具被晚注册顶掉，或莫名其妙被拒）。
func TestProjectScopedToolNameCoversExactlyTheRegisteredSet(t *testing.T) {
	registered := map[string]bool{}
	router := NewRouter(Deps{RegisterTool: func(name, _ string, _ map[string]interface{},
		_ func(context.Context, string) (string, error)) {
		registered[name] = true
	}})
	router.Register()

	for name := range projectScopedToolNames {
		if !registered[name] {
			t.Fatalf("判据面认为是项目作用域工具，但注册面没有它：%q", name)
		}
	}
	// 反向：注册面里的这六个名字必须都被判据面认领（非项目作用域的工具不在本用例范围，
	// 但项目作用域那几个一个都不能漏）。
	for _, name := range []string{ReadFileToolName, GrepSearchToolName, GlobToolName,
		WriteFileToolName, EditFileToolName, BashToolName} {
		if !registered[name] {
			t.Fatalf("注册面没有登记 %q（常量与注册点漂移了）", name)
		}
		if !ProjectScopedToolName(name) {
			t.Fatalf("%q 必须被判为项目作用域工具", name)
		}
	}
}

// TestProjectScopedToolNameRejectsOthers：名单之外的（含大小写变体、前缀相似）一律不是——
// 工具名是契约面，不做模糊匹配。
func TestProjectScopedToolNameRejectsOthers(t *testing.T) {
	for _, name := range []string{"", "Read_File", "read", "bash_bg", "read_batch", "team_plan", "bg_manage"} {
		if ProjectScopedToolName(name) {
			t.Fatalf("%q 不该被判为项目作用域工具", name)
		}
	}
}
