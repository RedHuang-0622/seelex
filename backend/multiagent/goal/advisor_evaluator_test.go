package goal

// advisor_evaluator_test.go 是 ADVISOR 评估器的**域级**用例（P21-13：评估器本体与
// 两条提示词用例随实现从 `backend/engine/assemble/runtime_role_prompt_test.go` 搬进来，
// 断言逐字；另补评估器与裁决解析的域级用例）。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// TestAdvisorSystemPromptPrefersRegisteredPrompt：登记了员工提示词就用它，未登记
// 回退内置角色设定；**两种情况都追加输出契约**（否则 goal 域解析不出指令）。
func TestAdvisorSystemPromptPrefersRegisteredPrompt(t *testing.T) {
	builtin := NewAdvisorEvaluator(AdvisorEvaluatorDeps{}).AdvisorSystemPrompt(false)
	if !strings.Contains(builtin, advisorRolePrompt) || !strings.Contains(builtin, advisorOutputContract) {
		t.Fatalf("未登记提示词时必须用内置角色设定 + 输出契约：%q", builtin)
	}

	evaluator := NewAdvisorEvaluator(AdvisorEvaluatorDeps{PromptProvider: func(roleName string) string {
		if roleName != AdvisorRoleName {
			t.Fatalf("提示词读面只应按 ADVISOR 角色名查询，实际 %q", roleName)
		}
		return "你是我司的评审官：先看测试证据，再看实现。"
	}})
	registered := evaluator.AdvisorSystemPrompt(true)
	if !strings.Contains(registered, "你是我司的评审官") {
		t.Fatalf("登记的提示词必须生效：%q", registered)
	}
	if strings.Contains(registered, advisorRolePrompt) {
		t.Fatalf("登记提示词必须替换内置角色设定：%q", registered)
	}
	if !strings.Contains(registered, advisorOutputContract) {
		t.Fatalf("输出契约必须永远追加：%q", registered)
	}

	// 读面返回空白 = 未登记（用内置兜底），不能让空提示词顶掉角色设定。
	blank := NewAdvisorEvaluator(AdvisorEvaluatorDeps{PromptProvider: func(string) string { return "   " }})
	if !strings.Contains(blank.AdvisorSystemPrompt(false), advisorRolePrompt) {
		t.Fatalf("空白登记提示词应回退内置角色设定：%q", blank.AdvisorSystemPrompt(false))
	}
}

// TestAdvisorSystemPromptReportsActualToolBoundary：工具边界必须与实际执行面**同源**
// ——提示词说"你有只读工具"而实际没有（或反之），模型的行为就会与实际能力错位，
// 比没有工具更糟（它会声称自己核对过文件）。
func TestAdvisorSystemPromptReportsActualToolBoundary(t *testing.T) {
	evaluator := NewAdvisorEvaluator(AdvisorEvaluatorDeps{})
	withTools := evaluator.AdvisorSystemPrompt(true)
	withoutTools := evaluator.AdvisorSystemPrompt(false)
	if !strings.Contains(withTools, advisorToolingWithTools) || strings.Contains(withTools, advisorToolingWithoutTools) {
		t.Fatalf("有工具时只应出现「有工具」段：%q", withTools)
	}
	if !strings.Contains(withoutTools, advisorToolingWithoutTools) || strings.Contains(withoutTools, advisorToolingWithTools) {
		t.Fatalf("无工具时只应出现「无工具」段：%q", withoutTools)
	}
	if !strings.Contains(withTools, advisorOutputContract) || !strings.Contains(withoutTools, advisorOutputContract) {
		t.Fatal("两种口径都必须带输出契约（否则 goal 域解析不出指令）")
	}
}

func advisorEmbed(mainSessionID string) TLSessionEmbed {
	return TLSessionEmbed{
		PeerID:    "peer-b",
		Goal:      GoalFrame{ID: "goal-1", Title: "架构重排", Statement: "把契约落成代码"},
		SessionID: mainSessionID,
	}
}

