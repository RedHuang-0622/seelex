package sessionstore

import (
	"os"
	"strings"
	"testing"
)

// TestTeamRegistryRoundTripAndIsolation：角色注册表整份替换落到
// `session/team/roles.json`，读回保持角色配置；与 message 物理隔离，不写 message。
func TestTeamRegistryRoundTripAndIsolation(t *testing.T) {
	store, key := roleSessionFixture(t)
	path := store.teamRegistryPath(key)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("registry file must not exist before first write: %v", err)
	}
	registry, err := store.readTeamRegistry(key)
	if err != nil {
		t.Fatal(err)
	}
	if registry.Configured || len(registry.Roles) != 0 {
		t.Fatalf("unconfigured registry = %+v", registry)
	}

	err = store.writeTeamRegistry(key, TeamRegistry{
		TeamID:      "review-team",
		TeamKind:    "review-team",
		OrderPolicy: "user_main_decided",
		Roles: []TeamRoleSpec{
			{RoleName: "reviewer", RoleKind: "agent", OrderPriority: 2, ToolsPolicy: "readonly"},
			{RoleName: "digest", RoleKind: "timer", OrderPriority: 9, JoinPolicy: "scheduled"},
			{RoleName: "reviewer", RoleKind: "agent"},
		},
	})
	if err == nil {
		t.Fatal("duplicate role_name must be rejected")
	}

	if err := store.writeTeamRegistry(key, TeamRegistry{
		TeamID:      "review-team",
		TeamKind:    "review-team",
		OrderPolicy: "user_main_decided",
		Roles: []TeamRoleSpec{
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
	stored, err := store.readTeamRegistry(key)
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

	rows, err := store.readAllRows(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Content != "start" {
		t.Fatalf("registry write must not touch message rows: %+v", rows)
	}
}

// TestTeamRegistryRejectsUnknownSchema：schema 版本不认识时显式报错，不静默降级。
func TestTeamRegistryRejectsUnknownSchema(t *testing.T) {
	store, key := roleSessionFixture(t)
	if err := store.writeTeamRegistry(key, TeamRegistry{SchemaVersion: 99}); err == nil {
		t.Fatal("unsupported schema must be rejected")
	}
}

// TestReadLifecycleOrderToleratesMissingHead 是 Agent Team 面板报错的回归：
// 会话没有 lifecycle head（还没编排过顺序，或工程解析为空、目录根本不存在）
// 时，读顺序必须返回空值，而不是把
// `open ...\metadata\lifecycle.json: The system cannot find the path specified`
// 抛给前端——"未编排"是正常状态，不是错误。
func TestReadLifecycleOrderToleratesMissingHead(t *testing.T) {
	router := newTestRouter(t)
	// 空工程 + 磁盘上不存在的会话：读取会命中 ERROR_PATH_NOT_FOUND。
	policy, roles, err := router.ReadLifecycleOrderWorkspace("", "session-not-on-disk")
	if err != nil {
		t.Fatalf("缺失 lifecycle head 不是错误，实际返回：%v", err)
	}
	if policy != "" || len(roles) != 0 {
		t.Fatalf("缺失 head 应返回空顺序，实际 = %q %v", policy, roles)
	}
}

// TestEnsureRoleSessionWorkspaceIsIdempotent：重复装配同一条 TeamSpec 不产生第二个
// 角色会话；顺序策略读写走 lifecycle head（唯一顺序事实）。
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

	created, err := router.EnsureRoleSessionWorkspace(projectID, mainSessionID, "reviewer", "review-team-reviewer", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first ensure must create the role session")
	}
	created, err = router.EnsureRoleSessionWorkspace(projectID, mainSessionID, "reviewer", "review-team-reviewer", 0)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("second ensure must be a no-op")
	}
	names, err := router.ListRoleSessionsWorkspace(projectID, mainSessionID)
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

	if err := router.SetLifecycleOrderWorkspace(projectID, mainSessionID, "user_main_decided",
		[]string{"user", "main", "reviewer"}); err != nil {
		t.Fatal(err)
	}
	policy, roles, err := router.ReadLifecycleOrderWorkspace(projectID, mainSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if policy != "user_main_decided" || len(roles) != 3 || roles[2] != "reviewer" {
		t.Fatalf("lifecycle order = %q %v", policy, roles)
	}

	if err := router.WriteTeamRegistryWorkspace(projectID, mainSessionID, TeamRegistry{
		TeamKind:    "review-team",
		OrderPolicy: "user_main_decided",
		Roles:       []TeamRoleSpec{{RoleName: "reviewer", RoleKind: "agent"}},
	}); err != nil {
		t.Fatal(err)
	}
	registry, err := router.ReadTeamRegistryWorkspace(projectID, mainSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !registry.Configured || registry.TeamKind != "review-team" {
		t.Fatalf("router registry = %+v", registry)
	}
}
