package goal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// errorEvaluator 是"b 回合没给出可用裁决"的桩：err 决定 gate 走哪条分类。
type errorEvaluator struct{ err error }

func (e errorEvaluator) Evaluate(context.Context, TLSessionEmbed) (TLDirective, error) {
	return TLDirective{}, e.err
}

// TestProposeFinishUnusableVerdictIsNotAbsence 钉住"裁决不可用"与"b 缺席"在面向
// 用户的说明里必须分开。
//
// 事故（2026-09-16 22:06）：ADVISOR 的裁决 JSON 因未转义的内层引号解析失败，gate
// 把它说成"B4 缺席默认（429/超时）"，于是面向用户的收口说明写"goal 仍为 active"，
// 而同一次运行的 goal 实际已 completed。两种失败都保持 active（安全默认：不拿
// 不可用的裁决收口），但说明必须诚实。
func TestProposeFinishUnusableVerdictIsNotAbsence(t *testing.T) {
	cases := []struct {
		name        string
		err         error
		wantAbsence bool
	}{
		{
			name:        "裁决不可用（ErrBadDirective）：不是缺席",
			err:         fmt.Errorf("%w: goal TL 输出非 JSON: invalid character 'å' after object key:value pair", ErrBadDirective),
			wantAbsence: false,
		},
		{
			name:        "回合失败（429/超时）：缺席",
			err:         errors.New("429 too many requests"),
			wantAbsence: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctl := newTestController(t, DefaultStackDepth)
			if _, err := ctl.Begin(testCtx, BeginRequest{
				Title: "目标", Acceptance: []string{"有结论"},
			}); err != nil {
				t.Fatalf("begin: %v", err)
			}
			sup := NewSupervisor(ctl, errorEvaluator{err: tc.err}, TechLeaderConfig{Enabled: true, EvalWindow: 0})
			result, err := sup.ProposeFinish(testCtx, FinishRequest{Result: "做完了"})
			if err != nil {
				t.Fatalf("propose: %v", err)
			}
			if result.Outcome != OutcomeEscalate {
				t.Fatalf("两种失败都应保持 active 并转人工（安全默认），得 %s", result.Outcome)
			}
			if active, _ := ctl.ActiveGoal(); active == nil {
				t.Fatal("失败后 goal 应保持 active")
			}
			absence := strings.Contains(result.Message, "B4 缺席默认")
			if absence != tc.wantAbsence {
				t.Fatalf("说明分类不符：期望缺席=%v，message=%q", tc.wantAbsence, result.Message)
			}
		})
	}
}
