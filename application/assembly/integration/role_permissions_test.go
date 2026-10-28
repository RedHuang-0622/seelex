package integration

import (
	"errors"
	assembly "github.com/RedHuang-0622/seelex/application/assembly"
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// role_permissions_test.go 钉住「会话加载时恢复每个角色权限」这条跳：
// 登记优先（含显式空集）、缺失走五级链路、未装配显式失败、并发读不串。
//
// 存储侧的并发保证（每角色一格各自持锁、每项目一把文件锁）由 store 包的
// role_permission_test.go 钉；这里钉的是应用层的**判据顺序**，两者不重复。

// rolePermissionFakeSessions 是实现了可选的登记/五层端口的会话端口桩。
type rolePermissionFakeSessions struct {
	fakeSessions
	mu          sync.Mutex
	vectors     map[string]map[string][]string
	defaults    map[string]map[string][]string
	accounts    map[string]map[string]string
	inherits    map[string]map[string]bool
	spaces      map[string]map[string][]string
	spacesSet   map[string]map[string]bool
	defaultsSSH map[string][]string
	layers      dto.RoleSettingsLayers
}

func newRolePermissionFakeSessions() *rolePermissionFakeSessions {
	return &rolePermissionFakeSessions{
		vectors:     map[string]map[string][]string{},
		defaults:    map[string]map[string][]string{},
		accounts:    map[string]map[string]string{},
		inherits:    map[string]map[string]bool{},
		spaces:      map[string]map[string][]string{},
		spacesSet:   map[string]map[string]bool{},
		defaultsSSH: map[string][]string{},
	}
}

func (fake *rolePermissionFakeSessions) RolePermissionVectors(sessionID string) (map[string][]string, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	out := map[string][]string{}
	for role, vector := range fake.vectors[sessionID] {
		out[role] = append([]string{}, vector...)
	}
	return out, nil
}

func (fake *rolePermissionFakeSessions) RolePermissionVector(sessionID, role string) ([]string, bool, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	vector, ok := fake.vectors[sessionID][role]
	if !ok {
		return nil, false, nil
	}
	return append([]string{}, vector...), true, nil
}

func (fake *rolePermissionFakeSessions) SetRolePermissionVector(sessionID, role string, vector []string) error {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.vectors[sessionID] == nil {
		fake.vectors[sessionID] = map[string][]string{}
	}
	fake.vectors[sessionID][role] = append([]string{}, vector...)
	return nil
}

func (fake *rolePermissionFakeSessions) RolePermissionLayers(sessionID, role string) dto.RoleSettingsLayers {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.layers
}

func (fake *rolePermissionFakeSessions) RoleDefaultVectors(sessionID string) (map[string][]string, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	out := map[string][]string{}
	for role, vector := range fake.defaults[sessionID] {
		out[role] = append([]string{}, vector...)
	}
	return out, nil
}

func (fake *rolePermissionFakeSessions) RoleDefault(sessionID, role string) ([]string, bool, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	vector, ok := fake.defaults[sessionID][role]
	return append([]string{}, vector...), ok, nil
}

func (fake *rolePermissionFakeSessions) SetRoleDefault(sessionID, role string, vector []string) error {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.defaults[sessionID] == nil {
		fake.defaults[sessionID] = map[string][]string{}
	}
	fake.defaults[sessionID][role] = append([]string{}, vector...)
	return nil
}

func (fake *rolePermissionFakeSessions) RoleAccount(sessionID, role string) (string, bool, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	account, ok := fake.accounts[sessionID][role]
	return account, ok, nil
}

func (fake *rolePermissionFakeSessions) SetRoleAccount(sessionID, role, account string) error {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.accounts[sessionID] == nil {
		fake.accounts[sessionID] = map[string]string{}
	}
	fake.accounts[sessionID][role] = account
	return nil
}

func (fake *rolePermissionFakeSessions) RoleInheritance(sessionID, role string) (bool, bool, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	inherit, ok := fake.inherits[sessionID][role]
	return inherit, ok, nil
}

func (fake *rolePermissionFakeSessions) SetRoleInheritance(sessionID, role string, inherit bool) error {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.inherits[sessionID] == nil {
		fake.inherits[sessionID] = map[string]bool{}
	}
	fake.inherits[sessionID][role] = inherit
	return nil
}

func (fake *rolePermissionFakeSessions) RoleSSHUserSpaces(sessionID, role string) ([]string, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.spacesSet[sessionID][role] {
		return append([]string{}, fake.spaces[sessionID][role]...), nil
	}
	return append([]string{}, fake.defaultsSSH[role]...), nil
}

func (fake *rolePermissionFakeSessions) RoleDefaultSSHUserSpaces(_ string, role string) ([]string, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return append([]string{}, fake.defaultsSSH[role]...), nil
}

func (fake *rolePermissionFakeSessions) SetRoleSSHUserSpaces(sessionID, role string, paths []string) error {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.defaultsSSH[role] = append([]string{}, paths...)
	return nil
}

func (fake *rolePermissionFakeSessions) SetRoleSessionSSHUserSpaces(sessionID, role string, paths []string) error {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.spaces[sessionID] == nil {
		fake.spaces[sessionID] = map[string][]string{}
	}
	if fake.spacesSet[sessionID] == nil {
		fake.spacesSet[sessionID] = map[string]bool{}
	}
	fake.spaces[sessionID][role] = append([]string{}, paths...)
	fake.spacesSet[sessionID][role] = true
	return nil
}

// fourLayerFixture 是五层中的四层配了一遍、且**互相冲突**的一组来源：
// user_allow 只给 ro；agent_dispatch 否决 rw；session_default 给 rw+ctl；
// global_default 给 adm。链路解析结果应为 [ro ctl adm]：rw 被高层的否决压住，
// ctl 没有任何高层否认（低层授予成立），adm 只出现在最低层（同样成立）。
func fourLayerFixture() dto.RoleSettingsLayers {
	return dto.RoleSettingsLayers{
		UserAllow: dto.RoleSettings{
			Account:          "worker-a",
			PermissionVector: []string{dto.PermissionTagRO},
		},
		AgentDispatch: dto.RoleSettings{
			PermissionVector: []string{"!" + dto.PermissionTagRW},
		},
		SessionDefault: dto.RoleSettings{
			DefaultPlugin:    "review",
			PermissionVector: []string{dto.PermissionTagRW, dto.PermissionTagCTL},
		},
		GlobalDefault: dto.RoleSettings{
			PermissionVector: []string{dto.PermissionTagBash},
		},
	}
}

func TestRestoreRolePermissionsPrefersStoredVector(t *testing.T) {
	sessions := newRolePermissionFakeSessions()
	sessions.layers = fourLayerFixture()
	if err := sessions.SetRolePermissionVector("sess-1", "tl", []string{dto.PermissionTagRWSession}); err != nil {
		t.Fatalf("SetRolePermissionVector: %v", err)
	}
	service := newLegacyTestService(t, &fakeEngine{}, withTestSessions(sessions))

	restored, err := service.components.roleApplication.RestoreRolePermissions("sess-1", []string{"tl", "coworker"})
	if err != nil {
		t.Fatalf("RestoreRolePermissions: %v", err)
	}
	if want := ([]string{dto.PermissionTagRWSession}); !reflect.DeepEqual(restored["tl"].PermissionVector, want) {
		t.Fatalf("tl 向量 = %v, want %v（登记里定过的不能被链路覆盖）", restored["tl"].PermissionVector, want)
	}
	// 未登记的角色走链路：向量按五级优先级合成，标量字段取首个非零层。
	coworker := restored["coworker"]
	if want := ([]string{dto.PermissionTagRO, dto.PermissionTagCTL, dto.PermissionTagBash}); !reflect.DeepEqual(coworker.PermissionVector, want) {
		t.Fatalf("coworker 向量 = %v, want %v", coworker.PermissionVector, want)
	}
	if coworker.Account != "worker-a" || coworker.DefaultPlugin != "review" {
		t.Fatalf("coworker 标量 = %+v, want account=worker-a plugin=review", coworker)
	}
}

// TestRestoreRolePermissionsExplicitEmptyVectorWins 是三态纪律的核心用例：
// 登记存在但向量为空 = "显式不给任何能力"，不得回落到链路（那是权限放大方向）。
func TestRestoreRolePermissionsExplicitEmptyVectorWins(t *testing.T) {
	sessions := newRolePermissionFakeSessions()
	sessions.layers = fourLayerFixture()
	if err := sessions.SetRolePermissionVector("sess-1", "coworker", nil); err != nil {
		t.Fatalf("SetRolePermissionVector: %v", err)
	}
	service := newLegacyTestService(t, &fakeEngine{}, withTestSessions(sessions))

	restored, err := service.components.roleApplication.RestoreRolePermissions("sess-1", []string{"coworker"})
	if err != nil {
		t.Fatalf("RestoreRolePermissions: %v", err)
	}
	vector := restored["coworker"].PermissionVector
	if vector == nil || len(vector) != 0 {
		t.Fatalf("向量 = %v, want 非 nil 空切片（显式空集不得被链路补回来）", vector)
	}
}

func TestRestoreRolePermissionsWithoutRegistryFailsLoudly(t *testing.T) {
	service := newLegacyTestService(t, &fakeEngine{}) // 默认 fakeSessions 不暴露登记能力
	if _, err := service.components.roleApplication.RestoreRolePermissions("sess-1", []string{"tl"}); !errors.Is(err, assembly.ErrRolePermissionsUnavailable) {
		t.Fatalf("错误 = %v, want assembly.ErrRolePermissionsUnavailable", err)
	}
	if _, err := service.components.roleApplication.RolePermissionVector("sess-1", "tl"); !errors.Is(err, assembly.ErrRolePermissionsUnavailable) {
		t.Fatalf("单角色读错误 = %v, want assembly.ErrRolePermissionsUnavailable", err)
	}
	if err := service.components.roleApplication.SetRolePermissionVector("sess-1", "tl", []string{dto.PermissionTagRO}); !errors.Is(err, assembly.ErrRolePermissionsUnavailable) {
		t.Fatalf("写错误 = %v, want assembly.ErrRolePermissionsUnavailable", err)
	}
	if _, err := service.components.roleApplication.RestoreRolePermissions("  ", []string{"tl"}); err == nil {
		t.Fatal("缺会话号必须报错")
	}
}

func TestRolePermissionVectorSingleRole(t *testing.T) {
	sessions := newRolePermissionFakeSessions()
	sessions.layers = fourLayerFixture()
	service := newLegacyTestService(t, &fakeEngine{}, withTestSessions(sessions))

	vector, err := service.components.roleApplication.RolePermissionVector("sess-1", "coworker")
	if err != nil {
		t.Fatalf("RolePermissionVector: %v", err)
	}
	if want := ([]string{dto.PermissionTagRO, dto.PermissionTagCTL, dto.PermissionTagBash}); !reflect.DeepEqual(vector, want) {
		t.Fatalf("向量 = %v, want %v（与整会话恢复同一判据）", vector, want)
	}

	// 登记过之后单角色读也走登记。
	if err := service.components.roleApplication.SetRolePermissionVector("sess-1", "coworker", []string{" RW ", "!adm", "rw"}); err != nil {
		t.Fatalf("SetRolePermissionVector: %v", err)
	}
	vector, err = service.components.roleApplication.RolePermissionVector("sess-1", "coworker")
	if err != nil {
		t.Fatalf("RolePermissionVector: %v", err)
	}
	if want := ([]string{dto.PermissionTagRW, "!" + dto.PermissionTagBash}); !reflect.DeepEqual(vector, want) {
		t.Fatalf("写入后向量 = %v, want %v（写入侧已规整去重）", vector, want)
	}
}

func TestRoleDefaultVectorIsExplicitNotEffective(t *testing.T) {
	sessions := newRolePermissionFakeSessions()
	sessions.layers = fourLayerFixture()
	service := newLegacyTestService(t, &fakeEngine{}, withTestSessions(sessions))

	vector, explicit, err := service.components.roleApplication.RoleDefaultVector("sess-1", "coworker")
	if err != nil {
		t.Fatalf("初始 RoleDefaultVector: %v", err)
	}
	if explicit || vector == nil || len(vector) != 0 {
		t.Fatalf("未设置时 = %v explicit=%v，want 空向量 + false", vector, explicit)
	}
	if err := service.components.roleApplication.SetRoleDefaultVector("sess-1", "coworker", []string{dto.PermissionTagRW}); err != nil {
		t.Fatalf("SetRoleDefaultVector: %v", err)
	}
	vector, explicit, err = service.components.roleApplication.RoleDefaultVector("sess-1", "coworker")
	if err != nil {
		t.Fatalf("设置后 RoleDefaultVector: %v", err)
	}
	if !explicit || !reflect.DeepEqual(vector, []string{dto.PermissionTagRW}) {
		t.Fatalf("默认 = %v explicit=%v，want [rw] true", vector, explicit)
	}
}

func TestRoleAccountRoundTrip(t *testing.T) {
	sessions := newRolePermissionFakeSessions()
	service := newLegacyTestService(t, &fakeEngine{}, withTestSessions(sessions))
	if account, explicit, err := service.components.roleApplication.RoleAccount("sess-1", "teammate"); err != nil || explicit || account != "" {
		t.Fatalf("未设置 = %q %v %v", account, explicit, err)
	}
	if err := service.components.roleApplication.SetRoleAccount("sess-1", "teammate", "teammate-2"); err != nil {
		t.Fatal(err)
	}
	account, explicit, err := service.components.roleApplication.RoleAccount("sess-1", "teammate")
	if err != nil || !explicit || account != "teammate-2" {
		t.Fatalf("读回 = %q %v %v", account, explicit, err)
	}
}

type roleAccountRuntimeSpy struct {
	*fakeRuntime
	role, account string
}

func (spy *roleAccountRuntimeSpy) SetRoleAccount(role, account string) error {
	spy.role, spy.account = role, account
	return nil
}

func TestRoleAccountPushesToRuntimeAndRestores(t *testing.T) {
	sessions := newRolePermissionFakeSessions()
	runtime := &roleAccountRuntimeSpy{fakeRuntime: &fakeRuntime{}}
	service := newLegacyTestService(t, &fakeEngine{}, withTestSessions(sessions), withTestRuntime(runtime))
	if err := service.components.roleApplication.SetRoleAccount("sess-1", "teammate", "teammate-2"); err != nil {
		t.Fatal(err)
	}
	if runtime.role != "teammate" || runtime.account != "teammate-2" {
		t.Fatalf("运行时收到 %q %q", runtime.role, runtime.account)
	}
	runtime.role, runtime.account = "", ""
	if err := service.components.roleApplication.RestoreRoleAccounts("sess-1"); err != nil {
		t.Fatal(err)
	}
	if runtime.role != "teammate" || runtime.account != "teammate-2" {
		t.Fatalf("恢复后运行时收到 %q %q", runtime.role, runtime.account)
	}
}

func TestRoleInheritanceDefaultsAndUserSpaceNormalization(t *testing.T) {
	sessions := newRolePermissionFakeSessions()
	service := newLegacyTestService(t, &fakeEngine{}, withTestSessions(sessions))
	if inherit, err := service.components.roleApplication.RoleInheritMainAgent("sess-1", "teammate"); err != nil || !inherit {
		t.Fatalf("teammate 默认应继承 mainagent：%v %v", inherit, err)
	}
	if inherit, err := service.components.roleApplication.RoleInheritMainAgent("sess-1", "goalplan"); err != nil || inherit {
		t.Fatalf("goalplan 不默认继承：%v %v", inherit, err)
	}
	if err := service.components.roleApplication.SetRoleInheritMainAgent("sess-1", "goalplan", true); err == nil {
		t.Fatal("goalplan 不允许切继承")
	}
	if err := service.components.roleApplication.SetRoleSSHUserSpaces("sess-1", "teammate", []string{"/home/u/.cache", " /srv/shared ", "/home/u/.cache"}); err != nil {
		t.Fatal(err)
	}
	if err := service.components.roleApplication.SetRoleInheritMainAgent("sess-1", "teammate", false); err != nil {
		t.Fatal(err)
	}
	paths, err := service.components.roleApplication.RoleSSHUserSpaces("sess-1", "teammate")
	if err != nil || !reflect.DeepEqual(paths, []string{"/home/u/.cache", "/srv/shared"}) {
		t.Fatalf("paths=%v err=%v", paths, err)
	}
	for _, invalid := range [][]string{{"relative"}, {"/"}, {}} {
		if err := service.components.roleApplication.SetRoleSSHUserSpaces("sess-1", "teammate", invalid); err == nil {
			t.Fatalf("非法路径 %v 必须报错", invalid)
		}
	}
}

func TestInheritedRoleUsesMainAgentSSHUserSpaces(t *testing.T) {
	sessions := newRolePermissionFakeSessions()
	service := newLegacyTestService(t, &fakeEngine{}, withTestSessions(sessions))
	if err := service.components.roleApplication.SetRoleSSHUserSpaces("sess-1", "agent", []string{"/srv/shared"}); err != nil {
		t.Fatal(err)
	}
	// teammate 默认继承；自己的旧路径不能偷偷覆盖继承来的允许范围。
	if err := service.components.roleApplication.SetRoleSSHUserSpaces("sess-1", "teammate", []string{"/tmp/stale"}); err != nil {
		t.Fatal(err)
	}
	paths, err := service.components.roleApplication.RoleSSHUserSpaces("sess-1", "teammate")
	if err != nil || !reflect.DeepEqual(paths, []string{"/srv/shared"}) {
		t.Fatalf("继承态 paths=%v err=%v，want mainagent 的 /srv/shared", paths, err)
	}
	if err := service.components.roleApplication.SetRoleInheritMainAgent("sess-1", "teammate", false); err != nil {
		t.Fatal(err)
	}
	paths, err = service.components.roleApplication.RoleSSHUserSpaces("sess-1", "teammate")
	if err != nil || !reflect.DeepEqual(paths, []string{"/tmp/stale"}) {
		t.Fatalf("关闭继承后 paths=%v err=%v，want 角色自己的 /tmp/stale", paths, err)
	}
}

func TestSessionSSHUserSpacesOverrideDefaultAndFollowInheritance(t *testing.T) {
	sessions := newRolePermissionFakeSessions()
	service := newLegacyTestService(t, &fakeEngine{}, withTestSessions(sessions))
	if err := service.components.roleApplication.SetRoleSSHUserSpaces("sess-1", "agent", []string{"/srv/default"}); err != nil {
		t.Fatal(err)
	}
	if err := service.components.roleApplication.SetRoleSessionSSHUserSpaces("sess-1", "agent", []string{"/srv/session"}); err != nil {
		t.Fatal(err)
	}

	paths, err := service.components.roleApplication.RoleSSHUserSpaces("sess-1", "teammate")
	if err != nil || !reflect.DeepEqual(paths, []string{"/srv/session"}) {
		t.Fatalf("teammate 应继承 mainagent 的会话覆盖: paths=%v err=%v", paths, err)
	}
	paths, err = service.components.roleApplication.RoleSSHUserSpaces("sess-2", "agent")
	if err != nil || !reflect.DeepEqual(paths, []string{"/srv/default"}) {
		t.Fatalf("其它会话应回落角色默认: paths=%v err=%v", paths, err)
	}
}

func TestSetRolePermissionVectorRejectsMalformedEntries(t *testing.T) {
	sessions := newRolePermissionFakeSessions()
	service := newLegacyTestService(t, &fakeEngine{}, withTestSessions(sessions))
	if err := service.components.roleApplication.SetRolePermissionVector("sess-1", "tl", []string{"bad tag"}); err == nil {
		t.Fatal("非法项必须显式报错（静默丢项 = 用户以为给了能力）")
	}
	if vectors, _ := sessions.RolePermissionVectors("sess-1"); len(vectors) != 0 {
		t.Fatalf("报错时不得落任何登记，得到 %+v", vectors)
	}
}

// TestRestoreRolePermissionsConcurrentRoles 让多个角色同时恢复：判据无共享可变
// 状态（每个角色现算现返回），所以既不该丢角色，也不该有跨角色的写入竞争。
func TestRestoreRolePermissionsConcurrentRoles(t *testing.T) {
	sessions := newRolePermissionFakeSessions()
	sessions.layers = fourLayerFixture()
	service := newLegacyTestService(t, &fakeEngine{}, withTestSessions(sessions))

	roles := []string{"r1", "r2", "r3", "r4", "r5", "r6", "r7", "r8"}
	for _, role := range roles {
		if err := service.components.roleApplication.SetRolePermissionVector("sess-1", role, []string{dto.PermissionTagRO, role}); err != nil {
			t.Fatalf("Set(%s): %v", role, err)
		}
	}
	var wait sync.WaitGroup
	for _, role := range roles {
		role := role
		wait.Add(1)
		go func() {
			defer wait.Done()
			vector, err := service.components.roleApplication.RolePermissionVector("sess-1", role)
			if err != nil {
				t.Errorf("RolePermissionVector(%s): %v", role, err)
				return
			}
			if len(vector) != 2 || vector[1] != role {
				t.Errorf("角色 %s 向量 = %v，被别的角色串了", role, vector)
			}
		}()
	}
	wait.Wait()

	restored, err := service.components.roleApplication.RestoreRolePermissions("sess-1", roles)
	if err != nil {
		t.Fatalf("RestoreRolePermissions: %v", err)
	}
	if len(restored) != len(roles) {
		t.Fatalf("恢复角色 %d 个, want %d", len(restored), len(roles))
	}
}

func TestRoleAccountApplicationSetAccountRollsBackRuntime(t *testing.T) {
	storeErr := errors.New("store failed")
	var calls []string
	port := &roleAccountApplicationFakeStore{
		roleAccount: func(string, string) (string, bool, error) {
			return "old-account", true, nil
		},
		setRoleAccount: func(string, string, string) error {
			calls = append(calls, "store")
			return storeErr
		},
	}
	runtimePort := &roleAccountApplicationFakeRuntime{
		setRoleAccount: func(role, account string) error {
			calls = append(calls, role+":"+account)
			return nil
		},
	}
	application := applicationpolicy.NewRoleApplication(applicationpolicy.Deps{
		RoleDefaults: func() applicationpolicy.RoleDefaultsPort { return port },
		Runtime:      func() contract.AccountRoleSelectionPort { return runtimePort },
	})

	err := application.SetRoleAccount(" sess-1 ", " teammate ", " teammate-2 ")
	if !errors.Is(err, storeErr) {
		t.Fatalf("SetRoleAccount error = %v", err)
	}
	want := "teammate:teammate-2,store,teammate:"
	if strings.Join(calls, ",") != want {
		t.Fatalf("calls = %v, want %s", calls, want)
	}
}

func TestRoleAccountApplicationRestoreSkipsExplicitlyMissingAccount(t *testing.T) {
	port := &roleAccountApplicationFakeStore{
		roleAccount: func(_ string, role string) (string, bool, error) {
			if role == string(dto.RoleTeammate) {
				return "teammate-2", true, nil
			}
			return "", false, nil
		},
	}
	var calls []string
	runtimePort := &roleAccountApplicationFakeRuntime{
		setRoleAccount: func(role, account string) error {
			calls = append(calls, role+":"+account)
			return nil
		},
	}
	application := applicationpolicy.NewRoleApplication(applicationpolicy.Deps{
		RoleDefaults: func() applicationpolicy.RoleDefaultsPort { return port },
		Runtime:      func() contract.AccountRoleSelectionPort { return runtimePort },
	})

	if err := application.RestoreRoleAccounts("sess-1"); err != nil {
		t.Fatal(err)
	}
	if want := string(dto.RoleTeammate) + ":teammate-2"; strings.Join(calls, ",") != want {
		t.Fatalf("calls = %v, want %s", calls, want)
	}
}

type roleAccountApplicationFakeStore struct {
	applicationpolicy.RoleDefaultsPort
	roleAccount    func(string, string) (string, bool, error)
	setRoleAccount func(string, string, string) error
}

func (store *roleAccountApplicationFakeStore) RoleAccount(sessionID, role string) (string, bool, error) {
	return store.roleAccount(sessionID, role)
}

func (store *roleAccountApplicationFakeStore) SetRoleAccount(sessionID, role, account string) error {
	return store.setRoleAccount(sessionID, role, account)
}

type roleAccountApplicationFakeRuntime struct{ setRoleAccount func(string, string) error }

func (runtime *roleAccountApplicationFakeRuntime) SetRoleAccount(role, account string) error {
	return runtime.setRoleAccount(role, account)
}
