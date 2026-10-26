package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// session_ssh_test.go 钉住两条不变式的判据（纯函数层）：
//  1. 放权集合必须是项目环境表的**子集**（写进不存在的名字要显式失败）；
//  2. 首次绑定的会话工作环境不可切换或清空。
//
// 端到端（会话端口 + 项目环境表）的路径由装配侧的测试覆盖；这两条是**规则本身**，
// 放在纯函数上跑得最快，也最难被后续重构悄悄改掉。

func TestSessionEnvironmentCanBeFixedOnceAndNeverChanged(t *testing.T) {
	workspace := &environmentTraceWorkspace{
		fakeWorkspace: newFakeWorkspace(),
		environments: []model.WorkspaceEnvironmentInfo{
			{Name: "dev"},
			{Name: "test", Host: "test.example.com", User: "qa"},
		},
	}
	project, err := workspace.Create("app", "/workspace/app", "")
	if err != nil {
		t.Fatal(err)
	}
	sessions := &environmentTraceSessions{}
	service := newTestService(t, &fakeEngine{},
		func(deps *contract.Dependencies) { deps.Workspace = workspace },
		withTestSessions(sessions),
	)
	list, _, err := frontendOfTestService(service).WorkspaceEnvironments(project.ID)
	if err != nil || len(list) != 2 {
		t.Fatalf("项目已存在但会话未绑定时仍须读到环境表: list=%v err=%v", list, err)
	}
	state, err := service.SetSessionSSHEnv("sess-fixed", project.ID, "test")
	if err != nil || state.CurrentEnv != "test" {
		t.Fatalf("bind = %+v err=%v", state, err)
	}
	if _, err := service.SetSessionSSHEnv("sess-fixed", project.ID, "test"); err != nil {
		t.Fatalf("重复固定同一环境应幂等: %v", err)
	}
	if _, err := service.SetSessionSSHEnv("sess-fixed", project.ID, "dev"); err == nil {
		t.Fatal("固定环境不得被 agent/工具切换")
	}
	if _, err := service.SetSessionSSHEnv("sess-fixed", project.ID, ""); err == nil {
		t.Fatal("固定环境不得被清空")
	}
	if _, err := service.SetSessionSSHEnv("sess-fixed", project.ID, "missing"); err == nil {
		t.Fatal("未知环境也不得替换固定值")
	}
}

func TestSessionSSHApplicationIsAssembled(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("sessionSSHApplication")
	if !ok {
		t.Fatal("serviceComponents 未装配 sessionSSHApplication")
	}
	if field.Type != reflect.TypeOf((*applicationsession.SSHApplication)(nil)) {
		t.Fatalf("sessionSSHApplication 字段类型 = %s", field.Type)
	}
}

func TestWorkspaceEnvironmentFacadeOnlyDelegatesToApplication(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "..", "compat", "frontend.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"WorkspaceEnvironments":          false,
		"SetWorkspaceEnvironment":        false,
		"DeleteWorkspaceEnvironment":     false,
		"SetDefaultWorkspaceEnvironment": false,
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name == nil {
			continue
		}
		if !functionHasReceiver(function, "Frontend") {
			continue
		}
		if _, tracked := want[function.Name.Name]; !tracked {
			continue
		}
		want[function.Name.Name] = true
		if len(function.Body.List) != 1 {
			t.Fatalf("%s 有 %d 条语句，want 一行 workspaceEnvironmentApplication 转发", function.Name.Name, len(function.Body.List))
		}
		returnStatement, ok := function.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(returnStatement.Results) != 1 {
			t.Fatalf("%s 不是单行 return 转发", function.Name.Name)
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("未找到 %s", name)
		}
	}
}

func TestSessionSSHFacadeOnlyDelegatesToApplication(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "session_ssh.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"SessionSSHState":      false,
		"SetSessionSSHAllowed": false,
		"SetSessionSSHEnv":     false,
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name == nil || !functionHasReceiver(function, "Service") {
			continue
		}
		if _, tracked := want[function.Name.Name]; !tracked {
			continue
		}
		want[function.Name.Name] = true
		if len(function.Body.List) != 1 {
			t.Fatalf("%s 有 %d 条语句，want 一行 sessionSSHApplication 转发", function.Name.Name, len(function.Body.List))
		}
		returnStatement, ok := function.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(returnStatement.Results) != 1 {
			t.Fatalf("%s 不是单行 return 转发", function.Name.Name)
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("未找到 %s", name)
		}
	}
}

func functionHasReceiver(function *ast.FuncDecl, name string) bool {
	if function == nil || function.Recv == nil || len(function.Recv.List) != 1 {
		return false
	}
	receiver := function.Recv.List[0].Type
	if pointer, ok := receiver.(*ast.StarExpr); ok {
		receiver = pointer.X
	}
	identifier, ok := receiver.(*ast.Ident)
	return ok && identifier.Name == name
}
