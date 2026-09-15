package seelebridge

// runtime_goal_tl.go — seelebridge → goal 域的真实 TL 评估器（P1 装配）。
//
// goal/govern 保持叶子包：TLEvaluator 由装配侧（seelebridge）实现，用
// Runtime 主 completer 完成有界 TL 回合，输出经 goal 域校验的 TLDirective。
// 组合根（main.go）在首次会话启动前经 app.SetGoalTLEvaluator 注入。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/types"

	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

// goalLLMEvaluator 实现 goaldomain.TLEvaluator：真实 LLM 评审 goal 域
// 嵌入（锚点 + 帧账本 + b 自身回合记忆），要求输出 TLDirective JSON。
type goalLLMEvaluator struct {
	completer agent.Completer
	// promptProvider 读"已装配的 ADVISOR 提示词"（Agent Team 员工入职时登记）；
	// nil 或返回空串 = 用内置角色设定。输出契约不受它影响。
	promptProvider func(roleName string) string
}

// goalAdvisorRolePrompt 是 ADVISOR(TL) 的内置角色设定（未登记员工提示词时使用）。
// 它是"角色设定"那一段；下面的输出契约永远追加（治理要解析 TLDirective，不能被
// 员工提示词改掉输出格式）。
const goalAdvisorRolePrompt = `你是 Seelex 的 TechLeader(ADVISOR)：只依据 [审查上下文] 做一次有界评审。`

// goalAdvisorOutputContract 是 ADVISOR 回合的输出契约（登记提示词也改不掉它，
// 否则 goal 域解析不出指令）。
const goalAdvisorOutputContract = `规则：
1. 只输出一个 JSON 对象，不要 markdown 围栏、不要解释；
2. kind ∈ verdict_done|verdict_not_done|checkpoint_ok|correct|escalate_human；
3. content ≤1200 字符，给出结论与下一步建议；
4. refs 只放仓库内相对路径（≤16）；无法判定或越权时用 escalate_human。`

// advisorRoleName 是 ADVISOR 的逻辑角色名（与 sessionstore.RoleTL 一致）。
const advisorRoleName = "tl"

// advisorSystemPrompt 组装 ADVISOR 回合的 system 提示词：**已登记的员工提示词
// 优先**（Agent Team 面板"员工入职 → 提示词"），未登记时用内置角色设定；无论
// 哪种情况都追加输出契约。
func (e *goalLLMEvaluator) advisorSystemPrompt() string {
	role := ""
	if e.promptProvider != nil {
		role = strings.TrimSpace(e.promptProvider(advisorRoleName))
	}
	if role == "" {
		role = goalAdvisorRolePrompt
	}
	return role + "\n" + goalAdvisorOutputContract
}

func (e *goalLLMEvaluator) Evaluate(ctx context.Context, embed goaldomain.TLSessionEmbed) (goaldomain.TLDirective, error) {
	if e == nil || e.completer == nil {
		return goaldomain.TLDirective{}, goaldomain.ErrTLDisabled
	}
	systemPrompt := e.advisorSystemPrompt()
	content := "请评审下面的 goal 治理上下文并输出 TLDirective JSON：" +
		` {"kind":"...","content":"...","refs":[],"severity":"P0|P1|P2"}` +
		"\n\n[审查上下文]\n" + embed.RenderText()

	message, err := e.completer.Complete(ctx, []types.Message{
		{Role: "system", Content: stringPtr(systemPrompt)},
		{Role: "user", Content: stringPtr(content)},
	}, nil)
	if err != nil {
		return goaldomain.TLDirective{}, fmt.Errorf("goal TL 回合失败（B4 缺席矩阵）: %w", err)
	}
	raw := ""
	if message.Content != nil {
		raw = *message.Content
	}
	directive, err := parseGoalDirective(raw)
	if err != nil {
		return goaldomain.TLDirective{}, err
	}
	if err := directive.Validate(); err != nil {
		return goaldomain.TLDirective{}, fmt.Errorf("goal TL 输出未通过域校验: %w（原文 %q）", err, truncateRunes(raw, 200))
	}
	return directive, nil
}

func parseGoalDirective(raw string) (goaldomain.TLDirective, error) {
	raw = strings.TrimSpace(raw)
	start := strings.IndexByte(raw, '{')
	end := strings.LastIndexByte(raw, '}')
	if start < 0 || end <= start {
		return goaldomain.TLDirective{}, fmt.Errorf("goal TL 输出缺少 JSON 对象（原文 %q）", truncateRunes(raw, 300))
	}
	var directive goaldomain.TLDirective
	if err := json.Unmarshal([]byte(raw[start:end+1]), &directive); err != nil {
		return goaldomain.TLDirective{}, fmt.Errorf("goal TL 输出非 JSON: %v（原文 %q）", err, truncateRunes(raw, 300))
	}
	return directive, nil
}

func stringPtr(value string) *string { return &value }

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max-1]) + "…"
}

// GoalTLEvaluator 返回真实 TL 评估器（用 Runtime 主 completer；未装配
// completer 时返回 nil → Supervisor 按 TL 未启用处理）。
//
// ADVISOR 的角色提示词取自 `SetRolePromptProvider` 注入的读面（装配根从 Agent
// Team 角色注册表读）：用户登记了提示词就用它，否则用内置角色设定；输出契约
// 永远追加。
func (r *Runtime) GoalTLEvaluator() goaldomain.TLEvaluator {
	if r == nil || r.completer == nil {
		return nil
	}
	return &goalLLMEvaluator{completer: r.completer, promptProvider: r.rolePromptFor}
}
