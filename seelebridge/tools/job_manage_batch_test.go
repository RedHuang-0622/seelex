package tools

// job_manage_batch_test.go — `job_manage` 的**多句柄**面（handles）：一次等一批、
// 一次看一批。它是"一批派发出去的作业本来就是一个工作单元"这条事实的工具面落点
// （read_batch 的 N 个读、fork_subagents 的一批子代理）。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// manageBatchForTest 走 job_manage 的 handles 入参，返回批量取回载荷。
func manageBatchForTest(t *testing.T, router *Router, ctx context.Context, op string, handles []string, waitMS int) (asyncBatchPayload, error) {
	t.Helper()
	args, err := json.Marshal(map[string]interface{}{
		"op": op, "handles": handles, "wait_ms": waitMS,
	})
	if err != nil {
		t.Fatal(err)
	}
	output, err := router.scopedJobManage(ctx, string(args))
	if err != nil {
		return asyncBatchPayload{}, err
	}
	var payload asyncBatchPayload
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("批量载荷不是合法 JSON: %v (%q)", err, output)
	}
	return payload, nil
}

// TestJobManageFetchManyReturnsWholeBatch 一次等一批并各自取回增量：三条作业全部
// 终态、各自那份正文都在、取回即销项。
func TestJobManageFetchManyReturnsWholeBatch(t *testing.T) {
	router := asyncTestRouter(t, true)
	ctx := asyncTestCtx(t.TempDir(), "sess-batch")

	markers := []string{"batch-one", "batch-two", "batch-three"}
	handles := make([]string, 0, len(markers))
	for _, marker := range markers {
		handles = append(handles, dispatchForTest(t, router, ctx, "echo "+marker).Handle)
	}

	payload, err := manageBatchForTest(t, router, ctx, "fetch", handles, 30000)
	if err != nil {
		t.Fatalf("批量取回: %v", err)
	}
	if payload.Status != "finished" {
		t.Fatalf("全部终态后 status = %q, want finished（%+v）", payload.Status, payload)
	}
	if payload.Count != len(handles) || len(payload.Jobs) != len(handles) {
		t.Fatalf("批量载荷 = %d 条 / count=%d，want %d", len(payload.Jobs), payload.Count, len(handles))
	}
	var joined strings.Builder
	for _, job := range payload.Jobs {
		if job.State != asyncStateDone {
			t.Fatalf("作业 %s 状态 = %q，want done（%+v）", job.Handle, job.State, job)
		}
		joined.WriteString(job.Output)
	}
	for _, marker := range markers {
		if got := strings.Count(joined.String(), marker); got != 1 {
			t.Fatalf("增量交付必须恰好一次：%s 出现 %d 次（正文 %q）", marker, got, joined.String())
		}
	}
	// 终态 + 已交付 ⇒ 销项（一批一起结清）。
	for _, handle := range handles {
		if _, ok := router.async.snapshot(handle); ok {
			t.Fatalf("终态取回之后句柄 %s 仍在登记表里", handle)
		}
	}
}

// TestJobManageFetchManySharesWaitBudget 整批**共用一份**等待预算：4 条件业在跑、
// wait_ms=1s 时，一次调用应在 1s 量级返回，而不是逐条各等 1s（4s）。
func TestJobManageFetchManySharesWaitBudget(t *testing.T) {
	router := asyncTestRouter(t, true)
	ctx := asyncTestCtx(t.TempDir(), "sess-budget")

	// 去重键是"同一会话同一命令"：四条命令必须各不相同，否则只有一条真的派发。
	handles := make([]string, 0, 4)
	for _, command := range []string{"sleep 21", "sleep 22", "sleep 23", "sleep 24"} {
		handles = append(handles, dispatchForTest(t, router, ctx, command).Handle)
	}
	t.Cleanup(func() {
		for _, handle := range handles {
			_, _ = manageForTest(t, router, ctx, "kill", handle)
		}
		for _, handle := range handles {
			if run, ok := router.async.snapshot(handle); ok {
				select {
				case <-run.done:
				case <-time.After(30 * time.Second):
				}
			}
		}
	})

	started := time.Now()
	payload, err := manageBatchForTest(t, router, ctx, "fetch", handles, 1000)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("批量取回: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("整批等待用了 %v：预算被逐条重复？（一次调用只该有一份预算）", elapsed)
	}
	if payload.Status != "progress" {
		t.Fatalf("仍在跑时 status = %q, want progress（%+v）", payload.Status, payload)
	}
	for _, job := range payload.Jobs {
		if job.State != asyncStateRunning {
			t.Fatalf("作业 %s 状态 = %q，want running", job.Handle, job.State)
		}
	}
}

