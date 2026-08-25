package scenario

import (
	"context"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
)

// TestEventRecorderWaitForRevisionAcceptsOutOfOrderDelivery 回归 macOS CI
// 上 TestOfflineApplicationBootstrapComposition 的偶发超时：并发 bump +
// publish 可能让低 Revision 事件最后到达，waitForRevision 必须接受任一
// 事件达到目标 Revision，而不是只盯最后一条。
func TestEventRecorderWaitForRevisionAcceptsOutOfOrderDelivery(t *testing.T) {
	hub := application.NewEventHub()
	recorder := newEventRecorder(hub.Subscribe(8))
	defer recorder.close()

	hub.Publish(application.EventMessageAdded, 11, "r1", nil)
	hub.Publish(application.EventSnapshotChanged, 10, "r2", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := recorder.waitForRevision(ctx, 11); err != nil {
		t.Fatalf("waitForRevision(11) = %v, want nil with revision 11 delivered before revision 10", err)
	}
}
