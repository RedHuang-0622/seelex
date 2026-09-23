package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/model"
)

// 压缩门禁进度（compaction.progress）：一轮折叠从「判据估算」到「写压缩记录」
// 要经过若干关口，每关收口时后端投一条事件，前端进度条据此推进，终局
// （done/failed）后生命周期结束。这几条测试钉的是进度面的**事实性**：
//
//   - 真的按执行顺序逐关报告（不是先画满再补）；
//   - 每一轮**恰好一条**终局事件——少了，进度条永远停在半途；多了，前端会在
//     同一条上反复开合；
//   - 装配报错时也要收口（失败终局），否则一次失败就留下一个永久进度条；
//   - 没有折叠发生时就**不该有**进度事件（登记为下一次兑现不是"压缩进行中"）。

// compactionProgressFrame 是一条进度事件的外壳 + 载荷：会话/请求归属属于事件
// 信封（Event.SessionID / Event.RequestID），载荷里不再重复一份同事实。
type compactionProgressFrame struct {
	sessionID string
	requestID string
	event     event.CompactionProgress
}

// drainCompactionProgress 取出订阅里已排队的 compaction.progress。发布与装配
// 同线程，CompactContextNow 返回时事件必已入缓冲，因此按 default 收尾。
func drainCompactionProgress(t *testing.T, subscription event.Subscription) []compactionProgressFrame {
	t.Helper()
	var out []compactionProgressFrame
	for {
		select {
		case received, ok := <-subscription.Events:
			if !ok {
				return out
			}
			if received.Kind != event.EventCompactionProgress {
				continue
			}
			var progress event.CompactionProgress
			if err := json.Unmarshal(received.Payload, &progress); err != nil {
				t.Fatalf("compaction.progress 载荷不是 JSON: %v (%s)", err, received.Payload)
			}
			out = append(out, compactionProgressFrame{
				sessionID: received.SessionID, requestID: received.RequestID, event: progress,
			})
		default:
			return out
		}
	}
}

// gateSequence 抽出运行中门禁的 id 序列（终局事件不带 gate；起手帧不是"某一关
// 收口"，因此不计入序列——它由 assertProgressShape 单独钉形状）。
func gateSequence(frames []compactionProgressFrame) []string {
	out := make([]string, 0, len(frames))
	for _, frame := range frames {
		if frame.event.State == event.CompactionProgressRunning && frame.event.Phase != event.CompactionPhaseBegin {
			out = append(out, frame.event.Gate)
		}
	}
	return out
}

