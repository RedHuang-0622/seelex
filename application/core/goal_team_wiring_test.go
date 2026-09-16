package core

import (
	"context"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

// teamRecordingSessions 在默认 fakeSessions 上补出 A2A 团队存储面，
// 记录 goal 创建时自动装配团队的调用（顺序策略与注册表）。
type teamRecordingSessions struct {
	fakeSessions
	mu       sync.Mutex
	ensured  []string
	policy   string
	order    []string
	registry dto.TeamRegistry
}

func (s *teamRecordingSessions) EnsureRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensured = append(s.ensured, roleName)
	return true, nil
}

func (s *teamRecordingSessions) ReadLifecycleOrder(string) (string, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.policy, append([]string(nil), s.order...), nil
}

func (s *teamRecordingSessions) SetLifecycleOrder(_ string, policy string, roles []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policy = policy
	s.order = append([]string(nil), roles...)
	return nil
}

func (s *teamRecordingSessions) ReadTeamRegistry(string) (dto.TeamRegistry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	registry := s.registry
	registry.Roles = append([]dto.RoleSpec(nil), s.registry.Roles...)
	return registry, nil
}

func (s *teamRecordingSessions) WriteTeamRegistry(_ string, registry dto.TeamRegistry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registry = registry
	return nil
}

// 以下方法是 contract.RoleSessionPort 的其余成员：本用例只走
// SetLifecycleOrder（顺序策略），其余保持最小实现以满足接口断言。

func (s *teamRecordingSessions) CreateRoleSession(string, string, string, uint64) (dto.RoleSessionInfo, error) {
	return dto.RoleSessionInfo{}, nil
}

func (s *teamRecordingSessions) AppendRoleDraft(string, string, string, []dto.RoleDraftRow) error {
	return nil
}

func (s *teamRecordingSessions) ReadRoleDraft(string, string, string) ([]dto.RoleDraftRow, error) {
	return nil, nil
}

func (s *teamRecordingSessions) SyncRoleDraft(string, string, string, []string) (dto.RoleDraftSyncResult, error) {
	return dto.RoleDraftSyncResult{}, nil
}

func (s *teamRecordingSessions) AppendRoleSessionRows(string, string, string, []dto.RoleRow) error {
	return nil
}

func (s *teamRecordingSessions) ReadRoleSessionRows(string, string, string) ([]dto.RoleRow, error) {
	return nil, nil
}

func (s *teamRecordingSessions) RoleSnapshot(string, string, string) (dto.RoleSnapshot, error) {
	return dto.RoleSnapshot{}, nil
}

func (s *teamRecordingSessions) AssembleRoleWire(string, string, string, int, int) (dto.RoleWireSnapshot, error) {
	return dto.RoleWireSnapshot{}, nil
}

func (s *teamRecordingSessions) SetRoleLifecycle(string, string, string, uint64, *dto.CompactFrameRef) error {
	return nil
}

// 以下是夹具的**加锁存取入口**：夹具状态只允许通过它们读写。
//
// 为什么这不是"多此一举的封装"（夹具竞态的正解）：夹具的**读侧**是加锁的
// （生产路径：服务/后台 goroutine 通过 SessionPort 读顺序与注册表），若测试侧
// 直接 `sessions.order = nil`，锁就只保护了一侧。按 Go 内存模型，读与写之间
// 没有 happens-before 关系 —— 这仍然是数据竞争，`-race` 会直接报 DATA RACE
// （复现见 fixture_concurrency_test.go 的说明）。两侧都走同一把锁才是解。
func (s *teamRecordingSessions) setLifecycle(policy string, order []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policy = policy
	s.order = append([]string(nil), order...)
}

// setOrder 只覆盖顺序（顺序策略不动）：装配前的"清现场"用它。
func (s *teamRecordingSessions) setOrder(order []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.order = append([]string(nil), order...)
}

// setRegistry 覆盖夹具的团队注册表（加锁）。
func (s *teamRecordingSessions) setRegistry(registry dto.TeamRegistry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registry = registry
}

// lifecycleSnapshot 读当前顺序策略与顺序（加锁 + 深拷贝，读到的是快照）。
func (s *teamRecordingSessions) lifecycleSnapshot() (string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.policy, append([]string(nil), s.order...)
}

// registrySnapshot 读当前注册表（加锁 + 拷贝角色切片）。
func (s *teamRecordingSessions) registrySnapshot() dto.TeamRegistry {
	s.mu.Lock()
	defer s.mu.Unlock()
	registry := s.registry
	registry.Roles = append([]dto.RoleSpec(nil), s.registry.Roles...)
	return registry
}

// orderSnapshot 只读顺序（加锁 + 拷贝）。
func (s *teamRecordingSessions) orderSnapshot() []string {
	_, order := s.lifecycleSnapshot()
	return order
}

// ensuredRoles 读已装配的角色会话名（加锁 + 拷贝）。
func (s *teamRecordingSessions) ensuredRoles() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ensured...)
}

func (s *teamRecordingSessions) ListRoleSessions(string) ([]string, error) {
	return nil, nil
}

// TestGoalBeginMaterializesGoalAgentTeam 钉住 goal → AgentTeam 接线：创建 goal
// 时自动装配 goal-a2a（TL 的 JoinPolicy=on_goal_create），顺序策略落 goal_loop，
// 且 TL 是真实角色会话——不再需要前端手动点一次「装配团队」。
func TestGoalBeginMaterializesGoalAgentTeam(t *testing.T) {
	sessions := &teamRecordingSessions{}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))

	if _, err := service.GoalBeginFor(context.Background(), "sess-goal", goaldomain.BeginRequest{Title: "接线验证"}); err != nil {
		t.Fatalf("GoalBeginFor: %v", err)
	}

	tlFound := false
	for _, name := range sessions.ensuredRoles() {
		if name == "tl" {
			tlFound = true
		}
	}
	if !tlFound {
		t.Fatalf("goal 创建未装配 TL 角色会话，ensured=%v", sessions.ensuredRoles())
	}
	policy, order := sessions.lifecycleSnapshot()
	if policy != dto.OrderPolicyGoalLoop {
		t.Fatalf("lifecycle order policy = %q, want %q", policy, dto.OrderPolicyGoalLoop)
	}
	if len(order) == 0 {
		t.Fatalf("lifecycle order roles 未写入")
	}
	if registry := sessions.registrySnapshot(); registry.TeamKind != dto.TeamKindGoalA2A {
		t.Fatalf("registry team_kind = %q, want %q", registry.TeamKind, dto.TeamKindGoalA2A)
	}
}

// TestGoalBeginWithoutTeamStorageIsBestEffort 钉住降级语义：宿主没有团队存储
// （旧宿主/纯治理桩）时，goal 创建仍必须成功，只是不装配团队。
func TestGoalBeginWithoutTeamStorageIsBestEffort(t *testing.T) {
	service := newTestService(t, &fakeEngine{})

	record, err := service.GoalBeginFor(context.Background(), "sess-plain", goaldomain.BeginRequest{Title: "无团队存储"})
	if err != nil {
		t.Fatalf("GoalBeginFor 在无团队存储时必须成功（best-effort），实际: %v", err)
	}
	if record == nil || record.Title != "无团队存储" {
		t.Fatalf("goal record = %+v", record)
	}
}
