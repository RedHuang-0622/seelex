package application_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"
)

func TestAssemblyOwnersDoNotImportStore(t *testing.T) {
	files := []string{
		"session/fork_lineage.go",
		"assembly/service_assembler.go",
		"conversation/queue.go",
	}
	for _, relative := range files {
		t.Run(relative, func(t *testing.T) {
			assertDoesNotImport(t, filepath.Join(sourceApplicationDir(t), filepath.FromSlash(relative)), "github.com/RedHuang-0622/seelex/store")
		})
	}
}

func TestProjectBoardReadDoesNotImportStore(t *testing.T) {
	assertDoesNotImport(
		t,
		filepath.Join(sourceApplicationDir(t), "execution", "project_board.go"),
		"github.com/RedHuang-0622/seelex/store",
	)
}

func assertDoesNotImport(t *testing.T, path, forbidden string) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	for _, imported := range file.Imports {
		value, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			t.Fatalf("parse import %s: %v", imported.Path.Value, err)
		}
		if value == forbidden {
			t.Errorf("%s imports forbidden package %s", path, forbidden)
		}
	}
}
