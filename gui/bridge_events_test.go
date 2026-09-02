package gui

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
)

// replayAwareFakeApplication 是带重放窗口订阅的宿主桩（C4）。
//
//	droppedSeq  模拟 Go→WebView 这条腿丢掉一条载荷（EventEmitter 无返回值，
//	            Bridge 从投递本身永远看不到这种丢失）。
//	replayLimit > 0 时强制小缓冲/小窗口，用于覆盖"窗口淘汰后必须整份重拉"。
type replayAwareFakeApplication struct {
	*sessionAwareFakeApplication
	droppedSeq     uint64
	replayLimit    int
	replayRequests int
	subscription   application.Subscription
}

func (fake *replayAwareFakeApplication) SubscribeSessionWithReplay(
	sessionID string, buffer, replayWindow int,
) (application.Subscription, error) {
	fake.replayRequests++
	if fake.replayLimit > 0 {
		buffer, replayWindow = 1, fake.replayLimit
	}
	subscription := fake.hub.SubscribeWithReplay(func(item application.Event) bool {
		return item.SessionID == "" || item.SessionID == sessionID
	}, buffer, replayWindow)
	fake.subscription = subscription
	return subscription, nil
}

// appliedRenderer 模拟渲染层落地：记录每个 delivery_seq 被应用几次与事件类型，
// 并可丢弃指定序号的首次投递。
type appliedRenderer struct {
	mu      sync.Mutex
	counts  map[uint64]int
	kinds   map[application.EventKind]int
	dropSeq uint64
}

func newAppliedRenderer(dropSeq uint64) *appliedRenderer {
	return &appliedRenderer{
		counts:  make(map[uint64]int),
		kinds:   make(map[application.EventKind]int),
		dropSeq: dropSeq,
	}
}

func (renderer *appliedRenderer) emit(name string, payload any) {
	if name != eventName {
		return
	}
	item, ok := payload.(application.Event)
	if !ok {
		return
	}
	renderer.mu.Lock()
	defer renderer.mu.Unlock()
	if renderer.dropSeq != 0 && item.DeliverySeq == renderer.dropSeq && renderer.counts[item.DeliverySeq] == 0 {
		renderer.dropSeq = 0 // 只丢一次：之后到达的同序号事件正是重推
		return
	}
	renderer.counts[item.DeliverySeq]++
	renderer.kinds[item.Kind]++
}

func (renderer *appliedRenderer) count(seq uint64) int {
	renderer.mu.Lock()
	defer renderer.mu.Unlock()
	return renderer.counts[seq]
}

func (renderer *appliedRenderer) kindCount(kind application.EventKind) int {
	renderer.mu.Lock()
	defer renderer.mu.Unlock()
	return renderer.kinds[kind]
}

// awaitCount 等待某个 delivery_seq 至少被应用 want 次。
func (renderer *appliedRenderer) awaitCount(t *testing.T, seq uint64, want int) {
	t.Helper()
	if err := renderer.pollUntil(func() bool { return renderer.count(seq) >= want }); err != nil {
		t.Fatalf("delivery_seq %d applied %d times, want >= %d: %v", seq, renderer.count(seq), want, err)
	}
}

// awaitKind 等待某个事件类型至少到达 want 次。
func (renderer *appliedRenderer) awaitKind(t *testing.T, kind application.EventKind, want int) {
	t.Helper()
	if err := renderer.pollUntil(func() bool { return renderer.kindCount(kind) >= want }); err != nil {
		t.Fatalf("kind %s arrived %d times, want >= %d: %v", kind, renderer.kindCount(kind), want, err)
	}
}

func (renderer *appliedRenderer) pollUntil(ready func() bool) error {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return context.DeadlineExceeded
}

