package tools

// process_tree_degraded_bench_test.go — ③U5 落地后的**量级读数**：同步链每条命令多出来的那一次
// 退化判据（`tree.Degraded()` + 不退化就直接返回）要花多少。
//
// 用法：`go test ./seelebridge/tools/ -run '^$' -bench ProcessTreeDegraded -benchmem -count=1`
//
// 为什么量这个：U5 补的是读数，判据落在**每条同步命令一次**的路径上（`executeScopedBash` 主体与
// docker 重试各一处），所以它必须是纳秒档、且不分配——否则"补读数"就变成了给 bash 加开销。
// 拿它跟一次同步 bash 的量级比：命令本身要起一个 shell 进程（毫秒档），判据是它的万分之一。

import (
	"testing"

	"github.com/RedHuang-0622/seelex/seelebridge/security"
)

// BenchmarkNoteProcessTreeDegradedHealthy 量的是生产上**绝大多数时候**走的那条：树健康 →
// 一次 `Degraded()`（一把锁 + 一次比较）后立刻返回，不发事件。
func BenchmarkNoteProcessTreeDegradedHealthy(b *testing.B) {
	router := NewRouter(Deps{ObserveBash: func(BashDiagnosticEvent) {}})
	tree := security.NewProcessTree()
	b.Cleanup(tree.Close)

	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		router.noteProcessTreeDegraded("bash.exe", tree)
	}
}

// BenchmarkNoteProcessTreeDegradedReported 量退化时那条：多一次诊断事件投递（观察者空实现）。
func BenchmarkNoteProcessTreeDegradedReported(b *testing.B) {
	var events int
	router := NewRouter(Deps{ObserveBash: func(BashDiagnosticEvent) { events++ }})
	tree := &security.ProcessTree{} // Job 没建成 = 真退化

	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		router.noteProcessTreeDegraded("bash.exe", tree)
	}
	if events == 0 {
		b.Fatal("退化必须报出读数")
	}
}
