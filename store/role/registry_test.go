package role

// registry_test.go — team registry（`session/team/roles.json`）owner 的行为门禁。
//
// M8 把读写/删除与规范化从根 storeEngine 迁到这里；这些断言直接跑在 `Store` 上：
// 物理路径、整份替换、规范化判据、幂等删除与冷读全部由本包负责。

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	storelayout "github.com/RedHuang-0622/seelex/store/internal/layout"
)

func registryFixture(t *testing.T) (*Store, *storelayout.Layout, storelayout.Scope) {
	t.Helper()
	layout := storelayout.New(t.TempDir())
	scope := storelayout.Scope{ProjectID: "project-role", SessionID: "session-role"}
	return New(layout), layout, scope
}

// TestRegistryPathIsUnchanged 钉住"零数据迁移"：注册表仍在 <sessionRoot>/team/roles.json。
func TestRegistryPathIsUnchanged(t *testing.T) {
	store, layout, scope := registryFixture(t)
	want := filepath.Join(layout.SessionRoot(scope), "team", "roles.json")
	if got := store.Path(scope); got != want {
		t.Fatalf("注册表路径变了：got %q want %q", got, want)
	}
	if RegistryDir != "team" || RegistryFile != "roles.json" {
		t.Fatalf("物理名字变了：%q / %q", RegistryDir, RegistryFile)
	}
}

// TestReadMissingMeansUnconfigured：文件不存在 = 没配置过团队，不是错误。
func TestReadMissingMeansUnconfigured(t *testing.T) {
	store, _, scope := registryFixture(t)
	registry, err := store.Read(scope)
	if err != nil {
		t.Fatalf("缺失注册表不是错误：%v", err)
	}
	if registry.Configured || len(registry.Roles) != 0 {
		t.Fatalf("缺失必须是未配置：%+v", registry)
	}
	if registry.SchemaVersion != SchemaVersion {
		t.Fatalf("schema 版本必须是当前版本：%d", registry.SchemaVersion)
	}
}

