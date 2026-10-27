package store

// teamwork.go — teamwork 面在根包的**兼容出口**（M8 起）。
//
// 口径（docs/arch/teamwork-leader-worker-architecture.md §4.6 / D3 / D4）不变：
//
//   - **顺序的唯一事实**是 plan.milestones[].depends_on（里程碑屏障）+ 里程碑内
//     plan.milestones[].items[].depends_on（DAG）；
//   - 存储分两件：**计划**（谁、什么顺序）落 moduleTeamwork 的 head，等价于
//     plan.json——整份替换型内容，原子发布 + 校验和自愈；**审计**落同模块的
//     数据文件 teamwork/events.jsonl——只追加、不重写；
//   - **不存内容**：作业句柄只在内存（jobs I-4）、worktree 路径与差异是 git 的
//     事（D9），这里只留 worktree 的**指派名**，重派时重建。
//
// M8 的所有权裁决：上述物理实现（head 整份替换、两条追加流水、崩溃残尾读取、
// 作业输出目录）已整体迁入 `store/teamwork`（`store/teamwork/store.go`）。本文件
// 只保留**同身份 alias + 单层转发**：根包不再持有 plan/event/binding 的 JSON 编解码、
// 打开文件、建目录或锁实现，也不做第二份判据。删除条件：上层调用方直接持有
// `store/teamwork.Store`（M10 Facade 清零）后，本文件的转发面随之退场。

import (
	"context"
	"errors"

	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
)

// 内置角色复用主会话（team.needsRoleSession），不得作为 teammate 派发。
// 这里重复一次常量而不是 import agentteam：存储层不该依赖上层角色装配，
// 而这条约束必须在**计划可被 leader 改写**的第二道校验关口上仍然成立。
const (
	TeamworkRoleMain = storeteamwork.RoleMain
	TeamworkRoleUser = storeteamwork.RoleUser
)

// 里程碑状态取值（空 = pending）。
const (
	TeamworkMilestonePending = storeteamwork.MilestonePending
	TeamworkMilestoneActive  = storeteamwork.MilestoneActive
	TeamworkMilestoneDone    = storeteamwork.MilestoneDone
)

// 审计事件的 kind 取值。
const (
	TeamworkEventPlan      = storeteamwork.EventPlan
	TeamworkEventDispatch  = storeteamwork.EventDispatch
	TeamworkEventJoin      = storeteamwork.EventJoin
	TeamworkEventMilestone = storeteamwork.EventMilestone
	// TeamworkEventRetire 是**旧口径**（team_retire，已整条删除）留在历史审计里的取值：
	// 常量保留是为了老审计还能读回，**不再有新写入**（本轮结束只有 team_close 一个入口）。
	TeamworkEventRetire = storeteamwork.EventRetire
	// TeamworkEventClose 是**整队收口**（team_close）的审计行：与旧口径的 retire
	// 分开记——收口是团队级动作，不是"最后一次退场"。
	TeamworkEventClose = storeteamwork.EventClose
	// TeamworkEventItem / Accept / Fail / Settle / Recover 是 **Work Item 生命周期**
	// 的事实流水（排活 / 验收通过 / 判失败 / 尾插 / 中断恢复）。
	TeamworkEventItem    = storeteamwork.EventItem
	TeamworkEventAccept  = storeteamwork.EventAccept
	TeamworkEventFail    = storeteamwork.EventFail
	TeamworkEventSettle  = storeteamwork.EventSettle
	TeamworkEventRecover = storeteamwork.EventRecover
	// TeamworkEventMessage 是**尾插**进 teammate 消息队列的一行：与审计同一条追加型
	// 事实流，避免"看板看到的"与"审计记的"漂移。
	TeamworkEventMessage = storeteamwork.EventMessage
)

// 计划的运行态取值（空 = 未收口；与里程碑状态的"空 = pending"同口径）。
//
// **闭板事实双写：域内权威在 plan.State**（U3 裁决）——看板存档（moduleBoardTeam）
// 只作历史/恢复，读侧不得据存档反推域内是否已收口。
// TeamworkStateClosed 是整队已收口（team_close）的兼容常量。
const TeamworkStateClosed = storeteamwork.StateClosed

// TeamworkPlan 是 leader 的硬编排计划（moduleTeamwork head 的 payload）。
// 同身份 alias：字段与 JSON tag 由 owner 定义，根包不复制。
type TeamworkPlan = storeteamwork.Plan

// TeamworkMember 是计划成员的同身份 alias。
type TeamworkMember = storeteamwork.Member

// TeamworkMilestone 是计划里程碑的同身份 alias。
type TeamworkMilestone = storeteamwork.Milestone

// TeamworkState 是计划运行态的同身份 alias。
type TeamworkState = storeteamwork.State

// TeamworkPlanIsEmpty 是计划的空判定转发。
func TeamworkPlanIsEmpty(plan TeamworkPlan) bool {
	return storeteamwork.PlanIsEmpty(plan)
}

// TeamworkEvent 是审计流水的一行：只追加，不重写。
type TeamworkEvent = storeteamwork.Event

