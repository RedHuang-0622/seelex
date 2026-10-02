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

// Audit 读回审计流水（派发 / 里程碑 / 退场的事实顺序）。
func (c *Coordinator) Audit(ctx context.Context) ([]sessionstore.TeamworkEvent, error) {
	return c.store.ReadEvents(ctx, c.key)
}

// SetPlan 写入（整份替换）一份计划，并把这次改写记进审计面。
//
// 缺 role_session_id 的成员按 (team_id, role) 补齐：派生是幂等的，于是
// "leader 只写角色名"也能得到一份合法计划，而不是被校验挡回来。
func (c *Coordinator) SetPlan(ctx context.Context, plan sessionstore.TeamworkPlan) error {
	for index := range plan.Members {
		if strings.TrimSpace(plan.Members[index].RoleSessionID) == "" {
			plan.Members[index].RoleSessionID = c.derive(c.key.SessionID, plan.TeamID, plan.Members[index].Role)
		}
	}
	if err := c.store.WritePlan(ctx, c.key, plan, c.maxMembers); err != nil {
		return err
	}
	return c.audit(ctx, sessionstore.TeamworkEvent{
		Kind:   sessionstore.TeamworkEventPlan,
		TeamID: plan.TeamID,
		Detail: fmt.Sprintf("stages=%d members=%d milestones=%d", len(plan.Stages), len(plan.Members), len(plan.Milestones)),
	})
}

