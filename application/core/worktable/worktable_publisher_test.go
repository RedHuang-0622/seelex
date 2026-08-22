package worktable

import (
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/model"
)

// ── worktable.changed CSP 汇聚发布器（白盒）──────────────────

func TestWorkTablePublisherCoalescesBurstToLatest(t *testing.T) {
	var mu sync.Mutex
	var published []WorkTableUpdate
	publisher := NewWorkTablePublisher(func(update WorkTableUpdate) {
		mu.Lock()
		published = append(published, update)
		mu.Unlock()
	})
	defer publisher.Close()

	const burst = 8
	for revision := uint64(1); revision <= burst; revision++ {
		publisher.Send(WorkTableUpdate{Revision: revision, Items: []model.WorkItem{{ID: "todo:1", Status: "doing"}}})
	}

	deadline := time.Now().Add(2 * time.Second)
	last := uint64(0)
	for time.Now().Before(deadline) {
		mu.Lock()
		if len(published) > 0 {
			last = published[len(published)-1].Revision
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
	var published []WorkTableUpdate
	publisher := NewWorkTablePublisher(func(update WorkTableUpdate) {
		mu.Lock()
		published = append(published, update)
		mu.Unlock()
	})

	publisher.Send(WorkTableUpdate{Revision: 1, Items: []model.WorkItem{{ID: "todo:1", Status: "pending"}}})
	publisher.Send(WorkTableUpdate{Revision: 2, Items: []model.WorkItem{{ID: "todo:1", Status: "doing"}}})
	publisher.Close()

	deadline := time.Now().Add(2 * time.Second)
	seenTail := false
	for time.Now().Before(deadline) {
		mu.Lock()
		for _, update := range published {
			if update.Revision == 2 {
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
	publisher.Send(WorkTableUpdate{Revision: 3, Items: nil})
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	for _, update := range published {
		if update.Revision > 3 {
			t.Fatalf("published revision %d exceeds the maximum sent revision", update.Revision)
		}
	}
}
