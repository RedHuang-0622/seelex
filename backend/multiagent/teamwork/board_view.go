package teamwork

// board_view.go 是团队看板的**投影判据**（P21-20「域方法随类型走」：从
// `backend/engine/assemble/runtime_teamwork_board.go` 把投影与折算搬进本域；plan §2.7 把
// `runtime_teamwork*.go` 列为 `backend/multiagent/teamwork/` 的目标）。
//
// 生态位（契约 docs/arch/team-board-gui-tui-contract.md §3/§4）：
//
//   - 事实三份：计划与审计落 store 的 moduleTeamwork（文件），作业行活在 `jobs.Manager`
//     的内存表里（jobs I-4：句柄只在内存，进程重启即作废）；
//   - 投影只做**搬运与折算**：拓扑、阶段状态折算、层号全部由渲染件的纯函数做——投影层再算
//     一遍就是第二份事实；
//   - **单向**：只有后端 → 前端，这里没有任何写入口。
//
// 装配层保留的是"读哪些事实、缓存多久"（文件读缓存与失效、`jobs.Manager` 的快照读、装配读数
// `assemblyViews` 的现算）——那些是宿主读数；本文件只吃已经读好的值。

import (
	"strings"

	"github.com/RedHuang-0622/Seele/jobs"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/store"
)

// BoardEventLimit 是审计投影的条数上限：审计是**只追加**的流水，
// 无界下发就是让快照载荷随派发次数线性增长。看板只需要"最近发生了什么"。
const BoardEventLimit = 32

const (
	// MemberRunning / MemberFree 是 teammate 状态的**全部取值**。
	MemberRunning = "running"
	MemberFree    = "free"
)

// SubjectPrefix 与 `SubjectForRole` 同源（`emp_<role>`）：装配层用它把作业行的作用域主体
// 反解回角色名。前缀不写字面量——写死一份就是两处定义，改一处漏一处
// （`SubjectForRole("")` 恰好只返回前缀）。
func SubjectPrefix() string { return SubjectForRole("") }

// BuildBoardView 是把（计划 + 审计 + 作业行 + 装配读数）组装成下发前端的
// `dto.TeamworkBoardView` 的**唯一一条**路径。
//
// 抽成自由函数而不是留在快照方法里：写侧（存档刷新）必须产出与下发**同形**的载荷——
// 两处各拼一遍就是两份事实，重启恢复出的看板迟早和活体下发的不一样。装配读数
// （assemblies）因此是**参数**而不是在这里现算：算它需要宿主的插件域与工具面。
func BuildBoardView(
	plan store.TeamworkPlan,
	events []store.TeamworkEvent,
	bindings []store.TeamworkBinding,
	records []jobs.Record,
	maxMembers int,
	assemblies []dto.PluginAssemblyView,
) dto.TeamworkBoardView {
	view := dto.TeamworkBoardView{
		TeamID:     plan.TeamID,
		Version:    plan.Version,
		MaxMembers: maxMembers,
		// 闭板三字段从**计划**搬（域内权威，U3 裁决）：存档里的同名字段只是历史副本，
		// 这里读计划才让"关闭后仍看得到已关闭态"成立（存档对 closed 是整块退场）。
		State:        plan.State.State,
		ClosedAt:     plan.State.ClosedAt,
		ClosedReason: plan.State.ClosedReason,
		Members:      MemberViews(plan, events, records, assemblies),
		Milestones:   MilestoneViews(plan.Milestones),
		WorkItems:    WorkItemViews(plan, bindings, records),
		Jobs:         JobViews(records),
		Events:       EventViews(events),
	}
	view.Stale = ProjectionStale(plan.State, view.Jobs)
	return view
}

// PlanHasOrchestration 报告一份计划有没有**可看的编排**。
//
// 判据 = 有里程碑（阶段口径 2026-10-04 整条退场：计划里**没有** stages 这个概念了）。
// 里程碑是计划的最小形状（校验层也要求至少一份），所以"没有里程碑"就等于"没有计划"。
func PlanHasOrchestration(plan store.TeamworkPlan) bool {
	return len(plan.Milestones) > 0
}

