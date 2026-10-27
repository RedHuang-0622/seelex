package assembly

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConversationTurnConstructorsStayInCompositionRoot prevents each
// extracted Conversation Turn component from growing a free `newService*`
// assembly file. The callbacks depend only on core-private hosts, so their
// assembly belongs to Service's composition phases rather than package-level
// forwarding constructors.
func TestConversationTurnConstructorsStayInCompositionRoot(t *testing.T) {
	requireNoFreeConstructors(t, "newServiceConversationTurn")
}

func TestSessionColdConstructorsStayInCompositionRoot(t *testing.T) {
	requireNoFreeConstructors(t, "newServiceSessionCold")
}

func TestEditForkConstructorsStayInCompositionRoot(t *testing.T) {
	requireNoFreeConstructors(t, "newServiceEditFork")
}

func TestSessionConstructorsStayInCompositionRoot(t *testing.T) {
	requireNoFreeConstructors(t, "newServiceSession")
}

func TestConversationHistoryQueryConstructorStaysInCompositionRoot(t *testing.T) {
	requireNoFreeConstructors(t, "newServiceConversationHistoryQuery")
}

func TestExtractedApplicationConstructorsStayInCompositionRoot(t *testing.T) {
	prefixes := []string{
		"newServiceProjectBoardQuery",
		"newServiceRoleApplication",
		"newServiceReviewApplication",
		"newServicePerfStatsApplication",
		"newServiceConversationQueue",
		"newServiceHistorySearchApplication",
		"newServiceFaultGuardApplication",
		"newServiceGoalApplication",
		"newServicePolicyApplication",
		"newServiceSubagentApplication",
		"newServiceExecutionApplication",
	}
	for _, prefix := range prefixes {
		requireNoFreeConstructors(t, prefix)
	}
}

func TestNoFreeServiceConstructors(t *testing.T) {
	requireNoFreeConstructors(t, "newService")
}

func requireNoFreeConstructors(t *testing.T, prefix string) {
	t.Helper()
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
			if !ok || function.Recv != nil || function.Name == nil {
				continue
			}
			if strings.HasPrefix(function.Name.Name, prefix) {
				offenders = append(offenders, name+":"+function.Name.Name)
			}
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("构造器应留在 Service 组合阶段，禁止自由 %s* 外壳:\n%s",
			prefix, strings.Join(offenders, "\n"))
	}
}
