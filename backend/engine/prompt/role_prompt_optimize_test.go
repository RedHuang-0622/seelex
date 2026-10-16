package prompt_layer

// role_prompt_optimize_test.go 是提示词优化回合的**域级**用例（P21-15：解析用例随实现从
// `backend/engine/assemble/runtime_role_prompt_test.go` 搬进来，断言逐字；另补回合编排的用例）。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// TestParseRolePromptOptimization：优化回合的 JSON 解析容忍围栏/前后噪声，缺
// optimized 显式报错，notes 去空且截断到 5 条。
func TestParseRolePromptOptimization(t *testing.T) {
	optimized, notes, err := parseRolePromptOptimization("```json\n{\"optimized\":\"新提示词\",\"notes\":[\"补了输出格式\",\"\",\"   \"]}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if optimized != "新提示词" || len(notes) != 1 || notes[0] != "补了输出格式" {
		t.Fatalf("解析结果 = %q %v", optimized, notes)
	}

	if _, _, err := parseRolePromptOptimization("抱歉，我无法完成"); err == nil {
		t.Fatal("缺 JSON 必须报错")
	}
	if _, _, err := parseRolePromptOptimization(`{"notes":["x"]}`); err == nil {
		t.Fatal("缺 optimized 必须报错")
	}

	_, notes, err = parseRolePromptOptimization(`{"optimized":"x","notes":["1","2","3","4","5","6","7"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 5 {
		t.Fatalf("notes 必须截断到 5 条，实际 %v", notes)
	}
}

// TestOptimizeRolePromptRoundTripsContext 钉住这一回合的编排：元指令固定（不可被登记提示词
// 覆盖）、员工上下文按标签渲染、回执带回原文/候选/理由/模型名。
func TestOptimizeRolePromptRoundTripsContext(t *testing.T) {
	var gotSystem, gotUser string
	optimizer := NewRolePromptOptimizer(RolePromptOptimizerDeps{
		Complete: func(_ context.Context, systemPrompt, userContent string) (string, error) {
			gotSystem, gotUser = systemPrompt, userContent
			return `{"optimized":"改写后的提示词","notes":["补了输出格式"]}`, nil
		},
		Model: func() string { return "gpt-5" },
	})
	result, err := optimizer.Optimize(context.Background(), dto.RolePromptOptimizeRequest{
		RoleName: "exec", RoleKind: "agent", TeamKind: "team-1", ToolsPolicy: "readonly",
		OrderRoles: []string{"pm", "exec"}, SystemPrompt: "  干活的  ",
	})
	if err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	if !strings.Contains(gotSystem, "<output_format>") || !strings.Contains(gotSystem, "改写") {
		t.Fatalf("元指令必须是固定的改写器指令：%q", gotSystem)
	}
	for _, want := range []string{"<employee_name>exec</employee_name>", "<role_kind>agent</role_kind>",
		"<team_kind>team-1</team_kind>", "<speaking_order>pm → exec</speaking_order>",
		"<tools_policy>readonly</tools_policy>", "<draft_prompt>\n干活的\n</draft_prompt>"} {
		if !strings.Contains(gotUser, want) {
			t.Fatalf("员工上下文缺 %q：%q", want, gotUser)
		}
	}
	if result.RoleName != "exec" || result.Original != "干活的" ||
		result.Optimized != "改写后的提示词" || len(result.Notes) != 1 || result.Model != "gpt-5" {
		t.Fatalf("回执 = %+v", result)
	}
}

// TestOptimizeRolePromptDefaultsAndGuards：缺省项有兜底（未命名 / agent / 未装配），
// 没有执行面或草稿为空时**不惊动模型**就报错（各自一条独立文案）。
func TestOptimizeRolePromptDefaultsAndGuards(t *testing.T) {
	var gotUser string
	optimizer := NewRolePromptOptimizer(RolePromptOptimizerDeps{
		Complete: func(_ context.Context, _, userContent string) (string, error) {
			gotUser = userContent
			return `{"optimized":"x"}`, nil
		},
	})
	if _, err := optimizer.Optimize(context.Background(), dto.RolePromptOptimizeRequest{SystemPrompt: "draft"}); err != nil {
		t.Fatalf("Optimize: %v", err)
	}
	for _, want := range []string{"<employee_name>（未命名）</employee_name>", "<role_kind>agent</role_kind>",
		"<team_kind>（未装配）</team_kind>"} {
		if !strings.Contains(gotUser, want) {
			t.Fatalf("缺省渲染缺 %q：%q", want, gotUser)
		}
	}
	if strings.Contains(gotUser, "<tools_policy>") || strings.Contains(gotUser, "<speaking_order>") {
		t.Fatalf("没有给值的可选段不该出现：%q", gotUser)
	}

	if _, err := optimizer.Optimize(context.Background(), dto.RolePromptOptimizeRequest{}); err == nil ||
		!strings.Contains(err.Error(), "system_prompt is required") {
		t.Fatalf("空草稿必须显式报错，得到 %v", err)
	}
	if _, err := NewRolePromptOptimizer(RolePromptOptimizerDeps{}).Optimize(
		context.Background(), dto.RolePromptOptimizeRequest{SystemPrompt: "draft"}); !errors.Is(err, ErrOptimizerUnavailable) {
		t.Fatalf("没有执行面必须报 ErrOptimizerUnavailable，得到 %v", err)
	}
	var nilOptimizer *RolePromptOptimizer
	if _, err := nilOptimizer.Optimize(context.Background(), dto.RolePromptOptimizeRequest{SystemPrompt: "draft"}); !errors.Is(err, ErrOptimizerUnavailable) {
		t.Fatalf("nil 优化器必须报 ErrOptimizerUnavailable，得到 %v", err)
	}
}

// TestOptimizeRolePromptWrapsRoundFailure：回合失败要包一层"提示词优化回合失败"
// （调用方据此区分"模型没答"与"答得不可用"），并保留底层错误可判。
func TestOptimizeRolePromptWrapsRoundFailure(t *testing.T) {
	sentinel := errors.New("provider 429")
	optimizer := NewRolePromptOptimizer(RolePromptOptimizerDeps{
		Complete: func(context.Context, string, string) (string, error) { return "", sentinel },
	})
	_, err := optimizer.Optimize(context.Background(), dto.RolePromptOptimizeRequest{SystemPrompt: "draft"})
	if err == nil || !errors.Is(err, sentinel) || !strings.Contains(err.Error(), "提示词优化回合失败") {
		t.Fatalf("回合失败必须包一层且保留底层错误，得到 %v", err)
	}
}

// TestOptimizeRolePromptTrimsNoteLength：每条理由截到 60 rune（有界回执），且不因为
// 多写几条理由就拒绝整份结果。
func TestOptimizeRolePromptTrimsNoteLength(t *testing.T) {
	long := strings.Repeat("理", 100)
	optimized, notes, err := parseRolePromptOptimization(`{"optimized":"x","notes":["` + long + `"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if optimized != "x" || len(notes) != 1 {
		t.Fatalf("结果 = %q %v", optimized, notes)
	}
	if got := len([]rune(notes[0])); got > 60 {
		t.Fatalf("理由必须截到 60 rune 内，实际 %d", got)
	}
}
