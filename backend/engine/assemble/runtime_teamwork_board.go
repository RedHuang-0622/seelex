package assemble

// runtime_teamwork_board.go — 团队看板的**宿主面**：读哪些事实、缓存多久、装配读数怎么来。
//
// 落点沿革（P21-20「域方法随类型走」）：**投影与折算的判据**（成员 / 工作项 / 里程碑 / 作业 /
// 审计五份行的搬运、人的状态两值折算、当前会话反解、卡点渲染、句柄过期提示位）搬进团队域
// [`backend/multiagent/teamwork`](../../multiagent/teamwork/README.md) 的 `board_view.go`
// （plan §2.7 把 `runtime_teamwork*.go` 列为该域目标）；本文件只剩三件事：
//
//   - **文件读缓存**（计划 + 审计 + 绑定 + 存档兜底）：会话快照是高频路径，而这两份读是文件读；
//     作业行**不进缓存**——`jobs.Manager.Snapshot` 是内存读，缓存它只会制造陈旧；
//   - **失效点**：`invalidateTeamworkBoard`（team_* 工具成功返回后整表丢弃）；
//   - **装配读数**：`teamBoardAssemblies` 走 `r.assemblyViews`（与编排回执**同一条**装配面判据）。
//
// 生态位（契约 docs/arch/team-board-gui-tui-contract.md §3/§4）：事实三份（计划/审计落 store、
// 作业行在内存表）；本层**单向**只读，没有任何写入口。

import (
	"context"
	"errors"
	"io/fs"

	"github.com/RedHuang-0622/Seele/jobs"

	"github.com/RedHuang-0622/seelex/backend/multiagent/teamwork"
	"github.com/RedHuang-0622/seelex/contract/dto"
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
)

// 同值常量别名（事实源在域包，既有调用点与用例零改动）。
const teamworkBoardEventLimit = teamwork.BoardEventLimit

// 同身份别名：投影判据只有一份实现（域包 `board_view.go`），本层保留旧名供同包其它文件与
// 既有用例继续使用。
var (
	teamworkSubjectPrefix        = teamwork.SubjectPrefix
	buildTeamworkBoardView       = teamwork.BuildBoardViewWithLimits
	teamworkPlanHasOrchestration = teamwork.PlanHasOrchestration
	teamworkMemberViews          = teamwork.MemberViews
	teamworkMemberStatus         = teamwork.MemberStatus
	teamworkMemberCurrent        = teamwork.MemberCurrent
	teamworkWorkItemViews        = teamwork.WorkItemViews
	teamworkMilestoneViews       = teamwork.MilestoneViews
	teamworkJobViews             = teamwork.JobViews
	roleFromSubject              = teamwork.RoleFromSubject
	teamworkEventViews           = teamwork.EventViews
	teamworkProjectionStale      = teamwork.ProjectionStale
)

// teamworkBoardSnapshotCache 是「计划 + 审计」两份文件读的缓存值。
//
// 只缓存**文件读**这一层：计划是整份替换型 head，审计是追加型 jsonl，
// 两者的变化都只可能来自本进程的 team_* 工具调用（见 invalidateTeamworkBoard）。
//
// planMissing 是**负缓存**：绝大多数会话没有团队计划，而会话快照是高频路径——
// 不缓存"没有计划"就等于每次快照都去 stat 一个不存在的文件。失效同样由
// invalidateTeamworkBoard 负责，因此 team_plan 之后的第一次采集立刻能看到新计划。
type teamworkBoardSnapshotCache struct {
	plan        storeteamwork.Plan
	events      []storeteamwork.Event
	bindings    []storeteamwork.Binding
	planMissing bool
	// archive 是活体给不出看板时的**存档兜底**（§6 重启恢复）：nil = 无可用存档
	// （没写过 / state=closed / 快照坏了）。它随同一份缓存条目一起取，而不是每次
	// 现读——绝大多数会话既没有计划也没有存档，负缓存的语义不能因为兜底退化成
	// "每次快照都去读一个不存在的存档文件"。
	archive *dto.TeamworkBoardView
}

// teamBoardAssemblies 算一次下发/存档需要的逐成员装配读数。
//
// 读数走**已经存在的那一条判据**（r.assemblyViews → 装配面裁决）：看板不另写
// 一份"收窄算法"——另写一份就是第二个事实源，回执与看板迟早对同一个人给出两个工具面。
// nil Runtime（未装配桥）→ nil：成员行的 Assembly 留 nil，前端据此不显示装配格。
func (r *Runtime) teamBoardAssemblies(plan storeteamwork.Plan) []dto.PluginAssemblyView {
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
	// 作用域解析只有一份判据（runtime_teamwork_scope.go）；看板快照额外要求 Store。
	backend, manager, key, err := r.resolveTeamworkScope(sessionID)
	if err != nil || backend.Store == nil {
		return nil
	}
	r.teamworkMu.Lock()
	cache := r.teamworkBoardCache
	r.teamworkMu.Unlock()

	cached, hit := cache[key]
	if !hit {
		plan, err := backend.Store.ReadPlan(context.Background(), key)
		// 注：已收口的计划**不进存档兜底**——退场由下面的分支一处说了算，这里为真
		// 的唯一后果是"不必再去读一块注定被拒的存档"（收口时存档已封板）。
		// 读失败**不上抛**：快照路径上的错误没有消费者（面板该做的是退场，
		// 而不是把存储错误渗进会话快照）。缺计划与读失败在这里同解，并**进负缓存**。
		missing := err != nil
		var events []storeteamwork.Event
		var bindings []storeteamwork.Binding
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
			r.teamworkBoardCache = map[dto.SessionKey]teamworkBoardSnapshotCache{}
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
	if cached.plan.State.State == storeteamwork.StateClosed {
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
	view := buildTeamworkBoardView(plan, cached.events, cached.bindings, records, backend.MaxTeammates, teamworkJobLimit(backend.MaxJobs), r.teamBoardAssemblies(plan))
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
