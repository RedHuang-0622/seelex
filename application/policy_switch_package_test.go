package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestPolicySwitchApplicationIsExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	packageFile := filepath.Join(applicationDir, "policy", "switch.go")
	if _, err := os.Stat(packageFile); err != nil {
		t.Fatalf("application/policy/switch.go is required: %v", err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), packageFile, nil, 0)
	if err != nil {
		t.Fatalf("parse application/policy/switch.go: %v", err)
	}
	if file.Name.Name != "policy" {
		t.Fatalf("application/policy/switch.go package = %q", file.Name.Name)
	}
	coreFile := filepath.Join(applicationDir, "core", "service_policy_application.go")
	core, err := parser.ParseFile(token.NewFileSet(), coreFile, nil, 0)
	if err != nil {
		t.Fatalf("parse core/service_policy_application.go: %v", err)
	}
	for _, declaration := range core.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, spec := range general.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if ok && (typeSpec.Name.Name == "policyApplication" ||
				typeSpec.Name.Name == "policyApplicationDeps") {
				t.Errorf("core still owns %s", typeSpec.Name.Name)
			}
		}
	}
}
