package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStoreDDDMContextOwnershipGate is the first M7 slice gate.
func TestStoreDDDMContextOwnershipGate(t *testing.T) {
	root := repoRoot()
	for _, name := range []string{"context.go", "README.md"} {
		if _, err := os.Stat(filepath.Join(root, "store", "context", name)); err != nil {
			t.Fatalf("store/context/%s 尚未落位：%v", name, err)
		}
	}
}
