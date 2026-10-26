package application_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestEditForkProductionFilesUseProtocolEvents(t *testing.T) {
	files := []string{
		"conversation/edit/resolver.go",
		"conversation/edit/process.go",
		"core/edit_fork_process.go",
		"core/edit_fork_target_resolver.go",
		"core/session_message_edit.go",
	}
	for _, relative := range files {
		path := filepath.Join(sourceApplicationDir(t), filepath.FromSlash(relative))
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", relative, err)
		}
		for _, imported := range file.Imports {
			importPath, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatalf("unquote import in %s: %v", relative, err)
			}
			switch importPath {
			case "github.com/RedHuang-0622/seelex/store",
				"github.com/RedHuang-0622/seelex/middle/protocol/model":
				t.Errorf("%s must use contract/protocol events, still imports %s", relative, importPath)
			}
			if strings.HasPrefix(importPath, "github.com/RedHuang-0622/seelex/store/") {
				t.Errorf("%s must use contract/protocol events, still imports %s", relative, importPath)
			}
		}
	}
}

func TestQueueRecoveryCoreUsesContractDTO(t *testing.T) {
	path := filepath.Join(sourceApplicationDir(t), "core", "session_queue_recover.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse core/session_queue_recover.go: %v", err)
	}
	for _, imported := range file.Imports {
		importPath, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			t.Fatalf("unquote import: %v", err)
		}
		if importPath == "github.com/RedHuang-0622/seelex/store" ||
			strings.HasPrefix(importPath, "github.com/RedHuang-0622/seelex/store/") {
			t.Errorf("core/session_queue_recover.go must use contract/dto, still imports %s", importPath)
		}
	}
}
