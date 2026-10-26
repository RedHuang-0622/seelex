package core

import (
	"testing"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/middle/residency"
	"github.com/RedHuang-0622/seelex/middle/switch/sessionusecase"
)

// P20 回归：超限收敛的**快照必须不别名**会话使用序的底层数组。
//
// 现场（`-race` 抓到）：`residency.Order.Forget` 就地过滤（写回底层数组）与
// `EvictUntilWithinLimit` 在临界区外的遍历撞在同一段内存上 —— Go race detector 报
// "Write at … by Order.Forget / Previous read at … by EvictUntilWithinLimit"。
// 判据侧无法知道自己拿到的是副本，因此**复制是宿主（本层）的责任**。
func TestEvictionSnapshotsDoNotAliasOrderBackingArray(t *testing.T) {
	engine := newMultiSessionEngine()
	service := newTestService(t, engine)
	defer frontendOfTestService(service).Shutdown()
	for _, sessionID := range []string{"session-a", "session-b"} {
		engine.register(sessionID)
		if err := frontendOfTestService(service).ResumeSession(sessionID); err != nil {
			t.Fatalf("resume %s: %v", sessionID, err)
		}
		service.components.view.SessionViewMutateLocked(sessionID, func(view *session.View) {
			view.Conversation = []model.Message{{Role: "user", Content: "内容 " + sessionID}}
			view.ContentUnloaded = false
		})
	}

	service.ViewMu.Lock()
	service.loadedContentOrder = residency.Order{"session-a", "session-b"}
	service.residentOrder = residency.Order{"session-a", "session-b"}

	content := service.contentLimitSnapshotLocked("session-a")
	resident := service.residentEvictionSnapshotLocked()
	service.ViewMu.Unlock()

	if len(content.Order) != 2 || len(resident.Order) != 2 {
		t.Fatalf("快照应带出两条使用序：content=%v resident=%v", content.Order, resident.Order)
	}
	if &content.Order[0] == &service.loadedContentOrder[0] {
		t.Fatal("正文层快照的 Order 与 service.loadedContentOrder 共用底层数组：锁外遍历会与 Order.Forget 的写撞车")
	}
	if &resident.Order[0] == &service.residentOrder[0] {
		t.Fatal("引擎层快照的 Order 与 service.residentOrder 共用底层数组：锁外遍历会与 Order.Forget 的写撞车")
	}

	// 就地过滤之后，已经取出的快照必须**保持不变**（这正是复制要保证的性质）。
	before := append(residency.Order(nil), content.Order...)
	beforeResident := append(residency.Order(nil), resident.Order...)
	service.ViewMu.Lock()
	service.residentOrder = testResidencyPort().Forget(service.residentOrder, "session-a")
	service.loadedContentOrder = testResidencyPort().Forget(service.loadedContentOrder, "session-a")
	service.ViewMu.Unlock()
	if len(before) != 2 || before[0] != "session-a" || before[1] != "session-b" {
		t.Fatalf("快照被就地过滤改写：%v", before)
	}
	if len(beforeResident) != 2 || beforeResident[0] != "session-a" || beforeResident[1] != "session-b" {
		t.Fatalf("引擎层快照被就地过滤改写：%v", beforeResident)
	}
}
