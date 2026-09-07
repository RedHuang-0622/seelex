package goal

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// ---- 测试替身 ----

// stubEvaluator 是 TLEvaluator 测试替身：记录最近嵌入并按队列返回指令。
type stubEvaluator struct {
	mu        sync.Mutex
	lastEmbed *TLSessionEmbed
	replies   []TLDirective
	count     int
}

func newStubEvaluator(replies ...TLDirective) *stubEvaluator {
	return &stubEvaluator{replies: replies}
}

func (s *stubEvaluator) Evaluate(_ context.Context, embed TLSessionEmbed) (TLDirective, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copyEmbed := embed
	s.lastEmbed = &copyEmbed
	s.count++
	if len(s.replies) == 0 {
		return TLDirective{Kind: DirectiveCheckpointOK, Content: "ok"}, nil
	}
	reply := s.replies[0]
	s.replies = s.replies[1:]
	return reply, nil
}

func (s *stubEvaluator) last() *TLSessionEmbed {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastEmbed
}

func (s *stubEvaluator) evalCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.count
}

// newTestSupervisor 构造带 stub 评估器的监督器。
func newTestSupervisor(t *testing.T, ctl *Controller, window int, replies ...TLDirective) (*Supervisor, *stubEvaluator) {
	t.Helper()
	stub := newStubEvaluator(replies...)
	cfg := TechLeaderConfig{Enabled: true, EvalWindow: window}
	sup := NewSupervisor(ctl, stub, cfg)
	return sup, stub
}

// ---- 契约校验 ----

func TestA2AContractValidation(t *testing.T) {
	if err := (TLEvalSignal{Kind: "bogus"}).Validate(); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("非法 signal kind 应报错: %v", err)
	}
	if err := (TLEvalSignal{Kind: SignalTurnCompleted}).Validate(); err != nil {
		t.Fatalf("合法 signal 应通过: %v", err)
	}
	long := strings.Repeat("x", MaxSignalDetailRunes+1)
	if err := (TLEvalSignal{Kind: SignalTurnCompleted, Detail: long}).Validate(); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("超长 signal detail 应报错: %v", err)
	}

	if err := (TLDirective{Kind: "bogus"}).Validate(); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("非法 directive kind 应报错: %v", err)
	}
	if err := (TLDirective{Kind: DirectiveCorrect, Content: strings.Repeat("x", MaxDirectiveRunes+1)}).Validate(); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("超长 directive 应报错: %v", err)
	}
	if err := (TLDirective{Kind: DirectiveCorrect, Content: "x", Severity: "P9"}).Validate(); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("非法 severity 应报错: %v", err)
	}
	if err := (TLDirective{Kind: DirectiveCorrect, Content: "x"}).Validate(); err != nil {
		t.Fatalf("合法 directive 应通过: %v", err)
	}

	embed := TLSessionEmbed{SessionTail: make([]TurnBrief, MaxEmbedTail+1)}
	if err := embed.Validate(); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("超长 tail 应报错: %v", err)
	}
	if err := (TLSessionEmbed{Goal: GoalFrame{ID: "g-1"}}).Validate(); err != nil {
		t.Fatalf("带 goal 帧的嵌入应通过: %v", err)
	}
	if err := (TLSessionEmbed{}).Validate(); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("缺 goal 帧应报错（防遗忘约束）: %v", err)
	}

	// 关键信号分类（design §5.2）。
	for _, kind := range []SignalKind{SignalContextCompacted, SignalBudgetWarning, SignalApprovalAsked, SignalTerminalProposal} {
		if !IsCriticalSignal(kind) {
			t.Fatalf("%s 应为关键信号", kind)
		}
	}
	if IsCriticalSignal(SignalTurnCompleted) || IsCriticalSignal(SignalStepCheckpoint) {
		t.Fatal("turn_completed/step_checkpoint 不应为关键信号")
	}
}

// ---- TechLeaderMailbox 有界队列 ----

func TestMailboxBoundedQueuesAndOverflow(t *testing.T) {
	mailbox := NewTechLeaderMailbox(2, 3)
	for index := 0; index < 5; index++ {
		mailbox.EnqueueSignal(TLEvalSignal{Kind: SignalTurnCompleted, Source: string(rune('a' + index))})
	}
	if got := mailbox.PendingSignals(); got != 2 {
		t.Fatalf("信号队列应封顶 2, 得 %d", got)
	}
	signals, _ := mailbox.Overflow()
	if signals != 3 {
		t.Fatalf("信号溢出应计数 3, 得 %d", signals)
	}
	// 保留最新两条（丢弃最旧）。
	taken := mailbox.TakeSignals(2)
	if len(taken) != 2 || taken[1].Source != "e" || taken[0].Source != "d" {
		t.Fatalf("应保留最新信号: %+v", taken)
	}

	for index := 0; index < 5; index++ {
		mailbox.PublishDirective(TLDirective{Kind: DirectiveCorrect, Content: "x"})
	}
	if got := mailbox.PendingDirectives(); got != 3 {
		t.Fatalf("指令队列应封顶 3, 得 %d", got)
	}
	_, overflowD := mailbox.Overflow()
	if overflowD != 2 {
		t.Fatalf("指令溢出应计数 2, 得 %d", overflowD)
	}
	if drained := mailbox.DrainDirectives(); len(drained) != 3 {
		t.Fatalf("排空应得 3 条, 得 %d", len(drained))
	}
	if drained := mailbox.DrainDirectives(); len(drained) != 0 {
		t.Fatalf("二次排空应为空（幂等）")
	}
}
