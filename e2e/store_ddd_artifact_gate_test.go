package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStoreDDDMArtifactOwnershipGate 是 M3 的结构门禁：Blob/media/search 的
// 内容寻址、配额、GC、索引和物理路径只能由 store/artifact 实现。
func TestStoreDDDMArtifactOwnershipGate(t *testing.T) {
	root := repoRoot()
	artifactDir := filepath.Join(root, "store", "artifact")
	if _, err := os.Stat(filepath.Join(artifactDir, "README.md")); err != nil {
		t.Fatalf("store/artifact 缺少模块 README：%v", err)
	}
	for _, name := range []string{"artifact.go", "blob.go", "media.go", "search.go"} {
		if _, err := os.Stat(filepath.Join(artifactDir, name)); err != nil {
			t.Fatalf("store/artifact 缺少 owner 文件 %s：%v", name, err)
		}
	}
	for _, name := range []string{"big_tool_result.go", "media.go", "search_index.go"} {
		data, err := os.ReadFile(filepath.Join(root, "store", name))
		if err != nil {
			t.Fatalf("读取根适配器 %s：%v", name, err)
		}
		text := string(data)
		for _, forbidden := range []string{"sha256.Sum256", "filepath.WalkDir", "json.MarshalIndent", "os.OpenFile"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("根适配器 %s 仍含 artifact 实现标记 %q", name, forbidden)
			}
		}
	}
}
