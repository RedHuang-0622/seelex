package role

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

// fakeLayout 是 ProjectLayoutPort 的测试实现：项目目录落在 t.TempDir() 下，
// 不碰真实根 store（这层契约正是为了让领域包能被独立测试）。
type fakeLayout struct {
	root string
}

func newFakeLayout(t *testing.T) *fakeLayout {
	t.Helper()
	return &fakeLayout{root: t.TempDir()}
}

// dir 复刻实现里的项目目录解析（projectID → 目录），供断言直接读盘用。
func (layout *fakeLayout) dir(projectID string) string {
	return filepath.Join(layout.root, "projects", storeroot.Hash(projectID))
}

func (layout *fakeLayout) WithProjectDir(projectID string, fn func(dir string) error) error {
	dir := layout.dir(projectID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return fn(dir)
}

// permissionPath 返回该项目的权限文件物理路径。
func (layout *fakeLayout) permissionPath(projectID string) string {
	return filepath.Join(layout.dir(projectID), rolePermissionFileName)
}

// TestPermissionPutVectorRoundTrip：Put 后 Vector 命中，Drop 后回到"从未定过"。
func TestPermissionPutVectorRoundTrip(t *testing.T) {
	layout := newFakeLayout(t)
	store := NewPermissionStore(layout)

	record, err := store.Put("project", "session-1", "reviewer", []string{" read ", "write", "read", ""})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !reflect.DeepEqual(record.PermissionVector, []string{"read", "write"}) {
		t.Fatalf("落盘返回值未归一：%+v", record.PermissionVector)
	}
	if record.SessionID != "session-1" || record.Role != "reviewer" {
		t.Fatalf("记录坐标漂移：%+v", record)
	}
	vector, ok, err := store.Vector("project", "session-1", "reviewer")
	if err != nil {
		t.Fatalf("Vector: %v", err)
	}
	if !ok || !reflect.DeepEqual(vector, []string{"read", "write"}) {
		t.Fatalf("Vector = %v,%v，want [read write],true", vector, ok)
	}

	// 未登记的角色必须报 ok=false（调用方按链路解析）。
	if _, ok, err := store.Vector("project", "session-1", "goalplan"); err != nil || ok {
		t.Fatalf("未登记角色 = ok=%v err=%v，want false,nil", ok, err)
	}

	if err := store.Drop("project", "session-1", "reviewer"); err != nil {
		t.Fatalf("Drop: %v", err)
	}
	if _, ok, err := store.Vector("project", "session-1", "reviewer"); err != nil || ok {
		t.Fatalf("Drop 后 = ok=%v err=%v，want false,nil", ok, err)
	}
}

// TestExplicitEmptyVectorDiffersFromUnset：显式空集是"什么都不给"，与"没定过"必须分得开，
// 且在盘上序列化为 `[]` 而不是丢字段。
func TestExplicitEmptyVectorDiffersFromUnset(t *testing.T) {
	layout := newFakeLayout(t)
	store := NewPermissionStore(layout)
	if _, err := store.Put("project", "session-1", "reviewer", []string{}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	vector, ok, err := store.Vector("project", "session-1", "reviewer")
	if err != nil {
		t.Fatalf("Vector: %v", err)
	}
	if !ok {
		t.Fatal("显式空集必须报 ok=true（否则会回落到默认链，那是权限放大）")
	}
	if vector == nil {
		t.Fatal("显式空集必须交回非 nil 空切片")
	}
	if len(vector) != 0 {
		t.Fatalf("显式空集 = %v，want 空", vector)
	}

	payload, err := os.ReadFile(layout.permissionPath("project"))
	if err != nil {
		t.Fatalf("读取权限文件：%v", err)
	}
	if !strings.Contains(string(payload), `"permission_vector": []`) {
		t.Fatalf("显式空集必须序列化成 []（不能丢字段）：%s", payload)
	}

	// 对照组：没定过的角色 Vector 报 ok=false。
	if _, ok, err := store.Vector("project", "session-1", "goalplan"); err != nil || ok {
		t.Fatalf("未登记角色 = ok=%v err=%v，want false,nil", ok, err)
	}
}

// TestPermissionFileShapeAndMode：文件名、版本、目录/文件权限与原子写后的 JSON 形状。
func TestPermissionFileShapeAndMode(t *testing.T) {
	layout := newFakeLayout(t)
	store := NewPermissionStore(layout)
	if _, err := store.Put("project", "session-1", "reviewer", []string{"read"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	path := layout.permissionPath("project")
	if filepath.Base(path) != "role-permissions.json" {
		t.Fatalf("文件名 = %q，want role-permissions.json（改名即数据迁移）", filepath.Base(path))
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取权限文件：%v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("权限文件必须是合法 JSON：%v", err)
	}
	version, ok := decoded["version"].(float64)
	if !ok || int(version) != rolePermissionLayoutVer {
		t.Fatalf("version = %v，want %d", decoded["version"], rolePermissionLayoutVer)
	}
	sessions, ok := decoded["sessions"].(map[string]any)
	if !ok {
		t.Fatalf("sessions 形状漂移：%s", payload)
	}
	if _, ok := sessions["session-1"].(map[string]any); !ok {
		t.Fatalf("sessions 必须按 sessionID 分组：%s", payload)
	}
	// 权限位只在 POSIX 上可断言（Windows 的 Mode().Perm() 恒为 0666，读不出 0600）。
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != rolePermissionFileMode {
			t.Fatalf("权限文件 mode = %#o, want %#o", info.Mode().Perm(), rolePermissionFileMode)
		}
	}
}

// TestCorruptPermissionFileReadsEmptyThenRewrites：坏文件按空登记读（收紧方向），
// 下一次写回要重建成合法文件。
func TestCorruptPermissionFileReadsEmptyThenRewrites(t *testing.T) {
	layout := newFakeLayout(t)
	store := NewPermissionStore(layout)
	if err := os.MkdirAll(layout.dir("project"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(layout.permissionPath("project"), []byte("{ 不是 JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	vector, ok, err := store.Vector("project", "session-1", "reviewer")
	if err != nil {
		t.Fatalf("坏文件不得让读面报错：%v", err)
	}
	if ok || len(vector) != 0 {
		t.Fatalf("坏文件读面 = %v,%v，want 空且未定过", vector, ok)
	}
	if defaults, err := store.Defaults("project"); err != nil || len(defaults) != 0 {
		t.Fatalf("坏文件的 Defaults = %v,%v，want 空", defaults, err)
	}

	if _, err := store.Put("project", "session-1", "reviewer", []string{"read"}); err != nil {
		t.Fatalf("坏文件存在时 Put: %v", err)
	}
	payload, err := os.ReadFile(layout.permissionPath("project"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded permissionFile
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("写回后必须是合法 JSON：%v（%s）", err, payload)
	}
	if decoded.Version != rolePermissionLayoutVer {
		t.Fatalf("写回后 version = %d", decoded.Version)
	}
	if got := decoded.Sessions["session-1"]["reviewer"].PermissionVector; !reflect.DeepEqual(got, []string{"read"}) {
		t.Fatalf("写回后向量 = %v", got)
	}
}

// TestRoleDefaultsRoundTrip：角色级默认向量的写入/读取/删除与三态纪律。
func TestRoleDefaultsRoundTrip(t *testing.T) {
	layout := newFakeLayout(t)
	store := NewPermissionStore(layout)

	if _, err := store.PutDefault("project", "teammate", []string{"read", "grep"}); err != nil {
		t.Fatalf("PutDefault: %v", err)
	}
	vector, ok, err := store.Default("project", "teammate")
	if err != nil || !ok || !reflect.DeepEqual(vector, []string{"read", "grep"}) {
		t.Fatalf("Default = %v,%v,%v", vector, ok, err)
	}
	defaults, err := store.Defaults("project")
	if err != nil || !reflect.DeepEqual(defaults["teammate"], []string{"read", "grep"}) {
		t.Fatalf("Defaults = %v,%v", defaults, err)
	}

	// 显式空集默认：ok=true 且非 nil。
	if _, err := store.PutDefault("project", "subagent", []string{}); err != nil {
		t.Fatalf("PutDefault(empty): %v", err)
	}
	empty, ok, err := store.Default("project", "subagent")
	if err != nil || !ok || empty == nil || len(empty) != 0 {
		t.Fatalf("显式空集默认 = %v,%v,%v", empty, ok, err)
	}

	if err := store.DropDefault("project", "teammate"); err != nil {
		t.Fatalf("DropDefault: %v", err)
	}
	if _, ok, err := store.Default("project", "teammate"); err != nil || ok {
		t.Fatalf("DropDefault 后 = ok=%v err=%v，want false,nil", ok, err)
	}
	// 删除一个角色的默认不得影响另一个。
	if _, ok, err := store.Default("project", "subagent"); err != nil || !ok {
		t.Fatalf("DropDefault 误伤其它角色：ok=%v err=%v", ok, err)
	}
}

// TestRoleAccountInheritanceAndUserSpaces：账号 / 继承 / SSH 路径三类默认共用一条记录，
// 写一个字段不得清掉另一个。
func TestRoleAccountInheritanceAndUserSpaces(t *testing.T) {
	layout := newFakeLayout(t)
	store := NewPermissionStore(layout)

	if _, ok, err := store.RoleAccount("project", "goalplan"); err != nil || ok {
		t.Fatalf("未设置的账号 = ok=%v err=%v", ok, err)
	}
	if err := store.SetRoleAccount("project", "goalplan", " work "); err != nil {
		t.Fatalf("SetRoleAccount: %v", err)
	}
	account, ok, err := store.RoleAccount("project", "goalplan")
	if err != nil || !ok || account != "work" {
		t.Fatalf("RoleAccount = %q,%v,%v，want work,true,nil", account, ok, err)
	}

	if _, ok, err := store.RoleInheritance("project", "goalplan"); err != nil || ok {
		t.Fatalf("未设置的继承 = ok=%v err=%v", ok, err)
	}
	if err := store.SetRoleInheritance("project", "goalplan", true); err != nil {
		t.Fatalf("SetRoleInheritance: %v", err)
	}
	inherit, ok, err := store.RoleInheritance("project", "goalplan")
	if err != nil || !ok || !inherit {
		t.Fatalf("RoleInheritance = %v,%v,%v，want true,true,nil", inherit, ok, err)
	}

	if err := store.SetRoleSSHUserSpaces("project", "goalplan", []string{"/srv/a"}); err != nil {
		t.Fatalf("SetRoleSSHUserSpaces: %v", err)
	}
	spaces, err := store.RoleSSHUserSpaces("project", "goalplan")
	if err != nil || !reflect.DeepEqual(spaces, []string{"/srv/a"}) {
		t.Fatalf("RoleSSHUserSpaces = %v,%v", spaces, err)
	}

	// 三条写入共用一条记录：前面的字段都必须还在。
	if account, ok, err := store.RoleAccount("project", "goalplan"); err != nil || !ok || account != "work" {
		t.Fatalf("写 SSH 路径把账号冲掉了：%q,%v,%v", account, ok, err)
	}
	if inherit, ok, err := store.RoleInheritance("project", "goalplan"); err != nil || !ok || !inherit {
		t.Fatalf("写 SSH 路径把继承冲掉了：%v,%v,%v", inherit, ok, err)
	}
	if _, err := store.PutDefault("project", "goalplan", []string{"read"}); err != nil {
		t.Fatalf("PutDefault: %v", err)
	}
	if account, ok, _ := store.RoleAccount("project", "goalplan"); !ok || account != "work" {
		t.Fatalf("写默认向量把账号冲掉了：%q,%v", account, ok)
	}
}

// TestRoleEffectiveSSHUserSpacesPrefersSession：会话级覆盖优先于项目级角色默认。
func TestRoleEffectiveSSHUserSpacesPrefersSession(t *testing.T) {
	layout := newFakeLayout(t)
	store := NewPermissionStore(layout)

	if err := store.SetRoleSSHUserSpaces("project", "reviewer", []string{"/default"}); err != nil {
		t.Fatalf("SetRoleSSHUserSpaces: %v", err)
	}
	// 没有会话覆盖时回落到角色默认。
	effective, err := store.RoleEffectiveSSHUserSpaces("project", "session-1", "reviewer")
	if err != nil || !reflect.DeepEqual(effective, []string{"/default"}) {
		t.Fatalf("未设会话覆盖时 = %v,%v，want [/default]", effective, err)
	}
	// 会话显式覆盖（含显式空集）优先。
	if err := store.SetRoleSessionSSHUserSpaces("project", "session-1", "reviewer", []string{"/session"}); err != nil {
		t.Fatalf("SetRoleSessionSSHUserSpaces: %v", err)
	}
	effective, err = store.RoleEffectiveSSHUserSpaces("project", "session-1", "reviewer")
	if err != nil || !reflect.DeepEqual(effective, []string{"/session"}) {
		t.Fatalf("会话覆盖未生效：%v,%v", effective, err)
	}
	if err := store.SetRoleSessionSSHUserSpaces("project", "session-2", "reviewer", []string{}); err != nil {
		t.Fatalf("SetRoleSessionSSHUserSpaces(empty): %v", err)
	}
	effective, err = store.RoleEffectiveSSHUserSpaces("project", "session-2", "reviewer")
	if err != nil || len(effective) != 0 {
		t.Fatalf("显式空覆盖必须压住默认：%v,%v", effective, err)
	}
	// 其它会话仍回落到默认（覆盖只作用于自己那格）。
	effective, err = store.RoleEffectiveSSHUserSpaces("project", "session-3", "reviewer")
	if err != nil || !reflect.DeepEqual(effective, []string{"/default"}) {
		t.Fatalf("其它会话被串写：%v,%v", effective, err)
	}
}

// TestDropSessionClearsOnlyThatSession：会话删除只清自己的登记。
func TestDropSessionClearsOnlyThatSession(t *testing.T) {
	layout := newFakeLayout(t)
	store := NewPermissionStore(layout)
	for _, sessionID := range []string{"session-1", "session-2"} {
		if _, err := store.Put("project", sessionID, "reviewer", []string{"read"}); err != nil {
			t.Fatalf("Put(%s): %v", sessionID, err)
		}
	}
	if err := store.DropSession("project", "session-1"); err != nil {
		t.Fatalf("DropSession: %v", err)
	}
	if _, ok, err := store.Vector("project", "session-1", "reviewer"); err != nil || ok {
		t.Fatalf("DropSession 未清掉自身登记：ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.Vector("project", "session-2", "reviewer"); err != nil || !ok {
		t.Fatalf("DropSession 误伤其它会话：ok=%v err=%v", ok, err)
	}
}

// TestConcurrentPutDifferentRolesKeepsAll：同一个项目文件被多个角色并发写，
// 谁都不许覆盖别人的记录（缺文件锁就会只剩最后一份）。
func TestConcurrentPutDifferentRolesKeepsAll(t *testing.T) {
	layout := newFakeLayout(t)
	store := NewPermissionStore(layout)
	roles := []string{"agent", "subagent", "goalplan", "teammate", "reviewer", "tl", "main", "user"}

	var wait sync.WaitGroup
	wait.Add(len(roles))
	for _, role := range roles {
		role := role
		go func() {
			defer wait.Done()
			if _, err := store.Put("project", "session-1", role, []string{"read:" + role}); err != nil {
				t.Errorf("Put(%s): %v", role, err)
			}
		}()
	}
	wait.Wait()

	// 独立实例重读（绕过进程内缓存），确认盘上真的都在。
	fresh := NewPermissionStore(layout)
	vectors, err := fresh.Vectors("project", "session-1")
	if err != nil {
		t.Fatalf("Vectors: %v", err)
	}
	if len(vectors) != len(roles) {
		t.Fatalf("盘上只剩 %d 条登记，want %d：并发写覆盖了别人的记录", len(vectors), len(roles))
	}
	for _, role := range roles {
		want := []string{"read:" + role}
		if !reflect.DeepEqual(vectors[role], want) {
			t.Fatalf("角色 %s 的向量 = %v，want %v", role, vectors[role], want)
		}
	}
}

// TestValidationErrorsAndUnassembledStore：参数校验与未装配端口的显式失败。
func TestValidationErrorsAndUnassembledStore(t *testing.T) {
	layout := newFakeLayout(t)
	store := NewPermissionStore(layout)
	if _, err := store.Put("project", "", "reviewer", []string{"read"}); err == nil {
		t.Fatal("空 sessionID 必须报错")
	}
	if _, err := store.Put("project", "session-1", "", []string{"read"}); err == nil {
		t.Fatal("空 role 必须报错")
	}
	if err := store.Drop("project", "", "reviewer"); err == nil {
		t.Fatal("空参数 Drop 必须报错")
	}
	if _, err := store.Vectors("project", ""); err == nil {
		t.Fatal("空 sessionID Vectors 必须报错")
	}
	if err := store.DropSession("project", ""); err == nil {
		t.Fatal("空 sessionID DropSession 必须报错")
	}
	if _, err := store.PutDefault("project", "", nil); err == nil {
		t.Fatal("空 role PutDefault 必须报错")
	}
	if err := store.DropDefault("project", ""); err == nil {
		t.Fatal("空 role DropDefault 必须报错")
	}
	if _, _, err := store.Default("project", ""); err == nil {
		t.Fatal("空 role Default 必须报错")
	}
	if _, _, err := store.RoleAccount("project", ""); err == nil {
		t.Fatal("空 role RoleAccount 必须报错")
	}
	if _, _, err := store.RoleInheritance("project", ""); err == nil {
		t.Fatal("空 role RoleInheritance 必须报错")
	}
	if _, err := store.RoleSSHUserSpaces("project", ""); err == nil {
		t.Fatal("空 role RoleSSHUserSpaces 必须报错")
	}
	if _, err := store.RoleEffectiveSSHUserSpaces("project", "", "reviewer"); err == nil {
		t.Fatal("空 sessionID RoleEffectiveSSHUserSpaces 必须报错")
	}

	// 未装配端口：读退化为空登记，写显式失败（不假装写成功）。
	unassembled := NewPermissionStore(nil)
	if vectors, err := unassembled.Vectors("project", "session-1"); err != nil || len(vectors) != 0 {
		t.Fatalf("未装配的读 = %v,%v，want 空且无错", vectors, err)
	}
	if defaults, err := unassembled.Defaults("project"); err != nil || len(defaults) != 0 {
		t.Fatalf("未装配的 Defaults = %v,%v，want 空且无错", defaults, err)
	}
	if _, err := unassembled.Put("project", "session-1", "reviewer", []string{"read"}); err == nil {
		t.Fatal("未装配的写必须显式失败")
	}
	// nil 接收者：读路径有显式判据，退化为空登记而不是崩。
	var nilStore *PermissionStore
	if vectors, err := nilStore.Vectors("project", "session-1"); err != nil || len(vectors) != 0 {
		t.Fatalf("nil 接收者的读 = %v,%v，want 空且无错", vectors, err)
	}
	if err := nilStore.mutate("project", func(*permissionFile) {}); err == nil {
		t.Fatal("nil 接收者的写必须显式失败")
	}
}
