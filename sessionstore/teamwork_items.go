package sessionstore

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
)

// teamworkBindingFile 是 Work Item ↔ Session/worktree 绑定账本的文件名（追加、不重写）。
const teamworkBindingFile = "worktrees.jsonl"

// Work Item 的状态取值（空 = pending，与里程碑"空 = pending"同口径）。
const (
	TeamworkItemPending = "pending"
	TeamworkItemRunning = "running"
	// TeamworkItemReview 是**待验收**：teammate 已经跑完、改动已合并（或显式报告合并
	// 失败），但 leader 还没给结论。它把"做完"与"验收通过"分开——这正是尾插程序与
	// leader 评估之间的那一段（见 seelebridge/teamwork/items.go 的 SettleWorkItem）。
	TeamworkItemReview = "review"
	TeamworkItemDone   = "done"
	TeamworkItemFailed = "failed"
)

// TeamworkWorkItem 是里程碑里的一个工作项（甘特图的节点）。
//
// 依赖（DependsOn）只在**同一个里程碑内部**表达：里程碑之间由屏障串行
// （Milestone.DependsOn），里程碑内部才是 DAG 并行——这正是 V 模型要的形状
// （exec 做完，test_case 立刻跟上，两者同属一个里程碑）。
type TeamworkWorkItem struct {
	// ID 是工作项的稳定标识（依赖、派发、验收、销项都以它为准），全计划唯一。
	ID string `json:"id"`
	// Milestone 是它所属的里程碑 id。
	Milestone string `json:"milestone"`
	// Role 是执行它的 teammate 角色（一 Work Item 一个 teammate）。
	Role string `json:"role"`
	// Name / Description / Goal 是工作内容三件：名称、描述、达成目标。
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Goal        string `json:"goal,omitempty"`
	// DependsOn 是同里程碑内前置工作项的 id（顺序/依赖的唯一事实之一）。
	DependsOn []string `json:"depends_on,omitempty"`
	// Status 空 = pending；取值见 TeamworkItem* 常量。
	Status string `json:"status,omitempty"`
	// SessionID / Worktree 是这一件事自己的执行隔离（一 Work Item 一套）。
	SessionID string `json:"session_id,omitempty"`
	Worktree  string `json:"worktree,omitempty"`
	// Handle 是最近一次派发的作业句柄——**只是投影**（jobs I-4：句柄只在内存，
	// 进程重启即作废），读侧一律以 Observe 的返回为准。
	Handle string `json:"handle,omitempty"`
	// Note 是 leader 的验收结论 / 失败说明（收尾时写回，看板直接显示）。
	Note string `json:"note,omitempty"`
	// StartedAt / FinishedAt 是派发与验收的时间戳（unix 秒；0 = 未发生）。
	StartedAt  int64 `json:"started_at,omitempty"`
	FinishedAt int64 `json:"finished_at,omitempty"`
}

// StatusOrPending 把空状态读成 pending（"空 = pending"的唯一读法）。
func (item TeamworkWorkItem) StatusOrPending() string {
	if strings.TrimSpace(item.Status) == "" {
		return TeamworkItemPending
	}
	return item.Status
}

// Terminal 报告这个工作项是否已经有结论（done / failed）。
func (item TeamworkWorkItem) Terminal() bool {
	switch item.StatusOrPending() {
	case TeamworkItemDone, TeamworkItemFailed:
		return true
	default:
		return false
	}
}

// TeamworkBinding 是「一个 Work Item ↔ 一个 Session + 一个 worktree」的绑定事实。
//
// 它是**可追加的事件流**而不是可变状态：绑定一次记一行，释放时再追加一行
// （Released=true）。读侧按 WorkItem 取最后一行 = 当前绑定；生命周期（team_close
// 之后一并结束）由最后一行是不是 Released 表达，而不是靠"删掉那一行"。
type TeamworkBinding struct {
	WorkItem  string `json:"work_item"`
	Milestone string `json:"milestone,omitempty"`
	Role      string `json:"role"`
	SessionID string `json:"session_id"`
	Worktree  string `json:"worktree,omitempty"`
	At        int64  `json:"at"`
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
			switch item.StatusOrPending() {
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
	file, err := readSharedFile(repository.layout.teamworkBindingsPath(key))
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
