package seelebridge

import (
	"context"
	"fmt"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelebridge/worktree"
	"github.com/RedHuang-0622/seelex/seelebridge/workunit"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// ── subagent 层的工作单元适配器（workunit.Unit 的第一份实现）────────────────
//
// 契约（seelebridge/workunit）：一个 Unit = 一件事 = 一份现场 + 一条会话，
// 四个动作 Begin/Finish/Reclaim/Recover 三层**只写一份**实现。本文件是 subagent
// 层的那一份：
//
//	Kind    —— KindSubagent（job 层不实现 Unit，见契约包注释）；
//	Begin   —— 既有 beginNodeWorktree（幂等；降级返回空现场）；
//	Finish  —— 既有 finishNodeWorktree + 契约的 ClassifyFinish；
//	Reclaim —— 既有 releaseNodeWorktree + 删掉会话账本里的记录；
//	Recover —— 既有 RestoreSubagentAnchors（认领现场必须在 Prune 之前）+ 契约
//	           的 Resume 读数。
//
// **薄**：这里只转调既有方法，不重写内部逻辑——现场语义、git 纪律、恢复顺序
// 都留在既有实现里。策略（FinishPolicy）是本层与 teammate 层**唯一**允许出现的
// 差异点：subagent 的现场是临时的，收尾即回收（Immediate）。
//
// 装配面：实现要能拿到 Runtime（本文件与 Runtime 同包），构造入口不导出——它只是
// Runtime 内部的接线，不需要新的公开面。

// nodeWorkUnit 是 subagent 层的一个工作单元（一件活 = 一份现场 + 一条会话）。
type nodeWorkUnit struct {
	runtime       *Runtime
	mainSessionID string          // 会话账本的主键（结论跟随 mainagent）
	nodeID        string          // 现场与会话的定位键：本层就是节点 id
	scope         model.NodeScope // 建现场用的作用域（角色决定有无独立现场）
}

// 编译期钉住"这就是 workunit.Unit 的一份实现"：契约漂移先红。
var _ workunit.Unit = (*nodeWorkUnit)(nil)

// newNodeWorkUnit 组装 subagent 层的工作单元（依赖构造式注入，不读全局）。
func (r *Runtime) newNodeWorkUnit(mainSessionID, nodeID string, scope model.NodeScope) *nodeWorkUnit {
	return &nodeWorkUnit{runtime: r, mainSessionID: mainSessionID, nodeID: nodeID, scope: scope}
}

// Kind 报告这一层：subagent。
func (u *nodeWorkUnit) Kind() workunit.Kind { return workunit.KindSubagent }

// FinishPolicy 回答"什么时候回收"：subagent 的现场临时，收尾即回收。
func (u *nodeWorkUnit) FinishPolicy() workunit.FinishPolicy { return workunit.Immediate{} }

// Begin 建现场：转调既有 beginNodeWorktree。幂等（同一 nodeID 复用同一现场）；
// 非 git 仓库或 entry 角色降级为共享工作区，此时 Scene.Worktree 为空——按契约，
// "空 = 这件事没有独立现场"。
//
// 会话不在这里建：本层的会话由节点 Run 自己开（工厂 NewAgent +
// RegisterNodeSession），Begin 只认领/创建现场，不越权替它开一条会话。
func (u *nodeWorkUnit) Begin(context.Context) (workunit.Scene, error) {
	scene := workunit.Scene{Kind: workunit.KindSubagent, NodeID: u.nodeID}
	// 指派名与现场的分支名是同一条约定（`seelex/<nodeID>`）：这里直接取现场的
	// Branch，不另拼一次命名（契约要求换算只有一处）。
	if wt := u.runtime.beginNodeWorktree(u.scope, u.nodeID); wt != nil {
		scene.Worktree = wt.Branch
	}
	return scene, nil
}

// Finish 收尾这一轮：既有 finishNodeWorktree（变基兜底 → 提交判定 → 审批 →
// merge → 清理）拿到合并错误，再交给契约的 ClassifyFinish 分类。
//
// mergeErr 是调用方**已经拿到**的合并错误；为空时由本方法触发收尾。返回的 error
// 只表示"收尾这个动作没法做"，合并失败的结论走 Outcome.Kind——判死与否由调用方
// 按 ClassifyFinish 的口径决定（node 侧见 AgentNode.Run）。
func (u *nodeWorkUnit) Finish(ctx context.Context, result workunit.Result, mergeErr error) (workunit.Outcome, error) {
	if u.runtime == nil {
		return workunit.Outcome{}, fmt.Errorf("workunit: runtime is nil")
	}
	if mergeErr == nil {
		mergeErr = u.runtime.finishNodeWorktree(ctx, u.nodeID, u.runtime.nodeWorktreeFor(u.nodeID))
	}
	return workunit.ClassifyFinish(result, mergeErr), nil
}

// Reclaim 拆这一份：释放现场（既有 releaseNodeWorktree）+ 删除会话账本里的记录。
// 幂等——现场不在册、记录已删，都是"已经回收过"，不报错。
//
// 读账本走**契约的** workunit.SessionLedger 端口（*sessionstore.NodeSessionStore
// 结构上就满足它）；删除用存储自己的 Delete（契约的端口只声明 Save/List——清会话
// 是本层要做的动作，不需要为它扩端口）。
func (u *nodeWorkUnit) Reclaim(context.Context) error {
	if u.runtime == nil {
		return fmt.Errorf("workunit: runtime is nil")
	}
	u.runtime.releaseNodeWorktree(u.nodeID)
	record, found, err := u.ledgerRecord()
	if err != nil {
		return err
	}
	if !found {
		return nil // 没有会话记录可删（未装配持久化，或已经回收过）
	}
	if err := u.runtime.nodeSessionStore.Delete(u.projectID(), u.mainSessionID, record.SessionID); err != nil {
		return fmt.Errorf("workunit: reclaim session record for %q: %w", u.nodeID, err)
	}
	return nil
}

// Recover 重启回灌：转调既有恢复链——RestoreSubagentAnchors 从残留记录重建详情
// 数据面/子代理树/现场认领（**必须在 Prune 之前**，否则"干净但还没合并"的现场会被
// 当孤儿删掉），再按契约的 Resume 形状报回有界读数。
func (u *nodeWorkUnit) Recover(context.Context) (workunit.Resume, error) {
	if u.runtime == nil {
		return workunit.Resume{}, fmt.Errorf("workunit: runtime is nil")
	}
	if err := u.runtime.RestoreSubagentAnchors(u.mainSessionID); err != nil {
		return workunit.Resume{}, err
	}
	record, found, err := u.ledgerRecord()
	if err != nil {
		return workunit.Resume{}, err
	}
	if !found {
		return workunit.Resume{}, nil
	}
	resume := workunit.Resume{Sessions: 1}
	if u.runtime.nodeWorktreeFor(u.nodeID) != nil {
		resume.Scenes = 1
	}
	// 记录说在跑（queued/running）而本进程已无它的执行面 ⇒ 中断：交上层重跑或
	// 人工处置。判定复用既有的 recordBelongsToCurrentMain（不另立第二条判据）。
	if record.Status == "queued" || record.Status == "running" {
		if u.runtime.recordBelongsToCurrentMain(record) {
			resume.Interrupted = []string{u.nodeID}
		}
	}
	return resume, nil
}

// ledgerRecord 从会话账本里读这一件事的记录（无存储/无记录 → found=false）。
func (u *nodeWorkUnit) ledgerRecord() (sessionstore.NodeSessionRecord, bool, error) {
	store := u.runtime.nodeSessionStore
	if store == nil {
		return sessionstore.NodeSessionRecord{}, false, nil
	}
	records, err := workunit.SessionLedger(store).List(u.projectID(), u.mainSessionID)
	if err != nil {
		return sessionstore.NodeSessionRecord{}, false,
			fmt.Errorf("workunit: list session records for %q: %w", u.nodeID, err)
	}
	for _, record := range records {
		if record.NodeID == u.nodeID {
			return record, true, nil
		}
	}
	return sessionstore.NodeSessionRecord{}, false, nil
}

// projectID 是会话账本的读取键之一（与恢复/清理链同一解析口径）。
func (u *nodeWorkUnit) projectID() string {
	return u.runtime.sessionProjectIDFor(u.mainSessionID)
}

// nodeWorktreeFor 返回节点在册的现场（无管理器/无现场 → nil）。
func (r *Runtime) nodeWorktreeFor(nodeID string) *worktree.NodeWorktree {
	if r == nil || r.worktreeMgr == nil {
		return nil
	}
	return r.worktreeMgr.WorktreeForNode(nodeID)
}
