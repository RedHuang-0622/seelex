package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/model"
)

// 压缩失败的两条红灯复现（用户口径 2026-10-02）：
//
//	需要红灯复现的用例：压缩失败 → 偷偷把旧消息折叠掉 → 模型失去原始上下文
//	改完之后的用例旅程：压缩失败 → 留下失败记录 → 原始上下文继续存在
//	                    → 模型仍然直接看到原来的上下文，不中断继续工作
//
// 两条用例各钉旅程的一半，判别力互补：
//
//	① TestCompactionFailureKeepsWholeContextAndLeavesTrace —— 装配上限：压缩失败时
//	   装配不能再按**内部安全线**（budget）截断。旧代码把保存窗口收到 budget，于是
//	   一条**本来装得进 provider 窗口**的旧轮次被静默丢掉——模型看不见上文、界面
//	   却不留任何痕迹，这正是用户说的"偷偷把旧消息折叠掉"。同时钉"留痕"：失败必须
//	   落一条 Failed 记录（旧代码只在瞬态进度里活 6 秒）。
//	② TestCompactionFailureDoesNotInterruptOverBudgetSession —— 中断：一次装不下的
//	   单轮（现场形态：某一轮本身就大于预算）会让旧代码在压缩失败之后返回
//	   ErrProviderContextBudgetExceeded，会话被这句内部错误中断
//	   （`provider context exceeds the safe token budget: estimated=… budget=…`）。
//
// 两条都先红后绿：旧代码 ① 丢最旧轮次且不留痕、② 直接返回错误。

// compactionFailureRuntime 是"摘要器装好了、但这次真的拿不到模型读后感"的宿主：
// 读数探针回执 summary_source=local（前缀重放失败 → 落回本地确定性压缩）。
//
// 复用的是既有夹具 compactionReadbackRuntime（见 context_compact_local_fallback_repro_test.go）：
// 这里不再造第二套 Runtime 桩，探针口径只有一处。
func compactionFailureRuntime() *compactionReadbackRuntime {
	return &compactionReadbackRuntime{
		compactionIndexRuntime: compactionIndexRuntime{
			runtimeWithContextLimits: runtimeWithContextLimits{fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192},
			recorder:                 &compactionIndexRecorder{},
		},
		readback: context_runtime.CompactionIndexReceipt{
			Summary:       "## 压缩内容 (Compacted Context)\n### 目标 (Goal)\n(本地确定性压缩)",
			SummarySource: context_runtime.CompactionSummarySourceLocal,
			SummaryNote:   "fold-local:replay-failed 前缀重放两次调用均失败，已回退本地确定性压缩：connect: connection refused",
		},
	}
}

// appendSizedRounds 追加 rounds 个已定稿轮次，每轮的 assistant 正文按 asciiRunes 个
// ASCII 字符生成（估算口径 4 字符 ≈ 1 token，见 seelexctx/tokens）。正文用 ASCII 是
// 为了 token 量可算可控：用例要的是"总上下文落在哪两条线之间"，不是内容语义。
func appendSizedRounds(t *testing.T, service *Service, taskID string, rounds int, asciiRunes int) {
	t.Helper()
	service.ViewMu.Lock()
	defer service.ViewMu.Unlock()
	for index := 0; index < rounds; index++ {
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: taskID, MessageID: "message-" + strconv.Itoa(index*2+1),
			Role: "user", Content: "question-" + strconv.Itoa(index),
		})
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: taskID, MessageID: "message-" + strconv.Itoa(index*2+2),
			Role: "assistant", Content: strings.Repeat("A", asciiRunes),
		})
	}
}

// startCompactionFailureSession 起一个任务执行中的会话（/compact 的显式路径）。
func startCompactionFailureSession(t *testing.T, taskID string) (*Service, *fakeEngine, string) {
	t.Helper()
	return startCompactionFailureSessionWith(t, compactionFailureRuntime(), taskID)
}

// startCompactionFailureSessionWith 同上，但宿主由调用方给——两种失败形态（回执里
// 只有本地压缩 / 读数调用本身就报错）只有 readback 那一格不同，不该各写一套装配。
func startCompactionFailureSessionWith(t *testing.T, runtime RuntimePort, taskID string) (*Service, *fakeEngine, string) {
	t.Helper()
	engine := &fakeEngine{}
	service := newTestService(t, engine, withTestRuntime(runtime))
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: taskID}
	service.components.tasks.BeginTask(taskID, "inspect", "high", nil, TaskCheckpoint{})
	service.ViewMu.Unlock()
	return service, engine, service.Snapshot().Session.ID
}

// compactionRecordsOf 取会话当前的压缩记录（内存态真值，不用快照投影代替）。
func compactionRecordsOf(t *testing.T, service *Service) []model.ContextCompaction {
	t.Helper()
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	state := service.components.tasks.CurrentTaskExecution()
	if state == nil {
		return nil
	}
	return append([]model.ContextCompaction(nil), state.ContextCompactions...)
}

