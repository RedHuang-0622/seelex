package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/Seele/engine"
	"github.com/RedHuang-0622/Seele/types"
	workplanTypes "github.com/RedHuang-0622/Seele/workplan/core/types"

	"github.com/RedHuang-0622/seelex/seelebridge"
)

func (service *Service) startChat(parent context.Context, request chatRequest) error {
	service.mu.Lock()
	if service.closed {
		service.mu.Unlock()
		return fmt.Errorf("application is shut down")
	}
	if service.draining {
		service.mu.Unlock()
		return ErrApplicationDraining
	}
	if service.snapshot.Chat.Running {
		service.mu.Unlock()
		return ErrChatRunning
	}
	requestID := fmt.Sprintf("chat-%d", time.Now().UnixNano())
	chatContext, cancel := context.WithCancel(parent)
	service.cancelChat = cancel
	service.markBusyLocked()
	service.snapshot.Chat = ChatState{Running: true, RequestID: requestID, StartedAt: time.Now()}
	if service.snapshot.Session.Name == "" {
		service.snapshot.Session.Name = sessionTitle(request.displayInput)
	}
	user := *service.appendMessageLocked("user", request.displayInput, nil)
	assistant := *service.appendMessageLocked("assistant", "", nil)
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventMessageAdded, revision, requestID, user)
	service.events.Publish(EventMessageAdded, revision, requestID, assistant)
	go service.runChat(chatContext, requestID, request)
	return nil
}

func (service *Service) runChat(ctx context.Context, requestID string, request chatRequest) {
	err := service.runPlanPreflight(ctx, requestID, request)
	if err == nil {
		_, err = service.deps.Engine.ChatStream(ctx, request.modelInput, func(chunk string) { service.appendDelta(requestID, chunk) })
	}
	if err == nil {
		if saveErr := service.deps.Sessions.SaveCurrent(service.deps.Engine.SessionID()); saveErr != nil {
			err = fmt.Errorf("保存会话失败: %w", saveErr)
		}
	}
	service.mu.Lock()
	if service.snapshot.Chat.RequestID != requestID {
		service.mu.Unlock()
		return
	}
	service.snapshot.Chat.Error = ""
	if err != nil {
		service.snapshot.Chat.Error = err.Error()
		service.appendMessageLocked("error", err.Error(), nil)
	}
	// 不在此处从 Engine.History() 重建 conversation——增量构建已在
	// startChat/handleToolStart/handleToolComplete/appendDelta 中完成，
	// 全量重建可能带入跨会话的残留消息。
	service.refreshRuntimeLocked(context.Background())
	// 处理输入队列：取所有排队输入合并为一条，批量发送
	processQueue := len(service.inputQueue) > 0
	var batchRequest chatRequest
	var nextContext context.Context
	nextRequestID := ""
	var nextUser, nextAssistant *Message
	if processQueue {
		// UI 展示原始输入，模型输入使用每次 Submit 时固化的 Skill 上下文。
		batchRequest = combineChatRequests(service.inputQueue)
		service.inputQueue = nil
		service.snapshot.Chat.QueuedCount = 0
		service.snapshot.Chat.InputQueue = nil
		nextRequestID = fmt.Sprintf("chat-%d", time.Now().UnixNano())
		nextContext, service.cancelChat = context.WithCancel(context.Background())
		service.snapshot.Chat = ChatState{Running: true, RequestID: nextRequestID, StartedAt: time.Now()}
		nextUser = service.appendMessageLocked("user", batchRequest.displayInput, nil)
		nextAssistant = service.appendMessageLocked("assistant", "", nil)
	} else {
		service.snapshot.Chat.Running = false
		service.cancelChat = nil
		service.markIdleLocked()
	}
	revision := service.bumpLocked()
	service.mu.Unlock()
	if err != nil {
		service.events.Publish(EventError, revision, requestID, map[string]string{"message": err.Error()})
	} else {
		service.events.Publish(EventSnapshotChanged, revision, requestID, nil)
	}
	// 批量发送：所有排队消息一次发给 LLM
	if processQueue {
		service.events.Publish(EventMessageAdded, revision, nextRequestID, *nextUser)
		service.events.Publish(EventMessageAdded, revision, nextRequestID, *nextAssistant)
		go service.runChat(nextContext, nextRequestID, batchRequest)
	}
}

