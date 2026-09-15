package sessionstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestTeamLibraryPathLayout：团队库落在**全局**作用域目录 `team/library.json`
// （数据根下，不随项目分目录）；旧布局路径 `project-<hash>/teams/library.json`
// 仍然可解析，但只用于只读回退。
func TestTeamLibraryPathLayout(t *testing.T) {
	root := t.TempDir()
	store := newStoreEngine(root, storageSettings{})
	want := filepath.Join(root, "team", "library.json")
	if got := store.teamGlobalLibraryPath(); got != want {
		t.Fatalf("teamGlobalLibraryPath = %q, want %q", got, want)
	}
	legacy := filepath.Join(root, "project-"+hash("project-1"), "teams", "library.json")
	if got := store.teamLegacyLibraryPath("project-1"); got != legacy {
		t.Fatalf("teamLegacyLibraryPath = %q, want %q", got, legacy)
	}
	if err := store.writeTeamLibraryGlobal(TeamLibrary{Teams: []TeamLibraryEntry{{TeamID: "t", Name: "T"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("全局团队库文件缺失：%v", err)
	}
	if _, err := os.Stat(legacy); err == nil {
		t.Fatal("写全局团队库不应在旧项目目录留下文件")
	}
}

// TestTeamLibraryRoundTrip：团队库整份落盘、读回保持条目（含顺序表的用户次序与
// 角色配置）。
func TestTeamLibraryRoundTrip(t *testing.T) {
	router := newTestRouter(t)
	const projectID = "project-library"
	router.SetWorkspace(projectID)

	empty, err := router.ReadTeamLibraryGlobal(projectID)
	if err != nil {
		t.Fatalf("未建库读团队库不应报错：%v", err)
	}
	if empty.Configured || len(empty.Teams) != 0 {
		t.Fatalf("未建库应为空库：%+v", empty)
	}

	err = router.WriteTeamLibraryGlobal(TeamLibrary{Teams: []TeamLibraryEntry{
		{TeamID: "dup", Name: "a", Roles: []TeamRoleSpec{{RoleName: "reviewer", RoleKind: "agent"}}},
		{TeamID: "dup", Name: "b"},
	}})
	if err == nil {
		t.Fatal("重复 team_id 必须显式报错，不静默丢弃")
	}
	err = router.WriteTeamLibraryGlobal(TeamLibrary{Teams: []TeamLibraryEntry{
		{TeamID: "t", Roles: []TeamRoleSpec{{RoleName: "reviewer"}, {RoleName: "reviewer"}}},
	}})
	if err == nil {
		t.Fatal("重复角色名必须显式报错")
	}

	if err := router.WriteTeamLibraryGlobal(TeamLibrary{Teams: []TeamLibraryEntry{
		{
			TeamID:      "review-team",
			Name:        "评审团队",
			TeamKind:    "review-team",
			OrderPolicy: "user_main_decided",
			// 顺序表刻意不是字典序：写回必须保序。
			OrderRoles: []string{"user", "main", "reviewer", ""},
			Roles: []TeamRoleSpec{
				{RoleName: "reviewer", RoleKind: "agent", ToolsPolicy: "readonly", SystemPrompt: "你是评审员"},
			},
			Origin: TeamLibraryOriginCurrent,
		},
	}}); err != nil {
		t.Fatalf("write team library: %v", err)
	}

	stored, err := router.ReadTeamLibraryGlobal(projectID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Configured || len(stored.Teams) != 1 {
		t.Fatalf("读回团队库 = %+v", stored)
	}
	entry := stored.Teams[0]
	if entry.TeamID != "review-team" || entry.Name != "评审团队" || entry.TeamKind != "review-team" {
		t.Fatalf("条目身份 = %+v", entry)
	}
	if got := entry.OrderRoles; len(got) != 3 || got[0] != "user" || got[2] != "reviewer" {
		t.Fatalf("顺序表必须保序去空：%v", got)
	}
	if len(entry.Roles) != 1 || entry.Roles[0].ToolsPolicy != "readonly" || entry.Roles[0].SystemPrompt != "你是评审员" {
		t.Fatalf("角色配置 = %+v", entry.Roles)
	}
}

// TestTeamLibraryRejectsUnknownSchema：schema 版本不认识时显式报错，不静默降级。
func TestTeamLibraryRejectsUnknownSchema(t *testing.T) {
	router := newTestRouter(t)
	if err := router.WriteTeamLibraryGlobal(TeamLibrary{SchemaVersion: 99}); err == nil {
		t.Fatal("未知 schema 必须被拒绝")
	}
}

// TestTeamLibraryIsGlobalScoped：团队库是**全局**粒度——A 项目写入的库在 B 项目
// 也读得到（与旧的项目级布局相反）。
func TestTeamLibraryIsGlobalScoped(t *testing.T) {
	router := newTestRouter(t)
	router.SetWorkspace("project-a")
	if err := router.WriteTeamLibraryGlobal(TeamLibrary{Teams: []TeamLibraryEntry{
		{TeamID: "team-a", Name: "A 队"},
	}}); err != nil {
		t.Fatal(err)
	}
	other, err := router.ReadTeamLibraryGlobal("project-b")
	if err != nil {
		t.Fatal(err)
	}
	if !other.Configured || len(other.Teams) != 1 || other.Teams[0].TeamID != "team-a" {
		t.Fatalf("团队库必须全局可见，project-b 读到 %+v", other)
	}
}

// TestTeamLibraryLegacyReadThrough：全局库缺失时只读回退到旧的项目级库；一旦对
// 全局库发生一次整份写入（含回退读后的写回），内容并入全局、旧文件保持不动。
func TestTeamLibraryLegacyReadThrough(t *testing.T) {
	root := t.TempDir()
	router, err := NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })
	const projectID = "project-legacy"
	router.SetWorkspace(projectID)

	legacyPath := filepath.Join(root, "sessions-json", "project-"+hash(projectID), "teams", "library.json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(TeamLibrary{SchemaVersion: TeamLibrarySchemaVersion, Teams: []TeamLibraryEntry{
		{TeamID: "old-team", Name: "老队", OrderRoles: []string{"user", "main", "reviewer"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	read, err := router.ReadTeamLibraryGlobal(projectID)
	if err != nil {
		t.Fatalf("回退读旧库不应报错：%v", err)
	}
	if !read.Configured || len(read.Teams) != 1 || read.Teams[0].TeamID != "old-team" {
		t.Fatalf("应只读回退到旧项目库：%+v", read)
	}

	// 第一次写全局：把回退读到的内容连同新条目一起并入全局。
	if err := router.WriteTeamLibraryGlobal(TeamLibrary{Teams: append(read.Teams, TeamLibraryEntry{TeamID: "new-team", Name: "新队"})}); err != nil {
		t.Fatal(err)
	}
	after, err := router.ReadTeamLibraryGlobal("project-other")
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Teams) != 2 {
		t.Fatalf("回退内容应并入全局：%+v", after.Teams)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("旧库文件必须原样保留（只读回退、不搬数据）：%v", err)
	}

	// 全局库就位后，另一个项目不再看到旧项目库。
	isolated, err := router.ReadTeamLibraryGlobal("project-untouched")
	if err != nil {
		t.Fatal(err)
	}
	if len(isolated.Teams) != 2 {
		t.Fatalf("全局库就位后所有项目读同一份：%+v", isolated.Teams)
	}
}