// historyHasContent 报告引擎工作历史里是否还留着某段正文。
func historyHasContent(history []EngineMessage, needle string) bool {
	for _, message := range history {
		if strings.Contains(message.Content, needle) {
			return true
		}
	}
	return false
}

// TestCompactionFailureKeepsWholeContextAndLeavesTrace（红灯复现①）
//
// 现场：一次 `/compact`（显式路径，不设阈值前提）判据命中，但读数拿不到模型读后感
// （summary_source=local）→ 这次压缩**失败**。旧代码在失败之后仍按内部安全线
// （budget = 窗口 − 输出预留 − 安全预留）装配，于是把一条**装得进 provider 窗口**的
// 最旧轮次静默丢在窗口外：模型看不见上文，界面也不留任何痕迹——"压缩失败 → 偷偷把
// 旧消息折叠掉 → 模型失去原始上下文"。
//
// 判别力在三处，缺一条都会被"什么都没做"或"照旧静默截断"蒙混过关：
//
//	① 最旧轮次仍在 provider 历史里（旧代码红：被安全线截掉）；
//	② 失败已留痕：一条 Failed 记录（旧代码红：只在瞬态进度里活 6 秒）；
//	③ 上下文照旧：累积起点没前移、没有区间、没有帧、版本没推进。
func TestCompactionFailureKeepsWholeContextAndLeavesTrace(t *testing.T) {
	const taskID = "task-compact-failure-journey"
	service, engine, sessionID := startCompactionFailureSession(t, taskID)
	// 4 个已定稿轮，每轮约 41k tokens（164,000 ASCII 字符 ≈ 41,000 tokens）：
	// 整段累积落在 budget（安全线）与 provider 窗口之间——"装得进窗口、超了安全线"。
	appendSizedRounds(t, service, taskID, 4, 164_000)

	ctx := task_context.WithSessionID(context.Background(), sessionID)
	result, err := service.CompactContextNow(ctx)
	if err != nil {
		t.Fatalf("压缩失败不得中断会话（这次返回 %v）", err)
	}
	if result.Compacted || result.Recorded {
		t.Fatalf("拿不到模型读后感时不得报成已压缩/已落记录：%+v", result)
	}
	if result.Failure == "" {
		t.Fatalf("失败必须带回原因（回执不能只说「压缩失败」）：%+v", result)
	}
	if !strings.Contains(result.Note, "压缩失败") {
		t.Fatalf("回执口径应是「压缩失败」：%q", result.Note)
	}

	// ① 原始上下文继续存在：最旧的轮次仍在 provider 历史里（装配没有按安全线把它
	// 截掉）。这是用户旅程里"模型仍然直接看到原来的上下文"的可判定形态。
	if !historyHasContent(engine.History(), "question-0") {
		t.Fatalf("压缩失败后最旧的轮次被折出了 provider 历史（模型失去原始上下文）："+
			"装配上限被收到安全线上，旧轮次被静默截掉。compared=%d assembled=%d failure=%q",
			result.ComparedTokens, result.EstimatedTokens, result.Failure)
	}

	// ② 留下失败记录：可查、带原因、且**不带区间与帧**（它不是一次压缩）。
	records := compactionRecordsOf(t, service)
	if len(records) != 1 {
		t.Fatalf("压缩失败应恰好留一条失败记录，实际 %d 条：%+v", len(records), records)
	}
	failure := records[0]
	if !failure.Failed {
		t.Fatalf("这条记录应标 Failed（失败痕不是一次压缩）：%+v", failure)
	}
	if failure.Note == "" {
		t.Fatalf("失败记录必须带原因：%+v", failure)
	}
	if failure.MessageFrom != "" || failure.MessageTo != "" || failure.EventFrom != 0 || failure.EventTo != 0 || failure.FrameRef != "" {
		t.Fatalf("失败痕不得带区间/帧（带上了就会被读成「已压出窗口」）：%+v", failure)
	}

	// ②b 失败痕进可见面：状态页读的是快照投影，只在内存态留痕等于没留。
	visible := 0
	for _, item := range service.Snapshot().Task.ContextCompactions {
		if item.Failed {
			visible++
		}
	}
	if visible != 1 {
		t.Fatalf("失败痕必须进快照可见面（状态页「上下文压缩」的唯一来路）：%d 条", visible)
	}

	// ③ 上下文照旧：累积起点没前移（没有"已压出窗口的前缀"）。
	service.ViewMu.RLock()
	retainedFrom := service.components.tasks.CurrentTaskExecution().ContextRetainedFrom
	service.ViewMu.RUnlock()
	if retainedFrom != 0 {
		t.Fatalf("累积起点被前移（上下文被压了）：ContextRetainedFrom=%d，want 0", retainedFrom)
	}

	// ③b 幂等：同一次失败（同一上下文版本 + 同一原因）不得每轮追加一条痕。再压一次
	// 必须还是那一条——否则一条长期压不下去的会话会把状态页写成流水账。
	if _, err := service.CompactContextNow(ctx); err != nil {
		t.Fatalf("重复压缩失败不得中断会话：%v", err)
	}
	if again := compactionRecordsOf(t, service); len(again) != 1 {
		t.Fatalf("同一次失败重复留痕（幂等破了）：%d 条：%+v", len(again), again)
	}
}

