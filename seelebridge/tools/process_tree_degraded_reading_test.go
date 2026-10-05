package tools

// process_tree_degraded_reading_test.go — ③U5 的红灯：**同步链必须说得出口"终止只及直接子进程"**。
//
// U5 的原始问题（docs/arch/workunit-duplication-inventory.md §四 U5）：进程树退化路径上的
// 两条链**对外主张不一致**。装配早已收成一份（`newProcessTreeCommand` + `startWithProcessTree`，
// 两条链同源，挂不上都不放弃执行），差的只是**读数**：
//
//	- 后台链有读数：探针把 `Degraded` 折进 `AsyncRunInfo.Degraded`（工作表格那一栏）与观察行
//	  "· 进程树挂不上，终止只及直接子进程"（async_probe.go:62/:125）；
//	- 同步链（bash / bash_read，含 docker 重试）**一个读数都没有**：树是局部变量，从头到尾
//	  没人读过 `Degraded()`——命令照跑、输出照给，调用方不知道"停止"打不到孙进程。
//
// 本用例钉的判据两条：
//
//	① 真退化时，同步链产出 `bash.process.degraded` 这条读数；
//	② 不退化时一个都不发（正常机器上 Job 建得出来，诊断阶段序列必须与从前一致——
//	   既有用例 TestScopedBashPublishesDiagnosticStages 的"逐个阶段相等"就是这条的守卫）。
//
// 退化状态没法在真机上按需复现（本机 `CreateJobObject` 成功），所以换掉建树的工厂：
// `&security.ProcessTree{}` 就是"Job 没建成"的真实形态（`Degraded()` 判 `job == 0`）。

import (
	"context"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelebridge/security"
)

// withDegradedProcessTree 在本用例期间把建树工厂换成"Job 没建成"的形态（用完立刻还原）。
func withDegradedProcessTree(t *testing.T) {
	t.Helper()
	restore := newExecProcessTree
	newExecProcessTree = func() *security.ProcessTree { return &security.ProcessTree{} }
	t.Cleanup(func() { newExecProcessTree = restore })
}

// diagnosticStages 取本轮诊断事件的阶段名序列。
func diagnosticStages(events []BashDiagnosticEvent) []string {
	stages := make([]string, 0, len(events))
	for _, event := range events {
		stages = append(stages, event.Stage)
	}
	return stages
}

func TestSyncChainReportsDegradedProcessTree(t *testing.T) {
	var events []BashDiagnosticEvent
	router := NewRouter(Deps{
		ToolCallTimeout:        time.Minute,
		ObserveBash:            func(event BashDiagnosticEvent) { events = append(events, event) },
		DisableDockerAutoStart: true,
	})
	ctx := model.WithNodeScope(context.Background(), model.NodeScope{
		NodeID: "x", Role: model.RoleSubAgent, BranchID: "x", WorkspaceID: t.TempDir(),
	})

	// ① 退化：命令照跑（挂不上不放弃执行），但"终止只及直接子进程"必须说出去。
	withDegradedProcessTree(t)
	output, err := router.scopedBash(ctx, `{"command":"echo degraded-probe"}`)
	if err != nil {
		t.Fatalf("树挂不上不该让命令失败（派发已经发生）：%v", err)
	}
	if output == "" {
		t.Fatal("退化路径仍必须把命令产出交回调用方")
	}
	degraded := false
	for _, event := range events {
		if event.Stage == bashProcessDegradedStage {
			degraded = true
		}
	}
	if !degraded {
		t.Fatalf("同步链必须报出「进程树挂不上」这条读数，实际阶段 = %v", diagnosticStages(events))
	}
}

// TestSyncChainStaysSilentWhenTreeIsHealthy 是阴性对照：不退化时一个字都不许说
// （否则"退化"这条读数就变成噪声，调用方再也不会看它）。
func TestSyncChainStaysSilentWhenTreeIsHealthy(t *testing.T) {
	var events []BashDiagnosticEvent
	router := NewRouter(Deps{
		ToolCallTimeout:        time.Minute,
		ObserveBash:            func(event BashDiagnosticEvent) { events = append(events, event) },
		DisableDockerAutoStart: true,
	})
	ctx := model.WithNodeScope(context.Background(), model.NodeScope{
		NodeID: "x", Role: model.RoleSubAgent, BranchID: "x", WorkspaceID: t.TempDir(),
	})

	if _, err := router.scopedBash(ctx, `{"command":"echo healthy-probe"}`); err != nil {
		t.Fatal(err)
	}
	for _, stage := range diagnosticStages(events) {
		if stage == bashProcessDegradedStage {
			t.Fatalf("没退化却报了退化（阶段 = %v）", diagnosticStages(events))
		}
	}
}
