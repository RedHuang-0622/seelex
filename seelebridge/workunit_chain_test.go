package seelebridge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/seelebridge/teamwork"
	"github.com/RedHuang-0622/seelex/seelebridge/worktree"
	"github.com/RedHuang-0622/seelex/seelebridge/workunit"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// ── 三层一致性：同一份脚本跑两遍（第三层是基线，不实现 Unit）────────────────
//
// 契约（`seelebridge/workunit`）说三层是一条**只增不减**的链：job（基线，不实现 Unit）→
// subagent（增现场 + 自己那条会话 + 存储与合并纪律）→ teammate（增调度 + 装配 + 现场与会话
// 归 team）。这个文件跑的是**同一份**脚本——"建 → 跑 → 收尾 → 回收 → 恢复"——分别在
// subagent 层与 teammate 层的**真实实现**上跑一遍，断言它们回答同一件事。
//
// 为什么必须有这个文件（而不是只看两层各自的用例）：两层的实现分散在不同文件里，一旦有人
// 在某一层"顺手再判一次"（第二份收尾分类、第二份在跑词表、第二份恢复说明），两层各自的用例
// **都会绿**——它们只证明自己的实现自洽。跨层用例才问得动"这两份是不是同一份"。
//
// 各层自己的机制细节由各自的用例守着（这里的脚本只问共同的那件事）：
//
//	subagent：TestNodeWorkUnitSinglePath（建→跑→收尾→回收→恢复）/ TestNodeFinishClassificationIsOneTable（workunit_node_test.go）
//	teammate：TestTeamUnitFinishPolicyKeepsTheScene / TestTeamUnitRecoverMarksInterruptedAndInjectsTheNoteOnce（workunit_team_test.go）
//	契约本体：contract_test.go / classify_test.go / session_test.go
//
// 这里刻意不断言机制（谁建了目录、谁写了哪条记录），只断言**两层必须一致的四件事**：
// 收尾分类只有一份、策略是唯一允许的层间差异、恢复说明只有一族、在跑词表只有一份。

// chainStubReadings 是契约层的最小**读数**桩（Unit）：只有身份与策略。契约里 Unit 是层的
// 读数（没有逻辑、不持端口），只有 Lifecycle 才是那份唯一实现——桩的形状因此跟着分两半。
type chainStubReadings struct{ kind workunit.Kind }

func (u chainStubReadings) Kind() workunit.Kind           { return u.kind }
func (u chainStubReadings) ID() string                    { return "chain-stub" }
func (u chainStubReadings) SessionPath() string           { return "chain-stub-session" }
func (u chainStubReadings) Policy() workunit.FinishPolicy { return workunit.Immediate{} }
func (u chainStubReadings) Owns() workunit.Ownership      { return workunit.Ownership{} }

var _ workunit.Unit = chainStubReadings{}

// chainStubLifecycle 是父实现替身：只数 Reclaim 被调用几次。用它把"策略"从"实现"里分出
// 来单独断言——策略是三层唯一允许出现的差异点，且两个策略调的是**同一个**析构函数
// （`Lifecycle.Reclaim`），不该藏在某个实现的分支里。
type chainStubLifecycle struct{ reclaims int }

func (l *chainStubLifecycle) Begin(context.Context, workunit.Unit) (workunit.Scene, error) {
	return workunit.Scene{NodeID: "chain-stub"}, nil
}

func (l *chainStubLifecycle) Finish(context.Context, workunit.Unit, workunit.Result, error) (workunit.Outcome, error) {
	return workunit.Outcome{Kind: workunit.OutcomeSettled, Notice: "跑完待验收"}, nil
}

func (l *chainStubLifecycle) Reclaim(context.Context, workunit.Unit) error {
	l.reclaims++
	return nil
}

func (l *chainStubLifecycle) Recover(context.Context, workunit.Unit) (workunit.Resume, error) {
	return workunit.Resume{}, nil
}

