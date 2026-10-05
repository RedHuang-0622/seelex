package teamwork

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/jobs"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// Plan 读回当前计划。
func (c *Coordinator) Plan(ctx context.Context) (sessionstore.TeamworkPlan, error) {
	return c.store.ReadPlan(ctx, c.key)
}

// Audit 读回审计流水（派发 / 里程碑 / 收口的事实顺序）。
func (c *Coordinator) Audit(ctx context.Context) ([]sessionstore.TeamworkEvent, error) {
	return c.store.ReadEvents(ctx, c.key)
}

// SetPlan 写入（整份替换）一份计划，并把这次改写记进审计面。
//
// 缺 role_session_id 的成员按 (team_id, role) 补齐：派生是幂等的，于是
// "leader 只写角色名"也能得到一份合法计划，而不是被校验挡回来。
//
// **Work Item 的保留规则**（2026-10-03）：同名里程碑（按 id 匹配）里已经排好的
// 工作项与它们的运行态**原样保留**——`team_plan` 改的是"谁、几个里程碑"，不是
// "已经把活排到哪一步了"。不保留的话，leader 为了加一个成员再调一次 team_plan，
// 看板上已完成的工作项连同句柄会被整份抹掉，而那是**事实**，不是配置。
func (c *Coordinator) SetPlan(ctx context.Context, plan sessionstore.TeamworkPlan) error {
	// 整份替换也是读-改-写（读回旧计划以保留同名里程碑里已排好的工作项），必须与
	// 并发的 item 级变更共用同一把计划锁——否则"改成员"会与"排活/派发"互相覆盖。
	c.lockPlan()
	defer c.unlockPlan()
	for index := range plan.Members {
		if strings.TrimSpace(plan.Members[index].RoleSessionID) == "" {
			plan.Members[index].RoleSessionID = c.derive(c.key.SessionID, plan.TeamID, plan.Members[index].Role)
		}
	}
	if previous, err := c.store.ReadPlan(ctx, c.key); err == nil {
		itemsByMilestone := make(map[string][]sessionstore.TeamworkWorkItem, len(previous.Milestones))
		for _, milestone := range previous.Milestones {
			if len(milestone.Items) > 0 {
				itemsByMilestone[milestone.ID] = milestone.Items
			}
		}
		for index := range plan.Milestones {
			if len(plan.Milestones[index].Items) > 0 {
				continue
			}
			if kept, ok := itemsByMilestone[plan.Milestones[index].ID]; ok {
				plan.Milestones[index].Items = kept
			}
		}
		// 未合并标记与工作项一样是**事实**（那份现场里还留着没合进去的产出），不是配置：
		// 整份替换时按 item id 保留仍然存在的那几条。不保留的话，leader 为了改一个成员
		// 再调一次 team_plan，收口闸门就跟着松一格（"这份现场还能不能拆"的判据被抹掉）
		// ——闸门不允许反向放松。
		if len(previous.State.Unmerged) > 0 {
			present := map[string]bool{}
			for _, milestone := range plan.Milestones {
				for _, item := range milestone.Items {
					present[item.ID] = true
				}
			}
			for itemID, reason := range previous.State.Unmerged {
				if !present[itemID] {
					continue
				}
				if plan.State.Unmerged == nil {
					plan.State.Unmerged = map[string]string{}
				}
				plan.State.Unmerged[itemID] = reason
			}
		}
	}
	recomputeMilestones(&plan)
	if err := c.store.WritePlan(ctx, c.key, plan, c.maxMembers); err != nil {
		return err
	}
	return c.audit(ctx, sessionstore.TeamworkEvent{
		Kind:   sessionstore.TeamworkEventPlan,
		TeamID: plan.TeamID,
		Detail: fmt.Sprintf("members=%d milestones=%d items=%d plugins=%s",
			len(plan.Members), len(plan.Milestones), countItems(plan), pluginAssemblySummary(plan.Members)),
	})
}

