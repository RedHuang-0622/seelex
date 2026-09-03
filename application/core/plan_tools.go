package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/subagent_view"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	seelplan "github.com/RedHuang-0622/seelex/seelebridge/plan"
)

// updatePlanFromLoad 从 plan_load 的参数 JSON 初始化 PlanState。
func (service *Service) updatePlanFromLoad(argsJSON string) {
	type planNodeSpec struct {
		Input string `json:"input"`
		Kind  string `json:"kind,omitempty"` // "auto" (default) or "manual"
	}
	var input struct {
		Entry string                  `json:"entry"`
		Nodes map[string]planNodeSpec `json:"nodes"`
		Edges map[string][]string     `json:"edges"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil || len(input.Nodes) == 0 {
		return
	}
	if _, ok := input.Nodes[input.Entry]; !ok || seelplan.DetectCycle(input.Edges) != nil {
		return
	}

	// 构建所有节点集合，供 TopoSort 使用
	allNodes := make(map[string]struct{}, len(input.Nodes))
	for id := range input.Nodes {
		allNodes[id] = struct{}{}
	}

	// 拓扑排序 → 稳定节点顺序
	order := seelplan.TopoSort(input.Entry, input.Edges, allNodes)

	nodes := make([]PlanNode, 0, len(input.Nodes))
	for _, id := range order {
		spec := input.Nodes[id]
		kind := spec.Kind
		if kind == "" {
			kind = "auto"
		}
		nodes = append(nodes, PlanNode{ID: id, Label: id, Kind: kind, Status: NodePending})
		if state := service.components.tasks.CurrentTaskExecution(); state != nil && state.RequestID == service.Core.Snapshot.Chat.RequestID {
			state.Checkpoint(id, spec.Input, string(NodePending), "", "")
		}
	}

	// 邻接表 → []PlanEdge
	planEdges := seelplan.AdjacencyToEdges(input.Edges)

	service.Core.Snapshot.Runtime.Plan = &PlanState{
		Name:        input.Entry,
		EntryNodeID: input.Entry,
		Status:      PlanPending,
		Nodes:       nodes,
		Edges:       planEdges,
	}
	// F-2c：视图会话的 plan 同步种子进 task_context 投影缓存（Snapshot 只是
	// 镜像副本；事件/结果后续都落协调器投影再镜像）。
	service.components.tasks.SeedPlanProjection(service.Core.Snapshot.Session.ID, service.Core.Snapshot.Runtime.Plan)
}

// updatePlanFromRunResult 从 plan_run 返回的 JSON 更新 PlanState。
// 解析格式对齐框架 NodeBase 的 snake_case JSON 标签（平铺，非嵌套）。
func (service *Service) updatePlanFromRunResult(resultJSON string) {
	var out struct {
		Status      string `json:"status"`
		NodeCount   int    `json:"node_count"`
		FinalOutput string `json:"final_output"`
		AbortReason string `json:"abort_reason,omitempty"`
		Nodes       []struct {
			NodeID    string `json:"node_id"`
			Kind      string `json:"kind"`
			Status    string `json:"status"`
			Output    string `json:"output,omitempty"`
			Skipped   bool   `json:"skipped"`
			Aborted   bool   `json:"aborted"`
			StartedAt string `json:"started_at,omitempty"`
			EndedAt   string `json:"ended_at,omitempty"`
		} `json:"nodes,omitempty"`
	}
	if err := json.Unmarshal([]byte(resultJSON), &out); err != nil {
		return
	}
	if service.Core.Snapshot.Runtime.Plan == nil {
		service.Core.Snapshot.Runtime.Plan = &PlanState{}
	}
	plan := service.Core.Snapshot.Runtime.Plan

	switch out.Status {
	case "completed":
		plan.Status = PlanCompleted
		plan.Progress = 1.0
	case "failed":
		plan.Status = PlanFailed
	case "aborted":
		plan.Status = PlanAborted
	default:
		plan.Status = PlanRunning
	}

	if out.NodeCount > 0 && len(plan.Nodes) == 0 {
		// 没有 plan_load 数据的情况下，用 node_count 创建占位节点
		for i := range out.NodeCount {
			plan.Nodes = append(plan.Nodes, PlanNode{
				ID:     fmt.Sprintf("node-%d", i+1),
				Label:  fmt.Sprintf("step-%d", i+1),
				Status: resolveNodeStatus(out.Nodes, fmt.Sprintf("node-%d", i+1)),
			})
		}
	}
	// 如果 framework 返回了 per-node 结果，更新详细信息
	if len(out.Nodes) > 0 {
		for i := range plan.Nodes {
			for _, on := range out.Nodes {
				if plan.Nodes[i].ID == on.NodeID {
					plan.Nodes[i].Status = task_context.PlanNodeStatus(on.Status)
					plan.Nodes[i].Kind = mapKindForDisplay(on.Kind)
					if on.Output != "" {
						plan.Nodes[i].Output = on.Output
					}
					if on.Skipped {
						plan.Nodes[i].Status = NodeSkipped
					}
					// 从 started_at/ended_at 计算耗时
					if on.StartedAt != "" && on.EndedAt != "" {
						if start, err := time.Parse(time.RFC3339, on.StartedAt); err == nil {
							if end, err2 := time.Parse(time.RFC3339, on.EndedAt); err2 == nil {
								plan.Nodes[i].Elapsed = end.Sub(start).String()
							}
						}
					}
					break
				}
			}
		}
	}

	// 计算已完成节点比例
	done := 0
	for _, n := range plan.Nodes {
		if n.Status == NodeCompleted || n.Status == NodeSkipped {
			done++
		}
	}
	if len(plan.Nodes) > 0 {
		plan.Progress = float64(done) / float64(len(plan.Nodes))
	}

	// 计算总耗时（最早 start → 最晚 end）
	var planStart, planEnd time.Time
	for _, n := range plan.Nodes {
		for _, on := range out.Nodes {
			if n.ID == on.NodeID && on.StartedAt != "" && on.EndedAt != "" {
				s, _ := time.Parse(time.RFC3339, on.StartedAt)
				e, _ := time.Parse(time.RFC3339, on.EndedAt)
				if planStart.IsZero() || s.Before(planStart) {
					planStart = s
				}
				if e.After(planEnd) {
					planEnd = e
				}
				break
			}
		}
	}
	if plan.Elapsed == "" && !planStart.IsZero() && !planEnd.IsZero() {
		plan.Elapsed = planEnd.Sub(planStart).String()
	}
	// F-2c：plan_run 结果同步回协调器投影（Snapshot 镜像与投影同轮收敛）。
	service.components.tasks.SeedPlanProjection(service.Core.Snapshot.Session.ID, plan)
}

// resolveNodeStatus 辅助：从框架返回的 nodes 列表中查找 nodeID 的状态。
func resolveNodeStatus(nodes []struct {
	NodeID    string `json:"node_id"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	Output    string `json:"output,omitempty"`
	Skipped   bool   `json:"skipped"`
	Aborted   bool   `json:"aborted"`
	StartedAt string `json:"started_at,omitempty"`
	EndedAt   string `json:"ended_at,omitempty"`
}, nodeID string) NodeStatus {
	for _, n := range nodes {
		if n.NodeID == nodeID {
			return task_context.PlanNodeStatus(n.Status)
		}
	}
	return NodePending
}

