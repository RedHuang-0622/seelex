package application_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnvironmentContextLivesInPromptPackage(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	legacy := filepath.Join(applicationDir, "core", "environment_context.go")
	if _, err := os.Stat(legacy); err == nil {
		t.Error("application/core/environment_context.go 仍存在；环境尾部渲染必须归 application/prompt")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(applicationDir, "prompt", "environment.go")); err != nil {
		t.Fatalf("application/prompt/environment.go 缺失: %v", err)
	}
}
