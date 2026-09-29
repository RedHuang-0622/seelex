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
	"errors"
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
	// 三段式（2026-09-29）：评估在 s.mu 之外跑，gate 不再把整轮锁在自己身上。
	directive, err := s.runRound(ctx, "gate:goal_finish", TLEvalSignal{
		Kind: SignalTerminalProposal, Source: "finish_gate", Detail: boundedProposalDetail(request.Result),
	})
	if err != nil {
		active, _ := s.ctl.ActiveGoal()
		// A 语义（2026-09-29）：**已有回合在飞 → 不排队**。排队等于把收口路径挂在
		// 一个可能永远不结束的回合上（那正是 roundGate 那条纪律要避免的事）。按 B4
		// 缺席矩阵保持 active 转人工。刻意**不 reap**：在飞的那一轮还在用这个 peer，
		// 拆掉它的上下文等于把正在跑的评审弄瞎。
		if errors.Is(err, ErrRoundInFlight) {
			return FinishProposalResult{
				Outcome: OutcomeEscalate,
				Goal:    active,
				Message: fmt.Sprintf("已有 ADVISOR 回合在进行中（不排队等待：a 永不等待 b），goal 保持 active 待重新提议: %v", err),
			}, nil
		}
		// B 语义（2026-09-29）：回合执行期间 goal 被收口/更换 → 这次裁决无处落地，
		// 已丢弃。此时旧的 peer 绑的是已经不存在的目标：顺手 reap，别让它带进下一个
		// goal（headless 直接 Finish/Abort 不 reap 是既有缺口，这条路径至少不留）。
		if errors.Is(err, ErrRoundGoalGone) {
			s.unbindIfTerminal("goal_gone_during_round")
			return FinishProposalResult{
				Outcome: OutcomeEscalate,
				Goal:    active,
				Message: fmt.Sprintf("b 回合期间 goal 已收口/更换，本次裁决已丢弃（未落地），转人工确认: %v", err),
			}, nil
		}
		// gate 的两种失败必须分开说，否则用户看到的是与实际相反的收口状态：
		//   - b **已作答但裁决不可用**（ErrBadDirective：原文不可解析 / 域校验不过 /
		//     goal 漂移）—— 裁决内容可能存在，只是没能落地；
		//   - b **缺席**（429/超时/整个回合失败）—— B4 缺席矩阵。
		// 两者都保持 active（安全默认：不拿一份不可用的裁决去收口 goal），但说明必须
		// 诚实。把解析失败说成"缺席"，用户就会得到"goal 仍 active"这种与实际相反的
		// 结论（2026-09-16 事故：裁决内容上已是 verdict_done）。
		s.unbindIfTerminal("evicted_round_failure")
		if errors.Is(err, ErrBadDirective) {
			return FinishProposalResult{
				Outcome: OutcomeEscalate,
				Goal:    active,
				Message: fmt.Sprintf("b 已作答但裁决不可用（非缺席：不是 429/超时），goal 保持 active 待重新裁决: %v", err),
			}, nil
		}
		// B4：b 缺席（429/超时/回合失败）——goal 保持 active，转人工/由上层决定（a 不卡死）。
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

// CloseTopGoalOnTerminal 把一次**常规治理回合**产出的终态裁决落成 goal 收口
// （design §5 逃生口：verdict_done → Controller.Finish → 出栈 + unbind(done) + reap）。
//
// 与 ProposeFinish（EXEC 提议收口的终态 gate）的区别：gate 只在 EXEC 主动提议
// 收口时运行；本方法是"TL 在普通评审回合里直接判 done"的收口路径。二者共用
// 同一收口语义，且互不重复——gate 已收口后 ActiveGoal 为空，这里即空操作。
//
// 只有 verdict_done 收口；verdict_not_done / correct / checkpoint_ok 不动 goal
// （让位给 EXEC 继续），escalate_human 保持 active 转人工（调用方按 §5 处理）。
// 返回 true 表示本次调用真的把 goal 收口出栈。
func (s *Supervisor) CloseTopGoalOnTerminal(ctx context.Context, directive TLDirective) (bool, error) {
	if directive.Kind != DirectiveVerdictDone {
		return false, nil
	}
	if _, ok := s.ctl.ActiveGoal(); !ok {
		return false, nil
	}
	if _, err := s.ctl.Finish(ctx, FinishRequest{
		Reason: directive.Summary(),
		Result: boundedFinishResult(directive.Content),
	}); err != nil {
		return false, err
	}
	s.unbindIfTerminal("done")
	return true, nil
}

// boundedFinishResult 把裁决正文压到 Finish 允许的进度长度（result 上限
// MaxProgressRunes）；超出截断，收口不因文案过长失败。
func boundedFinishResult(content string) string {
	if len([]rune(content)) > MaxProgressRunes {
		return string([]rune(content)[:MaxProgressRunes])
	}
	return content
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

	// 三段式（2026-09-29）：评估在 s.mu 之外跑（预筛同样是"回合"，同样不能把 s.mu
	// 横跨模型调用）。
	directive, err := s.runRound(ctx, "gate:approval_prescreen", TLEvalSignal{
		Kind: SignalApprovalAsked, Source: "approval_prescreen",
		Detail: boundedProposalDetail(summary), Ref: request.Ref,
	})
	if err != nil {
		// A 语义（2026-09-29）：已有回合在飞 → 不排队，直接转人工（审批侧默认拒绝
		// 兜底不变；在飞的那一轮仍在用 peer，因此不 reap）。
		if errors.Is(err, ErrRoundInFlight) {
			return ApprovalVerdict{Outcome: ApprovalOutcomeEscalate,
				Message: fmt.Sprintf("已有 ADVISOR 回合在进行中（不排队等待），转人工审批: %v", err)}, nil
		}
		// B 语义：回合期间 goal 被收口/更换 → 裁决已丢弃，不拿它去放行一次审批。
		if errors.Is(err, ErrRoundGoalGone) {
			s.unbindIfTerminal("goal_gone_during_round")
			return ApprovalVerdict{Outcome: ApprovalOutcomeEscalate,
				Message: fmt.Sprintf("b 回合期间 goal 已收口/更换（裁决已丢弃），转人工审批: %v", err)}, nil
		}
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
