package history

import (
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

func TestHistoryDoesNotImportViewState(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "page.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	const forbidden = "github.com/RedHuang-0622/seelex/middle/view/state"
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		if path == forbidden {
			t.Fatal("history still imports view state")
		}
	}
}