// assertProgressShape 校验所有来路都要守的公共形状：路由键齐、序号单调、总数
// 一致、状态词表合法、终局恰好一条且在末尾。显式路径还会有且只有一条起手帧
// （见 assertBeginFrame）。
func assertProgressShape(t *testing.T, frames []compactionProgressFrame, wantSession string) {
	t.Helper()
	if len(frames) == 0 {
		t.Fatal("一轮压缩没有任何门禁进度事件")
	}
	total := context_runtime.CompactionGateTotal()
	previousIndex := 0
	terminals := 0
	for position, frame := range frames {
		progress := frame.event
		if frame.sessionID != wantSession {
			t.Fatalf("第 %d 条门禁的会话路由 = %q，want %q（多会话并行时会串台）", position, frame.sessionID, wantSession)
		}
		if frame.requestID == "" {
			t.Fatalf("门禁进度事件必须带 request_id（前端按回合归属）：%+v", frame)
		}
		if progress.ElapsedMS < 0 {
			t.Fatalf("第 %d 条门禁的耗时 = %d ms，不可能为负：%+v", position, progress.ElapsedMS, progress)
		}
		switch progress.State {
		case event.CompactionProgressRunning:
			if progress.Gate == "" {
				t.Fatalf("第 %d 条 running 事件没有门禁 id：%+v", position, progress)
			}
			if progress.Index == 0 {
				// 起手帧：一关都没收口。它只能出现在第一位，且只能指判据估算。
				if position != 0 {
					t.Fatalf("起手帧出现在第 %d 条（必须是最先一条，否则进度会倒退）：%+v", position, progress)
				}
				if progress.Gate != context_runtime.CompactionGateJudge {
					t.Fatalf("起手帧的门禁 = %q，want %q", progress.Gate, context_runtime.CompactionGateJudge)
				}
				if progress.ElapsedMS != 0 {
					t.Fatalf("起手帧不该携带耗时（那时还没有任何一段完成）：%+v", progress)
				}
				break
			}
			if progress.Phase == event.CompactionPhaseBegin {
				t.Fatalf("起手帧不该带着已收口的序号：%+v", progress)
			}
			if index := context_runtime.CompactionGateIndex(progress.Gate); index != progress.Index {
				t.Fatalf("门禁 %s 的序号 = %d，权威顺序里是 %d", progress.Gate, progress.Index, index)
			}
			if progress.Index <= previousIndex {
				t.Fatalf("门禁序号没有单调递增：%+v（前一条 index=%d）", progress, previousIndex)
			}
			previousIndex = progress.Index
		case event.CompactionProgressDone, event.CompactionProgressFailed:
			terminals++
			if progress.Gate != "" {
				t.Fatalf("终局事件不应再带门禁 id：%+v", progress)
			}
			if progress.Index != progress.Total {
				t.Fatalf("终局事件必须收满：%+v", progress)
			}
		default:
			t.Fatalf("未知进度状态 %q：%+v", progress.State, progress)
		}
		if progress.Total != total {
			t.Fatalf("门禁总数 = %d，want %d（%+v）", progress.Total, total, progress)
		}
		if progress.Phase == event.CompactionPhaseBegin {
			continue // 起手帧还没有事实可说（见它的注释）
		}
		if strings.TrimSpace(progress.Detail) == "" {
			t.Fatalf("门禁 %s（state=%s）没有事实说明，进度条只剩空转：%+v", progress.Gate, progress.State, progress)
		}
	}
	if terminals != 1 {
		t.Fatalf("一轮压缩的终局事件 = %d 条，want 1（少了进度条卡在半途，多了会反复开合）", terminals)
	}
	if last := frames[len(frames)-1].event; last.State == event.CompactionProgressRunning {
		t.Fatalf("最后一条仍是运行中门禁 %+v，进度条会永远不结束", last)
	}
}

// assertBeginFrame 钉起手帧的事实性：显式压缩在动第一个重活之前就把"这一轮开始
// 了"送到界面（否则从"按下回车"到"判据关收口"这段最长的时间在界面上是空白，
// 用户看到的是"按了没反应"，然后突然冒出一条已完成的记录）。这里要确认的是：
//
//   - 有且只有一条，且在第一位（不是事后补画）；
//   - Index=0 / Gate=judge：不预支任何一格进度，只说"正在判据估算"；
//   - 版本号 0 = 未定（那一刻新版本还没定稿，不得先猜一个）；
//   - 紧随其后的判据关收口帧带上补正后的版本号。
func assertBeginFrame(t *testing.T, frames []compactionProgressFrame) {
	t.Helper()
	begins := 0
	for position, frame := range frames {
		if frame.event.Phase != event.CompactionPhaseBegin {
			continue
		}
		begins++
		if position != 0 {
			t.Fatalf("起手帧在第 %d 位，must 是第一位：%+v", position, frame.event)
		}
	}
	if begins != 1 {
		t.Fatalf("起手帧 = %d 条，want 1（显式压缩必须先告诉界面「开始压缩了」）", begins)
	}
	begin := frames[0].event
	if begin.State != event.CompactionProgressRunning || begin.Index != 0 || begin.Gate != context_runtime.CompactionGateJudge {
		t.Fatalf("起手帧 = %+v，want running/judge/index=0", begin)
	}
	if begin.Version != 0 {
		t.Fatalf("起手帧的版本 = %d，want 0（那一刻新版本还没定稿）", begin.Version)
	}
	if judge := frames[1].event; judge.Gate != context_runtime.CompactionGateJudge || judge.Version == 0 {
		t.Fatalf("判据关收口帧 = %+v，want judge 且带补正后的版本号", judge)
	}
}

