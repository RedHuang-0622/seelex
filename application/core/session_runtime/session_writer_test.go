package session_runtime

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	"github.com/RedHuang-0622/seelex/application/model"
)

// A3 单写者：会话落盘收口到 per-session actor（同会话串行 FIFO、跨会话并行），
// 且读路径不经过它（C2：长落盘不阻塞读）。

// writerTestSessions 只覆写读面（transcript 尾部读回）：证明读路径与写者锁
// 无关。其余 SessionPort 面继承既有夹具（同一个包的 granularPortTestSessions），
// 不为了"能注入"再手写一整套空实现。
type writerTestSessions struct {
	*granularPortTestSessions
	reads atomic.Int64
}

func (s *writerTestSessions) LoadTranscriptTailWorkspace(string, string, int, int) ([]model.TranscriptEvent, error) {
	s.reads.Add(1)
	return []model.TranscriptEvent{{Seq: 1, Role: "user", Content: "hi"}}, nil
}

func (s *writerTestSessions) LoadToolResultWorkspace(string, string, string) (model.StoredToolResult, error) {
	return model.StoredToolResult{}, nil
}

func newWriterTestCoordinator(t *testing.T, sessions contract.SessionPort) *Coordinator {
	t.Helper()
	core := state.New(contract.Dependencies{Sessions: sessions})
	return NewCoordinator(Deps{
		Core:                 core,
		TranscriptTailBudget: func(any) int { return 1000 },
		IsInternalContent:    func(string) bool { return false },
	})
}

// TestSessionWriterSerializesSameSessionFIFO 同一会话的写点严格串行且 FIFO：
// 占住写者锁后按顺序排队的两个写点，进入临界区的顺序必须与排队顺序一致。
func TestSessionWriterSerializesSameSessionFIFO(t *testing.T) {
	coordinator := newWriterTestCoordinator(t, &writerTestSessions{})

	var inFlight, peak atomic.Int64
	entered := make(chan int, 4)
	run := func(id int) error {
		current := inFlight.Add(1)
		for {
			seen := peak.Load()
			if current <= seen || peak.CompareAndSwap(seen, current) {
				break
			}
		}
		entered <- id
		inFlight.Add(-1)
		return nil
	}

	lock := coordinator.SessionWriterLock("s-1")
	lock.Lock() // 主 goroutine 先占住，制造排队
	var wait sync.WaitGroup
	queued := make(chan struct{}, 2)
	launch := func(id int) {
		wait.Add(1)
		go func() {
			defer wait.Done()
			queued <- struct{}{}
			if err := coordinator.RunSessionWrite("s-1", func() error { return run(id) }); err != nil {
				t.Errorf("写点 %d 失败：%v", id, err)
			}
		}()
	}
	launch(2)
	<-queued
	time.Sleep(2 * time.Millisecond) // 让 2 号的 acquire 先进入 actor 队列
	launch(3)
	<-queued
	time.Sleep(2 * time.Millisecond)
	lock.Unlock()
	wait.Wait()
	close(entered)

	order := make([]int, 0, 2)
	for id := range entered {
		order = append(order, id)
	}
	if len(order) != 2 || order[0] != 2 || order[1] != 3 {
		t.Fatalf("进入临界区顺序 = %v, want [2 3]（同会话 FIFO）", order)
	}
	if peak.Load() != 1 {
		t.Fatalf("同会话临界区并发峰值 = %d, want 1（单写者）", peak.Load())
	}
}

// TestSessionWriterParallelAcrossSessions 不同会话的写点并行（不是全局单写者）：
// 两个会话必须能同时进入各自的临界区，否则跨会话收尾会互相排队。
func TestSessionWriterParallelAcrossSessions(t *testing.T) {
	coordinator := newWriterTestCoordinator(t, &writerTestSessions{})
	entered := make(chan string, 2)
	release := make(chan struct{})
	var wait sync.WaitGroup
	for _, sessionID := range []string{"s-a", "s-b"} {
		wait.Add(1)
		go func(sessionID string) {
			defer wait.Done()
			_ = coordinator.RunSessionWrite(sessionID, func() error {
				entered <- sessionID
				<-release
				return nil
			})
		}(sessionID)
	}
	for index := 0; index < 2; index++ {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("跨会话写者被串行化：第二个会话没能进入临界区")
		}
	}
	close(release)
	wait.Wait()
}

// TestReaderNotBlockedBySessionWriter 写者持锁期间读路径正常返回（C2：读侧不
// 经过写者锁；长落盘不得把读卡住）。
func TestReaderNotBlockedBySessionWriter(t *testing.T) {
	sessions := &writerTestSessions{}
	coordinator := newWriterTestCoordinator(t, sessions)
	holding := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_ = coordinator.RunSessionWrite("s-1", func() error {
			close(holding)
			<-release
			return nil
		})
	}()
	<-holding

	done := make(chan error, 1)
	go func() {
		_, err := coordinator.LoadSessionTranscript(Location{WorkspaceID: "ws-1"}, "s-1")
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("读路径失败：%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("读路径被写者阻塞（C2 回归）")
	}
	if sessions.reads.Load() != 1 {
		t.Fatalf("读端口调用次数 = %d, want 1", sessions.reads.Load())
	}
	close(release)
}

// TestSessionWriterClosedDegradesToNoop 关闭后写点退化为"直接执行"而不是卡死
// （退出路径契约：关闭时仍在排队的写点必须收敛）。
func TestSessionWriterClosedDegradesToNoop(t *testing.T) {
	coordinator := newWriterTestCoordinator(t, &writerTestSessions{})
	coordinator.StopCatalogRefresh() // 关闭过渡互斥 actor + 落盘单写者

	ran := false
	done := make(chan error, 1)
	go func() {
		done <- coordinator.RunSessionWrite("s-1", func() error {
			ran = true
			return nil
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("关闭后写点返回错误：%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("关闭后写点卡死（退出路径回归）")
	}
	if !ran {
		t.Fatal("关闭后写点被丢弃：落盘收敛语义被破坏")
	}
}
