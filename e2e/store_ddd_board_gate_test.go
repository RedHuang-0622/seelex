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

	compat, err := os.ReadFile(filepath.Join(root, "store", "project_board_compat.go"))
	if err != nil {
		t.Fatalf("读取根兼容文件：%v", err)
	}
	compatText := string(compat)
	for _, forbidden := range []string{"json.Unmarshal", "os.OpenFile", "sort.Slice", "sync.Map", "TruncateCrashTail"} {
		if strings.Contains(compatText, forbidden) {
			t.Fatalf("根兼容文件出现业务/IO 实现标记 %q：只允许 alias 与单层转发", forbidden)
		}
	}
	if strings.Count(compatText, "storeboard.Fold(") != 1 || strings.Count(compatText, "storeboard.Append(") != 1 {
		t.Fatal("根兼容文件必须只保留一份向 store/board 的转发，不能复制折叠/追加实现")
	}

	if _, err := os.Stat(filepath.Join(boardDir, "README.md")); err != nil {
		t.Fatalf("store/board 缺少模块 README：%v", err)
	}
}
