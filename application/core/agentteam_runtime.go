// agentteam_runtime.go 把 AgentTeam 的发言调度运行态接到 Application 能力面。
//
// 边界：agentteam 包负责「环里怎么转」（链表顺序、环成员、逃生记账）；
// 本文件只负责**会话级持有与同步**：谁（哪个主会话）有一个环、环的顺序什么时候
// 跟着注册表走、运行态怎么投影进 dto.TeamView。不解释顺序语义（那份事实只有
// lifecycle.order_policy/order_roles 一份）。
package core

import (
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/agentteam"
)

// teamRuntimeStore 按主会话持有发言调度运行态。自带锁：环的推进（Next/记账）
// 与视图刷新互不阻塞，也不参与 Core.ViewMu 的快照事务。
type teamRuntimeStore struct {
	mu       sync.Mutex
	runtimes map[string]*agentteam.Runtime
}

func (store *teamRuntimeStore) get(mainSessionID string) *agentteam.Runtime {
	if store == nil {
		return nil
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.runtimes[mainSessionID]
}

func (store *teamRuntimeStore) put(mainSessionID string, runtime *agentteam.Runtime) {
	if store == nil {
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.runtimes == nil {
		store.runtimes = make(map[string]*agentteam.Runtime)
	}
	store.runtimes[mainSessionID] = runtime
}

// drop 释放某个主会话的运行时槽（会话删除/归档时经 releaseTeamRuntime 调用；
// 环是派生状态，重建即正确，留着只会把上一段生命的逃生结论一起带过来）。
func (store *teamRuntimeStore) drop(mainSessionID string) {
	if store == nil {
		return
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.runtimes, mainSessionID)
}

// roleSessionsOf 从成员表取 role_name → role_session_id（环里角色发言时的会话
// 坐标；user/main 复用主会话、无独立角色会话，因此空值合法）。
func roleSessionsOf(view dto.TeamView) map[string]string {
	sessions := make(map[string]string, len(view.Members))
	for _, member := range view.Members {
		if member.RoleSessionID != "" {
			sessions[member.RoleName] = member.RoleSessionID
		}
	}
	return sessions
}

// defaultTeamRoundLimit 是团队发言环的轮次上限（逃生路径第一道）。席位轮转退场
// 后它**不再**与 goal 治理循环同源——goal 没有"轮次"了；环的逃生记账是它自己的
// 兜底（到达上限即收束，见 agentteam.Runtime.NoteTurn）。
const defaultTeamRoundLimit = 24

// teamRuntimeFor 返回（需要时创建）指定主会话的发言调度运行态，并把注册表
// 顺序同步进环。读路径不写盘：顺序事实仍然只有 lifecycle 一份。
func (service *Service) teamRuntimeFor(mainSessionID string, view dto.TeamView) *agentteam.Runtime {
	runtime := service.teamRuntimes.get(mainSessionID)
	if runtime == nil {
		runtime = agentteam.NewRuntime(view.OrderRoles, roleSessionsOf(view), view.OrderPolicy, agentteam.RuntimeOptions{
			RoundLimit:      defaultTeamRoundLimit,
			NoProgressLimit: defaultTeamNoProgressLimit,
		})
		service.teamRuntimes.put(mainSessionID, runtime)
		return runtime
	}
	runtime.SyncOrder(view.OrderRoles, roleSessionsOf(view), view.OrderPolicy)
	return runtime
}

// syncTeamRuntime 在角色注册表/顺序发生变更后同步环（增删改员工与顺序调整的
// 统一后置动作）。失败不阻断写路径：环会在下一次读视图时按事实重建。
func (service *Service) syncTeamRuntime(mainSessionID string) {
	view, err := service.agentTeamRawView(mainSessionID)
	if err != nil {
		return
	}
	// 角色被删除后，环里已停止的状态不再有意义（成员表变了）；但"已停止"是
	// 显式逃生结论，不因为一次同步就被复活，因此同步而已。
	service.teamRuntimeFor(mainSessionID, view)
}

// releaseTeamRuntime 释放某个主会话的发言调度运行态（会话删除/归档时调用）。
//
// 环是**派生状态**：顺序来自 lifecycle、成员来自注册表，重建即正确。此前 drop
// 没有调用者，环因此与进程同寿，带来两个后果：① 每个见过的 sessionID 都留一条
// 记账（内存随会话数单调增长）；② 已停止的环会永久污染该会话后续的每一次治理
// 循环（见 Runtime.Reset 的说明）。会话消失时释放，重开时按落盘事实重建。
func (service *Service) releaseTeamRuntime(mainSessionID string) {
	if service == nil || strings.TrimSpace(mainSessionID) == "" {
		return
	}
	service.teamRuntimes.drop(mainSessionID)
}

// noteTeamWorkPrefix 把「主会话上下文（含主会话 draft）」的只读装配喂给团队环当前缀。
//
// 为什么读在应用层、生产不在应用层：前缀的作者是存储侧对主会话的装配
// （roleName=main 复用主会话自身的引擎与 key，wire = main compact 帧 + seq > 切点
// 的已发布行 + 主会话自身 pending draft）。TL 的对话记录同样是 engine loop 写出的
// 行，所以前缀与对话记录同口径——这也是它不能由治理域的回合摘要顶替的原因。
//
// 两条"宁缺勿造"：
//   - 该会话还没有环就不建环（对齐"没有环就没有消费者"：没有团队就没有前缀的
//     接收方）；
//   - 读不到事实（未装配存储/会话不存在/布局不支持）就不动前缀，绝不用占位正文
//     顶替主会话上下文——宁可让下一个成员拿到旧前缀，也不给它一段假的上下文。
func (service *Service) noteTeamWorkPrefix(mainSessionID string) {
	if service == nil || strings.TrimSpace(mainSessionID) == "" {
		return
	}
	runtime := service.teamRuntimes.get(mainSessionID)
	if runtime == nil {
		return
	}
	wire, err := service.AssembleRoleWire(mainSessionID, RoleNameMain, mainSessionID, teamPrefixWireBudget, teamPrefixWireK)
	if err != nil {
		return
	}
	runtime.NoteMainContext(wire)
}

// 读前缀用的装配参数：与前端 role wire 探针同口径（budget=200000, k=3）。参数不
// 一致就等于换了另一条 wire，前后端会立刻对不上。
const (
	teamPrefixWireBudget = 200_000
	teamPrefixWireK      = 3
)

// teamScheduleFor 投影指定会话的调度运行态（nil = 该会话没有环）。
func (service *Service) teamScheduleFor(mainSessionID string) *dto.TeamSchedule {
	runtime := service.teamRuntimes.get(mainSessionID)
	if runtime == nil {
		return nil
	}
	schedule := runtime.Snapshot()
	return &schedule
}

// defaultTeamNoProgressLimit 是连续无进展轮次的逃生下上限：连续这么多轮既没有
// 新工具产出、也没有正文推进，就认为它在空转。
const defaultTeamNoProgressLimit = 3

// teamRuntimeBySession 返回（需要时创建）指定会话的发言调度运行态。席位轮转退场
// 后（2026-10-03 阶段三 W3）它只剩一个生产用途：环逃生记账。宿主未装配 AgentTeam
// 存储时返回 nil（调用方按"没有环"处理，不做任何座位派生）。
func (service *Service) teamRuntimeBySession(sessionID string) *agentteam.Runtime {
	if service == nil {
		return nil
	}
	if runtime := service.teamRuntimes.get(sessionID); runtime != nil {
		return runtime
	}
	view, err := service.agentTeamRawView(sessionID)
	if err != nil {
		return nil
	}
	return service.teamRuntimeFor(sessionID, view)
}

// teamRoleSeatsFor / roleSeatOf 已随席位轮转退场删除（2026-10-01 阶段三 W3）：
// 治理循环不再按角色 kind 派生座位，团队角色只服务 leader 派活的 worker 作业。
