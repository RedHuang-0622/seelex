package seelebridge

// runtime_teamwork_items.go — Work Item 口径的运行时端口实现：
//
//   - teamwork.Workspaces：**一个 Work Item 一个 worktree** 的建 / 并 / 释放；
//   - teamwork.TeammateQueue：尾插的落点（teammate 的消息队列）；
//   - teamwork.ItemSettler：worker 回合结束后的**自动**尾插接线。
//
// 分工：协调器（seelebridge/teamwork）只认识"这件事的 id 与指派名"，git 目录布局、
// 项目根、会话作用域都在这里接上——**上层不认识 git，下层不认识编排**。

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/seelebridge/teamwork"
	"github.com/RedHuang-0622/seelex/seelebridge/worktree"
)

// SettleWorkItem 实现 teamwork.ItemSettler：worker 一轮跑完之后**自动**做尾插
// （先合并这件事的 worktree，再把有界回执插进 teammate 的消息队列）。
//
// 宿主从载荷里取（作业会活过派发它的那一轮，作业的 ctx 上没有主会话）——与
// Runtime.RunWorker 取宿主的口径一致。
func (r *Runtime) SettleWorkItem(ctx context.Context, request teamwork.WorkerRequest, runErr error) error {
	if strings.TrimSpace(request.WorkItemID) == "" {
		return nil // 非 Work Item 口径的派发：没有尾插的落点
	}
	coordinator, err := r.coordinatorForSession(request.MainSessionID)
	if err != nil {
		return err
	}
	// 看板缓存在**这里也要失效**：settle 是**不是工具调用**的那条写路径（teammate 跑完
	// 自动尾插：工作项状态 → 待验收/失败 + 回执进消息队列 + 一条 settle 审计行）。
	// 只靠 team_* 工具返回后失效的话，看板会一直显示"这件事还在跑、没有回执"，直到下一次
	// team_* 调用把它撞醒——现场（2026-10-04 headless 冒烟）：落盘已经是 review，
	// 看板还停在 running、Messages 空，于是"跑完了看不见回执"又出现一次，只是这次
	// 根因在投影缓存，不在尾插。
	defer r.invalidateTeamworkBoard()
	// 收尾分类是**同一份**（workunit.ClassifyFinish，在 settleWorkItem 步 3 里算），
	// 这里取它的读数只为把"这一轮怎么结束的"写进 teammate 单元的会话记录
	// （见 workunit_team.go：记录 = 重启回灌的依据）。
	outcome, err := coordinator.SettleWorkItemOutcome(ctx, request, runErr)
	if err != nil {
		return err
	}
	r.settleTeamUnitRecord(request.MainSessionID, teamUnitKeyFor(request), outcome)
	return nil
}

// BindWorkspace 实现 teamwork.Workspaces：为这件事建（或复用）一个 git worktree。
//
// 降级是**显式**的：没装配 git 面 / 不是 git 仓库 / 创建失败 → 返回未补现场的绑定，
// teammate 这一轮就落在主工作区上（与 worktree_manager.Begin 的既有降级语义一致）。
// 降级不等于失败：工作照做，只是没有隔离。
func (r *Runtime) BindWorkspace(_ context.Context, binding teamwork.WorkspaceBinding) (teamwork.WorkspaceBinding, error) {
	if r == nil || r.worktreeMgr == nil {
		return binding, nil
	}
	nodeID := workItemNodeID(binding.Worktree)
	if nodeID == "" {
		return binding, nil
	}
	wt := r.worktreeMgr.BeginNamed(nodeID)
	if wt == nil {
		return binding, nil
	}
	binding.Path = wt.Path
	binding.Branch = wt.Branch
	return binding, nil
}

