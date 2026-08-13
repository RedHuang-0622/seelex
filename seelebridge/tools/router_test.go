package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

// TestResolveNodePathUsesWorktreeRoot 验证 NodeScope.WorkspaceID（worktree 根）
// 优先于 ProjectScope，且越界路径（.. 逃逸）被拒绝。
func TestResolveNodePathUsesWorktreeRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "base.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Deps{})
	ctx := model.WithNodeScope(context.Background(), model.NodeScope{
		NodeID: "x", Role: model.RoleSubAgent, BranchID: "x", WorkspaceID: root,
	})
	resolved, err := router.resolveNodePath(ctx, "base.txt", false)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "base.txt"); resolved != want {
		t.Fatalf("resolved = %s, want %s", resolved, want)
	}
	if _, err := router.resolveNodePath(ctx, "../outside.txt", false); err == nil {
		t.Fatal("escape path must be rejected")
	}
}

// TestScopedBashPublishesDiagnosticStages 验证 scoped bash 全程发布诊断阶段，
// 且诊断元数据不暴露命令文本。
func TestScopedBashPublishesDiagnosticStages(t *testing.T) {
	var events []BashDiagnosticEvent
	router := NewRouter(Deps{
		ToolCallTimeout:        time.Minute,
		ObserveBash:            func(event BashDiagnosticEvent) { events = append(events, event) },
		DisableDockerAutoStart: true,
	})
	ctx := model.WithNodeScope(context.Background(), model.NodeScope{
		NodeID: "x", Role: model.RoleSubAgent, BranchID: "x", WorkspaceID: t.TempDir(),
	})
	output, err := router.scopedBash(ctx, `{"command":"echo diagnostic-ok"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "diagnostic-ok") {
		t.Fatalf("bash output = %q", output)
	}

	got := make([]string, 0, len(events))
	for _, event := range events {
		got = append(got, event.Stage)
		if event.Shell == "diagnostic-ok" {
			t.Fatal("diagnostic observer exposed command content as shell metadata")
		}
	}
	want := []string{
		"bash.resolve.start",
		"bash.resolve.done",
		"bash.command.prepared",
		"bash.process.starting",
		"bash.process.started",
		"bash.process.exited",
		"bash.handler.return",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("diagnostic stages = %v, want %v", got, want)
	}
}

// TestBashDiagnosticObserverPanicDoesNotBreakTool 验证观察者 panic 不影响工具调用。
func TestBashDiagnosticObserverPanicDoesNotBreakTool(t *testing.T) {
	router := NewRouter(Deps{
		ToolCallTimeout:        time.Minute,
		ObserveBash:            func(BashDiagnosticEvent) { panic("test observer") },
		DisableDockerAutoStart: true,
	})
	ctx := model.WithNodeScope(context.Background(), model.NodeScope{
		NodeID: "x", Role: model.RoleSubAgent, BranchID: "x", WorkspaceID: t.TempDir(),
	})
	if _, err := router.scopedBash(ctx, `{"command":"echo observer-safe"}`); err != nil {
		t.Fatalf("panic in diagnostic observer must not break bash: %v", err)
	}
}
