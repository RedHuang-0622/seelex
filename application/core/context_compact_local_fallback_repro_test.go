package core

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
)

// compactionReadbackRuntime 在索引面之上再实现「压缩之前先试一次模型读后感」的
// 窄可选探针（compactionReadbackProbe），按夹具给定的回执回答"这次到底有没有
// 模型读后感"。
//
// 探针刻意与 PushCompactionFrame 分开，且**不实现**它的 fake/harness（
// compactionIndexRuntime）走的是"没有读数闸 = 压缩照常"的兼容路径——因此这条
// 夹具同时钉住"读数闸缺省不改变既有宿主行为"。
type compactionReadbackRuntime struct {
	compactionIndexRuntime
	readback context_runtime.CompactionIndexReceipt
	// readbackErr 是夹具给的读数闸**调用**报错（非 nil = 这次回读本身失败，回执
	// 因此无效）。它与 readback 是两种形态：readback 说"调用成功了，但回执里只有
	// 本地压缩"（summary_source=local），readbackErr 说"调用就没跑成"。
	readbackErr error
	// lastReadback 记录读数闸最后一次收到的入参：重启/冷恢复到底剥夺了哪些输入
	// （重放素材是空、还是装配出来的那份历史），只能靠这个实测，不能靠猜。
	lastReadback context_runtime.CompactionIndexRequest
}

func (runtime *compactionReadbackRuntime) ReadbackCompactionSummary(
	_ context.Context,
	_ string,
	request context_runtime.CompactionIndexRequest,
) (context_runtime.CompactionIndexReceipt, error) {
	runtime.lastReadback = request
	return runtime.readback, runtime.readbackErr
}

// TestCompactionFailureWithFailedReadbackLeavesContextUntouched（现场复现）：
// 「结构上摘要器已装配」**不等于**「这次真拿到了模型读后感」。前缀重放会在运行时
// 失败（replay-failed / chunk-replay-failed / 素材不合法），压缩 DAG 于是落回本地
// 确定性压缩，回执的 summary_source=local。
//
// 用户口径（2026-10-02）：「压缩只是压缩失败的一个错误记录……agent 不需要失败的
// 压缩来覆盖之前的上下文，只有压缩成功（有 llm 读后感返回）才能让 agent 从新的
// compact 栈顶开始上下文。」
//
// 因此本用例钉住：**读数没有模型读后感时，这次压缩不得覆盖 agent 的上下文**
// （最旧的轮次必须仍在 provider 历史里）、不得落压缩记录、不得推压缩栈顶——只留
// 一条失败痕。
//
// 修复前（红）：判据只看结构探针（compactionSummaryAvailable），而真正的重放调用
// 发生在 replaceSessionHistory **之后**（推帧在锁外 B 段，替换在 A 段之前）。等
// 「这次是 local」被知道时，引擎历史已经被换成压缩窗口，最旧的轮次已经掉出 agent
// 的上下文——这正是「压缩之后看不见上文」。
func TestCompactionFailureWithFailedReadbackLeavesContextUntouched(t *testing.T) {
	// 读数回执 summary_source=local：模型读后感没有拿到（重放失败已回退本地压缩）。
	// 带 summary_note 是生产形态（compact-local:replay-failed + 真实报错）。
	recorder := &compactionIndexRecorder{}
	runtime := &compactionReadbackRuntime{
		compactionIndexRuntime: compactionIndexRuntime{
			runtimeWithContextLimits: runtimeWithContextLimits{fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192},
			recorder:                 recorder,
		},
		readback: context_runtime.CompactionIndexReceipt{
			Summary:       "## 压缩内容 (Compacted Context)\n### 目标 (Goal)\n(本地确定性压缩)",
			SummarySource: context_runtime.CompactionSummarySourceLocal,
			SummaryNote:   "compact-local:replay-failed 前缀重放两次调用均失败，已回退本地压缩：connect: connection refused",
		},
	}
	engine := &fakeEngine{}
	service := newTestService(t, engine, withTestRuntime(runtime))
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-local-fallback"}
	service.components.tasks.BeginTask("task-local-fallback", "inspect", "high", nil, TaskCheckpoint{})
	service.ViewMu.Unlock()
	appendIndexRounds(t, service, "task-local-fallback")
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

	// ① 上下文没有被这次失败的压缩覆盖：最旧的轮次仍在 provider 历史里。
	foundOldest := false
	for _, message := range engine.History() {
		if strings.Contains(message.Content, "question-0") {
			foundOldest = true
		}
	}
	if !foundOldest {
		t.Fatal("模型读后感没拿到（summary_source=local），上下文却被折了：最旧的轮次已不在 provider 历史里（agent 看不见上文）")
	}

	// ② 压缩失败只留失败痕，不留压缩记录、不动累积起点（它是一次"这次没压成"的
	// 证据，不是一次压缩）。
	service.ViewMu.RLock()
	state := service.components.tasks.CurrentTaskExecutionFor(sessionID)
	retainedFrom, compactions, failures := 0, 0, 0
	if state != nil {
		retainedFrom = state.ContextRetainedFrom
		for _, record := range state.ContextCompactions {
			compactions++
			if record.Failed {
				failures++
			}
		}
	}
	service.ViewMu.RUnlock()
	if compactions != failures || failures != 1 {
		t.Fatalf("失败压缩该留恰好一条失败痕（不是压缩记录）：共 %d 条、其中失败痕 %d 条", compactions, failures)
	}
	if retainedFrom != 0 {
		t.Fatalf("累积起点被前移（上下文被压了）：ContextRetainedFrom=%d，want 0", retainedFrom)
	}

	// ③ 压缩栈顶不动：读数说没有模型读后感，就不该有帧被推上去。
	requests, _ := recorder.snapshot()
	if len(requests) != 0 {
		t.Fatalf("没有模型读后感时不得推帧（compact stack top 不许动）：%+v", requests)
	}

	// ④ 回执把它报成「压缩失败」，而不是「已压缩」。
	if result.Compacted || result.Recorded {
		t.Fatalf("没有模型读后感时不得报成已压缩/已落记录：%+v", result)
	}
	if !strings.Contains(result.Note, "压缩失败") {
		t.Fatalf("回执口径应是「压缩失败」：%q", result.Note)
	}

	// ⑤ 失败痕：进度终局报 failed，Detail 写明是运行时读数没拿到（而不是结构上没装
	// 摘要器）——两种成因的下一步完全不同（查调用 vs 查配置）。
	frames := drainCompactionProgress(t, subscription)
	assertProgressShape(t, frames, sessionID)
	terminal := frames[len(frames)-1]
	if terminal.event.Outcome != string(context_runtime.CompactFailed) {
		t.Fatalf("进度终局 outcome = %q，want %q", terminal.event.Outcome, context_runtime.CompactFailed)
	}
	if !strings.Contains(terminal.event.Detail, "skipped=no_summary") ||
		!strings.Contains(terminal.event.Detail, "reason=no_model_readback") {
		t.Fatalf("进度终局 Detail = %q，缺少 skipped=no_summary / reason=no_model_readback", terminal.event.Detail)
	}
}

