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
	planMissing bool
	// archive 是活体给不出看板时的**存档兜底**（§6 重启恢复）：nil = 无可用存档
	// （没写过 / state=closed / 快照坏了）。它随同一份缓存条目一起取，而不是每次
	// 现读——绝大多数会话既没有计划也没有存档，负缓存的语义不能因为兜底退化成
	// "每次快照都去读一个不存在的存档文件"。
	archive *dto.TeamworkBoardView
}

// buildTeamworkBoardView 是把（计划 + 审计 + 作业行）组装成下发前端的
// dto.TeamworkBoardView 的**唯一一条**路径。
//
// 抽成自由函数而不是留在快照方法里：写侧（存档刷新，runtime_teamwork_board_archive.go）
// 必须产出与下发**同形**的载荷——两处各拼一遍就是两份事实，重启恢复出的看板迟早
// 和活体下发的不一样。
func buildTeamworkBoardView(plan sessionstore.TeamworkPlan, events []sessionstore.TeamworkEvent, records []jobs.Record, maxMembers int) dto.TeamworkBoardView {
	view := dto.TeamworkBoardView{
		TeamID:     plan.TeamID,
		Version:    plan.Version,
		MaxMembers: maxMembers,
		// 闭板三字段从**计划**搬（域内权威，U3 裁决）：存档里的同名字段只是历史副本，
		// 这里读计划才让"关闭后仍看得到已关闭态"成立（存档对 closed 是整块退场）。
		State:        plan.State.State,
		ClosedAt:     plan.State.ClosedAt,
		ClosedReason: plan.State.ClosedReason,
		Stages:       teamworkStageViews(plan.Stages),
		Members:      teamworkMemberViews(plan.Members),
		Milestones:   teamworkMilestoneViews(plan.Milestones),
		Jobs:         teamworkJobViews(records),
		Events:       teamworkEventViews(events),
	}
	view.Stale = teamworkProjectionStale(plan.State, view.Jobs)
	return view
}

// TeamworkBoardSnapshot 实现 contract.TeamworkBoardProjection：返回某会话的团队看板
// 只读投影。未装配 teamwork / 解析不出作用域 / 该会话尚无计划 → nil（前端与 TUI
// 据此整块退场，不留空壳）。
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
		// 读失败**不上抛**：快照路径上的错误没有消费者（面板该做的是退场，
		// 而不是把存储错误渗进会话快照）。缺计划与读失败在这里同解，并**进负缓存**。
		missing := err != nil
		var events []sessionstore.TeamworkEvent
		if !missing {
			events, err = backend.Store.ReadEvents(context.Background(), key)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				// 审计读不出来不该让整块看板消失：计划还在，就先只报计划。
				events = nil
			}
		}
		cached = teamworkBoardSnapshotCache{plan: plan, events: events, planMissing: missing}
		if missing || len(plan.Stages) == 0 {
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

	if cached.planMissing || len(cached.plan.Stages) == 0 {
		// 没有计划 / 没有阶段 = 活体给不出可看的编排：回落到存档快照（§6 重启恢复）。
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
	view := buildTeamworkBoardView(plan, cached.events, records, backend.MaxTeammates)
	return &view
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

// teamworkStageViews 搬运阶段（depends_on 是顺序的唯一事实，原样搬）。
func teamworkStageViews(stages []sessionstore.TeamworkStage) []dto.TeamworkStageView {
	if len(stages) == 0 {
		return nil
	}
	views := make([]dto.TeamworkStageView, 0, len(stages))
	for _, stage := range stages {
		views = append(views, dto.TeamworkStageView{
			ID:        stage.ID,
			Roles:     append([]string(nil), stage.Roles...),
			DependsOn: append([]string(nil), stage.DependsOn...),
		})
	}
	return views
}

// teamworkMemberViews 搬运在编成员。Permission 格子**不下发**：它是权责分配的实现细节，
// 面板要的是"这个人是 readonly 还是 readwrite"（tools_policy 已够）。
func teamworkMemberViews(members []sessionstore.TeamworkMember) []dto.TeamworkMemberView {
	if len(members) == 0 {
		return nil
	}
	views := make([]dto.TeamworkMemberView, 0, len(members))
	for _, member := range members {
		views = append(views, dto.TeamworkMemberView{
			Role:          member.Role,
			RoleSessionID: member.RoleSessionID,
			Worktree:      member.Worktree,
			ToolsPolicy:   member.ToolsPolicy,
		})
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
			ID:      milestone.ID,
			After:   append([]string(nil), milestone.After...),
			Status:  milestone.Status,
			Content: milestone.Content,
		})
	}
	return views
}

// teamworkJobViews 搬运作业行，并给出**权威归属**：Stage = record.Node（派发时写的是阶段 id），
// Role = 作用域主体 emp_<role> 反解。
//
// 渲染件里还有一条 stage → node → subject 的回落链，那是**接线前的过渡口径**；
// 这里把 stage/role 都填上，回落链就不会被走到。
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
			Stage:    record.Node,
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
			Stage:     event.Stage,
			Role:      event.Role,
			Handle:    event.Handle,
			Milestone: event.Milestone,
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