// Dispatch 派发一个 teammate 的作业：受理回执即 handle，调用方不等待。
//
// 顺序（谁先谁后）不在这里判定——它在计划的 depends_on 里，由 leader 掌控调
// 用时机（§6.2）。这里只管三件事：角色在编、人数未满、作用域正确。
func (c *Coordinator) Dispatch(ctx context.Context, role, goal string) (jobs.Handle, string, error) {
	if c.workers == nil {
		return "", "", errors.New("teamwork: team_dispatch 需要 WorkerRunner（未装配）")
	}
	plan, err := c.store.ReadPlan(ctx, c.key)
	if err != nil {
		return "", "", err
	}
	member, ok := memberFor(plan, role)
	if !ok {
		return "", "", fmt.Errorf("teamwork: 角色 %q 不在计划里（一角色一 teammate；先 team_plan 增补）", role)
	}
	if err := c.ensureCapacity(role); err != nil {
		return "", "", err
	}
	stage := stageFor(plan, role)
	groups, err := memberPermissionGroups(member)
	if err != nil {
		return "", "", err
	}
	// 输出归属（§4.7 / S5）：装配了产品输出面就把这一轮的正文交给产品自有文件，
	// 否则交回框架自建（并按框架语义在销项 / 驱逐 / Close 时被删）。分配失败不降级
	// 成"悄悄退回框架文件"——那会让"正文活到 close"这条保证时真时假。
	outputPath := ""
	if c.jobOutputs != nil {
		path, err := c.jobOutputs.JobOutputPath(ctx, role)
		if err != nil {
			return "", "", fmt.Errorf("teamwork: 分配作业输出路径失败: %w", err)
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
		Worktree:         member.Worktree,
		Stage:            stage,
		Goal:             goal,
		MaxTurns:         c.maxTurns,
		OutputPath:       outputPath,
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return "", "", fmt.Errorf("teamwork: 编码 worker 载荷: %w", err)
	}
	handle, err := c.jobs.Dispatch(ctx, jobs.Spec{
		Kind:  KindWorker,
		Scope: jobs.Scope{Session: c.key.SessionID, Subject: request.Subject},
		Node:  stage,
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
		return "", "", err
	}
	// 派发成功后才写状态：句柄只是**投影**（jobs I-4），不在册的值一律视为过期。
	plan.State.Stage = stage
	if plan.State.Jobs == nil {
		plan.State.Jobs = map[string]string{}
	}
	plan.State.Jobs[role] = describeHandle(handle)
	if err := c.store.WritePlan(ctx, c.key, plan, c.maxMembers); err != nil {
		return handle, stage, err
	}
	if err := c.audit(ctx, sessionstore.TeamworkEvent{
		Kind: sessionstore.TeamworkEventDispatch, TeamID: plan.TeamID, Stage: stage,
		Role: role, Handle: describeHandle(handle), Node: stage, Detail: goal,
	}); err != nil {
		return handle, stage, err
	}
	return handle, stage, nil
}

// Join 是一次**有界**汇合等待：只在该汇合点真有依赖时用（§6.2 / §6.3 的 A 姿势）。
//
// 它只**观察**，不取回：取回是消费式的，会把 teammate 的输出从工作表格上拿走。
func (c *Coordinator) Join(ctx context.Context, handles []jobs.Handle, budget time.Duration) ([]jobs.Record, error) {
	if len(handles) == 0 {
		return nil, nil
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
// 判据是**依赖边**而不是墙钟：after 里的每个阶段都必须至少派发过一次 teammate
// ——否则"里程碑"就会变成一句没有事实支撑的口号。
func (c *Coordinator) Milestone(ctx context.Context, id, content string) error {
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
	for _, stageID := range plan.Milestones[index].After {
		if !stageDispatched(plan, stageID) {
			return fmt.Errorf("teamwork: 里程碑 %q 的阶段 %q 还没有派发过任何 teammate（里程碑只能声明在依赖边之后）", id, stageID)
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

// retireSteps 是 Retire（单个人一轮结束）与 Close（整队收口）复用的**同一套**四步实现
// （D7 / §4.4）。两者只有一处不同：**回收作业**只发生在整队收口时（reclaim=true）。
//
// 为什么把回收从 Retire 里摘出来（2026-10-02，要求④）：Retire 不再 Reclaim 之后，
// "谁还在跑"在整队 Close 之前一直留在册上（作业正文不会被一次退场悄悄撤走），回收
// 统一收口到 Close 这一处；实现仍只有一份，用一个开关区分，不复制四步。
//
//  1. reclaim=true 时回收该 teammate 名下未完成的作业（只动这一个 teammate）
//  2. 释放 worktree（节约存储；脏工作区由实现按语义报错，不静默丢弃）
//  3. 清空该 teammate 的会话记录内容（工作历史 + durable 快照）
//  4. 保留 teammate 在编，worktree 字段置空待重派；回收时清掉句柄投影
//
// 顺序不能换：先停作业再清记忆，否则会"清完记忆还在写"。
func (c *Coordinator) retireSteps(ctx context.Context, role string, reclaim bool) (sessionstore.TeamworkPlan, error) {
	plan, err := c.store.ReadPlan(ctx, c.key)
	if err != nil {
		return plan, err
	}
	member, ok := memberFor(plan, role)
	if !ok {
		return plan, fmt.Errorf("teamwork: 角色 %q 不在计划里", role)
	}
	// 步 1：只回收这一个主体名下的作业，同会话其他人不受影响。整队收口（Close）才做。
	if reclaim {
		if err := c.jobs.Reclaim(ctx, jobs.Scope{Session: c.key.SessionID, Subject: SubjectForRole(role)}); err != nil {
			return plan, fmt.Errorf("teamwork: 收口步 1（回收作业）失败: %w", err)
		}
	}
	// 步 2：释放工作区。缺端口 = 显式报错：静默跳过会让 worktree 悄悄累积，
	// 而"teammate 不挂子代理 ⇒ worktree 数量被人数封顶"这条前提正是靠它成立。
	if c.worktrees == nil {
		return plan, errors.New("teamwork: 退场步 2 需要 WorkspaceReleaser（未装配）")
	}
	if err := c.worktrees.ReleaseWorkspace(ctx, role); err != nil {
		return plan, fmt.Errorf("teamwork: 退场步 2（释放工作区）失败: %w", err)
	}
	// 步 3：清会话内容（删的是对话记忆与工作区检出，不是注册/在编）。
	if c.sessions == nil {
		return plan, errors.New("teamwork: 退场步 3 需要 SessionResetter（未装配）")
	}
	if err := c.sessions.ResetSession(ctx, member.RoleSessionID); err != nil {
		return plan, fmt.Errorf("teamwork: 退场步 3（清会话内容）失败: %w", err)
	}
	// 步 4：留在编，worktree 指派名清空待重派；句柄投影只在回收时清（没回收时作业
	// 还在册，投影照实留着——它是看板上的"谁还在跑"）。
	for index := range plan.Members {
		if plan.Members[index].Role == role {
			plan.Members[index].Worktree = ""
		}
	}
	if reclaim {
		delete(plan.State.Jobs, role)
	}
	if err := c.store.WritePlan(ctx, c.key, plan, c.maxMembers); err != nil {
		return plan, err
	}
	return plan, nil
}

// Retire 结束某 teammate 的一轮任务：**顺序固定**四步（D7 / §4.4），与 Close 复用同一
// 套实现，两者只差一处——Retire **不回收作业**（回收统一收口到 Close，见 retireSteps）。
func (c *Coordinator) Retire(ctx context.Context, role string) error {
	plan, err := c.retireSteps(ctx, role, false)
	if err != nil {
		return err
	}
	return c.audit(ctx, sessionstore.TeamworkEvent{
		Kind: sessionstore.TeamworkEventRetire, TeamID: plan.TeamID, Role: role,
		Detail: "释放 worktree → 清会话内容 → 保在线（作业在整队 close 前仍在册）",
	})
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
	plan, err := c.store.ReadPlan(ctx, c.key)
	if err != nil {
		return false, err
	}
	if plan.State.State == sessionstore.TeamworkStateClosed {
		return true, nil
	}
	for _, member := range plan.Members {
		if _, err := c.retireSteps(ctx, member.Role, true); err != nil {
			return false, err
		}
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
// 口径（2026-10-02 收口改造后）：Retire 不再回收作业，"在跑作业数"因此不再等于"未被
// 退场的在编人数"——已退场但作业仍在跑的成员会一直占着名额。派发**同一个角色**时不
// 计它自己那一条：同一角色的重复派发会折叠到在跑的那一条（见 Dispatch 的 Dedup），
// 它不新增名额；不扣掉自己，退场后重派同一个角色就会自己把自己顶在上限外
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

// stageDispatched 报告某个阶段是否已经派发过 teammate。
func stageDispatched(plan sessionstore.TeamworkPlan, stageID string) bool {
	for role := range plan.State.Jobs {
		if stageFor(plan, role) == stageID {
			return true
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
