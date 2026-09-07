package goal

// gate.go：会话/目标终态 gate + 审批预筛（design §5.5-§5.6 的 MVP 切片）。
//
// mainagent 提议终态（finish）不再直接收口：进入 ProposeFinish → TL 回合 →
//   verdict_done        → 迁移 completed 并出栈（controller.Finish）；
//   verdict_not_done    → 附纠偏指令写回 goal 指令环，目标保持 active（不终态）；
//   escalate_human      → 保持 active，由调用方转人工（task_needs_user_decision 语义）；
//   TL 未启用          → 回退直连 finish（保持 Part I 语义，MVP 兼容）。
//
// 审批预筛（PreScreenApproval）：仅 low 风险可交给 TL 代答（approve/deny）；
// high 风险与 TL 不可用时一律 escalate_human（默认拒绝兜底，design §5.5/§7 风险表）。

import (
	"context"
	"fmt"
	"strings"
)

// ProposalOutcome 是终态提议的处理结果。
type ProposalOutcome string

const (
	OutcomeNoGoal    ProposalOutcome = "no_goal"        // 栈空，无目标可收口
	OutcomeNoTL      ProposalOutcome = "no_tl"          // TL 未启用 → 直连 finish（回退）
	OutcomeCompleted ProposalOutcome = "completed"      // TL verdict_done → 收口出栈
	OutcomeNotDone   ProposalOutcome = "not_done"       // TL verdict_not_done → 保持 active
	OutcomeEscalate  ProposalOutcome = "escalate_human" // 保持 active，转人工
)

// FinishProposalResult 是一次终态提议 gate 的结构化结果。
type FinishProposalResult struct {
	Outcome   ProposalOutcome `json:"outcome"`
	Directive *TLDirective    `json:"directive,omitempty"`
	Goal      *GoalRecord     `json:"goal,omitempty"` // completed 时为收口记录；其余为当前 active
	Message   string          `json:"message,omitempty"`
}

// ProposeFinish 把 mainagent 的 goal_finish 提议送入 TL 终态 gate。
// TL 未启用/无评估器时直连 controller.Finish（Part I 兼容回退）。
func (s *Supervisor) ProposeFinish(ctx context.Context, request FinishRequest) (FinishProposalResult, error) {
	if _, ok := s.ctl.ActiveGoal(); !ok {
		return FinishProposalResult{Outcome: OutcomeNoGoal}, nil
	}
	if !s.Enabled() {
		record, err := s.ctl.Finish(ctx, request)
		if err != nil {
			return FinishProposalResult{}, err
		}
		return FinishProposalResult{Outcome: OutcomeNoTL, Goal: record}, nil
	}

	// TL 启用：入队 terminal_proposal 信号并强制一回合。
	s.mailbox.EnqueueSignal(TLEvalSignal{
		Kind:   SignalTerminalProposal,
		At:     s.now(),
		Source: "finish_gate",
		Detail: boundedProposalDetail(request.Result),
	})
	s.mu.Lock()
	directive, err := s.runEvalLocked(ctx, "gate:goal_finish")
	s.mu.Unlock()
	if err != nil {
		return FinishProposalResult{}, err
	}

	switch directive.Kind {
	case DirectiveVerdictDone:
		record, err := s.ctl.Finish(ctx, request)
		if err != nil {
			return FinishProposalResult{}, err
		}
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
	ApprovalOutcomeApproved ApprovalOutcome = "approved"       // TL 代答放行（低风险）
	ApprovalOutcomeDenied   ApprovalOutcome = "denied"         // TL 代答拒绝
	ApprovalOutcomeEscalate ApprovalOutcome = "escalate_human" // 转人工（默认拒绝兜底在审批层）
)

// ApprovalScreenRequest 是一次审批预筛请求。
type ApprovalScreenRequest struct {
	Summary   string `json:"summary"`
	RiskLevel string `json:"risk_level"` // "low" | "high"（白名单判定在装配层；v0 仅 low 可代答）
	Detail    string `json:"detail,omitempty"`
	Ref       string `json:"ref,omitempty"`
}

// ApprovalVerdict 是预筛结果（reviewer=TL；escalate 时仍需原人工审批链）。
type ApprovalVerdict struct {
	Outcome   ApprovalOutcome `json:"outcome"`
	Directive *TLDirective    `json:"directive,omitempty"`
	Message   string          `json:"message,omitempty"`
}

// PreScreenApproval 在 ask_approve/ApprovalBroker 前做 TL 预筛：
//   - high 风险或 TL 不可用 → escalate_human（人工，默认拒绝兜底不变）；
//   - low 风险 → TL 回合，directive approve → Approved；deny → Denied；其余 → Escalate。
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
			Message: "TL 未启用：转人工审批（默认拒绝兜底）"}, nil
	}
	if _, ok := s.ctl.ActiveGoal(); !ok {
		return ApprovalVerdict{Outcome: ApprovalOutcomeEscalate,
			Message: "无 active goal：转人工审批（默认拒绝兜底）"}, nil
	}
	if strings.EqualFold(strings.TrimSpace(request.RiskLevel), "high") {
		return ApprovalVerdict{Outcome: ApprovalOutcomeEscalate,
			Message: "high 风险不在 TL 代答白名单：转人工审批（默认拒绝兜底）"}, nil
	}

	s.mailbox.EnqueueSignal(TLEvalSignal{
		Kind:   SignalApprovalAsked,
		At:     s.now(),
		Source: "approval_prescreen",
		Detail: boundedProposalDetail(summary),
		Ref:    request.Ref,
	})
	s.mu.Lock()
	directive, err := s.runEvalLocked(ctx, "gate:approval_prescreen")
	s.mu.Unlock()
	if err != nil {
		return ApprovalVerdict{}, err
	}
	switch directive.Kind {
	case DirectiveApprove:
		return ApprovalVerdict{Outcome: ApprovalOutcomeApproved, Directive: &directive,
			Message: "TL 代答放行（低风险白名单）"}, nil
	case DirectiveDeny:
		return ApprovalVerdict{Outcome: ApprovalOutcomeDenied, Directive: &directive,
			Message: directive.Summary()}, nil
	case DirectiveEscalateHuman:
		return ApprovalVerdict{Outcome: ApprovalOutcomeEscalate, Directive: &directive,
			Message: "TL 判越权：转人工审批"}, nil
	default:
		return ApprovalVerdict{}, fmt.Errorf("%w: 预筛期望 approve/deny/escalate_human, 得 %q",
			ErrBadDirective, directive.Kind)
	}
}
