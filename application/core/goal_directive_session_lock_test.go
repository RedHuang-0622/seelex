package core

// 红灯回归：迭代边界**不得**再入框架会话锁去写引擎历史。
//
// 现场（2026-09-22 实测，见 docs/devlog/2026-09-22-iteration-hook-session-lock-reentry.md）：
//
//	A 会话运行中收下一条输入 → 进 A 自己的消息队列（未发送）
//	→ A 这一轮收尾：回合尾治理跑 ADVISOR 回合，TL 指令留在待注入队列
//	→ 同一收尾把队列里的输入提升为下一轮（提升路径直接 runChat，
//	  不经过 startChatFor）
//	→ 提升出来的这一轮跑到工具迭代边界触发 OnIterationComplete
//	→ GoalIterationCompleted 排空指令并 appendEngineMessage
//	→ framework session.Session.AppendHistory 取「ChatStream 全程持有的那把锁」
//	→ 同一 goroutine 自锁死：回合永不收尾、会话永远停在「运行中」、
//	  排队输入再也不发出去、任务/回合关都关不掉。
//
// 这里用 Seele v0.3.1 的真实锁纪律替身（ChatStream 全程持锁；History/
// AppendHistory/ClearHistory/ReplaceHistory/SetSystemPrompt 取同一把锁）——
// 依据是 session/chat.go 的 ChatStream(进函数持锁) 与 AppendHistory(取锁)，
// 不是自证式桩。

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

// sessionLockEngine 按 Seele v0.3.1 的锁语义假装成 session.Session：
// ChatStream 进函数持锁、出函数释放；历史读写取同一把锁（不可重入）。
// 每次 ChatStream 驱动一轮「工具迭代」，并在迭代边界回调 OnIterationComplete
// —— 生产里那正是会话锁内的同步回调。
type sessionLockEngine struct {
	*fakeEngine

	mu      sync.Mutex
	history []EngineMessage

	hooks   *session.LoopHooks
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newSessionLockEngine() *sessionLockEngine {
	return &sessionLockEngine{
		fakeEngine: &fakeEngine{},
		started:    make(chan struct{}),
		release:    make(chan struct{}),
	}
}

// SessionBacked 报告"新 Session 装配"：OnIterationComplete 在会话锁内同步执行。
func (engine *sessionLockEngine) SessionBacked() bool { return true }

func (engine *sessionLockEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.once.Do(func() { close(engine.started) })
	select {
	case <-engine.release:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if engine.hooks != nil {
		// 一轮工具迭代：工具开始 → 工具完成 → 迭代边界回调（锁内）。
		info := session.ToolCallInfo{Turn: 0, Name: "bash", Arguments: "{}"}
		engine.hooks.OnToolStart(ctx, info)
		info.Result = `{"stdout":"ok","exit_code":0}`
		info.Duration = time.Millisecond
		engine.hooks.OnToolComplete(ctx, info)
		if !engine.hooks.OnIterationComplete(ctx, 0) {
			return "", nil
		}
	}
	engine.history = append(engine.history, EngineMessage{Role: "user", Content: input, ContentSet: true})
	return "answer", nil
}

func (engine *sessionLockEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error) {
	return engine.ChatStream(ctx, input, onChunk)
}

func (engine *sessionLockEngine) History() []EngineMessage {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return append([]EngineMessage(nil), engine.history...)
}

func (engine *sessionLockEngine) HistoryFor(string) []EngineMessage { return engine.History() }

func (engine *sessionLockEngine) AppendHistory(msg types.Message) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	content := ""
	if msg.Content != nil {
		content = *msg.Content
	}
	engine.history = append(engine.history, EngineMessage{Role: msg.Role, Content: content, ContentSet: true})
}

func (engine *sessionLockEngine) AppendHistoryFor(_ string, msg types.Message) {
	engine.AppendHistory(msg)
}

func (engine *sessionLockEngine) ClearHistory() {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.history = nil
}

func (engine *sessionLockEngine) ClearHistoryFor(string) { engine.ClearHistory() }

func (engine *sessionLockEngine) ReplaceHistory(sessionID string, history []EngineMessage) error {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.history = append([]EngineMessage(nil), history...)
	return nil
}

func (engine *sessionLockEngine) ReplaceHistoryFor(sessionID string, history []EngineMessage) error {
	return engine.ReplaceHistory(sessionID, history)
}

func (engine *sessionLockEngine) SetSystemPrompt(string) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
}

func (engine *sessionLockEngine) SetSystemPromptFor(string, string) { engine.SetSystemPrompt("") }

