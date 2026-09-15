// runtime_role_prompt.go — 员工提示词的"装配读面"与"一次有界优化"。
//
// 生态位（把 Agent Team 面板里登记的提示词接到真实回合上）：
//
//   - 读面：`SetRolePromptProvider` 注入"按角色名读已装配提示词"的闭包（装配根
//     从 Agent Team 角色注册表读），ADVISOR/TL 回合据此用用户登记的提示词，未
//     登记时回退内置提示词。这是"提示词装配"真正生效的那一段——此前
//     RoleSpec.SystemPrompt 只落盘、无人消费。
//   - 优化：`OptimizeRolePrompt` 实现 application/contract.RolePromptPort：一次
//     LLM 回合把员工提示词改写成更明确可执行的版本，只返回候选文本，不落盘、
//     不写会话消息（落盘仍由用户在入职/保存动作里确认）。
package seelebridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// SetRolePromptProvider 注入"已装配提示词"读面；传 nil 取消注入（回退内置）。
// 读面在同一次启动内只注入一次，重复调用以最后一次为准。
func (r *Runtime) SetRolePromptProvider(provider func(roleName string) string) {
	if r == nil {
		return
	}
	r.rolePromptMu.Lock()
	r.rolePrompt = provider
	r.rolePromptMu.Unlock()
}

// rolePromptFor 读某个角色登记的提示词；未注入读面或未登记时返回空串
// （调用方用内置提示词兜底）。
func (r *Runtime) rolePromptFor(roleName string) string {
	if r == nil {
		return ""
	}
	r.rolePromptMu.RLock()
	provider := r.rolePrompt
	r.rolePromptMu.RUnlock()
	if provider == nil {
		return ""
	}
	return strings.TrimSpace(provider(roleName))
}

// rolePromptOptimizeSystemPrompt 是"优化提示词"这一次回合的元指令：要求输出
// 单个 JSON 对象，便于调用方严格解析。它不是员工提示词本身，而是"改写器"的
// 指令，所以固定在这里、不可被登记提示词覆盖。
const rolePromptOptimizeSystemPrompt = `你是提示词工程助手：把用户给出的员工（agent 角色）提示词改写成更明确、可执行的版本。
规则：
1. 只输出一个 JSON 对象，不要 markdown 围栏、不要解释；
2. 字段固定为 {"optimized":"...","notes":["..."]}；
3. optimized 保留原意与语言（原文中文就写中文），补齐：职责边界、输入、输出格式、约束、不做什么；
4. 不要编造原文没有的能力、工具或权限；
5. notes 写最多 5 条改动理由（每条 ≤60 字）。`

// OptimizeRolePrompt 实现 application/contract.RolePromptPort：一次有界 LLM
// 回合，把员工提示词改写成候选版本。
func (r *Runtime) OptimizeRolePrompt(ctx context.Context, request dto.RolePromptOptimizeRequest) (dto.RolePromptOptimizeResult, error) {
	if r == nil || r.completer == nil {
		return dto.RolePromptOptimizeResult{}, errors.New("提示词优化不可用：当前没有可用的 LLM completer")
	}
	draft := strings.TrimSpace(request.SystemPrompt)
	if draft == "" {
		return dto.RolePromptOptimizeResult{}, errors.New("system_prompt is required")
	}
	roleName := strings.TrimSpace(request.RoleName)
	contextLines := []string{
		"员工角色名：" + nonEmptyOr(roleName, "（未命名）"),
		"角色类型：" + nonEmptyOr(strings.TrimSpace(request.RoleKind), "agent"),
		"所属团队：" + nonEmptyOr(strings.TrimSpace(request.TeamKind), "（未装配）"),
	}
	if len(request.OrderRoles) > 0 {
		contextLines = append(contextLines, "团队发言顺序："+strings.Join(request.OrderRoles, " → "))
	}
	if tools := strings.TrimSpace(request.ToolsPolicy); tools != "" {
		contextLines = append(contextLines, "工具权限："+tools)
	}
	content := "请优化下面的员工提示词并按要求输出 JSON：\n\n[员工上下文]\n" +
		strings.Join(contextLines, "\n") + "\n\n[待优化提示词]\n" + draft

	message, err := r.completer.Complete(ctx, []types.Message{
		{Role: "system", Content: stringPtr(rolePromptOptimizeSystemPrompt)},
		{Role: "user", Content: stringPtr(content)},
	}, nil)
	if err != nil {
		return dto.RolePromptOptimizeResult{}, fmt.Errorf("提示词优化回合失败: %w", err)
	}
	raw := ""
	if message.Content != nil {
		raw = *message.Content
	}
	optimized, notes, err := parseRolePromptOptimization(raw)
	if err != nil {
		return dto.RolePromptOptimizeResult{}, err
	}
	result := dto.RolePromptOptimizeResult{
		RoleName:  roleName,
		Original:  draft,
		Optimized: optimized,
		Notes:     notes,
		Model:     r.Model(),
	}
	return result, nil
}

// rolePromptOptimization 是优化回合的 JSON 输出契约。
type rolePromptOptimization struct {
	Optimized string   `json:"optimized"`
	Notes     []string `json:"notes"`
}

// parseRolePromptOptimization 从模型输出里抽出 JSON 对象并校验：optimized 必填，
// notes 去空、截断到 5 条（不因为多写几条理由就拒绝整份结果）。
func parseRolePromptOptimization(raw string) (string, []string, error) {
	raw = strings.TrimSpace(raw)
	start := strings.IndexByte(raw, '{')
	end := strings.LastIndexByte(raw, '}')
	if start < 0 || end <= start {
		return "", nil, fmt.Errorf("提示词优化输出缺少 JSON 对象（原文 %q）", truncateRunes(raw, 200))
	}
	var parsed rolePromptOptimization
	if err := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err != nil {
		return "", nil, fmt.Errorf("提示词优化输出非 JSON: %v（原文 %q）", err, truncateRunes(raw, 200))
	}
	optimized := strings.TrimSpace(parsed.Optimized)
	if optimized == "" {
		return "", nil, errors.New("提示词优化输出缺少 optimized 字段")
	}
	notes := make([]string, 0, len(parsed.Notes))
	for _, note := range parsed.Notes {
		if note = strings.TrimSpace(note); note == "" {
			continue
		}
		if len(notes) == 5 {
			break
		}
		notes = append(notes, truncateRunes(note, 60))
	}
	return optimized, notes, nil
}

func nonEmptyOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
