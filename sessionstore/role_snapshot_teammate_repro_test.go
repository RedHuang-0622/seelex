package sessionstore

// role_snapshot_teammate_repro_test.go — 复现「查看 teammate 的会话，看到的是主代理的会话」。
//
// 事实链（teamwork 的 teammate 走的就是这一条）：
//   - teammate 的一轮活跑在**这件事自己的会话**上（一 Work Item 一套 Session），而那是
//     **进程内执行面**：`seelebridge.runRoleRound` 造引擎时**刻意不接 DurableHistory**，
//     也不调 `CreateRoleSessionWorkspace`，所以角色子树下**一行都没有**；
//   - 而 GUI 的「员工会话」入口读的是**角色会话只读观察面** `RoleSnapshot`，它的形状是
//     「main 车道（主会话整段）+ 自身车道 + 草稿」——主会话是真会话，自身车道为空。
//
// 于是"teammate 的会话"这条读面在**没有自己那份行**的时候，返回的就**只有主代理的
// 行**：前端把它画进记录表的 main 车道，用户看到的就是主代理的整段对话。这不是渲染
// 的错，是这条读面被当成了 teammate 自己的会话（它从来不是）。
//
// 本用例钉住这个事实（读面本身不改：它对 goal TL 那类**真有角色行**的角色仍然正确）——
// 于是「teammate 的会话」只能走另一条读面（Work Item 会话的实时投影），而任何"回退到
// 角色会话"的入口都必须先解释清楚它拿到的是什么。

import "testing"

func TestRoleSnapshotForTeammateCarriesMainRowsWithNoOwnRows(t *testing.T) {
	store, mainKey := roleSessionFixture(t)
	// 主会话里放入"主代理自己的回合"（GUI 的 main_rows 就是它）。
	if _, err := store.messageCommit(mainKey, "main-round-1", []Event{
		{Role: "assistant", Content: "主代理的结论", Kind: EventKindLLM},
	}); err != nil {
		t.Fatal(err)
	}

	// teammate 的角色会话号由 (主会话, team_id, role) 派生（teamwork.WorkItemSessionID 的
	// 前半段 / DefaultRoleSessionID）。它的角色子树**从未被创建**——这正是 worker 回合的
	// 形态：进程内执行面，不写存储。
	const roleSessionID = "main-session-team-exec"
	snapshot, err := store.readRoleSnapshot(mainKey, "exec", roleSessionID)
	if err != nil {
		t.Fatalf("读 teammate 的角色会话：%v（读面必须能读出来，否则入口会报错而不是给错内容）", err)
	}
	if len(snapshot.RoleRows) != 0 || len(snapshot.DraftRows) != 0 {
		t.Fatalf("teammate 的角色会话不该有自己的行（worker 回合不写存储）：role=%d draft=%d",
			len(snapshot.RoleRows), len(snapshot.DraftRows))
	}
	if len(snapshot.MainRows) == 0 {
		t.Fatal("这条读面恒带主会话整段（main_rows）——这正是「看到主代理的会话」的来源")
	}
	for _, row := range snapshot.MainRows {
		if row.Content == "主代理的结论" {
			t.Logf("确认：teammate 的”会话读面“里，唯一的内容就是主代理的行（seq=%d role=%s）", row.Seq, row.Role)
			return
		}
	}
	t.Fatalf("main_rows 里没有主代理的行：%+v", snapshot.MainRows)
}
