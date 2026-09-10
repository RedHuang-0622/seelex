package session

import (
	"context"
	"testing"
	"time"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
)

// observeTimeout 是观测面读取的容许上界。真实修复前这些读取会卡在会话锁上
// 直到本轮流式结束（实测 28s，调用方 10s 超时后拿到空值），所以 2s 足以区分
// "立即返回"与"等执行面"。
const observeTimeout = 2 * time.Second

// awaitObservation 在独立 goroutine 里执行一次观测面读取并把超时判失败。
func awaitObservation[T any](t *testing.T, label string, call func() T) T {
	t.Helper()
	result := make(chan T, 1)
	go func() { result <- call() }()
	select {
	case value := <-result:
		return value
	case <-time.After(observeTimeout):
		t.Fatalf("%s 被运行中节点的会话锁阻塞（观测面等执行面）", label)
		var zero T
		return zero
	}
}

// TestSubagentSessionsReadsDoNotBlockOnStreamingNode 是 2026-09-10 长流热点的
// 观测面回归。注册表 actor 是单 goroutine：一个节点的长流若让 actor 卡在会话
// 锁上，整个注册表（其它节点详情、阶段日志、落账）都会排队，mailbox 满后还会
// 丢阶段事件（subagentSessionCmdCap=256）。用例让一个节点真的卡在流式里，断言：
//  1. 该节点自己的 Conversation/ContextSnapshot 立即返回，不是 10s 超时；
//  2. 另一个空闲节点的读取不被它拖住；
//  3. 返回的是循环发布的检查点内容（本轮 user 消息），而不是空。
func TestSubagentSessionsReadsDoNotBlockOnStreamingNode(t *testing.T) {
	completer := newBlockingChatCompleter()
	streaming, err := frameworkSession.NewSession(frameworkSession.SessionComponents{
		Agent: blockingAgent{llm: completer},
	})
	if err != nil {
		t.Fatalf("new streaming session: %v", err)
	}
	idle := frameworkSession.New(nil)

	registry := NewSubagentSessions(nil)
	defer registry.Close()
	registry.Register("streaming", streaming, "long stream")
	registry.Register("idle", idle, "idle")

	ctx, cancel := context.WithCancel(context.Background())
	streamDone := make(chan struct{})
	go func() {
		defer close(streamDone)
		// ChatStream 整段持会话锁；completer 阻塞即锁被持有。
		_, _ = streaming.ChatStream(ctx, "first", func(string) {})
	}()
	defer func() {
		cancel()
		select {
		case <-streamDone:
		case <-time.After(5 * time.Second):
		}
	}()

	select {
	case <-completer.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("blocking completer never entered: live stream did not start")
	}

	type conversationResult struct {
		messages []types.Message
		ok       bool
	}
	// 1+3. 运行中节点：立即返回 + 带检查点内容。
	live := awaitObservation(t, "Conversation(streaming-node)", func() conversationResult {
		messages, ok := registry.Conversation("streaming")
		return conversationResult{messages: messages, ok: ok}
	})
	if !live.ok {
		t.Fatal("Conversation(streaming-node) reported no conversation while the node runs")
	}
	if len(live.messages) != 1 || live.messages[0].Role != "user" {
		t.Fatalf("Conversation(streaming-node) = %d messages, want the published user turn", len(live.messages))
	}

	// 1. 运行中节点的上下文快照：同样不能等会话锁。
	contextSnapshot := awaitObservation(t, "ContextSnapshot(streaming-node)", func() bool {
		snapshot, ok := registry.ContextSnapshot("streaming")
		return ok && snapshot != nil && snapshot.MessageCount == 1
	})
	if !contextSnapshot {
		t.Fatal("ContextSnapshot(streaming-node) must carry the published message count")
	}

	// 2. 另一个节点不能被长流拖住（actor 单 goroutine，一处阻塞全表排队）。
	other := awaitObservation(t, "Conversation(idle-node)", func() conversationResult {
		messages, ok := registry.Conversation("idle")
		return conversationResult{messages: messages, ok: ok}
	})
	if !other.ok {
		t.Fatal("Conversation(idle-node) must stay readable while another node streams")
	}
}
