// agentteam_runtime.go —— 会话级「发言调度运行态」的持有面（**J-1a 之后为遗留
// 不可达路径**：2026-10-12 Agent Team 用户面退场）。
//
// 退场说明（谁删了什么、什么留下、为什么）：
//
//   - **来路全删**：环的唯一建造点 `teamRuntimeFor`（由 `MaterializeAgentTeam` /
//     `AgentTeamView` / `AgentTeamSetOrder` 调用）随 `application/core/agentteam_service.go`
//     整份删除，因此本进程**不再有任何来路**往 teamRuntimeStore 里 put 一个环。
//   - **留下的是形状而不是能力**：`teamRuntimeBySession`（goal 的 `TeamRuntimeFor`）、
//     `noteTeamWorkPrefix`（chat.go 的调用点）、`releaseTeamRuntime`（会话归档/切
//     工作区）三条**调用点**保留，全部退化到"没有环"那一支——与退场前的实际行为
//     逐字相同（退场前：会话没装配团队时这三条也是这么做/这么返回）。
//   - **不改 goal 逻辑**（本条目的硬约束）：`goal_coordinator.go` 的
//     `TeamRuntimeFor` 读面形状不变，只是恒为 nil ⇒ 环逃生记账（`NoteTurn`）不可达
//     但无害。
//   - **不删 `application/core/agentteam` 包的 runtime/scheduler**：`*agentteam.Runtime`
//     是 `TeamRuntimeFor` 的签名类型，删它就是改 goal 的签名；本条目只删用户面。

package core

import (
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/application/core/agentteam"
)

// teamRuntimeStore 按主会话持有发言调度运行态。自带锁：环的推进（Next/记账）
// 与视图刷新互不阻塞，也不参与 Core.ViewMu 的快照事务。
//
// J-1a 之后它恒为空（见文件头）：保留的是"读面恒返回 nil"这一条事实的形状。
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

// noteTeamWorkPrefix 把「主会话上下文（含主会话 draft）」的只读装配喂给团队环当前缀。
//
// **J-1a 之后不可达**（2026-10-12）：环的唯一来路（会话团队装配面）已退场，因此
// `teamRuntimes.get` 恒为 nil，本函数在第一道守卫处返回——这正是"没有环就没有
// 消费者"那条既有口径，也是退场前的实际行为。保留函数体（含它那两条"宁缺勿造"
// 的判断）是为了不改调用点、也不发明第二份口径；`AssembleRoleWire` 的读因此不会被
// 触发（它是有代价的主会话装配，不能让一条死路顺手跑）。
//
// 为什么读在应用层：前缀的作者是存储侧对主会话的装配（roleName=main 复用主会话
// 自身的引擎与 key，wire = main compact 帧 + seq > 切点的已发布行 + 主会话自身
// pending draft）。TL 的对话记录同样是 engine loop 写出的行，所以前缀与对话记录同
// 口径——这也是它不能由治理域的回合摘要顶替的原因。
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

// releaseTeamRuntime 释放某个主会话的发言调度运行态（会话删除/归档时调用）。
//
// 环是**派生状态**：顺序来自 lifecycle、成员来自注册表，重建即正确。会话消失时
// 释放，重开时按落盘事实重建。J-1a 之后表恒空，本函数是幂等的空操作（调用点保留：
// 它属于"会话生命周期"那条路径，不该因为环的来路退场而改变形状）。
func (service *Service) releaseTeamRuntime(mainSessionID string) {
	if service == nil || strings.TrimSpace(mainSessionID) == "" {
		return
	}
	service.teamRuntimes.drop(mainSessionID)
}

// teamRuntimeBySession 返回指定会话的发言调度运行态。
//
// **J-1a 之后恒为 nil**（2026-10-12）：环的唯一来路（会话团队装配面）随本条目退场，
// 因此"这个会话有没有环"恒为"没有"。goal 侧的 `TeamRuntimeFor` 读面形状不变，
// 环逃生记账（NoteTurn）由此成为**遗留不可达路径**——不可达但无害，且不改 goal 逻辑。
//
// 退场前它还有第二个分支（按主会话注册表按需建环）；那条读面（`agentTeamRawView`）
// 已随团队注册表整份删除，所以这里只留一条明确的"没有环"。
func (service *Service) teamRuntimeBySession(sessionID string) *agentteam.Runtime {
	if service == nil {
		return nil
	}
	return service.teamRuntimes.get(sessionID)
}