// TestExplicitCompactEmitsOrderedProgressGates：/compact 显式折叠时逐关报告，
// 顺序与 context_runtime.CompactionGates 一致，并把压缩记录上的版本/来源如实
// 带在进度面上（前端据此把进度条对到记录条目）。
func TestExplicitCompactEmitsOrderedProgressGates(t *testing.T) {
	service, _, sessionID := compactTestService(t, "task-progress-explicit")
	roundOf := func(index int) string {
		return "round-" + string(rune('a'+index)) + ":" + strings.Repeat("A", 160_000)
	}
	service.ViewMu.Lock()
	for index := 0; index < 4; index++ {
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: "task-progress", Role: "user", Content: "question-" + string(rune('a'+index)),
		})
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: "task-progress", Role: "assistant", Content: roundOf(index),
		})
	}
	service.ViewMu.Unlock()

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
	if !result.Compacted {
		t.Fatalf("本轮应真折叠，结果 = %+v", result)
	}
	frames := drainCompactionProgress(t, subscription)
	assertProgressShape(t, frames, sessionID)
	assertBeginFrame(t, frames)

	got := gateSequence(frames)
	if strings.Join(got, ",") != strings.Join(context_runtime.CompactionGates, ",") {
		t.Fatalf("门禁序列 = %v，want %v", got, context_runtime.CompactionGates)
	}

	service.ViewMu.RLock()
	records := service.components.tasks.CurrentTaskExecutionFor(sessionID).ContextCompactions
	service.ViewMu.RUnlock()
	if len(records) == 0 {
		t.Fatal("压缩记录缺失，无法比对进度面")
	}
	record := records[len(records)-1]
	for _, frame := range frames {
		if frame.event.Phase == event.CompactionPhaseBegin {
			continue // 起手帧的版本号是"未定"（那时新版本还没定稿），由 assertBeginFrame 钉住
		}
		if frame.event.Version != record.Version {
			t.Fatalf("进度面版本 = %d，压缩记录版本 = %d（进度条会对到别的条目上）", frame.event.Version, record.Version)
		}
		if !model.ExplicitCompactionOrigin(frame.event.Origin) {
			t.Fatalf("/compact 的门禁进度来源 = %q，want 显式来源", frame.event.Origin)
		}
	}
	terminal := frames[len(frames)-1].event
	if terminal.State != event.CompactionProgressDone || terminal.Outcome != string(context_runtime.CompactDone) {
		t.Fatalf("终局事件 = %+v，want done/compacted", terminal)
	}
}

// TestAutoCompactionEmitsProgressGates：自动路径（软阈值）在回合执行中折叠时
// 同样逐关报告——进度条不是显式命令的专属装饰，逼近上限那一轮最需要它。
func TestAutoCompactionEmitsProgressGates(t *testing.T) {
	service, _, sessionID := compactTestService(t, "task-progress-auto")
	service.ViewMu.Lock()
	for index := 0; index < 4; index++ {
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: "task-progress-auto", Role: "user", Content: "question",
		})
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: "task-progress-auto", Role: "assistant", Content: strings.Repeat("B", 160_000),
		})
	}
	service.ViewMu.Unlock()

	subscription, err := service.SubscribeSession(sessionID, 256)
	if err != nil {
		t.Fatalf("SubscribeSession: %v", err)
	}
	defer subscription.Close()

	if _, err := service.components.context.PrepareExecutionContextFor(sessionID, "task-progress-auto", "next"); err != nil {
		t.Fatalf("PrepareExecutionContextFor: %v", err)
	}
	frames := drainCompactionProgress(t, subscription)
	assertProgressShape(t, frames, sessionID)
	if first := frames[0].event; first.Gate != context_runtime.CompactionGateJudge || first.Index != 1 {
		t.Fatalf("自动路径首帧 = %+v，want judge/index=1（判据关收口）", first)
	}
	for _, frame := range frames {
		// 自动路径**不能**有起手帧：要不要折叠正是判据估算的结果，估完才知道。
		// 提前发一帧等于先告诉用户"要压缩了"，而这一轮可能根本不压。
		if frame.event.Phase == event.CompactionPhaseBegin {
			t.Fatalf("自动路径不该有起手帧（折叠与否要估完才知道）：%+v", frame.event)
		}
		if frame.event.Origin != model.CompactionOriginAuto {
			t.Fatalf("自动路径来源 = %q，want auto", frame.event.Origin)
		}
	}
}

