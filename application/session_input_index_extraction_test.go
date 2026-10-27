package application_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionInputIndexRuntimeLivesInSessionQuery(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	legacy := filepath.Join(applicationDir, "assembly", "session_input_index.go")
	if _, err := os.Stat(legacy); err == nil {
		t.Fatalf("application/assembly/session_input_index.go still exists")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat %s: %v", legacy, err)
	}
}
