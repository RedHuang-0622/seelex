package teamwork

import (
	"errors"
	"fmt"
	"strings"
)

// ValidatePlan validates one plan before persistence. maxTeammates <= 0 means
// the caller does not own a teammate-count limit.
func ValidatePlan(plan Plan, maxTeammates int) error {
	if strings.TrimSpace(plan.TeamID) == "" {
		return errors.New("teamwork: plan.team_id is required")
	}
	if plan.Version < 1 {
		return fmt.Errorf("teamwork: plan.version must be >= 1, got %d", plan.Version)
	}
	if len(plan.Members) == 0 {
		return errors.New("teamwork: plan.members must not be empty")
	}
	if maxTeammates > 0 && len(plan.Members) > maxTeammates {
		return fmt.Errorf(
			"teamwork: 在编 teammate %d 人超过 limits.team.max_teammates=%d（显式拒绝，不静默排队）",
			len(plan.Members), maxTeammates)
	}

	roles := make(map[string]struct{}, len(plan.Members))
	sessions := make(map[string]struct{}, len(plan.Members))
	for index, member := range plan.Members {
		role := strings.TrimSpace(member.Role)
		if role == "" {
			return fmt.Errorf("teamwork: plan.members[%d].role is required", index)
		}
		if role == RoleMain || role == RoleUser {
			return fmt.Errorf("teamwork: 角色 %q 是复用主会话的内置角色，不得作为 teammate 派发", role)
		}
		if _, duplicate := roles[role]; duplicate {
			return fmt.Errorf("teamwork: 一个 teammate = 一个角色，角色 %q 重复（D8 禁止重复角色）", role)
		}
		roles[role] = struct{}{}
		sessionID := strings.TrimSpace(member.RoleSessionID)
		if sessionID == "" {
			return fmt.Errorf("teamwork: plan.members[%d].role_session_id is required（由 (team_id, role) 派生）", index)
		}
		if _, duplicate := sessions[sessionID]; duplicate {
			return fmt.Errorf("teamwork: role_session_id %q 重复（一人一会话）", sessionID)
		}
		sessions[sessionID] = struct{}{}
		seenPlugins := make(map[string]struct{}, len(member.Plugins))
		for pluginIndex, raw := range member.Plugins {
			name := strings.TrimSpace(raw)
			if name == "" {
				return fmt.Errorf("teamwork: plan.members[%d].plugins[%d] 为空项（插件名不得为空）", index, pluginIndex)
			}
			if _, duplicate := seenPlugins[name]; duplicate {
				return fmt.Errorf("teamwork: plan.members[%d].plugins 里插件 %q 重复（显式拒绝，不静默去重）", index, name)
			}
			seenPlugins[name] = struct{}{}
		}
	}

	if len(plan.Milestones) == 0 {
		return errors.New("teamwork: plan.milestones 至少要有一份（顺序必须有唯一事实）")
	}

	milestoneIDs := make(map[string]struct{}, len(plan.Milestones))
	for index, milestone := range plan.Milestones {
		id := strings.TrimSpace(milestone.ID)
		if id == "" {
			return fmt.Errorf("teamwork: plan.milestones[%d].id is required", index)
		}
		if _, duplicate := milestoneIDs[id]; duplicate {
			return fmt.Errorf("teamwork: 里程碑 id %q 重复", id)
		}
		milestoneIDs[id] = struct{}{}
		for _, role := range milestone.Required {
			if _, exists := roles[role]; !exists {
				return fmt.Errorf("teamwork: 里程碑 %q 的 required 指向不在编的角色 %q", id, role)
			}
		}
		switch milestone.Status {
		case "", MilestonePending, MilestoneActive, MilestoneDone:
		default:
			return fmt.Errorf("teamwork: 里程碑 %q 的状态 %q 非法（pending|active|done）", id, milestone.Status)
		}
	}
	return validateWorkItems(plan, roles)
}

