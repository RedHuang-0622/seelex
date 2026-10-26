package store

// teamwork_items.go — teamwork 编排面的**第二层**：里程碑里的 Work Item（甘特图节点）
// 与「一个 Work Item 一个 Session + 一个 worktree」的绑定账本。
//
// 为什么要有这一层（2026-10-03 重构）：
//
//   - **Milestone 是屏障，Work Item 是调度单位**。阶段（stages）只回答"哪个角色先上"，
//     回答不了"这一步具体做什么、谁做、做完的判据是什么"；而 leader 排活、人看板、
//     依赖约束都要求工作是一个**有名字、有目标、有归属、有依赖**的实体。
//   - **一个 Work Item 一个 Session + 一个 worktree**：工作拆分之后专项专做，
//     隔离粒度从"人"下沉到"这一件事"。同一个人在同一个里程碑里承担多个 Work Item，
//     每个都拿到自己的会话与工作区——否则两次工作的上下文与改动会互相污染。
//   - **绑定关系落 JSONL（KV 语义）**：`{work_item → session_id + worktree}` 是
//     可追加、可回读的事实，生命周期与 Session 一致（team_close 之后一并结束）。
//     它不进计划 head：计划是"谁、什么顺序"的整份替换型内容，而绑定是**发生过的
//     事实流水**（与 events.jsonl 同一口径：只追加、不重写）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

// teamworkBindingFile 是 Work Item ↔ Session/worktree 绑定账本的文件名（追加、不重写）。
const teamworkBindingFile = "worktrees.jsonl"

// Work Item 的状态取值（空 = pending，与里程碑"空 = pending"同口径）。
const (
	TeamworkItemPending = dto.TeamworkItemPending
	TeamworkItemRunning = dto.TeamworkItemRunning
	TeamworkItemReview  = dto.TeamworkItemReview
	TeamworkItemDone    = dto.TeamworkItemDone
	TeamworkItemFailed  = dto.TeamworkItemFailed
)

// TeamworkWorkItem 是 contract/dto.TeamworkWorkItem 的持久化兼容 alias。
type TeamworkWorkItem = dto.TeamworkWorkItem

// TeamworkWorkItemStatusOrPending 是 contract/dto 自由函数的兼容入口。
func TeamworkWorkItemStatusOrPending(item TeamworkWorkItem) string {
	return dto.TeamworkWorkItemStatusOrPending(item)
}

// TeamworkWorkItemTerminal 是 contract/dto 自由函数的兼容入口。
func TeamworkWorkItemTerminal(item TeamworkWorkItem) bool {
	return dto.TeamworkWorkItemTerminal(item)
}