// MemberViews 搬运在编成员。Permission 格子**不下发**：它是权责分配的实现细节，
// 面板要的是"这个人是 readonly 还是 readwrite"（tools_policy 已够）。
//
// Status / Queue / Messages 是 **Work Item 口径**的三件（2026-10-03）：状态由这个人名下
// 工作项折算（只有 running / free 两值），队列是"还没完成的工作项名称"（销项即出队），
// 消息是尾插进来的回执。
//
// Plugins / Assembly 是**装配两格**（2026-10-05）：声明面从计划搬（`members[].plugins`），
// 生效面取调用方算好的读数（走宿主那**同一条**装配面判据）。两格都**拷贝**而不是共享底层
// 数组：计划来自看板缓存（同一份计划会被多个快照与并发读者共享），让 DTO 指回缓存就是给
// "前端读到一半的写"留口子。
func MemberViews(plan store.TeamworkPlan, events []store.TeamworkEvent, records []jobs.Record, assemblies []dto.PluginAssemblyView) []dto.TeamworkMemberView {
	if len(plan.Members) == 0 {
		return nil
	}
	assemblyByRole := make(map[string]dto.PluginAssemblyView, len(assemblies))
	for _, assembly := range assemblies {
		assemblyByRole[assembly.Role] = assembly
	}
	views := make([]dto.TeamworkMemberView, 0, len(plan.Members))
	for _, member := range plan.Members {
		view := dto.TeamworkMemberView{
			Role:          member.Role,
			RoleSessionID: member.RoleSessionID,
			Worktree:      member.Worktree,
			ToolsPolicy:   member.ToolsPolicy,
			Status:        MemberStatus(plan, member),
			Plugins:       append([]string(nil), member.Plugins...),
		}
		if assembly, ok := assemblyByRole[member.Role]; ok {
			copied := assembly
			copied.Plugins = append([]string(nil), assembly.Plugins...)
			copied.PluginFaceMissing = append([]string(nil), assembly.PluginFaceMissing...)
			view.Assembly = &copied
		}
		view.CurrentSessionID, view.CurrentWorkItem = MemberCurrent(plan, member.Role)
		for _, milestone := range plan.Milestones {
			for _, item := range milestone.Items {
				if item.Role != member.Role {
					continue
				}
				if item.StatusOrPending() == store.TeamworkItemDone {
					continue // 已销项：不占队列
				}
				view.Queue = append(view.Queue, item.Name)
			}
		}
		for _, event := range events {
			if event.Kind != store.TeamworkEventMessage || event.Role != member.Role {
				continue
			}
			view.Messages = append(view.Messages, dto.TeamworkTeammateMessageView{
				At: event.At.Unix(), Role: event.Role, Milestone: event.Milestone,
				WorkItem: event.WorkItem, Text: event.Detail,
			})
		}
		views = append(views, view)
	}
	return views
}

// MemberStatus 把一个人名下工作项的状态折算成人的状态，**只有两个值**（2026-10-04 用户口径）：
//
//   - `running`：这个人名下有工作项**正在跑**（status=running）；
//   - `free`：其余一切（没派过活 / 活跑完了在等验收 / 已销项）。
//
// 为什么没有 done / review：这个字段回答的是"**这个人此刻在不在干活**"。一件事做完没
// 做完是工作项自己的 status 列，两处都写"完成"就是把同一个事实存两遍——而 teammate 的
// 状态一旦出现 done，看板上就会出现"人 done 了但队列里还有活"这种自相矛盾的一帧。
func MemberStatus(plan store.TeamworkPlan, member store.TeamworkMember) string {
	for _, milestone := range plan.Milestones {
		for _, item := range milestone.Items {
			if item.Role != member.Role {
				continue
			}
			if item.StatusOrPending() == store.TeamworkItemRunning {
				return MemberRunning
			}
		}
	}
	return MemberFree
}

// MemberCurrent 反解"这个人此刻那件事的会话"：优先在跑的工作项，其次等验收的，
// 都没有就退到它最近一次真开过工的工作项（计划顺序里最后一个带会话号的）。
//
// 为什么不能拿 RoleSessionID 顶替（2026-10-04 用户口径：看板点开的是"历史会话"而不是
// 当前的 teammate 的会话）：RoleSessionID 是**员工的长期会话**（跨工作项、跨轮次），
// 而"一 Work Item 一个 Session"的隔离意味着每一件事都有自己的会话号——面板要的是后者。
func MemberCurrent(plan store.TeamworkPlan, role string) (sessionID, workItem string) {
	var runningSession, runningItem string
	var reviewSession, reviewItem string
	var lastSession, lastItem string
	for _, milestone := range plan.Milestones {
		for _, item := range milestone.Items {
			if item.Role != role || strings.TrimSpace(item.SessionID) == "" {
				continue
			}
			switch item.StatusOrPending() {
			case store.TeamworkItemRunning:
				// 在跑的那件事优先（同一个人同时只跑一件事：派发按工作项去重）。
				runningSession, runningItem = item.SessionID, item.ID
			case store.TeamworkItemReview:
				// 等验收的次之：结论已经有了，但会话还是"这件事的会话"。
				if reviewSession == "" {
					reviewSession, reviewItem = item.SessionID, item.ID
				}
			default:
				// 其余（pending / done）：只当兜底——"这个人最近开过工的那件事"。
				lastSession, lastItem = item.SessionID, item.ID
			}
		}
	}
	switch {
	case runningSession != "":
		return runningSession, runningItem
	case reviewSession != "":
		return reviewSession, reviewItem
	default:
		return lastSession, lastItem
	}
}

