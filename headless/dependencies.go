package headless

import (
	"context"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/contract/dto"
	goaldomain "github.com/RedHuang-0622/seelex/kernel/goal"
)

// dependencies.go — headless 控制面自己的窄端口与装配面（M2，兼容门面退场）。
//
// 为什么端口按 owner 切，而不是按 RPC 分段切：一个端口的形状必须与某个 owner 的
// 方法逐字一致，装配处才能直接交出 owner 指针（结构满足）。把两个 owner 的方法缝
// 进同一个端口，就需要一个缝合层——那正是本工作包要删掉的兼容门面。
//
// 为什么方法名跟着 owner 走（`Begin` 而不是 `BeginNewSession`、`Board` 而不是
// `ToolRecordBoard`）：改名只可能发生在转发层，而转发层不拥有任何判据，属于禁止的
// 第二份实现。RPC 线上名字（`/rpc` 的 method 字符串）是外部驱动契约，保持不变；
// 变的只是 Go 侧调用落到哪个 owner。
//
// 可选能力（Roles/Goal/Subagents/ToolRecords 面）在旧形态里靠运行时类型断言探测：
// 9 处断言、5 个可选面（`server.app.(roleRPCApplication)` 等）。生产路径上
// `Application` 的唯一实现者是兼容门面，它实现全部 5 个面，因此断言恒为真；false
// 分支只被测试里刻意只实现一部分面的假实现触达，且无用例断言那条「未装配」错误。
// 迁移后不留判空：有无只由装配表说话，缺 owner 就是装配错误，不新增一层走不到的错误面。

// ViewEvents 是全量会话事件流订阅（owner：session/viewstate 的视图协调器）。
type ViewEvents interface {
	Subscribe(buffer int) application.Subscription
}

// ViewSnapshot 是当前视图会话快照读数（owner：session/viewstate 的快照用例）。
// goal.* 分派用它解析"当前视图会话"，与 Bridge 同一口径。
type ViewSnapshot interface {
	Snapshot() application.Snapshot
}

// TurnControl 是回合中止与空闲等待（owner：application/lifecycle）。
type TurnControl interface {
	CancelChat(requestID string) bool
	WaitForIdle(ctx context.Context) error
}

// Submitter 把一条输入提交给当前视图会话（owner：application/input 的提交用例）。
type Submitter interface {
	Submit(ctx context.Context, text string) error
}

// DraftStarter 开启一个新的草稿会话（owner：session 的草稿入口用例）。
type DraftStarter interface {
	Begin() error
}

// SessionResumer 把视图切到某个已存在会话（owner：session 的驻留/恢复用例）。
// `ResumeSession` 与 `ActivateSession` 两条 RPC 是同一用例的两个入口。
type SessionResumer interface {
	Resume(sessionID string) error
}

// SessionForker 从父会话最新状态分叉（owner：session 的分叉用例）。
type SessionForker interface {
	ForkLatest(parentID string) (string, error)
}

// InteractionResolver 裁决一条待决交互（owner：session 的交互用例）。
type InteractionResolver interface {
	Resolve(ctx context.Context, interactionID, optionID string) error
}

// HistoryPager 是可见历史窗口的翻页（owner：application/history）。
type HistoryPager interface {
	LoadMoreHistory(limit int) error
	LoadLatestHistory() error
}

// SessionCatalog 是会话目录读数与收敛等待（owner：session/query）。
type SessionCatalog interface {
	ListSessions() []application.SessionInfo
	SnapshotOf(sessionID string) (application.SessionSnapshot, error)
	WaitCatalogRefresh(ctx context.Context) error
}

// WorkspaceSwitcher 是项目登记与视图绑定（owner：workspace/switch 用例）。
type WorkspaceSwitcher interface {
	CreateWorkspace(name, rootPath, gitRemote string) error
	BindWorkspace(workspaceID string) error
	UnbindWorkspace()
}

// PermissionTierSwitcher 切换运行期权限档位（owner：application/policy 的开关面）。
type PermissionTierSwitcher interface {
	SetPermissionTier(tier string) (string, error)
}