// TestCompactionWithReadbackStillCompacts：对照组——读数**拿到了**模型读后感
// （summary_source=replay）时，这次压缩必须照常发生（上下文换成有界窗口、落
// 记录、推栈）。没有这条，上面那条用例会被"永远不折"这种错误实现蒙混过关。
func TestCompactionWithReadbackStillCompacts(t *testing.T) {
	recorder := &compactionIndexRecorder{receipt: context_runtime.CompactionIndexReceipt{
		SegmentID:     "compact-readback-ok",
		Summary:       "## 压缩内容 (Compacted Context)\n### 目标 (Goal)\n读数闸放行",
		SummarySource: context_runtime.CompactionSummarySourceReplay,
	}}
	runtime := &compactionReadbackRuntime{
		compactionIndexRuntime: compactionIndexRuntime{
			runtimeWithContextLimits: runtimeWithContextLimits{fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192},
			recorder:                 recorder,
		},
		readback: context_runtime.CompactionIndexReceipt{
			Summary:       "## 压缩内容 (Compacted Context)\n### 目标 (Goal)\n读数闸放行",
			SummarySource: context_runtime.CompactionSummarySourceReplay,
		},
	}
	engine := &fakeEngine{}
	service := newTestService(t, engine, withTestRuntime(runtime))
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-readback-ok"}
	service.components.tasks.BeginTask("task-readback-ok", "inspect", "high", nil, TaskCheckpoint{})
	service.ViewMu.Unlock()
	appendIndexRounds(t, service, "task-readback-ok")
	sessionID := service.Snapshot().Session.ID

	ctx := task_context.WithSessionID(context.Background(), sessionID)
	result, err := service.CompactContextNow(ctx)
	if err != nil {
		t.Fatalf("CompactContextNow: %v", err)
	}
	if !result.Compacted || !result.Recorded {
		t.Fatalf("读数拿到模型读后感时压缩必须照常并落记录：%+v", result)
	}
	requests, _ := recorder.snapshot()
	if len(requests) != 1 {
		t.Fatalf("读数放行时推帧恰好一次，实际 %d 次", len(requests))
	}
	if requests[0].PrecomputedSummary == "" {
		t.Fatal("读数拿到的模型读后感应随推帧带下去（PrecomputedSummary），否则同一次压缩要调用两次模型")
	}
}
