package jobs

import (
	"context"
	"sync"
	"testing"

	frameworkevent "github.com/RedHuang-0622/Seele/event"
	seelejobs "github.com/RedHuang-0622/Seele/jobs"
)

// TestNewEventStreamWithoutManagerIsNil 钉住缺装配的语义：没有作业表就没有事件流，
// 且 Start/Close 对 nil 接收者安全（装配顺序上 SetTeamworkBackend 可能不跑）。
func TestNewEventStreamWithoutManagerIsNil(t *testing.T) {
	stream := NewEventStream(nil, nil, nil)
	if stream != nil {
		t.Fatalf("没有作业表时构造出非 nil 流：%#v", stream)
	}
	stream.Start()
	stream.Close()
}

// TestEventStreamProjectsOnlyStateChanges 钉住"投影是状态变化、不是逐帧回放"：
// 同一状态重复出现只落一条；状态变了才再落一条；**没有会话归属的作业一条都不落**
// （宁可丢弃，也不回填到别的会话）。
func TestEventStreamProjectsOnlyStateChanges(t *testing.T) {
	var mu sync.Mutex
	var appends []frameworkevent.Event
	persist := func(_ context.Context, event frameworkevent.Event) error {
		mu.Lock()
		appends = append(appends, event)
		mu.Unlock()
		return nil
	}
	records := []seelejobs.Record{
		{Handle: "a1", State: seelejobs.StateRunning, Scope: seelejobs.Scope{Session: "sess-1"}},
		{Handle: "a2", State: seelejobs.StateRunning, Scope: seelejobs.Scope{}}, // 无会话归属
	}
	reader := &stubReader{records: records}
	stream := NewEventStream(reader, func() func(context.Context, frameworkevent.Event) error {
		return persist
	}, nil)
	stream.drain()

	mu.Lock()
	first := len(appends)
	mu.Unlock()
	if first != 1 {
		t.Fatalf("首轮投影落 %d 条, want 1（无会话归属的作业不落库）", first)
	}

	// 同一状态再来一轮：一条都不新增（投影是状态变化，不是逐帧回放）。
	stream.drain()
	mu.Lock()
	second := len(appends)
	mu.Unlock()
	if second != first {
		t.Fatalf("同状态重复投影多落 %d 条", second-first)
	}

	// 状态变化 → 再落一条；已销项（快照里消失）的句柄从记忆里移除：重新出现该句柄时
	// 视作新作业（句柄序号单调、永不复用，所以这不会把"旧作业的新状态"漏掉）。
	reader.set([]seelejobs.Record{
		{Handle: "a1", State: seelejobs.StateDone, Scope: seelejobs.Scope{Session: "sess-2"}},
	})
	stream.drain()
	mu.Lock()
	defer mu.Unlock()
	if len(appends) != 2 {
		t.Fatalf("状态变化后共 %d 条, want 2", len(appends))
	}
	last := appends[1]
	if last.Status != frameworkevent.StatusCompleted {
		t.Fatalf("终态映射 = %q, want completed", last.Status)
	}
	if last.Source != EventSource || last.Type != frameworkevent.TypeLifecycle {
		t.Fatalf("source/type = %q/%q", last.Source, last.Type)
	}
	if got := sessionIDOf(last); got != "sess-2" {
		t.Fatalf("会话归属 = %q, want sess-2", got)
	}
	if last.Sequence == 0 {
		t.Fatalf("序号取时间基，不该是 0：%#v", last)
	}
}

// TestEventStatusMapping 钉住四种作业状态到框架事件状态的映射（含默认支路）。
func TestEventStatusMapping(t *testing.T) {
	cases := map[seelejobs.State]frameworkevent.Status{
		seelejobs.StateDone:    frameworkevent.StatusCompleted,
		seelejobs.StateFailed:  frameworkevent.StatusFailed,
		seelejobs.StateKilled:  frameworkevent.StatusCanceled,
		seelejobs.StateRunning: frameworkevent.StatusRunning,
		seelejobs.State("?"):   frameworkevent.StatusRunning,
	}
	for state, want := range cases {
		if got := eventStatus(state); got != want {
			t.Fatalf("eventStatus(%q) = %q, want %q", state, got, want)
		}
	}
}

// TestEventStreamStartCloseIsIdempotent 钉住生命周期的幂等与可终止：
// 起停各两次不 panic、Close 在 Start 之前调用也能返回。
func TestEventStreamStartCloseIsIdempotent(t *testing.T) {
	reader := &stubReader{}
	stream := NewEventStream(reader, nil, nil)
	stream.Start()
	stream.Close()
	stream.Close() // 第二次 Close 不再等待已关闭的 done 之外的任何东西

	idle := NewEventStream(reader, nil, nil)
	idle.Close() // 没起过协程也能关（loop 未跑时不阻塞在 done 上）
}

// stubReader 是 SnapshotReader 的最小替身（端口窄到只够投影用，所以替身也只有一格）。
type stubReader struct {
	mu      sync.Mutex
	records []seelejobs.Record
}

func (reader *stubReader) set(records []seelejobs.Record) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	reader.records = records
}

func (reader *stubReader) Snapshot(seelejobs.Scope) []seelejobs.Record {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	return append([]seelejobs.Record(nil), reader.records...)
}

// sessionIDOf 从事件的 agent.runtime 定位读会话归属（测试内的局部读法；生产侧的判据
// 只有一份，在 store.SessionIDFromEvent——这里刻意不引它，避免用例与实现共用同一段代码）。
func sessionIDOf(event frameworkevent.Event) string {
	for _, location := range event.Locations {
		if location.Kind == "agent.runtime" {
			return location.IDs["session_id"]
		}
	}
	return ""
}
