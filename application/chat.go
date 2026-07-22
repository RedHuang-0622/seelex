package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/Seele/engine"
)

func (service *Service) startChat(parent context.Context, input string) error {
	service.mu.Lock()
	if service.closed {
		service.mu.Unlock()
		return fmt.Errorf("application is shut down")
	}
	if service.snapshot.Chat.Running {
		service.mu.Unlock()
		return ErrChatRunning
	}
	requestID := fmt.Sprintf("chat-%d", time.Now().UnixNano())
	chatContext, cancel := context.WithCancel(parent)
	service.cancelChat = cancel
	service.snapshot.Chat = ChatState{Running: true, RequestID: requestID, StartedAt: time.Now()}
	user := *service.appendMessageLocked("user", input, nil)
	assistant := *service.appendMessageLocked("assistant", "", nil)
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventMessageAdded, revision, requestID, user)
	service.events.Publish(EventMessageAdded, revision, requestID, assistant)
	go service.runChat(chatContext, requestID, input)
	return nil
}

func (service *Service) runChat(ctx context.Context, requestID, input string) {
	_, err := service.deps.Engine.ChatStream(ctx, input, func(chunk string) { service.appendDelta(requestID, chunk) })
	service.mu.Lock()
	if service.snapshot.Chat.RequestID != requestID {
		service.mu.Unlock()
		return
	}
	service.snapshot.Chat.Running = false
	service.snapshot.Chat.Error = ""
	service.cancelChat = nil
	if err != nil {
		service.snapshot.Chat.Error = err.Error()
		service.appendMessageLocked("error", err.Error(), nil)
	} else {
		service.snapshot.Conversation = nil
		service.appendMessageLocked("system", fmt.Sprintf("Seele CLI — %s", service.deps.Runtime.Model()), nil)
		service.appendHistoryLocked(service.deps.Engine.History())
	}
	service.refreshRuntimeLocked(context.Background())
	// 处理输入队列：取所有排队输入合并为一条，批量发送
	processQueue := len(service.inputQueue) > 0
	var batchInput string
	if processQueue {
		// 把所有排队消息显示到对话框
		for _, q := range service.inputQueue {
			service.appendMessageLocked("user", q, nil)
		}
		// 合并为一条 LLM 输入
		batchInput = strings.Join(service.inputQueue, "\n---\n")
		service.inputQueue = nil
		service.snapshot.Chat.QueuedCount = 0
		service.snapshot.Chat.InputQueue = nil
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
		go service.startChat(context.Background(), batchInput)
	}
}

func (service *Service) appendDelta(requestID, chunk string) {
	service.mu.Lock()
	if !service.snapshot.Chat.Running || service.snapshot.Chat.RequestID != requestID {
		service.mu.Unlock()
		return
	}
	for index := len(service.snapshot.Conversation) - 1; index >= 0; index-- {
		if service.snapshot.Conversation[index].Role == "assistant" && service.snapshot.Conversation[index].Tool == nil {
			service.snapshot.Conversation[index].Content += chunk
			break
		}
	}
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventMessageDelta, revision, requestID, map[string]string{"delta": chunk})
}

