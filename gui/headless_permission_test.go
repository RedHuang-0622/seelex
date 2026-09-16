package gui

// headless 的 SetPermissionTier 分派用例（P1-2）：它是 chip / TUI 命令之外的第三个
// 运行期切档入口（脚本化/自动化）。分派有实现但无用例时，参数错误路径与"是否真的
// 到达 Application"都没人钉。

import (
	"strings"
	"testing"
)

func TestHeadlessSetPermissionTierDispatch(t *testing.T) {
	fake := newFakeApplication()
	base := newHeadlessTestServer(t, fake)

	result := headlessRPC(t, base, "SetPermissionTier", "full")
	if !result.OK {
		t.Fatalf("SetPermissionTier failed: %s", result.Error)
	}
	if fake.selectedTier != "full" {
		t.Fatalf("selectedTier = %q, want full（分派未到达 Application）", fake.selectedTier)
	}

	if result := headlessRPC(t, base, "SetPermissionTier"); result.OK || !strings.Contains(result.Error, "缺少参数") {
		t.Fatalf("SetPermissionTier without args should fail, got ok=%v error=%q", result.OK, result.Error)
	}
	if fake.selectedTier != "full" {
		t.Fatalf("参数错误不得改变档位：%q", fake.selectedTier)
	}
}
