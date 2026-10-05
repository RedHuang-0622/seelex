package seelebridge

// runtime_teamwork_scene_test.go — F4 的宿主级回归守卫：**重启不是团队现场的坟墓**。
//
// 工作项级的现场（`BindWorkspace → WorktreeManager.BeginNamed`）从来不在子代理的持久
// 恢复名单里（`NoteWorktree` 全仓只有 `beginNodeWorktree` 一处调用点），于是重启后它
// 既不在册（`bindWorkerProjectRoot` 回退主会话根 = "去不了目标的 worktree"），又紧接着
// 被 `Prune` 的"不在册 + 干净"判据当孤儿删掉（目录 + `seelex/<nodeID>` 分支一起没）。
//
// 修复把恢复链接成**子代理那条链的形状**：先 `Restore`（子代理锚点）→ **从团队计划 +
// 绑定账本认领**团队现场 → 最后 `Prune`。本文件钉住"认领先于清理、认领后绑根即落到
// 现场"，并覆盖账本两种行形状（Work Item 级 / teammate 级 WorkItem 留空）。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/seelebridge/worktree"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// sceneTestTeamPlan 造一份含两个现场的计划：teammate 级（member.Worktree）与
// Work Item 级（items[0].Worktree）。
func sceneTestTeamPlan() sessionstore.TeamworkPlan {
	return sessionstore.TeamworkPlan{
		TeamID:  "t-scene",
		Version: 1,
		Members: []sessionstore.TeamworkMember{
			{Role: "exec", RoleSessionID: "sess-1-t-scene-exec", Worktree: "seelex/exec"},
		},
		Milestones: []sessionstore.TeamworkMilestone{{
			ID: "m1",
			Items: []sessionstore.TeamworkWorkItem{
				{ID: "wi-clean", Milestone: "m1", Role: "exec", Name: "干净但还没合并", Worktree: "seelex/exec-wi-clean"},
			},
		}},
	}
}