func (service *Service) runPlanPreflight(ctx context.Context, requestID string, request chatRequest) error {
	if !request.requirePlan {
		return nil
	}
	result, err := service.deps.Runtime.PreparePlan(ctx, request.displayInput)
	if result.Arguments != "" {
		toolID := requestID + ":plan-preflight"
		service.handleToolStart("plan_load", toolID, result.Arguments)
		service.handleToolComplete("plan_load", toolID, result.Result, err, 0)
	}
	if err != nil {
		return fmt.Errorf("plan preflight: %w", err)
	}
	return nil
}

func closedSignal() chan struct{} {
	idle := make(chan struct{})
	close(idle)
	return idle
}

func (service *Service) markBusyLocked() {
	select {
	case <-service.idle:
		service.idle = make(chan struct{})
	default:
	}
}

func (service *Service) markIdleLocked() {
	select {
	case <-service.idle:
	default:
		close(service.idle)
	}
}

func (service *Service) appendDelta(requestID, chunk string) {
	service.mu.Lock()
	if !service.snapshot.Chat.Running || service.snapshot.Chat.RequestID != requestID {
		service.mu.Unlock()
		return
	}
	messageID := ""
	for index := len(service.snapshot.Conversation) - 1; index >= 0; index-- {
		if service.snapshot.Conversation[index].Role == "assistant" && service.snapshot.Conversation[index].Tool == nil {
			service.snapshot.Conversation[index].Content += chunk
			messageID = service.snapshot.Conversation[index].ID
			break
		}
	}
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventMessageDelta, revision, requestID, MessageDelta{MessageID: messageID, Delta: chunk})
}

func (service *Service) appendHistoryLocked(history []EngineMessage) {
	for _, historyMessage := range history {
		if historyMessage.Role != "tool" && historyMessage.Content != "" {
			content := historyMessage.Content
			if historyMessage.Role == "user" {
				content = displayUserInput(content)
			}
			service.appendMessageLocked(historyMessage.Role, content, nil)
		}
		for _, call := range historyMessage.ToolCalls {
			service.appendMessageLocked("tool", "", &ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments, Status: "success"})
		}
		if historyMessage.Role == "tool" {
			service.appendMessageLocked("tool_result", historyMessage.Content, &ToolCall{ID: historyMessage.ToolCallID, Name: historyMessage.Name, Result: historyMessage.Content, Status: "success"})
		}
	}
}

func (service *Service) handleToolStart(name, id, arguments string) {
	service.mu.Lock()
	tool := &ToolCall{ID: id, Name: name, Arguments: arguments, Status: "running"}
	message := *service.appendMessageLocked("tool", "", tool)

	// plan_load 启动时：解析 DAG 并初始化 PlanState
	if name == "plan_load" {
		service.updatePlanFromLoad(arguments)
	}
	// plan_clear 启动时：清空 PlanState
	if name == "plan_clear" {
		service.snapshot.Runtime.Plan = nil
	}
	if name == "plan_run" {
		service.deps.Runtime.SetPlanBranchBinding(service.planBranchBindingLocked())
	}

	revision := service.bumpLocked()
	requestID := service.snapshot.Chat.RequestID
	runtime := cloneRuntimeState(service.snapshot.Runtime)
	service.mu.Unlock()
	service.events.Publish(EventToolStarted, revision, requestID, message)
	service.events.Publish(EventRuntimeChanged, revision, requestID, runtime)
}

func (service *Service) planBranchBindingLocked() seelebridge.PlanBranchBinding {
	binding := seelebridge.PlanBranchBinding{
		SessionID: service.snapshot.Session.ID,
		AccountID: service.snapshot.Runtime.Account,
		TraceID:   service.snapshot.Chat.RequestID,
	}
	if workspace := service.snapshot.CurrentWorkspace; workspace != nil {
		binding.WorkspaceID = workspace.ID
	}
	if plan := service.snapshot.Runtime.Plan; plan != nil {
		binding.PlanID = plan.EntryNodeID
		binding.EntryNodeID = plan.EntryNodeID
	}
	return binding
}

