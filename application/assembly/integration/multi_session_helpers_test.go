package integration

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	assembly "github.com/RedHuang-0622/seelex/application/assembly"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

type multiSessionEngine struct {
	*fakeEngine
	mu          sync.Mutex
	active      string
	seq         int
	sessions    map[string][]contract.EngineMessage
	started     map[string]chan struct{}
	release     map[string]chan struct{}
	streamCalls map[string]int
}

var _ contract.SessionChatEngine = (*multiSessionEngine)(nil)

func newMultiSessionEngine() *multiSessionEngine {
	return &multiSessionEngine{
		fakeEngine:  &fakeEngine{},
		sessions:    map[string][]contract.EngineMessage{},
		started:     map[string]chan struct{}{},
		release:     map[string]chan struct{}{},
		streamCalls: map[string]int{},
	}
}

func (engine *multiSessionEngine) register(sessionID string) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.sessions[sessionID] = nil
	engine.started[sessionID] = make(chan struct{})
	engine.release[sessionID] = make(chan struct{})
}

func (engine *multiSessionEngine) StartSession() string {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.seq++
	sessionID := fmt.Sprintf("sess-%d", engine.seq)
	engine.sessions[sessionID] = nil
	engine.started[sessionID] = make(chan struct{})
	engine.release[sessionID] = make(chan struct{})
	engine.active = sessionID
	return sessionID
}

func (engine *multiSessionEngine) ActivateSession(sessionID string) error {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if _, ok := engine.sessions[sessionID]; !ok {
		engine.sessions[sessionID] = nil
		engine.started[sessionID] = make(chan struct{})
		engine.release[sessionID] = make(chan struct{})
	}
	engine.active = sessionID
	return nil
}

func (engine *multiSessionEngine) HasSession(sessionID string) bool {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	_, ok := engine.sessions[sessionID]
	return ok
}

func (engine *multiSessionEngine) UnloadSession(sessionID string) error {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	delete(engine.sessions, sessionID)
	delete(engine.started, sessionID)
	delete(engine.release, sessionID)
	delete(engine.streamCalls, sessionID)
	if engine.active == sessionID {
		engine.active = ""
	}
	return nil
}

func (engine *multiSessionEngine) SessionID() string {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return engine.active
}

func (engine *multiSessionEngine) History() []contract.EngineMessage {
	return engine.HistoryFor(engine.SessionID())
}

func (engine *multiSessionEngine) HistoryFor(sessionID string) []contract.EngineMessage {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	return append([]contract.EngineMessage(nil), engine.sessions[sessionID]...)
}

func (engine *multiSessionEngine) ReplaceHistory(sessionID string, history []contract.EngineMessage) error {
	return engine.ReplaceHistoryFor(sessionID, history)
}

func (engine *multiSessionEngine) ReplaceHistoryFor(sessionID string, history []contract.EngineMessage) error {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.sessions[sessionID] = append([]contract.EngineMessage(nil), history...)
	return nil
}

func (engine *multiSessionEngine) ResumeSession(sessionID string, history []contract.EngineMessage) error {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if _, ok := engine.sessions[sessionID]; !ok {
		engine.sessions[sessionID] = nil
		engine.started[sessionID] = make(chan struct{})
		engine.release[sessionID] = make(chan struct{})
		engine.streamCalls[sessionID] = 0
	}
	engine.sessions[sessionID] = append([]contract.EngineMessage(nil), history...)
	engine.active = sessionID
	return nil
}

func (engine *multiSessionEngine) AppendHistoryFor(sessionID string, message contract.EngineMessage) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.sessions[sessionID] = append(engine.sessions[sessionID], message)
}

func (engine *multiSessionEngine) ClearHistoryFor(sessionID string) {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	engine.sessions[sessionID] = nil
}

func (engine *multiSessionEngine) ClearHistory() {
	engine.ClearHistoryFor(engine.SessionID())
}

func (engine *multiSessionEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error) {
	return engine.ChatStreamFor(engine.SessionID(), ctx, input, onChunk)
}

func (engine *multiSessionEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error) {
	engine.mu.Lock()
	engine.streamCalls[sessionID]++
	engine.sessions[sessionID] = append(engine.sessions[sessionID],
		contract.EngineMessage{Role: "user", Content: input, ContentSet: true},
		contract.EngineMessage{Role: "assistant", Content: "answer", ContentSet: true},
	)
	started := engine.started[sessionID]
	release := engine.release[sessionID]
	if started == nil {
		started = make(chan struct{})
		engine.started[sessionID] = started
	}
	if release == nil {
		release = make(chan struct{})
		engine.release[sessionID] = release
	}
	select {
	case <-started:
	default:
		close(started)
	}
	engine.mu.Unlock()

	select {
	case <-release:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	return "answer", nil
}

func (engine *multiSessionEngine) SetSystemPrompt(string) {}

func (engine *multiSessionEngine) SetSystemPromptFor(string, string) {}

func waitChatStarted(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("chat did not start")
	}
}

// waitUnitIdle 等到会话真正空闲：视图状态不是运行态，且会话单元的 chat 也不再 running。
//
// 两个条件都要，因为它们是两条不同的读数：视图状态先收口，单元 chat 要等回合收尾
// （PromoteLocked → FinishLocked）才落。运行守卫（limit.Knobs.EnsureViewIdle → 单元
// ChatState().Running）读的是后者，只等前者会让守卫继续拒绝。
func waitUnitIdle(t *testing.T, service any, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var status model.SessionStatus
		chatRunning := false
		switch value := service.(type) {
		case *assembly.Service:
			value.ViewMu.RLock()
			status = assembly.ComponentsFor(value).Session.View.SessionStatusLocked(sessionID)
			if unit := assembly.ComponentsFor(value).Session.Domain.Unit(sessionID); unit != nil {
				chatRunning = unit.ChatState().Running
			}
			value.ViewMu.RUnlock()
		case *legacyTestService:
			value.ViewMu.RLock()
			status = assembly.ComponentsFor(value.Service).Session.View.SessionStatusLocked(sessionID)
			if unit := assembly.ComponentsFor(value.Service).Session.Domain.Unit(sessionID); unit != nil {
				chatRunning = unit.ChatState().Running
			}
			value.ViewMu.RUnlock()
		}
		statusText := string(status)
		if !chatRunning && statusText != "running" && statusText != "queued" && statusText != "awaiting_approval" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("session did not become idle within deadline")
}
