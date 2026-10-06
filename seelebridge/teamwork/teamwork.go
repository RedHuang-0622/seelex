// Package teamwork 把 teamwork 从「一排轮流发言的座位」重做成
// 「一个 leader + 一组可被派发 / 观察 / 终止的 worker 作业」。
//
// 分工（docs/arch/teamwork-leader-worker-architecture.md §2 / D1）：
//
//   - **作业面**归 Seele 的 jobs 根能力（契约 + Manager + jobs_manage）；
//   - **硬编排**归这里：计划（谁、什么顺序）+ 派发 + 汇合 + 里程碑 + 退场；
//   - **执行体**（在角色会话里真跑一轮）与**工作区**（git worktree）是端口，
//     由装配层注入——本包因此能在没有引擎、没有 git 的测试里把编排语义
//     （顺序、超员拒绝、作用域回收、退场四步）全部跑完。
//
// 顺序的唯一事实是计划的顺序边：里程碑之间是 milestones[].depends_on（屏障），
// 里程碑内是 work_items[].depends_on（DAG）；不是 leader 的调用姿势，也不是任何
// "上一轮是谁"的隐式状态。（阶段口径已整条退场：没有 stages 这个形状了。）
package teamwork

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/jobs"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// 作业类别：在 jobs 的开放 Kind 上注册，框架不解释这个值。
const (
	// KindWorker 是 teammate 作业：在角色会话里跑限量回合。
	KindWorker jobs.Kind = "worker"
)

// SubjectForRole 是 teammate 的主体名：既是权限主体，也是作业作用域的第二
// 分量（与 sessionstore 的 emp 主体同名，权限面因此不需要第二套映射）。
func SubjectForRole(role string) string { return "emp_" + role }

// DefaultRoleSessionID 派生角色会话号：同一个 (主会话, team_id, role) 永远得到同一
// 个值（重复装配幂等）。与 agentteam.RoleSessionID 同形；装配层可用
// Options.DeriveRoleSessionID 注入权威实现。
//
// 主会话身份是**必需分量**（2026-10-01，用例 2「团队会话粒度」）：两个会话召唤同一
// 支团队时，worker 的角色会话必须各自独立——否则角色引擎槽、项目根绑定、权责反查
// 会把两条会话的同名员工当成同一个人（详见 agentteam.RoleSessionID 的说明）。
func DefaultRoleSessionID(mainSessionID, teamID, roleName string) string {
	mainSessionID = strings.TrimSpace(mainSessionID)
	if mainSessionID == "" {
		return teamID + "-" + roleName
	}
	return mainSessionID + "-" + teamID + "-" + roleName
}

// PlanStore 是计划的持久面（生产实现 = sessionstore.TeamworkRepository）。
type PlanStore interface {
	WritePlan(ctx context.Context, key sessionstore.Key, plan sessionstore.TeamworkPlan, maxTeammates int) error
	ReadPlan(ctx context.Context, key sessionstore.Key) (sessionstore.TeamworkPlan, error)
	AppendEvent(ctx context.Context, key sessionstore.Key, event sessionstore.TeamworkEvent) error
	ReadEvents(ctx context.Context, key sessionstore.Key) ([]sessionstore.TeamworkEvent, error)
	// AppendBinding / ReadBindings 是「一个 Work Item ↔ 一个 Session + 一个 worktree」
	// 的绑定账本（追加型 JSONL，KV 语义；见 sessionstore.TeamworkBinding）。
	AppendBinding(ctx context.Context, key sessionstore.Key, binding sessionstore.TeamworkBinding) error
	ReadBindings(ctx context.Context, key sessionstore.Key) ([]sessionstore.TeamworkBinding, error)
}

