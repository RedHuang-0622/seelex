package seelebridge

// runtime_teamwork_board.go — 团队看板的**只读投影面**（计划 + 作业行 + 审计流水）。
//
// 生态位（契约 docs/arch/team-board-gui-tui-contract.md §3/§4）：
//
//   - 事实三份：计划与审计落 sessionstore 的 moduleTeamwork（文件），作业行活在
//     jobs.Manager 的内存表里（jobs I-4：句柄只在内存，进程重启即作废）。
//   - 本文件只做**搬运**：拓扑、阶段状态折算、层号全部由渲染件
//     （gui/frontend/dist/team-board-view.js）的纯函数做——投影层再算一遍就是第二份事实。
//   - **单向**：只有后端 → 前端。这里没有任何写入口，渲染结果不回写。
//
// 采集性能（§4 硬要求）：会话快照是高频路径，因此「计划 + 审计」这两份**文件读**
// 按会话作用域缓存，在 team_* 工具成功返回后失效；**作业行不进缓存**——
// jobs.Manager.Snapshot 是内存读，缓存它只会制造陈旧。

import (
	"context"
	"errors"
	"io/fs"
	"strings"

	"github.com/RedHuang-0622/Seele/jobs"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/teamwork"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// teamworkBoardEventLimit 是审计投影的条数上限：审计是**只追加**的流水，
// 无界下发就是让快照载荷随派发次数线性增长。看板只需要"最近发生了什么"。
const teamworkBoardEventLimit = 32

// teamworkSubjectPrefix 与 teamwork.SubjectForRole 同源（emp_<role>）；
// 这里用它把作业行的作用域主体反解回角色名。前缀不写字面量——写死一份就是
// 两处定义，改一处漏一处（`SubjectForRole("")` 恰好只返回前缀）。
func teamworkSubjectPrefix() string { return teamwork.SubjectForRole("") }

// teamworkBoardSnapshotCache 是「计划 + 审计」两份文件读的缓存值。
//
// 只缓存**文件读**这一层：计划是整份替换型 head，审计是追加型 jsonl，
// 两者的变化都只可能来自本进程的 team_* 工具调用（见 invalidateTeamworkBoard）。
//
// planMissing 是**负缓存**：绝大多数会话没有团队计划，而会话快照是高频路径——
// 不缓存"没有计划"就等于每次快照都去 stat 一个不存在的文件。失效同样由
// invalidateTeamworkBoard 负责，因此 team_plan 之后的第一次采集立刻能看到新计划。
type teamworkBoardSnapshotCache struct {
	plan        sessionstore.TeamworkPlan
	events      []sessionstore.TeamworkEvent
	bindings    []sessionstore.TeamworkBinding
	planMissing bool
	// archive 是活体给不出看板时的**存档兜底**（§6 重启恢复）：nil = 无可用存档
	// （没写过 / state=closed / 快照坏了）。它随同一份缓存条目一起取，而不是每次
	// 现读——绝大多数会话既没有计划也没有存档，负缓存的语义不能因为兜底退化成
	// "每次快照都去读一个不存在的存档文件"。
	archive *dto.TeamworkBoardView
}

// buildTeamworkBoardView 是把（计划 + 审计 + 作业行 + 装配读数）组装成下发前端的
// dto.TeamworkBoardView 的**唯一一条**路径。
//
// 抽成自由函数而不是留在快照方法里：写侧（存档刷新，runtime_teamwork_board_archive.go）
// 必须产出与下发**同形**的载荷——两处各拼一遍就是两份事实，重启恢复出的看板迟早
// 和活体下发的不一样。装配读数（assemblies）因此是**参数**而不是在这里现算：算它需要
// Runtime 的插件域与工具面，自由函数手里没有；三个调用方各自把 r.assemblyViews 的结果
// 传进来，走的是同一条判据。
func buildTeamworkBoardView(plan sessionstore.TeamworkPlan, events []sessionstore.TeamworkEvent, bindings []sessionstore.TeamworkBinding, records []jobs.Record, maxMembers int, assemblies []dto.PluginAssemblyView) dto.TeamworkBoardView {
	view := dto.TeamworkBoardView{
		TeamID:     plan.TeamID,
		Version:    plan.Version,
		MaxMembers: maxMembers,
		// 闭板三字段从**计划**搬（域内权威，U3 裁决）：存档里的同名字段只是历史副本，
		// 这里读计划才让"关闭后仍看得到已关闭态"成立（存档对 closed 是整块退场）。
		State:        plan.State.State,
		ClosedAt:     plan.State.ClosedAt,
		ClosedReason: plan.State.ClosedReason,
		Members:      teamworkMemberViews(plan, events, records, assemblies),
		Milestones:   teamworkMilestoneViews(plan.Milestones),
		WorkItems:    teamworkWorkItemViews(plan, bindings, records),
		Jobs:         teamworkJobViews(records),
		Events:       teamworkEventViews(events),
	}
	view.Stale = teamworkProjectionStale(plan.State, view.Jobs)
	return view
}

// teamBoardAssemblies 算一次下发/存档需要的逐成员装配读数。
//
// 读数走**已经存在的那一条判据**（r.assemblyViews → pluginFaceJudgement）：看板不另写
// 一份"收窄算法"——另写一份就是第二个事实源，回执与看板迟早对同一个人给出两个工具面。
// nil Runtime（未装配桥）→ nil：成员行的 Assembly 留 nil，前端据此不显示装配格。
func (r *Runtime) teamBoardAssemblies(plan sessionstore.TeamworkPlan) []dto.PluginAssemblyView {
	if r == nil || len(plan.Members) == 0 {
		return nil
	}
	return r.assemblyViews(plan.Members)
}

// TeamworkBoardSnapshot 实现 contract.TeamworkBoardProjection：返回某会话的团队看板
// 只读投影。未装配 teamwork / 解析不出作用域 / 该会话尚无计划 / **计划已收口** → nil
// （前端与 TUI 据此整块退场，不留空壳；"结束就是没有了"）。
func (r *Runtime) TeamworkBoardSnapshot(sessionID string) *dto.TeamworkBoardView {
	if r == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}

	r.teamworkMu.Lock()
	backend, manager := r.teamworkBackend, r.teamworkJobs
	cache := r.teamworkBoardCache
	r.teamworkMu.Unlock()
	if backend == nil || backend.Store == nil || backend.KeyFor == nil {
		return nil
	}
	key, ok := backend.KeyFor(sessionID)
	if !ok {
		return nil
	}

	cached, hit := cache[key]
	if !hit {
		plan, err := backend.Store.ReadPlan(context.Background(), key)
		// 注：已收口的计划**不进存档兜底**——退场由下面的分支一处说了算，这里为真
		// 的唯一后果是"不必再去读一块注定被拒的存档"（收口时存档已封板）。
		// 读失败**不上抛**：快照路径上的错误没有消费者（面板该做的是退场，
		// 而不是把存储错误渗进会话快照）。缺计划与读失败在这里同解，并**进负缓存**。
		missing := err != nil
		var events []sessionstore.TeamworkEvent
		var bindings []sessionstore.TeamworkBinding
		if !missing {
			events, err = backend.Store.ReadEvents(context.Background(), key)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				// 审计读不出来不该让整块看板消失：计划还在，就先只报计划。
				events = nil
			}
			// 绑定账本（一个 Work Item 一个 Session + worktree）：读不出来同理——
			// 看板少一列"现场在不在"，但不该整块消失。
			bindings, err = backend.Store.ReadBindings(context.Background(), key)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				bindings = nil
			}
		}
		cached = teamworkBoardSnapshotCache{plan: plan, events: events, bindings: bindings, planMissing: missing}
		if missing || !teamworkPlanHasOrchestration(plan) {
			// 活体给不出看板：同一次采集里把存档兜底也取回来（见 cache.archive 的说明）。
			cached.archive = r.readTeamBoardArchive(backend, key)
		}
		r.teamworkMu.Lock()
		if r.teamworkBoardCache == nil {
			r.teamworkBoardCache = map[sessionstore.Key]teamworkBoardSnapshotCache{}
		}
		r.teamworkBoardCache[key] = cached
		r.teamworkMu.Unlock()
	}

	// 已收口（team_close）→ 看板**整块退场**，口径同"没有计划"：结束就是没有了。
	//
	// 为什么 retiring 而不是"照常出图、前端写已关闭"：看板是**在册编排**的只读投影，
	// 收口之后没有在册编排可看；把已收口的计划继续画成一块看板，会让收口后的在编名册
	// （成员行）残留在面板上，读侧分不清"还在跑"与"早收口了"（2026-10-03 现场）。
	// 域内 closed 事实归计划（plan.State），它管的是"还在不在册"；投影这一侧只回答
	// "有没有在册编排可看"。存档侧本来就是同一口径（readTeamBoardArchive 只恢复 active），
	// 这里改完，活体与存档不再自相矛盾。
	if cached.plan.State.State == sessionstore.TeamworkStateClosed {
		return nil
	}

	if cached.planMissing || !teamworkPlanHasOrchestration(cached.plan) {
		// 没有计划 / 没有可看的编排：回落到存档快照（§6 重启恢复）。
		// 仍然是 nil 就表示"看板退场"（无存档 / 存档已 closed / 快照坏了）——
		// 与渲染件的空计划口径一致，不留空壳。
		if cached.archive == nil {
			return nil
		}
		// 浅拷贝后返回：缓存里那一份是共享的，不能让消费者的字段写入污染缓存
		// （活体路径每次新建 view，这里保持同一口径）。
		recovered := *cached.archive
		return &recovered
	}

	plan := cached.plan
	var records []jobs.Record
	if manager != nil {
		records = manager.Snapshot(jobs.Scope{Session: key.SessionID})
	}
	view := buildTeamworkBoardView(plan, cached.events, cached.bindings, records, backend.MaxTeammates, r.teamBoardAssemblies(plan))
	return &view
}

