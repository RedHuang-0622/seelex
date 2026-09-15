// agentteam_runtime.go 把 AgentTeam 的发言调度运行态接到 Application 能力面。
//
// 边界：agentteam 包负责「环里怎么转」（链表顺序、user 席位、逃生记账）；
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

// goalLoopRoundLimitForTeam 取治理循环实际生效的轮次上限：环的逃生路径第一道
// 与 Governor 的 maxRounds 必须同源，否则前端显示的"n/limit"和真正兜底的数字
// 会打架。
func (service *Service) goalLoopRoundLimitForTeam() int {
	if service == nil || service.components.goal == nil {
		return defaultGoalLoopMaxRounds
	}
	return goalLoopRoundLimit(service.components.goal.deps.MaxRounds)
}

// teamRuntimeFor 返回（需要时创建）指定主会话的发言调度运行态，并把注册表
// 顺序同步进环。读路径不写盘：顺序事实仍然只有 lifecycle 一份。
func (service *Service) teamRuntimeFor(mainSessionID string, view dto.TeamView) *agentteam.Runtime {
	runtime := service.teamRuntimes.get(mainSessionID)
	if runtime == nil {
		runtime = agentteam.NewRuntime(view.OrderRoles, roleSessionsOf(view), view.OrderPolicy, agentteam.RuntimeOptions{
			RoundLimit:      service.goalLoopRoundLimitForTeam(),
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

// NoteTeamUserQueued 告诉该会话的环"消息队列里有没有未消费的 user 输入"。
// user 席位口径为 queued（缺省）时，这正是 user 是否占位的唯一依据：user 可以
// 随时经消息队列插入会话，但不会因为"人还没说话"把整条环卡住。
//
// 该会话还没有环时不创建（没有团队就没有环；等 teamRuntimeFor 建环时再按当时
// 队列状态对齐）。
func (service *Service) NoteTeamUserQueued(mainSessionID string, pending bool) {
	if runtime := service.teamRuntimes.get(mainSessionID); runtime != nil {
		runtime.NoteUserQueued(pending)
	}
}

// teamScheduleFor 投影指定会话的调度运行态（nil = 该会话没有环）。
func (service *Service) teamScheduleFor(mainSessionID string) *dto.TeamSchedule {
	runtime := service.teamRuntimes.get(mainSessionID)
	if runtime == nil {
		return nil
	}
	schedule := runtime.Snapshot()
	return &schedule
}

// defaultTeamNoProgressLimit 是连续无进展轮次的逃生下上限：治理循环里连续
// 这么多轮既没有新工具产出、也没有正文推进，就认为它在空转。
const defaultTeamNoProgressLimit = 3

// teamRuntimeBySession 返回（需要时创建）指定会话的发言调度运行态，供治理循环
// 装配座位使用。宿主未装配 AgentTeam 存储时返回 nil（治理循环退回内置座位）。
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

// noteTeamUserSeat 把"该会话队列里有没有未消费的 user 输入"同步给团队环：
// user 经消息队列插话时，环要据此决定 user 是否占位（缺省口径 queued）。
func (service *Service) noteTeamUserSeat(sessionID string) {
	if service == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	pending := 0
	if unit := service.sessions.Unit(sessionID); unit != nil {
		pending = len(queuedChatRequests(unit.PendingRequests()))
	}
	service.NoteTeamUserQueued(sessionID, pending > 0)
}