// WorkerRequest 是一次 teammate 作业的全部输入。它同时是作业载荷
// （jobs.Spec.Payload）与执行体入参——只有一份定义，就不会出现"派发时带了、
// 执行时丢了"的静默降级。
type WorkerRequest struct {
	// MainSessionID 是派发它的主会话（leader 会话）：worker 的项目根与权限归属
	// 都挂在它上面；作业会活过派发它的那一轮，所以必须随载荷带过来，不能指望
	// 执行体的 ctx 里还有主会话（那是作业自己的 ctx）。
	MainSessionID string `json:"main_session_id"`
	TeamID        string `json:"team_id"`
	Role          string `json:"role"`
	RoleSessionID string `json:"role_session_id"`
	Subject       string `json:"subject"`
	// ToolsPolicy / PermissionGroups 是该 teammate 的权责口径（来自计划成员条目）：
	// 装配层据此在该角色自己的主体 emp_<role> 上分配位（与用户权限同一张表）。
	// 两者都空 = 继承宿主默认。
	ToolsPolicy      string           `json:"tools_policy,omitempty"`
	PermissionGroups map[string]uint8 `json:"permission_groups,omitempty"`
	// Plugins 是这个 teammate 的**按会话插件装配**（能力轴；空/缺失 = 不覆盖：
	// 工具面继承宿主当前装配 + 技能目录不注入）。与 ToolsPolicy 同构：随载荷走完
	// 整条透传链，派发时带了、执行时不许丢。
	//
	// 权限**不在这里**：插件只收窄能力（工具面 ∩），永不放宽权限面——"装了插件就
	// 有权限"是这条硬规则要挡住的那种读法。
	Plugins  []string `json:"plugins,omitempty"`
	Worktree string   `json:"worktree,omitempty"`
	// WorkItemID / Milestone 是这一轮工作属于甘特图的哪个节点。**一 Work Item 一个
	// Session + 一个 worktree** 的隔离与回收都以它为准（空 = 非 Work Item 口径的
	// 派发，走 teammate 级的老口径）。
	//
	// 归属只有这一个形状（2026-10-04）：阶段口径已退场，`stage` 字段整条删除——
	// 留着它就会出现"作业归属到底是 stage 还是 milestone"的两套答案。
	WorkItemID string `json:"work_item_id,omitempty"`
	Milestone  string `json:"milestone,omitempty"`
	Goal       string `json:"goal"`
	// MaxTurns 是本轮在角色会话里允许的回合上限（0 = 装配层默认）。
	MaxTurns int `json:"max_turns,omitempty"`
	// OutputPath 是本轮正文的落点（**产品自有**文件；空 = 交回框架自建）。
	//
	// 随载荷带过来而不是让执行体自己去分配：路径在派发那一刻就写进
	// jobs.Spec.OutputPath，执行体只是"按给定的落点写"——两处各算一次路径，
	// 迟早算成两个文件。非空时框架**不建写句柄、只按偏移读**（externalOutput），
	// 因此执行体必须自己写这个文件（sink.Note 在这种形态下是空操作），且写要经
	// JobOutputs.WriteJobOutput——那才与"收口清目录"串行（S5 残边）。
	OutputPath string `json:"output_path,omitempty"`
}

// WorkerRunner 在角色会话里跑有限回合。
//
// 生产实现是升格后的 runtime_role_turn：起手把 emp_<role> 主体放进执行
// ctx（该回合的工具面按角色权责收窄），并把 teammate 工具面里**没有**
// fork_subagents（D6 硬移除）这件事带进装配，而不是靠运行时判断。
type WorkerRunner interface {
	RunWorker(ctx context.Context, request WorkerRequest, sink jobs.Sink) error
}

// SeatRequest / SeatRunner / SeatRoundRunner 已随席位轮转退场删除
// （2026-10-01 阶段三 W3）。teamwork 作业面现在只有 worker 一类。

// WorkspaceReleaser 释放一个 teammate 的工作区（git worktree remove + 删本地
// 分支）。释放前若工作区脏，实现必须按 ErrUncommittedChanges 语义显式报错，
// 不得静默丢弃（D7 / §4.7）。
type WorkspaceReleaser interface {
	ReleaseWorkspace(ctx context.Context, role string) error
}

