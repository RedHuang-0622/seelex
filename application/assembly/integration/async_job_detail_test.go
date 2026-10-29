package integration

import (
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
	model "github.com/RedHuang-0622/seelex/contract/protocol"
)

// TestAsyncJobRowDetailWiredThroughAssembly 钉住装配链：工作表格的后台作业行
// （`async:<句柄>`）经**装配后的**子代理详情入口也必须能打开，读数是执行域投影、
// 正文是执行域的有界尾部。
//
// 回归背景（2026-10-29 用户实测）：后台测试跑起来后点开那一行的详情，面板永远
// 停在「加载会话记录…」。根因有两段：详情入口只认会话/子代理树/工作行（作业行
// 三条都不命中 → 报 "no conversation"），前端又把这次失败吞成占位。协调器单测钉
// 第一段，这里钉"装配层真的把两个读口接进去了"——漏接时这一层才会红。
func TestAsyncJobRowDetailWiredThroughAssembly(t *testing.T) {
	runtime := &fakeRuntime{
		asyncRuns: []dto.AsyncRunRecord{{
			SessionID:   "sess-bg",
			Handle:      "bt-2",
			Kind:        "process",
			Description: "跑一整轮集成测试",
			Command:     "go test ./...",
			State:       dto.AsyncStateRunning,
			ExitCode:    -1,
			LogBytes:    2048,
			Lines:       3,
			Tail:        "--- FAIL: TestX",
			LogPath:     "/tmp/seelex-async/bt-2.log",
			StartedAt:   time.Now().Add(-76 * time.Second),
		}},
		asyncOutputs: map[string]dto.AsyncOutputTail{
			"bt-2": {Found: true, Output: "=== RUN   TestX\n--- FAIL: TestX (0.30s)\n"},
		},
	}
	service := newLegacyTestService(t, &fakeEngine{}, withTestRuntime(runtime))

	detail, err := service.components.subagentApplication.Detail("async:bt-2")
	if err != nil {
		t.Fatalf("后台作业行的详情必须可取（面板不能停在占位）: %v", err)
	}
	if detail == nil || detail.Async == nil {
		t.Fatalf("详情里必须带作业读数: %+v", detail)
	}
	if detail.Async.Handle != "bt-2" || !detail.Async.Found {
		t.Fatalf("作业身份/在册标记不符: %+v", detail.Async)
	}
	if detail.Async.Output == "" {
		t.Fatal("输出正文没接进详情：装配漏了 AsyncOutputTail")
	}
	if detail.Status != model.NodeRunning || !detail.Running {
		t.Fatalf("running 作业的状态映射: status=%v running=%v", detail.Status, detail.Running)
	}
	if detail.Async.Command != "go test ./..." || detail.Async.LogBytes != 2048 {
		t.Fatalf("命令行/字节数必须如实搬运: %+v", detail.Async)
	}
}
