package adapters

// role_permission_port_test.go 钉住装配侧「角色设置」这条链的两件事：
//
//  1. 登记（事实源）住在 store：按 (项目, 会话, 角色) 落键，经**同一个**存储实例
//     读写（同一数据根必须共用实例，否则两个实例各持一份缓存，写后读会读到旧副本）；
//     且"显式空集"与"从未登记"分得开。
//  2. 五层来源的组装在**封闭世界**下自洽：某一层说了"这一组 0 位"，就必须投影成
//     `!tag`，不能让沉默把低层（尤其 global_default 的 root 全位）读成放行。
//     解析判据仍是 contract/dto 的唯一实现，本测试只喂层、只看结果。

import (
	"os"
	"path/filepath"
	"testing"

	assemble "github.com/RedHuang-0622/seelex/backend/engine/assemble"
	"github.com/RedHuang-0622/seelex/backend/limit/policy"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/store"
)

// newRolePermissionTestPort 建一个会话端口（真实 Router；无 Runtime 时五层里
// 派发/会话两层自然为空层）。
func newRolePermissionTestPort(t *testing.T, router *store.Router) SessionPort {
	t.Helper()
	port := SessionPort{Manager: NewSessionManager().WithRouter(router)}
	port.SetWorkspaceResolver(func(string) string { return "project-role-perm" })
	return port
}

func newRolePermissionTestRouter(t *testing.T) *store.Router {
	t.Helper()
	router, err := store.NewRouter(t.TempDir())
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router
}

// TestRolePermissionPortStoreRoundTrip：写入 → 换一个端口值读回；空集与"没登记"分开；
// 同一 Router 的两次构造共用同一个登记存储（进程内缓存必须是同一份）。
func TestRolePermissionPortStoreRoundTrip(t *testing.T) {
	router := newRolePermissionTestRouter(t)
	const sessionID = "sess-role-perm"

	writer := newRolePermissionTestPort(t, router)
	if err := writer.SetRolePermissionVector(sessionID, "review", []string{dto.PermissionTagRO}); err != nil {
		t.Fatalf("SetRolePermissionVector: %v", err)
	}
	// 显式空集：空切片必须能写进去，并在读面报告"登记过"。
	if err := writer.SetRolePermissionVector(sessionID, "coworker", []string{}); err != nil {
		t.Fatalf("SetRolePermissionVector(empty): %v", err)
	}

	reader := newRolePermissionTestPort(t, router)
	vectors, err := reader.RolePermissionVectors(sessionID)
	if err != nil {
		t.Fatalf("RolePermissionVectors: %v", err)
	}
	if got := vectors["review"]; len(got) != 1 || got[0] != dto.PermissionTagRO {
		t.Fatalf("review 的登记 = %v, want [%s]", got, dto.PermissionTagRO)
	}
	if got, stored, err := reader.RolePermissionVector(sessionID, "coworker"); err != nil || !stored || len(got) != 0 {
		t.Fatalf("coworker = %v stored=%v err=%v, want 显式空集且 stored=true", got, stored, err)
	}
	if _, stored, err := reader.RolePermissionVector(sessionID, "never-set"); err != nil || stored {
		t.Fatalf("从未登记的角色 stored=%v err=%v, want false", stored, err)
	}
}

func TestRoleDefaultOccupiesItsOwnLayer(t *testing.T) {
	router := newRolePermissionTestRouter(t)
	const sessionID = "sess-role-default-layer"
	port := newRolePermissionTestPort(t, router)
	if err := port.SetRoleDefault(sessionID, "review", []string{dto.PermissionTagRO}); err != nil {
		t.Fatalf("SetRoleDefault: %v", err)
	}
	layers := port.RolePermissionLayers(sessionID, "review")
	if got := layers.RoleDefault.PermissionVector; len(got) != 1 || got[0] != dto.PermissionTagRO {
		t.Fatalf("role_default 层 = %v, want [ro]", got)
	}
	if len(layers.GlobalDefault.PermissionVector) != 0 {
		t.Fatalf("角色默认不得再写进 global_default：%v", layers.GlobalDefault.PermissionVector)
	}
}

// TestRolePermissionPortWritesLandOnlyInOwnProject：登记按会话归属项目落键，
// 不跟着"用户现在在看哪个项目"漂移。
func TestRolePermissionPortWritesLandOnlyInOwnProject(t *testing.T) {
	router := newRolePermissionTestRouter(t)
	const sessionID = "sess-role-perm-scope"

	owner := SessionPort{Manager: NewSessionManager().WithRouter(router)}
	owner.SetWorkspaceResolver(func(string) string { return "project-owner" })
	if err := owner.SetRolePermissionVector(sessionID, "tl", []string{dto.PermissionTagRW}); err != nil {
		t.Fatalf("SetRolePermissionVector: %v", err)
	}

	other := SessionPort{Manager: NewSessionManager().WithRouter(router)}
	other.SetWorkspaceResolver(func(string) string { return "project-other" })
	if _, stored, err := other.RolePermissionVector(sessionID, "tl"); err != nil || stored {
		t.Fatalf("另一个项目读到 stored=%v err=%v, want 未登记（不得跨项目串）", stored, err)
	}
}

