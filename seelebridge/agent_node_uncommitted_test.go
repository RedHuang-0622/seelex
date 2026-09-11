package seelebridge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/types"
	seenode "github.com/RedHuang-0622/seelex/seelebridge/node"
)

// ── 收尾协议未执行的节点产出保全（2026-09-11 事故回归）────────────────
//
// 事故现场（会话事件库 framework-events.json）：
//  1. lit-en 节点 Chat 成功（seelex.subagent.result status=done，24 篇已核实文献）；
//  2. 进入 worktree 收尾（phase=rebasing）后，因为它在自己 worktree 里留下
//     未提交文件（`?? arxiv1.xml` / `?? bing.py` …，提交数 0），
//     worktree.Finish 返回 "subagent left uncommitted changes"；
//  3. AgentNode.Run 把该收尾错误当作节点错误返回 → 节点判 failed；
//  4. workplan fail-fast 连坐同批兄弟节点 lit-cn（其进行中的 SSE 读到
//     context canceled）→ 整个 fork 失败；
//  5. fork_subagents 只回一个被 error_presentation 兜底成
//     "该工具未能完成本次操作" 的通用错误 → 两个子代理的产出全部丢弃。
//
// 本文件锁定修复后的契约：**收尾协议未执行只降级为显式警告，不销毁节点
// 产出**；同时保留 worktree 的 P0 保证——未提交产出绝不静默删除。

// worktreeLeftoverCompleter 模拟"子代理产出了结论、但在自己的 worktree 里
// 留下未提交文件"（典型：只做调研/分析，把临时探针脚本落在工作区）。
// NodeScope.WorkspaceID 即该节点的 worktree 路径（AgentNode.Run 注入）。
type worktreeLeftoverCompleter struct {
	reply    string
	leftover string
}

func (c *worktreeLeftoverCompleter) Complete(ctx context.Context, _ []types.Message, _ []types.Tool) (types.Message, error) {
	scope, _ := seenode.NodeScopeFromContext(ctx)
	if scope.WorkspaceID != "" {
		_ = os.WriteFile(filepath.Join(scope.WorkspaceID, c.leftover), []byte("probe\n"), 0o644)
	}
	reply := c.reply
	return types.Message{Role: "assistant", Content: &reply}, nil
}

// TestPlanNodeUncommittedWorktreeKeepsResult 红灯复现：子代理 Chat 成功但
// worktree 有未提交改动时，plan_run 不得整体失败，节点结论必须仍然交付，
// 且未提交现场必须保留并可被前端查询。
func TestPlanNodeUncommittedWorktreeKeepsResult(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	repo := setupGitRepo(t)
	if err := runtime.BindProjectRoot(repo); err != nil {
		t.Fatal(err)
	}
	probe := &worktreeLeftoverCompleter{reply: "调研结论：已核实 24 篇文献", leftover: "probe.py"}
	injectScriptedCompleters(t, runtime, map[string]agent.Completer{"agent-1": probe})

	planJSON := `{"entry":"do","nodes":{"do":{"input":"调研文献","kind":"agent"}},"edges":{}}`
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", planJSON); err != nil {
		t.Fatal(err)
	}
	out, err := runtime.Agent().DirectDispatch(context.Background(), "plan_run", `{}`)
	if err != nil {
		t.Fatalf("收尾协议未执行不得让 plan_run 整体失败（会连坐兄弟节点并丢弃产出）：%v\n%s", err, out)
	}
	if !strings.Contains(out, `"status":"completed"`) {
		t.Fatalf("plan_run status 必须为 completed，实际：%s", out)
	}
	if !strings.Contains(out, "调研结论") {
		t.Fatalf("节点结论必须仍然交付给父代理：%s", out)
	}

	// 现场保留（P0 保证：未提交产出绝不静默删除）。
	wtPath := filepath.Join(filepath.Dir(repo), filepath.Base(repo)+"-seelex-do")
	if _, statErr := os.Stat(filepath.Join(wtPath, "probe.py")); statErr != nil {
		t.Fatalf("未提交产出必须保留在 worktree 现场：%v", statErr)
	}
	if _, ok := runtime.worktreeMgr.Info("do"); !ok {
		t.Fatal("未合并收尾的 worktree 必须留在注册表，供前端展示现场与人工恢复")
	}
	// 产出携带可操作提示：父代理/用户必须知道改动未合并及现场位置。
	// （JSON 会转义路径反斜杠，故断言稳定的 worktree 目录后缀。）
	if !strings.Contains(out, "未提交") || !strings.Contains(out, "-seelex-do") {
		t.Fatalf("节点产出必须携带未合并警告与现场路径：%s", out)
	}
}
