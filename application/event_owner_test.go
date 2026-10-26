package application_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestApplicationEventRuntimeUsesNeutralKernelOwner(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	var offenders []string
	err := filepath.WalkDir(applicationDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") ||
			strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range file.Imports {
			importPath, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			if importPath == "github.com/RedHuang-0622/seelex/middle/protocol/event" {
				relative, _ := filepath.Rel(applicationDir, path)
				offenders = append(offenders, filepath.ToSlash(relative))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk application: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("Application still imports middle/protocol/event directly:\n%s",
			strings.Join(offenders, "\n"))
	}
}