// HandlePlanNodeComplete 是 plan 执行事实的投影订阅（由 Runtime 经
// SetPlanNodeCallback 注册）：planEventSink 把 workplan 事件投影为
// PlanNodeEvent 后回调本方法，实时更新节点/计划状态并通知 TUI/GUI 重绘。
// NodeID 为空表示计划级投影（PlanStatus），否则为节点级投影（NodeStatus）。
// planProjectionLocked 返回指定会话的 plan 显示投影（调用方持有 Core.ViewMu）。
// 当前会话与 Snapshot.Runtime.Plan 同一指针（视图镜像）；后台会话的投影
// 缓存已归 task_context 协调器自有状态（planMu 护 map 结构，F），缺失时从
// 会话 plan 帧重建（ActivePlanFromStack）。
func (service *Service) planProjectionLocked(sessionID string) *PlanState {
	if sessionID == "" || sessionID == service.Core.Snapshot.Session.ID {
		return service.Core.Snapshot.Runtime.Plan
	}
	return service.components.tasks.PlanProjectionFor(sessionID, func() *PlanState {
		return task_context.ActivePlanFromStack(
			service.components.tasks.PlanStackFor(sessionID),
			service.components.tasks.ActivePlanIDFor(sessionID),
		)
	})
}

func (service *Service) HandlePlanNodeComplete(event dto.PlanNodeEvent) {
	service.ViewMu.RLock()
	viewSessionID := service.Core.Snapshot.Session.ID
	sessionID := event.SessionID
	if sessionID == "" {
		sessionID = viewSessionID
	}
	service.ViewMu.RUnlock()
	if sessionID != viewSessionID {
		service.handleBackgroundPlanNodeComplete(event, sessionID)
		return
	}
	service.handleViewPlanNodeComplete(event, sessionID)
}

