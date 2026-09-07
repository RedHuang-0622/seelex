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
	if err := (TLDirective{Kind: DirectiveCorrect, Content: "x", Corr: "corr-1"}).Validate(); err != nil {
		t.Fatalf("合法 directive（带 corr）应通过: %v", err)
	}

	// b 回合输入约束（DS-A2A）：peer 必填、锚点必填、帧 ref_seq 严格递增、记忆有界。
	if err := (TLSessionEmbed{Goal: GoalFrame{ID: "g-1"}}).Validate(); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("缺 peer id 应报错: %v", err)
	}
	if err := (TLSessionEmbed{PeerID: "b-1"}).Validate(); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("缺锚点 goal 应报错（防遗忘约束）: %v", err)
	}
	if err := (TLSessionEmbed{PeerID: "b-1", Goal: GoalFrame{ID: "g-1"}}).Validate(); err != nil {
		t.Fatalf("合法嵌入应通过: %v", err)
	}
	nonMonotonic := TLSessionEmbed{PeerID: "b-1", Goal: GoalFrame{ID: "g-1"},
		Frames: []Frame{{RefSeq: 3}, {RefSeq: 3}}}
	if err := nonMonotonic.Validate(); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("帧 ref_seq 非严格递增应报错: %v", err)
	}
	if err := (TLSessionEmbed{PeerID: "b-1", Goal: GoalFrame{ID: "g-1"},
		TLMemory: make([]string, MaxEmbedRounds+1)}).Validate(); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("TLMemory 超限应报错: %v", err)
	}

	// 关键信号分类。
	for _, kind := range []SignalKind{SignalContextCompacted, SignalBudgetWarning, SignalApprovalAsked, SignalTerminalProposal} {
		if !IsCriticalSignal(kind) {
			t.Fatalf("%s 应为关键信号", kind)
		}
	}
	if IsCriticalSignal(SignalTurnCompleted) || IsCriticalSignal(SignalStepCheckpoint) {
		t.Fatal("turn_completed/step_checkpoint 不应为关键信号")
	}

	// 帧 kind 白名单。
	for _, frame := range []FrameKind{FrameGoalStart, FrameGoalUpdated, FrameStepCheckpoint,
		FrameContextCompacted, FrameApprovalRequested, FrameTerminalProposed} {
		if err := (Frame{Kind: frame, RefSeq: 1}).Validate(); err != nil {
			t.Fatalf("合法帧 %s 应通过: %v", frame, err)
		}
	}
	if err := (Frame{Kind: "bogus", RefSeq: 1}).Validate(); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("非法帧 kind 应报错: %v", err)
	}
}

// ---- TechLeaderMailbox（b→a corr 信封队列） ----

func TestMailboxBoundedDirectivesAndOverflow(t *testing.T) {
	mailbox := NewTechLeaderMailbox(3)
	for index := 0; index < 5; index++ {
		mailbox.PublishDirective(TLDirective{Kind: DirectiveCorrect, Content: "x", Corr: string(rune('a' + index))})
	}
	if got := mailbox.PendingDirectives(); got != 3 {
		t.Fatalf("指令队列应封顶 3, 得 %d", got)
	}
	if overflow := mailbox.Overflow(); overflow != 2 {
		t.Fatalf("指令溢出应计数 2, 得 %d", overflow)
	}
	drained := mailbox.DrainDirectives()
	if len(drained) != 3 || drained[0].Corr != "c" || drained[2].Corr != "e" {
		t.Fatalf("应保留最新 3 条指令（丢最旧）: %+v", drained)
	}
	if drained := mailbox.DrainDirectives(); len(drained) != 0 {
		t.Fatalf("二次排空应为空（corr 幂等消费）")
	}
}
