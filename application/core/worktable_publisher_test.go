package core

import (
	"sync"
	"testing"
	"time"
)

// ── worktable.changed CSP 汇聚发布器（白盒）──────────────────

func TestWorkTablePublisherCoalescesBurstToLatest(t *testing.T) {
	var mu sync.Mutex
	var published []worktableUpdate
	publisher := newWorkTablePublisher(func(update worktableUpdate) {
		mu.Lock()
		published = append(published, update)
		mu.Unlock()
	})
	defer publisher.Close()

	const burst = 8
	for revision := uint64(1); revision <= burst; revision++ {
		publisher.Send(worktableUpdate{revision: revision, items: []WorkItem{{ID: "todo:1", Status: "doing"}}})
	}

	deadline := time.Now().Add(2 * time.Second)
	last := uint64(0)
	for time.Now().Before(deadline) {
		mu.Lock()
		if len(published) > 0 {
			last = published[len(published)-1].revision
		}
		mu.Unlock()
		if last == burst {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if last != burst {
		t.Fatalf("latest published revision = %d, want %d (published=%+v)", last, burst, published)
	}
	if len(published) > burst {
		t.Fatalf("publisher must not amplify updates: %d publishes for %d sends", len(published), burst)
	}
}

func TestWorkTablePublisherCloseDrainsTail(t *testing.T) {
	var mu sync.Mutex
	var published []worktableUpdate
	publisher := newWorkTablePublisher(func(update worktableUpdate) {
		mu.Lock()
		published = append(published, update)
		mu.Unlock()
	})

	publisher.Send(worktableUpdate{revision: 1, items: []WorkItem{{ID: "todo:1", Status: "pending"}}})
	publisher.Send(worktableUpdate{revision: 2, items: []WorkItem{{ID: "todo:1", Status: "doing"}}})
	publisher.Close()

	deadline := time.Now().Add(2 * time.Second)
	seenTail := false
	for time.Now().Before(deadline) {
		mu.Lock()
		for _, update := range published {
			if update.revision == 2 {
				seenTail = true
			}
		}
		mu.Unlock()
		if seenTail {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !seenTail {
		t.Fatalf("close must drain tail revision 2, published=%+v", published)
	}
	// 关闭后 Send 不 panic；race 窗口内最多发布到 2（关闭前排空）或
	// 3（关闭前已入队），绝不发布 >3 的更新。
	publisher.Send(worktableUpdate{revision: 3, items: nil})
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	for _, update := range published {
		if update.revision > 3 {
			t.Fatalf("published revision %d exceeds the maximum sent revision", update.revision)
		}
	}
}
