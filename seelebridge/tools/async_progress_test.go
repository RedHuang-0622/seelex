package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// pollRaw 直接取 handler 的原始返回字符串——"两次取回逐字节相同"必须按字节判，
// 结构体相等不等于重发出去的字节相等。
func pollRaw(t *testing.T, router *Router, ctx context.Context, handle string, waitMS int) string {
	t.Helper()
	args, err := json.Marshal(map[string]interface{}{"op": "fetch", "handle": handle, "wait_ms": waitMS})
	if err != nil {
		t.Fatal(err)
	}
	output, err := router.scopedJobManage(ctx, string(args))
	if err != nil {
		t.Fatalf("poll %s: %v", handle, err)
	}
	return output
}

// TestQuietPollResultsAreByteIdentical 钉住 D1 的前提事实：一条**安静**的长命令
// （还没有新输出）被连续取回两次，两次结果必须逐字节相同——载荷里放时间戳就等于
// 每轮重烧一次前缀缓存（见 asyncPayload 头注与 A/B 判据 5）。
//
// 正因为字节相同，core 侧"按载荷指纹算进展"的口径会把还在跑的回合判死；修法是
// "有在途后台执行被查询"本身算进展（见 application/core/task_context）。
func TestQuietPollResultsAreByteIdentical(t *testing.T) {
	router := asyncTestRouter(t, true)
	ctx := asyncTestCtx(t.TempDir(), "sess-quiet")

	receipt := dispatchForTest(t, router, ctx, "sleep 25")

	first := pollRaw(t, router, ctx, receipt.Handle, -1)
	second := pollRaw(t, router, ctx, receipt.Handle, -1)
	if got := decodeAsyncPayload(t, first); got.Status != "progress" || got.State != asyncStateRunning || got.Output != "" {
		t.Fatalf("安静命令的取回 = %+v，want progress/running/无增量", got)
	}
	if first != second {
		t.Fatalf("同一安静句柄的两次取回必须逐字节相同：\n%s\n%s", first, second)
	}
	if !strings.Contains(first, "job_manage") {
		t.Fatal("取回正文必须把下一步动作告诉模型")
	}

	// 在途计数是那个判据唯一的真值来源：只数本会话、只数还在跑的。
	if got := router.AsyncPendingFor("sess-quiet"); got != 1 {
		t.Fatalf("AsyncPendingFor = %d，want 1", got)
	}
	if got := router.AsyncPendingFor("sess-other"); got != 0 {
		t.Fatalf("别的会话不该看到别人的在途执行: %d", got)
	}

	router.CloseSessionAsync("sess-quiet")
	waitAsyncTerminalForTest(t, router, receipt.Handle)
	if got := router.AsyncPendingFor("sess-quiet"); got != 0 {
		t.Fatalf("终态后仍在途计数 = %d，want 0", got)
	}
}

// TestAsyncPendingDisabledReportsZero 能力关闭时计数恒 0：没有能力就没有"在途"可言，
// 判据因此退回原行为，而不是被一个假的正数放宽。
func TestAsyncPendingDisabledReportsZero(t *testing.T) {
	router := asyncTestRouter(t, false)
	if got := router.AsyncPendingFor("sess-quiet"); got != 0 {
		t.Fatalf("能力关闭时 AsyncPendingFor = %d，want 0", got)
	}
}
