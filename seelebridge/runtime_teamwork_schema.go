package seelebridge

// runtime_teamwork_schema.go — leader 编排工具的输入契约（JSON Schema）。
//
// 描述与参数面向**提示词**：leader 的 skill（plugins/default/teamwork）按这里的
// 形状调用，因此 schema 既是准入面也是文档面。

func teamworkPlanDescription() string {
	return "Define or replace the team's hard orchestration plan (stage order, members, milestones). " +
		"Order is the single fact carried by stages[].depends_on; one role per teammate; built-in roles " +
		"(main/user) are refused; the member ceiling is enforced. Anchors: docs/arch/teamwork-leader-worker-architecture.md §4.6."
}

func teamworkPlanSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"team_id": map[string]interface{}{"type": "string", "description": "团队标识（同一团队的计划整份替换）"},
			"version": map[string]interface{}{"type": "integer", "minimum": 1, "description": "计划版本（缺省 1）"},
			"stages": map[string]interface{}{
				"type":        "array",
				"description": "编排阶段；depends_on 是顺序/依赖的唯一事实",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id":         map[string]interface{}{"type": "string"},
						"roles":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
						"depends_on": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					},
					"required": []string{"id", "roles"},
				},
			},
			"members": map[string]interface{}{
				"type":        "array",
				"description": "在编 teammate（一角色一 teammate，禁止重复）",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"role":            map[string]interface{}{"type": "string"},
						"role_session_id": map[string]interface{}{"type": "string", "description": "缺省由 (team_id, role) 派生"},
						"worktree":        map[string]interface{}{"type": "string", "description": "git worktree 指派名（缺省回退共享主工作区）"},
						"tools_policy":    map[string]interface{}{"type": "string", "description": "权责档 readonly|readwrite（缺省继承宿主默认）"},
						"permission_groups": map[string]interface{}{
							"type": "object", "description": "路由组 → 位（逐格分配，优先于档位）",
							"additionalProperties": map[string]interface{}{"type": "integer"},
						},
					},
					"required": []string{"role"},
				},
			},
			"milestones": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id":       map[string]interface{}{"type": "string"},
						"after":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
						"required": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					},
					"required": []string{"id"},
				},
			},
		},
		"required": []string{"team_id", "stages", "members"},
	}
}

func teamworkDispatchDescription() string {
	return "Dispatch one teammate's job and return immediately with a handle (never waits). Refuses an unenrolled role " +
		"and refuses to exceed the teammate ceiling (explicitly, never silently queued); a repeated dispatch of the same " +
		"role collapses onto the job already running."
}

func teamworkDispatchSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"role": map[string]interface{}{"type": "string", "description": "在编角色名（须已在 team_plan 的 members 里）"},
			"goal": map[string]interface{}{"type": "string", "description": "本轮交给该 teammate 的工作正文（也是作业行标题）"},
		},
		"required": []string{"role", "goal"},
	}
}

func teamworkJoinDescription() string {
	return "Bounded join: wait up to budget_ms for the given handles, then return their read-only records. " +
		"Use it only at a real dependency point; it observes but never consumes output (that is jobs_manage op=fetch)."
}

func teamworkJoinSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"handles":   map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"budget_ms": map[string]interface{}{"type": "integer", "minimum": 0, "description": "等待上限（毫秒；缺省/0 = 5000）"},
		},
		"required": []string{"handles"},
	}
}

func teamworkMilestoneDescription() string {
	return "Declare a milestone and write its content. The predicate is a dependency edge, not wall time: every stage in " +
		"`after` must have dispatched at least one teammate."
}

func teamworkMilestoneSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id":      map[string]interface{}{"type": "string"},
			"content": map[string]interface{}{"type": "string", "description": "里程碑内容（由 leader 撰写）"},
		},
		"required": []string{"id", "content"},
	}
}

func teamworkRetireDescription() string {
	return "End one teammate's round: release its worktree, clear its session contents and keep it on the roster. " +
		"It does NOT reclaim its jobs — a dispatched job stays on the table (and its output stays readable) until " +
		"team_close, the team's single reclamation point. A dirty worktree is an explicit error, never a silent discard."
}

func teamworkRetireSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"role": map[string]interface{}{"type": "string"},
		},
		"required": []string{"role"},
	}
}

func teamworkCloseDescription() string {
	return "Close the whole team (team_close): every enrolled member runs the same four-step retire — this is the ONE place " +
		"that reclaims jobs, so job output stays on the table until here — then the team board is sealed (closed/team.close), " +
		"the plan is marked closed and a close audit line is written. Idempotent: a second call returns already_closed=true " +
		"and neither re-seals nor re-audits. Takes no arguments: closure is a team-level act, not a seat's (per-member exit is team_retire)."
}

func teamworkCloseSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func teamworkContextDescription() string {
	return "Read each member's work context (roster entry + its job row + optional body) — a READ-ONLY surface: it " +
		"never dispatches, reclaims or consumes. Body is read with a NON-consuming read (jobs.Manager.Peek): the cursor " +
		"does not advance and a terminal job is not retired, so 'take a look at a member' never turns unread content into " +
		"read. Body is off by default and bounded per member (default 4KB, hard cap 32KB)."
}

func teamworkContextSchema() map[string]interface{} {
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
