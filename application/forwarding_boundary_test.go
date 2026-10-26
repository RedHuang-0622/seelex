package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// TestServiceHasNoCompatibilityForwarders proves the compatibility surface has
// moved out of core: every remaining Service method owns state or lifecycle.
func TestServiceHasNoCompatibilityForwarders(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	coreDir := filepath.Join(applicationDir, "core")
	entries, err := os.ReadDir(coreDir)
	if err != nil {
		t.Fatalf("read core: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !isGoFile(name) || isTestGoFile(name) {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(coreDir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil || function.Name == nil ||
				!ast.IsExported(function.Name.Name) || !isServiceReceiver(function) {
				continue
			}
			if !isSingleForwardStatement(function.Body) {
				continue
			}
			t.Errorf("Service.%s is still a one-line compatibility forwarder", function.Name.Name)
		}
	}
}

// TestCoreHasNoPackageFunctionForwarders keeps the direct-dependency cleanup
// from stopping at import aliases. The list only contains helpers whose body
// adds no core state or locking; composition-root constructors and host
// adapters are deliberately excluded until their owning capability moves.
func TestCoreHasNoPackageFunctionForwarders(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	coreDir := filepath.Join(applicationDir, "core")
	forwarders := map[string]string{
		"NewDefaultWindowPolicy": "application/window",
		"DefaultWindowConfig":    "application/window",
		"LoadWindowConfig":       "application/window",
		"NewEventHub":            "application/event",
		"NewPromptStack":         "application/prompt",
		"NewEffortManager":       "application/prompt",
		"cloneSnapshot":          "contract/protocol",
		"cloneRuntimeState":      "contract/protocol",
		"maxLoopsFor":            "application/prompt",
		"reasoningEffortFor":     "application/prompt",
		"reactBudgetFor":         "application/prompt",
		"teamworkJobCompletions": "kernel/sessionpolicy",
		"teamworkJobEvents":      "kernel/sessionpolicy",
		"splitSigil":             "application/input",
		"deepCopyForkRecord":     "kernel/sessionpolicy",
		"ApplyWindowConfig":      "application/window",
		"CurrentWindowConfig":    "application/window",
		"withSessionID":          "application/execution/taskcontext",
		"sessionIDFromContext":   "application/execution/taskcontext",
		"HasSigilPrefix":         "application/input",
		"SigilOf":                "application/input",
		"ApplyLimits":            "application/policy",
		"Limits":                 "application/policy",
	}
	entries, err := os.ReadDir(coreDir)
	if err != nil {
		t.Fatalf("read core: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !isGoFile(name) || isTestGoFile(name) {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(coreDir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil || function.Name == nil {
				continue
			}
			target, tracked := forwarders[function.Name.Name]
			if !tracked {
				continue
			}
			if isSingleForwardStatement(function.Body) {
				t.Errorf("%s: core package function %s is still a one-expression forwarder; call %s directly",
					name, function.Name.Name, target)
			}
		}
	}
}

// TestCoreDoesNotOwnInputFacade keeps the sigil helpers in their lexical
// owner. The root application facade may re-export input functions, but core
// must not retain a second package-level facade merely because it predates the
// input package extraction.
func TestCoreDoesNotOwnInputFacade(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	coreDir := filepath.Join(applicationDir, "core")
	forbidden := map[string]bool{
		"HasSigilPrefix": true,
		"SigilOf":        true,
	}
	entries, err := os.ReadDir(coreDir)
	if err != nil {
		t.Fatalf("read core: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !isGoFile(name) || isTestGoFile(name) {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(coreDir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil || function.Name == nil || !forbidden[function.Name.Name] {
				continue
			}
			t.Errorf("%s: core must not define input facade %s; call application/input directly", name, function.Name.Name)
		}
	}
}

// TestCoreDoesNotOwnInputVocabulary keeps input constants and Suggestion type
// identity in application/input. Re-exporting them from core creates another
// apparent owner even though every definition is only an alias.
func TestCoreDoesNotOwnInputVocabulary(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	coreDir := filepath.Join(applicationDir, "core")
	forbidden := map[string]bool{
		"SigilCommand":          true,
		"SigilPlugin":           true,
		"SigilSkill":            true,
		"SuggestionKindCommand": true,
		"SuggestionKindSkill":   true,
		"SuggestionKindPlugin":  true,
		"Suggestion":            true,
	}
	entries, err := os.ReadDir(coreDir)
	if err != nil {
		t.Fatalf("read core: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !isGoFile(name) || isTestGoFile(name) {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(coreDir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || (general.Tok != token.CONST && general.Tok != token.TYPE) {
				continue
			}
			for _, specification := range general.Specs {
				switch spec := specification.(type) {
				case *ast.TypeSpec:
					if forbidden[spec.Name.Name] {
						t.Errorf("%s: core must not alias input type %s", name, spec.Name.Name)
					}
				case *ast.ValueSpec:
					for _, identifier := range spec.Names {
						if forbidden[identifier.Name] {
							t.Errorf("%s: core must not alias input constant %s", name, identifier.Name)
						}
					}
				}
			}
		}
	}
}

func isSingleForwardStatement(body *ast.BlockStmt) bool {
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
	_, ok := expression.(*ast.CallExpr)
	return ok
}

func isServiceReceiver(function *ast.FuncDecl) bool {
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

func isGoFile(name string) bool {
	return len(name) > 3 && name[len(name)-3:] == ".go"
}

func isTestGoFile(name string) bool {
	return len(name) > 8 && name[len(name)-8:] == "_test.go"
}