// PerfReader 是性能读数（owner：application/observability）。
type PerfReader interface {
	PerfStats() application.PerfStats
}

// ToolRecordController 是工具记录看板读数与计数开关（owner：execution/toolrecord）。
type ToolRecordController interface {
	Board() application.ToolRecordBoard
	SetEnabled(enabled bool) (bool, error)
}

// RoleSessionStore 是群聊角色会话的草稿/备份/装配/生命周期面
// （owner：contract.RoleSessionsPort，实现为 store 的角色会话读写）。
// 真实排序、幂等与 floor 判据都在 owner 内，这里只是透传。
// S27 收口：只用 contract/dto 纯 DTO，前端不 import 存储包。
type RoleSessionStore interface {
	CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (dto.RoleSessionInfo, error)
	AppendRoleDraft(mainSessionID, roleName, roleSessionID string, rows []dto.RoleDraftRow) error
	ReadRoleDraft(mainSessionID, roleName, roleSessionID string) ([]dto.RoleDraftRow, error)
	SyncRoleDraft(mainSessionID, roleName, roleSessionID string, order []string) (dto.RoleDraftSyncResult, error)
	AppendRoleSessionRows(mainSessionID, roleName, roleSessionID string, rows []dto.RoleRow) error
	ReadRoleSessionRows(mainSessionID, roleName, roleSessionID string) ([]dto.RoleRow, error)
	RoleSnapshot(mainSessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error)
	AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (dto.RoleWireSnapshot, error)
	SetLifecycleOrder(sessionID, policy string, roles []string) error
	SetRoleLifecycle(mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *dto.CompactFrameRef) error
	ListRoleSessions(mainSessionID string) ([]string, error)
	ScheduleRegister(sessionID string, payload dto.ScheduleEventPayload) error
	ScheduleCancel(sessionID string, payload dto.ScheduleEventPayload) error
	ScheduleFire(sessionID string, payload dto.ScheduleEventPayload) error
}

// GoalController 是 goal 治理面（owner：application/goal，按显式会话路由）。
type GoalController interface {
	BeginFor(ctx context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error)
	UpdateFor(ctx context.Context, sessionID string, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error)
	DoneFor(ctx context.Context, sessionID string, request goaldomain.FinishRequest, abort bool) (*goaldomain.GoalRecord, error)
	StatusFor(sessionID string) (goaldomain.StatusView, error)
	GovernanceViewFor(sessionID string) *dto.GoalGovernanceView
}

// SubagentRecovery 是子代理恢复面（owner：application/execution 的子代理用例）。
type SubagentRecovery interface {
	ListRecovery(sessionID string) ([]dto.SubagentRecoveryView, error)
	Fork(ctx context.Context, sessionID string, specs []dto.SubagentForkSpec) (string, error)
	ResumeInterrupted(ctx context.Context, sessionID string) (dto.SubagentResumeReport, error)
	Resume(ctx context.Context, sessionID, nodeID string) (dto.SubagentResumeResult, error)
}

// Dependencies 是 headless 控制面需要的全部应用面：一行一个 owner。
//
// 绑定语句本身就是编译期结构断言（左端字段类型是 consumer 接口、右端是具体 owner
// 指针），因此控制面里既没有适配器，也不需要额外的编译期类型绑定。
// 一类的编译期断言，更没有运行时能力探测。
//
// 全部字段都是控制面的骨架：装配表必须逐个交出 owner，缺任一个即装配错误。
// 没有"可选能力"，因此也没有对应的判空分支。
type Dependencies struct {
	Events      ViewEvents
	Snapshot    ViewSnapshot
	Turn        TurnControl
	Submission  Submitter
	Draft       DraftStarter
	Resume      SessionResumer
	Fork        SessionForker
	Interaction InteractionResolver
	History     HistoryPager
	Catalog     SessionCatalog
	Workspace   WorkspaceSwitcher
	Policy      PermissionTierSwitcher
	Perf        PerfReader
	ToolRecords ToolRecordController
	Roles       RoleSessionStore
	Goal        GoalController
	Subagents   SubagentRecovery
}
