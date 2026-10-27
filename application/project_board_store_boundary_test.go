package application_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"
)

func TestCoreProjectBoardDoesNotImportStore(t *testing.T) {
	file, err := parser.ParseFile(
		token.NewFileSet(),
		filepath.Join(sourceApplicationDir(t), "assembly", "project_board_read.go"),
		nil,
		parser.ImportsOnly,
	)
	if err != nil {
		t.Fatal(err)
	}
	const forbidden = "github.com/RedHuang-0622/seelex/store"
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		if path == forbidden {
			t.Fatal("project board read still imports Store")
		}
	}
}
