package core

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract"
)

// stagedChatEngine 分两段流式输出：第一段在提交后立即到达，第二段在
// stage2 释放后到达——用于模拟“切走视图后，后台会话仍在产生新正文”。
type stagedChatEngine struct {
	*fakeEngine
	mu      sync.Mutex
	started chan struct{}
	stage2  chan struct{}
	once    sync.Once
}

func newStagedChatEngine() *stagedChatEngine {
	return &stagedChatEngine{
		fakeEngine: &fakeEngine{},
		started:    make(chan struct{}),
		stage2:     make(chan struct{}),
	}
}

func (engine *stagedChatEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error) {
	engine.once.Do(func() { close(engine.started) })
	select {
	case <-engine.stage2:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if onChunk != nil {
		onChunk("after-switch-live-part")
	}
	return "done", nil
}

var _ contract.SessionChatEngine = (*stagedChatEngine)(nil)

// TestSwitchBackShowsLatestBackgroundContent 复现“切回会话时前端拿到的内容
// 是否最新”：会话 A 在跑（第二段正文被门闩卡住），切到 B，释放 A 的第二段
// 正文（此时 A 是后台），再切回 A——断言 Snapshot 的会话记录包含后台期间新
// 产生的正文。若此测试红，说明 core 侧存在内容滞后；绿则剩余嫌疑在前端
// 事件/渲染层。
func TestSwitchBackShowsLatestBackgroundContent(t *testing.T) {
	engine := newStagedChatEngine()
	service := newTestService(t, engine)
	defer service.Shutdown()

	if err := service.Submit(context.Background(), "start a"); err != nil {
		t.Fatal(err)
	}
	aID := engine.SessionID()
	select {
	case <-engine.started:
	case <-time.After(3 * time.Second):
		t.Fatal("chat a never started")
	}

	engine.fakeEngine.mu.Lock()
	engine.fakeEngine.loadedSessions["sess-b"] = true
	engine.fakeEngine.mu.Unlock()
	if err := service.ResumeSession("sess-b"); err != nil {
		t.Fatalf("switch to b: %v", err)
	}

	close(engine.stage2) // A 后台产生第二段正文
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := service.ResumeSession(aID); err != nil {
		t.Fatalf("switch back to a: %v", err)
	}
	found := false
	for _, message := range service.Snapshot().Conversation {
		if message.Content == "after-switch-live-part" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("切回后 Snapshot 未包含后台期间产生的最新正文（core 侧内容滞后）")
	}
}
