package seelebridge

// runtime_unit_record_identity_test.go — 记录快照的**身份**那一格（记录属于哪一类执行单元）
// 与记录侧现场四栏（收尾要用的两个事实）。
//
// 背景（本轮现网读数，`docs/2026-10-06-workunit-jobs-port/step-3-audit.md` §2 相邻事实）：
// `RestoreSubagentAnchors` 把 `NodeSessionStore.List` 的**全部**记录同时喂给子代理详情面、
// 子代理树与 worktree 注册表，而唯一的过滤判据是"记录的主人还活在本进程里没有"——**不区分
// 这条记录属于哪条链**。于是 teammate 的单元记录（NodeID = `<role>-<itemID>`）被当成"崩溃
// 遗留的子代理节点"恢复：领队这一轮的工作表格里就长着一条
// `subagent:audit-u2u5u6-wi-4-audit-u2u5u6 interrupted`，而它的真身（`teamwork:a3`）是 done。
//
// 两条用例：
//
//	① `TestTeamUnitRecordRestoresAsTeamworkSceneNotSubagentNode` —— 产品写点落下的 teammate
//	   单元记录，重启恢复后：**不是**子代理树节点（工作表格上那条 phantom 行），而它的现场
//	   带着收尾要用的四栏在册（MainBranch = 这次合回哪条分支；BaseCommit = 变基/提交判定基线）。
//	② `TestLegacyTeamUnitRecordWithoutIdentityIsClassifiedByTeamScene` —— 加固前写下的老记录
//	   快照里没有身份那一格：按**团队事实**（计划 + 绑定账本里的 nodeID 名单）判它属于 teammate，
//	   同样不许长成子代理节点。