// handleViewPlanNodeComplete 是当前视图会话的 plan 节点事件路径：投影即
// Snapshot.Runtime.Plan 视图镜像（镜像写留 ViewMu），与刷新/发布在同一个
// ViewMu 临界区内完成。
func (service *Service) handleViewPlanNodeComplete(event dto.PlanNodeEvent, sessionID string) {
	service.ViewMu.Lock()
	plan := service.Core.Snapshot.Runtime.Plan
	if plan == nil {
		service.ViewMu.Unlock()
		return
	}
	if event.NodeID == "" {
		// 计划级投影（PlanStatus）：终态最终仍以 plan_run 结果 JSON 为准，
		// 此处提前反映运行期状态。
		switch event.Status {
		case "running":
			if plan.Status == PlanPending {
				plan.Status = PlanRunning
			}
		case "completed":
			plan.Status = PlanCompleted
			plan.Progress = 1.0
		case "failed":
			plan.Status = PlanFailed
		case "canceled", "aborted":
			plan.Status = PlanAborted
		}
	}
	var changedNode *PlanNode
	if event.NodeID != "" {
		if node := subagent_view.FindPlanNodeByID(plan.Nodes, event.NodeID); node != nil {
			node.Status = task_context.PlanNodeStatus(event.Status)
			if event.Kind != "" {
				node.Kind = mapKindForDisplay(event.Kind)
			}
			if event.Elapsed != "" {
				node.Elapsed = event.Elapsed
			}
			if event.Output != "" {
				node.Output = event.Output
			}
			task_context.AppendPlanNodeEvent(node, event)
			// checkpoint 只对终态生效（旧 HandlePlanNodeComplete 只在节点完成时调用）；
			// 观测经 TaskService.ObservePlanEvent 写入功能打点快照
			// checkpoint 观测只对活跃会话（TaskService 无 For 变体）；后台
			// 会话 plan 打点由切换后基线重建，不做跨会话观测。
			if isTerminalNodeStatus(event.Status) && sessionID == service.Core.Snapshot.Session.ID {
				service.components.tasks.ObservePlanEvent(task_context.PlanEvent{
					NodeID: event.NodeID, Status: event.Status, Output: event.Output, Objective: node.Label,
				})
			}
			changedNode = node
		}
		task_context.RecalculatePlanProgress(plan)
	}
	// 子代理树投影：fork 子代理生命周期与 plan 节点事件同源（queued/running/
	// completed），树状态随权威 Snapshot 增量刷新（内存态，不落盘）。
	service.Core.Snapshot.Runtime.SubAgentTree = service.Deps.Engine.SubAgentTree()
	revision := service.bumpLocked()
	requestID := service.Core.Snapshot.Chat.RequestID
	var changed SubagentEvent
	if changedNode != nil {
		changed = subagent_view.SubagentChangedPayload(plan, event.PlanID, event.RunID, *changedNode)
	}
	service.ViewMu.Unlock()
	if changedNode != nil {
		service.publishSessionEvent(EventSubagentChanged, revision, requestID, sessionID, changed)
		service.refreshWorkTableFromSources()
		return
	}
	service.publishSessionEvent(EventSnapshotChanged, revision, requestID, sessionID, nil)
	service.refreshWorkTableFromSources()
}

