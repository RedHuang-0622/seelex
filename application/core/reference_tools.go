package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
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