// TeamworkBinding 是「一个 Work Item ↔ 一个 Session + 一个 worktree」的绑定事实。
//
// 它是**可追加的事件流**而不是可变状态：绑定一次记一行，释放时再追加一行
// （Released=true）。读侧按 WorkItem 取最后一行 = 当前绑定；生命周期（team_close
// 之后一并结束）由最后一行是不是 Released 表达，而不是靠"删掉那一行"。
type TeamworkBinding struct {
	WorkItem      string `json:"work_item"`
	Milestone     string `json:"milestone,omitempty"`
	Role          string `json:"role"`
	SessionID     string `json:"session_id"`
	Worktree      string `json:"worktree,omitempty"`
	WorkspaceMode string `json:"workspace_mode,omitempty"`
	At            int64  `json:"at"`
	// Released=true 表示这一份绑定已经结束（验收销项 / 整队收口）。
	Released bool   `json:"released,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// TeamworkBindings 把绑定流水折成"每个 Work Item 当前的绑定"。
//
// 折叠规则**只看最后一行**：绑定与释放都是追加，最后一行说了算（KV 语义）。
// 已释放的项不出现在返回值里——"没有绑定"就是"没有在跑的隔离"。
func TeamworkBindings(rows []TeamworkBinding) map[string]TeamworkBinding {
	current := make(map[string]TeamworkBinding, len(rows))
	for _, row := range rows {
		item := strings.TrimSpace(row.WorkItem)
		if item == "" {
			continue
		}
		if row.Released {
			delete(current, item)
			continue
		}
		current[item] = row
	}
	return current
}

// ValidateTeamworkPlan 之外的**第二道校验**：里程碑图与 Work Item。
//
// 拆成独立函数而不是塞进 ValidateTeamworkPlan：那段已经很长，而这里两条图
// （里程碑 DAG、里程碑内 item DAG）各有自己的错误词汇，混在一起读不出"哪一层错了"。
func validateTeamworkWorkItems(plan TeamworkPlan, roles map[string]struct{}) error {
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
	// 里程碑之间的依赖：引用存在、不自指、无环（屏障必须是可拓扑排序的）。
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

	// 工作项：全计划 id 唯一、角色在编、依赖同里程碑内存在、DAG 无环。
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
			switch TeamworkWorkItemStatusOrPending(item) {
			case TeamworkItemPending, TeamworkItemRunning, TeamworkItemReview, TeamworkItemDone, TeamworkItemFailed:
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
					// 跨里程碑的顺序不由 item 依赖表达——那会绕过"里程碑是屏障"这条
					// 唯一口径（屏障必须是显式的、可单独审阅的一层）。
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

// validateMilestoneGraphAcyclic 用 Kahn 拓扑消元检出里程碑依赖环。
func validateMilestoneGraphAcyclic(milestones []TeamworkMilestone) error {
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

// validateItemGraphAcyclic 用 Kahn 拓扑消元检出**同里程碑内**工作项依赖环。
// 环会让 leader 永远等不到可派发的工作项，而计划里看不出任何异常。
func validateItemGraphAcyclic(milestone TeamworkMilestone) error {
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

// teamworkBindingsPath 返回绑定账本路径。
func (store *storeEngine) teamworkBindingsPath(key Key) string {
	return filepath.Join(store.teamworkDir(key), teamworkBindingFile)
}

// AppendTeamworkBinding 追加一行绑定事实（只追加、不重写；崩溃残尾由读侧跳过）。
func (repository *jsonRepository) AppendTeamworkBinding(_ context.Context, key Key, binding TeamworkBinding) error {
	if err := key.validate(); err != nil {
		return err
	}
	if repository.layout == nil {
		return errors.New("session storage: teamwork requires the v8 layout engine")
	}
	if strings.TrimSpace(binding.WorkItem) == "" {
		return errors.New("teamwork: binding.work_item is required")
	}
	if binding.At == 0 {
		binding.At = time.Now().UTC().Unix()
	}
	line, err := json.Marshal(binding)
	if err != nil {
		return fmt.Errorf("teamwork: encode binding: %w", err)
	}
	store := repository.layout
	if _, err := store.ensureLayoutGuide(key); err != nil {
		return err
	}
	dir := store.teamworkDir(key)
	lock := store.mu(key, moduleTeamwork)
	lock.Lock()
	defer lock.Unlock()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(dir, teamworkBindingFile), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("teamwork: open binding log: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("teamwork: append binding log: %w", err)
	}
	return nil
}

// ReadTeamworkBindings 读回全部绑定事实（按文件顺序 = 发生顺序）。
func (repository *jsonRepository) ReadTeamworkBindings(_ context.Context, key Key) ([]TeamworkBinding, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	if repository.layout == nil {
		return nil, errors.New("session storage: teamwork requires the v8 layout engine")
	}
	file, err := storeroot.ReadSharedFile(repository.layout.teamworkBindingsPath(key))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []TeamworkBinding{}, nil
		}
		return nil, err
	}
	lines := strings.Split(string(file), "\n")
	bindings := make([]TeamworkBinding, 0, len(lines))
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var binding TeamworkBinding
		if err := json.Unmarshal([]byte(trimmed), &binding); err != nil {
			if index == len(lines)-1 {
				continue // 崩溃残尾
			}
			return nil, fmt.Errorf("teamwork: decode binding row %d: %w", index, err)
		}
		bindings = append(bindings, binding)
	}
	return bindings, nil
}
