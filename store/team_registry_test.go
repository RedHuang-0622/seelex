package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	storerole "github.com/RedHuang-0622/seelex/store/role"
)

// TestTeamRegistryOwnerRoundTripAndIsolation：角色注册表整份替换落到
// `session/team/roles.json`，读回保持角色配置；与 message 物理隔离，不写 message。
//
// M8：落盘实现已迁入 `store/role`（owner），本用例经根 storeEngine 持有的
// owner 实例验证**路径与语义零变化**（路径逐字断言，JSON 由 owner 定义）。
//
// 状态（A06，2026-10-15）：被测的读写面已是 **legacy 只读兼容面**（装配用户面随 J-1a
// 退场、当前无生产调用方）。本用例钉的是"旧格式仍能被正确读回"，保留原样继续当兼容护栏。
func TestTeamRegistryOwnerRoundTripAndIsolation(t *testing.T) {
	engine, key := roleSessionFixture(t)
	scope := layoutScope(key)
	path := engine.roles.Path(scope)
	if want := filepath.Join(engine.sessionRoot(key), "team", "roles.json"); path != want {
		t.Fatalf("roles.json 路径漂移：got %q want %q", path, want)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("registry file must not exist before first write: %v", err)
	}
	registry, err := engine.roles.Read(scope)
	if err != nil {
		t.Fatal(err)
	}
	if registry.Configured || len(registry.Roles) != 0 || registry.SchemaVersion != storerole.SchemaVersion {
		t.Fatalf("unconfigured registry = %+v", registry)
	}

	err = engine.roles.Write(scope, storerole.Registry{
		TeamID:      "review-team",
		TeamKind:    "review-team",
		OrderPolicy: "user_main_decided",
		Roles: []storerole.RoleSpec{
			{RoleName: "reviewer", RoleKind: "agent", OrderPriority: 2, ToolsPolicy: "readonly"},
			{RoleName: "digest", RoleKind: "timer", OrderPriority: 9, JoinPolicy: "scheduled"},
			{RoleName: "reviewer", RoleKind: "agent"},
		},
	})
	if err == nil {
		t.Fatal("duplicate role_name must be rejected")
	}

	if err := engine.roles.Write(scope, storerole.Registry{
		TeamID:      "review-team",
		TeamKind:    "review-team",
		OrderPolicy: "user_main_decided",
		Roles: []storerole.RoleSpec{
			{RoleName: "reviewer", RoleKind: "agent", OrderPriority: 2, ToolsPolicy: "readonly",
				MirrorPolicy: []string{"goal.start", "goal.update", "goal.start"}},
			{RoleName: "digest", RoleKind: "timer", OrderPriority: 9, JoinPolicy: "scheduled"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("registry file missing after write: %v", err)
	}
	stored, err := engine.roles.Read(scope)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Configured || stored.TeamKind != "review-team" || len(stored.Roles) != 2 {
		t.Fatalf("stored registry = %+v", stored)
	}
	if got := stored.Roles[0].MirrorPolicy; len(got) != 2 {
		t.Fatalf("mirror policy must be deduped, got %+v", got)
	}
	if stored.Roles[0].RoleName != "reviewer" || stored.Roles[0].ToolsPolicy != "readonly" {
		t.Fatalf("first role = %+v", stored.Roles[0])
	}
	// 幂等删除：删两次都不报错，删后读回 = 未配置。
	if err := engine.roles.Remove(scope); err != nil {
		t.Fatal(err)
	}
	if err := engine.roles.Remove(scope); err != nil {
		t.Fatalf("remove must be idempotent: %v", err)
	}
	removed, err := engine.roles.Read(scope)
	if err != nil || removed.Configured {
		t.Fatalf("removed registry = %+v err=%v", removed, err)
	}

	rows, err := engine.readAllRows(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Content != "start" {
		t.Fatalf("registry write must not touch message rows: %+v", rows)
	}
}

// TestTeamRegistryRejectsUnknownSchema：schema 版本不认识时显式报错，不静默降级。
func TestTeamRegistryRejectsUnknownSchema(t *testing.T) {
	engine, key := roleSessionFixture(t)
	if err := engine.roles.Write(layoutScope(key), storerole.Registry{SchemaVersion: 99}); err == nil {
		t.Fatal("unsupported schema must be rejected")
	}
}

// TestReadLifecycleOrderToleratesMissingHead 是 Agent Team 面板报错的回归：
// 会话没有 lifecycle head（还没编排过顺序，或工程解析为空、目录根本不存在）
// 时，读顺序必须返回空值，而不是把
// `open ...\metadata\lifecycle.json: The system cannot find the path specified`
// 抛给前端——"未编排"是正常状态，不是错误。
//
// 状态（A05/A06）：报错现场（Agent Team 面板）已退场，但"缺失 head 不是错误"这条
// 读兼容语义仍然成立（旧会话/空工程仍会走到这里）。
func TestReadLifecycleOrderToleratesMissingHead(t *testing.T) {
	router := newTestRouter(t)
	// 空工程 + 磁盘上不存在的会话：读取会命中 ERROR_PATH_NOT_FOUND。
	policy, roles, err := roleAdapter(router).ReadLifecycleOrderWorkspace("", "session-not-on-disk")
	if err != nil {
		t.Fatalf("缺失 lifecycle head 不是错误，实际返回：%v", err)
	}
	if policy != "" || len(roles) != 0 {
		t.Fatalf("缺失 head 应返回空顺序，实际 = %q %v", policy, roles)
	}
}

// TestReadMessageFloorWorkspaceToleratesUnstartedSession：没有 message head 的会话
// （工程解析为空/目录不存在）读 floor 必须返回空值，不把路径错误抛给调用方。
//
// 状态（A05/A06）：读面已无生产调用方，但"空会话 floor = nil 不是错误"的语义保留。
func TestReadMessageFloorWorkspaceToleratesUnstartedSession(t *testing.T) {
	router := newTestRouter(t)
	floor, err := roleAdapter(router).ReadMessageFloorWorkspace("", "session-not-on-disk")
	if err != nil {
		t.Fatalf("未开始的会话读 floor 不应报错，实际：%v", err)
	}
	if floor != nil {
		t.Fatalf("空会话 floor 应为 nil，实际：%+v", floor)
	}
}

// TestReadMessageFloorWorkspaceFollowsSequencerSync：floor 唯一写者是 sequencer
// （随 message head 原子发布）。sync 前无 floor；sync 后读到的就是当前发言角色——
// 它曾是前端 Agent Team 面板「floor 高亮」的数据源（那条面板已随 J-1a 退场）。
//
// 状态（A05）：`message head.floor` **本身是活动的历史/归属事实**（sequencer 仍在写），
// 本用例钉的是"读回来的是 sequencer 发布的那一份、且按值返回"，与面板是否还在无关。
func TestReadMessageFloorWorkspaceFollowsSequencerSync(t *testing.T) {
	router := newTestRouter(t)
	projectID := "project-1"
	mainSessionID := "main-floor"
	router.SetWorkspace(projectID)
	if err := router.SaveCommit(mainSessionID, Commit{Events: []Event{{
		Seq: 1, Role: "user", Content: "start", Kind: EventKindUserInput, MessageID: "m1",
	}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := roleAdapter(router).EnsureRoleSessionWorkspace(projectID, mainSessionID, RoleTL, "goal-a2a-tl", 1); err != nil {
		t.Fatal(err)
	}
	if err := roleAdapter(router).AppendRoleDraftWorkspace(projectID, mainSessionID, RoleTL, "goal-a2a-tl", []RoleDraftRow{{
		RoundID: 1, RoleName: RoleTL, RoleSessionID: "goal-a2a-tl", UnitSeq: 1, MessageID: "m2",
		Event: Event{Role: "assistant", Content: "裁决：done", Kind: EventKindLLM},
	}}); err != nil {
		t.Fatal(err)
	}
	// sync 之前：还没有人"发言"，floor 为空。
	floor, err := roleAdapter(router).ReadMessageFloorWorkspace(projectID, mainSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if floor != nil {
		t.Fatalf("sync 前不应有 floor: %+v", floor)
	}
	if _, err := roleAdapter(router).SyncRoleDraftWorkspace(projectID, mainSessionID, RoleTL, "goal-a2a-tl", []string{"user", "main", "tl"}); err != nil {
		t.Fatal(err)
	}
	floor, err = roleAdapter(router).ReadMessageFloorWorkspace(projectID, mainSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if floor == nil || floor.RoleName != RoleTL || floor.RoleSessionID != "goal-a2a-tl" || floor.RoundID != 1 {
		t.Fatalf("sync 后 floor = %+v, want tl/goal-a2a-tl round=1", floor)
	}
	// 返回的是克隆：调用方改动不得污染 message head。
	floor.RoleName = "tampered"
	again, err := roleAdapter(router).ReadMessageFloorWorkspace(projectID, mainSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if again == nil || again.RoleName != RoleTL {
		t.Fatalf("floor 必须按值返回（head 不应被外部改写）: %+v", again)
	}
}

// TestEnsureRoleSessionWorkspaceIsIdempotent：重复装配同一条 TeamSpec 不产生第二个
// 角色会话；顺序策略读写走 lifecycle head（唯一顺序事实）。
//
// 状态（A06）：装配方（`AgentTeamFactory`）已随 J-1a 退场，本用例现在钉的是
// **旧数据兼容语义**（幂等建角色会话 + lifecycle 顺序可读回），不再有生产调用方。
func TestEnsureRoleSessionWorkspaceIsIdempotent(t *testing.T) {
	router := newTestRouter(t)
	projectID := "project-1"
	mainSessionID := "main-session"
	router.SetWorkspace(projectID)
	if err := router.SaveCommit(mainSessionID, Commit{Events: []Event{{
		Seq: 1, Role: "user", Content: "start", Kind: EventKindUserInput, MessageID: "m1",
	}}}); err != nil {
		t.Fatal(err)
	}

	created, err := roleAdapter(router).EnsureRoleSessionWorkspace(projectID, mainSessionID, "reviewer", "review-team-reviewer", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first ensure must create the role session")
	}
	created, err = roleAdapter(router).EnsureRoleSessionWorkspace(projectID, mainSessionID, "reviewer", "review-team-reviewer", 0)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("second ensure must be a no-op")
	}
	names, err := roleAdapter(router).ListRoleSessionsWorkspace(projectID, mainSessionID)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, name := range names {
		if strings.HasPrefix(name, "role_") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("role session dirs = %v, want exactly one", names)
	}

	if err := roleAdapter(router).SetLifecycleOrderWorkspace(projectID, mainSessionID, "user_main_decided",
		[]string{"user", "main", "reviewer"}); err != nil {
		t.Fatal(err)
	}
	policy, roles, err := roleAdapter(router).ReadLifecycleOrderWorkspace(projectID, mainSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if policy != "user_main_decided" || len(roles) != 3 || roles[2] != "reviewer" {
		t.Fatalf("lifecycle order = %q %v", policy, roles)
	}

	layout := router.repository.(*jsonRepository).layout
	if err := layout.roles.Write(registryScope(projectID, mainSessionID), storerole.Registry{
		TeamKind:    "review-team",
		OrderPolicy: "user_main_decided",
		Roles:       []storerole.RoleSpec{{RoleName: "reviewer", RoleKind: "agent"}},
	}); err != nil {
		t.Fatal(err)
	}
	registry, err := layout.roles.Read(registryScope(projectID, mainSessionID))
	if err != nil {
		t.Fatal(err)
	}
	if !registry.Configured || registry.TeamKind != "review-team" {
		t.Fatalf("router registry = %+v", registry)
	}
}
