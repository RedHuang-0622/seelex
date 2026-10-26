package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestInputApplicationIsExtractedFromCore freezes the input suggestion package
// boundary. Sigil parsing and suggestion shaping are pure application policies;
// core should retain aliases and facade adapters only.
func TestInputApplicationIsExtractedFromCore(t *testing.T) {
	applicationDir := inputTestApplicationDir(t)
	packageDir := filepath.Join(applicationDir, "input")
	applicationFile := filepath.Join(packageDir, "suggestions.go")
	if _, err := os.Stat(applicationFile); err != nil {
		t.Fatalf("application/input/suggestions.go is required: %v", err)
	}

	file, err := parser.ParseFile(token.NewFileSet(), applicationFile, nil, 0)
	if err != nil {
		t.Fatalf("parse application/input/suggestions.go: %v", err)
	}
	if file.Name.Name != "input" {
		t.Fatalf("application/input package name = %q, want input", file.Name.Name)
	}

	wantTypes := map[string]bool{"Application": false, "Deps": false, "Suggestion": false}
	wantFuncs := map[string]bool{"New": false, "Parse": false}
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.GenDecl:
			for _, spec := range declaration.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if ok {
					if _, tracked := wantTypes[typeSpec.Name.Name]; tracked {
						wantTypes[typeSpec.Name.Name] = true
					}
				}
			}
		case *ast.FuncDecl:
			if declaration.Recv == nil {
				if _, tracked := wantFuncs[declaration.Name.Name]; tracked {
					wantFuncs[declaration.Name.Name] = true
				}
			}
		}
	}
	for name, found := range wantTypes {
		if !found {
			t.Errorf("application/input is missing exported type %s", name)
		}
	}
	for name, found := range wantFuncs {
		if !found {
			t.Errorf("application/input is missing function %s", name)
		}
	}

	coreFile := filepath.Join(applicationDir, "core", "completion.go")
	core, err := parser.ParseFile(token.NewFileSet(), coreFile, nil, 0)
	if err != nil {
		t.Fatalf("parse core/completion.go: %v", err)
	}
	for _, declaration := range core.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range general.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if ok && (typeSpec.Name.Name == "completionApplication" || typeSpec.Name.Name == "completionApplicationDeps") {
				t.Errorf("core still owns %s; move the implementation to application/input", typeSpec.Name.Name)
			}
		}
	}
}

func inputTestApplicationDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	return filepath.Dir(file)
}