// WorkspaceBinding 是「一个 Work Item ↔ 一个 worktree」的现场。
//
// Worktree 是**指派名**（进计划与账本），Path / Branch 是**现场**（git 的事，
// 由实现回填）；协调器不认识目录布局。
type WorkspaceBinding struct {
	MainSessionID string `json:"main_session_id"`
	TeamID        string `json:"team_id"`
	Milestone     string `json:"milestone,omitempty"`
	WorkItem      string `json:"work_item"`
	Role          string `json:"role"`
	SessionID     string `json:"session_id"`
	Worktree      string `json:"worktree"`
	Path          string `json:"path,omitempty"`
	Branch        string `json:"branch,omitempty"`
}

// Workspaces 是**一个 Work Item 一个 worktree** 的端口：建、并、释放。
//
// 三段各自回答一个问题——BindWorkspace「这件事在哪里干」（隔离），
// MergeWorkspace「这件事的改动怎么回到主干」（尾插的前置：先合并、成功才插入），
// ReleaseWorkspaceItem「这件事的现场什么时候消失」（验收通过 / 整队收口）。
//
// 缺失（未装配）= 不建现场（Worktree 只是一个指派名）、不合并、不释放——"有就有、
// 没有就是没装配"。降级是**显式**的：退场/验收时若绑定还在，实现必须自己确认
// "没有现场可释放"而不是让协调器猜。
type Workspaces interface {
	BindWorkspace(ctx context.Context, binding WorkspaceBinding) (WorkspaceBinding, error)
	MergeWorkspace(ctx context.Context, binding WorkspaceBinding) error
	ReleaseWorkspaceItem(ctx context.Context, binding WorkspaceBinding) error
}

// TeammateMessage 是尾插进 teammate **消息队列**的一行。
type TeammateMessage struct {
	MainSessionID string `json:"main_session_id"`
	TeamID        string `json:"team_id"`
	Role          string `json:"role"`
	RoleSessionID string `json:"role_session_id"`
	Milestone     string `json:"milestone,omitempty"`
	WorkItem      string `json:"work_item"`
	Text          string `json:"text"`
}

// TeammateQueue 是 teammate 的**消息队列**（尾插的落点）。
//
// 为什么需要它：作业是后台跑的，结果不能 push 进忙会话（§6.1 铁律）。尾插把
// "这件事跑完了、结果是这个、bug 是这个"追加到 teammate 自己的消息队列，由它在
// 下一个回合边界按有界摘要读走——既不唤醒忙会话，也不让结论悬空。
type TeammateQueue interface {
	EnqueueTeammateMessage(ctx context.Context, message TeammateMessage) error
}

// SessionResetter 清空一个角色会话的**记录内容**（工作历史 + durable 快照），
// 保留在编。删的是对话记忆，不是注册。
type SessionResetter interface {
	ResetSession(ctx context.Context, roleSessionID string) error
}

// BoardCloser 封板团队看板**存档**（closed / team.close）。
//
// 为什么是一个窄端口而不是让本包认识看板存档的形状：存档是**下游历史面**
// （sessionstore 的 moduleBoardTeam），本包只说"把这支团队的看板关掉"，记录形状与
// 写序归 seelebridge（那里已有唯一的存档写路径）。缺失 = 不写存档（域内 closed 与
// 审计照常）——"有就有、没有就是没装配"。
type BoardCloser interface {
	CloseTeamBoard(ctx context.Context) error
}