func (service *Service) handleToolComplete(name, id, result string, toolErr error, duration time.Duration) {
	service.mu.Lock()
	status, errorText := "success", ""
	if toolErr != nil {
		status, errorText = "error", toolErr.Error()
	}
	for index := len(service.snapshot.Conversation) - 1; index >= 0; index-- {
		tool := service.snapshot.Conversation[index].Tool
		if tool != nil && tool.ID == id {
			tool.Status, tool.Result, tool.Error, tool.Duration = status, result, errorText, duration
			break
		}
	}
	content := result
	if toolErr != nil {
		content = errorText
	}

	var planFailure *Interaction
	if name == "plan_run" {
		switch {
		case toolErr != nil:
			planFailure = service.handlePlanRunFailureLocked(toolErr.Error(), result)
		default:
			if failure := planRunFailure(result); failure != "" {
				planFailure = service.handlePlanRunFailureLocked(failure, result)
			} else {
				service.updatePlanFromRunResult(result)
			}
		}
	}

	message := *service.appendMessageLocked("tool_result", content, &ToolCall{ID: id, Name: name, Result: result, Error: errorText, Status: status, Duration: duration})
	// Only append empty assistant if the last message isn't already an empty assistant
	var assistant *Message
	if n := len(service.snapshot.Conversation); n == 0 || service.snapshot.Conversation[n-1].Role != "assistant" || service.snapshot.Conversation[n-1].Content != "" || service.snapshot.Conversation[n-1].Tool != nil {
		appended := *service.appendMessageLocked("assistant", "", nil)
		assistant = &appended
	}
	service.refreshRuntimeLocked(context.Background())
	revision := service.bumpLocked()
	requestID := service.snapshot.Chat.RequestID
	runtime := cloneRuntimeState(service.snapshot.Runtime)
	service.mu.Unlock()
	service.events.Publish(EventToolCompleted, revision, requestID, message)
	if planFailure != nil {
		service.events.Publish(EventInteractionOpened, revision, planFailure.ID, planFailure)
	}
	if assistant != nil {
		service.events.Publish(EventMessageAdded, revision, requestID, *assistant)
	}
	service.events.Publish(EventRuntimeChanged, revision, requestID, runtime)
}

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
	if _, ok := input.Nodes[input.Entry]; !ok || seelebridge.DetectCycle(input.Edges) != nil {
		return
	}

	// 构建所有节点集合，供 TopoSort 使用
	allNodes := make(map[string]struct{}, len(input.Nodes))
	for id := range input.Nodes {
		allNodes[id] = struct{}{}
	}

	// 拓扑排序 → 稳定节点顺序
	order := seelebridge.TopoSort(input.Entry, input.Edges, allNodes)

	nodes := make([]PlanNode, 0, len(input.Nodes))
	for _, id := range order {
		spec := input.Nodes[id]
		kind := spec.Kind
		if kind == "" {
			kind = "auto"
		}
		nodes = append(nodes, PlanNode{ID: id, Label: id, Kind: kind, Status: NodePending})
	}

	// 邻接表 → []PlanEdge
	planEdges := seelebridge.AdjacencyToEdges(input.Edges)

	service.snapshot.Runtime.Plan = &PlanState{
		Name:        input.Entry,
		EntryNodeID: input.Entry,
		Status:      PlanPending,
		Nodes:       nodes,
		Edges:       planEdges,
	}
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
	if service.snapshot.Runtime.Plan == nil {
		service.snapshot.Runtime.Plan = &PlanState{}
	}
	plan := service.snapshot.Runtime.Plan

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
					plan.Nodes[i].Status = PlanNodeStatus(on.Status)
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
			return PlanNodeStatus(n.Status)
		}
	}
	return NodePending
}

