package teamwork

// tool_shapes.go 是 leader 编排工具的**形状面**（P21-21「域方法随类型走」：从
// `backend/engine/assemble/runtime_teamwork_schema.go` 整份搬进本域；plan §2.7 把
// `runtime_teamwork*.go` 列为 `backend/multiagent/teamwork/` 的目标）。
//
// 描述与参数面向**提示词**：leader 的 skill（plugins/default/teamwork）按这里的形状调用，
// 因此 schema 既是准入面也是文档面。装配层只留一句注册（名字 + 描述 + schema + handler）。
//
// 门禁咬合：`test/tool_face_prompt_consistency_test.go` 从**注册面**扫工具名并与 leader 的 SKILL
// 对账——工具名常量因此声明在这里（注册点跨包引用它），扫描器按声明包名解析。

import "time"

const (
	// TeamPlanToolName 是 `team_plan` 的注册名（注册点跨包引用它）。
	TeamPlanToolName = "team_plan"
	// TeamDispatchToolName 是 `team_dispatch` 的注册名（注册点跨包引用它）。
	TeamDispatchToolName = "team_dispatch"
	// TeamWorkToolName 是 `team_work` 的注册名（注册点跨包引用它）。
	TeamWorkToolName = "team_work"
	// TeamItemToolName 是 `team_item` 的注册名（注册点跨包引用它）。
	TeamItemToolName = "team_item"
	// TeamAcceptToolName 是 `team_accept` 的注册名（注册点跨包引用它）。
	TeamAcceptToolName = "team_accept"
	// TeamFailToolName 是 `team_fail` 的注册名（注册点跨包引用它）。
	TeamFailToolName = "team_fail"
	// TeamRecoverToolName 是 `team_recover` 的注册名（注册点跨包引用它）。
	TeamRecoverToolName = "team_recover"
	// TeamItemsToolName 是 `team_items` 的注册名（注册点跨包引用它）。
	TeamItemsToolName = "team_items"
	// TeamJoinToolName 是 `team_join` 的注册名（注册点跨包引用它）。
	TeamJoinToolName = "team_join"
	// TeamMilestoneToolName 是 `team_milestone` 的注册名（注册点跨包引用它）。
	TeamMilestoneToolName = "team_milestone"
	// TeamCloseToolName 是 `team_close` 的注册名（注册点跨包引用它）。
	TeamCloseToolName = "team_close"
	// TeamContextToolName 是 `team_context` 的注册名（注册点跨包引用它）。
	TeamContextToolName = "team_context"
	TeamMessageToolName = "team_message"
)

func TeamMessageDescription() string {
	return "Message one teammate through its Work Item Session (role + item are required). " +
		"This appends one chat turn and waits up to wait_ms (default 30s); on timeout it returns the job handle. " +
		"It does not create work, change Work Item status, settle, accept, merge, or expose a long-lived role chat."
}

func TeamMessageSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"role":    map[string]interface{}{"type": "string", "description": "执行该 WorkItem 的在编 teammate"},
			"item":    map[string]interface{}{"type": "string", "description": "Work Item id；必填，用于定位这个 WorkItem 自己的持久 Session"},
			"message": map[string]interface{}{"type": "string", "description": "追加给 teammate 的一轮 user 输入"},
			"wait_ms": map[string]interface{}{
				"type": "integer", "minimum": 1, "maximum": int(TeammateMessageMaxWait / time.Millisecond),
				"description": "有界等待毫秒数；缺省 30000。超时返回 job handle，不取消这一轮对话",
			},
		},
		"required": []string{"role", "item", "message"},
	}
}