// TestAdvisorEvaluatorUsesRoundOnlyWithWorkspace：有工作区（embed 带主会话 id）且注入了
// 回合原语 ⇒ 走"带只读工具的角色回合"，且**权限按构造是只读**、上下文按帧渲染（FreshContext）、
// 流式分片从 ctx 挂的观察回调转发；两条口径（提示词说有没有工具 / 实际走哪条路）必须同源。
func TestAdvisorEvaluatorUsesRoundOnlyWithWorkspace(t *testing.T) {
	var got AdvisorRoundRequest
	roundCalled := 0
	evaluator := NewAdvisorEvaluator(AdvisorEvaluatorDeps{
		Complete: func(context.Context, string, string) (string, error) {
			t.Fatal("有工作区时不该走无工具兜底")
			return "", nil
		},
		Round: func(_ context.Context, request AdvisorRoundRequest) (string, error) {
			roundCalled++
			got = request
			return `{"kind":"verdict_done","content":"做完了"}`, nil
		},
	})

	ctx := WithTLDeltaSink(context.Background(), func(string) {})
	directive, err := evaluator.Evaluate(ctx, advisorEmbed("sess-main"))
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if roundCalled != 1 {
		t.Fatalf("回合原语调用次数 = %d, want 1", roundCalled)
	}
	if directive.Kind != DirectiveVerdictDone || directive.Content != "做完了" {
		t.Fatalf("裁决 = %+v", directive)
	}
	if got.ToolsPolicy != dto.ToolPolicyReadonly {
		t.Fatalf("评审者的权限口径必须是只读（按构造，不读注册表）：%q", got.ToolsPolicy)
	}
	if !got.FreshContext {
		t.Fatal("评审上下文按帧渲染：必须要求每轮清空引擎历史（跨 goal 隔离）")
	}
	if got.RoleName != AdvisorRoleName || got.RoleSessionID != AdvisorRoleSessionID("sess-main") {
		t.Fatalf("角色名/评审会话号 = %q/%q", got.RoleName, got.RoleSessionID)
	}
	if got.MaxLoops != AdvisorRoundMaxLoops || got.MaxLoops <= 0 {
		t.Fatalf("ReAct 上限 = %d, want %d", got.MaxLoops, AdvisorRoundMaxLoops)
	}
	if got.OnDelta == nil {
		t.Fatal("挂在 ctx 上的流式观察回调必须转发给回合（否则前端只能等终局裁决）")
	}
	if !strings.Contains(got.SystemPrompt, advisorToolingWithTools) {
		t.Fatalf("走带工具回合时提示词必须如实声明有只读工具：%q", got.SystemPrompt)
	}
}

// TestAdvisorEvaluatorFallsBackWithoutWorkspace：没有主会话 id（老宿主/单测）或没有回合
// 原语 ⇒ 退回无工具的一次 completer 调用，且提示词如实声明"本回合没有工具"。
func TestAdvisorEvaluatorFallsBackWithoutWorkspace(t *testing.T) {
	cases := map[string]struct {
		embed TLSessionEmbed
		deps  AdvisorEvaluatorDeps
	}{
		"没有主会话 id": {
			embed: advisorEmbed(""),
			deps: AdvisorEvaluatorDeps{Round: func(context.Context, AdvisorRoundRequest) (string, error) {
				t.Fatal("没有工作区时不该走角色回合")
				return "", nil
			}},
		},
		"没有回合原语": {embed: advisorEmbed("sess-main")},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			deps := testCase.deps
			var systemPrompt string
			deps.Complete = func(_ context.Context, system, _ string) (string, error) {
				systemPrompt = system
				return `{"kind":"escalate_human","content":"证据不足"}`, nil
			}
			evaluator := NewAdvisorEvaluator(deps)
			directive, err := evaluator.Evaluate(context.Background(), testCase.embed)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if directive.Kind != DirectiveEscalateHuman {
				t.Fatalf("裁决 = %+v", directive)
			}
			if !strings.Contains(systemPrompt, advisorToolingWithoutTools) {
				t.Fatalf("无工具兜底必须如实声明没有工具：%q", systemPrompt)
			}
		})
	}
}