func (l *chainStubLifecycle) AlreadySettled(context.Context, workunit.Unit) (bool, error) {
	return false, nil
}

func (l *chainStubLifecycle) Notice(workunit.Outcome) string { return "" }

var _ workunit.Lifecycle = (*chainStubLifecycle)(nil)

// chainPolicyOf 取一个 Unit 声明的收尾策略。策略是**契约的一部分**（`Unit.Policy()`），
// 因此这里直接读——不再写"实现各自暴露它"的类型断言（那是合同没抽对的证据）。
func chainPolicyOf(t *testing.T, unit workunit.Unit) workunit.FinishPolicy {
	t.Helper()
	policy := unit.Policy()
	if policy == nil {
		t.Fatalf("%T 的收尾策略是 nil", unit)
	}
	return policy
}

// chainClassifyRows 是脚本的输入表：同一份（这一轮结果，合并结果）喂给两层，两层的可观察
// 结果必须落在同一类上。node* 列是 subagent 层的可观察效果，team* 列是 teammate 层的。
type chainClassifyRow struct {
	name      string
	runErr    error
	mergeErr  error
	want      workunit.OutcomeKind
	nodePhase string // 节点侧应补记的阶段（"" = 不补记）
	nodeDead  bool   // 节点侧应判死（Run 返回非空 error）
	teamState string // 工作项终态
	unmerged  bool   // 计划里应留下"未合并"标记（收口闸门据此拒拆现场）
}

func chainClassifyRows() []chainClassifyRow {
	uncommitted := fmt.Errorf("worktree %q: 现场有未提交改动: %w", "wu-1", worktree.ErrUncommittedChanges)
	blocked := fmt.Errorf("worktree %q: 合并被主工作区挡住: %w", "wu-1", worktree.ErrMergeBlockedByMain)
	return []chainClassifyRow{
		{"干净落定", nil, nil, workunit.OutcomeSettled, "", false, sessionstore.TeamworkItemReview, false},
		{"这一轮跑失败", errors.New("provider 402"), nil, workunit.OutcomeFailed, "", true, sessionstore.TeamworkItemFailed, false},
		{"现场有未提交改动", nil, uncommitted, workunit.OutcomeUncommitted, "worktree_unmerged", false, sessionstore.TeamworkItemReview, true},
		{"主工作区挡路", nil, blocked, workunit.OutcomeMergeBlocked, "merge_blocked", false, sessionstore.TeamworkItemReview, true},
		{"其他合并错误", nil, errors.New("rebase 冲突"), workunit.OutcomeFailed, "", true, sessionstore.TeamworkItemFailed, false},
		{"两种收尾失败同时具备 → 未提交优先", nil,
			errors.Join(worktree.ErrUncommittedChanges, worktree.ErrMergeBlockedByMain),
			workunit.OutcomeUncommitted, "worktree_unmerged", false, sessionstore.TeamworkItemReview, true},
	}
}

