package assembly

import (
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/contract/protocol"
)

type blockingConversationRangeStore struct {
	*pagedSessionStore
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (store *blockingConversationRangeStore) LoadConversationRangeWorkspace(
	workspaceID, sessionID string,
	offset, limit int,
) ([]model.Message, int, error) {
	store.once.Do(func() { close(store.entered) })
	<-store.release
	return store.pagedSessionStore.LoadConversationRangeWorkspace(workspaceID, sessionID, offset, limit)
}

func TestLoadMoreHistoryReadsOutsideSharedWriteLock(t *testing.T) {
	defer withHistoryWindow(6)()

	store := &blockingConversationRangeStore{
		pagedSessionStore: newPagedSessionStore("slow-history", 20),
		entered:           make(chan struct{}),
		release:           make(chan struct{}),
	}
	service := newTestService(t, &fakeEngine{}, withTestSessions(store))
	if err := frontendOfTestService(service).ResumeSession("slow-history"); err != nil {
		t.Fatalf("resume slow history session: %v", err)
	}

	loadDone := make(chan error, 1)
	go func() {
		loadDone <- frontendOfTestService(service).LoadMoreHistory(0)
	}()
	select {
	case <-store.entered:
	case <-time.After(time.Second):
		t.Fatal("LoadMoreHistory 未进入预期冷读")
	}

	snapshotDone := make(chan struct{})
	go func() {
		service.Snapshot()
		close(snapshotDone)
	}()
	select {
	case <-snapshotDone:
	case <-time.After(time.Second):
		t.Fatal("慢历史 I/O 阻塞了 Snapshot 读路径，说明查询仍持有共享写锁")
	}

	close(store.release)
	select {
	case err := <-loadDone:
		if err != nil {
			t.Fatalf("load more history: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("LoadMoreHistory 在冷读解除后未收口")
	}
}