func TeamPlanDescription() string {
	return "Define or replace the team's hard orchestration plan (milestones, members and the work inside them). " +
		"Milestones are barriers and milestones[].depends_on is the order fact between them: a milestone may only be " +
		"arranged after every milestone it depends on is done, so you lay out one milestone at a time. Inside one " +
		"milestone the Work Items run in parallel following their own depends_on DAG (one Work Item = one teammate = " +
		"one Session + one git worktree). There are no stages: the stage-era shape is retired, and every order fact " +
		"lives in milestones[].depends_on plus work_items[].depends_on. " +
		"One role per teammate; built-in roles (main/user) are refused; the member ceiling is enforced. " +
		"A member may carry per-member execution params: max_turns caps that teammate's own role rounds " +
		"(omitted = the assembly default, 12). It rides the whole dispatch chain (member entry -> " +
		"WorkerRequest.MaxTurns -> role round session config); it is per-teammate, so tightening one does " +
		"not touch the others. " +
		"Anchors: docs/arch/teamwork-leader-worker-architecture.md §4.6."
}

func TeamPlanSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"team_id": map[string]interface{}{"type": "string", "description": "团队标识（同一团队的计划整份替换）"},
			"version": map[string]interface{}{"type": "integer", "minimum": 1, "description": "计划版本（缺省 1）"},
			"members": map[string]interface{}{
				"type":        "array",
				"description": "在编 teammate（一角色一 teammate，禁止重复）",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"role":            map[string]interface{}{"type": "string"},
						"role_session_id": map[string]interface{}{"type": "string", "description": "缺省由 (team_id, role) 派生"},
						"worktree":        map[string]interface{}{"type": "string", "description": "git worktree 指派名（缺省回退共享主工作区）"},
						"plugins": map[string]interface{}{
							"type": "array", "items": map[string]interface{}{"type": "string"},
							"description": "按会话插件装配（能力轴）：这个 teammate 用哪些插件，上限 limits.plugins.per_teammate（缺省 3），超限 / 未知名 / 重复**显式拒绝**（不静默忽略、不静默截断、不静默去重）；空/缺失 = 不覆盖（工具面继承宿主当前装配 + 技能目录不注入）。**只收窄权限面、不相对宿主装配**：装配集合替换宿主那一份收窄，可以比它更宽（声明一个 include/exclude 皆空的插件就等于拿到全工具面），也可以比它更窄；权限面与它相交后只会更小。对已开着的角色会话自下一轮生效",
						},
						"max_turns": map[string]interface{}{
							"type": "integer", "minimum": 1,
							"description": "这个 teammate 自己的角色回合上限（可选）。缺省 = 装配层默认（12）。它是「一个工作单元自己的执行参数」在 teammate 层的落点：随载荷走完整条透传链，派发时带了、执行时不许丢；给紧了不会影响别的成员。",
						},
						"tools_policy": map[string]interface{}{"type": "string", "description": "权责档 readonly|readwrite（缺省继承宿主默认）"},
						"permission_groups": map[string]interface{}{
							"type": "object", "description": "路由组 → 位（逐格分配，优先于档位）",
							"additionalProperties": map[string]interface{}{"type": "integer"},
						},
					},
					"required": []string{"role"},
				},
			},
			"milestones": map[string]interface{}{
				"type":        "array",
				"description": "里程碑：屏障（depends_on 是里程碑之间的顺序唯一事实）；里程碑内的工作项按各自 depends_on 并行",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id":   map[string]interface{}{"type": "string", "description": "里程碑 id（计划内唯一）"},
						"name": map[string]interface{}{"type": "string", "description": "里程碑名称"},
						"depends_on": map[string]interface{}{
							"type": "array", "items": map[string]interface{}{"type": "string"},
							"description": "前置里程碑 id（屏障：未 done 的里程碑不进入可排活）",
						},
						"required": map[string]interface{}{
							"type": "array", "items": map[string]interface{}{"type": "string"},
							"description": "这个里程碑需要哪些在编角色（只做校验，不参与顺序判定）",
						},
					},
					"required": []string{"id"},
				},
			},
		},
		"required": []string{"team_id", "members", "milestones"},
	}
}

func TeamDispatchDescription() string {
	return "Dispatch one teammate's job and return immediately with a handle (never waits). Refuses an unenrolled role " +
		"and refuses to exceed the teammate ceiling (explicitly, never silently queued); a repeated dispatch of the same " +
		"role collapses onto the job already running."
}

func TeamDispatchSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"role": map[string]interface{}{"type": "string", "description": "在编角色名（须已在 team_plan 的 members 里）"},
			"item": map[string]interface{}{"type": "string", "description": "Work Item id（给了它就按甘特节点派发：屏障与依赖闸门、一 Work Item 一套 Session + worktree 都由编排面管；与 role 二选一）"},
			"goal": map[string]interface{}{"type": "string", "description": "本轮交给该 teammate 的工作正文（也是作业行标题）"},
			"workspace": map[string]interface{}{
				"type": "string", "enum": []string{"worktree", "main"},
				"description": "工作区策略；缺省 worktree。main 只能在用户批准 worktree 创建失败后显式重试使用，并会标记为“主工作区（已批准）”。",
			},
		},
	}
}

func TeamWorkDescription() string {
	return "Arrange the work INSIDE one milestone: append work items (name / role / description / goal / depends_on). " +
		"Milestones are barriers — a milestone may only be arranged after every milestone it depends on is done, " +
		"so you plan one milestone at a time instead of laying out the whole run up front. depends_on is a within-milestone " +
		"DAG (the V-model edge exec -> test_case lives here). One work item = one teammate = one Session + one git worktree."
}

func TeamWorkSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"milestone": map[string]interface{}{"type": "string", "description": "里程碑 id（须已在 team_plan 的 milestones 里）"},
			"items": map[string]interface{}{
				"type":        "array",
				"description": "工作项：名称 / 描述 / 达成目标 / 执行 teammate / 里程碑内依赖",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id":          map[string]interface{}{"type": "string", "description": "工作项 id（整份计划唯一；依赖与验收都以它为准）"},
						"role":        map[string]interface{}{"type": "string", "description": "执行它的 teammate（须在编）"},
						"name":        map[string]interface{}{"type": "string", "description": "工作名称"},
						"description": map[string]interface{}{"type": "string", "description": "工作描述"},
						"goal":        map[string]interface{}{"type": "string", "description": "达成目标（验收判据的正文）"},
						"depends_on":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "同里程碑内的前置工作项 id"},
					},
					"required": []string{"id", "role", "name"},
				},
			},
		},
		"required": []string{"milestone", "items"},
	}
}

func TeamItemDescription() string {
	return "Adjust one work item that has NOT started yet (role / name / description / goal / depends_on). " +
		"Started work (running / review) and finished work (done / failed) are settled facts — adjust them by writing a new " +
		"item instead, never by rewriting history."
}

func TeamItemSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id":          map[string]interface{}{"type": "string", "description": "工作项 id"},
			"role":        map[string]interface{}{"type": "string", "description": "改派给谁（留空 = 不动）"},
			"name":        map[string]interface{}{"type": "string"},
			"description": map[string]interface{}{"type": "string"},
			"goal":        map[string]interface{}{"type": "string"},
			"depends_on":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		},
		"required": []string{"id"},
	}
}

func TeamAcceptDescription() string {
	return "Accept one work item after your review (status -> done). Acceptance also ends that item's execution isolation: " +
		"its git worktree is released, but its Session contents are NOT cleared. Session cleanup happens only at team_close. " +
		"A work item whose upstream dependencies are not done cannot be dispatched, so acceptance is " +
		"also what opens the next step of a within-milestone DAG. " +
		"Acceptance is the LAST link of the settle chain — merge worktree -> enqueue receipt -> status -> your review -> accept — " +
		"so a work item whose job is still running is refused: releasing its worktree mid-flight would race the automatic merge. " +
		"Wait for its receipt (team_items) or kill the job first."
}

func TeamAcceptSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id":   map[string]interface{}{"type": "string", "description": "工作项 id"},
			"note": map[string]interface{}{"type": "string", "description": "验收结论（写回工作项，看板直接显示）"},
		},
		"required": []string{"id"},
	}
}

func TeamFailDescription() string {
	return "Rule one work item as failed after your review. The worktree現場 and the Session memory are kept, so re-dispatching " +
		"the same item resumes in the SAME Session (this is what makes the interrupted-run memory survive)."
}

func TeamFailSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id":   map[string]interface{}{"type": "string", "description": "工作项 id"},
			"note": map[string]interface{}{"type": "string", "description": "判失败的理由（写回工作项）"},
		},
		"required": []string{"id"},
	}
}

func TeamRecoverDescription() string {
	return "Recover after an interruption (quota exhaustion / process restart): read the plan and the worktree binding ledger back, " +
		"and make explicit which work items are re-dispatchable (their job handle only lives in memory, so a restarted process " +
		"cannot see it). It touches NOTHING: sessions and worktrees stay alive so the memory and the现场 survive — that is why " +
		"re-dispatching reuses the same Session id."
}

func TeamRecoverSchema() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
}

func TeamItemsDescription() string {
	return "Read the work items (the gantt data): status, owner, milestone, dependencies, live binding (Session + worktree) and " +
		"whether a running item is interrupted (its handle is gone from this process's job table). " +
		"Each item also carries `gate` (where it is stuck right now: gate:依赖未 done / job:running / " +
		"job:句柄已作废（可重派） / merge:改动未合并待处置 / review:等你裁决 / failed:待人工处置 / " +
		"ready:可派发 / done:已验收) and `next` (the action that clears that gate) — read them instead of " +
		"inferring progress from the status yourself."
}

func TeamItemsSchema() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
}

func TeamJoinDescription() string {
	return "Bounded join: wait up to budget_ms for the given handles, then return their read-only records. " +
		"Use it only at a real dependency point; it observes but never consumes output (that is jobs_manage op=fetch)."
}

func TeamJoinSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"handles":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"budget_ms": map[string]interface{}{"type": "integer", "minimum": 0, "description": "等待上限（毫秒；缺省/0 = 5000）"},
		},
		"required": []string{"handles"},
	}
}

func TeamMilestoneDescription() string {
	return "Declare a milestone and write its content. The predicate is a dependency edge, not wall time: every stage in " +
		"`after` must have dispatched at least one teammate."
}

func TeamMilestoneSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id":      map[string]interface{}{"type": "string"},
			"content": map[string]interface{}{"type": "string", "description": "里程碑内容（由 leader 撰写）"},
		},
		"required": []string{"id", "content"},
	}
}

func TeamCloseDescription() string {
	return "Close the whole team (team_close): every enrolled member runs the same four close steps (reclaim its jobs → " +
		"release its worktree(s) → clear its session contents → keep it on the roster) — this is the ONE place that reclaims " +
		"jobs, so job output stays on the table until here — then the team board is sealed (closed/team.close), " +
		"the plan is marked closed and a close audit line is written. Idempotent: a second call returns already_closed=true " +
		"and neither re-seals nor re-audits. Takes no arguments: closure is a team-level act — it is the ONLY way a round ends " +
		"(the old per-member exit tool has been removed). " +
		"Refused while any work item is unsettled (running / awaiting review / failed): closing is the one place that tears down every " +
		"per-item worktree, so settle the account first — team_accept (or team_fail) an item awaiting review, wait for a running item's " +
		"receipt (or kill the job), and resolve a failed item's worktree by hand (conflicts, rebase, merge) before accepting it."
}

func TeamCloseSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func TeamContextDescription() string {
	return "Read each member's work context (roster entry + its job row + optional body) — a READ-ONLY surface: it " +
		"never dispatches, reclaims or consumes. Body is read with a NON-consuming read (jobs.Manager.Peek): the cursor " +
		"does not advance and a terminal job is not retired, so 'take a look at a member' never turns unread content into " +
		"read. Body is off by default and bounded per member (default 4KB, hard cap 32KB)."
}

func TeamContextSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"roles": map[string]interface{}{
				"type": "array", "items": map[string]interface{}{"type": "string"},
				"description": "只看这些角色（缺省 = 全部在编成员）",
			},
			"include_body": map[string]interface{}{
				"type":        "boolean",
				"description": "是否取作业正文（缺省 false；取法是非消费读，不推进游标）",
			},
			"max_bytes": map[string]interface{}{
				"type": "integer", "minimum": 1, "maximum": 32768,
				"description": "逐成员正文预算（字节；缺省 4096，上限 32768）",
			},
		},
	}
}
