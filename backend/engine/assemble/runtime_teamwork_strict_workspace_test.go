package assemble

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/backend/multiagent/teamwork"
)

// TestBindWorkspaceDoesNotFallBackToMain 钉住 teammate 路径：worktree 创建失败
// 返回 typed approval_required，不返回"成功但没有现场"的伪降级结果。
func TestBindWorkspaceDoesNotFallBackToMain(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	if err := runtime.BindProjectRoot(t.TempDir()); err != nil {
		t.Fatalf("BindProjectRoot(non-git): %v", err)
	}

	binding := teamwork.WorkspaceBinding{
		MainSessionID: "sess-main", TeamID: "team-1", Milestone: "m-build",
		WorkItem: "wi-1", Role: "exec", SessionID: "sess-main-team-1-exec-wi-wi-1",
		Worktree: "seelex/exec-wi-wi-1", WorkspaceMode: string(teamwork.WorkspaceWorktree),
	}
	bound, err := runtime.BindWorkspace(context.Background(), binding)
	if err == nil {
		t.Fatalf("非 git 根不得自动降级成主工作区，得到 %+v", bound)
	}
	var typed *WorkspaceCreateError
	if !errors.As(err, &typed) {
		t.Fatalf("错误必须是 WorkspaceCreateError，得到 %T %v", err, err)
	}
	if !typed.ApprovalRequired || typed.Code != "worktree_not_git_repository" {
		t.Fatalf("typed error 必须要求审批并保留分类: %+v", typed)
	}
	if !strings.Contains(err.Error(), "approval_required=true") {
		t.Fatalf("工具面必须能说清需要审批: %v", err)
	}
}
