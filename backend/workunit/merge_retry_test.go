package workunit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// retryScene 是现场端口的测试替身：只记录 merge 次数，并按脚本返回结果。
type retryScene struct {
	mu         sync.Mutex
	mergeCalls int
	merge      func(attempt int) error
	released   []string
}

func (s *retryScene) BeginScene(context.Context, Ownership, string, string) (Scene, error) {
	return Scene{}, nil
}

func (s *retryScene) MergeScene(context.Context, Ownership, string, string) error {
	s.mu.Lock()
	s.mergeCalls++
	attempt := s.mergeCalls
	script := s.merge
	s.mu.Unlock()
	if script == nil {
		return nil
	}
	return script(attempt)
}

func (s *retryScene) ReleaseScene(nodeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.released = append(s.released, nodeID)
}

func (s *retryScene) AdoptScenes(context.Context, string) error { return nil }

func (s *retryScene) SceneRegistered(string) bool { return true }

func (s *retryScene) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mergeCalls
}

// retryUnits 是零归属的编排端口：本组用例只走 subagent 那一侧。
type retryUnits struct{}

func (retryUnits) Owned(context.Context, string, Ownership) bool { return false }

func (retryUnits) Settled(context.Context, string, string) (bool, error) { return false, nil }

func (retryUnits) SettleUnit(context.Context, string, Ownership, error, error) (Outcome, error) {
	return Outcome{}, nil
}

func (retryUnits) ReclaimUnit(context.Context, string, string) error { return nil }

func (retryUnits) RecoverUnits(context.Context, string) (Resume, error) { return Resume{}, nil }

// retryRecords 是"没有记录"的账本：AlreadySettled 因此把现场是否在册当作判据。
type retryRecords struct {
	mu      sync.Mutex
	cleared int
}

func (*retryRecords) Status(context.Context, Ownership, string, string) (string, bool, error) {
	return "", false, nil
}

func (*retryRecords) Readout(context.Context, Ownership, string, string) (Resume, error) {
	return Resume{}, nil
}

func (*retryRecords) SaveRecord(context.Context, Ownership, string, string, string, string, string) {
}

func (*retryRecords) SettleRecord(context.Context, Ownership, string, string, Outcome) {}

func (r *retryRecords) Clear(context.Context, Ownership, string, string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cleared++
	return nil
}

type retryUnit struct{ id string }

func (retryUnit) Kind() Kind { return KindSubagent }

func (u retryUnit) ID() string { return u.id }

func (retryUnit) SessionPath() string { return "sess-retry" }

func (retryUnit) Policy() FinishPolicy { return Immediate{} }

func (retryUnit) Owns() Ownership { return Ownership{} }

func blockedErr(attempt int) error {
	return fmt.Errorf("worktree %q: merge blocked (attempt %d): %w", "impl", attempt, ErrMergeBlockedByMain)
}

// TestHostFinishRetriesMergeBlockedByMainWithinBudget 钉住新边界：挡路的等待与重试
// 属于生命周期实现（Host），现场端口只被反复问"这一份合回去了吗"。
func TestHostFinishRetriesMergeBlockedByMainWithinBudget(t *testing.T) {
	var phases []string
	scenes := &retryScene{merge: func(attempt int) error {
		if attempt <= 2 {
			return blockedErr(attempt)
		}
		return nil
	}}
	records := &retryRecords{}
	host := NewHost(scenes, retryUnits{}, records, WithMergeRetry(MergeRetry{
		Budget:   time.Second,
		Interval: time.Millisecond,
		Observe: func(_ context.Context, nodeID, phase string) {
			phases = append(phases, nodeID+":"+phase)
		},
	}))

	outcome, err := host.Finish(context.Background(), retryUnit{id: "impl"}, Result{}, nil)
	if err != nil {
		t.Fatalf("预算内自愈后不应报错：%v", err)
	}
	if outcome.Kind != OutcomeSettled {
		t.Fatalf("outcome = %+v, want settled", outcome)
	}
	if got := scenes.calls(); got != 3 {
		t.Fatalf("merge 尝试次数 = %d, want 3（2 次被挡 + 1 次成功）", got)
	}
	if len(phases) != 2 || !strings.HasSuffix(phases[0], ":"+MergePhaseAwaiting) {
		t.Fatalf("每次等待都必须报出 %s，实际 %v", MergePhaseAwaiting, phases)
	}
	if len(scenes.released) != 1 {
		t.Fatalf("落定后按 Immediate 策略回收一次，实际 %v", scenes.released)
	}
	if records.cleared != 1 {
		t.Fatalf("落定后会话记录应清一次，实际 %d", records.cleared)
	}
}

