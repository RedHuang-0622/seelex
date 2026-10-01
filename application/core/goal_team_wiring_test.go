package core

import (
	"context"
	"fmt"
	"strings"
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
	// wire 是前缀用例的桩：AssembleRoleWire 的返回值；wireAsks 记录装配请求
	// （role_name 必须是 main，budget/k 必须与前端同口径）。
	wire     dto.RoleWireSnapshot
	wireAsks []string
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
// （与 sessionstore 的"删文件 = 读面 Configured=false"同一语义）。
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

func (s *teamRecordingSessions) AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (dto.RoleWireSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wireAsks = append(s.wireAsks, fmt.Sprintf("%s|%s|%s|%d|%d", mainSessionID, roleName, roleSessionID, budget, k))
	return s.wire, nil
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
// team plan 决定）；goal 的 ADVISOR 裁决来自治理循环自带的 supervisor 座位
// （goaldomain.NewTurnGovernorForDSA2A），不依赖团队装配。
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

	if _, err := service.GoalBeginFor(context.Background(), "sess-goal", goaldomain.BeginRequest{Title: "接线验证"}); err != nil {
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

	record, err := service.GoalBeginFor(context.Background(), "sess-plain", goaldomain.BeginRequest{Title: "无团队存储"})
	if err != nil {
		t.Fatalf("GoalBeginFor 在无团队存储时必须成功（best-effort），实际: %v", err)
	}
	if record == nil || record.Title != "无团队存储" {
		t.Fatalf("goal record = %+v", record)
	}
}

// wireAsksSnapshot 只读前缀用例的装配请求记录（加锁 + 拷贝）。
func (s *teamRecordingSessions) wireAsksSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.wireAsks...)
}

// joinSeqsSnapshot 只读角色装配的 join_seq_id 记录（加锁 + 拷贝）。
func (s *teamRecordingSessions) joinSeqsSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.joinSeqs...)
}

// TestMaterializeJoinsTeammatesAtItsTurn 钉住 teammate 记录（它自己那份 team work
// 会话）的起点：装配时写入的 join_seq_id = 那一刻主会话已提交的尾 seq，于是 teammate
// 的记录从"装配它的那一回合"算起——它入伙之前的对话不在它的前缀匹配区间里。
//
// 修前这条断言的触发者是"goal 创建自动装配"，那条自动路径已删除（它会整份替换掉会话
// 已有的团队，见 goal_service.GoalBeginFor）；断言改为打在装配入口上。
func TestMaterializeJoinsTeammatesAtItsTurn(t *testing.T) {
	sessions := &teamRecordingSessions{mainHeadSeq: 5}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))

	if _, err := service.MaterializeAgentTeam("sess-join", goalTeamFixture(), service.teamJoinSeqFor("sess-join")); err != nil {
		t.Fatalf("MaterializeAgentTeam: %v", err)
	}

	joined := sessions.joinSeqsSnapshot()
	if len(joined) == 0 {
		t.Fatal("装配没有建立任何角色会话")
	}
	tlFound := false
	for _, entry := range joined {
		if entry == "tl|5" {
			tlFound = true
		}
		if strings.HasSuffix(entry, "|0") {
			t.Fatalf("角色被挂在主会话最开头（join_seq_id=0）：%v", joined)
		}
	}
	if !tlFound {
		t.Fatalf("TL 未按装配回合入伙（want tl|5）：%v", joined)
	}
}

// TestNoteTeamWorkPrefixReadsMainSessionContext 钉住前缀的作者与读取口径：
//   - 作者是主会话上下文（含主会话 draft）的只读装配：应用层向存储请求的必须是
//     role_name=main（main 复用主会话本身），budget/k 与前端 role wire 探针同口径；
//   - 前缀内容就是那条 wire 的逐行投影 + 锚点，不是治理域的回合摘要；
//   - 两条"宁缺勿造"：该会话没有环时不建环、也不读存储；读失败不动前缀。
func TestNoteTeamWorkPrefixReadsMainSessionContext(t *testing.T) {
	sessions := &teamRecordingSessions{wire: dto.RoleWireSnapshot{
		MainSessionID: "sess-prefix", RoleName: RoleNameMain, AppliedSeq: 9, TailStartSeq: 1,
		PrefixDigest: "digest-main", Messages: []dto.RoleWireMessage{
			{Role: "user", Content: "把前缀改成主会话上下文", Seq: 1},
			{Role: "assistant", Content: "改了 agentteam_runtime.go", Seq: 2},
		},
	}}
	service := newTestService(t, &fakeEngine{}, withTestSessions(sessions))

	// 没有环 = 没有前缀消费者：不建环、不读存储。
	service.noteTeamWorkPrefix("sess-prefix")
	if asks := sessions.wireAsksSnapshot(); len(asks) != 0 {
		t.Fatalf("没有环时不该读 wire：%v", asks)
	}

	service.teamRuntimeBySession("sess-prefix")
	service.noteTeamWorkPrefix("sess-prefix")

	schedule := service.teamScheduleFor("sess-prefix")
	if schedule == nil {
		t.Fatal("环应在装配后存在")
	}
	for _, want := range []string{"user: 把前缀改成主会话上下文", "assistant: 改了 agentteam_runtime.go", "起点 → 当前位置"} {
		if !strings.Contains(schedule.Prefix, want) {
			t.Fatalf("前缀缺少 %q：%q", want, schedule.Prefix)
		}
	}
	if schedule.PrefixDigest != "digest-main" || schedule.PrefixTailSeq != 1 || schedule.PrefixAppliedSeq != 9 {
		t.Fatalf("口径锚点没随快照暴露：digest=%q tail=%d applied=%d",
			schedule.PrefixDigest, schedule.PrefixTailSeq, schedule.PrefixAppliedSeq)
	}
	wantAsk := fmt.Sprintf("sess-prefix|%s|sess-prefix|%d|%d", RoleNameMain, teamPrefixWireBudget, teamPrefixWireK)
	if asks := sessions.wireAsksSnapshot(); len(asks) != 1 || asks[0] != wantAsk {
		t.Fatalf("wire 装配请求错位：%v，want [%s]", asks, wantAsk)
	}
}