func (service *Service) appendHistoryLocked(history []EngineMessage) {
	for _, historyMessage := range history {
		if historyMessage.Role != "tool" && historyMessage.Content != "" {
			service.appendMessageLocked(historyMessage.Role, historyMessage.Content, nil)
		}
		for _, call := range historyMessage.ToolCalls {
			service.appendMessageLocked("tool", "", &ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments, Status: "success"})
		}
		if historyMessage.Role == "tool" {
			service.appendMessageLocked("tool_result", historyMessage.Content, &ToolCall{Name: historyMessage.Name, Result: historyMessage.Content, Status: "success"})
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

	revision := service.bumpLocked()
	requestID := service.snapshot.Chat.RequestID
	service.mu.Unlock()
	service.events.Publish(EventToolStarted, revision, requestID, message)
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

	// plan_run 完成时：解析结果更新 PlanState
	if name == "plan_run" && toolErr == nil {
		service.updatePlanFromRunResult(result)
	}

	message := *service.appendMessageLocked("tool_result", content, &ToolCall{ID: id, Name: name, Result: result, Error: errorText, Status: status, Duration: duration})
	// Only append empty assistant if the last message isn't already an empty assistant
	if n := len(service.snapshot.Conversation); n == 0 || service.snapshot.Conversation[n-1].Role != "assistant" || service.snapshot.Conversation[n-1].Content != "" || service.snapshot.Conversation[n-1].Tool != nil {
		service.appendMessageLocked("assistant", "", nil)
	}
	service.refreshRuntimeLocked(context.Background())
	revision := service.bumpLocked()
	requestID := service.snapshot.Chat.RequestID
	service.mu.Unlock()
	service.events.Publish(EventToolCompleted, revision, requestID, message)
}

// updatePlanFromLoad 从 plan_load 的参数 JSON 初始化 PlanState。
func (service *Service) updatePlanFromLoad(argsJSON string) {
	type planNodeSpec struct {
		Input string `json:"input"`
	}
	var input struct {
		Entry string                   `json:"entry"`
		Nodes map[string]planNodeSpec  `json:"nodes"`
		Edges map[string][]string      `json:"edges"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil || len(input.Nodes) == 0 {
		return
	}
	nodes := make([]PlanNode, 0, len(input.Nodes))
	for id := range input.Nodes {
		label := id
		nodes = append(nodes, PlanNode{ID: id, Label: label, Status: NodePending})
	}
	service.snapshot.Runtime.Plan = &PlanState{
		Name:   input.Entry,
		Status: PlanPending,
		Nodes:  nodes,
	}
}

// updatePlanFromRunResult 从 plan_run 返回的 JSON 更新 PlanState。
func (service *Service) updatePlanFromRunResult(resultJSON string) {
	var out struct {
		Status       string `json:"status"`
		NodeCount    int    `json:"node_count"`
		FinalOutput  string `json:"final_output"`
		AbortReason  string `json:"abort_reason,omitempty"`
		// 扩展字段：若框架 plan_run 返回了 per-node 结果
		Nodes []struct {
			NodeID    string `json:"node_id"`
			Kind      string `json:"kind"`
			Status    string `json:"status"`
			Elapsed   string `json:"elapsed,omitempty"`
			Skipped   bool   `json:"skipped"`
			Aborted   bool   `json:"aborted"`
			Err       string `json:"err,omitempty"`
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
					plan.Nodes[i].Elapsed = on.Elapsed
					plan.Nodes[i].Kind = on.Kind
					if on.Skipped {
						plan.Nodes[i].Status = NodeSkipped
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
}

// resolveNodeStatus 辅助：从框架返回的 nodes 列表中查找 nodeID 的状态。
func resolveNodeStatus(nodes []struct {
	NodeID    string `json:"node_id"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	Elapsed   string `json:"elapsed,omitempty"`
	Skipped   bool   `json:"skipped"`
	Aborted   bool   `json:"aborted"`
	Err       string `json:"err,omitempty"`
}, nodeID string) NodeStatus {
	for _, n := range nodes {
		if n.NodeID == nodeID {
			return PlanNodeStatus(n.Status)
		}
	}
	return NodePending
}

// HandlePlanNodeComplete 由 plan_run 的 ProgressCallback 调用，
// 实时更新单节点状态并通知 TUI 重绘。
// 此方法设计为从 ToolHookBridge 之外的回调链路调用。
func (service *Service) HandlePlanNodeComplete(nodeID, kind, status string, elapsed time.Duration) {
	service.mu.Lock()
	plan := service.snapshot.Runtime.Plan
	if plan == nil {
		service.mu.Unlock()
		return
	}
	for i := range plan.Nodes {
		if plan.Nodes[i].ID == nodeID {
			plan.Nodes[i].Status = PlanNodeStatus(status)
			plan.Nodes[i].Elapsed = elapsed.String()
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

// PlanNodeStatus 将字符串转为 NodeStatus。
func PlanNodeStatus(s string) NodeStatus {
	switch s {
	case "running":
		return NodeRunning
	case "completed":
		return NodeCompleted
	case "failed":
		return NodeFailed
	case "aborted":
		return NodeAborted
	case "skipped":
		return NodeSkipped
	default:
		return NodePending
	}
}

type ToolHookBridge struct {
	mu      sync.RWMutex
	service *Service
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
			bridge.mu.RLock()
			service := bridge.service
			bridge.mu.RUnlock()
			if service != nil {
				service.handleToolStart(info.Name, fmt.Sprintf("%s-%d", info.Name, info.Turn), info.Arguments)
			}
		},
		OnToolComplete: func(_ context.Context, info engine.ToolCallInfo) {
			bridge.mu.RLock()
			service := bridge.service
			bridge.mu.RUnlock()
			if service != nil {
				service.handleToolComplete(info.Name, fmt.Sprintf("%s-%d", info.Name, info.Turn), info.Result, info.Error, info.Duration)
			}
		},
	}
}
