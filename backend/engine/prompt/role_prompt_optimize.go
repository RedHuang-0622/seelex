package prompt_layer

// role_prompt_optimize.go 是**员工提示词的"一次有界优化"**（`contract.RolePromptPort` 的实现）。
//
// 落点沿革（P21-15「域方法随类型走」）：从 `backend/engine/assemble/runtime_role_prompt.go`
// 把优化那一半搬进提示词域（plan §2.5 把 `seelebridge/runtime_role_prompt.go` 列为
// `backend/engine/prompt/` 的目标）。读面（`SetRolePromptProvider` / `rolePromptFor`）留在装配层：
// 它持的是宿主注入的读面状态，不是提示词域的事实。
//
// 优化回合只做一件事：一次 LLM 调用把员工提示词改写成更明确可执行的版本，**只返回候选文本**，
// 不落盘、不写会话消息（落盘仍由用户在入职/保存动作里确认）。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/jsonobj"
)

// ErrOptimizerUnavailable 是"没有可用的 LLM 执行面"时的报错（文案与迁移前逐字一致）。
var ErrOptimizerUnavailable = errors.New("提示词优化不可用：当前没有可用的 LLM completer")

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

// RolePromptOptimizerDeps 是这一面需要的两格宿主读数（都是函数值，域包因此不认识
// Runtime 与 completer）。
type RolePromptOptimizerDeps struct {
	// Complete 是"一次无工具回合"（system + user → 原文）。nil = 没有可用执行面。
	Complete func(ctx context.Context, systemPrompt, userContent string) (string, error)
	// Model 报告本次使用的模型名（回执用；nil = 留空）。
	Model func() string
}

// RolePromptOptimizer 把一次改写回合的**判据**收在一处：请求 → 元指令与上下文渲染 →
// 输出契约解析 → 回执。无状态，可并发使用。
type RolePromptOptimizer struct {
	deps RolePromptOptimizerDeps
}

// NewRolePromptOptimizer 装配优化器（装配点是组合根的 `Runtime.OptimizeRolePrompt`）。
func NewRolePromptOptimizer(deps RolePromptOptimizerDeps) *RolePromptOptimizer {
	return &RolePromptOptimizer{deps: deps}
}

// Optimize 跑一次有界改写回合，返回候选提示词与改动理由。
//
// 三条入口判据（顺序即判据）：没有执行面 → `ErrOptimizerUnavailable`；草稿为空 →
// 显式报错；回合失败 → 包一层"提示词优化回合失败"（与迁移前逐字一致）。
func (optimizer *RolePromptOptimizer) Optimize(ctx context.Context, request dto.RolePromptOptimizeRequest) (dto.RolePromptOptimizeResult, error) {
	if optimizer == nil || optimizer.deps.Complete == nil {
		return dto.RolePromptOptimizeResult{}, ErrOptimizerUnavailable
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

	raw, err := optimizer.deps.Complete(ctx, rolePromptOptimizeSystemPrompt, content)
	if err != nil {
		return dto.RolePromptOptimizeResult{}, fmt.Errorf("提示词优化回合失败: %w", err)
	}
	optimized, notes, err := parseRolePromptOptimization(raw)
	if err != nil {
		return dto.RolePromptOptimizeResult{}, err
	}
	model := ""
	if optimizer.deps.Model != nil {
		model = optimizer.deps.Model()
	}
	return dto.RolePromptOptimizeResult{
		RoleName:  roleName,
		Original:  draft,
		Optimized: optimized,
		Notes:     notes,
		Model:     model,
	}, nil
}

// rolePromptOptimization 是优化回合的 JSON 输出契约。
type rolePromptOptimization struct {
	Optimized string   `json:"optimized"`
	Notes     []string `json:"notes"`
}

// parseRolePromptOptimization 从模型输出里抽出 JSON 对象并校验：optimized 必填，
// notes 去空、截断到 5 条（不因为多写几条理由就拒绝整份结果）。
//
// 抽取走 `jsonobj.DecodeLenient`：optimized 是一整段**多行提示词**，值是"未转义
// 引号 / 裸换行"的高发地带，而这里失败会让用户的白等一次模型调用白费（见
// internal/jsonobj 的事故说明）。
func parseRolePromptOptimization(raw string) (string, []string, error) {
	raw = strings.TrimSpace(raw)
	var parsed rolePromptOptimization
	if err := jsonobj.DecodeLenient(raw, &parsed); err != nil {
		if errors.Is(err, jsonobj.ErrNoJSONObject) {
			return "", nil, fmt.Errorf("提示词优化输出缺少 JSON 对象（原文 %q）", truncatePromptRunes(raw, 200))
		}
		return "", nil, fmt.Errorf("提示词优化输出非 JSON: %v（原文 %q）", err, truncatePromptRunes(raw, 200))
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
		notes = append(notes, truncatePromptRunes(note, 60))
	}
	return optimized, notes, nil
}

// nonEmptyOr 取第一个非空值（空白视为空）。
func nonEmptyOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// truncatePromptRunes 把原文压到 max 个 rune 并加省略号（只用于错误文案的原文摘录）。
//
// 与仓库里别处同名助手"像却不并"：本仓另有若干 `truncateRunes`，判据并不相同
// （有没有省略号、要不要 TrimSpace、`<= 0` 时返回空串还是原样返回）。这里是提示词域的
// 口径：加省略号、`max <= 0` 原样返回（不切片越界）。
func truncatePromptRunes(value string, max int) string {
	if max <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max-1]) + "…"
}
