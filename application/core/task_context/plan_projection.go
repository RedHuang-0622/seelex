package task_context

import (
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/model"
)

// PlanProjectionFor 返回指定**后台**会话的 plan 显示投影的只读深拷贝
// （F：缓存归 task 协调器自有状态，planMu 保护；读者拿拷贝，写者走
// ApplyPlanNodeProjection 在 planMu 下原地变更）。当前视图会话的 plan 仍
// 是 Snapshot.Runtime.Plan 视图镜像，不进入本表。
func (c *Coordinator) PlanProjectionFor(sessionID string, fromStack func() *model.PlanState) *model.PlanState {
	if sessionID == "" {
		return nil
	}
	c.planMu.Lock()
	defer c.planMu.Unlock()
	if c.planProjections == nil {
		c.planProjections = make(map[string]*model.PlanState)
	}
	plan := c.planProjections[sessionID]
	if plan == nil {
		plan = fromStack()
		if plan == nil {
			return nil
		}
		c.planProjections[sessionID] = plan
	}
	return model.CloneRuntimeState(model.RuntimeState{Plan: plan}).Plan
}

// DropPlanProjection 释放指定后台会话的 plan 投影缓存（unload/删除路径）。
func (c *Coordinator) DropPlanProjection(sessionID string) {
	c.planMu.Lock()
	defer c.planMu.Unlock()
	delete(c.planProjections, sessionID)
}

// PlanNodeApplyResult 是后台 plan 事件应用结果（planMu 段内产生的深拷贝，
// 供调用方在锁外发布事件/刷新工作台）。
type PlanNodeApplyResult struct {
	Plan        *model.PlanState
	ChangedNode *model.PlanNode
	NodeFound   bool
	Applied     bool
}

// ApplyPlanNodeProjection 在 planMu 下把 plan 节点事件应用到指定后台会话的
// plan 投影（F：后台投影的变更不再占用 Core.ViewMu）。基线缺失时先从会话
// plan 栈重建（栈读需要 Core.ViewMu，锁外短读一次，不持 planMu 取 ViewMu）。
// 返回深拷贝与变更节点；调用方不得持有 Core.ViewMu 调用本方法。
func (c *Coordinator) ApplyPlanNodeProjection(sessionID string, event dto.PlanNodeEvent) PlanNodeApplyResult {
	var result PlanNodeApplyResult
	if sessionID == "" {
		return result
	}
	var baseline *model.PlanState
	c.ViewMu.RLock()
	st := c.sessionStateLocked(sessionID)
	baseline = ActivePlanFromStack(st.planStack, st.activePlanID)
	c.ViewMu.RUnlock()

	c.planMu.Lock()
	defer c.planMu.Unlock()
	if c.planProjections == nil {
		c.planProjections = make(map[string]*model.PlanState)
	}
	plan := c.planProjections[sessionID]
	if plan == nil {
		if baseline == nil {
			return result
		}
		plan = baseline
		c.planProjections[sessionID] = plan
	}
	result.Applied = true
	if event.NodeID == "" {
		switch event.Status {
		case "running":
			if plan.Status == model.PlanPending {
				plan.Status = model.PlanRunning
			}
		case "completed":
			plan.Status = model.PlanCompleted
			plan.Progress = 1.0
		case "failed":
			plan.Status = model.PlanFailed
		case "canceled", "aborted":
			plan.Status = model.PlanAborted
		}
	}
	if event.NodeID != "" {
		if node := findPlanNode(plan.Nodes, event.NodeID); node != nil {
			node.Status = PlanNodeStatus(event.Status)
			if event.Kind != "" {
				node.Kind = planKindForDisplay(event.Kind)
			}
			if event.Elapsed != "" {
				node.Elapsed = event.Elapsed
			}
			if event.Output != "" {
				node.Output = event.Output
			}
			AppendPlanNodeEvent(node, event)
			result.NodeFound = true
		}
		RecalculatePlanProgress(plan)
	}
	result.Plan = model.CloneRuntimeState(model.RuntimeState{Plan: plan}).Plan
	if result.NodeFound {
		if node := findPlanNode(result.Plan.Nodes, event.NodeID); node != nil {
			copy := *node
			result.ChangedNode = &copy
		}
	}
	return result
}

func findPlanNode(nodes []model.PlanNode, nodeID string) *model.PlanNode {
	for index := range nodes {
		if nodes[index].ID == nodeID {
			return &nodes[index]
		}
	}
	for index := range nodes {
		if node := findPlanNode(nodes[index].Children, nodeID); node != nil {
			return node
		}
	}
	return nil
}

func planKindForDisplay(kind string) string {
	if kind == "approve" || kind == "" {
		if kind == "approve" {
			return "manual"
		}
		return "auto"
	}
	return kind
}