// teamworkPlanHasOrchestration 报告一份计划有没有**可看的编排**。
//
// 判据 = 有里程碑（阶段口径 2026-10-04 整条退场：计划里**没有** stages 这个概念了）。
// 里程碑是计划的最小形状（校验层也要求至少一份），所以"没有里程碑"就等于"没有计划"。
func teamworkPlanHasOrchestration(plan sessionstore.TeamworkPlan) bool {
	return len(plan.Milestones) > 0
}

// invalidateTeamworkBoard 丢弃看板缓存（team_* 工具成功返回后调用）。
//
// 整表丢弃而不是按 key 删：看板缓存只省文件读，重建成本是"每次 team_* 之后
// 一次计划读 + 一次审计读"，而按 key 精确失效要在 5 个 handler 里各传一遍作用域，
// 漏一处就是**静默陈旧**（面板永远显示旧计划）。宁可多读一次。
func (r *Runtime) invalidateTeamworkBoard() {
	if r == nil {
		return
	}
	r.teamworkMu.Lock()
	r.teamworkBoardCache = nil
	r.teamworkMu.Unlock()
}

// teamworkMemberViews 搬运在编成员。Permission 格子**不下发**：它是权责分配的实现细节，
// 面板要的是"这个人是 readonly 还是 readwrite"（tools_policy 已够）。
//
// Status / Queue / Messages 是 **Work Item 口径**的三件（2026-10-03）：状态由这个人名下
// 工作项折算（只有 running / free 两值），队列是"还没完成的工作项名称"（销项即出队），
// 消息是尾插进来的回执。
//
// Plugins / Assembly 是**装配两格**（2026-10-05）：声明面从计划搬（`members[].plugins`），
// 生效面取调用方算好的读数（r.teamBoardAssemblies → 同一条 pluginFaceJudgement 判据）。
// 两格都**拷贝**而不是共享底层数组：计划来自看板缓存（同一份计划会被多个快照与并发读者
// 共享），让 DTO 指回缓存就是给"前端读到一半的写"留口子。
func teamworkMemberViews(plan sessionstore.TeamworkPlan, events []sessionstore.TeamworkEvent, records []jobs.Record, assemblies []dto.PluginAssemblyView) []dto.TeamworkMemberView {
	if len(plan.Members) == 0 {
		return nil
	}
	liveHandles := make(map[string]struct{}, len(records))
	for _, record := range records {
		liveHandles[string(record.Handle)] = struct{}{}
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
			Status:        teamworkMemberStatus(plan, member),
			Plugins:       append([]string(nil), member.Plugins...),
		}
		if assembly, ok := assemblyByRole[member.Role]; ok {
			copied := assembly
			copied.Plugins = append([]string(nil), assembly.Plugins...)
			copied.PluginFaceMissing = append([]string(nil), assembly.PluginFaceMissing...)
			view.Assembly = &copied
		}
		view.CurrentSessionID, view.CurrentWorkItem = teamworkMemberCurrent(plan, member.Role)
		for _, milestone := range plan.Milestones {
			for _, item := range milestone.Items {
				if item.Role != member.Role {
					continue
				}
				if item.StatusOrPending() == sessionstore.TeamworkItemDone {
					continue // 已销项：不占队列
				}
				view.Queue = append(view.Queue, item.Name)
			}
		}
		for _, event := range events {
			if event.Kind != sessionstore.TeamworkEventMessage || event.Role != member.Role {
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

// teamworkMemberStatus 把一个人名下工作项的状态折算成人的状态，**只有两个值**
// （2026-10-04 用户口径）：
//
//   - `running`：这个人名下有工作项**正在跑**（status=running）；
//   - `free`：其余一切（没派过活 / 活跑完了在等验收 / 已销项）。
//
// 为什么没有 done / review：这个字段回答的是"**这个人此刻在不在干活**"。一件事做完没
// 做完是工作项自己的 status 列，两处都写"完成"就是把同一个事实存两遍——而 teammate 的
// 状态一旦出现 done，看板上就会出现"人 done 了但队列里还有活"这种自相矛盾的一帧。
func teamworkMemberStatus(plan sessionstore.TeamworkPlan, member sessionstore.TeamworkMember) string {
	for _, milestone := range plan.Milestones {
		for _, item := range milestone.Items {
			if item.Role != member.Role {
				continue
			}
			if item.StatusOrPending() == sessionstore.TeamworkItemRunning {
				return teamworkMemberRunning
			}
		}
	}
	return teamworkMemberFree
}

const (
	// teamworkMemberRunning / teamworkMemberFree 是 teammate 状态的**全部取值**。
	teamworkMemberRunning = "running"
	teamworkMemberFree    = "free"
)

// teamworkMemberCurrent 反解"这个人此刻那件事的会话"：优先在跑的工作项，其次等验收的，
// 都没有就退到它最近一次真开过工的工作项（计划顺序里最后一个带会话号的）。
//
// 为什么不能拿 RoleSessionID 顶替（2026-10-04 用户口径：看板点开的是"历史会话"而不是
// 当前的 teammate 的会话）：RoleSessionID 是**员工的长期会话**（跨工作项、跨轮次），
// 而"一 Work Item 一个 Session"的隔离意味着每一件事都有自己的会话号——面板要的是后者。
func teamworkMemberCurrent(plan sessionstore.TeamworkPlan, role string) (sessionID, workItem string) {
	var runningSession, runningItem string
	var reviewSession, reviewItem string
	var lastSession, lastItem string
	for _, milestone := range plan.Milestones {
		for _, item := range milestone.Items {
			if item.Role != role || strings.TrimSpace(item.SessionID) == "" {
				continue
			}
			switch item.StatusOrPending() {
			case sessionstore.TeamworkItemRunning:
				// 在跑的那件事优先（同一个人同时只跑一件事：派发按工作项去重）。
				runningSession, runningItem = item.SessionID, item.ID
			case sessionstore.TeamworkItemReview:
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

// teamworkWorkItemViews 搬运工作项（甘特图的扁平数据面）。
//
// Live / Interrupted 都是**读出来的事实**：Live = 这件事有一份未释放的工作区绑定；
// Interrupted = 状态说在跑、而本进程作业表里查不到它的句柄（jobs I-4 的显式化——
// 进程重启后句柄必然作废，这不是错误，是"可以重派"的信号）。
func teamworkWorkItemViews(plan sessionstore.TeamworkPlan, bindings []sessionstore.TeamworkBinding, records []jobs.Record) []dto.TeamworkWorkItemView {
	liveBindings := sessionstore.TeamworkBindings(bindings)
	liveHandles := make(map[string]struct{}, len(records))
	for _, record := range records {
		liveHandles[string(record.Handle)] = struct{}{}
	}
	views := make([]dto.TeamworkWorkItemView, 0)
	for _, milestone := range plan.Milestones {
		for _, item := range milestone.Items {
			_, live := liveBindings[item.ID]
			interrupted := false
			if item.StatusOrPending() == sessionstore.TeamworkItemRunning {
				_, alive := liveHandles[item.Handle]
				interrupted = !alive || item.Handle == ""
			}
			views = append(views, dto.TeamworkWorkItemView{
				ID: item.ID, Milestone: item.Milestone, Role: item.Role, Name: item.Name,
				Description: item.Description, Goal: item.Goal,
				DependsOn: append([]string(nil), item.DependsOn...),
				Status:    item.StatusOrPending(), SessionID: item.SessionID, Worktree: item.Worktree,
				Handle: item.Handle, Note: item.Note,
				StartedAt: item.StartedAt, FinishedAt: item.FinishedAt,
				Live: live, Interrupted: interrupted,
			})
		}
	}
	if len(views) == 0 {
		return nil
	}
	return views
}

// teamworkMilestoneViews 搬运里程碑（after 是判据边，原样搬；status 空 = pending）。
func teamworkMilestoneViews(milestones []sessionstore.TeamworkMilestone) []dto.TeamworkMilestoneView {
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

// teamworkJobViews 搬运作业行，并给出**权威归属**：Node = record.Node（派发时写的是
// 工作项 id / 里程碑 id），Role = 作用域主体 emp_<role> 反解。阶段口径已退场，作业行
// 不再有 stage 这一列（同一件事的归属只有一个：Node）。
func teamworkJobViews(records []jobs.Record) []dto.TeamworkJobView {
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
			Role:     roleFromSubject(subject),
			Scope:    dto.TeamworkJobScopeView{Subject: subject},
		})
	}
	return views
}

// roleFromSubject 把 emp_<role> 反解成角色名；不是这个前缀就原样返回（不猜）。
func roleFromSubject(subject string) string {
	if role, ok := strings.CutPrefix(subject, teamworkSubjectPrefix()); ok {
		return role
	}
	return subject
}

// teamworkEventViews 搬运审计流水；只取**最近** teamworkBoardEventLimit 条（保持文件顺序 = 发生顺序）。
func teamworkEventViews(events []sessionstore.TeamworkEvent) []dto.TeamworkEventView {
	if len(events) == 0 {
		return nil
	}
	recent := events
	if len(recent) > teamworkBoardEventLimit {
		recent = recent[len(recent)-teamworkBoardEventLimit:]
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

// teamworkProjectionStale 判定"计划里的句柄投影是否已经过期"（jobs I-4 的显式化）：
// 计划里记着句柄、而本进程的作业表里一个都查不到，说明这些句柄来自**上一个进程**。
//
// 它只是一个提示位：不改变任何判定，也不阻止渲染——看板该显示的是事实，
// 而不是"因为可能过期就不显示"。
func teamworkProjectionStale(state sessionstore.TeamworkState, views []dto.TeamworkJobView) bool {
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
