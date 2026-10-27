package application_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// TestSessionResidencyRuntimeLivesInSessionPackage keeps both LRU orders and
// release-guard orchestration out of application/assembly.
func TestSessionResidencyRuntimeLivesInSessionPackage(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	for _, name := range []string{"resident_lru.go", "content_lru.go", "session_release.go"} {
		path := filepath.Join(applicationDir, "assembly", name)
		if _, err := os.Stat(path); err == nil {
			t.Errorf("application/assembly/%s 仍存在；Session residency runtime 必须由 application/session/residency 拥有", name)
		} else if !os.IsNotExist(err) {
			t.Fatalf("检查 %s: %v", path, err)
		}
	}
	runtimePath := filepath.Join(applicationDir, "session", "residency", "runtime.go")
	if _, err := parser.ParseFile(token.NewFileSet(), runtimePath, nil, 0); err != nil {
		t.Fatalf("读取 Session residency runtime %s: %v", runtimePath, err)
	}
}