// HandlePlanNodeComplete 由 plan_run 的 ProgressCallback 调用，
// 实时更新单节点状态并通知 TUI/GUI 重绘。
// 直接接收框架原生 *types.NodeResult，零适配开销。
func (service *Service) HandlePlanNodeComplete(nr *workplanTypes.NodeResult) {
	service.mu.Lock()
	plan := service.snapshot.Runtime.Plan
	if plan == nil {
		service.mu.Unlock()
		return
	}
	for i := range plan.Nodes {
		if plan.Nodes[i].ID == nr.NodeID {
			plan.Nodes[i].Status = PlanNodeStatus(nr.Status)
			plan.Nodes[i].Elapsed = nr.Elapsed().String()
			plan.Nodes[i].Kind = mapKindForDisplay(nr.Kind)
			if nr.Output != "" {
				plan.Nodes[i].Output = nr.Output
			}
			break
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
	revision := service.bumpLocked()
	requestID := service.snapshot.Chat.RequestID
	service.mu.Unlock()
	service.events.Publish(EventSnapshotChanged, revision, requestID, nil)
}

// HandlePlanBranchEvent applies a branch lifecycle transition received from
// the bridge and publishes the updated runtime snapshot for both frontends.
func (service *Service) HandlePlanBranchEvent(event seelebridge.PlanBranchEvent) {
	service.mu.Lock()
	plan := service.snapshot.Runtime.Plan
	if plan == nil {
		service.mu.Unlock()
		return
	}
	for index := range plan.Nodes {
		if plan.Nodes[index].ID != event.NodeID {
			continue
		}
		plan.Nodes[index].Status = PlanNodeStatus(event.Type)
		break
	}
	switch event.Type {
	case "queued", "started":
		if plan.Status == PlanPending {
			plan.Status = PlanRunning
		}
	case "failed", "panicked":
		plan.Status = PlanFailed
	}
	recalculatePlanProgress(plan)
	revision := service.bumpLocked()
	requestID := service.snapshot.Chat.RequestID
	runtime := cloneRuntimeState(service.snapshot.Runtime)
	service.mu.Unlock()
	service.events.Publish(EventRuntimeChanged, revision, requestID, runtime)
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

// PlanNodeStatus 将字符串转为 NodeStatus。
func PlanNodeStatus(s string) NodeStatus {
	switch s {
	case "queued":
		return NodeQueued
	case "running", "started":
		return NodeRunning
	case "completed":
		return NodeCompleted
	case "failed":
		return NodeFailed
	case "aborted":
		return NodeAborted
	case "skipped":
		return NodeSkipped
	case "canceled":
		return NodeCanceled
	case "panicked":
		return NodePanicked
	default:
		return NodePending
	}
}

func recalculatePlanProgress(plan *PlanState) {
	done := 0
	for _, node := range plan.Nodes {
		if node.Status == NodeCompleted || node.Status == NodeSkipped {
			done++
		}
	}
	if len(plan.Nodes) > 0 {
		plan.Progress = float64(done) / float64(len(plan.Nodes))
	}
}

// handlePlanRunFailure 处理 plan_run 执行失败的情况。
// 更新 PlanState 中失败节点的状态，弹出 retry/skip/abort 交互。
func (service *Service) handlePlanRunFailureLocked(errMsg, resultJSON string) *Interaction {
	plan := service.snapshot.Runtime.Plan
	if plan == nil {
		return nil
	}

	// 更新计划整体状态
	plan.Status = PlanFailed

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
						plan.Nodes[i].Status = PlanNodeStatus(on.Status)
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
	service.snapshot.Interaction = interaction
	return interaction
}

const maxReplanEvidenceBytes = 12 * 1024

// replanRequestLocked extracts the smallest useful recovery context from the
// authoritative snapshot. It must be called with service.mu held.
func (service *Service) replanRequestLocked(failure, idempotencyKey string) seelebridge.ReplanRequest {
	request := seelebridge.ReplanRequest{Failure: failure, IdempotencyKey: idempotencyKey}
	for index := len(service.snapshot.Conversation) - 1; index >= 0; index-- {
		message := service.snapshot.Conversation[index]
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
	if plan := service.snapshot.Runtime.Plan; plan != nil {
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
	if len(request.Evidence) > maxReplanEvidenceBytes {
		request.Evidence = request.Evidence[:maxReplanEvidenceBytes] + "\n[evidence truncated]"
	}
	return request
}

// replanFailedWork replaces a failed plan without running it. This preserves
// the user's review point between recovery planning and any new side effect.
func (service *Service) replanFailedWork(ctx context.Context, interactionID, failure string) (resultErr error) {
	service.mu.Lock()
	if _, exists := service.replanInFlight[interactionID]; exists {
		service.mu.Unlock()
		return fmt.Errorf("replan: duplicate interaction %q is already in progress", interactionID)
	}
	planAttempts := 0
	if plan := service.snapshot.Runtime.Plan; plan != nil {
		planAttempts = plan.ReplanCount
		if planAttempts >= maxReplansPerPlanChain {
			service.mu.Unlock()
			return fmt.Errorf("replan: plan recovery limit of %d reached", maxReplansPerPlanChain)
		}
	}
	service.replanInFlight[interactionID] = struct{}{}
	request := service.replanRequestLocked(failure, interactionID)
	requestID := service.snapshot.Chat.RequestID
	service.mu.Unlock()
	succeeded := false
	defer func() {
		if succeeded {
			return
		}
		service.mu.Lock()
		delete(service.replanInFlight, interactionID)
		service.refreshRuntimeLocked(context.Background())
		revision := service.bumpLocked()
		runtime := cloneRuntimeState(service.snapshot.Runtime)
		service.mu.Unlock()
		service.events.Publish(EventRuntimeChanged, revision, requestID, runtime)
	}()

	result, err := service.deps.Runtime.PrepareReplan(ctx, request)
	if err != nil {
		return fmt.Errorf("replan: %w", err)
	}
	if result.Arguments == "" {
		return fmt.Errorf("replan: runtime returned no plan_load arguments")
	}
	toolID := fmt.Sprintf("%s:plan-replan-%d", requestID, time.Now().UnixNano())
	service.handleToolStart("plan_load", toolID, result.Arguments)
	service.handleToolComplete("plan_load", toolID, result.Result, nil, 0)
	service.mu.Lock()
	if plan := service.snapshot.Runtime.Plan; plan != nil {
		plan.ReplanCount = planAttempts + 1
	}
	service.refreshRuntimeLocked(context.Background())
	revision := service.bumpLocked()
	runtime := cloneRuntimeState(service.snapshot.Runtime)
	service.mu.Unlock()
	service.events.Publish(EventRuntimeChanged, revision, requestID, runtime)
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

type ToolHookBridge struct {
	mu      sync.Mutex
	service *Service
	toolSeq uint64
	pending map[string][]string
}

func NewToolHookBridge() *ToolHookBridge { return &ToolHookBridge{} }
func (bridge *ToolHookBridge) Bind(service *Service) {
	bridge.mu.Lock()
	bridge.service = service
	bridge.mu.Unlock()
}
func (bridge *ToolHookBridge) Hooks() *engine.LoopHooks {
	return &engine.LoopHooks{
		OnToolStart: func(_ context.Context, info engine.ToolCallInfo) {
			service, id := bridge.beginTool(info)
			if service != nil {
				service.handleToolStart(info.Name, id, info.Arguments)
			}
		},
		OnToolComplete: func(_ context.Context, info engine.ToolCallInfo) {
			service, id := bridge.completeTool(info)
			if service != nil {
				service.handleToolComplete(info.Name, id, info.Result, info.Error, info.Duration)
			}
		},
		OnIterationComplete: func(_ context.Context, turn int) bool {
			bridge.mu.Lock()
			svc := bridge.service
			bridge.mu.Unlock()
			if svc == nil {
				return true
			}
			// 每轮 ReAct 结束后检查输入队列：非空时清空并注入到引擎对话历史，
			// 下一轮 LLM 调用将看到这些排队消息，无需停止 loop。
			svc.mu.Lock()
			if len(svc.inputQueue) == 0 {
				svc.mu.Unlock()
				return true
			}
			batch := combineChatRequests(svc.inputQueue)
			svc.inputQueue = nil
			svc.snapshot.Chat.QueuedCount = 0
			svc.snapshot.Chat.InputQueue = nil

			// 追加到引擎内部历史（同 goroutine，无需加锁）
			batchInput := batch.modelInput
			svc.mu.Unlock()
			svc.deps.Engine.AppendHistory(types.Message{Role: "user", Content: &batchInput})

			// 追加到 snapshot conversation → UI 即时展示
			svc.mu.Lock()
			svc.appendMessageLocked("user", batch.displayInput, nil)
			svc.appendMessageLocked("assistant", "", nil) // 占位，下一轮 fill
			revision := svc.bumpLocked()
			requestID := svc.snapshot.Chat.RequestID
			svc.mu.Unlock()
			svc.events.Publish(EventSnapshotChanged, revision, requestID, nil)
			return true // 继续 loop，下一轮 LLM 调用将处理排队输入
		},
	}
}

func (bridge *ToolHookBridge) beginTool(info engine.ToolCallInfo) (*Service, string) {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	id := bridge.nextToolIDLocked()
	if bridge.pending == nil {
		bridge.pending = make(map[string][]string)
	}
	key := toolHookKey(info)
	bridge.pending[key] = append(bridge.pending[key], id)
	return bridge.service, id
}

func (bridge *ToolHookBridge) completeTool(info engine.ToolCallInfo) (*Service, string) {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	key := toolHookKey(info)
	ids := bridge.pending[key]
	if len(ids) == 0 {
		return bridge.service, bridge.nextToolIDLocked()
	}
	id := ids[0]
	if len(ids) == 1 {
		delete(bridge.pending, key)
	} else {
		bridge.pending[key] = ids[1:]
	}
	return bridge.service, id
}

func (bridge *ToolHookBridge) nextToolIDLocked() string {
	bridge.toolSeq++
	return fmt.Sprintf("tool-%d", bridge.toolSeq)
}

func toolHookKey(info engine.ToolCallInfo) string {
	return fmt.Sprintf("%d\x00%s\x00%s", info.Turn, info.Name, info.Arguments)
}
