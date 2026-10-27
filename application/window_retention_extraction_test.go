package application_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContextRetentionPolicyLivesInWindowPackage(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	if _, err := os.Stat(filepath.Join(applicationDir, "assembly", "window.go")); err == nil {
		t.Error("application/assembly/window.go 仍存在；context retention policy 必须归 application/window")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(applicationDir, "window", "retention.go")); err != nil {
		t.Fatalf("application/window/retention.go missing: %v", err)
	}
}
