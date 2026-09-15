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
// 写法按 Claude 官方提示词规范组织：先角色、再任务、再约束，每段一个 XML 标签
// （模型能准确定位"哪一段在讲什么"，也便于人审时逐段替换）。它只是"角色设定"那
// 一段；输出契约由 goalAdvisorOutputContract 追加，员工提示词改不掉它。
const goalAdvisorRolePrompt = `<role>
你是 Seelex 的 TechLeader（ADVISOR，逻辑角色名 tl）：在 goal 治理回合里做**一次**有界评审。
</role>

<task>
只依据 <review_context> 给出的证据，判断当前 goal 是否达成，并给出下一步动作（继续 / 收口 / 转人工）。不扩大范围、不索取新证据、不调用工具。
</task>

<constraints>
- 证据不足就不要猜：按输出契约给 escalate_human，或先要检查点；
- 不重复裁决已经裁过的事，除非上下文里有新证据；
- 依据必须落在仓库内可核对的事实上（refs 用相对路径）；
- 语言跟随上下文（中文上下文用中文）。
</constraints>`

// goalAdvisorOutputContract 是 ADVISOR 回合的输出契约（登记提示词也改不掉它，
// 否则 goal 域解析不出指令）。
const goalAdvisorOutputContract = `<output_contract>
只输出一个 JSON 对象，不要 markdown 围栏、不要任何解释文字：
{"kind":"verdict_done|verdict_not_done|checkpoint_ok|correct|escalate_human","content":"结论与下一步建议","refs":["仓库内相对路径"],"severity":"P0|P1|P2"}
规则：
1. kind 只能取上面五个值之一；
2. content ≤1200 字符，写结论 + 下一步建议，不写推理过程；
3. refs 只放仓库内相对路径，最多 16 条；
4. 无法判定或越权时用 escalate_human。
</output_contract>`

// advisorRoleName 是 ADVISOR 的逻辑角色名（与 sessionstore.RoleTL 一致）。
const advisorRoleName = "tl"

// advisorSystemPrompt 组装 ADVISOR 回合的 system 提示词：**已登记的员工提示词
// 优先**（Agent Team 面板"员工入职 → 提示词"），未登记时用内置角色设定；无论
// 哪种情况都追加输出契约。两段用 XML 标签分开——角色设定可被人替换，输出契约
// 是治理的解析前提，标签让边界一眼可见（也是 Claude 规范推荐的分段方式）。
func (e *goalLLMEvaluator) advisorSystemPrompt() string {
	role := ""
	if e.promptProvider != nil {
		role = strings.TrimSpace(e.promptProvider(advisorRoleName))
	}
	if role == "" {
		role = goalAdvisorRolePrompt
	}
	return "<role_prompt>\n" + role + "\n</role_prompt>\n\n" + goalAdvisorOutputContract
}

func (e *goalLLMEvaluator) Evaluate(ctx context.Context, embed goaldomain.TLSessionEmbed) (goaldomain.TLDirective, error) {
	if e == nil || e.completer == nil {
		return goaldomain.TLDirective{}, goaldomain.ErrTLDisabled
	}
	systemPrompt := e.advisorSystemPrompt()
	content := "<review_context>\n" + embed.RenderText() + "\n</review_context>" +
		"\n\n<task>\n按系统提示词里的角色设定与输出契约评审以上上下文，只输出 TLDirective JSON。\n</task>"

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
