package agentteam

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// TestNormalizeRoleNormalizesPlugins 钉住写入侧的**语法**口径（与编排侧共用
// dto.NormalizePlugins）：去空白/去空项、**重复声明显式拒绝**、超上限显式拒绝、
// 空/缺失保持"不覆盖"（nil，而不是空切片）。
//
// 重复那一格是 2026-10-05 对齐的（对抗复核 D）：修前这里静默去重，于是"唯一生效的
// 口径"是静默合并，而计划/存储层写的是"显式拒绝"——两边相反。
func TestNormalizeRoleNormalizesPlugins(t *testing.T) {
	role, err := NormalizeRole(dto.RoleSpec{RoleName: "exec", Plugins: []string{" cad ", "", "docs"}})
	if err != nil {
		t.Fatalf("合法装配不得被拒：%v", err)
	}
	if len(role.Plugins) != 2 || role.Plugins[0] != "cad" || role.Plugins[1] != "docs" {
		t.Fatalf("规整后得 %#v", role.Plugins)
	}

	if _, err := NormalizeRole(dto.RoleSpec{
		RoleName: "exec",
		Plugins:  []string{"cad", " cad ", "docs"},
	}); err == nil || !strings.Contains(err.Error(), "重复") {
		t.Fatalf("重复声明必须在写入侧显式拒绝（不静默去重），得 %v", err)
	}

	if _, err := NormalizeRole(dto.RoleSpec{
		RoleName: "exec",
		Plugins:  []string{"a", "b", "c", "d"},
	}); err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("超上限必须在写入侧显式拒绝，得 %v", err)
	}

	inherited, err := NormalizeRole(dto.RoleSpec{RoleName: "exec"})
	if err != nil || inherited.Plugins != nil {
		t.Fatalf("空/缺失 = 不覆盖（nil），得 (%v, %#v)", err, inherited.Plugins)
	}
}