// TestChainOneFinishScriptForBothLayers 是跨层主用例：**同一份输入表**分别喂给 subagent 层
// （真实 `AgentNode.Run` 的收尾段，见 runNodeFinish）与 teammate 层（真实
// `Coordinator.SettleWorkItemOutcome`，见 newSettleFixture），断言两层的可观察结果都与契约
// 的唯一一份分类一致——**判死只有一种原因**，"没合进去"在两层都只是警告 + 留住现场。
func TestChainOneFinishScriptForBothLayers(t *testing.T) {
	for _, row := range chainClassifyRows() {
		t.Run(row.name, func(t *testing.T) {
			ctx := context.Background()
			contract := workunit.ClassifyFinish(workunit.Result{Err: row.runErr}, row.mergeErr)
			if contract.Kind != row.want {
				t.Fatalf("契约分类 = %s，want %s（本行是脚本的基准，改了它整条链都跟着变）", contract.Kind, row.want)
			}

			// ── 左腿：subagent 层 ──────────────────────────────────────────
			effect := runNodeFinish(t, row.runErr, row.mergeErr)
			nodeKind := workunit.OutcomeSettled
			switch {
			case effect.err != nil:
				nodeKind = workunit.OutcomeFailed
			case effect.phaseReported("worktree_unmerged"):
				nodeKind = workunit.OutcomeUncommitted
			case effect.phaseReported("merge_blocked"):
				nodeKind = workunit.OutcomeMergeBlocked
			}
			if nodeKind != contract.Kind {
				t.Errorf("subagent 层分类 = %s，契约 = %s（这一层不许自己再判一次）", nodeKind, contract.Kind)
			}
			if effect.phaseReported(row.nodePhase) && row.nodePhase == "" {
				t.Errorf("subagent 层补记了不该补的阶段：%v", effect.phases)
			}
			if row.nodePhase != "" && !effect.phaseReported(row.nodePhase) {
				t.Errorf("subagent 层没补记阶段 %q：%v", row.nodePhase, effect.phases)
			}
			if (effect.err != nil) != row.nodeDead {
				t.Errorf("subagent 层判死 = %v，want %v（Run 的 error 是「这轮失败」的唯一出口）", effect.err != nil, row.nodeDead)
			}
			// 现场释放时机：只有"落定"才释放——未提交/挡路都要留住现场（现场是人的资产）。
			if effect.released != contract.Settled() {
				t.Errorf("现场释放 = %v，契约 Settled() = %v（未合进去的现场不许被框架悄悄拆掉）", effect.released, contract.Settled())
			}
			// 非落定的结论必须把"没合进去 + 怎么办"写进交付出去的产出（回合之外发生的事，
			// 人只能从产出里看到）；判死那两类走 Run 的 error，不进产出。
			if notMerged := contract.Kind == workunit.OutcomeUncommitted || contract.Kind == workunit.OutcomeMergeBlocked; notMerged {
				if !strings.Contains(effect.result, "收尾警告") {
					t.Errorf("subagent 层产出里没有收尾警告：%q", effect.result)
				}
			}

			// ── 右腿：teammate 层 ─────────────────────────────────────────
			coordinator, store, _, queue := newSettleFixture(t, row.mergeErr)
			outcome, err := coordinator.SettleWorkItemOutcome(ctx, teamwork.WorkerRequest{
				MainSessionID: "sess-1", WorkItemID: "wi-1",
			}, row.runErr)
			if err != nil {
				t.Fatalf("teammate 层收尾失败：%v", err)
			}
			if outcome.Kind != contract.Kind {
				t.Errorf("teammate 层分类 = %s，契约 = %s（这一层不许自己再判一次）", outcome.Kind, contract.Kind)
			}
			plan, err := store.ReadPlan(ctx, teamTestKey)
			if err != nil {
				t.Fatal(err)
			}
			if got := plan.Milestones[0].Items[0].StatusOrPending(); got != row.teamState {
				t.Errorf("teammate 层工作项终态 = %s，want %s（未提交/挡路都进 review，不许判死）", got, row.teamState)
			}
			if _, unmerged := plan.State.Unmerged["wi-1"]; unmerged != row.unmerged {
				t.Errorf("未合并标记 = %v，want %v（收口闸门据此判定「这份现场还能不能拆」）", unmerged, row.unmerged)
			}
			if len(queue.texts) != 1 {
				t.Errorf("teammate 层尾插 %d 条，want 恰好 1 条（有界回执）：%v", len(queue.texts), queue.texts)
			}
		})
	}
}

