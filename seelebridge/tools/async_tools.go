package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// 后台命令的轮询型执行域·工具面：async_output（取回增量）与 async_kill（终止），
// 以及 bash 的 background 提示。表与状态机在 async_exec.go，执行体在 async_run.go。

// ── async_output：取回增量 ───────────────────────────────────────────────

type asyncOutputInput struct {
	Handle string `json:"handle"`
	WaitMS int    `json:"wait_ms,omitempty"`
}

// scopedAsyncOutput 取回一次后台执行的进展/终态。只交付**新增**输出：反复轮询
// 同一句柄不会把整份输出重播进上下文（那是轮询型唯一真实的 token 风险）。
//
// 授权：句柄只对本会话有效（跨会话取回直接拒绝）；取回本身是只读，不重复弹
// 审批——它取的是已经获批的那次派发的输出。
func (r *Router) scopedAsyncOutput(ctx context.Context, argsJSON string) (string, error) {
	if !r.asyncEnabled() {
		return "", fmt.Errorf("async_output: %s", asyncDisabledText)
	}
	var input asyncOutputInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("async_output: invalid args: %w", err)
	}
	if input.Handle == "" {
		return "", fmt.Errorf("async_output: handle is required")
	}
	run, ok := r.async.snapshot(input.Handle)
	if !ok {
		return "", fmt.Errorf("async_output: 未知句柄 %q（可能已被驱逐，或进程重启后登记表已清空）", input.Handle)
	}
	if run.sessionID != r.sessionKey(ctx) {
		return "", fmt.Errorf("async_output: 句柄 %q 不属于本会话", input.Handle)
	}
	if run.state == asyncStateRunning {
		awaitAsyncDeadline(ctx, run.done, input.WaitMS)
	}
	next, delta, truncated, ok := r.async.advanceTail(input.Handle, asyncPollTailBudget)
	if !ok {
		return "", fmt.Errorf("async_output: 句柄 %q 已不在登记表里", input.Handle)
	}
	r.async.markCursor(input.Handle, next, truncated)
	fresh, _ := r.async.snapshot(input.Handle)
	return renderPolled(fresh, delta, truncated)
}

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

// ── async_kill：终止一棵进程树 ───────────────────────────────────────────

type asyncKillInput struct {
	Handle string `json:"handle"`
}

// scopedAsyncKill 终止一个后台执行。只终止、不取回：结果一律走 async_output，
// 否则同一次调用有两种返回形状，模型分不清"杀掉了"和"拿到结果了"。
//
// 三种返回：杀成功（status=killed，终态由执行体收尾时合成）、句柄已终态
// （status=already_finished，不是错误）、终止请求失败（error，绝不谎报已杀）。
func (r *Router) scopedAsyncKill(ctx context.Context, argsJSON string) (string, error) {
	if !r.asyncEnabled() {
		return "", fmt.Errorf("async_kill: %s", asyncDisabledText)
	}
	var input asyncKillInput
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("async_kill: invalid args: %w", err)
	}
	if input.Handle == "" {
		return "", fmt.Errorf("async_kill: handle is required")
	}
	tree, alreadyDone, err := r.async.killTarget(input.Handle, r.sessionKey(ctx))
	if err != nil {
		return "", err
	}
	if alreadyDone {
		run, _ := r.async.snapshot(input.Handle)
		return renderAlreadyFinished(run)
	}
	// 终止在锁外做：finish 也要那把锁，等 TerminateJobObject / taskkill 返回不能把
	// 整张表按住。失败就报错，绝不谎报已杀。
	if err := tree.Terminate(); err != nil {
		return "", fmt.Errorf("async_kill: 句柄 %q 终止失败: %w", input.Handle, err)
	}
	// 只有确认发出终止才落 killed 意图——否则状态会跑在进程前面。
	r.async.markKilled(input.Handle)
	run, _ := r.async.snapshot(input.Handle)
	return renderKilled(run)
}

// ── 工具面 ──────────────────────────────────────────────────────────────

// asyncOutputSchema 是 async_output 的入参 schema。
func asyncOutputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"handle":  map[string]interface{}{"type": "string"},
			"wait_ms": map[string]interface{}{"type": "integer"},
		},
		"required": []string{"handle"},
	}
}

// asyncOutputDescription 说明取回语义：只给增量、wait_ms 三档、句柄是本会话的，
// 并且明确"确认进展不必花一次往返"——轮询的成本长在信封重发上，能省一轮是一轮。
func asyncOutputDescription() string {
	return "Fetch the incremental output of a command dispatched with bash background=true. " +
		"Each call returns only the bytes produced since the previous call for that handle " +
		"(never a replay of the whole log). wait_ms: negative returns immediately, 0 or omitted " +
		"waits up to 5s, values above 60000 are capped at 60000 — set it near the command's " +
		"expected remaining time so one call finishes the wait. Do not call this just to check " +
		"whether the command is still running: the work-table trace block already lists every " +
		"running handle."
}

// asyncKillSchema 是 async_kill 的入参 schema。
func asyncKillSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"handle": map[string]interface{}{"type": "string"}},
		"required":   []string{"handle"},
	}
}

// asyncKillDescription 说明终止语义：杀整棵进程树、只终止不取回、终态要看一次取回。
func asyncKillDescription() string {
	return "Terminate a background command started with bash background=true, including its " +
		"child processes. The result is a receipt, not the command output: call " +
		"async_output(handle) afterwards to get the tail and the terminal state " +
		"(state=killed, exit_code=137). Handles are scoped to the calling session."
}

// asyncBackgroundHint 是 bash 描述在能力常驻时追加的一句：派发回执不含输出、
// background 必须带 description（工作表格的行标题）、取回与终止各用哪个工具。
// 不写清，模型会以为 background 只是"超时更长"。
const asyncBackgroundHint = " With background=true the command runs in the background and that call's result is only " +
	"an acceptance receipt (handle + log_path), never the command output — use async_output(handle) " +
	"for results and async_kill(handle) to terminate it. background=true requires description: " +
	"one line on what the command is doing, which becomes the work-table row title."
