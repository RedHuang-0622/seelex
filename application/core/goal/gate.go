package goal

// gate.go：终态 gate + 审批预筛的 DS-A2A 语义（协议 §5 裁决 + §8 B4 缺席矩阵）。
//
// EXEC(a) 提议终态（finish）不再直接收口：进入 ProposeFinish → b 回合（terminal.proposed
// 帧 append 到 b 上下文后评估）→
//   verdict_done     → 迁移 completed 并出栈（controller.Finish）；
//   verdict_not_done → goal 保持 active（指令经 DirectiveBus corr 信封待 a 领取，不回写 goal）；
//   escalate_human   → 保持 active，由调用方转人工（task_needs_user_decision 语义）；
//   b 缺席（未启用/回合失败 429/超时） → 安全默认：low 放行（直连 finish 回退）/ high 升人工。
//
// B4 铁律：a 永不等待 b —— 任何 gate 都有边界（回合失败不阻塞收口路径，goal 保持可驱动）。

import (
	"context"
	"fmt"
	"strings"
)

// ProposalOutcome 是终态提议的处理结果。
type ProposalOutcome string

const (
	OutcomeNoGoal    ProposalOutcome = "no_goal"        // 栈空，无目标可收口
	OutcomeNoTL      ProposalOutcome = "no_tl"          // b 未启用/缺席 → 直连 finish（回退）
	OutcomeCompleted ProposalOutcome = "completed"      // b verdict_done → 收口出栈
	OutcomeNotDone   ProposalOutcome = "not_done"       // b verdict_not_done → 保持 active
	OutcomeEscalate  ProposalOutcome = "escalate_human" // 保持 active，转人工
)

// FinishProposalResult 是一次终态提议 gate 的结构化结果。
type FinishProposalResult struct {
	Outcome   ProposalOutcome `json:"outcome"`
	Directive *TLDirective    `json:"directive,omitempty"`
	Goal      *GoalRecord     `json:"goal,omitempty"` // completed 时为收口记录；其余为当前 active
	Message   string          `json:"message,omitempty"`
}

// ProposeFinish 把 EXEC 的 goal_finish 提议送入 b 终态 gate（DS-A2A）：
// b 未启用/回合失败 → 安全默认（absent）。成功裁决按 done/not_done/escalate 走。
func (s *Supervisor) ProposeFinish(ctx context.Context, request FinishRequest) (FinishProposalResult, error) {
	if _, ok := s.ctl.ActiveGoal(); !ok {
		return FinishProposalResult{Outcome: OutcomeNoGoal}, nil
	}
	if !s.Enabled() {
		record, err := s.ctl.Finish(ctx, request)
		if err != nil {
			return FinishProposalResult{}, err
		}
		s.unbindIfTerminal("no_tl_absent")
		return FinishProposalResult{Outcome: OutcomeNoTL, Goal: record}, nil
	}

	// b 启用：terminal.proposed 帧进入 b 上下文并强制一回合。
	s.mu.Lock()
	directive, err := s.runRoundLocked(ctx, "gate:goal_finish", TLEvalSignal{
		Kind: SignalTerminalProposal, Source: "finish_gate", Detail: boundedProposalDetail(request.Result),
	})
	s.mu.Unlock()
	if err != nil {
		// B4：b 缺席（429/超时/回合失败）——goal 保持 active，转人工/由上层决定（a 不卡死）。
		active, _ := s.ctl.ActiveGoal()
		s.unbindIfTerminal("evicted_round_failure")
		return FinishProposalResult{
			Outcome: OutcomeEscalate,
			Goal:    active,
			Message: fmt.Sprintf("b 回合失败（B4 缺席默认：转人工），goal 保持 active: %v", err),
		}, nil
	}

	switch directive.Kind {
	case DirectiveVerdictDone:
		record, err := s.ctl.Finish(ctx, request)
		if err != nil {
			return FinishProposalResult{}, err
		}
		s.unbindIfTerminal("done")
		return FinishProposalResult{Outcome: OutcomeCompleted, Directive: &directive, Goal: record}, nil
	case DirectiveVerdictNotDone:
		current, ok := s.ctl.ActiveGoal()
		if !ok || current == nil {
			return FinishProposalResult{}, fmt.Errorf("%w: verdict_not_done 后无 active goal", ErrInvalidArgument)
		}
		return FinishProposalResult{
			Outcome:   OutcomeNotDone,
			Directive: &directive,
			Goal:      current,
			Message:   directive.Summary(),
		}, nil
	case DirectiveEscalateHuman:
		current, _ := s.ctl.ActiveGoal()
		return FinishProposalResult{
			Outcome:   OutcomeEscalate,
			Directive: &directive,
			Goal:      current,
			Message:   directive.Summary(),
		}, nil
	default:
		return FinishProposalResult{}, fmt.Errorf("%w: 终态 gate 期望 verdict_done/verdict_not_done/escalate_human, 得 %q",
			ErrBadDirective, directive.Kind)
	}
}

