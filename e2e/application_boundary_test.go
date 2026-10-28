package e2e

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTUIStaysOnApplicationBoundary keeps the TUI on its consumer-owned
// application boundary and prevents it from importing implementation layers.
func TestTUIStaysOnApplicationBoundary(t *testing.T) {
	entries, err := os.ReadDir("../tui")
	if err != nil {
		t.Fatalf("读取 tui: %v", err)
	}
	fileSet := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, filepath.Join("../tui", entry.Name()), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("解析 tui/%s: %v", entry.Name(), err)
		}
		for _, imported := range file.Imports {
			path := strings.Trim(imported.Path.Value, `"`)
			if !strings.HasPrefix(path, seelexModulePrefix) {
				continue
			}
			if path == "github.com/RedHuang-0622/seelex/contract" ||
				strings.HasPrefix(path, "github.com/RedHuang-0622/seelex/contract/") ||
				strings.HasPrefix(path, "github.com/RedHuang-0622/seelex/application/") ||
				strings.HasPrefix(path, "github.com/RedHuang-0622/seelex/tui/") {
				continue
			}
			t.Errorf("tui/%s 绕过 Application 边界直接依赖 %s", entry.Name(), path)
		}
	}
}
