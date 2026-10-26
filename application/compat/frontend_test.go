package compat

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"
)

func TestMigratedCompatibilityMethodsArePresent(t *testing.T) {
	typ := reflect.TypeOf((*Frontend)(nil))
	for _, name := range []string{
		"AddAccount",
		"Suggestions",
		"SelectAccount",
		"SwitchEffort",
		"SwitchPlugin",
		"LoadMoreHistory",
		"LoadLatestHistory",
		"ReorderQueuedInput",
		"RecallQueuedInput",
		"AccountDetails",
		"UpdateAccount",
		"DeleteAccount",
		"AccountBalance",
		"SetAccountWindow",
		"RoleDefaultVector",
		"SetRoleDefaultVector",
		"RoleAccount",
		"SetRoleAccount",
		"RolePermissionVector",
		"SetRolePermissionVector",
		"CreateRoleSession",
		"ReadRoleSessionRows",
		"ScheduleRegister",
		"WorkspaceEnvironments",
		"WorkspaceGitRepos",
		"DeleteSession",
		"CreateWorkspace",
		"BindWorkspace",
		"WorkspaceTree",
		"WorkspaceFileContent",
		"WorkspaceWriteFile",
	} {
		if _, ok := typ.MethodByName(name); !ok {
			t.Errorf("Frontend 缺少迁移入口 %s", name)
		}
	}
}

func TestCompatibilityMethodsOnlyForward(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "frontend.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	helpers := map[string]bool{
		"New": true, "admin": true, "role": true, "roleSessions": true,
		"completion": true, "conversationQueue": true, "history": true, "policy": true,
		"workspace": true, "workspaceUseCase": true, "sessionDelete": true,
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name == nil || helpers[function.Name.Name] {
			continue
		}
		if function.Body == nil || len(function.Body.List) != 1 {
			t.Errorf("%s must contain exactly one forwarding statement", function.Name.Name)
		}
	}
}
