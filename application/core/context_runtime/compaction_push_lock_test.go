package context_runtime

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
)

// 推帧的窄串行（compactionPushLock）：压缩栈是链式结构，PushCompact 校验
// PrevSegmentID / PrevRequestFrom / PrevRequestTo 必须与栈顶逐一相等（见
// sessionstore.SessionContextStore.PushCompact）。装配层这条推帧路径原先是靠
// Core.ViewMu 的宽临界区**顺带**串行的；推帧移出 ViewMu 之后（见
// prepareExecutionContextFor 的锁纪律）必须显式补回，否则两个压缩并发推同一会话
// 时，后来者会撞锚点校验——那是我们自己引入的降级。
//
// 判据是确定性的：探针在进入推帧时登记并发度并阻塞，于是"两条推帧是否重叠"变成
// 可观测量。删掉 compactionPushLock，第一个用例立刻红。

// pushConcurrencyProbe 是"同时在推的帧数"探针。
type pushConcurrencyProbe struct {
	entered chan string // 进入推帧的会话键
	release chan struct{}

	mu       sync.Mutex
	inFlight int
	max      int
}

func (probe *pushConcurrencyProbe) PushCompactionFrame(
	_ context.Context,
	sessionID string,
	_ CompactionIndexRequest,
) (CompactionIndexReceipt, error) {
	probe.mu.Lock()
	probe.inFlight++
	if probe.inFlight > probe.max {
		probe.max = probe.inFlight
	}
	probe.mu.Unlock()
	probe.entered <- sessionID
	<-probe.release
	probe.mu.Lock()
	probe.inFlight--
	probe.mu.Unlock()
	return CompactionIndexReceipt{SegmentID: "seg-" + sessionID, SummarySource: "local"}, nil
}

func (probe *pushConcurrencyProbe) maxConcurrent() int {
	probe.mu.Lock()
	defer probe.mu.Unlock()
	return probe.max
}

// pushProbeOverflow 是一份非空的溢出素材（空溢出走 Skipped 分支，不会问索引面）。
func pushProbeOverflow() []contract.EngineMessage {
	content := "compacted turns"
	return []contract.EngineMessage{{Role: "user", Content: content, ContentSet: true}}
}

// startPush 在后台发起一次推帧，返回完成信号与（推完后的）回执读取。
func startPush(coordinator *Coordinator, sessionID string) chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = coordinator.pushCompactionFrame(sessionID, "task-"+sessionID,
			pushProbeOverflow(), nil, task_context.TranscriptEventRange{EventFrom: 1, EventTo: 2}, "")
	}()
	return done
}

func awaitEntered(t *testing.T, entered <-chan string) string {
	t.Helper()
	select {
	case sessionID := <-entered:
		return sessionID
	case <-time.After(2 * time.Second):
		t.Fatal("2s 内没有进入推帧（夹具失效）")
		return ""
	}
}

func awaitPush(t *testing.T, done chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("2s 内推帧没有返回")
	}
}

// TestCompactionPushSerializesWithinSession：同会话的两条推帧不得重叠。
func TestCompactionPushSerializesWithinSession(t *testing.T) {
	probe := &pushConcurrencyProbe{entered: make(chan string, 8), release: make(chan struct{})}
	coordinator := &Coordinator{compactionIndex: probe}

	first := startPush(coordinator, "session-s1")
	if sessionID := awaitEntered(t, probe.entered); sessionID != "session-s1" {
		t.Fatalf("第一条推帧的会话键 = %q，want session-s1", sessionID)
	}

	second := startPush(coordinator, "session-s1")
	select {
	case sessionID := <-probe.entered:
		t.Fatalf("同会话的第二条推帧进入了索引面（%q）：推帧串行锁没生效", sessionID)
	case <-time.After(200 * time.Millisecond):
	}

	close(probe.release)
	awaitPush(t, first)
	awaitPush(t, second)
	if max := probe.maxConcurrent(); max != 1 {
		t.Fatalf("同会话并发推帧峰值 = %d，want 1（链式栈的锚点校验要求一次只推一帧）", max)
	}
}

// TestCompactionPushDoesNotSerializeAcrossSessions：锁必须按会话键取——
// 不同会话的压缩互不等待（否则就把 ViewMu 那条全局串行原样搬回来了）。
func TestCompactionPushDoesNotSerializeAcrossSessions(t *testing.T) {
	probe := &pushConcurrencyProbe{entered: make(chan string, 8), release: make(chan struct{})}
	coordinator := &Coordinator{compactionIndex: probe}

	first := startPush(coordinator, "session-a")
	if sessionID := awaitEntered(t, probe.entered); sessionID != "session-a" {
		t.Fatalf("第一条推帧的会话键 = %q，want session-a", sessionID)
	}

	second := startPush(coordinator, "session-b")
	if sessionID := awaitEntered(t, probe.entered); sessionID != "session-b" {
		t.Fatalf("第二条推帧的会话键 = %q，want session-b（跨会话不该互等）", sessionID)
	}

	close(probe.release)
	awaitPush(t, first)
	awaitPush(t, second)
	if max := probe.maxConcurrent(); max != 2 {
		t.Fatalf("跨会话并发推帧峰值 = %d，want 2（锁必须按会话键取）", max)
	}
}
