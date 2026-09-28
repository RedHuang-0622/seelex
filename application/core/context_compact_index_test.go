package core

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
)

// compactionIndexRecorder 记录装配层折叠推进索引面的请求（回执与失败可配）。
type compactionIndexRecorder struct {
	mu       sync.Mutex
	requests []context_runtime.CompactionIndexRequest
	sessions []string
	receipt  context_runtime.CompactionIndexReceipt
	err      error
}

func (recorder *compactionIndexRecorder) push(
	_ context.Context,
	sessionID string,
	request context_runtime.CompactionIndexRequest,
) (context_runtime.CompactionIndexReceipt, error) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.requests = append(recorder.requests, request)
	recorder.sessions = append(recorder.sessions, sessionID)
	return recorder.receipt, recorder.err
}

func (recorder *compactionIndexRecorder) snapshot() ([]context_runtime.CompactionIndexRequest, []string) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	requests := append([]context_runtime.CompactionIndexRequest(nil), recorder.requests...)
	sessions := append([]string(nil), recorder.sessions...)
	return requests, sessions
}

// compactionIndexRuntime 是实现了 context_runtime.CompactionIndexPort 的 Runtime 桩：
// 形态与生产 adapters.RuntimePort 一致——宽 RuntimePort 之外多一个**窄可选**能力。
// 未实现它的 fakeRuntime 走的是"索引面未装配"的降级路径（另一条用例钉住）。
type compactionIndexRuntime struct {
	runtimeWithContextLimits
	recorder *compactionIndexRecorder
}

func (runtime *compactionIndexRuntime) PushCompactionFrame(
	ctx context.Context,
	sessionID string,
	request context_runtime.CompactionIndexRequest,
) (context_runtime.CompactionIndexReceipt, error) {
	return runtime.recorder.push(ctx, sessionID, request)
}

// indexGateDetail 取出本轮进度里门禁 index 的事实行（找不到直接失败：一轮真折叠
// 必须经过这一关，否则进度条会少一格、回执会缺一关）。
func indexGateDetail(t *testing.T, frames []compactionProgressFrame) string {
	t.Helper()
	for _, frame := range frames {
		if frame.event.Gate == context_runtime.CompactionGateStackPush {
			return frame.event.Detail
		}
	}
	t.Fatalf("门禁进度里没有 index 关：%+v", frames)
	return ""
}

// appendIndexRounds 追加 4 个已定稿轮（每轮约 4 万 tokens），足以越过软阈值触发
// 自动折叠，并带非空的 message id——区间（EventFrom/To、MessageFrom/To）才有东西可记。
func appendIndexRounds(t *testing.T, service *Service, taskID string) {
	t.Helper()
	service.ViewMu.Lock()
	defer service.ViewMu.Unlock()
	for index := 0; index < 4; index++ {
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: taskID, MessageID: fmt.Sprintf("message-%d", index*2+1),
			Role: "user", Content: fmt.Sprintf("question-%d", index),
		})
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: taskID, MessageID: fmt.Sprintf("message-%d", index*2+2),
			Role: "assistant", Content: strings.Repeat("A", 160_000),
		})
	}
}

