package application_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuiltinCommandApplicationIsExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	for _, name := range []string{"command.go", "README.md"} {
		path := filepath.Join(applicationDir, "input", "command", name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("application/input/command/%s is required: %v", name, err)
		}
	}
}
