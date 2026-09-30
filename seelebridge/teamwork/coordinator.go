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
			plan.Members[index].RoleSessionID = c.derive(plan.TeamID, plan.Members[index].Role)
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
	if err := c.ensureCapacity(); err != nil {
		return "", "", err
	}
	stage := stageFor(plan, role)
	request := WorkerRequest{
		TeamID:        plan.TeamID,
		Role:          role,
		RoleSessionID: member.RoleSessionID,
		Subject:       SubjectForRole(role),
		Worktree:      member.Worktree,
		Stage:         stage,
		Goal:          goal,
		MaxTurns:      c.maxTurns,
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

// Retire 结束某 teammate 的一轮任务：**顺序固定**四步（D7 / §4.4）。
//
//  1. 回收该 teammate 名下未完成的作业（只动这一个 teammate）
//  2. 释放 worktree（节约存储；脏工作区由实现按语义报错，不静默丢弃）
//  3. 清空该 teammate 的会话记录内容（工作历史 + durable 快照）
//  4. 保留 teammate 在编，worktree 字段置空待重派
//
// 顺序不能换：先停作业再清记忆，否则会"清完记忆还在写"。
func (c *Coordinator) Retire(ctx context.Context, role string) error {
	plan, err := c.store.ReadPlan(ctx, c.key)
	if err != nil {
		return err
	}
	member, ok := memberFor(plan, role)
	if !ok {
		return fmt.Errorf("teamwork: 角色 %q 不在计划里", role)
	}
	// 步 1：只回收这一个主体名下的作业，同会话其他人不受影响。
	if err := c.jobs.Reclaim(ctx, jobs.Scope{Session: c.key.SessionID, Subject: SubjectForRole(role)}); err != nil {
		return fmt.Errorf("teamwork: retire 步 1（回收作业）失败: %w", err)
	}
	// 步 2：释放工作区。缺端口 = 显式报错：静默跳过会让 worktree 悄悄累积，
	// 而"teammate 不挂子代理 ⇒ worktree 数量被人数封顶"这条前提正是靠它成立。
	if c.worktrees == nil {
		return errors.New("teamwork: retire 步 2 需要 WorkspaceReleaser（未装配）")
	}
	if err := c.worktrees.Release(ctx, role); err != nil {
		return fmt.Errorf("teamwork: retire 步 2（释放工作区）失败: %w", err)
	}
	// 步 3：清会话内容（删的是对话记忆与工作区检出，不是注册/在编）。
	if c.sessions == nil {
		return errors.New("teamwork: retire 步 3 需要 SessionResetter（未装配）")
	}
	if err := c.sessions.Reset(ctx, member.RoleSessionID); err != nil {
		return fmt.Errorf("teamwork: retire 步 3（清会话内容）失败: %w", err)
	}
	// 步 4：留在编，worktree 指派名清空待重派；句柄投影一并清掉（它已不在册）。
	for index := range plan.Members {
		if plan.Members[index].Role == role {
			plan.Members[index].Worktree = ""
		}
	}
	delete(plan.State.Jobs, role)
	if err := c.store.WritePlan(ctx, c.key, plan, c.maxMembers); err != nil {
		return err
	}
	return c.audit(ctx, sessionstore.TeamworkEvent{
		Kind: sessionstore.TeamworkEventRetire, TeamID: plan.TeamID, Role: role,
		Detail: "回收作业 → 释放 worktree → 清会话内容 → 保在线",
	})
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
func (c *Coordinator) ensureCapacity() error {
	if c.maxMembers <= 0 {
		return nil
	}
	running := 0
	for _, record := range c.jobs.Snapshot(jobs.Scope{Session: c.key.SessionID}) {
		if record.Kind == KindWorker && record.State == jobs.StateRunning {
			running++
		}
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
