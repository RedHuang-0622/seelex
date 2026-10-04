package plugin

import "testing"

import "github.com/RedHuang-0622/Seele/types"

func faceTestTools(names ...string) []types.Tool {
	tools := make([]types.Tool, 0, len(names))
	for _, name := range names {
		tools = append(tools, types.Tool{Type: "function", Function: types.ToolFunction{Name: name}})
	}
	return tools
}

func faceTestNames(tools []types.Tool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Function.Name)
	}
	return names
}

func faceTestSameNames(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

// TestFaceSingleDefMatchesFilter 钉住"同一套语义"：单元素装配集合与 Filter（全局
// 激活态）必须给出同一个结果集——按会话装配是 include/exclude 的推广，不是第二套
// 过滤规则（否则读数与 root 路径会对不上）。
func TestFaceSingleDefMatchesFilter(t *testing.T) {
	all := faceTestTools("cad_draw", "cad_view", "read_file", "write_file")
	manager := NewManager()
	if err := manager.Define("cad", "CAD", []string{"cad_*"}, []string{"cad_view"}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Activate("cad"); err != nil {
		t.Fatal(err)
	}
	defs, missing := manager.DefsFor([]string{"cad"})
	if len(missing) != 0 || len(defs) != 1 {
		t.Fatalf("cad 应已定义，得 defs=%v missing=%v", defs, missing)
	}
	got := faceTestNames(Face(defs, all))
	want := faceTestNames(manager.Filter(all))
	if !faceTestSameNames(got, want) {
		t.Fatalf("单元素集合与 Filter 必须同解：got %v want %v", got, want)
	}
	if !faceTestSameNames(want, []string{"cad_draw"}) {
		t.Fatalf("基座前提不成立（include cad_* 且 exclude cad_view）：%v", want)
	}
}

// TestDefsForReportsMissingNames 钉住"未知名可被显式拒绝"的读面：DefsFor 必须把
// 未定义的名字单独列出来（调用方据此裁决，而不是拿到一份静默缩小的定义集）。
func TestDefsForReportsMissingNames(t *testing.T) {
	manager := NewManager()
	if err := manager.Define("cad", "CAD", nil, nil); err != nil {
		t.Fatal(err)
	}
	if !manager.Defined("cad") || manager.Defined("ghost") {
		t.Fatal("Defined 必须区分已定义与未定义")
	}
	defs, missing := manager.DefsFor([]string{"cad", "ghost"})
	if len(defs) != 1 || len(missing) != 1 || missing[0] != "ghost" {
		t.Fatalf("得 defs=%v missing=%v", defs, missing)
	}
	var nilManager *Manager
	if nilManager.Defined("cad") {
		t.Fatal("nil Manager 不得报已定义")
	}
}

// TestFaceUnionOfIncludesAndExcludeVeto 钉住集合语义：include 取并集、exclude 取
// 并集硬拆（任一插件的 exclude 命中即否决），没写 include 的插件不设准入。
func TestFaceUnionOfIncludesAndExcludeVeto(t *testing.T) {
	defs := []Def{
		{Name: "alpha", Include: []string{"alpha_*", "shared"}},
		{Name: "beta", Include: []string{"beta_*"}, Exclude: []string{"shared"}},
	}
	all := faceTestTools("alpha_1", "beta_1", "shared", "other")
	if got := faceTestNames(Face(defs, all)); !faceTestSameNames(got, []string{"alpha_1", "beta_1"}) {
		t.Fatalf("并集 + 硬拆得 %v", got)
	}
	if VisibleName(defs, "shared") {
		t.Fatal("shared 被 beta 的 exclude 命中，必须否决（另一个插件 include 它也不行）")
	}
	if VisibleName(defs, "other") {
		t.Fatal("other 两个插件都没接纳，必须不可见")
	}
	wide := []Def{{Name: "wide"}}
	if !VisibleName(wide, "other") {
		t.Fatal("没写 include 的插件不设准入 = 接纳")
	}
	if !VisibleName(nil, "other") {
		t.Fatal("没装配插件时一律可见（不装配就不改变工具面）")
	}
	if got := Face(nil, all); len(got) != len(all) {
		t.Fatalf("空集合必须原样返回，得 %v", faceTestNames(got))
	}
}