// TestRestoreAnchorsAdoptsTeamScenesBeforePrune —— F4 ①（宿主级）：
// 重启 + 宿主恢复之后，团队现场在册、目录/分支保留、Prune 不删它，且绑根落到现场。
func TestRestoreAnchorsAdoptsTeamScenesBeforePrune(t *testing.T) {
	ctx := context.Background()
	key := sessionstore.Key{ProjectID: "p-team", SessionID: "sess-1"}
	store := &memPlanStore{}
	if err := store.WritePlan(ctx, key, sceneTestTeamPlan(), 0); err != nil {
		t.Fatal(err)
	}
	// 账本两种行形状都要认领：
	//   - Work Item 级（WorkItem 非空）；
	//   - teammate 级（WorkItem 留空、Role=角色名、Worktree=seelex/<role>，leader 定的形状）
	//     ——`sessionstore.TeamworkBindings` 的折叠按 work_item 为键，会把这一行整行丢掉，
	//     所以恢复侧必须读**原始行**。
	for _, row := range []sessionstore.TeamworkBinding{
		{WorkItem: "wi-clean", Role: "exec", SessionID: "sess-1-t-scene-exec", Worktree: "seelex/exec-wi-clean"},
		{WorkItem: "", Role: "exec", SessionID: "sess-1-t-scene-exec", Worktree: "seelex/exec"},
	} {
		if err := store.AppendBinding(ctx, key, row); err != nil {
			t.Fatal(err)
		}
	}

	router, err := sessionstore.NewRouter(filepath.Join(t.TempDir(), "session-storage.json"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	nodeStore := sessionstore.NewNodeSessionStore(router)
	repo := setupGitRepo(t)

	// ── 旧进程：建出两个现场（都**干净**——探针实测里被 Prune 删掉的就是这种）
	previous := newTestRuntime(t)
	defer previous.Shutdown()
	previous.AttachHistoryRouter(router)
	previous.AttachSubSessionStore(nodeStore)
	if err := previous.BindProjectRoot(repo); err != nil {
		t.Fatal(err)
	}
	scenes := map[string]string{}
	for _, nodeID := range []string{"exec-wi-clean", "exec"} {
		wt := previous.worktreeMgr.BeginNamed(nodeID)
		if wt == nil {
			t.Fatalf("前置：真实 git 仓库里应能建出现场 %q", nodeID)
		}
		scenes[nodeID] = wt.Path
	}
	if _, err := os.Stat(scenes["exec-wi-clean"]); err != nil {
		t.Fatalf("前置：现场目录应存在：%v", err)
	}

	// ── 重启：新 Runtime（WorktreeManager 注册表是内存态，重启即空）
	restarted := newTestRuntime(t)
	defer restarted.Shutdown()
	restarted.AttachHistoryRouter(router)
	restarted.AttachSubSessionStore(nodeStore)
	if err := restarted.BindProjectRoot(repo); err != nil {
		t.Fatal(err)
	}
	if err := restarted.SetTeamworkBackend(teamworkTestBackend(store, "sess-1")); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	if restarted.worktreeMgr.RegisteredCount() != 0 {
		t.Fatal("前置条件：重启后的注册表必须是空的")
	}

	if err := restarted.RestoreSubagentAnchors("sess-1"); err != nil {
		t.Fatalf("RestoreSubagentAnchors: %v", err)
	}

	// ① 在册（两个现场：计划 + 账本两种行形状都认出来了）。
	for nodeID, path := range scenes {
		info, ok := restarted.worktreeMgr.Info(nodeID)
		if !ok {
			t.Fatalf("重启恢复后团队现场 %q 必须在册（不在册 = 绑根只能回退主会话根）", nodeID)
		}
		if !strings.EqualFold(filepath.Clean(info.Path), filepath.Clean(path)) {
			t.Fatalf("现场 %q 路径漂了：got %q want %q", nodeID, info.Path, path)
		}
	}
	// ② 目录与分支都保留（Prune 已经在这一趟里跑过，就在认领之后）。
	if _, err := os.Stat(scenes["exec-wi-clean"]); err != nil {
		t.Fatalf("`干净但还没合并` 的现场被 Prune 当孤儿删了：%v", err)
	}
	branches, err := worktree.GitRunner(repo, "branch", "--list", "seelex/exec-wi-clean")
	if err != nil || !strings.Contains(branches, "seelex/exec-wi-clean") {
		t.Fatalf("现场分支不得被删掉：out=%q err=%v", branches, err)
	}
	// ③ 认领之后**真的可用**：worker 回合的绑根必须落到自己的 worktree，而不是 main。
	restarted.bindWorkerProjectRoot("sess-1", "sess-1-t-scene-exec", "seelex/exec-wi-clean")
	got := restarted.projectScope.RootFor("sess-1-t-scene-exec")
	if !strings.EqualFold(filepath.Clean(got), filepath.Clean(scenes["exec-wi-clean"])) {
		t.Fatalf("teammate 工具根必须落在自己的 worktree：\n got  = %q\n want = %q\n main = %q",
			got, scenes["exec-wi-clean"], repo)
	}
}

// TestTeamSceneNodeIDsReadsPlanAndRawLedgerRows 钉住认领名单的两个来源与两条规则：
// 计划（member/item 的 Worktree）+ 账本**原始行**（teammate 级空 WorkItem 不得被折叠丢掉）；
// 已释放（Released）的绑定不再认领。
func TestTeamSceneNodeIDsReadsPlanAndRawLedgerRows(t *testing.T) {
	ctx := context.Background()
	key := sessionstore.Key{ProjectID: "p-team", SessionID: "sess-1"}
	store := &memPlanStore{}
	if err := store.WritePlan(ctx, key, sceneTestTeamPlan(), 0); err != nil {
		t.Fatal(err)
	}
	for _, row := range []sessionstore.TeamworkBinding{
		{WorkItem: "wi-clean", Role: "exec", SessionID: "rs", Worktree: "seelex/exec-wi-clean"},
		{WorkItem: "", Role: "exec", SessionID: "rs", Worktree: "seelex/exec"},
		{WorkItem: "wi-old", Role: "exec", SessionID: "rs", Worktree: "seelex/exec-wi-old", Released: true, Reason: "accept"},
	} {
		if err := store.AppendBinding(ctx, key, row); err != nil {
			t.Fatal(err)
		}
	}

	runtime := newTestRuntime(t)
	defer runtime.Shutdown()

	// 未装配 teamwork 后端 = 不猜、不扫目录：名单为空。
	if got := runtime.teamSceneNodeIDs("sess-1"); len(got) != 0 {
		t.Fatalf("未装配 teamwork 后端时不得给出任何 nodeID：%v", got)
	}

	if err := runtime.SetTeamworkBackend(teamworkTestBackend(store, "sess-1")); err != nil {
		t.Fatal(err)
	}
	got := runtime.teamSceneNodeIDs("sess-1")
	seen := map[string]bool{}
	for _, id := range got {
		seen[id] = true
	}
	for _, want := range []string{"exec-wi-clean", "exec"} {
		if !seen[want] {
			t.Fatalf("认领名单缺 %q（计划 / 账本原始行）：%v", want, got)
		}
	}
	if seen["exec-wi-old"] {
		t.Fatalf("已释放（Released）的绑定不得再认领：%v", got)
	}
	// 换一个会话（无作用域键）→ 空：不在别人的作用域里认领。
	if other := runtime.teamSceneNodeIDs("sess-else"); len(other) != 0 {
		t.Fatalf("别的会话不得拿到本会话的现场名单：%v", other)
	}
}