// handleBackgroundPlanNodeComplete 是后台会话的 plan 节点事件路径：投影变更
// 在 task_context 协调器 planMu 下完成（F：不与视图写共享 ViewMu），视图
// 镜像（子代理树/Revision bump）在随后的 ViewMu 短临界区写，两段不嵌套。
func (service *Service) handleBackgroundPlanNodeComplete(event dto.PlanNodeEvent, sessionID string) {
	result := service.components.tasks.ApplyPlanNodeProjection(sessionID, event)
	if !result.Applied {
		return
	}
	service.ViewMu.Lock()
	service.Core.Snapshot.Runtime.SubAgentTree = service.Deps.Engine.SubAgentTree()
	revision := service.bumpLocked()
	requestID := service.Core.Snapshot.Chat.RequestID
	service.ViewMu.Unlock()
	if result.NodeFound && result.ChangedNode != nil {
		changed := subagent_view.SubagentChangedPayload(result.Plan, event.PlanID, event.RunID, *result.ChangedNode)
		service.publishSessionEvent(EventSubagentChanged, revision, requestID, sessionID, changed)
	} else {
		service.publishSessionEvent(EventSnapshotChanged, revision, requestID, sessionID, nil)
	}
	service.syncTasksFromSourcesFor(sessionID)
}

// HandlePlanBranchEvent 应用来自桥接层的分支生命周期迁移，并向两端前端发布
// 更新后的 runtime 快照。
func (service *Service) HandlePlanBranchEvent(event seelplan.PlanBranchEvent) {
	service.ViewMu.Lock()
	plan := service.Core.Snapshot.Runtime.Plan
	if plan == nil {
		service.ViewMu.Unlock()
		return
	}
	node := subagent_view.FindPlanNodeByID(plan.Nodes, event.NodeID)
	if node != nil {
		node.Status = task_context.PlanNodeStatus(event.Type)
		task_context.AppendPlanNodeEvent(node, dto.PlanNodeEvent{NodeID: event.NodeID, Status: event.Type, Output: event.Error, At: event.At})
	}
	switch event.Type {
	case "queued", "started":
		if plan.Status == PlanPending {
			plan.Status = PlanRunning
		}
	case "failed", "panicked":
		plan.Status = PlanFailed
	}
	task_context.RecalculatePlanProgress(plan)
	// 子代理树投影：分支生命周期（queued/started/failed）同样刷新树状态。
	service.Core.Snapshot.Runtime.SubAgentTree = service.Deps.Engine.SubAgentTree()
	revision := service.bumpLocked()
	requestID := service.Core.Snapshot.Chat.RequestID
	viewSessionID := service.Core.Snapshot.Session.ID
	var changed SubagentEvent
	if node != nil {
		changed = subagent_view.SubagentChangedPayload(plan, "", "", *node)
	}
	service.ViewMu.Unlock()
	if node != nil {
		service.publishSessionEvent(EventSubagentChanged, revision, requestID, viewSessionID, changed)
		service.refreshWorkTableFromSources()
		return
	}
	service.publishSessionEvent(EventSnapshotChanged, revision, requestID, viewSessionID, nil)
	service.refreshWorkTableFromSources()
}

// mapKindForDisplay 将框架节点 kind 映射为 seelex PlanNode 展示值。
// 框架内部使用 "approve"（KindApprove），但在用户侧展示为 "manual"。
func mapKindForDisplay(kind string) string {
	if kind == "approve" || kind == "" {
		if kind == "approve" {
			return "manual"
		}
		return "auto"
	}
	return kind
}

// isTerminalNodeStatus 判定节点状态是否为终态（checkpoint 只对终态生效）。
func isTerminalNodeStatus(status string) bool {
	switch status {
	case "completed", "failed", "aborted", "skipped", "canceled", "panicked":
		return true
	default:
		return false
	}
}