// pluginAssemblySummary 把成员的插件装配压成一行**审计摘要**（与回执同一口径：
// 空集写成 inherit-host，不覆盖是"说出来的事实"，不是缺失的字段）。
//
// 全员没声明时给 "none"：那时每个成员的语义都是"继承宿主当前装配"，逐人写一遍
// inherit-host 只会把不装插件的团队的审计行拉长到没人看——一条 none 比四行噪音更
// 接近事实。
func pluginAssemblySummary(members []sessionstore.TeamworkMember) string {
	if len(members) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(members))
	anyDeclared := false
	for _, member := range members {
		names := make([]string, 0, len(member.Plugins))
		for _, raw := range member.Plugins {
			if name := strings.TrimSpace(raw); name != "" {
				names = append(names, name)
			}
		}
		if len(names) == 0 {
			parts = append(parts, member.Role+"=inherit-host")
			continue
		}
		anyDeclared = true
		parts = append(parts, member.Role+"="+strings.Join(names, "+"))
	}
	if !anyDeclared {
		return "none"
	}
	return strings.Join(parts, ";")
}

// countItems 数一份计划里的工作项总数（审计行用）。
func countItems(plan sessionstore.TeamworkPlan) int {
	total := 0
	for _, milestone := range plan.Milestones {
		total += len(milestone.Items)
	}
	return total
}

