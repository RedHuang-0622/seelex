package integration

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	assembly "github.com/RedHuang-0622/seelex/application/assembly"
	"github.com/RedHuang-0622/seelex/contract/dto"
	goaldomain "github.com/RedHuang-0622/seelex/kernel/goal"
)

// teamRecordingSessions 在默认 fakeSessions 上补出 A2A 团队存储面（顺序策略 + 注册表），
// 用来钉住"goal 创建**不**碰会话团队"这条口径。
//
// 状态（A06，2026-10-15）：这一面（`dto.TeamRegistry` / `ReadLifecycleOrder` /
// `WriteTeamRegistry` / `RemoveTeamRegistry`）现在只有 **legacy 只读兼容**地位——
// 装配方（`AgentTeamFactory`）已随 J-1a 退场，产品不再自动装配或改写团队。夹具保留的
// 理由是回归点仍然成立：goal 上线不得改写会话里已有的顺序/注册表（哪怕是旧格式）。
type teamRecordingSessions struct {
	fakeSessions
	mu       sync.Mutex
	ensured  []string
	policy   string
	order    []string
	registry dto.TeamRegistry
	// joinSeqs 记录角色会话装配时的 join_seq_id（teammate 记录的起点），
	// mainHeadSeq 是 RoleSnapshot 桩返回的主会话尾 seq。
	joinSeqs    []string
	mainHeadSeq uint64
	// dismissed 记录"团队离场"次数（干完就走人）。
	dismissed int
}

func (s *teamRecordingSessions) EnsureRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensured = append(s.ensured, roleName)
	s.joinSeqs = append(s.joinSeqs, fmt.Sprintf("%s|%d", roleName, joinSeq))
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

// RemoveTeamRegistry 是 agentteam.DismissPort 的桩：团队离场 = 注册表清空 + 顺序复位
// （与 store 的"删文件 = 读面 Configured=false"同一语义）。
func (s *teamRecordingSessions) RemoveTeamRegistry(string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registry = dto.TeamRegistry{}
	s.policy = ""
	s.order = nil
	s.dismissed++
	return nil
}

// dismissCount 返回团队离场次数。
func (s *teamRecordingSessions) dismissCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dismissed
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return dto.RoleSnapshot{MainHeadSeq: s.mainHeadSeq}, nil
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

// joinSeqSnapshot 读角色会话装配时的 join 切点（加锁 + 拷贝）：
// "role|joinSeq" 形态，召唤/自动装配的入伙切点断言用它。
func (s *teamRecordingSessions) joinSeqSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.joinSeqs...)
}

func (s *teamRecordingSessions) ListRoleSessions(string) ([]string, error) {
	return nil, nil
}

