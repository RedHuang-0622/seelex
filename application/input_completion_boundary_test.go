package application_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInputCompletionPolicyLivesOutsideCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	coreFile := filepath.Join(applicationDir, "core", "completion.go")
	if _, err := os.Stat(coreFile); err == nil {
		t.Fatalf("input completion policy still lives in %s", coreFile)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", coreFile, err)
	}
}
