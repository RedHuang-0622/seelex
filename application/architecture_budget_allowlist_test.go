package application_test

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

// TestCoreInternalDependencyAllowListHasNoStaleEntries 钉住允许清单与实测直接依赖
// 双向一致。只检查“实测 ⊆ 清单”会让已迁出的依赖永久留在预算里，掩盖下一批可
// 继续收紧的边界。
func TestCoreInternalDependencyAllowListHasNoStaleEntries(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	var budget applicationBudget
	readJSON(t, filepath.Join(applicationDir, "testdata", "application_budget.json"), &budget)

	got := directInternalDependencies(t, filepath.Join(applicationDir, "core"))
	want := append([]string(nil), budget.AllowedCoreInternalDependencies...)
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) || (len(got) > 0 && !reflect.DeepEqual(got, want)) {
		t.Fatalf("application/core 内部依赖允许清单与实测不一致：\n got=%v\nwant=%v",
			got, want)
	}
	if len(got) != budget.CoreInternalDependencyLimit {
		t.Fatalf("application/core 直接内部依赖 = %d，预算 = %d：预算必须随白名单同步收紧",
			len(got), budget.CoreInternalDependencyLimit)
	}
}
