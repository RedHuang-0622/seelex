package team

// plugin_reject_test.go 是"未装配名字"判决的**域级**用例（P21-27：随实现从
// `backend/engine/assemble/runtime_teamwork_curated.go` 搬进本域）。

import (
	"errors"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/store"
)

// TestRejectUnassembledPluginsSkipsAssembledNames：已装插件不问（判定只发生在"确实未定义"时）。
func TestRejectUnassembledPluginsSkipsAssembledNames(t *testing.T) {
	asked := 0
	deps := UnassembledPluginDeps{
		Limit:   3,
		Defined: func(name string) bool { return name == "cad" },
		UnassembledReason: func(string) error {
			asked++
			return errors.New("不该问已装插件")
		},
	}
	if err := RejectUnassembledPlugins([]store.TeamworkMember{{Role: "exec", Plugins: []string{"cad"}}}, deps); err != nil {
		t.Fatalf("已装插件应当放行：%v", err)
	}
	if asked != 0 {
		t.Fatalf("已装插件不该被问原因：%d 次", asked)
	}
}

// TestRejectUnassembledPluginsPointsAtTheReason：未装名字必须**带着来源**被拒——
// 判据函数给什么理由就说什么理由（pending 路线图 / 两边都不在 / 精选目录没读到）。
func TestRejectUnassembledPluginsPointsAtTheReason(t *testing.T) {
	deps := UnassembledPluginDeps{
		Limit:   3,
		Defined: func(string) bool { return false },
		UnassembledReason: func(name string) error {
			return errors.New("插件 " + name + " 在精选目录（pending）里，尚未落盘")
		},
	}
	err := RejectUnassembledPlugins([]store.TeamworkMember{{Role: "exec", Plugins: []string{"docs"}}}, deps)
	if err == nil || !strings.Contains(err.Error(), "成员 \"exec\" 的插件装配被拒绝") ||
		!strings.Contains(err.Error(), "精选目录（pending）") {
		t.Fatalf("必须点名成员与来源：%v", err)
	}
}

// TestRejectUnassembledPluginsLeavesSyntaxErrorsToTheOtherRule：语法层的话（超限 / 重复）在这里
// **跳过**——语义错误不许盖住语法错误（规整报错就不进语义判定）。
func TestRejectUnassembledPluginsLeavesSyntaxErrorsToTheOtherRule(t *testing.T) {
	asked := 0
	deps := UnassembledPluginDeps{
		Limit:   2,
		Defined: func(string) bool { return false },
		UnassembledReason: func(string) error {
			asked++
			return errors.New("不该问语法非法的成员")
		},
	}
	over := []store.TeamworkMember{{Role: "exec", Plugins: []string{"a", "b", "c"}}}
	if err := RejectUnassembledPlugins(over, deps); err != nil {
		t.Fatalf("超限成员应当跳过（留给语法层说）：%v", err)
	}
	duplicated := []store.TeamworkMember{{Role: "exec", Plugins: []string{"a", " a "}}}
	if err := RejectUnassembledPlugins(duplicated, deps); err != nil {
		t.Fatalf("重复成员应当跳过（留给语法层说）：%v", err)
	}
	if asked != 0 {
		t.Fatalf("语法非法的成员不该进语义判定：%d 次", asked)
	}
}

// TestRejectUnassembledPluginsWithoutReasonIsNoOp：宿主没接判决面（Reason = nil）⇒ 整个函数空操作，
// 判据函数一次都不问（那正是"不带产品启动面"的用法）。
func TestRejectUnassembledPluginsWithoutReasonIsNoOp(t *testing.T) {
	asked := 0
	deps := UnassembledPluginDeps{
		Limit:   3,
		Defined: func(string) bool { asked++; return false },
	}
	members := []store.TeamworkMember{{Role: "exec", Plugins: []string{"unknown"}}}
	if err := RejectUnassembledPlugins(members, deps); err != nil {
		t.Fatalf("没有判决面应当空操作：%v", err)
	}
	if asked != 0 {
		t.Fatalf("没有判决面时不该问任何读数：%d 次", asked)
	}
}

// TestRejectUnassembledPluginsReturnsNilReasonWhenNobodyKnows：判决函数对某个名字给 nil（"这不归我判"）
// ⇒ 不拒绝该名字（口径仍是 ValidateMemberPlugins 那一句）。
func TestRejectUnassembledPluginsReturnsNilReasonWhenNobodyKnows(t *testing.T) {
	deps := UnassembledPluginDeps{
		Limit:             3,
		Defined:           func(string) bool { return false },
		UnassembledReason: func(string) error { return nil },
	}
	if err := RejectUnassembledPlugins([]store.TeamworkMember{{Role: "exec", Plugins: []string{"mystery"}}}, deps); err != nil {
		t.Fatalf("判据函数说“不归我判”时应当放行：%v", err)
	}
}