// handlePlanRunFailure 处理 plan_run 执行失败的情况。
// 更新 PlanState 中失败节点的状态，弹出 retry/skip/abort 交互。
func (service *Service) handlePlanRunFailureLocked(errMsg, resultJSON string) *Interaction {
	plan := service.Core.Snapshot.Runtime.Plan
	if plan == nil {
		return nil
	}

	// 更新计划整体状态
	plan.Status = PlanFailed
	service.components.tasks.ObservePlanEvent(task_context.PlanEvent{
		NodeID: extractFailedNodeID(errMsg), Status: "failed", Output: resultJSON,
		Failure: errMsg, Objective: "authoritative plan node",
	})

	// 尝试解析 resultJSON 中的部分节点结果（framework 返回失败点之前的节点）
	if resultJSON != "" {
		var out struct {
			Status      string `json:"status"`
			NodeCount   int    `json:"node_count"`
			FinalOutput string `json:"final_output"`
			AbortReason string `json:"abort_reason,omitempty"`
			Nodes       []struct {
				NodeID    string `json:"node_id"`
				Kind      string `json:"kind"`
				Status    string `json:"status"`
				Output    string `json:"output,omitempty"`
				Skipped   bool   `json:"skipped"`
				Aborted   bool   `json:"aborted"`
				StartedAt string `json:"started_at,omitempty"`
				EndedAt   string `json:"ended_at,omitempty"`
			} `json:"nodes,omitempty"`
		}
		if err := json.Unmarshal([]byte(resultJSON), &out); err == nil && len(out.Nodes) > 0 {
			// 从 result 中的 status 更新计划状态
			switch out.Status {
			case "completed":
				plan.Status = PlanCompleted
				plan.Progress = 1.0
			case "aborted":
				plan.Status = PlanAborted
			default:
				plan.Status = PlanFailed
			}

			// 更新各节点状态
			for i := range plan.Nodes {
				for _, on := range out.Nodes {
					if plan.Nodes[i].ID == on.NodeID {
						plan.Nodes[i].Status = task_context.PlanNodeStatus(on.Status)
						plan.Nodes[i].Kind = mapKindForDisplay(on.Kind)
						if on.Output != "" {
							plan.Nodes[i].Output = on.Output
						}
						if on.Skipped {
							plan.Nodes[i].Status = NodeSkipped
						}
						if on.StartedAt != "" && on.EndedAt != "" {
							if start, err := time.Parse(time.RFC3339, on.StartedAt); err == nil {
								if end, err2 := time.Parse(time.RFC3339, on.EndedAt); err2 == nil {
									plan.Nodes[i].Elapsed = end.Sub(start).String()
								}
							}
						}
						break
					}
				}
			}

			// 重新计算进度
			done := 0
			for _, n := range plan.Nodes {
				if n.Status == NodeCompleted || n.Status == NodeSkipped {
					done++
				}
			}
			if len(plan.Nodes) > 0 {
				plan.Progress = float64(done) / float64(len(plan.Nodes))
			}
		}
	}

	// 提取失败节点 ID
	failedNodeID := extractFailedNodeID(errMsg)
	if failedNodeID != "" {
		for i := range plan.Nodes {
			if plan.Nodes[i].ID == failedNodeID {
				plan.Nodes[i].Status = NodeFailed
				break
			}
		}
	}

	// 创建 retry/skip/abort 交互
	interaction := &Interaction{
		ID:       fmt.Sprintf("plan-fail-%d", time.Now().UnixNano()),
		Kind:     "plan_retry",
		Title:    "节点执行失败",
		Question: fmt.Sprintf("节点 %s 执行失败：%s", failedNodeID, errMsg),
		Options: []InteractionOption{
			{ID: "replan", Label: "Replan", Description: "Load a reviewed recovery plan without executing it.", Style: "primary"},
			{ID: "retry", Label: "重试", Description: "重新执行整个工作流", Style: "warning"},
			{ID: "skip", Label: "跳过", Description: "修改工作流跳过失败节点再执行", Style: "secondary"},
			{ID: "abort", Label: "终止", Description: "终止当前工作流", Style: "danger"},
		},
		OpenedAt: time.Now(),
	}
	service.Core.Snapshot.Interaction = interaction
	return interaction
}

// replanRequestLocked 从权威快照提取最小的可用恢复上下文。要求调用方持有
// service.ViewMu。
func (service *Service) replanRequestLocked(failure, idempotencyKey string) dto.ReplanRequest {
	request := dto.ReplanRequest{
		SessionID:      service.Core.Snapshot.Session.ID,
		Failure:        failure,
		IdempotencyKey: idempotencyKey,
	}
	for index := len(service.Core.Snapshot.Conversation) - 1; index >= 0; index-- {
		message := service.Core.Snapshot.Conversation[index]
		if request.Objective == "" && message.Role == "user" {
			request.Objective = message.Content
		}
		if request.PreviousPlan == "" && message.Tool != nil && message.Tool.Name == "plan_load" {
			request.PreviousPlan = message.Tool.Arguments
		}
		if request.Objective != "" && request.PreviousPlan != "" {
			break
		}
	}
	if request.Objective == "" {
		request.Objective = "Recover the failed plan safely."
	}
	if plan := service.Core.Snapshot.Runtime.Plan; plan != nil {
		var evidence strings.Builder
		for _, node := range plan.Nodes {
			if node.Status != NodeCompleted && node.Status != NodeSkipped && node.Status != NodeFailed {
				continue
			}
			fmt.Fprintf(&evidence, "node=%s status=%s", node.ID, node.Status)
			if node.Output != "" {
				fmt.Fprintf(&evidence, " output=%q", node.Output)
			}
			evidence.WriteByte('\n')
		}
		request.Evidence = evidence.String()
	}
	if state := service.components.tasks.CurrentTaskExecution(); state != nil {
		if checkpointEvidence := state.EvidenceText(); checkpointEvidence != "" {
			request.Evidence += "checkpoint evidence:\n" + checkpointEvidence
		}
	}
	if limit := Limits().ReplanEvidenceBytes; limit > 0 && len(request.Evidence) > limit {
		request.Evidence = request.Evidence[:limit] + "\n[evidence truncated]"
	}
	return request
}

