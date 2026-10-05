package tools

// process_tree_assembly_test.go — 钉住「进程树装配只有一份」（③C）。
//
// 装配这条序列（NewProcessTree → CommandContext → winhide → Dir → ConfigureHiddenCommand
// → ConfigureProcessTree → Cancel = tree.Terminate() → WaitDelay）原先在两条链里各写一遍：
// 同步链 router.newScopedCommand、后台链 async_run.startAsync。长得一样、语义也一样，
// 却不在一处——一条链补了兜底、另一条漏掉，没人会发现。
//
// 收成一份之后，**超时与取消策略仍然有意不并**：后台链用
// `context.WithTimeout(context.WithoutCancel(ctx), asyncHardCap)`（受理回执一返回，
// 本次调用 ctx 就失效，沿用它会把刚起的命令连带杀掉），同步链用
// `context.WithTimeout(ctx, r.scopedToolTimeout(...))`（受本次调用预算约束、可被停止）。
// 所以本用例钉的是**装配**，并钉住"装配的输入里有 runCtx 这一件"。

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestProcessTreeAssemblyIsSingleAcrossChains(t *testing.T) {
	workdir := t.TempDir()
	shell, shellArgs := scopedBashCommand("echo hi")

	syncCtx, syncCancel := context.WithCancel(context.Background())
	defer syncCancel()
	syncCmd, syncTree := newProcessTreeCommand(syncCtx, shell, shellArgs, workdir)
	defer syncTree.Close()

	backgroundCtx, backgroundCancel := context.WithCancel(context.Background())
	defer backgroundCancel()
	backgroundCmd, backgroundTree := newProcessTreeCommand(backgroundCtx, shell, shellArgs, workdir)
	defer backgroundTree.Close()

	cmds := map[string]*exec.Cmd{"同步链": syncCmd, "后台链": backgroundCmd}
	for name, cmd := range cmds {
		if cmd == nil {
			t.Fatalf("%s: 装配必须返回命令", name)
		}
		if cmd.Cancel == nil {
			t.Fatalf("%s: 取消必须换成整组终止（tree.Terminate）", name)
		}
		if cmd.WaitDelay != asyncWaitDelay {
			t.Fatalf("%s: WaitDelay = %v，want %v（兜底不许两条链各写一份）", name, cmd.WaitDelay, asyncWaitDelay)
		}
		if cmd.SysProcAttr == nil {
			t.Fatalf("%s: 进程组位没落上（ConfigureProcessTree 未生效）", name)
		}
		if cmd.Dir != workdir {
			t.Fatalf("%s: Dir = %q，want %q", name, cmd.Dir, workdir)
		}
		if filepath.Base(cmd.Path) != filepath.Base(shell) {
			t.Fatalf("%s: 起命令位 = %q，want %q", name, cmd.Path, shell)
		}
		if len(cmd.Args) < 1 || cmd.Args[0] != cmd.Path {
			t.Fatalf("%s: Args[0] 应与起命令位一致：%v", name, cmd.Args)
		}
	}

	// 装配的输入里有 runCtx：两条链传进来的是**各自的** ctx，命令各自受它约束。
	if syncCmd.Cancel == nil || backgroundCmd.Cancel == nil {
		t.Fatal("Cancel 必须由装配统一装上")
	}
	// 一棵树一个终止器：两条链的取消各管自己那一组进程，不共用实现。
	if syncTree == backgroundTree {
		t.Fatal("两条链必须各自持有自己的进程树（共用一棵等于把另一条链的进程也杀了）")
	}
}
