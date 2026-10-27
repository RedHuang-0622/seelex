package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStoreDDDMCommitOwnershipGate 是 M4 的结构门槛：跨聚合写序由
// application/conversation 的 TurnStoreCommit 编排，store 只暴露三个单聚合提交。
func TestStoreDDDMCommitOwnershipGate(t *testing.T) {
	root := repoRoot()
	commitFile := filepath.Join(root, "application", "conversation", "turn_store_commit.go")
	if _, err := os.Stat(commitFile); err != nil {
		t.Fatalf("application TurnStoreCommit 尚未落位：%v", err)
	}
	source, err := os.ReadFile(filepath.Join(root, "store", "commit_writer.go"))
	if err != nil {
		t.Fatalf("store 单聚合提交器尚未落位：%v", err)
	}
	text := string(source)
	for _, method := range []string{"WriteArtifacts(", "WriteMessages(", "WriteCompactions("} {
		if !strings.Contains(text, method) {
			t.Fatalf("store commit writer 缺少 %s", method)
		}
	}
	if strings.Contains(text, "WriteCommit(") {
		t.Fatal("新 commit writer 不得重新暴露跨聚合 WriteCommit")
	}
}
