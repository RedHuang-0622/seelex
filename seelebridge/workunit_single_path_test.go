package seelebridge

// workunit_single_path_test.go — 跨层一致性：**同一份输入**喂给两个注册点（subagent 的
// nodeWorkUnit 与 teammate 的 teamUnit），得到的结论必须是同一份。
//
// 断言的是"两个注册点共用同一个父实现"，而不是"两个实现碰巧一致"：
//
//	①同一输入 → 同一结论（收尾分类逐字对齐）；
//	②重入 → 两层都交回**零值结论**（不重复合并、不重复回执）；
//	③差异只剩策略（同一个 finished 结论下：subagent 收尾即回收，teammate 留给 team_close）。

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	seetelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/seelebridge/teamwork"
	"github.com/RedHuang-0622/seelex/seelebridge/workunit"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// TestLifecycleOneImplementationAcrossLayers：两个注册点，一份结论（含重入）。
func TestLifecycleOneImplementationAcrossLayers(t *testing.T) {
	ctx := seetelemetry.WithSessionID(context.Background(), "sess-1")

	// ── subagent 层的注册点：真 git 现场 + 真会话账本 ────────────────────
	nodeRuntime := newTestRuntime(t)
	defer nodeRuntime.Shutdown()
	if err := nodeRuntime.BindProjectRoot(setupGitRepo(t)); err != nil {
		t.Fatalf("bind project root: %v", err)
	}
	nodeRouter, err := sessionstore.NewRouter(filepath.Join(t.TempDir(), "node-storage.json"), t.TempDir())
	if err != nil {
		t.Fatalf("new router: %v", err)
	}
	t.Cleanup(func() { _ = nodeRouter.Close() })
	nodeRuntime.AttachHistoryRouter(nodeRouter)
	nodeRuntime.AttachSubSessionStore(sessionstore.NewNodeSessionStore(nodeRouter))
	if _, err := nodeRuntime.NewMainSessionWithID("sess_main", nil); err != nil {
		t.Fatalf("new main session: %v", err)
	}
	nodeUnit := nodeRuntime.newNodeWorkUnit("sess_main", "wu-1", model.NodeScope{
		NodeID: "wu-1", Role: model.RoleSubAgent,
	})
	if _, err := nodeUnit.Begin(ctx); err != nil {
		t.Fatalf("subagent Begin: %v", err)
	}
	nodeOutcome, err := nodeUnit.Finish(ctx, workunit.Result{Summary: "跑完"}, nil)
	if err != nil {
		t.Fatalf("subagent Finish: %v", err)
	}
	nodeRepeat, err := nodeUnit.Finish(ctx, workunit.Result{Summary: "跑完"}, nil)
	if err != nil {
		t.Fatalf("subagent Finish（重入）: %v", err)
	}

	// ── teammate 层的注册点：团队计划 + 团队账本（同一个父实现）────────────
	fixture := newTeamHostFixture(t, teamHostPlan(), teamHostBindings())
	if wt := fixture.runtime.worktreeMgr.BeginNamed("exec-wi-1"); wt == nil {
		t.Fatal("前置：应当能建出 teammate 现场 exec-wi-1")
	}
	teamScene := workunit.Scene{
		NodeID: "exec-wi-1", SessionID: "sess-1-t-exec-wi-1",
		Worktree: "seelex/exec-wi-1", TeamID: "t-host", WorkItem: "wi-1",
	}
	teamRequest := teamwork.WorkerRequest{
		MainSessionID: "sess-1", TeamID: "t-host", Role: "exec", RoleSessionID: "sess-1-t-exec-wi-1",
		WorkItemID: "wi-1", Milestone: "m1", Worktree: "seelex/exec-wi-1",
	}
	teamUnitAdapter, err := newTeamUnit(fixture.runtime, "sess-1", "exec", teamScene, teamRequest)
	if err != nil {
		t.Fatalf("newTeamUnit: %v", err)
	}
	teamOutcome, err := teamUnitAdapter.Finish(ctx, workunit.Result{Summary: "跑完"}, nil)
	if err != nil {
		t.Fatalf("teammate Finish: %v", err)
	}
	teamRepeat, err := teamUnitAdapter.Finish(ctx, workunit.Result{Summary: "跑完"}, nil)
	if err != nil {
		t.Fatalf("teammate Finish（重入）: %v", err)
	}

	// ① 同一份输入 → 同一份结论。
	if nodeOutcome.Kind != teamOutcome.Kind || !nodeOutcome.Settled() {
		t.Fatalf("两层同一输入必须同一结论：subagent = %s（%s），teammate = %s（%s）",
			nodeOutcome.Kind, nodeOutcome.Notice, teamOutcome.Kind, teamOutcome.Notice)
	}
	// ② 重入：两层都交回零值结论（不再合并、不再回执、不再判定）。
	if nodeRepeat.Kind != "" || nodeRepeat.Notice != "" {
		t.Fatalf("subagent 重入必须交回零值结论：%+v", nodeRepeat)
	}
	if teamRepeat.Kind != "" || teamRepeat.Notice != "" {
		t.Fatalf("teammate 重入必须交回零值结论：%+v", teamRepeat)
	}
	// ③ 差异只剩策略：同一个落定结论下，subagent 收尾即回收，teammate 留给 team_close。
	if _, ok := nodeUnit.FinishPolicy().(workunit.Immediate); !ok {
		t.Fatalf("subagent 的策略 = %T，想要 Immediate", nodeUnit.FinishPolicy())
	}
	if _, ok := teamUnitAdapter.FinishPolicy().(workunit.AtTeamClose); !ok {
		t.Fatalf("teammate 的策略 = %T，想要 AtTeamClose", teamUnitAdapter.FinishPolicy())
	}
	if _, ok := nodeRuntime.worktreeMgr.Info("wu-1"); ok {
		t.Fatal("subagent 落定之后现场必须已经回收（Immediate）")
	}
	// teammate 侧：落定之后**不动**现场与会话——策略只是"什么时候回收"，
	// 回收的唯一入口仍是 team_close（这里的现场会被合并步顺手清掉的是"没有提交可并"，
	// 与策略无关；能证明策略的是**会话记录还在**）。
	if _, ok := teamUnitAdapter.FinishPolicy().(workunit.AtTeamClose); !ok {
		t.Fatal("teammate 的策略必须留给 team_close")
	}
	teamRecords, err := fixture.nodeStore.List("p-team", "sess-1")
	if err != nil {
		t.Fatalf("list teammate records: %v", err)
	}
	found := false
	for _, record := range teamRecords {
		if record.NodeID == "exec-wi-1" {
			found = true
			if record.Status != teamUnitStatusDone {
				t.Fatalf("teammate 落定后的记录状态 = %q，想要 %q", record.Status, teamUnitStatusDone)
			}
		}
	}
	if !found {
		t.Fatal("teammate 落定之后会话记录必须还在（回收唯一入口 = team_close）")
	}
	nodeRecords, err := nodeRuntime.nodeSessionStore.List(nodeRouter.Workspace(), "sess_main")
	if err != nil {
		t.Fatalf("list subagent records: %v", err)
	}
	if len(nodeRecords) != 0 {
		t.Fatalf("subagent 回收之后会话记录必须清掉：%+v", nodeRecords)
	}
}