// TestChainFinishPolicyIsTheOnlyLayerDifference 钉住"层间差异只在策略上"：同一份 Finish 之后，
// Immediate（现场临时）立刻回收，AtTeamClose（现场归 team）什么都不做——回收唯一入口 = team_close。
func TestChainFinishPolicyIsTheOnlyLayerDifference(t *testing.T) {
	rows := []struct {
		name   string
		policy workunit.FinishPolicy
		want   int
	}{
		{"subagent / Immediate：收尾即回收", workunit.Immediate{}, 1},
		{"teammate / AtTeamClose：留给 team_close 统一回收", workunit.AtTeamClose{}, 0},
	}
	for _, row := range rows {
		host := &chainStubLifecycle{}
		stub := chainStubReadings{kind: workunit.KindSubagent}
		if err := row.policy.AfterFinish(context.Background(), host, stub); err != nil {
			t.Fatalf("%s：AfterFinish 返回错误：%v", row.name, err)
		}
		if host.reclaims != row.want {
			t.Errorf("%s：Reclaim 被调用 %d 次，想要 %d 次", row.name, host.reclaims, row.want)
		}
	}
}

// TestChainTwoRealLayersDeclareTheirPolicy 用**两个真实读数**（零值、只读声明）核对上一条：
// subagent 单元声明 Immediate、teammate 单元声明 AtTeamClose，且各自的 Kind 正确。
//
// 为什么用零值就能断言：策略与 Kind 是本层的**常量事实**，不该依赖任何运行期状态；能靠零值
// 回答的层间差异，就必须是声明式的（实现里出现按层判断的 if，说明契约没抽对）。
func TestChainTwoRealLayersDeclareTheirPolicy(t *testing.T) {
	units := []struct {
		name   string
		unit   workunit.Unit
		kind   workunit.Kind
		policy any
	}{
		{"subagent 层读数", nodeUnitReadings{}, workunit.KindSubagent, workunit.Immediate{}},
		{"teammate 层读数", teamUnitReadings{}, workunit.KindTeammate, workunit.AtTeamClose{}},
	}
	for _, row := range units {
		if got := row.unit.Kind(); got != row.kind {
			t.Errorf("%s：Kind() = %s，想要 %s", row.name, got, row.kind)
		}
		policy := chainPolicyOf(t, row.unit)
		if got, want := policy, row.policy; got != want {
			t.Errorf("%s：Policy() = %#v，想要 %#v", row.name, got, want)
		}
	}
	// 注册点只转发读数：nil 接收者也能答出本层的常量策略（不碰任何运行期状态）。
	if got, want := any((*nodeWorkUnit)(nil).FinishPolicy()), any(workunit.Immediate{}); got != want {
		t.Errorf("nodeWorkUnit.FinishPolicy() = %#v，想要 %#v", got, want)
	}
	if got, want := any((*teamUnit)(nil).FinishPolicy()), any(workunit.AtTeamClose{}); got != want {
		t.Errorf("teamUnit.FinishPolicy() = %#v，想要 %#v", got, want)
	}
}

// TestChainEveryLayerCoversEveryOutcome 钉住"契约加了分类，两层都得接"：teammate 层的记录
// 终态映射必须覆盖契约**全部** OutcomeKind（只有 Failed 记 failed），subagent 层反过来必须把
// 分类**原样**交回（它的收尾段直接 switch 契约的 Kind，见 agent_node.go）。
//
// 这一条防的是"分层漂移"里最隐蔽的一种：契约新增第五类，某层忘了接，于是它落到 default 分支
// 被当成判死——两层的用例各自都绿。
func TestChainEveryLayerCoversEveryOutcome(t *testing.T) {
	kinds := []workunit.OutcomeKind{
		workunit.OutcomeSettled, workunit.OutcomeUncommitted,
		workunit.OutcomeMergeBlocked, workunit.OutcomeFailed,
	}
	for _, kind := range kinds {
		status := teamUnitStatusFor(kind)
		if status != teamUnitStatusDone && status != teamUnitStatusFailed {
			t.Errorf("teammate 层没有为分类 %s 定义记录终态（新增分类忘了接）", kind)
		}
		if (status == teamUnitStatusFailed) != (kind == workunit.OutcomeFailed) {
			t.Errorf("teammate 层把 %s 记成 %q：只有真的失败才记 failed", kind, status)
		}
	}
	// subagent 层：每一个非落定类都必须有阶段名/警告这条落地路径——用输入表核对一遍，
	// 保证"新增分类"不会在这层悄悄变成"什么都不做"。
	for _, row := range chainClassifyRows() {
		if row.want == workunit.OutcomeSettled {
			continue
		}
		if row.nodePhase == "" && !row.nodeDead {
			t.Errorf("subagent 层对分类 %s 既没补阶段也没判死：新增分类必须落到一个可观察效果上", row.want)
		}
	}
}

