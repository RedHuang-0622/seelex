package application_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInputCompletionPolicyLivesOutsideCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	assemblyFile := filepath.Join(applicationDir, "assembly", "completion.go")
	if _, err := os.Stat(assemblyFile); err == nil {
		t.Fatalf("input completion policy still lives in %s", assemblyFile)
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", assemblyFile, err)
	}
}