// MergeWorkspace 实现 teamwork.Workspaces：把这件事的改动并回主工作区。
//
// 语义沿用 worktree 的收尾协议（WorktreeManager.Finish）：先变基兜底，再看有没有
// 提交——没有提交就是没有可并的改动（顺手清现场）；**有未提交改动却没有提交**时
// 显式报错（ErrUncommittedChanges 语义，现场保留）。这一条正是尾插正文里
// "插入失败、请 leader 亲自执行"的来源：产出是人的资产，框架不替人决定丢还是留。
func (r *Runtime) MergeWorkspace(ctx context.Context, binding teamwork.WorkspaceBinding) error {
	if r == nil || r.worktreeMgr == nil {
		return nil
	}
	nodeID := workItemNodeID(binding.Worktree)
	if nodeID == "" {
		return nil
	}
	wt := r.worktreeMgr.WorktreeForNode(nodeID)
	if wt == nil {
		return nil // 没有现场可合并（降级共享主工作区）
	}
	if err := r.worktreeMgr.Finish(ctx, nodeID, wt); err != nil {
		return err
	}
	// 收尾成功 = 现场已在**磁盘上**被回收（Finish 的 cleanup 已 `git worktree remove`）。
	// 必须配一次 Release 把**注册表**也清掉——否则账本/注册表里还留着一份"已不存在的
	// 绑定"，accept 的释放步骤会对它再动手一次（缺陷 A：exit status 128）。这与
	// node/AgentNode.Run 成功分支的 Finish→Release 是同一口径（`Finish` 的两条成功
	// 路径都清目录，调用方负责配对 Release）。
	r.worktreeMgr.Release(nodeID)
	return nil
}

// ReleaseWorkspaceItem 实现 teamwork.Workspaces：释放这件事的现场（验收通过 /
// 整队收口时调用）。无现场 = 无可释放（幂等）。
func (r *Runtime) ReleaseWorkspaceItem(_ context.Context, binding teamwork.WorkspaceBinding) error {
	if r == nil || r.worktreeMgr == nil {
		return nil
	}
	nodeID := workItemNodeID(binding.Worktree)
	if nodeID == "" {
		return nil
	}
	wt := r.worktreeMgr.WorktreeForNode(nodeID)
	if wt == nil {
		r.worktreeMgr.Release(nodeID)
		return nil
	}
	root := r.workspaceRootFor(binding.MainSessionID)
	if root != "" {
		if err := worktree.CleanupWorktree(root, wt); err != nil {
			return fmt.Errorf("teamwork: 清理工作项 %q 的 worktree 失败: %w", binding.WorkItem, err)
		}
	}
	r.worktreeMgr.Release(nodeID)
	return nil
}

// EnqueueTeammateMessage 实现 teamwork.TeammateQueue：把尾插消息记进 teammate 的
// 消息队列（审计流里的 message 行，按角色会话号读）。
func (r *Runtime) EnqueueTeammateMessage(ctx context.Context, message teamwork.TeammateMessage) error {
	coordinator, err := r.coordinatorForSession(message.MainSessionID)
	if err != nil {
		return err
	}
	return coordinator.AppendTeammateMessage(ctx, message)
}

// workItemNodeID 把 worktree 指派名折成 worktree 管理器的节点 id。
//
// 命名是**同一条**：指派名 `seelex/<role>-<item>` 去掉分支前缀就是节点 id
// （worktree_manager 的 branch = "seelex/" + nodeID）。两处各算一次命名，迟早算成
// 两个目录——所以这里只做"去前缀"，不重新拼。
func workItemNodeID(worktreeName string) string {
	return strings.TrimPrefix(strings.TrimSpace(worktreeName), "seelex/")
}

// workspaceRootFor 返回某宿主会话的项目根（无 = 空串）。
func (r *Runtime) workspaceRootFor(sessionID string) string {
	if r == nil || r.projectScope == nil {
		return ""
	}
	return strings.TrimSpace(r.projectScope.RootFor(strings.TrimSpace(sessionID)))
}

// coordinatorForSession 按**主会话号**取（必要时建）该会话的协调器。
//
// 尾插与消息队列都发生在**作业的执行体**里，那里的 ctx 上没有主会话（作业 ctx 由
// jobs.Manager 从 Background 派生）——所以宿主必须从载荷里带过来，这是唯一可靠的
// 取法（见 WorkerRequest.MainSessionID 的说明）。
func (r *Runtime) coordinatorForSession(sessionID string) (*teamwork.Coordinator, error) {
	if r == nil {
		return nil, errors.New("teamwork: runtime 为空")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("teamwork: 缺少宿主主会话号（尾插必须有归属）")
	}
	r.teamworkMu.Lock()
	backend := r.teamworkBackend
	r.teamworkMu.Unlock()
	if backend == nil || backend.KeyFor == nil {
		return nil, errors.New("teamwork: leader 编排未装配（缺 SetTeamworkBackend）")
	}
	key, ok := backend.KeyFor(sessionID)
	if !ok {
		return nil, fmt.Errorf("teamwork: 无法解析会话 %q 的作用域键（项目/会话）", sessionID)
	}
	return r.coordinatorForKey(key)
}