func validateWorkItems(plan Plan, roles map[string]struct{}) error {
	milestoneIDs := make(map[string]struct{}, len(plan.Milestones))
	for index, milestone := range plan.Milestones {
		id := strings.TrimSpace(milestone.ID)
		if id == "" {
			return fmt.Errorf("teamwork: plan.milestones[%d].id is required", index)
		}
		if _, duplicate := milestoneIDs[id]; duplicate {
			return fmt.Errorf("teamwork: 里程碑 id %q 重复", id)
		}
		milestoneIDs[id] = struct{}{}
	}
	for _, milestone := range plan.Milestones {
		seen := make(map[string]struct{}, len(milestone.DependsOn))
		for _, dependency := range milestone.DependsOn {
			dependency = strings.TrimSpace(dependency)
			if dependency == "" {
				return fmt.Errorf("teamwork: 里程碑 %q 的 depends_on 含空项", milestone.ID)
			}
			if dependency == milestone.ID {
				return fmt.Errorf("teamwork: 里程碑 %q 依赖自己", milestone.ID)
			}
			if _, exists := milestoneIDs[dependency]; !exists {
				return fmt.Errorf("teamwork: 里程碑 %q 依赖不存在的里程碑 %q", milestone.ID, dependency)
			}
			if _, duplicate := seen[dependency]; duplicate {
				return fmt.Errorf("teamwork: 里程碑 %q 的 depends_on 里 %q 重复", milestone.ID, dependency)
			}
			seen[dependency] = struct{}{}
		}
	}
	if err := validateMilestoneGraphAcyclic(plan.Milestones); err != nil {
		return err
	}

	itemMilestone := make(map[string]string)
	for _, milestone := range plan.Milestones {
		for index, item := range milestone.Items {
			id := strings.TrimSpace(item.ID)
			if id == "" {
				return fmt.Errorf("teamwork: 里程碑 %q 的 items[%d].id is required", milestone.ID, index)
			}
			if owner, duplicate := itemMilestone[id]; duplicate {
				return fmt.Errorf("teamwork: 工作项 id %q 重复（已属于里程碑 %q）", id, owner)
			}
			itemMilestone[id] = milestone.ID
			if declared := strings.TrimSpace(item.Milestone); declared != "" && declared != milestone.ID {
				return fmt.Errorf("teamwork: 工作项 %q 声明属于里程碑 %q，却挂在 %q 下", id, declared, milestone.ID)
			}
			role := strings.TrimSpace(item.Role)
			if role == "" {
				return fmt.Errorf("teamwork: 工作项 %q 的 role is required（一 Work Item 一个 teammate）", id)
			}
			if _, exists := roles[role]; !exists {
				return fmt.Errorf("teamwork: 工作项 %q 的执行角色 %q 不在编（先 team_plan 增补成员）", id, role)
			}
			if strings.TrimSpace(item.Name) == "" {
				return fmt.Errorf("teamwork: 工作项 %q 的 name is required（工作内容必须有名字）", id)
			}
			switch WorkItemStatusOrPending(item) {
			case ItemPending, ItemRunning, ItemReview, ItemDone, ItemFailed:
			default:
				return fmt.Errorf("teamwork: 工作项 %q 的状态 %q 非法（pending|running|review|done|failed）", id, item.Status)
			}
		}
	}
	for _, milestone := range plan.Milestones {
		for _, item := range milestone.Items {
			for _, dependency := range item.DependsOn {
				dependency = strings.TrimSpace(dependency)
				if dependency == "" {
					return fmt.Errorf("teamwork: 工作项 %q 的 depends_on 含空项", item.ID)
				}
				if dependency == item.ID {
					return fmt.Errorf("teamwork: 工作项 %q 依赖自己", item.ID)
				}
				owner, exists := itemMilestone[dependency]
				if !exists {
					return fmt.Errorf("teamwork: 工作项 %q 依赖不存在的工作项 %q", item.ID, dependency)
				}
				if owner != milestone.ID {
					return fmt.Errorf("teamwork: 工作项 %q（里程碑 %q）依赖了里程碑 %q 的工作项 %q（跨里程碑顺序请用里程碑的 depends_on）",
						item.ID, milestone.ID, owner, dependency)
				}
			}
		}
		if err := validateItemGraphAcyclic(milestone); err != nil {
			return err
		}
	}
	return nil
}

func validateMilestoneGraphAcyclic(milestones []Milestone) error {
	if len(milestones) == 0 {
		return nil
	}
	remaining := make(map[string]int, len(milestones))
	dependents := make(map[string][]string, len(milestones))
	for _, milestone := range milestones {
		remaining[milestone.ID] = len(milestone.DependsOn)
		for _, dependency := range milestone.DependsOn {
			dependents[dependency] = append(dependents[dependency], milestone.ID)
		}
	}
	queue := make([]string, 0, len(milestones))
	for id, count := range remaining {
		if count == 0 {
			queue = append(queue, id)
		}
	}
	resolved := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		resolved++
		for _, dependent := range dependents[id] {
			remaining[dependent]--
			if remaining[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}
	if resolved != len(milestones) {
		return errors.New("teamwork: plan.milestones 的 depends_on 存在环（里程碑屏障必须是可拓扑排序的 DAG）")
	}
	return nil
}

func validateItemGraphAcyclic(milestone Milestone) error {
	if len(milestone.Items) == 0 {
		return nil
	}
	remaining := make(map[string]int, len(milestone.Items))
	dependents := make(map[string][]string, len(milestone.Items))
	for _, item := range milestone.Items {
		remaining[item.ID] = len(item.DependsOn)
		for _, dependency := range item.DependsOn {
			dependents[dependency] = append(dependents[dependency], item.ID)
		}
	}
	queue := make([]string, 0, len(milestone.Items))
	for id, count := range remaining {
		if count == 0 {
			queue = append(queue, id)
		}
	}
	resolved := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		resolved++
		for _, dependent := range dependents[id] {
			remaining[dependent]--
			if remaining[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}
	if resolved != len(milestone.Items) {
		return fmt.Errorf("teamwork: 里程碑 %q 的工作项 depends_on 存在环（里程碑内依赖必须是 DAG）", milestone.ID)
	}
	return nil
}
