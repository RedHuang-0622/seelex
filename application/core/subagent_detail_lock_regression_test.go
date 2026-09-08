package core

import (
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/model"
	subagentsession "github.com/RedHuang-0622/seelex/seelebridge/session"
)

// lockOrderEngine 复刻真实锁序：节点执行期间会持有 "session 锁"
// （sessionMu，对应 Seele Session.History 的 mu），引擎子树导出同样要拿它。
type lockOrderEngine struct {
	*fakeEngine
	sessionMu sync.Mutex
}

func (engine *lockOrderEngine) SubAgentTree() []dto.SubAgentTreeNode {
	engine.sessionMu.Lock()
	defer engine.sessionMu.Unlock()
	return engine.fakeEngine.SubAgentTree()
}

// TestSubagentDetailNoViewMuSessionLockInversion 复现 2026-09-08 死锁链路：
// 节点 goroutine 持 Session 锁回调 HandleSubagentToolEvent（要 ViewMu.Lock），
// 同时前端取 SubagentDetail。修复前 SubagentDetail 持 ViewMu.RLock 再读
// 引擎子树（Session 锁）→ 循环等待；修复后引擎调用在 ViewMu 外，双方在
// 有限时间内都能完成。
func TestSubagentDetailNoViewMuSessionLockInversion(t *testing.T) {
	engine := &lockOrderEngine{fakeEngine: &fakeEngine{}}
	engine.mu.Lock()
	engine.subAgentTree = []dto.SubAgentTreeNode{{
		ID: "node-1", Status: "running", SessionID: "sub-session-1",
	}}
	engine.mu.Unlock()

	service := newTestService(t, engine)
	service.ViewMu.Lock()
	service.Core.Snapshot.Session.ID = "main-session"
	service.Core.Snapshot.Chat.RequestID = "request-1"
	service.Core.Snapshot.Runtime.Plan = &model.PlanState{
		Nodes: []model.PlanNode{{ID: "node-1"}},
	}
	service.ViewMu.Unlock()

	var wait sync.WaitGroup
	wait.Add(2)
	// 节点 goroutine：持 Session 锁执行工具事件投影（真实路径为
	// ReActLoop 持 Session.mu → HandleSubagentToolEvent）。
	go func() {
		defer wait.Done()
		engine.sessionMu.Lock()
		defer engine.sessionMu.Unlock()
		service.HandleSubagentToolEvent(subagentsession.SubagentToolEvent{
			NodeID: "node-1", ID: "tool-1", Status: "running",
		})
	}()
	// 前端详情读取：锁外取子树 → 锁内读 Plan/WorkTable 快照。
	var detailErr error
	go func() {
		defer wait.Done()
		_, detailErr = service.SubagentSessionDetail("node-1")
	}()

	done := make(chan struct{})
	go func() { wait.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("SubagentDetail 与节点工具事件锁序成环（ViewMu ⇄ Session.mu），3s 未完成")
	}
	if detailErr != nil {
		t.Fatalf("SubagentSessionDetail = %v", detailErr)
	}
}