// TestRolePermissionLayersProjectEachGroupClosedWorld 是五层来源的核心断言：
// 位为 0 的组必须投影成 `!tag`，否则五层解析会把"这一层明确不给"读成"没提"，
// 让 global_default（root 全位）反手放行。
func TestRolePermissionLayersProjectEachGroupClosedWorld(t *testing.T) {
	accounts := filepath.Join(t.TempDir(), "accounts.yaml")
	if err := os.WriteFile(accounts, []byte("roles:\n  agent:\n    - model: test-model\n      base_url: http://localhost\n      api_key: test-key-not-used\n"), 0o644); err != nil {
		t.Fatalf("write accounts: %v", err)
	}
	runtime, err := assemble.NewRuntime(assemble.RuntimeConfig{AccountsPath: accounts})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	t.Cleanup(runtime.Shutdown)
	runtime.SetPermissionConfig(policy.PermissionConfigForShell(policy.ShellBash), nil)
	if err := runtime.AssignEmployeePermissions([]dto.RoleSpec{
		{RoleName: "review", ToolsPolicy: "readonly"},
		{RoleName: "exec", ToolsPolicy: "readwrite", PermissionGroups: map[string]uint8{policy.GroupRO: policy.BitRead}},
	}); err != nil {
		t.Fatalf("AssignEmployeePermissions: %v", err)
	}

	port := SessionPort{Manager: NewSessionManager().WithRouter(newRolePermissionTestRouter(t)), Runtime: runtime}

	// readonly 员工：派发层没显式格子 → 空层；会话层按档位派生，只读 + ssh 通道位。
	review := port.RolePermissionLayers("sess-role-layers", "review")
	if len(review.AgentDispatch.PermissionVector) != 0 {
		t.Fatalf("review 的派发层 = %v, want 空层（没显式格子）", review.AgentDispatch.PermissionVector)
	}
	// ssh 出现在这一层是**刻意的**（2026-10-2x ssh 工作项）：通道位表达"能不能把
	// 调用打到远端"，与操作类型（ro/rw）正交，派生口径里默认给（远端面在本工作项
	// 之前就一直可达）。越界位 ssh.userspace 不在这一层——那是要立的那道闸门。
	if got := dto.ResolveRoleSettings(review.Ordered()...).PermissionVector; len(got) != 2 ||
		got[0] != dto.PermissionTagRO || got[1] != dto.PermissionGroupSSH {
		t.Fatalf("review 生效向量 = %v, want [%s %s]（读写/控制/属主/越界都被会话层显式否掉）",
			got, dto.PermissionTagRO, dto.PermissionGroupSSH)
	}

	// 显式格子的员工：派发层非空即封闭，只开 ro；档位派生那层不在（无可用档位）。
	exec := port.RolePermissionLayers("sess-role-layers", "exec")
	if got := dto.ResolveRoleSettings(exec.Ordered()...).PermissionVector; len(got) != 1 || got[0] != dto.PermissionTagRO {
		t.Fatalf("exec 生效向量 = %v, want [%s]（显式格子只开 ro；global 的 root 全位不得漏上来）", got, dto.PermissionTagRO)
	}

	// 没分配的员工：**global_default 层故意留空**（2026-10-21 收紧），生效向量因此为空。
	//
	// 为什么不是"回落到 root 全组"：执行面按"这几层叠出来的向量"判授权，若身份不明时
	// 补一份 root 的全组位，就等于"谁都不知道你是谁 ⇒ 给你主代理的全部能力"——典型
	// fail-open。留空后由执行面按 ctx 身份（显式格子 → 档位）派生，方向上只会更窄。
	unknown := port.RolePermissionLayers("sess-role-layers", "unassigned")
	got := dto.ResolveRoleSettings(unknown.Ordered()...).PermissionVector
	if len(got) != 0 {
		t.Fatalf("未分配角色的生效向量 = %v, want 空（global_default 不兜底 root 全位：必须 fail-closed）", got)
	}
	if len(unknown.GlobalDefault.PermissionVector) != 0 {
		t.Fatalf("未分配角色的 global_default 层 = %v, want 空层（不表态，不是允许）", unknown.GlobalDefault.PermissionVector)
	}
}

// TestPermissionVectorTagsMarksZeroBitsAsDeny 钉住投影规则本身（一条判据一处实现）。
func TestPermissionVectorTagsMarksZeroBitsAsDeny(t *testing.T) {
	tags := permissionVectorTags(map[string]uint8{
		policy.GroupRO: policy.BitRead,
		policy.GroupRW: 0, // 显式零位 = 明确不给
	})
	allowed := map[string]bool{}
	for _, tag := range tags {
		if len(tag) > 0 && tag[0] == '!' {
			continue
		}
		allowed[tag] = true
	}
	if !allowed[dto.PermissionTagRO] {
		t.Fatalf("ro 未出现在允许面：%v", tags)
	}
	if allowed[dto.PermissionTagRW] {
		t.Fatalf("rw 位为 0 却出现在允许面：%v", tags)
	}
	// 规范组序：标签数量固定 = 组数（每格要么给、要么显式不给）。
	if len(tags) != len(dto.PermissionGroupNames()) {
		t.Fatalf("标签数 = %d, want %d（每组都要表态）：%v", len(tags), len(dto.PermissionGroupNames()), tags)
	}
}
