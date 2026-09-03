package task_context

import (
	"github.com/RedHuang-0622/seelex/application/model"
)

// PlanProjectionFor 返回指定**后台**会话的 plan 显示投影缓存（F：缓存归
// task 协调器自有状态，planMu 保护 map 结构；当前视图会话的 plan 仍是
// Snapshot.Runtime.Plan 视图镜像，不进入本表）。
//
// fromStack 在缓存缺失时从会话 plan 栈重建基线；读取会话栈需要
// Core.ViewMu（迁移期约定：调用方持有 ViewMu，变更也发生在 ViewMu 临界区
// 内；planMu 是叶子锁，本方法持有时绝不反向取 ViewMu）。
func (c *Coordinator) PlanProjectionFor(sessionID string, fromStack func() *model.PlanState) *model.PlanState {
	if sessionID == "" {
		return nil
	}
	c.planMu.Lock()
	defer c.planMu.Unlock()
	if c.planProjections == nil {
		c.planProjections = make(map[string]*model.PlanState)
	}
	if plan := c.planProjections[sessionID]; plan != nil {
		return plan
	}
	plan := fromStack()
	if plan == nil {
		return nil
	}
	c.planProjections[sessionID] = plan
	return plan
}

// DropPlanProjection 释放指定后台会话的 plan 投影缓存（unload/删除路径）。
func (c *Coordinator) DropPlanProjection(sessionID string) {
	c.planMu.Lock()
	defer c.planMu.Unlock()
	delete(c.planProjections, sessionID)
}