// JobOutputs 是 teammate 作业**输出文件**的产品面（§4.7 输出归属）：
//
//   - JobOutputPath 在派发时分配一个产品自有路径。非空即接管：框架
//     （jobs.Spec.OutputPath）不建写句柄、只按偏移读，销项 / 驱逐 / Close 都不删它
//     ——于是"阶段收尾读一次产出"不会把正文带走，正文活到产品决定的那一刻。
//   - WriteJobOutput 是执行体写正文的**唯一入口**：写经产品面而不是各自 os.WriteFile，
//     于是"写"与"清目录"落在实现里的同一把锁上串行。这条串行正是"被取消的执行体最后
//     一次写"不越过收口清目录的保证（S5 残边，devlog §4.3）。
//   - ClearJobOutputs 在**整队收口**时清掉这一批文件：生命周期归产品，收口就是
//     产品决定的那一刻（会话目录本身仍由会话清理兜底）。清掉的同时**作废**这一批
//     落点，迟到的写因此被丢弃，而不是把残文件重新造出来。
//   - LatestJobOutputPath 按**角色名**定位该角色最近一份正文。它服务于一条残边：
//     作业行是内存态、会被框架 prune 逐出（框架没有 pin 概念），逐出之后按句柄读不到，
//     而正文文件归产品、活到收口——读面据此按角色名回读（devlog §4.2）。
//
// 缺失 = 不接管（框架自建文件、按框架语义删除）——"有就有、没有就是没装配"。
type JobOutputs interface {
	JobOutputPath(ctx context.Context, role string) (string, error)
	WriteJobOutput(path, text string) error
	ClearJobOutputs(ctx context.Context) error
	LatestJobOutputPath(ctx context.Context, role string) (string, bool, error)
}

// Options 装配一个 Coordinator。必填：Store、Jobs；其余端口按能力装配，
// 缺失时对应的动作显式报错（不静默降级）。
type Options struct {
	// Key 是会话作用域（会话键 = 作业隔离与回收粒度）。
	Key sessionstore.Key
	// Store 是计划与审计的持久面。
	Store PlanStore
	// Jobs 是作业面（frame jobs.Manager），worker/seat 执行体注册在它上面。
	Jobs jobs.Manager
	// Workers 是 teammate 执行体（缺失 ⇒ team_dispatch 拒绝）。
	Workers WorkerRunner
	// Worktrees 释放工作区（缺失 ⇒ team_close 的收口第二步显式报错，不静默跳过）。
	Worktrees WorkspaceReleaser
	// Spaces 是「一个 Work Item 一个 worktree」的端口（建 / 并 / 释放）。缺失 =
	// 不建现场、不合并、不释放——Work Item 仍可排活与派发，只是没有工作区隔离。
	Spaces Workspaces
	// Teammates 是 teammate 的消息队列（尾插落点）。缺失 = 尾插被丢弃（只留审计行）。
	Teammates TeammateQueue
	// Sessions 清角色会话内容（缺失 ⇒ team_close 的收口第三步显式报错）。
	Sessions SessionResetter
	// Boards 封板团队看板存档（缺失 ⇒ Close 只做域内 closed + 审计，不写存档）。
	Boards BoardCloser
	// JobOutputs 分配 / 清理 teammate 作业的输出文件（缺失 ⇒ 交回框架自建文件，
	// 按框架语义在销项 / 驱逐 / Close 时删除）。
	JobOutputs JobOutputs
	// MaxTeammates 是产品级人数上限（seelexctx.TeamLimits.MaxTeammates）。
	// <= 0 = 不限制（不推荐：框架在途上限会先于产品约束生效）。
	MaxTeammates int
	// MaxTurns 是每个 teammate 作业的回合上限（0 = 装配层默认）。
	MaxTurns int
	// DeriveRoleSessionID 覆盖角色会话号的派生（默认 DefaultRoleSessionID）。
	DeriveRoleSessionID func(mainSessionID, teamID, roleName string) string
	// Clock 覆盖墙钟（测试用）。
	Clock func() time.Time
}

