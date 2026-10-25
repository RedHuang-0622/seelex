package worktree

import (
	"errors"
	"testing"
)

func TestBeginNamedStrictReturnsTypedNonGitError(t *testing.T) {
	root := t.TempDir()
	manager := NewWorktreeManager(WorktreeManagerDeps{Root: func() string { return root }})
	t.Cleanup(manager.Close)

	wt, err := manager.BeginNamedStrict("wi-strict")
	if wt != nil {
		t.Fatalf("严格创建失败时不得返回可用的 worktree: %+v", wt)
	}
	var typed *WorktreeCreateError
	if !errors.As(err, &typed) {
		t.Fatalf("严格创建必须返回 typed error，得到 %T %v", err, err)
	}
	if typed.Code != WorktreeErrorNotGitRepository {
		t.Fatalf("非 git 根的错误码 = %q，want %q", typed.Code, WorktreeErrorNotGitRepository)
	}
	if typed.NodeID != "wi-strict" || typed.ApprovalRequired != true {
		t.Fatalf("typed error 必须带目标节点并要求审批: %+v", typed)
	}
}

func TestBeginNamedLegacyWrapperKeepsFallbackSemantics(t *testing.T) {
	root := t.TempDir()
	manager := NewWorktreeManager(WorktreeManagerDeps{Root: func() string { return root }})
	t.Cleanup(manager.Close)
	if wt := manager.BeginNamed("wi-legacy"); wt != nil {
		t.Fatalf("兼容 wrapper 应保留 nil 降级语义，得到 %+v", wt)
	}
}
