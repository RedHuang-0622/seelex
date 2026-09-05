# core/plan

## 生态位

Plan 打点/分支事件/重规划

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### plan_tools.go

- `func (service *Service) updatePlanFromLoad(argsJSON string)` — updatePlanFromLoad 从 plan_load 的参数 JSON 初始化 PlanState。
- `func (service *Service) updatePlanFromRunResult(resultJSON string)` — updatePlanFromRunResult 从 plan_run 返回的 JSON 更新 PlanState。
- `func resolveNodeStatus(nodes []struct { NodeID string `json:"node_id"` Kind string `json:"kind"` Status string `json:"status"` Output string `json:"output,omitempty"` Skipped bool `json:"skipped"` Aborted bool `json:"aborted"` StartedAt string `json:"started_at,omitempty"` EndedAt string `json:"ended_at,omitempty"` }, nodeID string) NodeStatus` — resolveNodeStatus 辅助：从框架返回的 nodes 列表中查找 nodeID 的状态。
- `func (service *Service) planProjectionLocked(sessionID string) *PlanState` — HandlePlanNodeComplete 是 plan 执行事实的投影订阅（由 Runtime 经
- `func (service *Service) mirrorPlanProjectionForSessionLocked(sessionID string, fallback func() *PlanState)` — mirrorPlanProjectionForSessionLocked 在会话成为视图会话时刷新
- `func (service *Service) HandlePlanNodeComplete(event dto.PlanNodeEvent)`
- `func (service *Service) handleViewPlanNodeComplete(event dto.PlanNodeEvent, sessionID string)` — handleViewPlanNodeComplete 是当前视图会话的 plan 节点事件路径：投影即
- `func (service *Service) handleBackgroundPlanNodeComplete(event dto.PlanNodeEvent, sessionID string)` — handleBackgroundPlanNodeComplete 是后台会话的 plan 节点事件路径：投影变更
- `func (service *Service) HandlePlanBranchEvent(event seelplan.PlanBranchEvent)` — HandlePlanBranchEvent 应用来自桥接层的分支生命周期迁移，并向两端前端发布
- `func mapKindForDisplay(kind string) string` — mapKindForDisplay 将框架节点 kind 映射为 seelex PlanNode 展示值。
- `func isTerminalNodeStatus(status string) bool` — isTerminalNodeStatus 判定节点状态是否为终态（checkpoint 只对终态生效）。
- `func (service *Service) handlePlanRunFailureLocked(errMsg, resultJSON string) *Interaction` — handlePlanRunFailure 处理 plan_run 执行失败的情况。
- `func (service *Service) replanRequestLocked(failure, idempotencyKey string) dto.ReplanRequest` — replanRequestLocked 从权威快照提取最小的可用恢复上下文。要求调用方持有
- `func (service *Service) replanFailedWork(ctx context.Context, interactionID, failure string) (resultErr error)` — replanFailedWork 替换失败的 Plan 但不执行它：保留用户在恢复规划与任何新
- `func planRunFailure(resultJSON string) string`
- `func extractFailedNodeID(errMsg string) string` — extractFailedNodeID 从 scheduler 错误消息中提取失败节点的 ID。
