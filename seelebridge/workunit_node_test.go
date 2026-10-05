package seelebridge

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/workplan/codec"
	frameworknode "github.com/RedHuang-0622/Seele/workplan/core/node"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	seenode "github.com/RedHuang-0622/seelex/seelebridge/node"
	"github.com/RedHuang-0622/seelex/seelebridge/plan"
	"github.com/RedHuang-0622/seelex/seelebridge/worktree"
	"github.com/RedHuang-0622/seelex/seelebridge/workunit"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// ── 1. 收尾分类：契约 OutcomeKind 与节点效果钉在同一张表上 ────────────────────
//
// 接线口径（node/agent_node.go 的收尾段）：分类只从 workunit.ClassifyFinish 取，
// 节点只按分类做三件事——释放现场 / 补记阶段 + 产出警告 / 判死。本用例把
// 「同一输入 → 契约分类」与「同一输入 → 节点效果」两列并排断言：任何一处再长出
// 第二份判据，这张表就会先红（2026-09-11 事故的根因之一就是同一份判断写了两处）。

// nodeFinishFakeAgent 是节点表用例里的模型替身：Chat 直接给结论（它不是
// frameworkSession.Session，因此 AgentNode.Run 走 agent.Chat 这条非流式路径）。
type nodeFinishFakeAgent struct {
	reply string
	err   error
}

func (a nodeFinishFakeAgent) Chat(context.Context, string) (string, error) { return a.reply, a.err }

// nodeFinishFakeFactory 按节点输入造出上面那个替身。
type nodeFinishFakeFactory struct{ agent frameworknode.Agent }

func (f nodeFinishFakeFactory) NewAgent(string) frameworknode.Agent { return f.agent }

// nodeFinishEffect 是 AgentNode.Run 收尾段的可观察效果（释放/阶段/警告/判死）。
type nodeFinishEffect struct {
	result   string
	err      error
	released bool
	finished bool
	phases   []string
}

// phaseReported 报告某个阶段名是否被补记（worktree_unmerged / merge_blocked）。
func (e nodeFinishEffect) phaseReported(phase string) bool {
	for _, reported := range e.phases {
		if reported == phase {
			return true
		}
	}
	return false
}

// runNodeFinish 用假 Deps 驱动一次 AgentNode.Run：现场与收尾都走注入的回调，
// 因此不需要 git 也不需要模型——被观察的正是"收尾分类落地成什么节点效果"。
func runNodeFinish(t *testing.T, runErr, finishErr error) nodeFinishEffect {
	t.Helper()
	effect := nodeFinishEffect{}
	deps := seenode.Deps{
		CurrentPlanBranchBinding: func() plan.PlanBranchBinding { return plan.PlanBranchBinding{} },
		CurrentAgentFactory: func() frameworknode.AgentFactory {
			return nodeFinishFakeFactory{agent: nodeFinishFakeAgent{reply: "节点结论", err: runErr}}
		},
		AppendNodePhase: func(_ context.Context, _ string, status string) {
			effect.phases = append(effect.phases, status)
		},
		BeginNodeWorktree: func(model.NodeScope, string) *worktree.NodeWorktree {
			return &worktree.NodeWorktree{Path: t.TempDir(), Branch: "seelex/wu-1", MainBranch: "main"}
		},
		FinishNodeWorktree: func(context.Context, string, *worktree.NodeWorktree) error {
			effect.finished = true
			return finishErr
		},
		ReleaseNodeWorktree:  func(string) { effect.released = true },
		CompleteSubagentNode: func(string, string, error) {},
		MergeBackIntoParent:  func(*snapshot.ContextSnapshot) *snapshot.ContextSnapshot { return nil },
		NodePromptBlocks:     func(plan.SeelexNodeInput) []seelectx.PromptBlock { return nil },
	}
	spec := codec.NodeSpec[plan.SeelexNodeInput]{
		ID:    "wu-1",
		Input: plan.SeelexNodeInput{ID: "wu-1", Input: "do the thing"},
	}
	effect.result, effect.err = seenode.NewAgentNode(spec, deps).Run(context.Background(), nil)
	return effect
}

