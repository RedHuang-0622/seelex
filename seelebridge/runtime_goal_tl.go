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
}

func (e *goalLLMEvaluator) Evaluate(ctx context.Context, embed goaldomain.TLSessionEmbed) (goaldomain.TLDirective, error) {
	if e == nil || e.completer == nil {
		return goaldomain.TLDirective{}, goaldomain.ErrTLDisabled
	}
	systemPrompt := `你是 Seelex 的 TechLeader(ADVISOR)：只依据 [审查上下文] 做一次有界评审。
规则：
1. 只输出一个 JSON 对象，不要 markdown 围栏、不要解释；
2. kind ∈ verdict_done|verdict_not_done|checkpoint_ok|correct|escalate_human；
3. content ≤1200 字符，给出结论与下一步建议；
4. refs 只放仓库内相对路径（≤16）；无法判定或越权时用 escalate_human。`
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
func (r *Runtime) GoalTLEvaluator() goaldomain.TLEvaluator {
	if r == nil || r.completer == nil {
		return nil
	}
	return &goalLLMEvaluator{completer: r.completer}
}
