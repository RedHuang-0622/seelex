package subagentview

import (
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/kernel/state"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// asyncDetailCoordinator 构造一个只带后台作业读面的协调器：作业记录来自执行域
// 的只读投影，输出正文来自执行域的有界尾部读口（两个入口都由装配层注入）。
func asyncDetailCoordinator(records []dto.AsyncRunRecord, output string) *Coordinator {
	return NewCoordinator(Deps{
		Core: &state.Core{},
		Limits: func() dto.ViewLimits {
			return dto.ViewLimits{PlanNodeEvents: 100, EvidenceChars: 800}
		},
		AsyncRuns: func() []dto.AsyncRunRecord { return records },
		AsyncOutputTail: func(handle string, maxBytes int) dto.AsyncOutputTail {
			if handle == "" || maxBytes <= 0 || output == "" {
				return dto.AsyncOutputTail{}
			}
			return dto.AsyncOutputTail{Found: true, Output: output}
		},
	})
}

func runningAsyncRecord() dto.AsyncRunRecord {
	return dto.AsyncRunRecord{
		SessionID:   "sess-1",
		Handle:      "bt-2",
		Kind:        "process",
		Description: "跑一整轮集成测试",
		Command:     "go test ./...",
		State:       dto.AsyncStateRunning,
		ExitCode:    -1,
		LogBytes:    1536,
		Lines:       12,
		Tail:        "=== RUN   TestFoo",
		LogPath:     "/tmp/seelex-async/bt-2.log",
		StartedAt:   time.Now().Add(-76 * time.Second),
	}
}

// TestSubagentDetailServesAsyncJobRow 钉住后台作业行的详情数据面：工作表格的
// `async:<句柄>` 行必须能打开详情（状态/字节/实时输出），而不是报“没有会话”。
//
// 回归背景（2026-10-29 用户实测）：后台测试跑起来后点开那一行的详情，面板永远
// 停在「加载会话记录…」——前端把这一次失败吞成了占位，因为后端对 `async:` 行
// 只走了会话/子代理树/工作行的查找，三条都不命中就返回“no conversation”。
func TestSubagentDetailServesAsyncJobRow(t *testing.T) {
	c := asyncDetailCoordinator(
		[]dto.AsyncRunRecord{runningAsyncRecord()},
		"=== RUN   TestFoo\n--- PASS: TestFoo (1.20s)\n",
	)
	detail, err := c.SubagentDetail("async:bt-2")
	if err != nil {
		t.Fatalf("后台作业行的详情必须可取，不能报错: %v", err)
	}
	if detail == nil || detail.Async == nil {
		t.Fatalf("详情里必须带后台作业读数: %+v", detail)
	}
	job := detail.Async
	if job.Handle != "bt-2" || job.Command != "go test ./..." || job.State != dto.AsyncStateRunning.String() {
		t.Fatalf("作业身份/命令行/状态不符: %+v", job)
	}
	if job.LogBytes != 1536 || job.Lines != 12 || job.LogPath == "" {
		t.Fatalf("字节数/行数/日志路径必须如实搬运: %+v", job)
	}
	if job.Output != "=== RUN   TestFoo\n--- PASS: TestFoo (1.20s)\n" {
		t.Fatalf("实时输出必须来自执行域的有界尾部: %q", job.Output)
	}
	if detail.Status != model.NodeRunning || !detail.Running {
		t.Fatalf("running 作业的状态映射: status=%v running=%v", detail.Status, detail.Running)
	}
	if len(detail.Trace) == 0 {
		t.Fatal("功能打点必须有一条探针打点（状态/字节/末行）")
	}
}

// TestSubagentDetailAsyncJobUnknownHandle 钉住“不在册”的诚实表达：作业已结束被
// 驱逐、或进程重启后登记表清空时，详情仍要打开（不能报错让面板停在占位），
// 只是状态为 unknown 且说明原因。
func TestSubagentDetailAsyncJobUnknownHandle(t *testing.T) {
	c := asyncDetailCoordinator(nil, "")
	detail, err := c.SubagentDetail("async:bt-999")
	if err != nil {
		t.Fatalf("不在册的作业行也必须能打开详情: %v", err)
	}
	if detail == nil || detail.Async == nil || detail.Async.Handle != "bt-999" {
		t.Fatalf("不在册时仍要带句柄身份: %+v", detail)
	}
	if detail.Async.Output != "" || detail.Async.LogBytes != 0 {
		t.Fatalf("不在册时不得编造读数: %+v", detail.Async)
	}
}