// TestExplicitCompactGateTimeline：逐关计时是这一轮压缩**串行工作**的唯一证据。
// 整轮通常只有几十毫秒（一次全量 token 估算 + 一次装配 + 一次落帧），界面上因此
// 不可能看到"慢慢走"的进度；能看见的是每一关自己花掉的时间。这条测试同时钉三件
// 事：起手帧先到（用户按下回车后立刻有反馈，不用等判据估算跑完）、六关按权威顺序
// 各自收口一次（一轮里混进第二个发射器就是在给同一件事发两份进度），以及每帧带的
// 毫秒数之和不超过整轮墙钟（是逐段记的实测值，不是抄来的累计值）。
//
// 判据只能用**整轮墙钟**，不能用读者看到的到达间隔：发布与装配同线程，到达时刻
// 却含投递唤醒抖动（首帧能被推迟十几毫秒），拿它当上界量的是一台机器的调度器，
// 不是门禁。因此这里用 drainCompactionProgress 在收帧线程上直接取——同线程意味着
// CompactContextNow 返回时全部帧必已在缓冲里，帧集合是确定的整轮，不含前缀截断。
func TestExplicitCompactGateTimeline(t *testing.T) {
	service, _, sessionID := compactTestService(t, "task-timeline")
	service.ViewMu.Lock()
	for index := 0; index < 4; index++ {
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: "task-timeline", Role: "user", Content: "question-" + string(rune('a'+index)),
		})
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: "task-timeline", Role: "assistant",
			Content: "round-" + string(rune('a'+index)) + ":" + strings.Repeat("A", 160_000),
		})
	}
	service.ViewMu.Unlock()

	subscription, err := service.SubscribeSession(sessionID, 256)
	if err != nil {
		t.Fatalf("SubscribeSession: %v", err)
	}
	defer subscription.Close()

	ctx := task_context.WithSessionID(context.Background(), sessionID)
	started := time.Now()
	result, err := service.CompactContextNow(ctx)
	window := time.Since(started)
	if err != nil {
		t.Fatalf("CompactContextNow: %v", err)
	}
	if !result.Compacted {
		t.Fatalf("本轮应真折叠，结果 = %+v", result)
	}

	frames := drainCompactionProgress(t, subscription)
	assertProgressShape(t, frames, sessionID)
	assertBeginFrame(t, frames)
	// 六关各自恰好收口一次：既是"进度条走的每一步都有对应的事实发生"，也让同轮
	// 混进第二个发射器时在这里炸掉（否则下面的墙钟判据会被两轮的耗时之和撑大）。
	if got := gateSequence(frames); strings.Join(got, ",") != strings.Join(context_runtime.CompactionGates, ",") {
		t.Fatalf("门禁序列 = %v，want %v", got, context_runtime.CompactionGates)
	}

	sumElapsed := 0
	for position, frame := range frames {
		sumElapsed += frame.event.ElapsedMS
		t.Logf("  %-2d %-9s index=%d/%d %-8s %4dms %s",
			position, frame.event.Gate, frame.event.Index, frame.event.Total,
			string(frame.event.State), frame.event.ElapsedMS, frame.event.Detail)
	}
	t.Logf("整轮墙钟 %v；逐关计时之和 %dms（%d 帧）",
		window.Round(time.Millisecond), sumElapsed, len(frames))
	if sumElapsed <= 0 {
		t.Fatal("整轮没有任何一关报出耗时：进度面上的耗时必须是逐段实测，不能恒为 0")
	}
	// 每一段都发生在 CompactContextNow 之内且向下取整，故之和不可能超过整轮墙钟；
	// 只有"各自从开轮算起的累计值"会超出（六关累计之和约为墙钟的 3-6 倍）。
	// 每帧最多因取整少算 1ms，故留"帧数 + 5ms"的余量。
	if budget := int(window.Milliseconds()) + len(frames) + 5; sumElapsed > budget {
		t.Fatalf("逐关计时之和 %dms 超过整轮墙钟 %v（余量 %dms）——说明不是逐段实测，而是各自从头算的累计值",
			sumElapsed, window.Round(time.Millisecond), budget)
	}
}