// Dispatch 派发一个 teammate 的作业：受理回执即 handle，调用方不等待。
//
// 顺序（谁先谁后）不在这里判定——它在计划的 depends_on 里，由 leader 掌控调
// 用时机（§6.2）。这里只管三件事：角色在编、人数未满、作用域正确。
//
// 返回值只有 handle（2026-10-04）：阶段口径退场后，这个"teammate 级老口径"的派发
// 不再有可回执的归属（归 Work Item 的一轮走 DispatchItem，那里回 item id）。
func (c *Coordinator) Dispatch(ctx context.Context, role, goal string) (jobs.Handle, error) {
	c.lockPlan()
	defer c.unlockPlan()
	if c.workers == nil {
		return "", errors.New("teamwork: team_dispatch 需要 WorkerRunner（未装配）")
	}
	plan, err := c.store.ReadPlan(ctx, c.key)
	if err != nil {
		return "", err
	}
	member, ok := memberFor(plan, role)
	if !ok {
		return "", fmt.Errorf("teamwork: 角色 %q 不在计划里（一角色一 teammate；先 team_plan 增补）", role)
	}
	if err := c.ensureCapacity(role); err != nil {
		return "", err
	}
	groups, err := memberPermissionGroups(member)
	if err != nil {
		return "", err
	}
	// 建现场（2026-10-05 F2）：老口径派发的 teammate 也要有自己的 worktree——它过去
	// 从头到尾没有一次 BindWorkspace，于是 WorkerRequest.Worktree 永远为空，
	// bindWorkerProjectRoot 查空后回退主会话根：teammate 的读写全落在 main 上。
	//
	// nodeID = 角色名（契约：teammate 级 nodeID = `<role>`），指派名 = `seelex/<role>`；
	// 建现场**只经** Workspaces 端口——coordinator 不碰 git、不碰文件系统。
	nodeID := strings.TrimSpace(role)
	binding := WorkspaceBinding{
		MainSessionID: c.key.SessionID, TeamID: plan.TeamID,
		WorkItem: nodeID, Role: role, SessionID: member.RoleSessionID,
		Worktree: TeammateWorktreeName(nodeID),
	}
	created := ""
	if c.spaces != nil {
		bound, bindErr := c.spaces.BindWorkspace(ctx, binding)
		if bindErr != nil {
			return "", fmt.Errorf("teamwork: 为 teammate %q 建工作区失败: %w", role, bindErr)
		}
		if strings.TrimSpace(bound.Worktree) != "" {
			binding.Worktree = bound.Worktree
		}
		binding.Path, binding.Branch = bound.Path, bound.Branch
		created = binding.Worktree
	}
	// 输出归属（§4.7 / S5）：装配了产品输出面就把这一轮的正文交给产品自有文件，
	// 否则交回框架自建（并按框架语义在销项 / 驱逐 / Close 时被删）。分配失败不降级
	// 成"悄悄退回框架文件"——那会让"正文活到 close"这条保证时真时假。
	outputPath := ""
	if c.jobOutputs != nil {
		path, err := c.jobOutputs.JobOutputPath(ctx, role)
		if err != nil {
			return "", fmt.Errorf("teamwork: 分配作业输出路径失败: %w", err)
		}
		outputPath = path
	}
	request := WorkerRequest{
		MainSessionID:    c.key.SessionID,
		TeamID:           plan.TeamID,
		Role:             role,
		RoleSessionID:    member.RoleSessionID,
		Subject:          SubjectForRole(role),
		ToolsPolicy:      member.ToolsPolicy,
		PermissionGroups: groups,
		Plugins:          member.Plugins,
		Worktree:         binding.Worktree,
		Goal:             goal,
		MaxTurns:         c.maxTurns,
		OutputPath:       outputPath,
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("teamwork: 编码 worker 载荷: %w", err)
	}
	handle, err := c.jobs.Dispatch(ctx, jobs.Spec{
		Kind:  KindWorker,
		Scope: jobs.Scope{Session: c.key.SessionID, Subject: request.Subject},
		// 行标题只能来自派发时这句话（作业会活过这一轮）。
		Description: fmt.Sprintf("%s: %s", role, goal),
		Payload:     payload,
		// 输出归属：非空 = 产品自有文件（框架只按偏移读、永不删），空 = 框架自建。
		OutputPath: outputPath,
		// 一个 teammate 同时只跑一轮：同一角色的重复派发折叠到在跑的那一条，
		// 而不是并排出第二条（并排会让"谁在改这个 worktree"说不清）。
		Dedup: "teamwork:" + role,
	})
	if err != nil {
		// 刚建的现场随失败的派发一起撤掉（与 DispatchItem 同一口径）：绑定描述的是
		// "这一轮在跑"，没跑起来就不该占着。
		if created != "" && c.spaces != nil {
			_ = c.spaces.ReleaseWorkspaceItem(ctx, binding)
		}
		return "", err
	}
	// 派发成功后才写状态：句柄只是**投影**（jobs I-4），不在册的值一律视为过期。
	if plan.State.Jobs == nil {
		plan.State.Jobs = map[string]string{}
	}
	plan.State.Jobs[role] = describeHandle(handle)
	// 现场指派名写回在编成员：绑根侧（WorkerRequest.Worktree）与看板都要看得到它。
	for index := range plan.Members {
		if plan.Members[index].Role == role {
			plan.Members[index].Worktree = binding.Worktree
		}
	}
	if err := c.store.WritePlan(ctx, c.key, plan, c.maxMembers); err != nil {
		return handle, err
	}
	// 账本：teammate 级绑定一行（KV 语义，读侧取最后一行）。WorkItem 列记的是它的
	// **现场 nodeID**（= 角色名）——账本的这一列是非空主键，而 teammate 级没有 Work
	// Item 可记；用 nodeID 填这一格，与 Worktree 的去前缀换算互为同一个键。
	if err := c.store.AppendBinding(ctx, c.key, sessionstore.TeamworkBinding{
		WorkItem: nodeID, Role: role, SessionID: member.RoleSessionID,
		Worktree: binding.Worktree, At: c.clock().UTC().Unix(),
	}); err != nil {
		return handle, err
	}
	if err := c.audit(ctx, sessionstore.TeamworkEvent{
		Kind: sessionstore.TeamworkEventDispatch, TeamID: plan.TeamID,
		Role: role, Handle: describeHandle(handle), Detail: goal,
	}); err != nil {
		return handle, err
	}
	return handle, nil
}