// TestHostFinishDoesNotRetryDefinitiveMergeFailure 钉住"只有挡路才重试"：
// rebase/merge 冲突之类的确定性失败只问现场一次，分类仍是失败。
func TestHostFinishDoesNotRetryDefinitiveMergeFailure(t *testing.T) {
	definitive := errors.New("merge conflict in main.go: resolve conflicts and git merge --abort")
	scenes := &retryScene{merge: func(int) error { return definitive }}
	host := NewHost(scenes, retryUnits{}, &retryRecords{}, WithMergeRetry(MergeRetry{
		Budget:   time.Second,
		Interval: time.Millisecond,
	}))

	outcome, err := host.Finish(context.Background(), retryUnit{id: "impl"}, Result{}, nil)
	if !errors.Is(err, definitive) {
		t.Fatalf("确定性失败原文必须交回调用方，got %v", err)
	}
	if IsMergeBlockedByMain(err) {
		t.Fatalf("确定性失败不得被判成可重试的挡路：%v", err)
	}
	if got := scenes.calls(); got != 1 {
		t.Fatalf("确定性失败只许试一次，实际 %d 次", got)
	}
	if outcome.Kind != OutcomeFailed {
		t.Fatalf("outcome = %+v, want failed", outcome)
	}
}

// TestHostFinishStopsRetryWhenContextCancelled：等待期间取消必须立刻收口为
// ErrMergeBlockedByMain（现场保留），不许把调用方挂到预算耗尽。
func TestHostFinishStopsRetryWhenContextCancelled(t *testing.T) {
	firstAttempt := make(chan struct{}, 1)
	scenes := &retryScene{merge: func(int) error {
		select {
		case firstAttempt <- struct{}{}:
		default:
		}
		return blockedErr(1)
	}}
	host := NewHost(scenes, retryUnits{}, &retryRecords{}, WithMergeRetry(MergeRetry{
		Budget:   time.Minute,
		Interval: 5 * time.Millisecond,
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := host.Finish(ctx, retryUnit{id: "impl"}, Result{}, nil)
		done <- err
	}()
	select {
	case <-firstAttempt:
	case <-time.After(time.Second):
		t.Fatal("merge 没有被尝试")
	}
	cancel()
	select {
	case err := <-done:
		if !IsMergeBlockedByMain(err) {
			t.Fatalf("取消后必须收口为挡路族错误，got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("取消后 Host 没有收口")
	}
}

// TestHostFinishReportsBlockedBudgetWithRemedy：预算耗尽仍被挡住 → 交回带
// ErrMergeBlockedByMain 的分类错误，并带上人能照着做的处置办法。
func TestHostFinishReportsBlockedBudgetWithRemedy(t *testing.T) {
	scenes := &retryScene{merge: func(attempt int) error { return blockedErr(attempt) }}
	host := NewHost(scenes, retryUnits{}, &retryRecords{}, WithMergeRetry(MergeRetry{
		Budget:   20 * time.Millisecond,
		Interval: 2 * time.Millisecond,
	}))

	outcome, err := host.Finish(context.Background(), retryUnit{id: "impl"}, Result{}, nil)
	if !IsMergeBlockedByMain(err) {
		t.Fatalf("err = %v, want IsMergeBlockedByMain", err)
	}
	if outcome.Kind != OutcomeMergeBlocked {
		t.Fatalf("outcome = %+v, want merge blocked", outcome)
	}
	if !strings.Contains(err.Error(), "先提交或暂存主工作区的在途改动") {
		t.Fatalf("预算耗尽的错误必须带处置办法：%v", err)
	}
	if scenes.calls() < 2 {
		t.Fatalf("预算内必须真的重试过：mergeCalls = %d", scenes.calls())
	}
}

// TestHostFinishClassificationStaysTheOneClassifier：同一份合并事实交给 Host 与
// ClassifyFinish，结论必须一致——Host 只负责等待，不另写一份分类。
func TestHostFinishClassificationStaysTheOneClassifier(t *testing.T) {
	cases := []error{
		nil,
		fmt.Errorf("dirty scene: %w", ErrUncommittedChanges),
		fmt.Errorf("main workspace blocked: %w", ErrMergeBlockedByMain),
		errors.New("rebase failed"),
	}
	for _, mergeErr := range cases {
		host := NewHost(&retryScene{}, retryUnits{}, &retryRecords{})
		outcome, err := host.Finish(context.Background(), retryUnit{id: "impl"}, Result{}, mergeErr)
		want := ClassifyFinish(Result{}, mergeErr)
		if outcome.Kind != want.Kind {
			t.Errorf("mergeErr=%v: Host outcome=%s, ClassifyFinish=%s", mergeErr, outcome.Kind, want.Kind)
		}
		if (mergeErr == nil) != (err == nil) {
			t.Errorf("mergeErr=%v: Host 返回 err=%v，调用方收到的合并事实不一致", mergeErr, err)
		}
	}
}
