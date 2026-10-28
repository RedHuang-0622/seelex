package architecture

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

func TestCoreHoldsNoSessionContainerFields(t *testing.T) {
	for _, name := range []string{"service.go", "service_state.go"} {
		path := filepath.Join("..", name)
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			spec, ok := node.(*ast.TypeSpec)
			if !ok || (spec.Name.Name != "Service" && spec.Name.Name != "serviceState") {
				return true
			}
			structure, ok := spec.Type.(*ast.StructType)
			if !ok {
				t.Fatalf("%s is not a struct", spec.Name.Name)
			}
			for _, field := range structure.Fields.List {
				var rendered bytes.Buffer
				if err := format.Node(&rendered, token.NewFileSet(), field.Type); err != nil {
					t.Fatalf("render %s field: %v", spec.Name.Name, err)
				}
				if strings.Contains(rendered.String(), "SessionUnit") {
					t.Fatalf("%s directly owns SessionUnit container type %s", spec.Name.Name, rendered.String())
				}
			}
			return true
		})
	}
}