// TeamworkRepository 是 teamwork 模块的读 / 写面。
//
// 它**不并进 Repository 主接口**：主接口是"每个后端都必须给出"的能力面
// （接口方法不可在包外实现，也不存在"某个后端没有栈通道"的运行期分支），而
// teamwork 只在 v8 JSON 布局上有意义。用可选接口 + 类型断言表达"有就有、
// 没有就是没装配"，比给主接口加一堆在别的后端恒返回错误的成员诚实。
type TeamworkRepository interface {
	WriteTeamworkPlan(context.Context, Key, TeamworkPlan, int) error
	ReadTeamworkPlan(context.Context, Key) (TeamworkPlan, error)
	AppendTeamworkEvent(context.Context, Key, TeamworkEvent) error
	ReadTeamworkEvents(context.Context, Key) ([]TeamworkEvent, error)
	// TeamworkJobOutputDir 返回（必要时创建）该会话 teammate 作业输出文件所在的
	// **产品自有目录**：路径交给 jobs.Spec.OutputPath 之后，框架只按偏移读、永不删，
	// 生命周期归产品（整队收口时清）。
	TeamworkJobOutputDir(context.Context, Key) (string, error)
	// AppendTeamworkBinding / ReadTeamworkBindings 是「一个 Work Item 一个 Session +
	// 一个 worktree」的绑定账本（追加型 JSONL，KV 语义：按 work_item 取最后一行）。
	// 生命周期与 Session 一致：整队收口（team_close）之后一并结束。
	AppendTeamworkBinding(context.Context, Key, TeamworkBinding) error
	ReadTeamworkBindings(context.Context, Key) ([]TeamworkBinding, error)
}

// Teamwork 把 Repository 收窄到 teamwork 面。JSON 后端实现它；其他后端返回 false。
func Teamwork(repository Repository) (TeamworkRepository, bool) {
	teamwork, ok := repository.(TeamworkRepository)
	return teamwork, ok
}

// TeamworkFor 返回**当前活跃后端**的 teamwork 读/写面（JSON 后端实现它；其他后端
// 返回 false）。组合根据此把 moduleTeamwork 装配进 leader 编排面——它不需要知道
// 后端是怎么被选出来的，只问"当前后端给不给这个能力"。
func (router *Router) TeamworkFor() (TeamworkRepository, bool) {
	if router == nil {
		return nil, false
	}
	var repository TeamworkRepository
	var ok bool
	_ = router.withRepository(func(current Repository, _ string) error {
		repository, ok = Teamwork(current)
		return nil
	})
	return repository, ok
}

// ValidateTeamworkPlan 校验一份计划（M2 的第二道关口）。
//
// 为什么存储边界也要校验一次：计划**可被 leader 改写**，装配侧的
// team.Normalize 只保住了"装配那一刻"的唯一性；把校验放在真正落盘的那
// 一步，才能保证"写进会话的每一份计划都是合法的"这条不因调用方不同而漂移。
//
// maxTeammates <= 0 表示调用方不掌握人数上限（只做结构性校验）。
func ValidateTeamworkPlan(plan TeamworkPlan, maxTeammates int) error {
	return storeteamwork.ValidatePlan(plan, maxTeammates)
}

// WriteTeamworkPlan 写入（整份替换）一份计划。maxTeammates <= 0 = 只做结构性校验。
func (repository *jsonRepository) WriteTeamworkPlan(_ context.Context, key Key, plan TeamworkPlan, maxTeammates int) error {
	if err := key.Validate(); err != nil {
		return err
	}
	if repository.layout == nil {
		return errors.New("session storage: teamwork requires the v8 layout engine")
	}
	return repository.layout.teamwork.WritePlan(layoutScope(key), plan, maxTeammates)
}

// ReadTeamworkPlan 读回计划。**缺失 = 空计划（还没 team_plan 声明过团队），不是错误**。
//
// 读面宽容、写面显式：需要计划的写面自己用 `TeamworkPlan.IsEmpty` 判空并给领域文案
// （见 backend/multiagent/teamwork 的 requireTeam）——判据在那一处，不在这里复制。
func (repository *jsonRepository) ReadTeamworkPlan(_ context.Context, key Key) (TeamworkPlan, error) {
	if err := key.Validate(); err != nil {
		return TeamworkPlan{}, err
	}
	if repository.layout == nil {
		return TeamworkPlan{}, errors.New("session storage: teamwork requires the v8 layout engine")
	}
	return repository.layout.teamwork.ReadPlan(layoutScope(key))
}

// AppendTeamworkEvent 追加一条审计事件（只追加、不重写）。
func (repository *jsonRepository) AppendTeamworkEvent(_ context.Context, key Key, event TeamworkEvent) error {
	if err := key.Validate(); err != nil {
		return err
	}
	if repository.layout == nil {
		return errors.New("session storage: teamwork requires the v8 layout engine")
	}
	return repository.layout.teamwork.AppendEvent(layoutScope(key), event)
}

// ReadTeamworkEvents 读回全部审计事件（按文件顺序 = 发生顺序）。
func (repository *jsonRepository) ReadTeamworkEvents(_ context.Context, key Key) ([]TeamworkEvent, error) {
	if err := key.Validate(); err != nil {
		return nil, err
	}
	if repository.layout == nil {
		return nil, errors.New("session storage: teamwork requires the v8 layout engine")
	}
	return repository.layout.teamwork.ReadEvents(layoutScope(key))
}

// TeamworkJobOutputDir 返回（并确保存在）teammate 作业输出文件所在的产品自有目录。
func (repository *jsonRepository) TeamworkJobOutputDir(_ context.Context, key Key) (string, error) {
	if err := key.Validate(); err != nil {
		return "", err
	}
	if repository.layout == nil {
		return "", errors.New("session storage: teamwork requires the v8 layout engine")
	}
	return repository.layout.teamwork.JobOutputDir(layoutScope(key))
}