// awaitWatermark 等待 hub 给本订阅分配到 at least 序号（作为发布的同步点）。
func awaitWatermark(t *testing.T, subscription application.Subscription, atLeast uint64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if subscription.DeliveryWatermark() >= atLeast {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("delivery watermark = %d, want >= %d", subscription.DeliveryWatermark(), atLeast)
}

func startReplayBridge(t *testing.T, app *replayAwareFakeApplication, renderer *appliedRenderer) *Bridge {
	t.Helper()
	bridge, err := NewBridge(app, Options{})
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	bridge.Start(ctx, func(_ context.Context, name string, payload any) { renderer.emit(name, payload) })
	t.Cleanup(bridge.Stop)
	return bridge
}

// TestBridgeResendsEventsTheRendererHasNotAcked 是 C4 的主场景：Go→WebView 丢了
// 一条事件，Bridge 从投递本身无从得知，只能靠渲染层回执的水位发现落后，并从
// 订阅的重放窗口增量重推。重推是幂等的（渲染层按 delivery_seq 去重）。
func TestBridgeResendsEventsTheRendererHasNotAcked(t *testing.T) {
	app := &replayAwareFakeApplication{
		sessionAwareFakeApplication: &sessionAwareFakeApplication{fakeApplication: newFakeApplication()},
		droppedSeq:                  2,
	}
	renderer := newAppliedRenderer(2)
	bridge := startReplayBridge(t, app, renderer)

	if app.replayRequests == 0 {
		t.Fatal("Bridge must subscribe with a replay window (C4)")
	}
	for index := 1; index <= 3; index++ {
		app.hub.Publish(application.EventSnapshotChanged, uint64(index), "", nil)
	}
	awaitWatermark(t, app.subscription, 3)

	// 渲染层只应用到 1：2 在链路里丢了，3 虽然到了但它没有回执过。
	bridge.AckEvents(1)
	renderer.awaitCount(t, 2, 1)
	// 水位不再推进时重复重推是幂等的，但必须封顶（eventResendMaxTries）。
	if err := renderer.pollUntil(func() bool { return renderer.count(2) >= eventResendMaxTries }); err != nil {
		t.Logf("resend settled before the cap: count(2)=%d", renderer.count(2))
	}
	if got := renderer.count(2); got > eventResendMaxTries {
		t.Fatalf("delivery_seq 2 resent %d times, want <= %d (resend cap)", got, eventResendMaxTries)
	}
	if got := renderer.kindCount(application.EventResyncRequired); got != 0 {
		t.Fatalf("covered gap must not force resync, got %d resync events", got)
	}

	// 过期回执必须被忽略：水位只允许单调推进，否则会把已确认的区间再判成缺口。
	bridge.AckEvents(1)
	bridge.AckEvents(0)
	bridge.mu.Lock()
	acked := bridge.ackedSeq
	bridge.mu.Unlock()
	if acked != 1 {
		t.Fatalf("acked watermark = %d after stale acks, want 1", acked)
	}
}

// TestBridgeFallsBackToResyncWhenWindowEvicted 覆盖补不齐的分支：落后事件已滑出
// 重放窗口时，Bridge 不得对补不回来的区间无限重推，而要投递一条带当前水位的
// resync.required，让渲染层整份重拉并把水位抬到该处。
func TestBridgeFallsBackToResyncWhenWindowEvicted(t *testing.T) {
	app := &replayAwareFakeApplication{
		sessionAwareFakeApplication: &sessionAwareFakeApplication{fakeApplication: newFakeApplication()},
		replayLimit:                 2,
	}
	renderer := newAppliedRenderer(0)
	bridge := startReplayBridge(t, app, renderer)

	for index := 1; index <= 6; index++ {
		app.hub.Publish(application.EventSnapshotChanged, uint64(index), "", nil)
	}
	awaitWatermark(t, app.subscription, 6)

	bridge.AckEvents(1)
	renderer.awaitKind(t, application.EventResyncRequired, 1)
	// 止损：水位不再推进时重推必须封顶，不能对补不回来的区间无限投递。
	if got := renderer.kindCount(application.EventResyncRequired); got > eventResendMaxTries {
		t.Fatalf("resync events = %d, want <= %d (resend cap)", got, eventResendMaxTries)
	}
}

// TestBridgeReplayEventsRequiresRunningRelay 确认未启动时补取显式不可用：
// 渲染层拿 covered=false 就会走权威快照重拉，而不是误以为"没有缺口"。
func TestBridgeReplayEventsRequiresRunningRelay(t *testing.T) {
	app := &replayAwareFakeApplication{
		sessionAwareFakeApplication: &sessionAwareFakeApplication{fakeApplication: newFakeApplication()},
	}
	bridge, err := NewBridge(app, Options{})
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}
	if replay := bridge.ReplayEvents(0); replay.Covered {
		t.Fatalf("ReplayEvents before Start = %#v, want covered=false", replay)
	}
	if app.replayRequests != 0 {
		t.Fatalf("Start must not run before it is called, replay requests = %d", app.replayRequests)
	}
}
