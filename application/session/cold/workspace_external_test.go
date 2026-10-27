package cold_test

import (
	cold "github.com/RedHuang-0622/seelex/application/session/cold"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"strings"
	"testing"
)

func TestSessionColdWorkspaceBindsExistingWorkspace(t *testing.T) {
	workspace := &model.WorkspaceInfo{ID: "workspace-a", RootPath: "C:/workspace-a"}
	var calls []string
	component := cold.NewWorkspace(cold.WorkspaceDeps{
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
	component := cold.NewWorkspace(cold.WorkspaceDeps{
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
	component := cold.NewWorkspace(cold.WorkspaceDeps{
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
	component := cold.NewWorkspace(cold.WorkspaceDeps{
		UnbindGlobalRoot: func() { t.Fatal("归属项目存在但元数据缺失时不得全局解绑") },
		UnbindSession:    func(string) { t.Fatal("归属项目存在但元数据缺失时不得永久解绑") },
	})

	component.Bind("session-a", "workspace-a", nil)

	if len(calls) != 0 {
		t.Fatalf("calls = %v", calls)
	}
}