// TestFoldPushesCompactionFrameIntoIndex：装配层折叠必须把这次折出的区间推进会话
// 压缩栈（compaction index），并把回执里的 segment_id / 摘要来源如实带进帧正文与
// 门禁 index 关。
//
// 这是 190049e 声称"接通"、实际缺调用点的那一跳：能力面（CompactionIndexPort）、
// 落点（PushCompactionFrame）、适配器三层都在，但没有调用方，于是压缩栈对这类会话
// 恒空——记忆块初筛返回 nil、search_history 退化为尾部扫描、真空区覆盖被主动跳过。
// 用例的判别力在三处：请求真的发出去了、区间取自**记录值**而不是重算、回执真的落到
// 了帧正文与门禁上。少任何一处都可能又是一次"声称接通"。
func TestFoldPushesCompactionFrameIntoIndex(t *testing.T) {
	recorder := &compactionIndexRecorder{receipt: context_runtime.CompactionIndexReceipt{
		SegmentID:     "compact-idx-1",
		Summary:       "## 压缩内容 (Compacted Context)\n### 目标 (Goal)\n接通装配层推帧",
		SummarySource: "replay",
	}}
	runtime := &compactionIndexRuntime{
		runtimeWithContextLimits: runtimeWithContextLimits{fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192},
		recorder:                 recorder,
	}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-index-1"}
	service.components.tasks.BeginTask("task-index-1", "inspect", "high", nil, TaskCheckpoint{})
	service.ViewMu.Unlock()
	appendIndexRounds(t, service, "task-index")
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
	if !result.Compacted || !result.Recorded {
		t.Fatalf("本轮应真折叠并落记录：%+v", result)
	}

	// ① 请求真的发出去了，且归属到正在折叠的会话。
	requests, sessions := recorder.snapshot()
	if len(requests) != 1 {
		t.Fatalf("装配层折叠应推帧恰好一次，实际 %d 次：%+v", len(requests), requests)
	}
	if sessions[0] != sessionID {
		t.Fatalf("推帧会话 = %q，want %q（多会话并行时会串台）", sessions[0], sessionID)
	}
	request := requests[0]

	// ② 素材与区间都取自折叠那一刻手里的事实：溢出区是 wire 原文（非空），
	// 区间与压缩记录逐一相等（重算就会和记录漂移）。
	if len(request.Overflow) == 0 {
		t.Fatal("推帧素材为空：压缩栈会收到一帧没有原文的区间")
	}
	if result.EventFrom == 0 || result.EventTo == 0 {
		t.Fatalf("夹具应有非空事件区间（否则区间断言没有判别力）：%+v", result)
	}
	if request.EventFrom != result.EventFrom || request.EventTo != result.EventTo {
		t.Fatalf("推帧区间事件号 = %d..%d，压缩记录 = %d..%d（必须是记录值，不是重算）",
			request.EventFrom, request.EventTo, result.EventFrom, result.EventTo)
	}
	if request.MessageFrom != result.MessageFrom || request.MessageTo != result.MessageTo {
		t.Fatalf("推帧区间消息号 = %q..%q，压缩记录 = %q..%q",
			request.MessageFrom, request.MessageTo, result.MessageFrom, result.MessageTo)
	}
	if request.RequestID != "task-index-1" {
		t.Fatalf("推帧请求归属 = %q，want 触发折叠的回合标识", request.RequestID)
	}

	// ③ 回执落到了帧正文：segment_id 与摘要来源都在元数据块里，读后感原样嵌入，
	// 且 readback 段不再说"没有细筛入口"。
	page, err := service.ToolResultContent(ctx, result.FrameRef, 0, 0)
	if err != nil {
		t.Fatalf("按 ref 读帧正文: %v", err)
	}
	body := page.Content
	for _, want := range []string{
		`"segment_id": "compact-idx-1"`,
		`"summary_source": "replay"`,
		"接通装配层推帧",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("帧正文缺少 %q：\n%s", want, body)
		}
	}
	if strings.Contains(body, "索引面未启用") || strings.Contains(body, "推帧失败") {
		t.Fatalf("推帧成功却仍报没有细筛入口：\n%s", body)
	}

	// ④ 门禁 index 关如实报出标识与来源（进度条与回执读同一份事实）。
	frames := drainCompactionProgress(t, subscription)
	assertProgressShape(t, frames, sessionID)
	detail := indexGateDetail(t, frames)
	for _, want := range []string{"segment=compact-idx-1", "source=replay"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("门禁 index 关 Detail = %q，缺少 %q", detail, want)
		}
	}
}

// TestFoldWithoutIndexFaceReportsDegradedGate：索引面未装配（Runtime 不实现
// CompactionIndexPort——测试夹具与只读宿主就是这种形态）时，折叠照常成立，但
// **两种降级都要如实留痕**：门禁 index 关报 unavailable，帧正文的 readback 段
// 说明"索引面未启用"而不是留下一个空白段。
//
// 判别力：把这一步写成"没有 Port 就静默跳过"，进度条会少一格（门禁序列
// 与 CompactionGates 不再相等），帧正文也会变成一句没有来由的"没有入口"。
func TestFoldWithoutIndexFaceReportsDegradedGate(t *testing.T) {
	service, _, sessionID := compactTestService(t, "task-index-degraded")
	appendIndexRounds(t, service, "task-index-degraded")

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
	if !result.Compacted || !result.Recorded {
		t.Fatalf("索引面缺失不得影响折叠：%+v", result)
	}
	frames := drainCompactionProgress(t, subscription)
	assertProgressShape(t, frames, sessionID)
	if detail := indexGateDetail(t, frames); detail != "index=unavailable" {
		t.Fatalf("未装配索引面时门禁 index 关 = %q，want index=unavailable", detail)
	}

	page, err := service.ToolResultContent(ctx, result.FrameRef, 0, 0)
	if err != nil {
		t.Fatalf("按 ref 读帧正文: %v", err)
	}
	if !strings.Contains(page.Content, "索引面未启用") {
		t.Fatalf("帧正文应如实说明没有细筛入口的原因：\n%s", page.Content)
	}
	if strings.Contains(page.Content, "推帧失败") {
		t.Fatalf("未尝试与试了失败是两种事实，不得混为一谈：\n%s", page.Content)
	}
}