// TestGoalBeginLeavesSessionTeamAlone 钉住 2026-10-01 的口径：创建 goal **不**自动装配
// 团队，也不覆盖会话里已有的团队。
//
// 修前的行为是事故：`GoalBeginFor → ensureGoalAgentTeam` 按内置形态 goal-a2a 装配，而
// 装配 = 注册表 + lifecycle 顺序的**整份替换**（factory.Materialize → WriteTeamRegistry /
// SetLifecycleOrder），于是"开始一个 goal"会把用户手工加的员工（worker/reviewer）一起
// 冲成模板那三个人。删掉自动装配后，团队由用户/leader 决定（leader-worker 目标态里由
// team plan 决定）；goal 的 ADVISOR 裁决来自 goal 域自己的 Supervisor（终态 gate /
// 审批预筛里的真实 TL 回合），不依赖团队装配。
func TestGoalBeginLeavesSessionTeamAlone(t *testing.T) {
	sessions := &teamRecordingSessions{}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))

	// 会话里先有一支"用户自己的"团队：tl + worker（后者是用户加的，不在任何模板里）。
	sessions.setRegistry(dto.TeamRegistry{
		TeamID: "my-team", TeamKind: "my-team", OrderPolicy: dto.OrderPolicyGoalLoop, Configured: true,
		Roles: []dto.RoleSpec{
			{RoleName: "tl", RoleKind: dto.RoleKindTechlead, ToolsPolicy: dto.ToolPolicyReadonly},
			{RoleName: "worker", RoleKind: dto.RoleKindAgent},
		},
	})
	sessions.setLifecycle(dto.OrderPolicyGoalLoop, []string{"user", "main", "tl", "worker"})

	if _, err := assembly.ComponentsFor(service).Admin.Goal.BeginFor(context.Background(), "sess-goal", goaldomain.BeginRequest{Title: "接线验证"}); err != nil {
		t.Fatalf("GoalBeginFor: %v", err)
	}

	registry := sessions.registrySnapshot()
	if registry.TeamID != "my-team" || len(registry.Roles) != 2 {
		t.Fatalf("goal 上线不得改写会话团队：%+v", registry)
	}
	policy, order := sessions.lifecycleSnapshot()
	if policy != dto.OrderPolicyGoalLoop || strings.Join(order, ",") != "user,main,tl,worker" {
		t.Fatalf("goal 上线不得改写工作顺序：policy=%q order=%v", policy, order)
	}
	if ensured := sessions.ensuredRoles(); len(ensured) != 0 {
		t.Fatalf("goal 上线不该新建任何角色会话：%v", ensured)
	}
}

// TestGoalBeginWithoutTeamStorageIsBestEffort 钉住降级语义：宿主没有团队存储
// （旧宿主/纯治理桩）时，goal 创建仍必须成功，只是不装配团队。
func TestGoalBeginWithoutTeamStorageIsBestEffort(t *testing.T) {
	service := newTestService(t, &fakeEngine{})

	record, err := assembly.ComponentsFor(service).Admin.Goal.BeginFor(context.Background(), "sess-plain", goaldomain.BeginRequest{Title: "无团队存储"})
	if err != nil {
		t.Fatalf("GoalBeginFor 在无团队存储时必须成功（best-effort），实际: %v", err)
	}
	if record == nil || record.Title != "无团队存储" {
		t.Fatalf("goal record = %+v", record)
	}
}

// joinSeqsSnapshot 只读角色装配的 join_seq_id 记录（加锁 + 拷贝）。
func (s *teamRecordingSessions) joinSeqsSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.joinSeqs...)
}

// TestMaterializeJoinsTeammatesAtItsTurn 已随会话团队装配面退场（2026-10-12 J-1a）
// 删除：它钉的是"装配时把 teammate 挂在那一回合上"（`MaterializeAgentTeam` 的
// join_seq_id = 主会话 message 尾 seq，经 `teamJoinSeqFor` 取）。两个被调用的入口
// （`MaterializeAgentTeam` / `teamJoinSeqFor`）都随本条目删除，因此这条断言的**被测
// 对象**不再存在。
//
// "teammate 的记录从入伙那一回合算起"这条产品口径**没有被丢掉**，它现在归 teamwork：
// 一 Work Item = 一 Session + 一 worktree，作业的入伙切点由 `backend/multiagent/teamwork`
// 的派活面带着走（那条面本条目未动）。

// TestNoteTeamWorkPrefixReadsMainSessionContext 已随 A03（2026-10-15）删除：它钉的是
// "没有环就没有前缀消费者"，而"环 + 前缀"整条接线（teamRuntimeStore /
// teamRuntimeBySession / noteTeamWorkPrefix / TeamRuntimeFor）已随 J-1a 之后的 A03
// 清理一并移除，被测对象不再存在。
//
// 2026-10-15（A04）起连前缀的**生产者**也不在了：`agentteam.Runtime.NoteMainContext`
// 与 `prefix_test.go` 随发言环（`runtime.go` / `scheduler.go`）整条删除，因此本仓
// 不再有任何 team work 前缀实现，"没有前缀消费者"从此是结构事实而不是运行态事实。
