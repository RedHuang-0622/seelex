package seelebridge

import "testing"

// 打点 K-0 的**运行时读面**验收：装配后的注册表里，每个 seelex 注册的工具都
// 带着簇属声明（Groups 非空），且没有"注册了却没分封"的名字。
//
// 与根包 tool_meta_declaration_test.go 的分工：那边扫源码，覆盖 main.go 里那些
// 不在 RegisterBuiltins 路径上的工具（read_tool_result / switch_plugin / …）；
// 这边装配真注册表，覆盖"路径通了但没走到 AddInline 填表"这类装配型失效——
// 例如某天有人新写一个 AddInline 的兄弟函数忘了填 Meta，源码扫描看不出来。
func TestBuiltinToolsDeclareGroupsAndMetas(t *testing.T) {
	runtime := newTestRuntime(t)
	runtime.RegisterBuiltins()

	if undeclared := runtime.UndeclaredTools(); len(undeclared) != 0 {
		t.Errorf("以下内联工具没有簇属声明（未分封 = 没有权限策略、也没有并发分类）：%v", undeclared)
	}
	metas := runtime.ToolMetas()
	if len(metas) == 0 {
		t.Fatal("装配后一个内联工具都没有：用例本身失效，比断言失败更糟")
	}
	for name, meta := range metas {
		if len(meta.Groups) == 0 {
			t.Errorf("工具 %s 的 ToolsMeta.Groups 为空：权限判定会退回按名字路由，声明形同不存在", name)
		}
	}
	// 代表工具点名（覆盖不同的注册路径与组，防止"表填了但填错组"）。
	cases := []struct {
		tool  string
		group string
	}{
		{"read_file", "ro"},
		{"bash", "rw"},
		{"todo_init", "rw"},
		{"fork_subagents", "ctl"},
	}
	for _, tc := range cases {
		meta, ok := metas[tc.tool]
		if !ok {
			t.Errorf("装配后没有 %s：注册路径断了", tc.tool)
			continue
		}
		if len(meta.Groups) != 1 || meta.Groups[0] != tc.group {
			t.Errorf("%s 的声明簇 = %v, want [%s]", tc.tool, meta.Groups, tc.group)
		}
	}
}