// TestFoldPushFailureIsReportedNotFatal：索引面在、推帧报错时，折叠与本次请求
// 照常（索引缺失是降级不是错误），但帧正文必须带真实原因、门禁必须报 error——
// "没报错就是推上了"与"看起来有原文、其实没有入口"同样是假事实。
func TestFoldPushFailureIsReportedNotFatal(t *testing.T) {
	recorder := &compactionIndexRecorder{err: context_runtime.ErrCompactionIndexUnavailable}
	runtime := &compactionIndexRuntime{
		runtimeWithContextLimits: runtimeWithContextLimits{fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192},
		recorder:                 recorder,
	}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-index-fail"}
	service.components.tasks.BeginTask("task-index-fail", "inspect", "high", nil, TaskCheckpoint{})
	service.ViewMu.Unlock()
	appendIndexRounds(t, service, "task-index-fail")
	sessionID := service.Snapshot().Session.ID

	subscription, err := service.SubscribeSession(sessionID, 256)
	if err != nil {
		t.Fatalf("SubscribeSession: %v", err)
	}
	defer subscription.Close()

	ctx := task_context.WithSessionID(context.Background(), sessionID)
	result, err := service.CompactContextNow(ctx)
	if err != nil {
		t.Fatalf("推帧失败不得中断折叠与请求：%v", err)
	}
	if !result.Compacted || !result.Recorded {
		t.Fatalf("推帧失败时折叠仍应成立并落记录：%+v", result)
	}
	frames := drainCompactionProgress(t, subscription)
	assertProgressShape(t, frames, sessionID)
	if detail := indexGateDetail(t, frames); !strings.Contains(detail, "index=error") {
		t.Fatalf("推帧失败应报 error 终局事实：%q", detail)
	}

	page, err := service.ToolResultContent(ctx, result.FrameRef, 0, 0)
	if err != nil {
		t.Fatalf("按 ref 读帧正文: %v", err)
	}
	if !strings.Contains(page.Content, "推帧失败") ||
		!strings.Contains(page.Content, context_runtime.ErrCompactionIndexUnavailable.Error()) {
		t.Fatalf("帧正文应写出推帧失败的真实原因：\n%s", page.Content)
	}
}

// TestFoldWithoutOverflowReportsSkippedNotUnavailable：索引面在，但这次折叠**没有
// 折出任何完整协议单元**（尚未越过任何保留窗口就显式 /compact：区间为空、没有原文
// 可归档）时，门禁与帧正文必须报"这次无事可做"，而不是"索引面未启用"——后者会让
// 读帧的人去查一个并不存在的配置事故，而"没尝试 / 试了失败 / 无区间可推"本就是要
// 分开记账的三种事实。
func TestFoldWithoutOverflowReportsSkippedNotUnavailable(t *testing.T) {
	recorder := &compactionIndexRecorder{}
	runtime := &compactionIndexRuntime{
		runtimeWithContextLimits: runtimeWithContextLimits{fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192},
		recorder:                 recorder,
	}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-index-skip"}
	service.components.tasks.BeginTask("task-index-skip", "inspect", "high", nil, TaskCheckpoint{})
	// 只有两个短事件：整个 transcript 都装得进保留窗口，因此显式压缩折不出任何
	// 区间（溢出为空）——这正是"索引面就绪但无事可做"的形态。
	for _, event := range []TranscriptEvent{
		{TaskID: "task-index-skip", MessageID: "message-1", Role: "user", Content: "hello"},
		{TaskID: "task-index-skip", MessageID: "message-2", Role: "assistant", Content: "hi"},
	} {
		service.components.tasks.AppendTranscriptEventLocked(event)
	}
	service.ViewMu.Unlock()
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
	if !result.Compacted || !result.Recorded {
		t.Fatalf("显式压缩即使无区间可折也应照常折叠并落记录：%+v", result)
	}

	// ① 溢出为空时不拿一个空区间去打扰索引面（推一帧没有原文可归档的区间，只会让
	// 检索命中一个读不回来的段）；但**这一步仍然被记为一关**：门禁与帧正文要如实
	// 说出"索引面就绪、这次没有可推的原文"，而不是沉默跳过。
	requests, _ := recorder.snapshot()
	if len(requests) != 0 {
		t.Fatalf("溢出为空不该问索引面（空区间推上去只会让检索命中读不回来的段）：%+v", requests)
	}

	// ② 门禁如实报 skipped（不是 unavailable）。
	frames := drainCompactionProgress(t, subscription)
	assertProgressShape(t, frames, sessionID)
	if detail := indexGateDetail(t, frames); detail != "index=skipped reason=no_overflow" {
		t.Fatalf("无区间可推时门禁 index 关 = %q，want index=skipped reason=no_overflow", detail)
	}

	// ③ 帧正文说"没有折出任何完整协议单元"，不得说成"索引面未启用"。
	page, err := service.ToolResultContent(ctx, result.FrameRef, 0, 0)
	if err != nil {
		t.Fatalf("按 ref 读帧正文: %v", err)
	}
	if !strings.Contains(page.Content, "没有折出任何完整协议单元") {
		t.Fatalf("帧正文应说清这次没有区间可推：\n%s", page.Content)
	}
	if strings.Contains(page.Content, "索引面未启用") {
		t.Fatalf("索引面已就绪却说成未启用（两种降级混为一谈）：\n%s", page.Content)
	}
}
