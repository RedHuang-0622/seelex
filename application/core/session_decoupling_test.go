package core

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/chat"
	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/session"
)

// TestCoreHoldsNoSessionContainerFields（T2.4 编译断言）：core 门面不再
// 直接持有会话容器字段（*session.SessionUnit 及其切片/映射；自造
// Unit/ChatRuntime 平行容器已于 9.5 删除）。会话资源唯一所有者是 session
// 域（registry 属共享面 G），core 只保留当前会话的只读视图指针 V。
func TestCoreHoldsNoSessionContainerFields(t *testing.T) {
	assertNoSessionContainerField(t, reflect.TypeOf(Service{}))
	assertNoSessionContainerField(t, reflect.TypeOf(serviceState{}))
}

func assertNoSessionContainerField(t *testing.T, typ reflect.Type) {
	t.Helper()
	for index := 0; index < typ.NumField(); index++ {
		field := typ.Field(index)
		if isSessionContainerType(field.Type) {
			t.Fatalf("%s.%s 是会话容器字段（%v）；core 不得直接持有会话容器",
				typ.Name(), field.Name, field.Type)
		}
	}
}

func isSessionContainerType(typ reflect.Type) bool {
	switch typ {
	case reflect.TypeOf(&session.SessionUnit{}),
		reflect.TypeOf([]*session.SessionUnit{}),
		reflect.TypeOf(map[string]*session.SessionUnit{}):
		return true
	default:
		return false
	}
}

// TestEventFingerprintStable（P5 事件指纹回归）：相同输入序列驱动两次
// 独立装配，事件序列（kind + 会话/请求/消息 ID 序数）一致——9.1.2 依赖
// 方切换不得改变行为。
func TestEventFingerprintStable(t *testing.T) {
	first := runFingerprintScenario(t)
	second := runFingerprintScenario(t)
	if len(first) != len(second) {
		t.Fatalf("event fingerprint length mismatch: %d vs %d\nfirst: %v\nsecond: %v",
			len(first), len(second), first, second)
	}
	for index := range first {
		if first[index] != second[index] {
			t.Fatalf("event fingerprint diverged at %d: %q vs %q\nfirst: %v\nsecond: %v",
				index, first[index], second[index], first, second)
		}
	}
}

// runFingerprintScenario 装配一次性服务并驱动单次对话，返回归一化事件
// 指纹（时间戳/自增 ID 一律映射为序数，消除运行间噪音）。
func runFingerprintScenario(t *testing.T) []string {
	t.Helper()
	engine := &fakeEngine{chunks: []string{"hel", "lo"}}
	service := newTestService(t, engine)
	sub := service.Events.Subscribe(256)
	defer sub.Close()

	if err := service.Submit(context.Background(), "hello"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if err := service.WaitForIdle(context.Background()); err != nil {
		t.Fatalf("WaitForIdle: %v", err)
	}

	var events []event.Event
	quiet := time.After(50 * time.Millisecond)
	draining := true
	for draining {
		select {
		case ev, ok := <-sub.Events:
			if !ok {
				draining = false
				break
			}
			events = append(events, ev)
		case <-quiet:
			draining = false
		}
	}

	ids := make(map[string]string)
	next := 0
	ordinal := func(value string) string {
		if value == "" {
			return "-"
		}
		if _, ok := ids[value]; !ok {
			ids[value] = string(rune('0' + next))
			next++
		}
		return ids[value]
	}

	fingerprint := make([]string, 0, len(events))
	for _, ev := range events {
		if ev.RequestID == "" {
			// 装配期/目录刷新的全局事件与输入序列无关，不进入指纹。
			continue
		}
		key := string(ev.Kind) + "|" + ordinal(ev.SessionID) + "|" + ordinal(ev.RequestID)
		if ev.Kind == event.EventMessageAdded || ev.Kind == event.EventMessageDelta {
			key += "|" + ordinal(messageIDFromPayload(ev.Payload))
		}
		fingerprint = append(fingerprint, key)
	}
	return fingerprint
}

func messageIDFromPayload(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var probe struct {
		MessageID string `json:"message_id"`
		ID        string `json:"id"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ""
	}
	if probe.MessageID != "" {
		return probe.MessageID
	}
	return probe.ID
}

// 解耦测试（约束 C2 终态）：后台会话的流式增量不得阻塞在全局锁 Core.ViewMu 上。
// 测试持锁模拟活跃会话的独占临界区，同时驱动后台会话 appendDelta——
// 若后台路径仍取全局锁会死锁（超时失败）；重构后走 View.mu 快路径立即完成。

func TestBackgroundDeltaDoesNotBlockOnGlobalLock(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	defer service.Shutdown()

	const bgID = "sess-bg"
	service.ViewMu.Lock()
	unit := service.sessions.Unit(bgID)
	if unit == nil {
		unit, _ = session.NewSessionUnit(bgID)
		service.sessions.Register(unit)
	}
	unit.SetChatState(ChatState{Running: true, RequestID: "bg-req"}, nil)
	unit.SetStream(chat.NewVisibleOutputStream("bg-req"))
	service.appendSessionMessageLocked(bgID, "assistant", "", nil)
	service.components.tasks.BeginTaskFor(bgID, "bg-req", "bg objective", "high", nil, TaskCheckpoint{})
	service.ViewMu.Unlock()

	// 活跃会话（boot 态 session-1）持全局锁，模拟活跃独占临界区
	lockHeld := make(chan struct{})
	releaseLock := make(chan struct{})
	lockDone := make(chan struct{})
	go func() {
		defer close(lockDone)
		service.ViewMu.Lock()
		close(lockHeld)
		<-releaseLock
		service.ViewMu.Unlock()
	}()
	<-lockHeld

	deltaDone := make(chan struct{})
	go func() {
		defer close(deltaDone)
		// 后台会话流式增量：不应等待全局锁
		service.appendDelta("bg-req", "hello chunk")
	}()

	select {
	case <-deltaDone:
		// 通过：后台增量在活跃持锁时完成
	case <-time.After(2 * time.Second):
		close(releaseLock)
		t.Fatal("background delta blocked on global lock (deadlock)")
	}
	close(releaseLock)
	<-lockDone

	unit = service.sessions.Unit(bgID)
	if unit == nil {
		t.Fatal("bg unit missing")
	}
	last := unit.View.Conversation[len(unit.View.Conversation)-1]
	if last.Role != "assistant" || !containsText(last.Content, "hello chunk") {
		t.Fatalf("bg view last message = %+v, want assistant with hello chunk", last)
	}
}

func containsText(value, sub string) bool {
	for index := 0; index+len(sub) <= len(value); index++ {
		if value[index:index+len(sub)] == sub {
			return true
		}
	}
	return false
}
