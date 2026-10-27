package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStoreDDDMBoardOwnershipGate 是 M1 的红灯门禁：项目看板实现必须先落到
// store/board，根包只能保留同身份 alias 与单层转发。
func TestStoreDDDMBoardOwnershipGate(t *testing.T) {
	root := repoRoot()
	boardDir := filepath.Join(root, "store", "board")
	entries, err := os.ReadDir(boardDir)
	if err != nil {
		t.Fatalf("store/board 尚未成为看板 owner：%v", err)
	}
	hasImplementation := false
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") {
			hasImplementation = true
			break
		}
	}
	if !hasImplementation {
		t.Fatal("store/board 没有生产实现文件")
	}

	for _, name := range []string{"project_board.go", "project_board_markdown.go"} {
		if _, err := os.Stat(filepath.Join(root, "store", name)); err == nil {
			t.Fatalf("根包仍保留 %s：M1 后只能有薄转发，不能保留第二份实现", name)
		}
	}
}