// Join 是一次**有界**汇合等待：只在该汇合点真有依赖时用（§6.2 / §6.3 的 A 姿势）。
//
// 它只**观察**，不取回：取回是消费式的，会把 teammate 的输出从工作表格上拿走。
func (c *Coordinator) Join(ctx context.Context, handles []jobs.Handle, budget time.Duration) ([]jobs.Record, error) {
	if len(handles) == 0 {
		// 没有句柄就没有可汇合的成员：显式写出类型的零值，receipt 里的 jobs 仍是
		// null（与改动前逐字节一致）；裸 `return nil, nil` 被静态门禁禁止。
		return []jobs.Record(nil), nil
	}
	deadline := c.clock().Add(budget)
	events := c.jobs.Events()
	for {
		records, pending := c.observe(handles)
		if pending == 0 || budget <= 0 {
			if err := c.audit(ctx, sessionstore.TeamworkEvent{
				Kind: sessionstore.TeamworkEventJoin, Handle: describeHandle(handles[0]),
				Detail: fmt.Sprintf("%d 条作业：%s", len(records), summarize(records)),
			}); err != nil {
				return records, err
			}
			return records, nil
		}
		remaining := deadline.Sub(c.clock())
		if remaining <= 0 {
			if err := c.audit(ctx, sessionstore.TeamworkEvent{
				Kind: sessionstore.TeamworkEventJoin, Handle: describeHandle(handles[0]),
				Detail: fmt.Sprintf("汇合窗口用尽，仍在跑 %d 条：%s", pending, summarize(records)),
			}); err != nil {
				return records, err
			}
			return records, nil
		}
		timer := time.NewTimer(minDuration(remaining, 100*time.Millisecond))
		select {
		case <-events:
			timer.Stop()
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return records, ctx.Err()
		}
	}
}