// TestCompactionFailureDoesNotInterruptOverBudgetSession（红灯复现②）
//
// 现场（用户报告）：压缩失败之后聊天被中断，界面出现
// `ERROR … provider context exceeds the safe token budget: estimated=281424 budget=163616`。
//
// 形态是"某一轮本身就装不下"：单条协议单元（不可拆分）大于安全线时，装配无论怎么
// 收窗口都会留着它，`estimated` 必然越线。旧代码在这里直接返回
// ErrProviderContextBudgetExceeded——而这次压不下去的原因恰恰是"压缩失败"（拿不到
// 模型读后感）。用户口径：压缩失败 → 原始上下文继续存在 → **不中断继续工作**。
//
// 因此这里钉：不可压缩的那部分（system 指令 + 工具 schema + 当轮输入）自己还装得下
// 时，best-effort 发出，而不是用内部安全线把会话锁死；结构性超限（system 指令自身
// 就超预算，见 TestPrepareExecutionContextCountsActiveSystemPrompt）仍然照旧拒绝。
func TestCompactionFailureDoesNotInterruptOverBudgetSession(t *testing.T) {
	const taskID = "task-compact-failure-budget"
	service, _, sessionID := startCompactionFailureSession(t, taskID)
	// 两个常规轮 + 一个巨轮（约 175k tokens）：巨轮单条就大于安全线。
	appendSizedRounds(t, service, taskID, 2, 164_000)
	appendSizedRounds(t, service, taskID, 1, 700_000)

	ctx := task_context.WithSessionID(context.Background(), sessionID)
	result, err := service.CompactContextNow(ctx)
	if err != nil {
		if errors.Is(err, context_runtime.ErrProviderContextBudgetExceeded) {
			t.Fatalf("压缩失败时不得用内部安全线中断会话（用户现场那句 ERROR 就是它）：%v", err)
		}
		t.Fatalf("CompactContextNow: %v", err)
	}
	if result.Failure == "" {
		t.Fatalf("这次压缩必须是失败（夹具：读数拿不到模型读后感）：%+v", result)
	}
	// 失败痕带上真实数字：读者据此判断该不该调窗口/预留，而不是只看到一句"压缩失败"。
	if !strings.Contains(result.Failure, "budget=") {
		t.Fatalf("失败原因应带数字事实（estimated/budget/window）：%q", result.Failure)
	}
	// 失败照样留痕，且会话继续可用（下一次装配不再被安全线拒绝）。
	records := compactionRecordsOf(t, service)
	if len(records) != 1 || !records[0].Failed {
		t.Fatalf("压缩失败应留一条失败记录：%+v", records)
	}
	if _, err := service.components.context.PrepareExecutionContext(taskID, "continue"); err != nil {
		t.Fatalf("压缩失败之后装配不得再被安全线拒绝（不中断继续工作）：%v", err)
	}
}