// TestJobManageFetchManyRejectsUnknownHandle 批量里有未知句柄 = 明确报错，
// 不静默跳过（静默跳过会让模型以为那一行已经取过）。
func TestJobManageFetchManyRejectsUnknownHandle(t *testing.T) {
	router := asyncTestRouter(t, true)
	ctx := asyncTestCtx(t.TempDir(), "sess-unknown")
	ack := dispatchForTest(t, router, ctx, "echo known-marker")

	if _, err := manageBatchForTest(t, router, ctx, "fetch", []string{ack.Handle, "a404"}, -1); err == nil {
		t.Fatal("批量里带未知句柄必须报错")
	}
	waitAsyncTerminalForTest(t, router, ack.Handle)
}

// TestJobManageObserveManyListsEachHandle 一次看一批：每条一行只读读数，不推进游标。
func TestJobManageObserveManyListsEachHandle(t *testing.T) {
	router := asyncTestRouter(t, true)
	ctx := asyncTestCtx(t.TempDir(), "sess-observe")
	first := dispatchForTest(t, router, ctx, "echo observe-one").Handle
	second := dispatchForTest(t, router, ctx, "echo observe-two").Handle

	args, err := json.Marshal(map[string]interface{}{"op": "observe", "handles": []string{first, second}})
	if err != nil {
		t.Fatal(err)
	}
	output, err := router.scopedJobManage(ctx, string(args))
	if err != nil {
		t.Fatalf("批量观察: %v", err)
	}
	// 观察走的是同一份只读投影，句柄两行都在。
	for _, handle := range []string{first, second} {
		if !strings.Contains(output, handle) {
			t.Fatalf("批量观察缺 %s: %q", handle, output)
		}
	}
	// 只读不销项：观察之后仍在登记表里。
	if _, ok := router.async.snapshot(first); !ok {
		t.Fatal("observe 不得销项/改状态")
	}
	waitAsyncTerminalForTest(t, router, first)
	waitAsyncTerminalForTest(t, router, second)
}

// TestJobManageRejectsMultiHandleKillAndDone kill / done 一次只认一条：kill 会终止
// 整批共用编排（一条影响 N 条），done 是"我确认收到这一条"的动作——都不该被数组放大。
func TestJobManageRejectsMultiHandleKillAndDone(t *testing.T) {
	router := asyncTestRouter(t, true)
	ctx := asyncTestCtx(t.TempDir(), "sess-single")
	handles := []string{
		dispatchForTest(t, router, ctx, "echo single-one").Handle,
		dispatchForTest(t, router, ctx, "echo single-two").Handle,
	}
	t.Cleanup(func() {
		for _, handle := range handles {
			_, _ = manageForTest(t, router, ctx, "kill", handle)
		}
		waitAsyncTerminalForTest(t, router, handles[0])
		waitAsyncTerminalForTest(t, router, handles[1])
	})

	for _, op := range []string{"kill", "done"} {
		args, err := json.Marshal(map[string]interface{}{"op": op, "handles": handles})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := router.scopedJobManage(ctx, string(args)); err == nil {
			t.Fatalf("op=%s 收到多条句柄必须报错", op)
		}
	}
}
