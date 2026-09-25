package core

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// 这一份测试钉的是"策略零改动"：同一份 transcript，折叠发生在**回合内**（走环内
// 通道）还是发生在**锁外**（走取锁方法），压缩判据、被压区间与写回 provider 历史
// 必须逐字段一致。通道只换"历史字节从哪来/回到哪去"，不换任何阈值、窗口、帧口径。

// inLoopCompactMarker 是替身引擎辨认"这次算在回合内"的记号（真实实现里这把证据
// 由 Seele 注入本轮 ctx 的环内把手提供，端口只认把手，不靠时间戳/计数猜）。
type inLoopCompactMarker struct{}

func inLoopCompactContext() context.Context {
	return context.WithValue(context.Background(), inLoopCompactMarker{}, true)
}

// inLoopFakeEngine 在 fakeEngine 之外多实现 contract.InLoopEngine：带记号的 ctx 走
// 环内通道并计数，不带记号如实回报"不在环内"（调用方因此回落取锁方法）。
type inLoopFakeEngine struct {
	*fakeEngine
	mu            sync.Mutex
	inLoopReads   int
	inLoopWrites  int
	inLoopPrompts int
}

func (engine *inLoopFakeEngine) marked(ctx context.Context) bool {
	return ctx != nil && ctx.Value(inLoopCompactMarker{}) != nil
}

func (engine *inLoopFakeEngine) HistoryInLoop(ctx context.Context) ([]EngineMessage, bool) {
	if !engine.marked(ctx) {
		return nil, false
	}
	engine.mu.Lock()
	engine.inLoopReads++
	engine.mu.Unlock()
	return engine.fakeEngine.History(), true
}

func (engine *inLoopFakeEngine) ReplaceHistoryInLoop(ctx context.Context, history []EngineMessage) (bool, error) {
	if !engine.marked(ctx) {
		return false, nil
	}
	engine.mu.Lock()
	engine.inLoopWrites++
	engine.mu.Unlock()
	return true, engine.fakeEngine.ReplaceHistory(engine.fakeEngine.SessionID(), history)
}

func (engine *inLoopFakeEngine) SetSystemPromptInLoop(ctx context.Context, prompt string) (bool, error) {
	if !engine.marked(ctx) {
		return false, nil
	}
	engine.mu.Lock()
	engine.inLoopPrompts++
	engine.mu.Unlock()
	engine.fakeEngine.SetSystemPrompt(prompt)
	return true, nil
}

func (engine *inLoopFakeEngine) counters() (int, int, int) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return engine.inLoopReads, engine.inLoopWrites, engine.inLoopPrompts
}

// newInLoopCompactService 复刻 compactTestService 的纪元装配，但引擎换成能同时
// 回答"在环内/不在环内"的替身（两条路共用同一个引擎实例，才谈得上"同一份历史"）。
func newInLoopCompactService(t *testing.T, requestID string) (*Service, *inLoopFakeEngine, string) {
	t.Helper()
	runtime := runtimeWithContextLimits{fakeRuntime: &fakeRuntime{}, window: 200_000, output: 8_192}
	engine := &inLoopFakeEngine{fakeEngine: &fakeEngine{}}
	service := newTestService(t, engine, withTestRuntime(runtime))
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: requestID}
	service.components.tasks.BeginTask(requestID, "inspect", "high", nil, TaskCheckpoint{})
	service.ViewMu.Unlock()
	sessionID := service.Snapshot().Session.ID
	if sessionID == "" {
		sessionID = service.components.tasks.SessionIDForRequest(requestID)
	}
	return service, engine, sessionID
}

// seedLongRounds 与 TestCompactContextHandlerFoldsTranscript 同一份量：4 轮、每轮
// 16 万字符，稳过软阈值，保证两条路都真的发生折叠。
func seedLongRounds(t *testing.T, service *Service, requestID string) {
	t.Helper()
	service.ViewMu.Lock()
	defer service.ViewMu.Unlock()
	for index := 0; index < 4; index++ {
		body := "round-" + string(rune('a'+index)) + ":" + strings.Repeat("A", 160_000)
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: requestID, Role: "user", Content: "q-" + string(rune('a'+index)),
		})
		service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
			TaskID: requestID, Role: "assistant", Content: body,
		})
	}
}

