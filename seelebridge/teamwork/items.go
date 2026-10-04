package teamwork

// items.go — **Work Item 生命周期**：里程碑里的一件事从排活到销项的全过程。
//
// 口径（2026-10-03 重构）：
//
//   - **Milestone 是屏障，Work Item 是调度单位**。里程碑之间串行（depends_on），
//     里程碑内部按工作项依赖 DAG 并行——V 模型要的正是这个形状。
//   - **一个 Work Item 一个 Session + 一个 worktree**：隔离粒度下沉到"这件事"。
//     同一个人在同一里程碑里承担多件事时，各自拿到自己的会话与工作区。
//   - **分里程碑排活**：只给"依赖已完成"的里程碑排活（`PlanMilestone`），
//     派发也只认活跃里程碑里的工作项。工作安排不是一次性把全程铺好。
//   - **未开始的工作可调整，已开始/已结束的是既定的**（`AdjustItem` / `FailItem`）。
//   - **尾插**：teammate 跑完 → 先合并这件事的 worktree → 成功才插入一条有界回执到
//     teammate 的消息队列；合并失败也插入，但正文写明"插入失败、请 leader 亲自执行"，
//     并把 bug 原文打印进去（`SettleWorkItem`）。
//   - **生命周期到 team_close 为止**：验收通过释放这一件事的会话与工作区（下一件事
//     重新开），整队收口把剩下的一并结束；**中断恢复不清会话**（记忆必须建在）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/jobs"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// WorkItemSpec 是 leader 排活 / 调整时的输入（**不含状态**：状态由编排面维护，
// leader 不能靠写计划把一件事说成"已完成"）。
type WorkItemSpec struct {
	ID          string   `json:"id"`
	Role        string   `json:"role"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Goal        string   `json:"goal,omitempty"`
	DependsOn   []string `json:"depends_on,omitempty"`
}

// ItemView 是一个工作项的只读读面（计划条目 + 当前绑定 + 队列位置）。
type ItemView struct {
	Item sessionstore.TeamworkWorkItem `json:"item"`
	// Live 报告这件事现在是不是真的有一份工作区绑定（账本里的最后一行未释放）。
	Live bool `json:"live"`
	// Interrupted 报告"状态说在跑，但本进程的作业表里查不到它的句柄"——
	// jobs I-4 的显式化（句柄只在内存）。它不是错误，是**可重派**的信号。
	Interrupted bool `json:"interrupted"`
}

// WorkItemSessionID 派生「一个 Work Item 一个 Session」的会话号。
//
// 派生而不是让 leader 传：会话号必须**稳定**——重启之后同一个工作项要落回同一个
// 会话，否则"恢复之后上下文记忆还在"就不成立（角色会话是进程内执行面，会话号是
// 它取历史的唯一钥匙）。
func WorkItemSessionID(derive func(mainSessionID, teamID, roleName string) string, mainSessionID, teamID, role, itemID string) string {
	return derive(mainSessionID, teamID, role) + "-wi-" + itemID
}

// WorkItemWorktreeName 派生「一个 Work Item 一个 worktree」的指派名。
func WorkItemWorktreeName(role, itemID string) string {
	sanitized := strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-").Replace(strings.TrimSpace(itemID))
	return "seelex/" + strings.TrimSpace(role) + "-" + sanitized
}

// milestoneIndex 返回里程碑在计划里的下标（不存在 = -1）。
func milestoneIndex(plan sessionstore.TeamworkPlan, id string) int {
	for index, milestone := range plan.Milestones {
		if milestone.ID == id {
			return index
		}
	}
	return -1
}

// findItem 定位一个工作项，返回它所属的里程碑与**可变指针**。
func findItem(plan *sessionstore.TeamworkPlan, id string) (*sessionstore.TeamworkMilestone, *sessionstore.TeamworkWorkItem, bool) {
	if plan == nil {
		return nil, nil, false
	}
	for milestoneIndex := range plan.Milestones {
		milestone := &plan.Milestones[milestoneIndex]
		for itemIndex := range milestone.Items {
			if milestone.Items[itemIndex].ID == id {
				return milestone, &milestone.Items[itemIndex], true
			}
		}
	}
	return nil, nil, false
}

// recomputeMilestones 重算里程碑状态（屏障 + 收敛）。
//
// 判据是**两条**都成立才算 done：① 它的依赖里程碑都 done（屏障）；② 它自己的工作项
// 都 done。没有工作项的里程碑是"占位/遗留"，状态归 leader 的 team_milestone 管，
// 这里不覆盖（阶段制时代的计划照旧读）。
func recomputeMilestones(plan *sessionstore.TeamworkPlan) {
	if plan == nil || len(plan.Milestones) == 0 {
		return
	}
	memo := make(map[int]string, len(plan.Milestones))
	visiting := make(map[int]bool, len(plan.Milestones))
	for index := range plan.Milestones {
		resolveMilestoneStatus(plan, index, memo, visiting)
	}
}

func resolveMilestoneStatus(plan *sessionstore.TeamworkPlan, index int, memo map[int]string, visiting map[int]bool) string {
	if status, ok := memo[index]; ok {
		return status
	}
	milestone := &plan.Milestones[index]
	// 只有**参与新里程碑图**的里程碑才由这里派生状态：要么它下面排了工作项，要么它
	// 声明了屏障依赖。阶段制时代的里程碑（After/Required、无 Items、无 DependsOn）
	// 的状态归 leader 的 team_milestone 管，这里不覆盖。
	if len(milestone.Items) == 0 && len(milestone.DependsOn) == 0 {
		memo[index] = milestone.Status
		return milestone.Status
	}
	if visiting[index] {
		// 环：计划校验已拦下；这里是纯防御，返回 pending 而不是无限递归。
		return sessionstore.TeamworkMilestonePending
	}
	visiting[index] = true
	depsDone := true
	for _, dependency := range milestone.DependsOn {
		dependencyIndex := milestoneIndex(*plan, dependency)
		if dependencyIndex < 0 || resolveMilestoneStatus(plan, dependencyIndex, memo, visiting) != sessionstore.TeamworkMilestoneDone {
			depsDone = false
		}
	}
	delete(visiting, index)
	// 空工作项的里程碑**不算完成**（没排活 = 还没干），但屏障打开时它是 active。
	allDone := len(milestone.Items) > 0
	for _, item := range milestone.Items {
		if item.StatusOrPending() != sessionstore.TeamworkItemDone {
			allDone = false
			break
		}
	}
	status := sessionstore.TeamworkMilestonePending
	if depsDone {
		status = sessionstore.TeamworkMilestoneActive
	}
	if depsDone && allDone {
		status = sessionstore.TeamworkMilestoneDone
	}
	memo[index] = status
	milestone.Status = status
	return status
}

// PlanMilestone 给一个里程碑**安排工作**（分里程碑排活）。
//
// 闸门：该里程碑的依赖必须都已 done。这条闸门就是"完成一个里程碑之后再做下一个
// 里程碑的工作安排"——不靠 leader 自觉，靠编排面拒收。
func (c *Coordinator) PlanMilestone(ctx context.Context, milestoneID string, specs []WorkItemSpec) error {
	c.lockPlan()
	defer c.unlockPlan()
	milestoneID = strings.TrimSpace(milestoneID)
	plan, err := c.store.ReadPlan(ctx, c.key)
	if err != nil {
		return err
	}
	index := milestoneIndex(plan, milestoneID)
	if index < 0 {
		return fmt.Errorf("teamwork: 计划里没有里程碑 %q（先 team_plan 声明里程碑）", milestoneID)
	}
	if err := c.milestoneOpen(plan, plan.Milestones[index]); err != nil {
		return err
	}
	if len(specs) == 0 {
		return errors.New("teamwork: 排活至少要有一条工作项（名称 + 执行 teammate）")
	}
	roles := make(map[string]struct{}, len(plan.Members))
	for _, member := range plan.Members {
		roles[member.Role] = struct{}{}
	}
	for _, spec := range specs {
		id := strings.TrimSpace(spec.ID)
		if id == "" {
			return errors.New("teamwork: 工作项 id is required")
		}
		if _, _, exists := findItem(&plan, id); exists {
			return fmt.Errorf("teamwork: 工作项 %q 已存在（工作项 id 在整份计划里唯一）", id)
		}
		role := strings.TrimSpace(spec.Role)
		if _, enrolled := roles[role]; !enrolled {
			return fmt.Errorf("teamwork: 工作项 %q 的执行角色 %q 不在编（一 Work Item 一个 teammate）", id, role)
		}
		if strings.TrimSpace(spec.Name) == "" {
			return fmt.Errorf("teamwork: 工作项 %q 的 name is required（工作内容必须有名字）", id)
		}
		plan.Milestones[index].Items = append(plan.Milestones[index].Items, sessionstore.TeamworkWorkItem{
			ID:          id,
			Milestone:   milestoneID,
			Role:        role,
			Name:        strings.TrimSpace(spec.Name),
			Description: strings.TrimSpace(spec.Description),
			Goal:        strings.TrimSpace(spec.Goal),
			DependsOn:   trimmedList(spec.DependsOn),
			Status:      sessionstore.TeamworkItemPending,
		})
	}
	recomputeMilestones(&plan)
	if err := c.store.WritePlan(ctx, c.key, plan, c.maxMembers); err != nil {
		return err
	}
	for _, spec := range specs {
		if err := c.audit(ctx, sessionstore.TeamworkEvent{
			Kind: sessionstore.TeamworkEventItem, TeamID: plan.TeamID, Milestone: milestoneID,
			Role: strings.TrimSpace(spec.Role), WorkItem: strings.TrimSpace(spec.ID),
			Detail: "排活：" + strings.TrimSpace(spec.Name),
		}); err != nil {
			return err
		}
	}
	return nil
}

// AdjustItem 调整一条**尚未开始**的工作项（人 / 名称 / 描述 / 达成目标 / 依赖）。
//
// 铁律：已经开始的（running / review）与已经有结论的（done / failed）都是**既定事实**，
// 改它们等于改历史；要改就先把结论落下来再做新的一条。
func (c *Coordinator) AdjustItem(ctx context.Context, itemID string, spec WorkItemSpec) error {
	c.lockPlan()
	defer c.unlockPlan()
	plan, err := c.store.ReadPlan(ctx, c.key)
	if err != nil {
		return err
	}
	milestone, item, ok := findItem(&plan, strings.TrimSpace(itemID))
	if !ok {
		return fmt.Errorf("teamwork: 计划里没有工作项 %q", itemID)
	}
	if item.StatusOrPending() != sessionstore.TeamworkItemPending {
		return fmt.Errorf("teamwork: 工作项 %q 已经开始（状态 %s），开始与结束的工作是既定的，不可调整",
			item.ID, item.StatusOrPending())
	}
	if err := c.milestoneOpen(plan, *milestone); err != nil {
		return err
	}
	if role := strings.TrimSpace(spec.Role); role != "" {
		enrolled := false
		for _, member := range plan.Members {
			if member.Role == role {
				enrolled = true
				break
			}
		}
		if !enrolled {
			return fmt.Errorf("teamwork: 调整后的执行角色 %q 不在编", role)
		}
		item.Role = role
	}
	if name := strings.TrimSpace(spec.Name); name != "" {
		item.Name = name
	}
	if spec.Description != "" {
		item.Description = strings.TrimSpace(spec.Description)
	}
	if spec.Goal != "" {
		item.Goal = strings.TrimSpace(spec.Goal)
	}
	if spec.DependsOn != nil {
		item.DependsOn = trimmedList(spec.DependsOn)
	}
	if err := c.store.WritePlan(ctx, c.key, plan, c.maxMembers); err != nil {
		return err
	}
	return c.audit(ctx, sessionstore.TeamworkEvent{
		Kind: sessionstore.TeamworkEventItem, TeamID: plan.TeamID, Milestone: item.Milestone,
		Role: item.Role, WorkItem: item.ID, Detail: "调整（未开始）：" + item.Name,
	})
}

// DispatchItem 派发**一个工作项**。
//
// 它一次做完四件事：屏障闸门（里程碑）、依赖闸门（里程碑内 DAG）、执行隔离
// （这件事自己的会话与工作区）、作业派发（后台跑，受理即返回）。
func (c *Coordinator) DispatchItem(ctx context.Context, itemID string) (jobs.Handle, error) {
	c.lockPlan()
	defer c.unlockPlan()
	if c.workers == nil {
		return "", errors.New("teamwork: 派发工作项需要 WorkerRunner（未装配）")
	}
	itemID = strings.TrimSpace(itemID)
	plan, err := c.store.ReadPlan(ctx, c.key)
	if err != nil {
		return "", err
	}
	milestone, item, ok := findItem(&plan, itemID)
	if !ok {
		return "", fmt.Errorf("teamwork: 计划里没有工作项 %q（先 team_work 排活）", itemID)
	}
	if err := c.milestoneOpen(plan, *milestone); err != nil {
		return "", err
	}
	status := item.StatusOrPending()
	switch status {
	case sessionstore.TeamworkItemDone:
		return "", fmt.Errorf("teamwork: 工作项 %q 已经完成（验收通过之后不可重派）", itemID)
	case sessionstore.TeamworkItemRunning:
		if c.handleAlive(item.Handle) {
			return jobs.Handle(item.Handle), nil // 在跑：折叠到那一条（幂等）
		}
		// 句柄不在册（进程重启 / 被驱逐）：视为**可重派**，会话号照旧复用。
	case sessionstore.TeamworkItemReview:
		return "", fmt.Errorf("teamwork: 工作项 %q 已跑完待验收（先 team_accept 或 team_fail）", itemID)
	}
	for _, dependency := range item.DependsOn {
		_, dependencyItem, exists := findItem(&plan, dependency)
		if !exists {
			return "", fmt.Errorf("teamwork: 工作项 %q 依赖的 %q 不在计划里", itemID, dependency)
		}
		if dependencyItem.StatusOrPending() != sessionstore.TeamworkItemDone {
			return "", fmt.Errorf("teamwork: 工作项 %q 依赖的 %q 还没完成（状态 %s）——里程碑内的依赖是硬闸门（V 模型：exec 做完 test 才能跟上）",
				itemID, dependency, dependencyItem.StatusOrPending())
		}
	}
	member, enrolled := memberFor(plan, item.Role)
	if !enrolled {
		return "", fmt.Errorf("teamwork: 工作项 %q 的执行角色 %q 不在编", itemID, item.Role)
	}
	if err := c.ensureCapacity(item.Role); err != nil {
		return "", err
	}
	groups, err := memberPermissionGroups(member)
	if err != nil {
		return "", err
	}
	outputPath := ""
	if c.jobOutputs != nil {
		path, err := c.jobOutputs.JobOutputPath(ctx, item.Role)
		if err != nil {
			return "", fmt.Errorf("teamwork: 分配作业输出路径失败: %w", err)
		}
		outputPath = path
	}
	// 会话与工作区：**中断恢复时复用**（记忆必须建在），首次派发才派生新号。
	sessionID := strings.TrimSpace(item.SessionID)
	if sessionID == "" {
		sessionID = WorkItemSessionID(c.derive, c.key.SessionID, plan.TeamID, item.Role, item.ID)
	}
	worktree := strings.TrimSpace(item.Worktree)
	if worktree == "" {
		worktree = WorkItemWorktreeName(item.Role, item.ID)
	}
	binding := WorkspaceBinding{
		MainSessionID: c.key.SessionID, TeamID: plan.TeamID, Milestone: milestone.ID,
		WorkItem: item.ID, Role: item.Role, SessionID: sessionID, Worktree: worktree,
	}
	created := ""
	if c.spaces != nil {
		bound, bindErr := c.spaces.BindWorkspace(ctx, binding)
		if bindErr != nil {
			return "", fmt.Errorf("teamwork: 为工作项 %q 建工作区失败: %w", item.ID, bindErr)
		}
		if strings.TrimSpace(bound.Worktree) != "" {
			binding.Worktree = bound.Worktree
		}
		binding.Path, binding.Branch = bound.Path, bound.Branch
		created = binding.Worktree
	}
	request := WorkerRequest{
		MainSessionID:    c.key.SessionID,
		TeamID:           plan.TeamID,
		Role:             item.Role,
		RoleSessionID:    sessionID,
		Subject:          SubjectForRole(item.Role),
		ToolsPolicy:      member.ToolsPolicy,
		PermissionGroups: groups,
		Plugins:          member.Plugins,
		Worktree:         binding.Worktree,
		WorkItemID:       item.ID,
		Milestone:        milestone.ID,
		Goal:             workItemGoal(*item),
		MaxTurns:         c.maxTurns,
		OutputPath:       outputPath,
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("teamwork: 编码 worker 载荷: %w", err)
	}
	handle, err := c.jobs.Dispatch(ctx, jobs.Spec{
		Kind:        KindWorker,
		Scope:       jobs.Scope{Session: c.key.SessionID, Subject: request.Subject},
		Node:        item.ID,
		Description: fmt.Sprintf("%s/%s: %s", item.Role, item.ID, item.Name),
		Payload:     payload,
		OutputPath:  outputPath,
		// 一件事同时只跑一轮：同一**工作项**的重复派发折叠到在跑的那一条。
		Dedup: "teamwork:item:" + item.ID,
	})
	if err != nil {
		// 刚建的现场随失败的派发一起撤掉：绑定描述的是"这件事在跑"，没跑起来就不该占着。
		if created != "" && c.spaces != nil {
			_ = c.spaces.ReleaseWorkspaceItem(ctx, binding)
		}
		return "", err
	}
	now := c.clock().UTC().Unix()
	item.Status = sessionstore.TeamworkItemRunning
	item.SessionID = sessionID
	item.Worktree = binding.Worktree
	item.Handle = describeHandle(handle)
	item.StartedAt = now
	if item.StartedAt != 0 && item.FinishedAt != 0 {
		item.FinishedAt = 0
	}
	recomputeMilestones(&plan)
	if err := c.store.WritePlan(ctx, c.key, plan, c.maxMembers); err != nil {
		return handle, err
	}
	// 账本：绑定事实一行（KV 语义，读侧取最后一行）。
	if err := c.store.AppendBinding(ctx, c.key, sessionstore.TeamworkBinding{
		WorkItem: item.ID, Milestone: milestone.ID, Role: item.Role,
		SessionID: sessionID, Worktree: binding.Worktree, At: now,
	}); err != nil {
		return handle, err
	}
	if err := c.audit(ctx, sessionstore.TeamworkEvent{
		Kind: sessionstore.TeamworkEventDispatch, TeamID: plan.TeamID, Milestone: milestone.ID,
		Role: item.Role, Handle: describeHandle(handle), Node: item.ID, WorkItem: item.ID,
		Detail: item.Name,
	}); err != nil {
		return handle, err
	}
	return handle, nil
}

// SettleWorkItem 是 teammate 一轮跑完之后的**尾插程序**：先合并这件事的 worktree，
// 成功才插入；失败也插入，但正文写明"插入失败、请 leader 亲自执行"，并把 bug 原文
// 打印进 teammate 的消息里。
//
// 它由 worker 执行体在回合结束后调用（见 executor.go），因此是**自动**的：不依赖
// leader 记得来收，也不唤醒任何忙会话（§6.1）——尾插落在 teammate 自己的消息队列上。
func (c *Coordinator) SettleWorkItem(ctx context.Context, request WorkerRequest, runErr error) error {
	itemID := strings.TrimSpace(request.WorkItemID)
	if itemID == "" {
		return nil // 非 Work Item 口径的派发：没有尾插的落点
	}
	// ── 阶段 A（临界区内，只读 + 判定）─────────────────────────────────
	// 确认这件事还在跑（幂等闸门），并取出合并要用的现场。**合并这一步不在这里**：
	// 它是慢 git 操作（300 提交的成功变基实测 ≈114s），关进临界区就等于把整支团队的
	// 编排面冻结几十秒，并发 settle 也会排成一条长队。
	c.lockPlan()
	plan, err := c.store.ReadPlan(ctx, c.key)
	if err != nil {
		c.unlockPlan()
		return err
	}
	if _, item, ok := findItem(&plan, itemID); !ok || item.StatusOrPending() != sessionstore.TeamworkItemRunning {
		c.unlockPlan()
		return nil // 已经收口过了（幂等：尾插恰好一次）
	}
	bindings, err := c.store.ReadBindings(ctx, c.key)
	if err != nil {
		c.unlockPlan()
		return err
	}
	binding, hasBinding := sessionstore.TeamworkBindings(bindings)[itemID]
	teamID := plan.TeamID
	c.unlockPlan()

	// ── 步 1：合并（**在计划锁之外**）──────────────────────────────────
	// 合并是"改动回到主干"的动作，插在**插入之前**——顺序反了就会出现"leader 已经看到
	// 结论、而主干上还没有这份改动"。可观察顺序因此仍是：合并 → 尾插 → 状态。
	var mergeErr error
	if hasBinding && c.spaces != nil && strings.TrimSpace(binding.Worktree) != "" {
		mergeErr = c.spaces.MergeWorkspace(ctx, c.workspaceBinding(binding, teamID))
	}

	// ── 阶段 B（临界区内，读-改-写）────────────────────────────────────
	// 重读计划（合并这段时间里计划可能已被别人改写——leader 的手动处置、另一次 settle），
	// 再次确认这件事仍是 running，然后 步 2 尾插 → 步 3 写态。这一整段读-改-写在同一把
	// 计划锁内完成，因此并发 settle **不会**再互相覆盖（这正是本条修复的丢失更新）。
	c.lockPlan()
	defer c.unlockPlan()
	plan, err = c.store.ReadPlan(ctx, c.key)
	if err != nil {
		return err
	}
	milestone, item, ok := findItem(&plan, itemID)
	if !ok || item.StatusOrPending() != sessionstore.TeamworkItemRunning {
		return nil // 合并期间已被收口：幂等返回（不再重复尾插 / 写态）
	}
	// 步 2：尾插（有界一行；bug 原文与合并失败说明都在里面）。
	text := settleMessage(*item, *milestone, mergeErr, runErr)
	if c.teammates != nil {
		if err := c.teammates.EnqueueTeammateMessage(ctx, TeammateMessage{
			MainSessionID: c.key.SessionID, TeamID: plan.TeamID, Role: item.Role,
			RoleSessionID: item.SessionID, Milestone: milestone.ID, WorkItem: item.ID, Text: text,
		}); err != nil {
			// 尾插失败**不是工作失败**：正文随审计一起留痕，状态照走。
			text += "（尾插失败：" + err.Error() + "）"
		}
	}
	// 步 3：状态 → 待验收 / 失败（失败是可重派/待处置的静态，不抹掉现场与记忆）。
	item.Status = sessionstore.TeamworkItemReview
	item.Note = "跑完待验收"
	if runErr != nil || mergeErr != nil {
		item.Status = sessionstore.TeamworkItemFailed
		item.Note = text
	}
	item.FinishedAt = c.clock().UTC().Unix()
	if err := c.store.WritePlan(ctx, c.key, plan, c.maxMembers); err != nil {
		return err
	}
	return c.audit(ctx, sessionstore.TeamworkEvent{
		Kind: sessionstore.TeamworkEventSettle, TeamID: plan.TeamID, Milestone: milestone.ID,
		Role: item.Role, Handle: item.Handle, WorkItem: item.ID, Detail: text,
	})
}

// AcceptItem 是 leader 的**验收通过**：工作项 → done，销项，并结束这一件事的执行隔离
// （释放 worktree + 清这一件事的会话），teammate 下一件事重新开一套。
//
// **责任链（一条链，顺序即责任）**：
//
//	合并（尾插步 1：MergeWorkspace）→ 回执（步 2）→ 状态（步 3：review/failed）
//	→ leader 审查 → 验收入账（本方法）/ 重新派活 / 整队收口
//
// 验收是链尾，它要**释放现场**（删 worktree 目录 + 删分支），所以只许在尾插走完之后
// 动手——下游抢在上游前面，就会对一份正在被合并使用的现场下手。真实 git 上的现场形状
// 是 `fork/exec …git.exe: The directory name is invalid`（复现：
// seelebridge/worktree/worktree_vanished_scene_repro_test.go）。
//
// 「在跑」的判据沿用 DispatchItem 与看板投影的**同一处**口径：running **且** handle 还在
// 册 = 这一件事真的还在飞（→ 拒绝，让它把尾插走完）。handle 已作废（进程重启 / 已被回收）
// 则相反：尾插不会再跑，验收就此成了清理动作（→ 放行，这正是"重启后收尾"那条路）。
func (c *Coordinator) AcceptItem(ctx context.Context, itemID, note string) error {
	c.lockPlan()
	defer c.unlockPlan()
	plan, err := c.store.ReadPlan(ctx, c.key)
	if err != nil {
		return err
	}
	_, item, ok := findItem(&plan, strings.TrimSpace(itemID))
	if !ok {
		return fmt.Errorf("teamwork: 计划里没有工作项 %q", itemID)
	}
	switch item.StatusOrPending() {
	case sessionstore.TeamworkItemReview:
		// 尾插已走完（合并 → 回执 → 状态）：链尾该动的地方，放行。
	case sessionstore.TeamworkItemRunning:
		if c.handleAlive(item.Handle) {
			return fmt.Errorf("teamwork: 工作项 %q 还在跑（handle %s 还在册），不能验收——先等它的回执（尾插会先合并、再插回执），或先 jobs_manage(op=kill, handle=\"%s\") 把它停掉，那时现场才归你处置",
				item.ID, item.Handle, item.Handle)
		}
	case sessionstore.TeamworkItemFailed:
		// 链尾只有**一个**销项动作，就是验收：failed 的现场与记忆都留给 leader
		// （人工解冲突 / 变基 / 合并，或判定放弃），处置完由此销项。收口的闸门要求
		// 账先收干净（见 Close 的 unsettledItems），所以这条路必须留着——否则一个
		// failed 的工作项既不能验收、又挡着收口，整队就永远收不了。
	case sessionstore.TeamworkItemDone:
		return nil // 幂等
	default:
		return fmt.Errorf("teamwork: 工作项 %q 还没跑完（状态 %s），不能验收", item.ID, item.StatusOrPending())
	}
	if err := c.releaseItem(ctx, *item, plan.TeamID, "accept"); err != nil {
		return err
	}
	item.Status = sessionstore.TeamworkItemDone
	if trimmed := strings.TrimSpace(note); trimmed != "" {
		item.Note = trimmed
	} else if strings.TrimSpace(item.Note) == "" {
		item.Note = "验收通过"
	}
	item.Handle = ""
	item.SessionID = ""
	item.Worktree = ""
	item.FinishedAt = c.clock().UTC().Unix()
	recomputeMilestones(&plan)
	if err := c.store.WritePlan(ctx, c.key, plan, c.maxMembers); err != nil {
		return err
	}
	return c.audit(ctx, sessionstore.TeamworkEvent{
		Kind: sessionstore.TeamworkEventAccept, TeamID: plan.TeamID, Milestone: item.Milestone,
		Role: item.Role, WorkItem: item.ID, Detail: item.Note,
	})
}

// FailItem 是 leader 的**判定不通过**：状态 → failed（现场与记忆都留着，可重派）。
func (c *Coordinator) FailItem(ctx context.Context, itemID, note string) error {
	c.lockPlan()
	defer c.unlockPlan()
	plan, err := c.store.ReadPlan(ctx, c.key)
	if err != nil {
		return err
	}
	_, item, ok := findItem(&plan, strings.TrimSpace(itemID))
	if !ok {
		return fmt.Errorf("teamwork: 计划里没有工作项 %q", itemID)
	}
	if item.StatusOrPending() == sessionstore.TeamworkItemDone {
		return fmt.Errorf("teamwork: 工作项 %q 已经完成，不能判失败", item.ID)
	}
	item.Status = sessionstore.TeamworkItemFailed
	item.Note = strings.TrimSpace(note)
	item.FinishedAt = c.clock().UTC().Unix()
	if err := c.store.WritePlan(ctx, c.key, plan, c.maxMembers); err != nil {
		return err
	}
	return c.audit(ctx, sessionstore.TeamworkEvent{
		Kind: sessionstore.TeamworkEventFail, TeamID: plan.TeamID, Milestone: item.Milestone,
		Role: item.Role, WorkItem: item.ID, Detail: item.Note,
	})
}

// Items 读回全部工作项（按里程碑顺序、里程碑内按排活顺序）。
func (c *Coordinator) Items(ctx context.Context) ([]ItemView, error) {
	plan, err := c.store.ReadPlan(ctx, c.key)
	if err != nil {
		return nil, err
	}
	bindings, err := c.store.ReadBindings(ctx, c.key)
	if err != nil {
		return nil, err
	}
	live := sessionstore.TeamworkBindings(bindings)
	views := make([]ItemView, 0)
	for _, milestone := range plan.Milestones {
		for _, item := range milestone.Items {
			_, hasBinding := live[item.ID]
			views = append(views, ItemView{
				Item:        item,
				Live:        hasBinding,
				Interrupted: item.StatusOrPending() == sessionstore.TeamworkItemRunning && !c.handleAlive(item.Handle),
			})
		}
	}
	return views, nil
}

// RecoveryReport 是中断恢复的读数（token 额度中断 / 进程重启之后的样子）。
type RecoveryReport struct {
	// Items 是计划里的工作项总数；Pending/Running/Review/Done/Failed 是分状态计数。
	Items                          int `json:"items"`
	Pending, Running, Review, Done, Failed int
	// Interrupted 是"状态说在跑、而本进程作业表里查不到句柄"的工作项（可重派）。
	Interrupted []string `json:"interrupted,omitempty"`
	// Bindings 是仍然活着的 worktree / session 绑定（生命周期到 team_close 为止）。
	Bindings []sessionstore.TeamworkBinding `json:"bindings,omitempty"`
}

// Recover 做**中断恢复**：把计划与绑定账本读回来，把"句柄已作废"显式化。
//
// 它**不动**任何会话与工作区：中断（额度耗尽 / 进程重启）之后要接着干，记忆与现场
// 必须还在（这正是"恢复之后上下文记忆必须建在"的落点）。重派同一个工作项时，
// `DispatchItem` 会复用原会话号——角色会话是进程内执行面，会话号是它取历史的钥匙。
func (c *Coordinator) Recover(ctx context.Context) (RecoveryReport, error) {
	report := RecoveryReport{}
	plan, err := c.store.ReadPlan(ctx, c.key)
	if err != nil {
		return report, err
	}
	for _, milestone := range plan.Milestones {
		for _, item := range milestone.Items {
			report.Items++
			switch item.StatusOrPending() {
			case sessionstore.TeamworkItemPending:
				report.Pending++
			case sessionstore.TeamworkItemRunning:
				report.Running++
				if !c.handleAlive(item.Handle) {
					// 句柄只在内存（jobs I-4）：重启之后它必然作废，这不是错误，
					// 是"这一件事可以重派"的信号。会话号与现场都留着。
					report.Interrupted = append(report.Interrupted, item.ID)
				}
			case sessionstore.TeamworkItemReview:
				report.Review++
			case sessionstore.TeamworkItemDone:
				report.Done++
			case sessionstore.TeamworkItemFailed:
				report.Failed++
			}
		}
	}
	bindings, err := c.store.ReadBindings(ctx, c.key)
	if err != nil {
		return report, err
	}
	// 账本是**追加型**：释放是再记一行而不是删行，因此这里必须按 KV 语义折叠
	// （取每个 work item 的最后一行）——直接遍历会把"历史行"当成"活绑定"。
	for _, binding := range sessionstore.TeamworkBindings(bindings) {
		report.Bindings = append(report.Bindings, binding)
	}
	if err := c.audit(ctx, sessionstore.TeamworkEvent{
		Kind: sessionstore.TeamworkEventRecover, TeamID: plan.TeamID,
		Detail: fmt.Sprintf("恢复：items=%d running=%d review=%d done=%d failed=%d 可重派=%d 活绑定=%d",
			report.Items, report.Running, report.Review, report.Done, report.Failed,
			len(report.Interrupted), len(report.Bindings)),
	}); err != nil {
		return report, err
	}
	return report, nil
}

// releaseItem 结束一个工作项的执行隔离：释放 worktree → 清这一件事的会话 → 追加
// 绑定释放行（账本是追加型，释放是再记一行而不是删行）。
func (c *Coordinator) releaseItem(ctx context.Context, item sessionstore.TeamworkWorkItem, teamID, reason string) error {
	bindings, err := c.store.ReadBindings(ctx, c.key)
	if err != nil {
		return err
	}
	binding, ok := sessionstore.TeamworkBindings(bindings)[item.ID]
	if !ok {
		return nil // 没有活绑定：无可释放（幂等）
	}
	if c.spaces != nil {
		if err := c.spaces.ReleaseWorkspaceItem(ctx, c.workspaceBinding(binding, teamID)); err != nil {
			return fmt.Errorf("teamwork: 释放工作项 %q 的工作区失败: %w", item.ID, err)
		}
	}
	if c.sessions != nil && strings.TrimSpace(binding.SessionID) != "" {
		if err := c.sessions.ResetSession(ctx, binding.SessionID); err != nil {
			return fmt.Errorf("teamwork: 清工作项 %q 的会话内容失败: %w", item.ID, err)
		}
	}
	return c.store.AppendBinding(ctx, c.key, sessionstore.TeamworkBinding{
		WorkItem: item.ID, Milestone: binding.Milestone, Role: binding.Role,
		SessionID: binding.SessionID, Worktree: binding.Worktree,
		At: c.clock().UTC().Unix(), Released: true, Reason: reason,
	})
}

// releaseAllItems 是整队收口时的**一并结束**：所有活绑定（会话 + 工作区）到此为止。
func (c *Coordinator) releaseAllItems(ctx context.Context, teamID string) error {
	rows, err := c.store.ReadBindings(ctx, c.key)
	if err != nil {
		return err
	}
	// 先折叠成"当前绑定"（KV 语义：一个 work item 只有最后一行说了算），再逐个结束——
	// 否则历史行会被重复释放，而且同一个 work item 会被当成多份现场。
	for _, binding := range sessionstore.TeamworkBindings(rows) {
		if c.spaces != nil {
			if err := c.spaces.ReleaseWorkspaceItem(ctx, c.workspaceBinding(binding, teamID)); err != nil {
				return fmt.Errorf("teamwork: 收口释放工作项 %q 的工作区失败: %w", binding.WorkItem, err)
			}
		}
		if c.sessions != nil && strings.TrimSpace(binding.SessionID) != "" {
			if err := c.sessions.ResetSession(ctx, binding.SessionID); err != nil {
				return fmt.Errorf("teamwork: 收口清工作项 %q 的会话内容失败: %w", binding.WorkItem, err)
			}
		}
		if err := c.store.AppendBinding(ctx, c.key, sessionstore.TeamworkBinding{
			WorkItem: binding.WorkItem, Milestone: binding.Milestone, Role: binding.Role,
			SessionID: binding.SessionID, Worktree: binding.Worktree,
			At: c.clock().UTC().Unix(), Released: true, Reason: "team_close",
		}); err != nil {
			return err
		}
	}
	return nil
}

// workspaceBinding 把账本里的一行绑定折成工作区端口认识的现场（补上会话作用域键与
// 团队 id：账本存的是"发生过的事实"，端口要的是"现在该怎么动这个现场"）。
func (c *Coordinator) workspaceBinding(binding sessionstore.TeamworkBinding, teamID string) WorkspaceBinding {
	return WorkspaceBinding{
		MainSessionID: c.key.SessionID,
		TeamID:        teamID,
		Milestone:     binding.Milestone,
		WorkItem:      binding.WorkItem,
		Role:          binding.Role,
		SessionID:     binding.SessionID,
		Worktree:      binding.Worktree,
	}
}

// AppendTeammateMessage 把一条尾插消息记进 teammate 的**消息队列**（= 审计流里的一行
// message 事件，按 role_session_id 读）。队列按**追加**增长，读法是有界过滤。
func (c *Coordinator) AppendTeammateMessage(ctx context.Context, message TeammateMessage) error {
	return c.audit(ctx, sessionstore.TeamworkEvent{
		Kind:          sessionstore.TeamworkEventMessage,
		TeamID:        message.TeamID,
		Role:          message.Role,
		RoleSessionID: message.RoleSessionID,
		Milestone:     message.Milestone,
		WorkItem:      message.WorkItem,
		Detail:        message.Text,
	})
}

// TeammateMessages 读一个 teammate（按角色会话号）的消息队列，最近 limit 条
// （limit <= 0 = 不截断）。
func (c *Coordinator) TeammateMessages(ctx context.Context, roleSessionID string, limit int) ([]sessionstore.TeamworkEvent, error) {
	events, err := c.store.ReadEvents(ctx, c.key)
	if err != nil {
		return nil, err
	}
	roleSessionID = strings.TrimSpace(roleSessionID)
	queue := make([]sessionstore.TeamworkEvent, 0)
	for _, event := range events {
		if event.Kind != sessionstore.TeamworkEventMessage {
			continue
		}
		if roleSessionID != "" && event.RoleSessionID != roleSessionID {
			continue
		}
		queue = append(queue, event)
	}
	if limit > 0 && len(queue) > limit {
		queue = queue[len(queue)-limit:]
	}
	return queue, nil
}

// milestoneOpen 是屏障闸门：里程碑的依赖没完成，就不许给它排活、也不许派发它下面的
// 工作项（"完成一个里程碑之后再做下一个里程碑的工作"）。
func (c *Coordinator) milestoneOpen(plan sessionstore.TeamworkPlan, milestone sessionstore.TeamworkMilestone) error {
	for _, dependency := range milestone.DependsOn {
		dependencyIndex := milestoneIndex(plan, dependency)
		if dependencyIndex < 0 {
			return fmt.Errorf("teamwork: 里程碑 %q 依赖的 %q 不在计划里", milestone.ID, dependency)
		}
		status := plan.Milestones[dependencyIndex].Status
		if len(plan.Milestones[dependencyIndex].Items) > 0 && status != sessionstore.TeamworkMilestoneDone {
			return fmt.Errorf("teamwork: 里程碑 %q 依赖的里程碑 %q 还没完成（状态 %q）——里程碑之间串行：完成一整个里程碑再排下一个",
				milestone.ID, dependency, status)
		}
	}
	return nil
}

// handleAlive 报告一个作业句柄在本进程的作业表里是否还在册。
func (c *Coordinator) handleAlive(handle string) bool {
	trimmed := strings.TrimSpace(handle)
	if trimmed == "" {
		return false
	}
	_, ok := c.jobs.Observe(jobs.Handle(trimmed))
	return ok
}

// workItemGoal 把工作内容折成这一轮的正文（名称 / 描述 / 达成目标三件）。
func workItemGoal(item sessionstore.TeamworkWorkItem) string {
	builder := &strings.Builder{}
	builder.WriteString("工作项：" + item.Name)
	if item.Description != "" {
		builder.WriteString("\n描述：" + item.Description)
	}
	if item.Goal != "" {
		builder.WriteString("\n达成目标：" + item.Goal)
	}
	return builder.String()
}

// settleMessage 组装尾插正文（**有界**：看板与上下文都吃这一行）。
func settleMessage(item sessionstore.TeamworkWorkItem, milestone sessionstore.TeamworkMilestone, mergeErr, runErr error) string {
	builder := &strings.Builder{}
	builder.WriteString("[" + item.ID + "] " + item.Name + "（里程碑 " + milestone.ID + "，角色 " + item.Role + "）")
	switch {
	case runErr != nil:
		builder.WriteString("：这一轮失败——")
		builder.WriteString(clipOneLine(runErr.Error()))
	case mergeErr != nil:
		builder.WriteString("：改动**插入失败**（worktree 合并失败：" + clipOneLine(mergeErr.Error()) + "）")
		builder.WriteString("；请 leader 亲自执行合并，现场保留在 " + item.Worktree)
	default:
		builder.WriteString("：跑完，改动已合并回主工作区，等待 leader 评估")
	}
	return builder.String()
}

// clipOneLine 把错误正文压成一行（尾插是有界回执，不是日志搬运）。
func clipOneLine(text string) string {
	flat := strings.Join(strings.Fields(text), " ")
	const limit = 240
	if len(flat) <= limit {
		return flat
	}
	return flat[:limit] + "…"
}

// trimmedList 去掉空项与重复项，保持顺序（依赖列表的输入卫生）。
func trimmedList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, duplicate := seen[trimmed]; duplicate {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}