import (
	"os"
	"path/filepath"
	"testing"

	bridgesession "github.com/RedHuang-0622/seelex/seelebridge/session"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// teamUnitRecordFixture 搭出"旧进程落一条 teammate 单元记录 + 建出现场"的现场。
//
// 现场用 `BeginNamed`（与派发时 `BindWorkspace` 同一条命名）在**真 git 仓库**里建，记录用
// 产品写点 `markTeamUnitRunning` 落（不手搓 JSON：要钉的正是"写侧写出去什么形状"）。
func teamUnitRecordFixture(t *testing.T, sessionID, nodeID, roleSessionID string, write func(*Runtime, *sessionstore.NodeSessionStore)) (repo, scenePath string, router *sessionstore.Router, nodeStore *sessionstore.NodeSessionStore) {
	t.Helper()
	router, err := sessionstore.NewRouter(filepath.Join(t.TempDir(), "session-storage.json"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })
	nodeStore = sessionstore.NewNodeSessionStore(router)
	repo = setupGitRepo(t)

	previous := newTestRuntime(t)
	defer previous.Shutdown()
	previous.AttachHistoryRouter(router)
	previous.AttachSubSessionStore(nodeStore)
	if err := previous.BindProjectRoot(repo); err != nil {
		t.Fatal(err)
	}
	// 记录的存储作用域与团队后端同一个键（`teamUnitScope` 走 backend.KeyFor）。
	previous.SetSessionWorkspace(sessionID, "p-team")
	if err := previous.SetTeamworkBackend(teamworkTestBackend(&memPlanStore{}, sessionID)); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	created := previous.worktreeMgr.BeginNamed(nodeID)
	if created == nil {
		t.Fatal("前置：真实 git 仓库里应能建出现场")
	}
	write(previous, nodeStore)
	if _, err := os.Stat(created.Path); err != nil {
		t.Fatalf("前置：现场目录应存在：%v", err)
	}
	return repo, created.Path, router, nodeStore
}

// restartOverScene 在同一份存储上起一个新 Runtime（现场注册表是内存态，重启即空）。
func restartOverScene(t *testing.T, repo string) *Runtime {
	t.Helper()
	restarted := newTestRuntime(t)
	t.Cleanup(restarted.Shutdown)
	if err := restarted.BindProjectRoot(repo); err != nil {
		t.Fatal(err)
	}
	return restarted
}

// teammateSubagentTreeNodes 返回子代理树上当前的全部节点 id（工作表格的 phantom 行就从这里长出来：
// 投影是"合成根 + 递归子节点"，所以必须走整棵树，只看顶层只会看到那个合成根 `main`）。
func teammateSubagentTreeNodes(r *Runtime) []string {
	ids := []string{}
	var walk func(nodes []bridgesession.SubAgentTreeNode)
	walk = func(nodes []bridgesession.SubAgentTreeNode) {
		for _, node := range nodes {
			ids = append(ids, node.ID)
			walk(node.Children)
		}
	}
	walk(r.subagentTree.Projection())
	return ids
}

// TestTeamUnitRecordRestoresAsTeamworkSceneNotSubagentNode —— ① 上面那段背景的直接用例。
func TestTeamUnitRecordRestoresAsTeamworkSceneNotSubagentNode(t *testing.T) {
	const (
		sessionID = "sess-1"
		nodeID    = "exec-wi-1"
		roleSess  = "sess-1-t-exec"
	)
	repo, scenePath, router, nodeStore := teamUnitRecordFixture(t, sessionID, nodeID, roleSess,
		func(r *Runtime, _ *sessionstore.NodeSessionStore) {
			r.markTeamUnitRunning(sessionID, teamUnitRecordKey{
				NodeID: nodeID, RoleSessionID: roleSess, Goal: "只写文档：把现场四栏写全",
			})
		})

	records, err := nodeStore.List("p-team", sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("前置：teammate 单元记录应落盘一条，实际 %d 条：%+v", len(records), records)
	}
	// 写侧：记录必须自带收尾要用的四栏（不是只有 Path/Branch 的"弱登记"）。
	if records[0].Worktree.MainBranch == "" {
		t.Errorf("记录侧缺 MainBranch（这次合回哪条分支没有来源）：%+v", records[0].Worktree)
	}
	if records[0].Worktree.BaseCommit == "" {
		t.Errorf("记录侧缺 BaseCommit（变基与提交判定的基线没有来源）：%+v", records[0].Worktree)
	}

	// ── 重启
	restarted := restartOverScene(t, repo)
	restarted.AttachHistoryRouter(router)
	restarted.AttachSubSessionStore(nodeStore)
	restarted.SetSessionWorkspace(sessionID, "p-team")
	if err := restarted.SetTeamworkBackend(teamworkTestBackend(&memPlanStore{}, sessionID)); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	if err := restarted.RestoreSubagentAnchors(sessionID); err != nil {
		t.Fatalf("RestoreSubagentAnchors: %v", err)
	}

	// ① teammate 的单元记录**不是**子代理节点：它的真身是团队现场，不是"崩溃遗留的子代理"。
	for _, id := range teammateSubagentTreeNodes(restarted) {
		if id == nodeID {
			t.Errorf("teammate 单元记录被当成子代理节点恢复了（工作表格上那条 `subagent:%s interrupted` 假行）：树上节点=%v",
				nodeID, teammateSubagentTreeNodes(restarted))
		}
	}
	// ② 现场在册，且记录自带的那两栏真的到了注册表里（收尾据此决定合回哪条分支）。
	info, ok := restarted.worktreeMgr.Info(nodeID)
	if !ok {
		t.Fatalf("teammate 现场必须从它自己的记录恢复进注册表（否则绑根只能回退主会话根）")
	}
	if info.MainBranch == "" {
		t.Errorf("记录侧恢复出来的现场缺 MainBranch：Info=%+v（先到的那一份没有这一栏，收尾就合不回 main）", info)
	}
	// ③ 现场目录原样（Prune 已经在这一趟里跑过，就在恢复之后）。
	if _, err := os.Stat(scenePath); err != nil {
		t.Errorf("现场被 Prune 当孤儿删掉了：%v", err)
	}
}

// TestLegacyTeamUnitRecordWithoutIdentityIsClassifiedByTeamScene —— ② 加固前的老记录。
//
// 老记录快照里没有身份那一格（`Unit.Kind` 空），恢复侧不能靠"nodeID 的形状"猜，但可以读
// **团队事实**：nodeID 落在本会话的团队现场名单（计划 + 绑定账本，`teamSceneIndex`）里，
// 它就是一个 teammate 单元。
func TestLegacyTeamUnitRecordWithoutIdentityIsClassifiedByTeamScene(t *testing.T) {
	const (
		sessionID = "sess-legacy"
		// nodeID 取 `sceneTestTeamPlan()` 里那个 Work Item 级现场的名字（`seelex/exec-wi-clean`）
		// ——老记录的快照里没有身份，判据只能来自**团队事实**（计划 + 账本里的名单）。
		nodeID   = "exec-wi-clean"
		roleSess = "sess-legacy-t-exec"
	)
	repo := setupGitRepo(t)
	router, err := sessionstore.NewRouter(filepath.Join(t.TempDir(), "session-storage.json"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	nodeStore := sessionstore.NewNodeSessionStore(router)

	// 老进程：建现场；把**加固前形状**的记录直接落盘（没有 Unit 那一格）。
	previous := newTestRuntime(t)
	previous.AttachHistoryRouter(router)
	previous.AttachSubSessionStore(nodeStore)
	if err := previous.BindProjectRoot(repo); err != nil {
		t.Fatal(err)
	}
	created := previous.worktreeMgr.BeginNamed(nodeID)
	if created == nil {
		t.Fatal("前置：真实 git 仓库里应能建出现场")
	}
	legacy := sessionstore.NodeSessionRecord{
		SchemaVersion: sessionstore.NodeSessionSchemaVersion,
		NodeID:        nodeID,
		SessionID:     roleSess,
		MainSessionID: sessionID,
		Goal:          "老记录：快照里没有身份那一格",
		Status:        "running",
		Worktree:      sessionstore.NodeWorktreeRecord{Path: created.Path, Branch: created.Branch},
	}
	if err := nodeStore.Save("p-team", sessionID, legacy); err != nil {
		t.Fatal(err)
	}
	previous.Shutdown()

	// 重启：团队事实（计划 + 账本）说这个 nodeID 是 teammate 的。
	store := &memPlanStore{}
	if err := store.WritePlan(t.Context(), sessionstore.Key{ProjectID: "p-team", SessionID: sessionID}, sceneTestTeamPlan(), 0); err != nil {
		t.Fatal(err)
	}
	restarted := restartOverScene(t, repo)
	restarted.AttachHistoryRouter(router)
	restarted.AttachSubSessionStore(nodeStore)
	restarted.SetSessionWorkspace(sessionID, "p-team")
	if err := restarted.SetTeamworkBackend(teamworkTestBackend(store, sessionID)); err != nil {
		t.Fatalf("SetTeamworkBackend: %v", err)
	}
	if err := restarted.RestoreSubagentAnchors(sessionID); err != nil {
		t.Fatalf("RestoreSubagentAnchors: %v", err)
	}

	for _, id := range teammateSubagentTreeNodes(restarted) {
		if id == nodeID {
			t.Errorf("老 teammate 记录（快照无身份）仍被当成子代理节点恢复：树上节点=%v；"+
				"判据应读团队事实（计划 + 账本里的 nodeID 名单）而不是猜形状", teammateSubagentTreeNodes(restarted))
		}
	}
}
