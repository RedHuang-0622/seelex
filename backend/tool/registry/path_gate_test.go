package registry

// path_gate_test.go 钉住 P1 的**顺序**：路径门在权限门之前。
//
// 判据不是"越界会被拒"（handler 本来也会拒），而是"越界**不弹审批**"——
// 这正是把路径门前移的全部收益。用例用计数 approval handler 直接观察顺序。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/seelex/backend/limit/gate"
	"github.com/RedHuang-0622/seelex/internal/model"
)

func TestPathGateRejectsEscapeBeforePermissionAsk(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	scope := security.NewProjectScope()
	if err := scope.BindFor("sess-gate", root); err != nil {
		t.Fatalf("bind: %v", err)
	}

	approvals := 0
	permission := &PermissionGate{SessionFromContext: func(context.Context) string { return "sess-gate" }}
	permission.Set(toolspermission.PermissionConfig{
		Mode:  toolspermission.ModeManual,
		Rules: []toolspermission.PermissionRule{{ToolName: WriteFileToolName, Action: toolspermission.ActionAsk}},
	}, func(*toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
		approvals++
		return &toolspermission.ApprovalResponse{Choice: "allow"}, nil
	})

	ran := 0
	next := permission.Middleware(0)(WriteFileToolName, frameworktools.ToolMeta{},
		frameworktools.HandlerFunc(func(context.Context, string) (string, error) {
			ran++
			return "ok", nil
		}))
	gated := PathGate(Deps{ProjectScope: scope, SessionKey: func(context.Context) string { return "sess-gate" }})
	handler := gated(WriteFileToolName, frameworktools.ToolMeta{}, next)

	escape := filepath.Join(outside, "leak.txt")
	_, err := handler.Execute(context.Background(), `{"path":`+quoteJSON(escape)+`,"content":"x"}`)
	if err == nil || !strings.Contains(err.Error(), "bound project") {
		t.Fatalf("越界路径应被路径门拒绝，err=%v", err)
	}
	if approvals != 0 {
		t.Fatalf("越界路径不得先弹审批：approvals=%d", approvals)
	}
	if ran != 0 {
		t.Fatalf("越界路径不得执行 handler：ran=%d", ran)
	}

	// 界内路径仍走权限门，且审批照常发生。
	inside := filepath.Join(root, "ok.txt")
	if _, err := handler.Execute(context.Background(), `{"path":`+quoteJSON(inside)+`,"content":"x"}`); err != nil {
		t.Fatalf("界内路径应放行：%v", err)
	}
	if approvals != 1 || ran != 1 {
		t.Fatalf("界内路径应经权限门并执行一次：approvals=%d ran=%d", approvals, ran)
	}
}

func TestPathGateUsesNodeScopeRoot(t *testing.T) {
	project := t.TempDir()
	worktree := t.TempDir()
	scope := security.NewProjectScope()
	if err := scope.BindFor("sess-node", project); err != nil {
		t.Fatalf("bind: %v", err)
	}
	ctx := model.WithNodeScope(context.Background(), model.NodeScope{
		NodeID: "node-1", WorkspaceID: worktree,
	})

	handler := PathGate(Deps{ProjectScope: scope, SessionKey: func(context.Context) string { return "sess-node" }})(
		ReadFileToolName, frameworktools.ToolMeta{},
		frameworktools.HandlerFunc(func(context.Context, string) (string, error) { return "ok", nil }))

	inside := filepath.Join(worktree, "note.txt")
	if _, err := handler.Execute(ctx, `{"path":`+quoteJSON(inside)+`}`); err != nil {
		t.Fatalf("worktree 内的路径应放行（节点根优先）：%v", err)
	}
	escape := filepath.Join(filepath.Dir(worktree), "outside.txt")
	if _, err := handler.Execute(ctx, `{"path":`+quoteJSON(escape)+`}`); err == nil {
		t.Fatal("worktree 外路径必须被拒")
	}
}

// TestPathGateCoversBatchAndBashWorkdir：路径入参表不漏（read_batch 的每个
// path、bash 的 workdir 都过同一道门）。
func TestPathGateCoversBatchAndBashWorkdir(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	scope := security.NewProjectScope()
	if err := scope.BindFor("sess-batch", root); err != nil {
		t.Fatalf("bind: %v", err)
	}
	gateway := PathGate(Deps{ProjectScope: scope, SessionKey: func(context.Context) string { return "sess-batch" }})
	ok := frameworktools.HandlerFunc(func(context.Context, string) (string, error) { return "ok", nil })

	batch := gateway("read_batch", frameworktools.ToolMeta{}, ok)
	if _, err := batch.Execute(context.Background(),
		`{"paths":[`+quoteJSON(filepath.Join(root, "ok.txt"))+`,`+quoteJSON(filepath.Join(outside, "x.txt"))+`]}`); err == nil {
		t.Fatal("read_batch 里任意一条越界都必须拒绝")
	}
	bash := gateway(BashToolName, frameworktools.ToolMeta{}, ok)
	if _, err := bash.Execute(context.Background(),
		`{"command":"ls","workdir":`+quoteJSON(outside)+`}`); err == nil {
		t.Fatal("bash workdir 越界必须拒绝")
	}
	if _, err := bash.Execute(context.Background(),
		`{"command":"ls","workdir":`+quoteJSON(root)+`}`); err != nil {
		t.Fatalf("bash workdir 界内应放行：%v", err)
	}
}

// quoteJSON 只用于用例拼路径字面量（Windows 反斜杠必须转义）。
func quoteJSON(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + replacer.Replace(value) + `"`
}