// replanFailedWork 替换失败的 Plan 但不执行它：保留用户在恢复规划与任何新
// 副作用之间的复核点。
func (service *Service) replanFailedWork(ctx context.Context, interactionID, failure string) (resultErr error) {
	service.ViewMu.Lock()
	if service.components.tasks.ReplanInFlight(interactionID) {
		service.ViewMu.Unlock()
		return fmt.Errorf("replan: duplicate interaction %q is already in progress", interactionID)
	}
	planAttempts := 0
	if plan := service.Core.Snapshot.Runtime.Plan; plan != nil {
		planAttempts = plan.ReplanCount
		if planAttempts >= Limits().MaxReplansPerPlanChain {
			service.ViewMu.Unlock()
			return fmt.Errorf("replan: plan recovery limit of %d reached", Limits().MaxReplansPerPlanChain)
		}
	}
	service.components.tasks.MarkReplanInFlight(interactionID)
	request := service.replanRequestLocked(failure, interactionID)
	requestID := service.Core.Snapshot.Chat.RequestID
	service.ViewMu.Unlock()
	succeeded := false
	defer func() {
		if succeeded {
			return
		}
		runtimeProjection := service.collectRuntimeProjection(context.Background())
		service.ViewMu.Lock()
		service.components.tasks.DeleteReplanInFlight(interactionID)
		service.applyRuntimeProjectionLocked(runtimeProjection)
		revision := service.bumpLocked()
		runtime := cloneRuntimeState(service.Core.Snapshot.Runtime)
		sessionID := service.Core.Snapshot.Session.ID
		service.ViewMu.Unlock()
		service.publishSessionEvent(EventRuntimeChanged, revision, requestID, sessionID, runtime)
	}()

	result, err := service.Deps.Runtime.PrepareReplan(ctx, request)
	if err != nil {
		return fmt.Errorf("replan: %w", err)
	}
	if result.Arguments == "" {
		return fmt.Errorf("replan: runtime returned no plan_load arguments")
	}
	toolID := fmt.Sprintf("%s:plan-replan-%d", requestID, time.Now().UnixNano())
	service.handleToolStart(ctx, "plan_load", toolID, result.Arguments)
	service.handleToolComplete("plan_load", toolID, result.Result, nil, 0)
	runtimeProjection := service.collectRuntimeProjection(context.Background())
	service.ViewMu.Lock()
	if plan := service.Core.Snapshot.Runtime.Plan; plan != nil {
		plan.ReplanCount = planAttempts + 1
	}
	service.applyRuntimeProjectionLocked(runtimeProjection)
	revision := service.bumpLocked()
	runtime := cloneRuntimeState(service.Core.Snapshot.Runtime)
	sessionID := service.Core.Snapshot.Session.ID
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventRuntimeChanged, revision, requestID, sessionID, runtime)
	service.addNotice("Recovery plan loaded. Review it before calling plan_run.")
	succeeded = true
	return nil
}

func planRunFailure(resultJSON string) string {
	var result struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal([]byte(resultJSON), &result); err != nil || result.Status != "failed" {
		return ""
	}
	if result.Error != "" {
		return result.Error
	}
	return "plan_run failed"
}

// extractFailedNodeID 从 scheduler 错误消息中提取失败节点的 ID。
// 错误格式: node "X": reason
func extractFailedNodeID(errMsg string) string {
	if strings.Contains(errMsg, `node "`) {
		start := strings.Index(errMsg, `node "`) + len(`node "`)
		end := strings.Index(errMsg[start:], `"`)
		if end > 0 {
			return errMsg[start : start+end]
		}
	}
	return ""
}
