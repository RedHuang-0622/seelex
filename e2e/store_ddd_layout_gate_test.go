package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStoreDDDMLayoutOwnershipGate 是 M2 的结构红灯：路径、head 信封与模块锁
// 机制必须先落到 store/internal/layout，根 storeEngine 只保留领域重建策略。
func TestStoreDDDMLayoutOwnershipGate(t *testing.T) {
	root := repoRoot()
	layoutDir := filepath.Join(root, "store", "internal", "layout")
	entries, err := os.ReadDir(layoutDir)
	if err != nil {
		t.Fatalf("store/internal/layout 尚未成为布局机制 owner：%v", err)
	}
	hasProduction := false
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") {
			hasProduction = true
			break
		}
	}
	if !hasProduction {
		t.Fatal("store/internal/layout 没有生产实现")
	}
	if _, err := os.Stat(filepath.Join(layoutDir, "README.md")); err != nil {
		t.Fatalf("store/internal/layout 缺少 README：%v", err)
	}

	source, err := os.ReadFile(filepath.Join(root, "store", "module_heads.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "layout.") {
		t.Fatal("根 storeEngine 没有委托 store/internal/layout")
	}
}