// Coordinator 是 leader 的编排面：计划 + 派发 + 汇合 + 里程碑 + 退场。
//
// 它自己**不跑**任何 teammate：跑是 jobs.Manager + WorkerRunner 的事，它只
// 掌控顺序、作用域与收口——这正是"leader 阻塞与否与 worker 是否推进正交"的
// 落点（§6.3）。
type Coordinator struct {
	key        sessionstore.Key
	store      PlanStore
	jobs       jobs.Manager
	workers    WorkerRunner
	worktrees  WorkspaceReleaser
	sessions   SessionResetter
	boards     BoardCloser
	jobOutputs JobOutputs
	spaces     Workspaces
	teammates  TeammateQueue
	maxMembers int
	maxTurns   int
	derive     func(mainSessionID, teamID, roleName string) string
	clock      func() time.Time
}

// New 装配一个 Coordinator。
func New(options Options) (*Coordinator, error) {
	if strings.TrimSpace(options.Key.ProjectID) == "" || strings.TrimSpace(options.Key.SessionID) == "" {
		return nil, errors.New("teamwork: 会话作用域（project_id + session_id）是必填")
	}
	if options.Store == nil {
		return nil, errors.New("teamwork: PlanStore 是必填")
	}
	if options.Jobs == nil {
		return nil, errors.New("teamwork: jobs.Manager 是必填")
	}
	derive := options.DeriveRoleSessionID
	if derive == nil {
		derive = DefaultRoleSessionID
	}
	clock := options.Clock
	if clock == nil {
		clock = time.Now
	}
	return &Coordinator{
		key:        options.Key,
		store:      options.Store,
		jobs:       options.Jobs,
		workers:    options.Workers,
		worktrees:  options.Worktrees,
		sessions:   options.Sessions,
		boards:     options.Boards,
		jobOutputs: options.JobOutputs,
		spaces:     options.Spaces,
		teammates:  options.Teammates,
		maxMembers: options.MaxTeammates,
		maxTurns:   options.MaxTurns,
		derive:     derive,
		clock:      clock,
	}, nil
}

// Key 返回会话作用域。
func (c *Coordinator) Key() sessionstore.Key { return c.key }

// audit 追加一条审计行。审计失败**不吞**：它与计划是同一份事实的两个面。
func (c *Coordinator) audit(ctx context.Context, event sessionstore.TeamworkEvent) error {
	event.At = c.clock().UTC()
	return c.store.AppendEvent(ctx, c.key, event)
}

// memberFor 返回在编成员。
func memberFor(plan sessionstore.TeamworkPlan, role string) (sessionstore.TeamworkMember, bool) {
	for _, member := range plan.Members {
		if member.Role == role {
			return member, true
		}
	}
	return sessionstore.TeamworkMember{}, false
}

// memberPermissionGroups 把计划里的权限格子（组 → 位）折成框架的 uint8 位图。
// 位是非负整数且落在 0..255；越界是**计划写错了**，显式报错而不是截断伪装成
// "分配成功"。
func memberPermissionGroups(member sessionstore.TeamworkMember) (map[string]uint8, error) {
	if len(member.Permission) == 0 {
		// 没写权限格子 = 没有要折的位，不是错误：显式写出类型的零值而不是裸
		// `return nil, nil`（静态门禁口径，见 permission.go 里同一条注释）。
		return map[string]uint8(nil), nil
	}
	groups := make(map[string]uint8, len(member.Permission))
	for group, bits := range member.Permission {
		if bits < 0 || bits > 255 {
			return nil, fmt.Errorf("teamwork: 成员 %q 的权限组 %q 位=%d 越界（0..255）", member.Role, group, bits)
		}
		groups[group] = uint8(bits)
	}
	return groups, nil
}

func describeHandle(handle jobs.Handle) string { return string(handle) }

func summarize(records []jobs.Record) string {
	states := make([]string, 0, len(records))
	for _, record := range records {
		states = append(states, fmt.Sprintf("%s=%s", record.Handle, record.State))
	}
	return strings.Join(states, " ")
}