func boundedProposalDetail(result string) string {
	if len([]rune(result)) > MaxSignalDetailRunes {
		return string([]rune(result)[:MaxSignalDetailRunes])
	}
	return result
}

// ApprovalOutcome 是审批预筛结果。
type ApprovalOutcome string

const (
	ApprovalOutcomeApproved ApprovalOutcome = "approved"       // b 代答放行（低风险）
	ApprovalOutcomeDenied   ApprovalOutcome = "denied"         // b 代答拒绝
	ApprovalOutcomeEscalate ApprovalOutcome = "escalate_human" // 转人工（默认拒绝兜底在审批层）
)

// ApprovalScreenRequest 是一次审批预筛请求。
type ApprovalScreenRequest struct {
	Summary   string `json:"summary"`
	RiskLevel string `json:"risk_level"` // "low" | "high"（白名单判定在装配层；v0 仅 low 可代答）
	Detail    string `json:"detail,omitempty"`
	Ref       string `json:"ref,omitempty"`
}

// ApprovalVerdict 是预筛结果（reviewer=b；escalate 时仍需原人工审批链）。
type ApprovalVerdict struct {
	Outcome   ApprovalOutcome `json:"outcome"`
	Directive *TLDirective    `json:"directive,omitempty"`
	Message   string          `json:"message,omitempty"`
}

// PreScreenApproval 在 ask_approve/ApprovalBroker 前做 b 预筛（DS-A2A + B4）：
//   - high 风险或 b 缺席 → escalate_human（人工，默认拒绝兜底不变）；
//   - low 风险 → b 回合（approval.requested 帧）→ approve → Approved；deny → Denied；其余 → Escalate。
func (s *Supervisor) PreScreenApproval(ctx context.Context, request ApprovalScreenRequest) (ApprovalVerdict, error) {
	summary := strings.TrimSpace(request.Summary)
	if summary == "" {
		return ApprovalVerdict{}, fmt.Errorf("%w: approval summary 必填", ErrInvalidArgument)
	}
	if len([]rune(summary)) > MaxSignalDetailRunes {
		return ApprovalVerdict{}, fmt.Errorf("%w: approval summary 超长（> %d runes）", ErrInvalidArgument, MaxSignalDetailRunes)
	}
	if !s.Enabled() {
		return ApprovalVerdict{Outcome: ApprovalOutcomeEscalate,
			Message: "b 未启用：转人工审批（默认拒绝兜底）"}, nil
	}
	if _, ok := s.ctl.ActiveGoal(); !ok {
		return ApprovalVerdict{Outcome: ApprovalOutcomeEscalate,
			Message: "无 active goal：转人工审批（默认拒绝兜底）"}, nil
	}
	if strings.EqualFold(strings.TrimSpace(request.RiskLevel), "high") {
		return ApprovalVerdict{Outcome: ApprovalOutcomeEscalate,
			Message: "high 风险不在 b 代答白名单：转人工审批（默认拒绝兜底）"}, nil
	}

	s.mu.Lock()
	directive, err := s.runRoundLocked(ctx, "gate:approval_prescreen", TLEvalSignal{
		Kind: SignalApprovalAsked, Source: "approval_prescreen",
		Detail: boundedProposalDetail(summary), Ref: request.Ref,
	})
	s.mu.Unlock()
	if err != nil {
		// B4：b 缺席（429/超时）→ 转人工（默认拒绝兜底不变）。
		s.unbindIfTerminal("evicted_round_failure")
		return ApprovalVerdict{Outcome: ApprovalOutcomeEscalate,
			Message: fmt.Sprintf("b 回合失败（B4 缺席默认：转人工审批）: %v", err)}, nil
	}
	switch directive.Kind {
	case DirectiveApprove:
		return ApprovalVerdict{Outcome: ApprovalOutcomeApproved, Directive: &directive,
			Message: "b 代答放行（低风险白名单）"}, nil
	case DirectiveDeny:
		return ApprovalVerdict{Outcome: ApprovalOutcomeDenied, Directive: &directive,
			Message: directive.Summary()}, nil
	case DirectiveEscalateHuman:
		return ApprovalVerdict{Outcome: ApprovalOutcomeEscalate, Directive: &directive,
			Message: "b 判越权：转人工审批"}, nil
	default:
		return ApprovalVerdict{}, fmt.Errorf("%w: 预筛期望 approve/deny/escalate_human, 得 %q",
			ErrBadDirective, directive.Kind)
	}
}
