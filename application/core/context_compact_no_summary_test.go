package core

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
)

// compactionIndexNoSummaryRuntime 在索引面之上再实现「这次折叠能不能拿到模型
// 读后感」的窄可选探针，并回答**不能**：模拟生效配置里折叠处厚摘要开关关闭
// （或 QuickChat 装配失败、摘要器构造失败）的宿主——这条链路上折叠 DAG 的
// chapter2Node 必然落到本地确定性折叠。
//
// 探针刻意与 PushCompactionFrame 分开：compactionIndexRuntime（既有三条用例的
// 夹具）**不实现**它，走的是"探针缺省 = 可用、折叠照常"的兼容路径——这条用例
// 因此也是在钉"探针缺省不改变既有宿主行为"。
type compactionIndexNoSummaryRuntime struct {
	compactionIndexRuntime
}

func (runtime *compactionIndexNoSummaryRuntime) CompactionSummaryAvailable() bool { return false }

// TestFoldWithoutModelSummaryLeavesContextAndStackUntouched 钉住用户口径
// （2026-10-01）：**没有模型读后感**时这次折叠不落地——不折上下文（原来的轮次
// 原样继续 append）、不推压缩栈顶（索引面一次都不被问到）、不落压缩记录；只把这
// 次判据如实留痕（终局 outcome=skipped_no_summary + Detail 里的 skipped=no_summary）。
//
// 为什么必须这样：一帧的价值分配是「元数据 + 模型读后感」（见
// context_runtime/compaction_frame.go 的文件头），缺了读后感的一帧对检索毫无用处，
// 而折叠会改写请求前缀、把 provider 的整段前缀缓存作废。用户现场把这条归纳成
// 「压缩没有出读后感只有折叠」——折了也压不出东西，还把缓存废掉。
func TestFoldWithoutModelSummaryLeavesContextAndStackUntouched(t *testing.T) {
	recorder := &compactionIndexRecorder{receipt: context_runtime.CompactionIndexReceipt{
		SegmentID: "compact-must-not-push", SummarySource: "local",
	}}
	runtime := &compactionIndexNoSummaryRuntime{compactionIndexRuntime{
		runtimeWithContextLimits: runtimeWithContextLimits{fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192},
		recorder:                 recorder,
	}}
	engine := &fakeEngine{}
	service := newTestService(t, engine, withTestRuntime(runtime))
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-nosummary"}
	service.components.tasks.BeginTask("task-nosummary", "inspect", "high", nil, TaskCheckpoint{})
	service.ViewMu.Unlock()
	appendIndexRounds(t, service, "task-nosummary")
	sessionID := service.Snapshot().Session.ID

	subscription, err := service.SubscribeSession(sessionID, 256)
	if err != nil {
		t.Fatalf("SubscribeSession: %v", err)
	}
	defer subscription.Close()

	ctx := task_context.WithSessionID(context.Background(), sessionID)
	result, err := service.CompactContextNow(ctx)
	if err != nil {
		t.Fatalf("CompactContextNow: %v", err)
	}

	// ① 不折：没有折叠、没有记录。回执必须说清"这次为什么没折"（没有模型读后感），
	// 而不是让用户以为判据没命中。
	if result.Compacted || result.Recorded || result.Scheduled {
		t.Fatalf("没有模型读后感时不得折叠、不得落记录：%+v", result)
	}
	if !strings.Contains(result.Note, "拿不到模型读后感") {
		t.Fatalf("回执应说明这次为什么没折（拿不到模型读后感）：%q", result.Note)
	}

	// ② 不推压缩栈顶：索引面一次都没被问到。
	requests, _ := recorder.snapshot()
	if len(requests) != 0 {
		t.Fatalf("没有读后感时不得推帧（compact stack top 不许动）：%+v", requests)
	}

	// ③ 上下文原样 append：累积起点没有前移，最旧的轮次仍在 provider 历史里。
	service.ViewMu.RLock()
	state := service.components.tasks.CurrentTaskExecutionFor(sessionID)
	retainedFrom, compactions := 0, 0
	if state != nil {
		retainedFrom = state.ContextRetainedFrom
		compactions = len(state.ContextCompactions)
	}
	service.ViewMu.RUnlock()
	if retainedFrom != 0 {
		t.Fatalf("累积起点被前移（上下文被折了）：ContextRetainedFrom=%d，want 0", retainedFrom)
	}
	if compactions != 0 {
		t.Fatalf("没有折叠就不该留下压缩记录：%d 条", compactions)
	}
	foundOldest := false
	for _, message := range engine.History() {
		if strings.Contains(message.Content, "question-0") {
			foundOldest = true
		}
	}
	if !foundOldest {
		t.Fatal("最旧的轮次被折出了 provider 历史：上下文没有保持原样")
	}

	// ④ 留痕：进度帧终局报 skipped_no_summary，Detail 写明原因（进度条不该走到一半
	// 就沉默——读者需要一个能自答的句号）。
	frames := drainCompactionProgress(t, subscription)
	assertProgressShape(t, frames, sessionID)
	terminal := frames[len(frames)-1]
	if terminal.event.Outcome != string(context_runtime.CompactSkippedNoSummary) {
		t.Fatalf("进度终局 outcome = %q，want %q", terminal.event.Outcome, context_runtime.CompactSkippedNoSummary)
	}
	if !strings.Contains(terminal.event.Detail, "skipped=no_summary") {
		t.Fatalf("进度终局 Detail = %q，缺少 skipped=no_summary", terminal.event.Detail)
	}
}