// TestWriteReadRoundTripAndNormalizes：写面按规范化落盘，读面原样返回。
func TestWriteReadRoundTripAndNormalizes(t *testing.T) {
	store, _, scope := registryFixture(t)
	if err := store.Write(scope, Registry{
		TeamID:      "  review-team  ",
		TeamKind:    "review-team",
		OrderPolicy: "user_main_decided",
		Roles: []RoleSpec{
			{RoleName: "digest", RoleKind: "timer", OrderPriority: 9, JoinPolicy: "scheduled"},
			{RoleName: " reviewer ", OrderPriority: 2, ToolsPolicy: "readonly",
				MirrorPolicy: []string{"goal.update", "", "goal.start", "goal.start"}},
		},
	}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	registry, err := store.Read(scope)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !registry.Configured || registry.TeamID != "review-team" || registry.TeamKind != "review-team" {
		t.Fatalf("注册表头部不对：%+v", registry)
	}
	if len(registry.Roles) != 2 {
		t.Fatalf("角色数 = %d, want 2", len(registry.Roles))
	}
	if registry.Roles[0].RoleName != "reviewer" || registry.Roles[0].RoleKind != "agent" {
		t.Fatalf("首角色必须按 order_priority 排序，且 kind 默认 agent：%+v", registry.Roles[0])
	}
	if got := registry.Roles[0].MirrorPolicy; !reflect.DeepEqual(got, []string{"goal.start", "goal.update"}) {
		t.Fatalf("字符串集合必须去空、去重、排序：%+v", got)
	}
	if registry.Roles[0].OrderPriority != 2 || registry.Roles[1].OrderPriority != 9 {
		t.Fatalf("角色必须按 order_priority 升序：%+v", registry.Roles)
	}
	if registry.UpdatedAt.IsZero() {
		t.Fatal("写入必须记录 updated_at")
	}
}

// TestWriteReplacesWholeRegistry：整份替换——第二份写入后第一份的角色不能残留。
func TestWriteReplacesWholeRegistry(t *testing.T) {
	store, _, scope := registryFixture(t)
	if err := store.Write(scope, Registry{Roles: []RoleSpec{
		{RoleName: "reviewer"}, {RoleName: "digest"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Write(scope, Registry{Roles: []RoleSpec{{RoleName: "reviewer"}}}); err != nil {
		t.Fatal(err)
	}
	registry, err := store.Read(scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Roles) != 1 || registry.Roles[0].RoleName != "reviewer" {
		t.Fatalf("整份替换后旧角色不得残留：%+v", registry.Roles)
	}
}

// TestNormalizeIsIdempotent：规范化不能反复漂移（两次规整结果必须逐字相同）。
func TestNormalizeIsIdempotent(t *testing.T) {
	dirty := Registry{
		TeamID: " t ",
		Roles: []RoleSpec{
			{RoleName: " b ", MirrorPolicy: []string{"z", "a", "a"}},
			{RoleName: "a", RoleKind: "agent", OrderPriority: 3},
		},
	}
	once, err := Normalize(dirty)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	twice, err := Normalize(once)
	if err != nil {
		t.Fatalf("Normalize(once): %v", err)
	}
	once.UpdatedAt, twice.UpdatedAt = time.Time{}, time.Time{}
	if !reflect.DeepEqual(once, twice) {
		t.Fatalf("规范化必须幂等：\nonce=%+v\ntwice=%+v", once, twice)
	}
}

// TestNormalizeRejectsInvalidInput：空名、重名、未知 schema 显式报错，不静默丢弃。
func TestNormalizeRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name     string
		registry Registry
	}{
		{"missing role_name", Registry{Roles: []RoleSpec{{RoleKind: "agent"}}}},
		{"duplicate role_name", Registry{Roles: []RoleSpec{{RoleName: "a"}, {RoleName: "a"}}}},
		{"unknown schema", Registry{SchemaVersion: 99}},
	}
	for _, item := range cases {
		if _, err := Normalize(item.registry); err == nil {
			t.Errorf("%s 必须被拒绝", item.name)
		}
	}
}

// TestWriteRejectsInvalidRegistryWithoutTouchingDisk：校验失败不得落盘。
func TestWriteRejectsInvalidRegistryWithoutTouchingDisk(t *testing.T) {
	store, _, scope := registryFixture(t)
	if err := store.Write(scope, Registry{SchemaVersion: 99}); err == nil {
		t.Fatal("未知 schema 必须被拒绝")
	}
	if _, err := os.Stat(store.Path(scope)); !os.IsNotExist(err) {
		t.Fatalf("被拒绝的写入不得创建文件：%v", err)
	}
}

// TestRemoveIsIdempotent：删除是"团队离场"，重复删除不是错误；删除后读面回到未配置。
func TestRemoveIsIdempotent(t *testing.T) {
	store, _, scope := registryFixture(t)
	if err := store.Write(scope, Registry{Roles: []RoleSpec{{RoleName: "reviewer"}}}); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := store.Remove(scope); err != nil {
			t.Fatalf("第 %d 次 Remove: %v", attempt+1, err)
		}
	}
	registry, err := store.Read(scope)
	if err != nil {
		t.Fatal(err)
	}
	if registry.Configured {
		t.Fatalf("删除后必须回到未配置：%+v", registry)
	}
}

// TestReadCorruptRegistryFailsLoudly：文件损坏必须报错，不返回半份配置。
func TestReadCorruptRegistryFailsLoudly(t *testing.T) {
	store, _, scope := registryFixture(t)
	if err := os.MkdirAll(filepath.Dir(store.Path(scope)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(scope), []byte(`{"roles": [`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(scope); err == nil {
		t.Fatal("损坏的注册表必须显式报错")
	}
}

// TestReadColdStartAndLegacySchemaZero：换实例仍可读，schema 缺省按当前版本理解。
func TestReadColdStartAndLegacySchemaZero(t *testing.T) {
	store, layout, scope := registryFixture(t)
	if err := os.MkdirAll(filepath.Dir(store.Path(scope)), 0o700); err != nil {
		t.Fatal(err)
	}
	// 旧文件没有 schema_version 字段：读面必须按当前版本理解，而不是报错。
	legacy := `{"team_kind":"review-team","roles":[{"role_name":"reviewer"}]}`
	if err := os.WriteFile(store.Path(scope), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	dataRoot := filepath.Dir(filepath.Dir(layout.SessionRoot(scope)))
	cold := New(storelayout.New(dataRoot))
	registry, err := cold.Read(scope)
	if err != nil {
		t.Fatalf("旧注册表必须可读：%v", err)
	}
	if !registry.Configured || registry.SchemaVersion != SchemaVersion || len(registry.Roles) != 1 {
		t.Fatalf("旧注册表读回不对：%+v", registry)
	}
}

// TestConcurrentWritesStayWhole：同一路径的整份替换互相串行，最终文件必须是一份完整配置。
func TestConcurrentWritesStayWhole(t *testing.T) {
	store, _, scope := registryFixture(t)
	const writers = 8
	var wait sync.WaitGroup
	for index := 0; index < writers; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			teamID := "team-" + string(rune('a'+index))
			if err := store.Write(scope, Registry{
				TeamID: teamID,
				Roles:  []RoleSpec{{RoleName: "reviewer"}, {RoleName: "digest", OrderPriority: 9}},
			}); err != nil {
				t.Errorf("并发写注册表：%v", err)
			}
		}(index)
	}
	wait.Wait()

	registry, err := store.Read(scope)
	if err != nil {
		t.Fatalf("并发后必须仍可解析：%v", err)
	}
	if !registry.Configured || !strings.HasPrefix(registry.TeamID, "team-") || len(registry.Roles) != 2 {
		t.Fatalf("并发后文件不是一份完整配置：%+v", registry)
	}
}
