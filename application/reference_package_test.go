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

// TestReferenceApplicationIsExtractedFromCore freezes the package boundary for
// the reference use cases. The pure request, alias, pagination and plan-filter
// orchestration belongs to application/reference; core may only keep the
// Service facade and adapter callbacks.
func TestReferenceApplicationIsExtractedFromCore(t *testing.T) {
	applicationDir := referenceTestApplicationDir(t)
	packageDir := filepath.Join(applicationDir, "reference")
	applicationFile := filepath.Join(packageDir, "application.go")
	if _, err := os.Stat(applicationFile); err != nil {
		t.Fatalf("application/reference/application.go is required: %v", err)
	}

	file, err := parser.ParseFile(token.NewFileSet(), applicationFile, nil, 0)
	if err != nil {
		t.Fatalf("parse application/reference/application.go: %v", err)
	}
	if file.Name.Name != "reference" {
		t.Fatalf("application/reference package name = %q, want reference", file.Name.Name)
	}

	wantTypes := map[string]bool{"Application": false, "Deps": false}
	wantFuncs := map[string]bool{"New": false}
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
			t.Errorf("application/reference is missing exported type %s", name)
		}
	}
	for name, found := range wantFuncs {
		if !found {
			t.Errorf("application/reference is missing constructor %s", name)
		}
	}

	coreFile := filepath.Join(applicationDir, "core", "reference_tools.go")
	core, err := parser.ParseFile(token.NewFileSet(), coreFile, nil, 0)
	if err != nil {
		t.Fatalf("parse core/reference_tools.go: %v", err)
	}
	for _, declaration := range core.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range general.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if ok && (typeSpec.Name.Name == "referenceApplication" || typeSpec.Name.Name == "referenceApplicationDeps") {
				t.Errorf("core still owns %s; move the implementation to application/reference", typeSpec.Name.Name)
			}
		}
	}
}

// TestCompressedTurnApplicationIsExtractedFromCore pins the second reference
// adapter to the same ownership boundary. read_compressed_turn only shapes a
// segment request, loads an archived turn and paginates the rendered text;
// those rules must not remain as a second component graph inside core.
func TestCompressedTurnApplicationIsExtractedFromCore(t *testing.T) {
	applicationDir := referenceTestApplicationDir(t)
	referenceFile := filepath.Join(applicationDir, "reference", "compressed_turn.go")
	if _, err := os.Stat(referenceFile); err != nil {
		t.Fatalf("application/reference/compressed_turn.go is required: %v", err)
	}

	coreFile := filepath.Join(applicationDir, "core", "compressed_turn_application.go")
	if _, err := os.Stat(coreFile); !os.IsNotExist(err) {
		t.Fatalf("core/compressed_turn_application.go must be deleted after extraction: %v", err)
	}
}

func referenceTestApplicationDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	return filepath.Dir(file)
}
