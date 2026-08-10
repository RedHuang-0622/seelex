package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/RedHuang-0622/seelex/application/model"
)

// 引用工具分页默认值收编进 seele.yaml limits 段
// （reference_page_size / max_reference_page_size，默认 4000 / 12000）。
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

	// node:<nodeID>: 前缀 = 子代理工具结果：经引擎桥读回节点专属归档
	// （P1 修复——子代理 ref 主会话原本读不到；ref 前缀由节点归档器写入）。
	if nodeID, ok := nodeResultRef(input.ResultRef); ok {
		service.mu.RLock()
		raw, found := service.nodeToolResult(nodeID, input.ResultRef)
		service.mu.RUnlock()
		if !found {
			return "", errors.New("read_tool_result: node result_ref is not available (node finished or ref unknown)")
		}
		result := StoredToolResult{ToolResultRef: model.ToolResultRef{Ref: input.ResultRef}, Content: raw}
		return encodeToolResultPage(result, input.Offset, input.Limit, input.Contains)
	}

	// result:call_<callID> 别名：模型在省略占位提示后常自行拼接
	// result:call_...，而系统归档的真实 ref 是 tr-<digest>。按工具调用 ID
	// 映射回真实 ref 再读取，避免「result_ref is not available」假阴性
	// （2026-08-10：GUI 会话记录实证——fork 结果过大被省略后模型用
	// result:call_... 读回失败）。
	input.ResultRef = service.resolveToolResultRefAlias(input.ResultRef)

	service.mu.RLock()
	if !service.hasToolResultRefLocked(input.ResultRef) {
		service.mu.RUnlock()
		return "", errors.New("read_tool_result: result_ref is not available in the current session")
	}
	for _, pending := range service.pendingToolResults {
		if pending.Ref == input.ResultRef {
			service.mu.RUnlock()
			return encodeToolResultPage(pending, input.Offset, input.Limit, input.Contains)
		}
	}
	sessionID := service.snapshot.Session.ID
	workspaceID := workspaceID(service.snapshot.CurrentWorkspace)
	service.mu.RUnlock()

	store, ok := service.deps.Sessions.(sessionTranscriptPort)
	if !ok {
		return "", errors.New("read_tool_result: durable result storage is unavailable")
	}
	result, err := store.LoadToolResultWorkspace(workspaceID, sessionID, input.ResultRef)
	if err != nil {
		return "", fmt.Errorf("read_tool_result: %w", err)
	}
	return encodeToolResultPage(result, input.Offset, input.Limit, input.Contains)
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
	service.mu.RLock()
	realRef := service.resultRefsByToolCallID[callID]
	service.mu.RUnlock()
	if realRef == "" {
		return ref
	}
	return realRef
}

// nodeResultRef 解析 node:<nodeID>: 前缀的子代理结果引用；非节点引用 → false。
func nodeResultRef(ref string) (string, bool) {
	const prefix = "node:"
	if !strings.HasPrefix(ref, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(ref, prefix)
	sep := strings.IndexByte(rest, ':')
	if sep <= 0 {
		return "", false
	}
	return rest[:sep], true
}

// nodeToolResult 读回子代理工具结果（引擎桥；Engine 未装配 → 不可用）。
func (service *Service) nodeToolResult(nodeID, ref string) (string, bool) {
	if service == nil || service.deps.Engine == nil {
		return "", false
	}
	return service.deps.Engine.NodeToolResult(nodeID, ref)
}

func (service *Service) hasToolResultRefLocked(resultRef string) bool {
	for _, result := range service.toolResultRefs {
		if result.Ref == resultRef {
			return true
		}
	}
	return false
}

func encodeToolResultPage(result StoredToolResult, offset, limit int, contains string) (string, error) {
	content := result.Content
	if contains = strings.TrimSpace(contains); contains != "" {
		lines := strings.Split(content, "\n")
		matched := lines[:0]
		for _, line := range lines {
			if strings.Contains(line, contains) {
				matched = append(matched, line)
			}
		}
		content = strings.Join(matched, "\n")
	}
	if offset > len(content) {
		return "", errors.New("read_tool_result: offset exceeds filtered content")
	}
	end := offset + limit
	if end > len(content) {
		end = len(content)
	}
	offset = utf8Start(content, offset)
	end = utf8End(content, end)
	if end == offset && offset < len(content) {
		_, size := utf8.DecodeRuneInString(content[offset:])
		end = offset + size
	}
	payload := map[string]any{
		"result_ref": result.Ref, "tool": result.Tool, "digest": result.Digest,
		"offset": offset, "next_offset": end, "total_bytes": len(content),
		"has_more": end < len(content), "content": content[offset:end],
	}
	encoded, err := json.Marshal(payload)
	return string(encoded), err
}

func utf8Start(value string, index int) int {
	for index > 0 && index < len(value) && !utf8.RuneStart(value[index]) {
		index--
	}
	return index
}

func utf8End(value string, index int) int {
	for index < len(value) && index > 0 && !utf8.RuneStart(value[index]) {
		index--
	}
	return index
}

func (service *Service) ReadPlanHandler(_ context.Context, argsJSON string) (string, error) {
	var input struct {
		PlanRef string   `json:"plan_ref"`
		NodeIDs []string `json:"node_ids,omitempty"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
		return "", fmt.Errorf("read_plan: invalid JSON: %w", err)
	}
	service.mu.RLock()
	planRef := strings.TrimSpace(input.PlanRef)
	if planRef == "" {
		planRef = service.activePlanID
	}
	frame := activePlanFrame(service.planStack, planRef)
	if frame == nil {
		service.mu.RUnlock()
		return "", errors.New("read_plan: plan_ref is not available in the current session")
	}
	arguments := frame.Arguments
	planState := cloneRuntimeState(RuntimeState{Plan: frame.Plan}).Plan
	service.mu.RUnlock()

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
