package integration

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/protocol"
)

type liveCatalogSessions struct {
	fakeSessions
	mu    sync.Mutex
	infos []model.SessionInfo
}

func (sessions *liveCatalogSessions) SessionsOf(string) []model.SessionInfo {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	return append([]model.SessionInfo(nil), sessions.infos...)
}

func (sessions *liveCatalogSessions) setInfos(infos []model.SessionInfo) {
	sessions.mu.Lock()
	sessions.infos = append([]model.SessionInfo(nil), infos...)
	sessions.mu.Unlock()
}

func runtimeKnobs(service *legacyTestService) (string, int) {
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	return service.Core.Snapshot.Runtime.ReasoningEffort, service.Core.Snapshot.Runtime.LoopLimit
}

func enqueueQueuedInputs(t *testing.T, service *legacyTestService, inputs ...string) {
	t.Helper()
	for _, input := range inputs {
		if err := service.components.submission.Submit(context.Background(), input); err != nil {
			t.Fatalf("Submit(%q): %v", input, err)
		}
	}
}

func sessionIDsOf(sessions []model.SessionInfo) []string {
	ids := make([]string, 0, len(sessions))
	for _, session := range sessions {
		ids = append(ids, session.ID)
	}
	return ids
}

func containsSessionID(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func lastMessageOf(messages []model.Message) *model.Message {
	if len(messages) == 0 {
		return nil
	}
	return &messages[len(messages)-1]
}

func dumpParallelState(_ *legacyTestService, engine *multiSessionEngine, ids ...string) string {
	return engine.debugSnapshot(ids...)
}

type blockingEngine struct {
	*fakeEngine
	blockCh chan struct{}
}

func (engine *blockingEngine) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error) {
	if engine.blockCh == nil {
		engine.blockCh = make(chan struct{})
	}
	onChunk("started")
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-engine.blockCh:
		return "done", nil
	}
}

func (engine *blockingEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error) {
	return engine.ChatStream(ctx, input, onChunk)
}

func (engine *multiSessionEngine) debugSnapshot(sessionIDs ...string) string {
	engine.mu.Lock()
	defer engine.mu.Unlock()
	var builder strings.Builder
	for _, sessionID := range sessionIDs {
		_, hasSession := engine.sessions[sessionID]
		fmt.Fprintf(&builder, "engine[%s]: hasSession=%v streamCalls=%d historyLen=%d\n",
			sessionID, hasSession, engine.streamCalls[sessionID], len(engine.sessions[sessionID]))
	}
	return builder.String()
}
