package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStoreDDDMSessionOwnershipGate is the first M5 slice gate: shard naming and
// pending-tail classification must have a Session-domain owner.
func TestStoreDDDMSessionOwnershipGate(t *testing.T) {
	root := repoRoot()
	for _, name := range []string{"session.go", "README.md"} {
		if _, err := os.Stat(filepath.Join(root, "store", "session", name)); err != nil {
			t.Fatalf("store/session/%s 尚未落位：%v", name, err)
		}
	}
}
