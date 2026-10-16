package switching

import (
	"errors"
	"testing"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// fakeMetaStore 是 MetaStore 的最小结构满足实现。
type fakeMetaStore struct {
	meta    map[string]model.SessionMeta
	setErr  error
	readErr error
}

func (store *fakeMetaStore) SetSessionMeta(sessionID string, meta model.SessionMeta) error {
	if store.setErr != nil {
		return store.setErr
	}
	if store.meta == nil {
		store.meta = map[string]model.SessionMeta{}
	}
	store.meta[sessionID] = meta
	return nil
}

func (store *fakeMetaStore) SessionMeta(sessionID string) (model.SessionMeta, error) {
	if store.readErr != nil {
		return model.SessionMeta{}, store.readErr
	}
	return store.meta[sessionID], nil
}

// TestSetMetaValidatesAndWakesOnlyOnSuccess：空会话 ID 与未装配存储都不写盘、不唤醒；
// 成功写入恰好唤醒一次目录刷新。
func TestSetMetaValidatesAndWakesOnlyOnSuccess(t *testing.T) {
	wakes := 0
	deps := MetaDeps{Wake: func() { wakes++ }}

	if err := SetMeta(deps, "  ", model.SessionMeta{Pinned: true}); err == nil ||
		err.Error() != "session ID is required" {
		t.Fatalf("空会话 ID 文案必须保留，got %v", err)
	}
	if err := SetMeta(deps, "s1", model.SessionMeta{}); !errors.Is(err, ErrMetaUnsupported) {
		t.Fatalf("未装配存储应报 ErrMetaUnsupported，got %v", err)
	}
	if wakes != 0 {
		t.Fatalf("失败路径不得唤醒目录刷新，got %d", wakes)
	}

	store := &fakeMetaStore{}
	deps.Store = store
	if err := SetMeta(deps, " s1 ", model.SessionMeta{Pinned: true, Alias: "线上排查", SortOrder: 3}); err != nil {
		t.Fatal(err)
	}
	if wakes != 1 {
		t.Fatalf("成功写入应唤醒一次，got %d", wakes)
	}
	if got := store.meta["s1"]; !got.Pinned || got.Alias != "线上排查" || got.SortOrder != 3 {
		t.Fatalf("元数据应原样落盘（会话 ID 先 trim）：%+v", got)
	}
}

// TestSetMetaPropagatesStoreFailure：写盘失败要冒泡且**不**唤醒（否则会推一轮没变化的目录）。
func TestSetMetaPropagatesStoreFailure(t *testing.T) {
	wakes := 0
	store := &fakeMetaStore{setErr: errors.New("disk full")}
	err := SetMeta(MetaDeps{Store: store, Wake: func() { wakes++ }}, "s1", model.SessionMeta{})
	if err == nil || err.Error() != "disk full" {
		t.Fatalf("写盘失败应原样冒泡，got %v", err)
	}
	if wakes != 0 {
		t.Fatalf("写盘失败不得唤醒，got %d", wakes)
	}
}

// TestReadMeta：零值清除语义由存储侧表达（这里只钉转发与两种失败路径）。
func TestReadMeta(t *testing.T) {
	store := &fakeMetaStore{meta: map[string]model.SessionMeta{"s1": {Alias: "a"}}}
	deps := MetaDeps{Store: store}

	if meta, err := ReadMeta(deps, "s1"); err != nil || meta.Alias != "a" {
		t.Fatalf("读回应转发给存储，got %+v / %v", meta, err)
	}
	if _, err := ReadMeta(deps, " "); err == nil || err.Error() != "session ID is required" {
		t.Fatalf("空会话 ID 文案必须保留，got %v", err)
	}
	if _, err := ReadMeta(MetaDeps{}, "s1"); !errors.Is(err, ErrMetaUnsupported) {
		t.Fatalf("未装配存储应报 ErrMetaUnsupported，got %v", err)
	}
	store.readErr = errors.New("unreadable")
	if _, err := ReadMeta(deps, "s1"); err == nil || err.Error() != "unreadable" {
		t.Fatalf("读失败应原样冒泡，got %v", err)
	}
}