// WorkItemViews 搬运工作项（甘特图的扁平数据面）。
//
// Live / Interrupted 都是**读出来的事实**：Live = 这件事有一份未释放的工作区绑定；
// Interrupted = 状态说在跑、而本进程作业表里查不到它的句柄（jobs I-4 的显式化——
// 进程重启后句柄必然作废，这不是错误，是"可以重派"的信号）。
func WorkItemViews(plan store.TeamworkPlan, bindings []store.TeamworkBinding, records []jobs.Record) []dto.TeamworkWorkItemView {
	liveBindings := store.TeamworkBindings(bindings)
	readings := make(map[string]JobReading, len(records))
	for _, record := range records {
		readings[string(record.Handle)] = JobReading{Alive: true, Running: record.Running()}
	}
	views := make([]dto.TeamworkWorkItemView, 0)
	for _, milestone := range plan.Milestones {
		for _, item := range milestone.Items {
			_, live := liveBindings[item.ID]
			// 卡点由编排面的**同一处判据**给出（ItemGateOf）：看板只是它的渲染面，
			// 不自己再推一遍"卡在哪"（审计 D7）。
			gate := ItemGateOf(plan, item, readings[item.Handle])
			views = append(views, dto.TeamworkWorkItemView{
				ID: item.ID, Milestone: item.Milestone, Role: item.Role, Name: item.Name,
				Description: item.Description, Goal: item.Goal,
				DependsOn: append([]string(nil), item.DependsOn...),
				Status:    item.StatusOrPending(), SessionID: item.SessionID, Worktree: item.Worktree,
				Handle: item.Handle, Note: item.Note,
				StartedAt: item.StartedAt, FinishedAt: item.FinishedAt,
				// Interrupted 与卡点是同一事实的两种渲染（判据仍是 ItemGateOf）。
				Live: live, Interrupted: gate == ItemGateJobInterrupted,
				Gate: string(gate), Next: gate.Next(),
			})
		}
	}
	if len(views) == 0 {
		return nil
	}
	return views
}

// MilestoneViews 搬运里程碑（after 是判据边，原样搬；status 空 = pending）。
func MilestoneViews(milestones []store.TeamworkMilestone) []dto.TeamworkMilestoneView {
	if len(milestones) == 0 {
		return nil
	}
	views := make([]dto.TeamworkMilestoneView, 0, len(milestones))
	for _, milestone := range milestones {
		views = append(views, dto.TeamworkMilestoneView{
			ID:        milestone.ID,
			Name:      milestone.Name,
			DependsOn: append([]string(nil), milestone.DependsOn...),
			Required:  append([]string(nil), milestone.Required...),
			Status:    milestone.Status,
			Content:   milestone.Content,
		})
	}
	return views
}

// JobViews 搬运作业行，并给出**权威归属**：Node = record.Node（派发时写的是
// 工作项 id / 里程碑 id），Role = 作用域主体 emp_<role> 反解。阶段口径已退场，作业行
// 不再有 stage 这一列（同一件事的归属只有一个：Node）。
func JobViews(records []jobs.Record) []dto.TeamworkJobView {
	if len(records) == 0 {
		return nil
	}
	views := make([]dto.TeamworkJobView, 0, len(records))
	for _, record := range records {
		subject := strings.TrimSpace(record.Scope.Subject)
		views = append(views, dto.TeamworkJobView{
			Handle:   string(record.Handle),
			State:    string(record.State),
			ExitCode: record.ExitCode,
			Bytes:    record.Bytes,
			Node:     record.Node,
			Role:     RoleFromSubject(subject),
			Scope:    dto.TeamworkJobScopeView{Subject: subject},
		})
	}
	return views
}

// RoleFromSubject 把 emp_<role> 反解成角色名；不是这个前缀就原样返回（不猜）。
func RoleFromSubject(subject string) string {
	if role, ok := strings.CutPrefix(subject, SubjectPrefix()); ok {
		return role
	}
	return subject
}

// EventViews 搬运审计流水；只取**最近** `BoardEventLimit` 条（保持文件顺序 = 发生顺序）。
func EventViews(events []store.TeamworkEvent) []dto.TeamworkEventView {
	if len(events) == 0 {
		return nil
	}
	recent := events
	if len(recent) > BoardEventLimit {
		recent = recent[len(recent)-BoardEventLimit:]
	}
	views := make([]dto.TeamworkEventView, 0, len(recent))
	for _, event := range recent {
		views = append(views, dto.TeamworkEventView{
			At:        event.At.Unix(),
			Kind:      event.Kind,
			Role:      event.Role,
			Handle:    event.Handle,
			Node:      event.Node,
			Milestone: event.Milestone,
			WorkItem:  event.WorkItem,
			Detail:    event.Detail,
		})
	}
	return views
}

// ProjectionStale 判定"计划里的句柄投影是否已经过期"（jobs I-4 的显式化）：
// 计划里记着句柄、而本进程的作业表里一个都查不到，说明这些句柄来自**上一个进程**。
//
// 它只是一个提示位：不改变任何判定，也不阻止渲染——看板该显示的是事实，
// 而不是"因为可能过期就不显示"。
func ProjectionStale(state store.TeamworkState, views []dto.TeamworkJobView) bool {
	if len(state.Jobs) == 0 {
		return false
	}
	live := make(map[string]struct{}, len(views))
	for _, view := range views {
		live[view.Handle] = struct{}{}
	}
	for _, handle := range state.Jobs {
		if _, ok := live[handle]; ok {
			return false
		}
	}
	return true
}
