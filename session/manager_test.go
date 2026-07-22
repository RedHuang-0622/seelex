package session

import (
	"errors"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/seelebridge"
)

// ── fakeStore ──────────────────────────────────────────────────

type fakeStore struct {
	mu   sync.Mutex
	meta []seelebridge.SessionMeta
	msgs map[string][]seelebridge.Message
	err  error // 通用错误，设为非 nil 时可模拟错误
}

func (s *fakeStore) List() []seelebridge.SessionMeta {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.meta
}

func (s *fakeStore) Delete(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	delete(s.msgs, sessionID)
	for i, m := range s.meta {
		if m.SessionID == sessionID {
			s.meta = append(s.meta[:i], s.meta[i+1:]...)
			break
		}
	}
	return nil
}

func (s *fakeStore) Load(sessionID string) ([]seelebridge.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	return s.msgs[sessionID], nil
}

func (s *fakeStore) LoadRange(sessionID string, offset, limit int) ([]seelebridge.Message, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, 0, s.err
	}
	msgs := s.msgs[sessionID]
	total := len(msgs)
	if offset >= total {
		return nil, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return msgs[offset:end], total, nil
}

func (s *fakeStore) MessageCount(sessionID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return 0, s.err
	}
	return len(s.msgs[sessionID]), nil
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		msgs: make(map[string][]seelebridge.Message),
	}
}

// ── 核心测试 ──────────────────────────────────────────────────

func TestNewManager(t *testing.T) {
	m := NewManager(newFakeStore())
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	// 默认未注入回调
	if err := m.SaveCurrent("s1"); err == nil {
		t.Error("expected error when saveFn not injected")
	}
	if err := m.Resume("s1"); err == nil {
		t.Error("expected error when loadFn not injected")
	}
}

func TestInjectSaveLoad(t *testing.T) {
	m := NewManager(newFakeStore())
	saveCalled := false
	loadCalled := false
	m.InjectSaveLoad(
		func(id string) error { saveCalled = true; return nil },
		func(id string) error { loadCalled = true; return nil },
	)

	_ = m.SaveCurrent("s1")
	if !saveCalled {
		t.Error("saveFn should have been called")
	}
	_ = m.Resume("s1")
	if !loadCalled {
		t.Error("loadFn should have been called")
	}
}

func TestSaveCurrent_WithoutInjection(t *testing.T) {
	m := NewManager(newFakeStore())
	err := m.SaveCurrent("s1")
	if err == nil || err.Error() != "session: saveFn not injected" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResume_WithoutInjection(t *testing.T) {
	m := NewManager(newFakeStore())
	err := m.Resume("s1")
	if err == nil || err.Error() != "session: loadFn not injected" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSaveCurrent_SaveFnError(t *testing.T) {
	m := NewManager(newFakeStore())
	m.InjectSaveLoad(
		func(id string) error { return errors.New("disk full") },
		nil,
	)
	err := m.SaveCurrent("s1")
	if err == nil || err.Error() != "disk full" {
		t.Fatalf("expected 'disk full', got %v", err)
	}
}

func TestResume_LoadFnError(t *testing.T) {
	m := NewManager(newFakeStore())
	m.InjectSaveLoad(
		nil,
		func(id string) error { return errors.New("not found") },
	)
	err := m.Resume("s1")
	if err == nil || err.Error() != "not found" {
		t.Fatalf("expected 'not found', got %v", err)
	}
}

func TestList(t *testing.T) {
	store := newFakeStore()
	store.meta = []seelebridge.SessionMeta{
		{SessionID: "s1"},
		{SessionID: "s2"},
	}
	m := NewManager(store)

	meta := m.List()
	if len(meta) != 2 {
		t.Fatalf("expected 2, got %d", len(meta))
	}
	if meta[0].SessionID != "s1" || meta[1].SessionID != "s2" {
		t.Errorf("unexpected order: %+v", meta)
	}
}

func TestList_Empty(t *testing.T) {
	store := newFakeStore()
	m := NewManager(store)
	meta := m.List()
	if len(meta) != 0 {
		t.Fatalf("expected 0, got %d", len(meta))
	}
}

func TestDelete(t *testing.T) {
	content := "hello"
	store := newFakeStore()
	store.msgs["s1"] = []seelebridge.Message{{Role: "user", Content: &content}}
	store.meta = []seelebridge.SessionMeta{{SessionID: "s1"}}
	m := NewManager(store)

	if err := m.Delete("s1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.msgs) != 0 {
		t.Error("messages should have been deleted")
	}
	if len(store.meta) != 0 {
		t.Error("meta should have been deleted")
	}
}

func TestDelete_StoreError(t *testing.T) {
	store := newFakeStore()
	store.err = errors.New("store error")
	m := NewManager(store)

	if err := m.Delete("s1"); err == nil {
		t.Error("expected error from store")
	}
}

func TestLoadHistory(t *testing.T) {
	content1 := "hi"
	content2 := "hello"
	store := newFakeStore()
	store.msgs["s1"] = []seelebridge.Message{
		{Role: "user", Content: &content1},
		{Role: "assistant", Content: &content2},
	}
	m := NewManager(store)

	msgs, err := m.LoadHistory("s1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2, got %d", len(msgs))
	}
	if *msgs[0].Content != "hi" {
		t.Errorf("expected 'hi', got %q", *msgs[0].Content)
	}
}

func TestLoadHistory_NotFound(t *testing.T) {
	store := newFakeStore()
	m := NewManager(store)
	msgs, err := m.LoadHistory("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0, got %d", len(msgs))
	}
}