// TestNodeFinishClassificationIsOneTable：同一输入 → 契约 OutcomeKind 与
// 节点效果（释放 / 警告 / 判死）逐行对齐。
func TestNodeFinishClassificationIsOneTable(t *testing.T) {
	uncommitted := fmt.Errorf("worktree %q: subagent left uncommitted changes: %w", "wu-1", worktree.ErrUncommittedChanges)
	blocked := fmt.Errorf("worktree %q: merge blocked: %w", "wu-1", worktree.ErrMergeBlockedByMain)
	other := errors.New("git merge 撞了别的错")

	cases := []struct {
		name        string
		runErr      error
		mergeErr    error
		wantKind    workunit.OutcomeKind
		wantRelease bool
		wantPhase   string
		wantWarn    bool
		wantFailed  bool
	}{
		{
			name:     "跑完且合进去 → 落定：释放现场，无警告不判死",
			wantKind: workunit.OutcomeSettled, wantRelease: true,
		},
		{
			name:     "跑完但现场有未提交改动 → 未提交：不释放，记 worktree_unmerged 并警告，不判死",
			mergeErr: uncommitted, wantKind: workunit.OutcomeUncommitted,
			wantPhase: "worktree_unmerged", wantWarn: true,
		},
		{
			name:     "跑完但主工作区挡路 → 挡路：不释放，记 merge_blocked 并警告，不判死",
			mergeErr: blocked, wantKind: workunit.OutcomeMergeBlocked,
			wantPhase: "merge_blocked", wantWarn: true,
		},
		{
			name:     "跑完但合并撞别的错 → 判死",
			mergeErr: other, wantKind: workunit.OutcomeFailed, wantFailed: true,
		},
		{
			name:   "这一轮就失败了 → 判死（收尾不入场，runErr 主导）",
			runErr: errors.New("模型超时"), mergeErr: blocked,
			wantKind: workunit.OutcomeFailed, wantFailed: true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// 第一列：契约分类（唯一一份判据）。
			outcome := workunit.ClassifyFinish(workunit.Result{Err: testCase.runErr}, testCase.mergeErr)
			if outcome.Kind != testCase.wantKind {
				t.Fatalf("契约分类 = %s，想要 %s（notice: %s）", outcome.Kind, testCase.wantKind, outcome.Notice)
			}
			// 第二列：同输入落到节点上的效果（释放 / 阶段 / 警告 / 判死）。
			effect := runNodeFinish(t, testCase.runErr, testCase.mergeErr)
			if effect.released != testCase.wantRelease {
				t.Fatalf("释放现场 = %v，想要 %v（phases=%v）", effect.released, testCase.wantRelease, effect.phases)
			}
			if testCase.wantPhase != "" && !effect.phaseReported(testCase.wantPhase) {
				t.Fatalf("必须补记阶段 %q，实际 phases=%v", testCase.wantPhase, effect.phases)
			}
			warned := strings.Contains(effect.result, "[收尾警告]")
			if warned != testCase.wantWarn {
				t.Fatalf("产出警告 = %v，想要 %v（result=%q）", warned, testCase.wantWarn, effect.result)
			}
			if (effect.err != nil) != testCase.wantFailed {
				t.Fatalf("判死 = %v，想要 %v（err=%v）", effect.err != nil, testCase.wantFailed, effect.err)
			}
			// 结论照常交付：警告与判死都不销毁节点产出。
			if testCase.runErr == nil && !strings.Contains(effect.result, "节点结论") {
				t.Fatalf("节点结论必须继续交付：%q", effect.result)
			}
			// 收尾只在"这一轮本身跑完"时入场（与契约判据顺序同一口径：runErr 主导）。
			if wantFinish := testCase.runErr == nil; effect.finished != wantFinish {
				t.Fatalf("收尾入场 = %v，想要 %v", effect.finished, wantFinish)
			}
			if testCase.wantWarn && !strings.Contains(effect.result, testCase.mergeErr.Error()) {
				t.Fatalf("警告必须带上原因原文：%q", effect.result)
			}
		})
	}
}

// ── 2. 恢复说明：两层是同一种东西（前缀族 + role 的硬断言）──────────────────

// TestRecoveryNoteIsOneFamilyAcrossLayers：subagent 的前缀/role 不是自造的第二份，
// 而是契约前缀族与契约 role 的一支——workunit.RecoveryNote(KindSubagent, rec)
// 生成的正文必然以 subagentRecoveryNotePrefix 开头。
func TestRecoveryNoteIsOneFamilyAcrossLayers(t *testing.T) {
	if !strings.HasPrefix(subagentRecoveryNotePrefix, workunit.RecoveryNotePrefix) {
		t.Fatalf("subagent 前缀 %q 必须落在契约前缀族 %q 里",
			subagentRecoveryNotePrefix, workunit.RecoveryNotePrefix)
	}
	if SubagentRecoveryNoteRole != workunit.RecoveryNoteRole {
		t.Fatalf("恢复说明 role = %q，契约 role = %q（两层必须同一个）",
			SubagentRecoveryNoteRole, workunit.RecoveryNoteRole)
	}
	record := sessionstore.NodeSessionRecord{
		NodeID: "sub-1", SessionID: "node-1", Goal: "巡检存储层", Status: "running",
		Summary: "读到 message_rows.go", Error: "deadline exceeded",
		StagesJSON: []byte(`[{"stage":"running","preview":"read message_rows.go"}]`),
		Worktree:   sessionstore.NodeWorktreeRecord{Path: "D:/tmp/x-seelex-sub-1", Branch: "seelex/sub-1"},
	}
	note := workunit.RecoveryNote(workunit.KindSubagent, record)
	if !strings.HasPrefix(note, subagentRecoveryNotePrefix) {
		t.Fatalf("契约 builder 的 subagent 正文必须以 %q 开头：%q", subagentRecoveryNotePrefix, note)
	}
	// 事实项不丢：目标/状态/阶段/结论/错误/现场都在正文里。
	for _, want := range []string{"巡检存储层", "running", "message_rows.go", "读到 message_rows.go", "deadline exceeded", "seelex/sub-1"} {
		if !strings.Contains(note, want) {
			t.Fatalf("恢复说明缺事实项 %q：\n%s", want, note)
		}
	}
}