// keepGoingEvaluator 每轮都给一条「继续」裁决：回合尾治理据此把指令留在
// 待注入队列里（事故现场就是这个状态）。
type keepGoingEvaluator struct {
	mu    sync.Mutex
	calls int
}

func (e *keepGoingEvaluator) Evaluate(_ context.Context, _ goaldomain.TLSessionEmbed) (goaldomain.TLDirective, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	return goaldomain.TLDirective{Kind: goaldomain.DirectiveCheckpointOK, Content: "继续"}, nil
}

func (e *keepGoingEvaluator) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

func TestQueuedRoundMustNotReenterSessionLock(t *testing.T) {
	sessions := newLibrarySessions()
	sessions.mainHeadSeq = 1
	sessions.setRegistry(dto.TeamRegistry{
		TeamID: "goal-a2a", TeamKind: "goal-a2a", OrderPolicy: dto.OrderPolicyGoalLoop,
		Roles: []dto.RoleSpec{
			{RoleName: "user", RoleKind: dto.RoleKindUser},
			{RoleName: "main", RoleKind: dto.RoleKindMain},
			{RoleName: "tl", RoleKind: dto.RoleKindTechlead, ToolsPolicy: dto.ToolPolicyReadonly},
		},
	})
	sessions.setLifecycle(dto.OrderPolicyGoalLoop, []string{"user", "main", "tl"})
	engine := newSessionLockEngine()
	service := newTestService(t, engine, withTestSessions(sessions))
	service.ViewMu.Lock()
	service.Core.Snapshot.Session.ID = "sess-summon"
	service.ViewMu.Unlock()

	// `@` 只认团队库条目（内置形态目录已删）：先把现场存成库条目，召唤才走得通。
	if _, err := service.AgentTeamSaveCurrentTeam("sess-summon", "goal-a2a", "goal-a2a"); err != nil {
		t.Fatalf("AgentTeamSaveCurrentTeam: %v", err)
	}
	sessions.setRegistry(dto.TeamRegistry{})
	sessions.setOrder(nil)

	bridge := NewToolHookBridge()
	bridge.Bind(service)
	engine.hooks = bridge.Hooks()

	evaluator := &keepGoingEvaluator{}
	service.SetGoalTLEvaluator(evaluator)

	// 第 1 轮：@团队 起手（这一轮的回合尾会跑 ADVISOR 回合，产出 TL 指令）。
	if err := service.Submit(context.Background(), "@goal-a2a 跑一轮再看队列提升"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	select {
	case <-engine.started:
	case <-time.After(3 * time.Second):
		t.Fatalf("第一轮没有进入引擎\n%s", dumpAllGoroutines())
	}

	// 运行中再发一条：这就是"消息队列里有东西没发出去"。
	if err := service.Submit(context.Background(), "排队等提升的第二问"); err != nil {
		t.Fatalf("Submit queued: %v", err)
	}
	service.ViewMu.RLock()
	queued := len(service.sessions.Unit("sess-summon").PendingRequests())
	service.ViewMu.RUnlock()
	if queued != 1 {
		t.Fatalf("排队项 = %d, want 1（运行中提交必须进本会话队列）", queued)
	}

	// 放行第 1 轮：收尾 → ADVISOR 回合产出裁决 → 队列提升开第 2 轮。
	close(engine.release)

	idle := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		idle <- service.WaitForIdle(ctx)
	}()
	select {
	case err := <-idle:
		if err != nil {
			t.Fatalf("RED: 队列提升的下一轮没有收尾：%v\n%s", err, dumpAllGoroutines())
		}
	case <-time.After(6 * time.Second):
		t.Fatalf("RED: 队列提升的下一轮卡在会话锁上（会话永远「运行中」、排队消息发不出去、任务关不掉）\n%s",
			dumpAllGoroutines())
	}

	if evaluator.count() == 0 {
		t.Fatal("回合尾治理没有跑过 ADVISOR 回合：用例假设不成立")
	}
	snapshot := service.Snapshot()
	if snapshot.Chat.Running {
		t.Fatalf("收尾后仍在运行中：%+v", snapshot.Chat)
	}
	if snapshot.Chat.QueuedCount != 0 {
		t.Fatalf("提升后队列未排空：%+v", snapshot.Chat)
	}
	// 裁决不许被"改成安全注入"顺手吃掉：可见回放通道（transcript 行）照旧，
	// 下一次 ChatStream 起手才做受信注入（不再在锁内注入）。
	if text := conversationTexts(snapshot.Conversation); !strings.Contains(strings.Join(text, "\n"), "TL 指令") {
		t.Fatalf("裁决没有回放进可见会话：%v", text)
	}
}