func TestLoadHistory_StoreError(t *testing.T) {
	store := newFakeStore()
	store.err = errors.New("load error")
	m := NewManager(store)
	_, err := m.LoadHistory("s1")
	if err == nil {
		t.Error("expected error from store")
	}
}

func TestLoadHistoryRange(t *testing.T) {
	contentA := "a"
	contentB := "b"
	contentC := "c"
	store := newFakeStore()
	store.msgs["s1"] = []seelebridge.Message{
		{Role: "user", Content: &contentA},
		{Role: "assistant", Content: &contentB},
		{Role: "user", Content: &contentC},
	}
	m := NewManager(store)

	msgs, total, err := m.LoadHistoryRange("s1", 1, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 3 {
		t.Fatalf("expected total 3, got %d", total)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if *msgs[0].Content != "b" || *msgs[1].Content != "c" {
		t.Errorf("unexpected messages: %+v", msgs)
	}
}

func TestLoadHistoryRange_OutOfRange(t *testing.T) {
	contentA := "a"
	store := newFakeStore()
	store.msgs["s1"] = []seelebridge.Message{{Role: "user", Content: &contentA}}
	m := NewManager(store)

	msgs, total, err := m.LoadHistoryRange("s1", 10, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 {
		t.Fatalf("expected total 1, got %d", total)
	}
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages, got %d", len(msgs))
	}
}

func TestLoadHistoryRange_StoreError(t *testing.T) {
	store := newFakeStore()
	store.err = errors.New("range error")
	m := NewManager(store)
	_, _, err := m.LoadHistoryRange("s1", 0, 10)
	if err == nil {
		t.Error("expected error from store")
	}
}

func TestMessageCount(t *testing.T) {
	contentA := "a"
	contentB := "b"
	store := newFakeStore()
	store.msgs["s1"] = []seelebridge.Message{
		{Role: "user", Content: &contentA},
		{Role: "assistant", Content: &contentB},
	}
	m := NewManager(store)
	count, err := m.MessageCount("s1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2, got %d", count)
	}
}

func TestMessageCount_NotFound(t *testing.T) {
	store := newFakeStore()
	m := NewManager(store)
	count, err := m.MessageCount("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}
}

func TestMessageCount_StoreError(t *testing.T) {
	store := newFakeStore()
	store.err = errors.New("count error")
	m := NewManager(store)
	_, err := m.MessageCount("s1")
	if err == nil {
		t.Error("expected error from store")
	}
}

func TestConcurrentAccess(t *testing.T) {
	store := newFakeStore()
	content := "test"
	store.msgs["s1"] = []seelebridge.Message{{Role: "user", Content: &content}}
	store.meta = []seelebridge.SessionMeta{{SessionID: "s1"}}
	m := NewManager(store)
	m.InjectSaveLoad(
		func(id string) error { return nil },
		func(id string) error { return nil },
	)

	done := make(chan struct{}, 10)
	for i := 0; i < 10; i++ {
		go func() {
			_ = m.SaveCurrent("s1")
			_ = m.Resume("s1")
			_, _ = m.LoadHistory("s1")
			_, _, _ = m.LoadHistoryRange("s1", 0, 10)
			_, _ = m.MessageCount("s1")
			_ = m.Delete("s1")
			_ = m.List()
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
