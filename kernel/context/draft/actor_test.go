package draft

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type memoryDurable struct {
	mu       sync.Mutex
	states   map[string]State
	failNext bool
	commits  []CommitBatch
}

func newMemoryDurable() *memoryDurable {
	return &memoryDurable{states: make(map[string]State)}
}

func (store *memoryDurable) Load(_ context.Context, sessionID string) (State, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	state := store.states[sessionID]
	state.Items = cloneItems(state.Items)
	return state, nil
}

func (store *memoryDurable) Save(_ context.Context, sessionID string, state State) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	state.Items = cloneItems(state.Items)
	store.states[sessionID] = state
	return nil
}

func (store *memoryDurable) Commit(_ context.Context, batch CommitBatch) (Ack, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.failNext {
		store.failNext = false
		return Ack{}, errors.New("injected commit failure")
	}
	store.commits = append(store.commits, batch)
	state := store.states[batch.SessionID]
	state.Watermark = batch.Through
	state.Uncommitted = false
	if len(state.Items) > 0 && state.Items[len(state.Items)-1].Seq <= batch.Through {
		state.Items = nil
	}
	store.states[batch.SessionID] = state
	return Ack{SessionID: batch.SessionID, Through: batch.Through, Digest: batch.Digest}, nil
}

func TestActorConcurrentAppendIsOrderedAndDuplicateIsIdempotent(t *testing.T) {
	store := newMemoryDurable()
	actor, err := NewActor(context.Background(), "session-a", 8, store)
	if err != nil {
		t.Fatal(err)
	}
	defer actor.Close()

	var wg sync.WaitGroup
	for seq := uint64(1); seq <= 100; seq++ {
		seq := seq
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := actor.Append(context.Background(), Item{Seq: seq, Bytes: []byte{byte(seq)}}); err != nil {
				t.Errorf("append %d: %v", seq, err)
			}
		}()
	}
	wg.Wait()
	for seq := uint64(1); seq <= 100; seq += 7 {
		ack, err := actor.Append(context.Background(), Item{Seq: seq, Bytes: []byte{byte(seq)}})
		if err != nil || ack.Through != seq {
			t.Fatalf("duplicate seq=%d ack=%+v err=%v", seq, ack, err)
		}
	}

	state := actor.Snapshot()
	if len(state.Items) != 100 {
		t.Fatalf("items=%d want=100", len(state.Items))
	}
	for index, item := range state.Items {
		if item.Seq != uint64(index+1) {
			t.Fatalf("item[%d].Seq=%d want=%d", index, item.Seq, index+1)
		}
	}
}

func TestActorCommitFailureKeepsDraftAndAckAdvancesWatermark(t *testing.T) {
	store := newMemoryDurable()
	actor, err := NewActor(context.Background(), "session-a", 4, store)
	if err != nil {
		t.Fatal(err)
	}
	defer actor.Close()

	for seq := uint64(1); seq <= 3; seq++ {
		if _, err := actor.Append(context.Background(), Item{Seq: seq, Bytes: []byte{byte(seq)}, Final: seq == 3}); err != nil {
			t.Fatal(err)
		}
	}
	store.failNext = true
	if _, err := actor.Commit(context.Background(), 3); !errors.Is(err, ErrCommitFailed) {
		t.Fatalf("commit error=%v want ErrCommitFailed", err)
	}
	state := actor.Snapshot()
	if !state.Uncommitted || state.Watermark != 0 || len(state.Items) != 3 {
		t.Fatalf("failed commit lost draft: %+v", state)
	}

	ack, err := actor.Commit(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if ack.Through != 3 || len(actor.Snapshot().Items) != 0 || actor.Snapshot().Watermark != 3 {
		t.Fatalf("successful commit did not advance watermark: ack=%+v state=%+v", ack, actor.Snapshot())
	}

	restarted, err := NewActor(context.Background(), "session-a", 4, store)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if state := restarted.Snapshot(); state.Watermark != 3 || len(state.Items) != 0 {
		t.Fatalf("restart did not recover acknowledged watermark: %+v", state)
	}
}

func TestActorBackpressureWaitsInsteadOfDropping(t *testing.T) {
	blocker := make(chan struct{})
	store := &blockingDurable{blocker: blocker, started: make(chan struct{})}
	actor, err := NewActor(context.Background(), "session-a", 1, store)
	if err != nil {
		t.Fatal(err)
	}
	defer actor.Close()

	firstDone := make(chan error, 1)
	go func() {
		_, err := actor.Append(context.Background(), Item{Seq: 1, Bytes: []byte("one")})
		firstDone <- err
	}()
	select {
	case <-store.started:
	case <-time.After(time.Second):
		t.Fatal("writer did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := actor.Append(ctx, Item{Seq: 2, Bytes: []byte("two")}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("full queue error=%v want deadline", err)
	}
	close(blocker)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if _, err := actor.Append(context.Background(), Item{Seq: 2, Bytes: []byte("two")}); err != nil {
		t.Fatalf("queue did not recover after backpressure: %v", err)
	}
}

type blockingDurable struct {
	blocker chan struct{}
	started chan struct{}
	once    sync.Once
	state   State
}

func (store *blockingDurable) Load(context.Context, string) (State, error) { return store.state, nil }

func (store *blockingDurable) Save(context.Context, string, State) error {
	store.once.Do(func() {
		close(store.started)
	})
	<-store.blocker
	return nil
}

func (store *blockingDurable) Commit(_ context.Context, batch CommitBatch) (Ack, error) {
	return Ack{SessionID: batch.SessionID, Through: batch.Through, Digest: batch.Digest}, nil
}
