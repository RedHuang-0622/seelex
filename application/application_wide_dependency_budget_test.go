package application_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type applicationWideDependencyBudget struct {
	ConcreteDependencyLimit     int      `json:"concrete_dependency_limit"`
	AllowedConcreteDependencies []string `json:"allowed_concrete_dependencies"`
}

func TestApplicationWideConcreteDependencyBudget(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	var budget applicationWideDependencyBudget
	readJSON(t, filepath.Join(applicationDir, "testdata", "application_wide_dependency_budget.json"), &budget)

	got := concreteDependenciesUnder(t, applicationDir)
	want := append([]string(nil), budget.AllowedConcreteDependencies...)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("application concrete dependency set drifted:\n got=%v\nwant=%v", got, want)
	}
	if len(got) != budget.ConcreteDependencyLimit {
		t.Fatalf("application concrete dependency count = %d, budget = %d", len(got), budget.ConcreteDependencyLimit)
	}
}

func concreteDependenciesUnder(t *testing.T, root string) []string {
	t.Helper()
	dependencies := make(map[string]bool)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := filepath.Base(path)
		if !isGoFile(name) || isTestGoFile(name) {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if isConcreteApplicationDependency(importPath) {
				dependencies[importPath] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan application dependencies: %v", err)
	}
	result := make([]string, 0, len(dependencies))
	for dependency := range dependencies {
		result = append(result, dependency)
	}
	sort.Strings(result)
	return result
}

func isConcreteApplicationDependency(path string) bool {
	const prefix = "github.com/RedHuang-0622/seelex/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	relative := strings.TrimPrefix(path, prefix)
	return relative == "store" ||
		strings.HasPrefix(relative, "store/") ||
		strings.HasPrefix(relative, "backend/") ||
		strings.HasPrefix(relative, "middle/")
}
