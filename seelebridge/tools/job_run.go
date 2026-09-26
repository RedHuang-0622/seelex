package tools

import (
	"context"
	"time"
)

// job_run.go — 作业契约的**执行侧**：非进程作业（inline / subagent）的起、收尾与
// "等一下再取回"的等待预算。表与状态机在 async_exec.go，工具面在 job_tools.go。
//
// 为什么 inline / subagent 也走同一个日志文件载体：契约的 fetch 是"取回增量"，
// 而"增量"的唯一定义是**文件偏移**（`cursor`）。把非进程作业的产出也落成文件，
// 三类作业就共用同一套 observe（不推游标）/ fetch（消费式）/ kill（保留已产出）/
// done（销项）语义——差异只剩"谁来写这个文件、怎么取消"。

// awaitAsyncDeadline 等到执行体收尾、等待预算用尽或调用被取消——三者都返回，
// 让取回带回"此刻"的增量（等待不是目的，交付增量才是）。
func awaitAsyncDeadline(ctx context.Context, done <-chan struct{}, waitMS int) {
	budget := clampAsyncWaitMS(waitMS)
	if budget <= 0 {
		return
	}
	timer := time.NewTimer(time.Duration(budget) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	case <-ctx.Done():
	}
}

// clampAsyncWaitMS 归一 wait_ms：负数 = 不等待（立刻交付当前增量）；0/省略 =
// 默认 5s；超上限按上限——模型给一个大数不该让这一问卡住整轮（上限同时是
// 单次工具调用钉住会话的上限，见不变量 I-22）。
func clampAsyncWaitMS(waitMS int) int {
	switch {
	case waitMS < 0:
		return 0
	case waitMS == 0:
		return asyncDefaultWaitMS
	case waitMS > asyncMaxWaitMS:
		return asyncMaxWaitMS
	default:
		return waitMS
	}
}
