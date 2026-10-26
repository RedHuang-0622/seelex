package application_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"
)

func TestForkLineageApplicationDoesNotImportStore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	path := filepath.Join(applicationDir, "session", "fork_lineage.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("解析 %s: %v", path, err)
	}
	for _, imported := range file.Imports {
		value, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			t.Fatalf("解析 import %s: %v", imported.Path.Value, err)
		}
		if value == "github.com/RedHuang-0622/seelex/store" {
			t.Fatal("application/session fork lineage 必须只消费自己的窄 JSON 投影，不得 import store")
		}
	}
}