// TestCompactProgressTerminatesOnAssemblyError：装配失败也必须收口。结构性超限
// （system 指令自身超预算）在折叠中途返回错误——没有终局事件，进度条就永远
// 停在半途，比没有进度条更糟。
func TestCompactProgressTerminatesOnAssemblyError(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
	budget := task_context.DefaultContextBudget()
	service.promptStack.Push("base", "oversized-system", strings.Repeat("s", budget.Budget*3))
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-progress-fail"}
	service.components.tasks.BeginTask("task-progress-fail", "inspect", "high", nil, TaskCheckpoint{})
	service.ViewMu.Unlock()
	sessionID := service.components.tasks.SessionIDForRequest("task-progress-fail")

	subscription, err := service.SubscribeSession(sessionID, 256)
	if err != nil {
		t.Fatalf("SubscribeSession: %v", err)
	}
	defer subscription.Close()

	if _, err := service.components.context.PrepareExecutionContextFor(sessionID, "task-progress-fail", "continue"); err == nil {
		t.Fatal("结构性超限应报错，否则没有失败终局可断言")
	}
	frames := drainCompactionProgress(t, subscription)
	if len(frames) == 0 {
		t.Fatal("折叠中途报错却没有进度事件，前端进度条会永久卡住")
	}
	assertProgressShape(t, frames, sessionID)
	terminal := frames[len(frames)-1].event
	if terminal.State != event.CompactionProgressFailed {
		t.Fatalf("报错轮的终局 = %+v，want state=failed", terminal)
	}
	if !strings.Contains(terminal.Outcome, "budget") {
		t.Fatalf("失败终局要带上真实原因：%+v", terminal)
	}
}

// TestNoProgressEventsWithoutFold：没折叠就没有进度。「登记为下一条消息兑现」
// 不是压缩进行中——在这里发事件，进度条会对一件没发生的事走动。
func TestNoProgressEventsWithoutFold(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()
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
	if !result.Scheduled {
		t.Fatalf("无执行纪元时应登记为下一次装配兑现，结果 = %+v", result)
	}
	if frames := drainCompactionProgress(t, subscription); len(frames) != 0 {
		t.Fatalf("没有折叠却发了进度事件：%+v", frames)
	}
}

// gateLabelLine 匹配前端文案表里的一行：`judge: "判定是否需要折叠",`。
var gateLabelLine = regexp.MustCompile(`^\s*([a-z_]+):\s*"([^"]*)"\s*,?\s*$`)

// TestFrontendGateLabelsMatchBackendOrder：门禁 id 是跨语言协议字面量——后端
// CompactionGates 是权威顺序，前端 compaction-format.js 的 compactionGateLabels
// 按同一顺序给中文关卡名。后端加了一关而前端没跟，进度条会露出英文 id；后端换了
// 执行顺序而前端没换，进度条会把「帧正文落盘」说在「写压缩记录」之后。两种都是
// 画给用户看的假事实，所以在测试里钉死两份字面量，而不是等肉眼比对。
func TestFrontendGateLabelsMatchBackendOrder(t *testing.T) {
	path := filepath.Join("..", "..", "gui", "frontend", "dist", "compaction-format.js")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取前端门禁文案表失败（前端文件搬迁要同步这里）: %v", err)
	}
	text := string(source)
	start := strings.Index(text, "export const compactionGateLabels = {")
	if start < 0 {
		t.Fatal("前端没有 compactionGateLabels 文案表，进度条将没有关卡名")
	}
	block := text[start:]
	end := strings.Index(block, "\n};")
	if end < 0 {
		t.Fatal("compactionGateLabels 文案表没有正常闭合")
	}
	gates := make([]string, 0, len(context_runtime.CompactionGates))
	for _, line := range strings.Split(block[:end], "\n") {
		match := gateLabelLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		gates = append(gates, match[1])
		if strings.TrimSpace(match[2]) == "" {
			t.Fatalf("门禁 %s 的文案为空，进度条会画出一格空白", match[1])
		}
	}
	if got := strings.Join(gates, ","); got != strings.Join(context_runtime.CompactionGates, ",") {
		t.Fatalf("前端门禁文案表键序 = %v，want %v（键序就是进度条的格子序）", gates, context_runtime.CompactionGates)
	}
}