// TestAdvisorEvaluatorDisabledWithoutDeps：两格执行面都没装 ⇒ `ErrTLDisabled`
// （"b 缺席"，不是"b 作答了但裁决不可用"——两者在 gate 上的收口说明不同）。
func TestAdvisorEvaluatorDisabledWithoutDeps(t *testing.T) {
	if _, err := NewAdvisorEvaluator(AdvisorEvaluatorDeps{}).Evaluate(context.Background(), advisorEmbed("sess-main")); !errors.Is(err, ErrTLDisabled) {
		t.Fatalf("未装配执行面应报 ErrTLDisabled，得到 %v", err)
	}
	var nilEvaluator *AdvisorEvaluator
	if _, err := nilEvaluator.Evaluate(context.Background(), advisorEmbed("sess-main")); !errors.Is(err, ErrTLDisabled) {
		t.Fatalf("nil 评估器应报 ErrTLDisabled，得到 %v", err)
	}
}

// TestAdvisorEvaluatorRejectsBadDirective 钉住**两类失败的区分**：原文解析不出 / 域校验
// 不过都必须是 `ErrBadDirective`（"b 已作答、裁决不可用"），不能混成"缺席"。
func TestAdvisorEvaluatorRejectsBadDirective(t *testing.T) {
	cases := map[string]string{
		"完全不是 JSON":  "抱歉，我无法完成",
		"JSON 但不是裁决": `{"foo":"bar"}`,
		"kind 非法":    `{"kind":"nonsense","content":"x"}`,
		"refs 超限":    `{"kind":"verdict_done","content":"x","refs":["1","2","3","4","5","6","7","8","9","10","11","12","13","14","15","16","17"]}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			evaluator := NewAdvisorEvaluator(AdvisorEvaluatorDeps{
				Complete: func(context.Context, string, string) (string, error) { return raw, nil },
			})
			_, err := evaluator.Evaluate(context.Background(), advisorEmbed("sess-main"))
			if !errors.Is(err, ErrBadDirective) {
				t.Fatalf("%s：必须报 ErrBadDirective，得到 %v", name, err)
			}
		})
	}
}

// TestParseGoalDirectiveLenientSyntax：容错只修**语法**（围栏 / 未转义引号），不改语义；
// 缺 JSON 对象的报错必须点明"缺少 JSON 对象"。
func TestParseGoalDirectiveLenientSyntax(t *testing.T) {
	directive, err := parseGoalDirective("```json\n{\"kind\":\"verdict_not_done\",\"content\":\"还没完\"}\n```")
	if err != nil {
		t.Fatalf("围栏 JSON 应当能解：%v", err)
	}
	if directive.Kind != DirectiveVerdictNotDone || directive.Content != "还没完" {
		t.Fatalf("解析结果 = %+v", directive)
	}

	_, err = parseGoalDirective("没有任何对象的解释文字")
	if !errors.Is(err, ErrBadDirective) || !strings.Contains(err.Error(), "缺少 JSON 对象") {
		t.Fatalf("缺 JSON 对象必须报 ErrBadDirective 且点明原因，得到 %v", err)
	}
}

// TestAdvisorRoleSessionIDIsNamespacedPerMainSession：评审会话号走独立命名空间且按主会话稳定
// （与 tl 员工做工会话刻意分开——共用会让评审上下文与员工上下文互相污染）。
func TestAdvisorRoleSessionIDIsNamespacedPerMainSession(t *testing.T) {
	if got := AdvisorRoleSessionID("  s-1  "); got != "advisor:s-1" {
		t.Fatalf("评审会话号 = %q, want advisor:s-1（trim 后加前缀）", got)
	}
	if AdvisorRoleSessionID("s-1") == AdvisorRoleSessionID("s-2") {
		t.Fatal("不同主会话的评审会话号必须不同")
	}
}

// TestTruncateAdvisorRunesIsSafeForNonPositive：`max <= 0` 原样返回（不切片越界 panic）。
func TestTruncateAdvisorRunesIsSafeForNonPositive(t *testing.T) {
	if got := truncateAdvisorRunes("正文", 0); got != "正文" {
		t.Fatalf("max=0 应原样返回，得到 %q", got)
	}
	if got := truncateAdvisorRunes("正文", -3); got != "正文" {
		t.Fatalf("max<0 应原样返回，得到 %q", got)
	}
	if got := truncateAdvisorRunes("一二三四五", 3); got != "一二…" {
		t.Fatalf("截断 = %q, want 一二…", got)
	}
}