// Milestone 声明一个里程碑并写入 leader 撰写的内容（§4.5）。
//
// 判据是**依赖边**而不是墙钟：它依赖的每个里程碑都必须已经 done——否则"里程碑"就会
// 变成一句没有事实支撑的口号。（阶段制时代这里看的是 `after` 里的阶段派发过没有；
// 阶段口径退场后，判据回到唯一那份顺序事实：milestones[].depends_on。）
func (c *Coordinator) Milestone(ctx context.Context, id, content string) error {
	c.lockPlan()
	defer c.unlockPlan()
	plan, err := c.store.ReadPlan(ctx, c.key)
	if err != nil {
		return err
	}
	index := -1
	for position, milestone := range plan.Milestones {
		if milestone.ID == id {
			index = position
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("teamwork: 计划里没有里程碑 %q", id)
	}
	for _, dependency := range plan.Milestones[index].DependsOn {
		if !milestoneDone(plan, dependency) {
			return fmt.Errorf("teamwork: 里程碑 %q 依赖的里程碑 %q 还没完成（里程碑只能声明在依赖边之后）", id, dependency)
		}
	}
	plan.Milestones[index].Status = sessionstore.TeamworkMilestoneDone
	plan.Milestones[index].Content = content
	if plan.State.Milestones == nil {
		plan.State.Milestones = map[string]string{}
	}
	plan.State.Milestones[id] = content
	if err := c.store.WritePlan(ctx, c.key, plan, c.maxMembers); err != nil {
		return err
	}
	return c.audit(ctx, sessionstore.TeamworkEvent{
		Kind: sessionstore.TeamworkEventMilestone, TeamID: plan.TeamID, Milestone: id, Detail: content,
	})
}

// closeStepsLocked 是**整队收口**里对单个在编成员做的四步（Close 的唯一实现），也是整个
// 团队唯一一处回收实现（D7 / §4.4；此前还有一条"单角色退场"的口径，2026-10-06 整条删除）。
//
// **调用约定**：本方法假定调用方**已经持有计划锁**（c.lockPlan），因此它自己不再取锁
// （锁不可重入）。Close 在自己的最外层取一次锁，然后调这里——这样"收口四步"与随后的
// 计划写入落在同一个临界区里，中途不会被并发的派发/尾插插进来。
//
// 关于 c.jobs.Reclaim（步 1）在锁内的安全性：它取消并**等待**目标作业终结（上限
// jobs.Limits.DefaultWait），而 worker 执行体在收尾时会回调 SettleWorkItem（要取同一把
// 计划锁）。这条"等待"不会成环，因为 Close 的 unsettledItems 闸门已经保证：此刻不存在
// "还在 running 的工作项尾插"（真在跑会被闸门拒收）。于是 Reclaim 只可能等到"非 Work Item
// 口径的作业"（其 SettleWorkItem 在读到 WorkItemID == "" 时**在取锁之前**就返回）或
// "已终结的作业"（live=false，不必等）。
//
// 为什么回收只在这一处（2026-10-02 拆出来、2026-10-06 唯一化）：整队收口之前，"谁还在跑"
// 一直留在册上——作业正文不会被任何"退场"动作悄悄撤走；实现也只有这一份，不复制四步。
//
//  1. 回收该 teammate 名下未完成的作业（只动这一个 teammate，不牵连同会话其他人）
//  2. 释放该 teammate 的**全部**现场：角色级（nodeID = 角色名）+ 该角色名下每一个
//     已派发 Work Item 的现场（逐个释放；脏工作区由实现按语义报错，不静默丢弃）
//  3. 清空该 teammate 的会话记录内容（工作历史 + durable 快照）
//  4. 保留 teammate 在编，worktree 字段置空待重派；回收时清掉句柄投影
//
// 顺序不能换：先停作业再清记忆，否则会"清完记忆还在写"。
//
//	步 1 回收这个主体名下的作业 → 步 2 拆它的全部现场（角色级 + 名下每个 Work Item）
//	→ 步 3 清会话内容 → 步 4 名册动作（指派名与句柄投影清掉，成员留在编）
func (c *Coordinator) closeStepsLocked(ctx context.Context, role string) (sessionstore.TeamworkPlan, error) {
	plan, err := c.store.ReadPlan(ctx, c.key)
	if err != nil {
		return plan, err
	}
	if err := c.reclaimStepsLocked(ctx, plan, role); err != nil {
		return plan, err
	}
	// 步 4：留在编，worktree 指派名与句柄投影一并清掉（现场已经拆了，地址不该再留着）。
	for index := range plan.Members {
		if plan.Members[index].Role == role {
			plan.Members[index].Worktree = ""
		}
	}
	delete(plan.State.Jobs, role)
	if err := c.store.WritePlan(ctx, c.key, plan, c.maxMembers); err != nil {
		return plan, err
	}
	return plan, nil
}

// Reclaim 回收**一个 teammate 单元**：步 1 回收它名下的作业 → 步 2 拆它的现场（角色级
// + 它名下每个 Work Item；**只拆自己这一份**，不牵连同会话其他人）→ 步 3 清它的会话内容。
//
// 它就是整队收口四步的前三步（`closeStepsLocked` 步 4 的名册动作只属于整队收口），也是
// 契约（seelebridge/workunit）里 `Unit.Reclaim` 的落点——"拆现场 + 清会话 + 回收作业"
// 只有这一份实现，适配器不另写一套。幂等：三步各自幂等（无作业可收 / 无现场可拆 /
// 无会话可清都是 no-op）。
func (c *Coordinator) Reclaim(ctx context.Context, role string) error {
	c.lockPlan()
	defer c.unlockPlan()
	plan, err := c.store.ReadPlan(ctx, c.key)
	if err != nil {
		return err
	}
	return c.reclaimStepsLocked(ctx, plan, role)
}

// reclaimStepsLocked 是收口四步的**前三步**（步 1–3）的唯一实现。
//
// **调用约定**：假定调用方已持有计划锁（同 closeStepsLocked 的理由：锁不可重入）。
func (c *Coordinator) reclaimStepsLocked(ctx context.Context, plan sessionstore.TeamworkPlan, role string) error {
	member, ok := memberFor(plan, role)
	if !ok {
		return fmt.Errorf("teamwork: 角色 %q 不在计划里", role)
	}
	// 步 1：只回收这一个主体名下的作业，同会话其他人不受影响。
	if err := c.jobs.Reclaim(ctx, jobs.Scope{Session: c.key.SessionID, Subject: SubjectForRole(role)}); err != nil {
		return fmt.Errorf("teamwork: 收口步 1（回收作业）失败: %w", err)
	}
	// 步 2：释放该 teammate 的**全部**现场（角色级 + 该角色名下每一个已派发 Work
	// Item）。缺端口 = 显式报错：静默跳过会让 worktree 悄悄累积。
	if c.worktrees == nil {
		return errors.New("teamwork: 收口步 2 需要 WorkspaceReleaser（未装配）")
	}
	if err := c.releaseTeammateScenes(ctx, plan.TeamID, role, "team_close"); err != nil {
		return fmt.Errorf("teamwork: 收口步 2（释放工作区）失败: %w", err)
	}
	// 步 3：清会话内容（删的是对话记忆与工作区检出，不是注册/在编）。
	if c.sessions == nil {
		return errors.New("teamwork: 收口步 3 需要 SessionResetter（未装配）")
	}
	if err := c.sessions.ResetSession(ctx, member.RoleSessionID); err != nil {
		return fmt.Errorf("teamwork: 收口步 3（清会话内容）失败: %w", err)
	}
	return nil
}

// unsettledItems 返回"没落定"的工作项（`id(状态)` 口径），供收口闸门拒收用：
//
//	running 且 handle 还在册 —— 尾插还在飞（现场正被合并使用）
//	failed                  —— 尾插把现场留给 leader 人工处置
//	review 且带未合并标记     —— 尾插没能把改动合进去（未提交 / 主工作区挡路）：
//	                           不判死（一份已完成的产出不该被说成"得重派"），但现场里
//	                           还留着没落地的产出，静默拆掉就是丢产出
//
// 待验收（review）**本身**不算没落定：尾插已走完、合并已落地，释放它的现场是安全的。
// 这把尺子量的是**每一件工作项**（不按角色分档）：收口会把**所有** per-item 现场连同
// 角色级现场一并拆掉，所以凡是"还被尾插拿着"或"留给 leader 人工处置"或"改动还没合进去"
// 的现场都不许被它静默拆掉（见 Close 的注释）。
func (c *Coordinator) unsettledItems(plan sessionstore.TeamworkPlan) []string {
	unsettled := make([]string, 0)
	for _, milestone := range plan.Milestones {
		for _, item := range milestone.Items {
			switch item.StatusOrPending() {
			case sessionstore.TeamworkItemRunning:
				if c.handleAlive(item.Handle) {
					unsettled = append(unsettled, item.ID+"(running)")
				}
			case sessionstore.TeamworkItemFailed:
				unsettled = append(unsettled, item.ID+"(failed)")
			case sessionstore.TeamworkItemReview:
				if _, unmerged := plan.State.Unmerged[item.ID]; unmerged {
					unsettled = append(unsettled, item.ID+"(review:改动未合并)")
				}
			}
		}
	}
	return unsettled
}

// AliveSceneNodes 返回计划里"本进程还有执行面"的现场 nodeID 集合（契约命名：
// Work Item 级 = `<role>-<itemID>`，teammate 级 = `<role>`）。
//
// 它是**投影**而不是事实：句柄只在内存（jobs I-4），进程重启之后这个集合必然是空的——
// 那正是"这些单元可以重派/需要人工处置"的信号。会话回灌（workunit 契约的 Recover）用它
// 与"角色会话句柄还在册"一起判定"记录说在跑、而本进程已无它的执行面"。
func (c *Coordinator) AliveSceneNodes(ctx context.Context) (map[string]bool, error) {
	plan, err := c.store.ReadPlan(ctx, c.key)
	if err != nil {
		return nil, err
	}
	alive := map[string]bool{}
	for _, milestone := range plan.Milestones {
		for _, item := range milestone.Items {
			if item.StatusOrPending() == sessionstore.TeamworkItemRunning && c.handleAlive(item.Handle) {
				alive[SceneNodeID(item.Role, item.ID)] = true
			}
		}
	}
	for _, member := range plan.Members {
		if c.handleAlive(plan.State.Jobs[member.Role]) {
			alive[strings.TrimSpace(member.Role)] = true
		}
	}
	return alive, nil
}

// Close 收口整支团队（team_close）：逐在编成员走同一套四步（这里 reclaim=true，回收
// 统一收口到这一处）→ 封板团队看板（closed/team.close）→ 计划标 closed → 落一条
// close 审计。
//
// 幂等：第二次调用返回 alreadyClosed=true，且**不重复封板、不重复落审计**——收口
// 事实只有一个（plan.State），它已经在册。
//
// 失败语义：成员四步与"计划标 closed + 审计"是硬事实（出错即上抛）；封板失败同样
// 上抛——存档里还写着 active 的收口看板会在重启恢复时冒充"在册"（board.go 对 goal
// 看板的同一条口径）。封板失败时域内尚未标 closed，重试是一次干净的收口。
func (c *Coordinator) Close(ctx context.Context) (bool, error) {
	// 收口是一段**长的**读-改-写（闸门判定 → 逐人退场 → 释放现场 → 封板 → 标 closed），
	// 全程持计划锁：否则闸门放行之后、标 closed 之前，一次并发 DispatchItem 就能把"已收口的
	// 计划"重新写回 running。持锁期间不做任何 git 合并（那在尾插里，见 SettleWorkItem）。
	c.lockPlan()
	defer c.unlockPlan()
	plan, err := c.store.ReadPlan(ctx, c.key)
	if err != nil {
		return false, err
	}
	if plan.State.State == sessionstore.TeamworkStateClosed {
		return true, nil
	}
	// 收口闸门（责任链的下游端）：收口是**唯一**会拆 per-item 现场的地方（releaseAllItems
	// 把每件活绑定的 worktree 目录与分支一并删掉），所以"还被尾插拿着"与"留给 leader 人工
	// 处置"这两类现场不许被它静默拆掉——
	//   - running 且 handle 还在册：尾插还在飞。**慢变基**就落在这段窗口里（本机实测：
	//     300 提交的成功变基 ≈ 114s，而 Reclaim 只等 5s，见 jobs.Limits.DefaultWait）；
	//   - failed：尾插把现场留给了 leader（解冲突 / 变基 / 合并）。收口拆掉它等于把 leader
	//     要用的东西删了，分支也一并删——产出只剩 reflog。
	// 待验收（review）**不拦**：尾插已走完、合并已落地，释放它的现场是安全的；pending
	// 没有现场；running 而 handle 已作废（重启后）也不会再被合并使用（可重派/可清理）。
	if unsettled := c.unsettledItems(plan); len(unsettled) > 0 {
		return false, fmt.Errorf("teamwork: 还有 %d 件工作没落定（%s）——整队收口会把它们的现场一并拆掉，"+
			"先把账收干净：还在跑的先等它的回执（尾插会先合并、再插回执），或 jobs_manage(op=kill) 停掉；"+
			"失败/合并冲突/改动未合并的先人工处置（在它的 worktree 里解冲突、变基、合并）再 team_accept 销项，"+
			"要重做就 team_fail 后 team_dispatch 重派",
			len(unsettled), strings.Join(unsettled, ", "))
	}
	for _, member := range plan.Members {
		if _, err := c.closeStepsLocked(ctx, member.Role); err != nil {
			return false, err
		}
	}
	// 收口是**整队**动作，不只逐人退场：Work Item 口径下，"一个 Work Item 一个 Session
	// + 一个 worktree"的绑定的生命周期到此为止——所有还活着的绑定一并结束（这是
	// "team_done 之后 Session 与 worktree 如约关掉"的唯一落点）。
	if err := c.releaseAllItems(ctx, plan.TeamID); err != nil {
		return false, err
	}
	// 清掉这一轮团队的作业输出文件（S5 / §4.7 输出归属）：正文**活到收口**是因为
	// 文件归产品（销项 / 驱逐 / Close 都不由框架删），收口就是产品决定"不再需要"的
	// 那一刻。放在"标 closed + 审计"之前：清失败就还没收口，重试是一次干净的收口。
	if c.jobOutputs != nil {
		if err := c.jobOutputs.ClearJobOutputs(ctx); err != nil {
			return false, fmt.Errorf("teamwork: 收口清作业输出失败（域内尚未标 closed，可重试）: %w", err)
		}
	}
	if c.boards != nil {
		if err := c.boards.CloseTeamBoard(ctx); err != nil {
			return false, fmt.Errorf("teamwork: 收口封板失败（域内尚未标 closed，可重试）: %w", err)
		}
	}
	plan, err = c.store.ReadPlan(ctx, c.key)
	if err != nil {
		return false, err
	}
	plan.State.State = sessionstore.TeamworkStateClosed
	plan.State.ClosedAt = c.clock().UTC().Unix()
	plan.State.ClosedReason = sessionstore.BoardCloseTeamClose
	plan.State.Jobs = nil
	if err := c.store.WritePlan(ctx, c.key, plan, c.maxMembers); err != nil {
		return false, err
	}
	if err := c.audit(ctx, sessionstore.TeamworkEvent{
		Kind: sessionstore.TeamworkEventClose, TeamID: plan.TeamID,
		Detail: fmt.Sprintf("收口：回收 %d 名在编成员的作业 → 封板 %s", len(plan.Members), sessionstore.BoardCloseTeamClose),
	}); err != nil {
		return false, err
	}
	return false, nil
}

// observe 汇总一组句柄的只读读数，并返回仍在跑的条数。
func (c *Coordinator) observe(handles []jobs.Handle) ([]jobs.Record, int) {
	records := make([]jobs.Record, 0, len(handles))
	pending := 0
	for _, handle := range handles {
		record, ok := c.jobs.Observe(handle)
		if !ok {
			// 不在册（已销项或驱逐）：如实报，不把它当成"还在跑"。
			records = append(records, jobs.Record{
				Handle: handle, State: jobs.StateFailed, Degraded: true,
				Summary: "句柄不在册（已销项或驱逐）",
			})
			continue
		}
		if record.Running() {
			pending++
		}
		records = append(records, record)
	}
	return records, pending
}

// ensureCapacity 是**产品级**人数约束的闸门（§4.3）。
//
// 它必须先于框架的 jobs 在途上限生效：撞到框架兜底说明这条产品约束已经失效。
//
// 口径（2026-10-06 回收唯一化后）：作业只在一处回收（整队收口），因此"在跑作业数"在收口
// 之前并不等于"在编人数"——在编成员各自在跑的作业会一直占着名额。派发**同一个
// 角色**时不计它自己那一条：同一角色的重复派发会折叠到在跑的那一条（见 Dispatch 的 Dedup），
// 它不新增名额；不扣掉自己，重派同一个角色就会自己把自己顶在上限外
// （max_teammates = 在编人数时的必然撞车）。
func (c *Coordinator) ensureCapacity(role string) error {
	if c.maxMembers <= 0 {
		return nil
	}
	subject := SubjectForRole(role)
	running := 0
	for _, record := range c.jobs.Snapshot(jobs.Scope{Session: c.key.SessionID}) {
		if record.Kind != KindWorker || record.State != jobs.StateRunning {
			continue
		}
		if strings.TrimSpace(record.Scope.Subject) == subject {
			continue
		}
		running++
	}
	if running >= c.maxMembers {
		return fmt.Errorf(
			"teamwork: 在跑 teammate %d 人已达上限 limits.team.max_teammates=%d（显式拒绝，不静默排队）",
			running, c.maxMembers)
	}
	return nil
}

// milestoneDone 报告某个里程碑是否已 done（不在计划里 = 未完成，不静默放过）。
func milestoneDone(plan sessionstore.TeamworkPlan, id string) bool {
	for _, milestone := range plan.Milestones {
		if milestone.ID == id {
			return milestone.Status == sessionstore.TeamworkMilestoneDone
		}
	}
	return false
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}