// ── 3. workunit.Unit 适配器：建 → 跑 → 收尾 → 回收 → 恢复 单一一路跑通 ────────

// TestNodeWorkUnitSinglePath：subagent 层的 Unit 实现只转调既有方法——本用例让
// 这条路整条跑一遍（真 git 现场 + 真会话账本），并钉住四件事：现场按命名约定建出、
// 收尾由契约分类、回收拆现场且清会话记录、恢复读数随账本变化。
func TestNodeWorkUnitSinglePath(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	repo := setupGitRepo(t)
	if err := runtime.BindProjectRoot(repo); err != nil {
		t.Fatalf("bind project root: %v", err)
	}
	router, err := sessionstore.NewRouter(filepath.Join(t.TempDir(), "session-storage.json"), t.TempDir())
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	store := sessionstore.NewNodeSessionStore(router)
	runtime.AttachHistoryRouter(router)
	runtime.AttachSubSessionStore(store)
	if _, err := runtime.NewMainSessionWithID("sess_main", nil); err != nil {
		t.Fatalf("new main session: %v", err)
	}
	projectID := router.Workspace()

	scope := seenode.NodeScope{NodeID: "wu-1", Role: model.RoleSubAgent, BranchID: "wu-1"}
	adapter := runtime.newNodeWorkUnit("sess_main", "wu-1", scope)
	var unit workunit.Unit = adapter // 编译期：这就是契约的一份实现

	if unit.Kind() != workunit.KindSubagent {
		t.Fatalf("Kind = %s，想要 %s", unit.Kind(), workunit.KindSubagent)
	}
	if _, ok := adapter.FinishPolicy().(workunit.Immediate); !ok {
		t.Fatal("subagent 层的策略必须是 Immediate（收尾即回收）")
	}

	// 建：真 git 现场，指派名与分支名同一约定。
	scene, err := unit.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if scene.Kind != workunit.KindSubagent || scene.NodeID != "wu-1" {
		t.Fatalf("scene = %+v", scene)
	}
	if scene.Worktree != "seelex/wu-1" {
		t.Fatalf("现场指派名 = %q，想要 seelex/wu-1", scene.Worktree)
	}
	if _, ok := runtime.worktreeMgr.Info("wu-1"); !ok {
		t.Fatal("Begin 之后现场必须在册（供前端展示与人工恢复）")
	}

	// 跑：这一件事在运行期落盘（一条记录 = 一份会话）。
	if err := store.Save(projectID, "sess_main", sessionstore.NodeSessionRecord{
		SchemaVersion: sessionstore.NodeSessionSchemaVersion,
		NodeID:        "wu-1", SessionID: "node-wu-1", MainSessionID: "sess_main",
		Goal: "do the thing", Status: "running", UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save session record: %v", err)
	}

	// 恢复读数（账本里有一条在跑、本进程没有它的执行面）：认领现场 + 报中断。
	resume, err := unit.Recover(context.Background())
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if resume.Sessions != 1 || resume.Scenes != 1 || len(resume.Interrupted) != 1 || resume.Interrupted[0] != "wu-1" {
		t.Fatalf("resume = %+v，想要 1 现场 / 1 会话 / 中断 [wu-1]", resume)
	}

	// 收尾：现场无提交且干净 → 契约分类为落定（真 git：现场被清理）。
	outcome, err := unit.Finish(context.Background(), workunit.Result{Summary: "done"}, nil)
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if outcome.Kind != workunit.OutcomeSettled || !outcome.Settled() {
		t.Fatalf("outcome = %s（%s），想要 settled", outcome.Kind, outcome.Notice)
	}

	// 回收：策略 Immediate → Reclaim（拆现场 + 清会话记录），且幂等。
	if err := adapter.FinishPolicy().AfterFinish(context.Background(), unit); err != nil {
		t.Fatalf("AfterFinish: %v", err)
	}
	if _, ok := runtime.worktreeMgr.Info("wu-1"); ok {
		t.Fatal("回收之后现场不得留在注册表")
	}
	records, err := store.List(projectID, "sess_main")
	if err != nil {
		t.Fatalf("list session records: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("回收之后会话记录必须删除：%+v", records)
	}
	if err := unit.Reclaim(context.Background()); err != nil {
		t.Fatalf("Reclaim 必须幂等：%v", err)
	}

	// 恢复（回路终点）：回收之后没有残留可回灌，读数为空且不报错。
	resume, err = unit.Recover(context.Background())
	if err != nil {
		t.Fatalf("Recover(after reclaim): %v", err)
	}
	if !resume.Empty() {
		t.Fatalf("回收之后恢复读数必须为空：%+v", resume)
	}
}
