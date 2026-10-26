package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestSessionColdWorkspaceRulesAreExtracted(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("sessionColdWorkspace")
	if !ok {
		t.Fatal("serviceComponents 未装配 sessionColdWorkspace")
	}
	if field.Type != reflect.TypeOf((*sessionColdWorkspace)(nil)) {
		t.Fatalf("sessionColdWorkspace 字段类型 = %s", field.Type)
	}
	workspaceType := reflect.TypeOf((*sessionColdWorkspace)(nil))
	if _, ok := workspaceType.MethodByName("Bind"); !ok {
		t.Fatal("sessionColdWorkspace 缺少 Bind")
	}

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "session_history.go")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name.Name != "resumeSessionCold" {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok && (selector.Sel.Name == "bindProjectRootIfSafe" ||
				selector.Sel.Name == "BindSession" ||
				selector.Sel.Name == "UnbindGlobalProjectRoot" ||
				selector.Sel.Name == "UnbindSession" ||
				selector.Sel.Name == "UnbindProjectRootFor") {
				t.Errorf("resumeSessionCold 仍直接调用 %s", selector.Sel.Name)
			}
			return true
		})
		return
	}
}

func TestSessionColdWorkspaceBindsExistingWorkspace(t *testing.T) {
	workspace := &model.WorkspaceInfo{ID: "workspace-a", RootPath: "C:/workspace-a"}
	var calls []string
	component := newSessionColdWorkspace(sessionColdWorkspaceDeps{
		BindProjectRootIfSafe: func(sessionID, rootPath string) bool {
			calls = append(calls, "root:"+rootPath)
			return true
		},
		SetWriteScope: func(workspaceID string) { calls = append(calls, "scope:"+workspaceID) },
		BindSession:   func(sessionID, workspaceID string) { calls = append(calls, "bind:"+workspaceID) },
	})

	component.Bind("session-a", "workspace-a", workspace)

	want := []string{"root:C:/workspace-a", "scope:workspace-a", "bind:workspace-a"}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestSessionColdWorkspaceSkipsGlobalRootWhileChatRuns(t *testing.T) {
	workspace := &model.WorkspaceInfo{ID: "workspace-a", RootPath: "C:/workspace-a"}
	var calls []string
	component := newSessionColdWorkspace(sessionColdWorkspaceDeps{
		BindProjectRootIfSafe: func(string, string) bool { return false },
		SetWriteScope:         func(string) { t.Fatal("不安全时不得切全局写作用域") },
		BindSession: func(_ string, workspaceID string) {
			calls = append(calls, "bind:"+workspaceID)
		},
	})

	component.Bind("session-a", "workspace-a", workspace)

	if got := strings.Join(calls, ","); got != "bind:workspace-a" {
		t.Fatalf("calls = %q", got)
	}
}

func TestSessionColdWorkspaceUnbindsOnlyTrulyUnboundSession(t *testing.T) {
	var calls []string
	component := newSessionColdWorkspace(sessionColdWorkspaceDeps{
		AnyChatRunning:    func() bool { return false },
		UnbindGlobalRoot:  func() { calls = append(calls, "global") },
		SetWriteScope:     func(workspaceID string) { calls = append(calls, "scope:"+workspaceID) },
		UnbindSession:     func(sessionID string) { calls = append(calls, "session:"+sessionID) },
		UnbindRuntimeRoot: func(sessionID string) { calls = append(calls, "runtime:"+sessionID) },
	})

	component.Bind("session-a", "", nil)

	want := []string{"global", "scope:", "session:session-a", "runtime:session-a"}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestSessionColdWorkspaceDoesNotUnbindMissingWorkspaceMetadata(t *testing.T) {
	var calls []string
	component := newSessionColdWorkspace(sessionColdWorkspaceDeps{
		UnbindGlobalRoot: func() { t.Fatal("归属项目存在但元数据缺失时不得全局解绑") },
		UnbindSession:    func(string) { t.Fatal("归属项目存在但元数据缺失时不得永久解绑") },
	})

	component.Bind("session-a", "workspace-a", nil)

	if len(calls) != 0 {
		t.Fatalf("calls = %v", calls)
	}
}
