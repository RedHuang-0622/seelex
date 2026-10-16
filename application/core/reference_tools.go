package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/backend/engine/session"
	"github.com/RedHuang-0622/seelex/backend/engine/task"
	"github.com/RedHuang-0622/seelex/backend/tool/reference"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// 引用工具分页默认值收编进 seele.yaml limits 段
// （reference_page_size / max_reference_page_size，默认 4000 / 12000）。
// ReadToolResultHandler 是 read_tool_result 工具的 handler（模型面；
// 返回 JSON 字符串）。读取逻辑与 GUI 的 Service.ToolResultContent 共用
// toolResultContent，保证两条通道读到同一份归档。
func (service *Service) ReadToolResultHandler(_ context.Context, argsJSON string) (string, error) {
	var input struct {
		ResultRef string `json:"result_ref"`
		Offset    int    `json:"offset,omitempty"`
		Limit     int    `json:"limit,omitempty"`
		Contains  string `json:"contains,omitempty"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("read_tool_result: invalid JSON: %w", err)
	}
	input.ResultRef = strings.TrimSpace(input.ResultRef)
	if input.ResultRef == "" || input.Offset < 0 {
		return "", errors.New("read_tool_result: result_ref is required and offset must be non-negative")
	}
	if input.Limit <= 0 {
		input.Limit = Limits().ReferencePageSize
	}
	if max := Limits().MaxReferencePageSize; max > 0 && input.Limit > max {
		input.Limit = max
	}
	page, err := service.toolResultContent(input.ResultRef, input.Offset, input.Limit, input.Contains)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(page)
	return string(encoded), err
}

// ToolResultContent 按 result_ref 分页读回完整工具输出（GUI 面：快照被
// 截断的工具输出，前端"加载完整输出"调用；复用 read_tool_result 的持久
// 化通道，offset/limit 语义一致）。
func (service *Service) ToolResultContent(_ context.Context, resultRef string, offset, limit int) (model.ToolResultPage, error) {
	resultRef = strings.TrimSpace(resultRef)
	if resultRef == "" || offset < 0 {
		return model.ToolResultPage{}, errors.New("tool_result_content: result_ref is required and offset must be non-negative")
	}
	if limit <= 0 {
		limit = Limits().ReferencePageSize
	}
	if max := Limits().MaxReferencePageSize; max > 0 && limit > max {
		limit = max
	}
	return service.toolResultContent(resultRef, offset, limit, "")
}

// toolResultContent 解析 result_ref 并分页读回工具输出（含 contains 过滤
// 的模型面参数）。node:<nodeID>: 前缀走引擎桥读子代理归档；result:call_
// 别名映射回真实 tr- ref；其余查 pending（内存态）→ 会话落盘存储。
func (service *Service) toolResultContent(resultRef string, offset, limit int, contains string) (model.ToolResultPage, error) {
	// node:<nodeID>: 前缀 = 子代理工具结果：经引擎桥读回节点专属归档
	// （P1 修复——子代理 ref 主会话原本读不到；ref 前缀由节点归档器写入）。
	if nodeID, ok := reference.NodeResultRef(resultRef); ok {
		service.ViewMu.RLock()
		raw, found := service.nodeToolResult(nodeID, resultRef)
		service.ViewMu.RUnlock()
		if !found {
			return model.ToolResultPage{}, errors.New("read_tool_result: node result_ref is not available (node finished or ref unknown)")
		}
		result := StoredToolResult{ToolResultRef: model.ToolResultRef{Ref: resultRef}, Content: raw}
		return reference.BuildPage(result, offset, limit, contains), nil
	}

	// result:call_<callID> 别名：模型在省略占位提示后常自行拼接
	// result:call_...，而系统归档的真实 ref 是 tr-<digest>。按工具调用 ID
	// 映射回真实 ref 再读取，避免「result_ref is not available」假阴性
	// （2026-08-10：GUI 会话记录实证——fork 结果过大被省略后模型用
	// result:call_... 读回失败）。
	resultRef = service.resolveToolResultRefAlias(resultRef)

	service.ViewMu.RLock()
	if !service.hasToolResultRefLocked(resultRef) {
		service.ViewMu.RUnlock()
		return model.ToolResultPage{}, errors.New("read_tool_result: result_ref is not available in the current session")
	}
	for _, pending := range service.components.tasks.PendingToolResults() {
		if pending.Ref == resultRef {
			service.ViewMu.RUnlock()
			return reference.BuildPage(pending, offset, limit, contains), nil
		}
	}
	sessionID := service.Core.Snapshot.Session.ID
	workspaceID := session_runtime.WorkspaceID(service.Core.Snapshot.CurrentWorkspace)
	service.ViewMu.RUnlock()

	store, ok := service.Deps.Sessions.(session_runtime.SessionTranscriptPort)
	if !ok {
		return model.ToolResultPage{}, errors.New("read_tool_result: durable result storage is unavailable")
	}
	result, err := store.LoadToolResultWorkspace(workspaceID, sessionID, resultRef)
	if err != nil {
		return model.ToolResultPage{}, fmt.Errorf("read_tool_result: %w", err)
	}
	return reference.BuildPage(result, offset, limit, contains), nil
}

// nodeToolResult 读回子代理工具结果（引擎桥；Engine 未装配 → 不可用）。
func (service *Service) nodeToolResult(nodeID, ref string) (string, bool) {
	if service == nil || service.Deps.Engine == nil {
		return "", false
	}
	return service.Deps.Engine.NodeToolResult(nodeID, ref)
}

// hasToolResultRefLocked 判定该 ref 是否在当前会话的**内存态**归档里
// （调用方持 Core.ViewMu）。
func (service *Service) hasToolResultRefLocked(resultRef string) bool {
	for _, result := range service.components.tasks.ToolResultRefs() {
		if result.Ref == resultRef {
			return true
		}
	}
	return false
}

// resolveToolResultRefAlias 把模型常见的 result:call_<callID> 引用映射为
// 归档的真实 ref（tr-<digest>）。resultRefsByToolCallID 在工具结果过大被
// 省略时记录 callID → resultRef；模型若不使用占位里给出的 result_ref 而
// 自造 result:call_...，此映射保证仍能读回。非别名格式原样返回。
func (service *Service) resolveToolResultRefAlias(ref string) string {
	const prefix = "result:"
	if !strings.HasPrefix(ref, prefix) {
		return ref
	}
	callID := strings.TrimPrefix(ref, prefix)
	if callID == "" {
		return ref
	}
	service.ViewMu.RLock()
	realRef := service.components.tasks.ToolResultRefByCallID(callID)
	service.ViewMu.RUnlock()
	if realRef == "" {
		return ref
	}
	return realRef
}

func (service *Service) ReadPlanHandler(_ context.Context, argsJSON string) (string, error) {
	var input struct {
		PlanRef string   `json:"plan_ref"`
		NodeIDs []string `json:"node_ids,omitempty"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("read_plan: invalid JSON: %w", err)
	}
	service.ViewMu.RLock()
	planRef := strings.TrimSpace(input.PlanRef)
	if planRef == "" {
		planRef = service.components.tasks.ActivePlanID()
	}
	frame := task_context.ActivePlanFrame(service.components.tasks.PlanStack(), planRef)
	if frame == nil {
		service.ViewMu.RUnlock()
		return "", errors.New("read_plan: plan_ref is not available in the current session")
	}
	arguments := frame.Arguments
	planState := cloneRuntimeState(RuntimeState{Plan: frame.Plan}).Plan
	service.ViewMu.RUnlock()

	var canonical map[string]any
	if err := json.Unmarshal([]byte(arguments), &canonical); err != nil {
		return "", fmt.Errorf("read_plan: stored Plan is invalid: %w", err)
	}
	if len(input.NodeIDs) > 0 {
		nodes, _ := canonical["nodes"].(map[string]any)
		selected := make(map[string]any, len(input.NodeIDs))
		for _, nodeID := range input.NodeIDs {
			if node, ok := nodes[nodeID]; ok {
				selected[nodeID] = node
			}
		}
		canonical["nodes"] = selected
	}
	payload := map[string]any{"plan_ref": planRef, "canonical": canonical, "state": planState}
	encoded, err := json.Marshal(payload)
	return string(encoded), err
}