func TestCompactContextInLoopAndOutOfLoopAgree(t *testing.T) {
	type foldFacts struct {
		Outcome         string
		EventFrom       uint64
		EventTo         uint64
		MessageFrom     string
		MessageTo       string
		ComparedTokens  int
		AssembledTokens int
		SoftThreshold   int
		HardThreshold   int
		Version         uint64
		Reason          string
		Origin          string
		History         []EngineMessage
	}
	collect := func(service *Service, sessionID string, ctx context.Context) foldFacts {
		outcome, err := service.components.context.CompactContextNow(ctx, sessionID)
		if err != nil {
			t.Fatalf("CompactContextNow: %v", err)
		}
		facts := foldFacts{
			Outcome: string(outcome.Outcome), ComparedTokens: outcome.ComparedTokens,
			AssembledTokens: outcome.AssembledTokens, SoftThreshold: outcome.SoftThreshold,
			HardThreshold: outcome.HardThreshold,
		}
		facts.EventFrom = outcome.Record.EventFrom
		facts.EventTo = outcome.Record.EventTo
		facts.MessageFrom = outcome.Record.MessageFrom
		facts.MessageTo = outcome.Record.MessageTo
		facts.Version = outcome.Record.Version
		facts.Reason = outcome.Record.Reason
		facts.Origin = outcome.Record.Origin
		facts.History = service.Deps.Engine.History()
		return facts
	}

	// 回合内：环内通道读/写。
	inLoopService, inLoopEngine, inLoopSession := newInLoopCompactService(t, "task-inloop-fold")
	seedLongRounds(t, inLoopService, "task-inloop-fold")
	inLoop := collect(inLoopService, inLoopSession, inLoopCompactContext())

	// 锁外：同一个引擎类型，ctx 没有环内记号 → 必须回落到取锁方法。
	outService, outEngine, outSession := newInLoopCompactService(t, "task-outloop-fold")
	seedLongRounds(t, outService, "task-outloop-fold")
	outOfLoop := collect(outService, outSession, context.Background())

	if inLoop.Outcome != "compacted" {
		t.Fatalf("环内折叠结果 = %q, want compacted（%+v）", inLoop.Outcome, inLoop)
	}
	// 判据与区间逐字段一致 = 压缩策略没被这次改动动过。
	if inLoop.ComparedTokens != outOfLoop.ComparedTokens ||
		inLoop.AssembledTokens != outOfLoop.AssembledTokens ||
		inLoop.SoftThreshold != outOfLoop.SoftThreshold ||
		inLoop.HardThreshold != outOfLoop.HardThreshold {
		t.Fatalf("判据量分叉：环内 %+v vs 锁外 %+v", inLoop, outOfLoop)
	}
	if inLoop.EventFrom != outOfLoop.EventFrom || inLoop.EventTo != outOfLoop.EventTo ||
		inLoop.MessageFrom != outOfLoop.MessageFrom || inLoop.MessageTo != outOfLoop.MessageTo {
		t.Fatalf("被压区间分叉：环内 [%d,%d]/[%s,%s] vs 锁外 [%d,%d]/[%s,%s]",
			inLoop.EventFrom, inLoop.EventTo, inLoop.MessageFrom, inLoop.MessageTo,
			outOfLoop.EventFrom, outOfLoop.EventTo, outOfLoop.MessageFrom, outOfLoop.MessageTo)
	}
	if inLoop.Reason != outOfLoop.Reason || inLoop.Origin != outOfLoop.Origin {
		t.Fatalf("压缩原因/来源分叉：环内 %s/%s vs 锁外 %s/%s",
			inLoop.Reason, inLoop.Origin, outOfLoop.Reason, outOfLoop.Origin)
	}
	if inLoop.Origin != "explicit" {
		t.Fatalf("显式压缩来源 = %q, want explicit", inLoop.Origin)
	}
	if !reflect.DeepEqual(inLoop.History, outOfLoop.History) {
		t.Fatalf("两条路写回的 provider 历史不一致：环内 %d 条 vs 锁外 %d 条",
			len(inLoop.History), len(outOfLoop.History))
	}

	// 通道确实被用/确实回落——否则上面的一致只是"两次都走了同一条路"。
	reads, writes, prompts := inLoopEngine.counters()
	if reads == 0 || writes == 0 || prompts == 0 {
		t.Fatalf("环内路径没走通道：reads=%d writes=%d prompts=%d", reads, writes, prompts)
	}
	if plainReads, plainWrites, plainPrompts := outEngine.counters(); plainReads != 0 || plainWrites != 0 || plainPrompts != 0 {
		t.Fatalf("环外路径误走通道：reads=%d writes=%d prompts=%d", plainReads, plainWrites, plainPrompts)
	}
	// 折叠产物一致性已由上面的 DeepEqual 覆盖；在飞尾部的下界由端口/引擎层的
	// TestInLoopDropsInFlightTailIsRefused（真实 Session）钉住。
}
