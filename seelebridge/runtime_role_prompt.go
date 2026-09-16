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
// 组织方式按 Claude 官方提示词规范：角色 → 任务 → 改写清单 → 规则 → 输出格式，
// 每段一个 XML 标签；"要写成什么形状"用标签清单讲清，而不是靠形容词堆叠。
const rolePromptOptimizeSystemPrompt = `<role>
你是提示词工程助手，负责把一个「员工（agent 角色）」的系统提示词改写成更明确、可执行的版本。
</role>

<task>
改写 <draft_prompt> 里的提示词：保留原意与原语言（原文是中文就写中文），把含糊的意图补成可执行的结构。
</task>

<rewrite_checklist>
缺什么补什么，已经有的保留；改写结果按下面这些段组织：
1. <role>：这个员工是谁、站在什么视角干活；
2. <responsibilities>：职责边界——做什么、明确不做什么；
3. <inputs>：它会收到什么材料（上游给它的输入）；
4. <output_format>：它必须产出什么形状的结果；
5. <constraints>：硬约束（不许做的事、必须遵守的规则）。
</rewrite_checklist>

<rules>
- 只改写提示词本身：不要编造原文没有的能力、工具或权限；
- 用短句与编号列表，删掉空泛形容词；
- 不要寒暄、不要复述本指令、不要输出解释文字。
</rules>

<output_format>
只输出一个 JSON 对象，不要 markdown 围栏、不要任何解释文字：
{"optimized":"<改写后的完整提示词>","notes":["<改动理由>"]}
notes 最多 5 条，每条不超过 60 字。
</output_format>`

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
		"<employee_name>" + nonEmptyOr(roleName, "（未命名）") + "</employee_name>",
		"<role_kind>" + nonEmptyOr(strings.TrimSpace(request.RoleKind), "agent") + "</role_kind>",
		"<team_kind>" + nonEmptyOr(strings.TrimSpace(request.TeamKind), "（未装配）") + "</team_kind>",
	}
	if len(request.OrderRoles) > 0 {
		contextLines = append(contextLines, "<speaking_order>"+strings.Join(request.OrderRoles, " → ")+"</speaking_order>")
	}
	if tools := strings.TrimSpace(request.ToolsPolicy); tools != "" {
		contextLines = append(contextLines, "<tools_policy>"+tools+"</tools_policy>")
	}
	content := "<employee_context>\n" + strings.Join(contextLines, "\n") + "\n</employee_context>" +
		"\n\n<draft_prompt>\n" + draft + "\n</draft_prompt>" +
		"\n\n<task>\n按系统指令改写 <draft_prompt>，只输出规定的 JSON 对象。\n</task>"

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
//
// 抽取走 decodeJSONObjectLenient：optimized 是一整段**多行提示词**，值是"未转义
// 引号 / 裸换行"的高发地带，而这里失败会让用户的白等一次模型调用白费（见
// json_object.go 的事故说明）。
func parseRolePromptOptimization(raw string) (string, []string, error) {
	raw = strings.TrimSpace(raw)
	var parsed rolePromptOptimization
	if err := decodeJSONObjectLenient(raw, &parsed); err != nil {
		if errors.Is(err, ErrNoJSONObject) {
			return "", nil, fmt.Errorf("提示词优化输出缺少 JSON 对象（原文 %q）", truncateRunes(raw, 200))
		}
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