// TestChainRecoveryNoteIsOneFamily 钉住"恢复说明只有一族"：任何一层、任何 kind 的说明都以
// `族前缀 + " " + kind` 开头、用同一个注入 role，并且把记录里的事实（节点/会话/目标/状态/
// 阶段/结论/错误/现场）摆出来——审计里两层因此是**同一种东西**，而不是两份自造说明。
func TestChainRecoveryNoteIsOneFamily(t *testing.T) {
	if workunit.RecoveryNoteRole != "system" {
		t.Errorf("恢复说明的注入 role = %q，想要 system（编排事实，不是模型发言）", workunit.RecoveryNoteRole)
	}
	// subagent 层既有的稳定前缀必须是这一族里"kind = subagent"的那一支（强断言：不是同前缀，
	// 是**正好相等**——既有的识别口径一个字符都不能漂）。
	if want := workunit.RecoveryNotePrefix + " " + string(workunit.KindSubagent); subagentRecoveryNotePrefix != want {
		t.Errorf("subagent 前缀 = %q，想要 %q", subagentRecoveryNotePrefix, want)
	}
	record := sessionstore.NodeSessionRecord{
		NodeID:    "exec-wi-1",
		SessionID: "node-sess-1",
		Goal:      "把收尾口径接进契约",
		Status:    workunit.StatusRunning.String(),
		Summary:   "改到一半",
		Worktree:  sessionstore.NodeWorktreeRecord{Path: `C:\wt\exec-wi-1`, Branch: "seelex/exec-wi-1"},
	}
	facts := []string{"exec-wi-1", "node-sess-1", "把收尾口径接进契约", workunit.StatusRunning.String(), "改到一半", "seelex/exec-wi-1"}
	for _, kind := range []workunit.Kind{workunit.KindSubagent, workunit.KindTeammate} {
		note := workunit.RecoveryNote(kind, record)
		if want := workunit.RecoveryNotePrefix + " " + string(kind); !strings.HasPrefix(note, want) {
			t.Errorf("%s：说明不以族前缀 %q 开头：%q", kind, want, note)
		}
		for _, fact := range facts {
			if !strings.Contains(note, fact) {
				t.Errorf("%s：说明里丢了事实 %q：%q", kind, fact, note)
			}
		}
		if runes := len([]rune(note)); runes > 2000 {
			t.Errorf("%s：说明 %d rune，超出有界口径（它要进 system 注入）", kind, runes)
		}
	}
}

// TestChainInFlightVocabularyHasOneSource 钉住"在跑词表只有一份"（判中断的依据）：teammate
// 侧只转调契约，subagent 侧的 `Recover` 也走 `workunit.InFlight`（见 workunit_node.go）——
// 两层的 Recover 因此对"记录说在跑而本进程已无执行面"给出同一判断。
func TestChainInFlightVocabularyHasOneSource(t *testing.T) {
	rows := []struct {
		status string
		want   bool
	}{
		{workunit.StatusQueued.String(), true},
		{workunit.StatusRunning.String(), true},
		{" running ", true},
		{"done", false},
		{"failed", false},
		{"completed", false},
		{"", false},
	}
	for _, row := range rows {
		if got := workunit.InFlight(row.status); got != row.want {
			t.Errorf("workunit.InFlight(%q) = %v，想要 %v", row.status, got, row.want)
		}
	}
}