// TestCompactionFailureNoteCarriesReadbackEvidence（② 的留痕补丁，先红后绿）
//
// 现场：失败痕的 note 只写 `no_model_summary estimated=… budget=… window=… overhead=…`
// ——判据量、预算、窗口都写了，"这次为什么读不到读后感"却一个字没有。而那句话
// （读数闸这次调用的真实报错，或落回本地压缩时的降级原因＋底层报错）无法从记录的
// 任何其它字段反推：没有它，读痕的人只知道"没拿到读后感"，不知道该去查配置、查
// 重放素材，还是查那一次调用。
//
// 判别力：note 里必须出现 ` error=` 与夹具那句报错原文；只在**内存态**留痕不算——
// 右栏「上下文压缩」读的是快照投影（唯一来路）。
func TestCompactionFailureNoteCarriesReadbackEvidence(t *testing.T) {
	const taskID = "task-compact-failure-readback-evidence"
	// 夹具（compactionFailureRuntime）：读数闸回执 summary_source=local，note 带着
	// 重放失败的真实报错——生产形态就是这一句（见 seelebridge 的 frameSummaryNote）。
	const evidence = "connect: connection refused"
	service, _, sessionID := startCompactionFailureSession(t, taskID)
	appendSizedRounds(t, service, taskID, 4, 164_000)

	ctx := task_context.WithSessionID(context.Background(), sessionID)
	result, err := service.CompactContextNow(ctx)
	if err != nil {
		t.Fatalf("压缩失败不得中断会话：%v", err)
	}
	if result.Failure == "" {
		t.Fatalf("夹具前提：这次压缩应当是失败的（读数拿不到模型读后感）：%+v", result)
	}
	if !strings.Contains(result.Failure, evidence) {
		t.Fatalf("回执里的失败原因应带上读数闸的报错原文（err 也写进 note）：%q", result.Failure)
	}

	records := compactionRecordsOf(t, service)
	if len(records) != 1 || !records[0].Failed {
		t.Fatalf("压缩失败应恰好留一条失败记录：%+v", records)
	}
	if !strings.Contains(records[0].Note, " error=") || !strings.Contains(records[0].Note, evidence) {
		t.Fatalf("失败痕的 note 必须带上这次为什么读不到读后感的证据（缺了它，"+
			"读者只知道「没拿到读后感」，不知道下一步该查什么）：%q", records[0].Note)
	}

	// 可见面同源：右栏只读快照投影，内存态留痕而投影不带等于没留。
	task := service.Snapshot().Task
	if task == nil || len(task.ContextCompactions) != 1 {
		t.Fatalf("失败痕必须进快照可见面：%+v", task)
	}
	if !strings.Contains(task.ContextCompactions[0].Note, evidence) {
		t.Fatalf("快照里的失败痕没带上读数闸报错（前端因此渲染不出这一句）：%q",
			task.ContextCompactions[0].Note)
	}
}

// TestCompactionFailureNoteCarriesReadbackProbeError：读数闸**调用本身报错**（不是
// "调用成功了但只有本地压缩"）时，err 同样必须进 note。这条路径此前把 err 整个丢掉、
// 交回**零值**回执，于是失败痕里连 `source=`/`note=` 都写的是空——同一句
// `no_model_summary` 背后，差的是"重放素材为空"「调用被打断」还是「配置没开」，
// 三者下一步完全不同，却读出同一行字。
func TestCompactionFailureNoteCarriesReadbackProbeError(t *testing.T) {
	const taskID = "task-compact-failure-probe-error"
	// 生产形态：回读这一跳的调用报错（seelebridge 的 readback summary: %w 包装）。
	const probeError = "compaction index: readback summary: seelexctx: prefix replay requires history bytes"
	runtime := compactionFailureRuntime()
	runtime.readback = context_runtime.CompactionIndexReceipt{}
	runtime.readbackErr = errors.New(probeError)
	service, _, sessionID := startCompactionFailureSessionWith(t, runtime, taskID)
	appendSizedRounds(t, service, taskID, 4, 164_000)

	ctx := task_context.WithSessionID(context.Background(), sessionID)
	result, err := service.CompactContextNow(ctx)
	if err != nil {
		t.Fatalf("读数闸调用报错不得中断会话：%v", err)
	}
	if result.Failure == "" {
		t.Fatalf("读数闸调用报错 = 这次压不下去（只留痕）：%+v", result)
	}
	records := compactionRecordsOf(t, service)
	if len(records) != 1 || !records[0].Failed {
		t.Fatalf("压缩失败应恰好留一条失败记录：%+v", records)
	}
	if !strings.Contains(records[0].Note, probeError) {
		t.Fatalf("读数闸调用报错的原文必须进失败痕的 note（此前 err 被丢掉，"+
			"note 里连 source/note 都是空的）：%q", records[0].Note)
	}
}

// TestFrontendCompactionFailureErrorMarkerMatchesBackend：失败痕 note 的报错段标记是
// **跨语言协议字面量**——后端按它把报错原文接在 note 末尾（自由文本只有放末尾才不必
// 引号转义），前端 compactionFailureError 按它取末段渲染「报错：…」那一行。两处一旦
// 漂移，失败条目上只是**静默少一行**：读者再也看不到"读数闸这次报了什么"，而这正是
// 失败痕里唯一无法从其它字段反推的事实。所以在测试里钉死两份字面量。
func TestFrontendCompactionFailureErrorMarkerMatchesBackend(t *testing.T) {
	path := filepath.Join("..", "..", "gui", "frontend", "dist", "compaction-format.js")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取前端压缩记录格式化件失败（前端文件搬迁要同步这里）: %v", err)
	}
	marker := context_runtime.CompactionFailureNoteErrorMarker
	want := `const marker = "` + marker + `";`
	if !strings.Contains(string(source), want) {
		t.Fatalf("报错段标记与后端漂移：后端 %q（%s），前端 compaction-format.js 里应有 `%s`；"+
			"漂移的后果是右栏失败条目上的「报错：」静默消失。", marker,
			"CompactionFailureNoteErrorMarker", want)
	}
}
