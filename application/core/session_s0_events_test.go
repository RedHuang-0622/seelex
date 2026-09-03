package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/session"
)

// s0ChunkEngine 在 multiSessionEngine 上于 ChatStream 起点发射一段带会话
// 标记的流式文本（模拟后台会话运行中的可见输出），随后照常阻塞等待释放。
type s0ChunkEngine struct {
	*multiSessionEngine
}

func (engine *s0ChunkEngine) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error) {
	if onChunk != nil {
		onChunk("chunk-" + sessionID)
	}
	return engine.multiSessionEngine.ChatStreamFor(sessionID, ctx, input, onChunk)
}

// viewContains 报告指定会话可见对话中是否出现目标文本。
func viewContains(unit *session.SessionUnit, text string) bool {
	found := false
	unit.View.Read(func(view *session.View) {
		for _, message := range view.Conversation {
			if strings.Contains(message.Content, text) {
				found = true
				return
			}
		}
	})
	return found
}

// TestS0BackgroundEventsDoNotPolluteActiveSnapshot（波 1 验收锚）：
// 会话 A 为视图、B 后台并行运行——B 的流式文本只进 B 自己的可见区与事件
// 通道，A 的订阅收不到 B 的 message.delta，活跃快照不含 B 内容。
func TestS0BackgroundEventsDoNotPolluteActiveSnapshot(t *testing.T) {
	engine := &s0ChunkEngine{multiSessionEngine: newMultiSessionEngine()}
	service := newTestService(t, engine)
	ctx := context.Background()

	if err := service.Submit(ctx, "task A"); err != nil {
		t.Fatal(err)
	}
	aID := engine.SessionID()
	waitChatStarted(t, engine.started[aID])

	// A 的会话级订阅：只应收 A 或全局事件。
	subscription, err := service.SubscribeSession(aID, 256)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()

	bID := "sess-s0-b"
	engine.register(bID)
	if err := service.SubmitToSession(ctx, bID, "task B"); err != nil {
		t.Fatal(err)
	}
	waitChatStarted(t, engine.started[bID])

	// B 的流式文本进入 B 自己的可见区（后台增量只写自身 View）。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		unit := service.sessions.Unit(bID)
		if unit != nil && viewContains(unit, "chunk-"+bID) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if unit := service.sessions.Unit(bID); unit == nil || !viewContains(unit, "chunk-"+bID) {
		t.Fatal("background session B visible view lost its streamed text")
	}

	// A 的订阅上出现的 message.delta 不得携带 B 的内容。
	drainDeadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(drainDeadline) {
		select {
		case event := <-subscription.Events:
			if event.Kind != EventMessageDelta {
				continue
			}
			var delta struct {
				Delta string `json:"delta"`
			}
			if err := json.Unmarshal(event.Payload, &delta); err == nil && strings.Contains(delta.Delta, "chunk-"+bID) {
				t.Fatalf("session A subscription received background B delta: %s", delta.Delta)
			}
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}

	// 释放两会话并等待收敛：活跃快照仍只含 A 的流式内容。
	close(engine.release[aID])
	close(engine.release[bID])
	if err := service.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	snapshot := service.Snapshot()
	if strings.Contains(strings.Join(conversationTexts(snapshot.Conversation), "\n"), "chunk-"+bID) {
		t.Fatal("background session B streamed text polluted the active snapshot")
	}
	if !strings.Contains(strings.Join(conversationTexts(snapshot.Conversation), "\n"), "chunk-"+aID) {
		t.Fatal("active session A streamed text missing from the active snapshot")
	}
}

// TestS0SwitchResyncsBaseline（波 1 验收锚）：视图切到会话 B 后按显式 sid
// 重订阅，B 的提交事件携带 sid=B 且在新订阅上可达；订阅键含 sid 而非空
// 通配（G2 口径）。
func TestS0SwitchResyncsBaseline(t *testing.T) {
	engine := newMultiSessionEngine()
	service := newTestService(t, engine)
	ctx := context.Background()

	if err := service.Submit(ctx, "task A"); err != nil {
		t.Fatal(err)
	}
	aID := engine.SessionID()
	waitChatStarted(t, engine.started[aID])
	close(engine.release[aID])
	if err := service.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}

	// 切换视图到空闲会话 B（切换即重订阅由 Bridge 完成；这里验证应用层
	// 显式 sid 订阅的口径）。
	bID := "sess-s0-switch-b"
	engine.register(bID)
	if err := service.ResumeSession(bID); err != nil {
		t.Fatalf("switch to B: %v", err)
	}
	if got := service.Snapshot().Session.ID; got != bID {
		t.Fatalf("view session after switch = %q, want %q", got, bID)
	}

	subscription, err := service.SubscribeSession(bID, 128)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()

	if err := service.Submit(ctx, "task B after switch"); err != nil {
		t.Fatal(err)
	}
	waitChatStarted(t, engine.started[bID])
	close(engine.release[bID])
	if err := service.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}

	// 新订阅（键 = bID）应收到 B 的 snapshot.changed / message.added 事件，
	// 且事件携带 sid=bID；旧 A 的显式订阅早已随切换关闭，收不到 B 事件。
	var sawSnapshotB, sawMessageB bool
	drainDeadline := time.Now().Add(time.Second)
	for time.Now().Before(drainDeadline) {
		select {
		case event := <-subscription.Events:
			if event.SessionID != bID {
				t.Fatalf("event on B subscription carries sid %q, want %q", event.SessionID, bID)
			}
			if event.Kind == EventSnapshotChanged {
				sawSnapshotB = true
			}
			if event.Kind == EventMessageAdded {
				sawMessageB = true
			}
			if sawSnapshotB && sawMessageB {
				break
			}
		default:
			time.Sleep(2 * time.Millisecond)
		}
	}
	if !sawSnapshotB || !sawMessageB {
		t.Fatalf("B subscription missed baseline events: snapshot=%v message=%v", sawSnapshotB, sawMessageB)
	}
}

func conversationTexts(messages []Message) []string {
	texts := make([]string, 0, len(messages))
	for _, message := range messages {
		texts = append(texts, message.Content)
	}
	return texts
}
