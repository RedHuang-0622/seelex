package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCoreDoesNotExposeComponentGetters pins the composition boundary: once a
// capability lives in an application subpackage, core must not grow a Service
// method whose only job is to expose that component to another file or test.
// Package-internal callers can read service.components directly; external
// callers use the owning capability package or the registered FrontendPorts.
func TestCoreDoesNotExposeComponentGetters(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read core package: %v", err)
	}
	var offenders []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil || function.Name == nil || function.Type.Params == nil || function.Type.Results == nil {
				continue
			}
			if !hasServiceReceiver(function) ||
				len(function.Type.Params.List) != 0 ||
				len(function.Type.Results.List) != 1 ||
				!strings.HasSuffix(function.Name.Name, "Component") {
				continue
			}
			offenders = append(offenders, name+":"+function.Name.Name)
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("core 仍暴露组件转发 getter；调用方应直接使用 service.components 或领域包:\n%s",
			strings.Join(offenders, "\n"))
	}
}

// TestCoreDoesNotOwnInboundAPIContracts keeps consumer-facing interfaces at
// the root facade. Core may implement those contracts, but it must not define
// or re-export their type identity.
func TestCoreDoesNotOwnInboundAPIContracts(t *testing.T) {
	forbidden := map[string]bool{
		"ViewFace":                 true,
		"CommandFace":              true,
		"PolicyFace":               true,
		"QueueFace":                true,
		"ConversationAPI":          true,
		"SessionAPI":               true,
		"ExecutionQueryAPI":        true,
		"WorkItemQueryAPI":         true,
		"TeammateQueryAPI":         true,
		"SubagentQueryAPI":         true,
		"TranscriptQueryAPI":       true,
		"ExecutionCommandAPI":      true,
		"WorkspaceAPI":             true,
		"GoalAPI":                  true,
		"PolicyAPI":                true,
		"AdminAPI":                 true,
		"RoleAPI":                  true,
		"SubagentAPI":              true,
		"StateAPI":                 true,
		"QueueAPI":                 true,
		"LifecycleAPI":             true,
		"SessionQuery":             true,
		"ConversationHistoryQuery": true,
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read core package: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, specification := range general.Specs {
				typeSpec, ok := specification.(*ast.TypeSpec)
				if !ok || !forbidden[typeSpec.Name.Name] {
					continue
				}
				if _, ok := typeSpec.Type.(*ast.InterfaceType); ok {
					t.Errorf("%s: core must not define inbound interface %s", name, typeSpec.Name.Name)
				}
			}
		}
	}
}

// TestCoreHasNoComponentMethodForwarders removes the second forwarding layer:
// a Service method whose entire body is one call into service.components.
// Package-internal callers can invoke the component method directly.
func TestCoreHasNoComponentMethodForwarders(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read core package: %v", err)
	}
	var offenders []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil || function.Name == nil || !hasServiceReceiver(function) || function.Body == nil {
				continue
			}
			if !isComponentsForwardCall(function.Body) {
				continue
			}
			offenders = append(offenders, name+":"+function.Name.Name)
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("core Service 仍有组件单调用转发方法；调用方应直接使用 service.components:\n%s",
			strings.Join(offenders, "\n"))
	}
}

func isComponentsForwardCall(body *ast.BlockStmt) bool {
	if body == nil || len(body.List) != 1 {
		return false
	}
	var expression ast.Expr
	switch statement := body.List[0].(type) {
	case *ast.ReturnStmt:
		if len(statement.Results) != 1 {
			return false
		}
		expression = statement.Results[0]
	case *ast.ExprStmt:
		expression = statement.X
	default:
		return false
	}
	call, ok := expression.(*ast.CallExpr)
	if !ok {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return startsWithServiceComponents(selector.X)
}

func startsWithServiceComponents(expression ast.Expr) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if selector.Sel.Name == "components" {
		service, ok := selector.X.(*ast.Ident)
		return ok && service.Name == "service"
	}
	return startsWithServiceComponents(selector.X)
}

func hasServiceReceiver(function *ast.FuncDecl) bool {
	if function.Recv == nil || len(function.Recv.List) != 1 {
		return false
	}
	expression := function.Recv.List[0].Type
	if pointer, ok := expression.(*ast.StarExpr); ok {
		expression = pointer.X
	}
	identifier, ok := expression.(*ast.Ident)
	return ok && identifier.Name == "Service"
}
