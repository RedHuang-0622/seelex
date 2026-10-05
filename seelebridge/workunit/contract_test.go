package workunit

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/seelebridge/worktree"
)

// TestClassifyFinishIsTheOneClassification：三层唯一一份收尾分类的判据表。
// 修复类口径：node 侧原有的三支（uncommitted / merge_blocked / 其他）在这份实现里
// 必须逐条对得上，否则"同一份分类"就只是换了个人抄。
func TestClassifyFinishIsTheOneClassification(t *testing.T) {
	uncommitted := fmt.Errorf("worktree %q: subagent left uncommitted changes: %w", "exec-wi-1", worktree.ErrUncommittedChanges)
	blocked := fmt.Errorf("worktree %q: merge blocked: %w", "exec-wi-1", worktree.ErrMergeBlockedByMain)
	other := errors.New("git merge 撞了别的错")

	cases := []struct {
		name     string
		result   Result
		mergeErr error
		want     OutcomeKind
	}{
		{"跑完且合进去 → 落定", Result{Summary: "done"}, nil, OutcomeSettled},
		{"跑完但有未提交改动 → 未提交（不判死）", Result{Summary: "done"}, uncommitted, OutcomeUncommitted},
		{"跑完但主工作区挡路 → 挡路（不判死）", Result{Summary: "done"}, blocked, OutcomeMergeBlocked},
		{"跑完但合并撞别的错 → 判死", Result{Summary: "done"}, other, OutcomeFailed},
		{"这一轮就失败了 → 判死", Result{Err: errors.New("模型超时")}, nil, OutcomeFailed},
		{"这一轮失败 + 合并挡路 → 仍判死（runErr 主导）", Result{Err: errors.New("模型超时")}, blocked, OutcomeFailed},
		{"两种收尾失败同时具备 → 未提交优先（先判现场没交）", Result{}, errors.Join(worktree.ErrUncommittedChanges, worktree.ErrMergeBlockedByMain), OutcomeUncommitted},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := ClassifyFinish(testCase.result, testCase.mergeErr)
			if got.Kind != testCase.want {
				t.Fatalf("ClassifyFinish 分类 = %s，想要 %s（notice: %s）", got.Kind, testCase.want, got.Notice)
			}
			if strings.TrimSpace(got.Notice) == "" {
				t.Fatal("每一次分类都要带一句给人和看板看的说明（Notice 不得为空）")
			}
		})
	}
}

// TestClassifyFinishNoticeIsBounded：说明要进回执与看板行，必须有界。
func TestClassifyFinishNoticeIsBounded(t *testing.T) {
	long := errors.New(strings.Repeat("很长的原因", 500))
	got := ClassifyFinish(Result{}, long)
	if runes := len([]rune(got.Notice)); runes > outcomeNoticeLimit+1 {
		t.Fatalf("Notice 长度 = %d rune，超过上限 %d（+1 省略号）", runes, outcomeNoticeLimit)
	}
	if !strings.HasSuffix(got.Notice, "…") {
		t.Fatalf("被裁的 Notice 应以省略号结尾：%q", got.Notice)
	}
	if !got.Settled() == (got.Kind == OutcomeSettled) {
		t.Fatal("Settled() 必须只在 OutcomeSettled 时为真")
	}
}

// TestFinishPolicyIsTheOnlyDifference：三层之间唯一的差异点是"什么时候回收"——
// 立刻回收（job / subagent）vs 留给 team_close 扫账本（teammate）。
//
// 两个策略调的是**同一个**析构函数（`Lifecycle.Reclaim`），差别只在调用点；因此断言落在
// "那一个函数被调了几次"上：`Immediate` 恰一次、`AtTeamClose` 零次（它的调用点在整队收口，
// 不在 Finish 这里）。
func TestFinishPolicyIsTheOnlyDifference(t *testing.T) {
	subagent := fakeUnit{kind: KindSubagent}
	host := &fakeLifecycle{}
	if err := (Immediate{}).AfterFinish(context.Background(), host, subagent); err != nil {
		t.Fatalf("Immediate.AfterFinish: %v", err)
	}
	if host.reclaims != 1 {
		t.Fatalf("Immediate 策略应立刻回收现场一次，实际 %d 次", host.reclaims)
	}

	teammate := fakeUnit{kind: KindTeammate}
	other := &fakeLifecycle{}
	if err := (AtTeamClose{}).AfterFinish(context.Background(), other, teammate); err != nil {
		t.Fatalf("AtTeamClose.AfterFinish: %v", err)
	}
	if other.reclaims != 0 {
		t.Fatalf("AtTeamClose 策略不得自己拆现场（回收唯一入口 = team_close），实际回收 %d 次", other.reclaims)
	}

	// 策略是对 Unit 的行为，不看 Kind：同一份策略在两层的语义必须一致，
	// 否则差异就藏进实现里了（这正是契约要消灭的漂移）。
	if _, ok := any(Immediate{}).(FinishPolicy); !ok {
		t.Fatal("Immediate 必须实现 FinishPolicy")
	}
	if _, ok := any(AtTeamClose{}).(FinishPolicy); !ok {
		t.Fatal("AtTeamClose 必须实现 FinishPolicy")
	}
	if subagent.Policy() == nil || teammate.Policy() == nil {
		t.Fatal("策略是 Unit 的一部分（Unit.Policy()）：读数必须答得出它")
	}
}

// fakeUnit 是读数替身：契约里 Unit 的全部面（只有身份与策略，没有逻辑）。
type fakeUnit struct {
	kind Kind
}

func (f fakeUnit) Kind() Kind           { return f.kind }
func (f fakeUnit) ID() string           { return "fake-" + string(f.kind) }
func (f fakeUnit) SessionPath() string  { return "sess-fake" }
func (f fakeUnit) Policy() FinishPolicy { return Immediate{} }
func (f fakeUnit) Owns() Ownership      { return Ownership{} }

var _ Unit = fakeUnit{}

// fakeLifecycle 是父实现替身：只记"Reclaim 被调了几次"（策略断言据此分辨"现在回收"与
// "留给收口"）。
type fakeLifecycle struct {
	reclaims int
}

func (f *fakeLifecycle) Begin(context.Context, Unit) (Scene, error) { return Scene{}, nil }

func (f *fakeLifecycle) Finish(context.Context, Unit, Result, error) (Outcome, error) {
	return Outcome{Kind: OutcomeSettled, Notice: "跑完待验收"}, nil
}

func (f *fakeLifecycle) Reclaim(context.Context, Unit) error { f.reclaims++; return nil }

func (f *fakeLifecycle) Recover(context.Context, Unit) (Resume, error) { return Resume{}, nil }

func (f *fakeLifecycle) AlreadySettled(context.Context, Unit) (bool, error) { return false, nil }

func (f *fakeLifecycle) Notice(Outcome) string { return "" }

var _ Lifecycle = (*fakeLifecycle)(nil)
