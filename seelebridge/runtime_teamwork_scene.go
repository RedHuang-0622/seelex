package seelebridge

// runtime_teamwork_scene.go — 团队现场（teammate / Work Item 级）的**认领**（重启恢复链的第三段）。
//
// 它补上 F4 那条链缺的一环：从**团队计划 + 绑定账本**得到 nodeID 列表，逐个把磁盘上
// 既有的 worktree 现场认领回注册表。
//
// 为什么必须有这一段：
//   - 子代理那条链的形状是 `beginNodeWorktree`（`Begin` → 立刻落持久节点记录）→ 宿主
//     恢复时 `Restore(records)` 回灌注册表 → `Prune()` 在后清理（顺序是判据的一部分）；
//   - teammate 现场走的却是 `BindWorkspace → WorktreeManager.BeginNamed`，**没有任何
//     持久登记**进入子代理的恢复名单（`NoteWorktree` 的调用点全仓只有 `beginNodeWorktree`
//     一处）。于是重启后它既不在册（`bindWorkerProjectRoot` 只能回退主会话根 = "去不了
//     目标的 worktree"），又紧接着被 `Prune` 的"不在册 + 干净"判据当孤儿删掉——目录与
//     `seelex/<nodeID>` 分支一起没了（F4 探针实测）。
//
// 持久事实 = **计划 + 账本**（leader 已定口径），不新造第二套目录扫描：计划里的
// `item.Worktree` / `member.Worktree` 与账本里的绑定行存的都是**指派名**
// （`seelex/<role>[-<item>]`），去前缀换回 worktree 管理器的注册键——换算只有
// `workItemNodeID` 一处，这里只复用，不重新拼命名。
//
// 顺序（`RestoreSubagentAnchors` 内）：先 `Restore`（子代理锚点）、再**认领**（团队现场）、
// 最后 `Prune`。认领必须早于 `Prune`，否则认领之前现场就已经被当孤儿删掉了。

import (
	"context"
	"strings"
)

// teamSceneNodeIDs 返回本会话团队现场的全部 nodeID（计划 + 账本，去重保序）。
//
// 两个来源都要读，缺一不可：
//   - **计划**：`item.Worktree`（派发时写进计划，是账本之外唯一的线索）与
//     `member.Worktree`（teammate 级现场）；
//   - **账本**：绑定流水（追加型 JSONL）。已释放（`Released`）的行表示这一份绑定已经
//     结束，不再认领。
//
// 未装配 teamwork 后端 / 会话没有作用域键 → 返回空：不猜、不扫目录（"恢复链只据计划 +
// 账本回灌注册表"）。
func (r *Runtime) teamSceneNodeIDs(sessionID string) []string {
	if r == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	r.teamworkMu.Lock()
	backend := r.teamworkBackend
	r.teamworkMu.Unlock()
	if backend == nil || backend.Store == nil || backend.KeyFor == nil {
		return nil
	}
	key, ok := backend.KeyFor(sessionID)
	if !ok || strings.TrimSpace(key.ProjectID) == "" || strings.TrimSpace(key.SessionID) == "" {
		return nil
	}
	ctx := context.Background()
	seen := make(map[string]bool, 4)
	nodeIDs := make([]string, 0, 4)
	add := func(assigned string) {
		nodeID := workItemNodeID(assigned)
		if nodeID == "" || seen[nodeID] {
			return
		}
		seen[nodeID] = true
		nodeIDs = append(nodeIDs, nodeID)
	}
	if plan, err := backend.Store.ReadPlan(ctx, key); err == nil {
		for _, member := range plan.Members {
			add(member.Worktree)
		}
		for _, milestone := range plan.Milestones {
			for _, item := range milestone.Items {
				add(item.Worktree)
			}
		}
	}
	if rows, err := backend.Store.ReadBindings(ctx, key); err == nil {
		// 用**原始行**而不经 `sessionstore.TeamworkBindings` 的折叠结果：折叠以
		// work_item 为键，会把 WorkItem 留空的 teammate 级行（leader 定的账本行形状）
		// 整行丢掉。
		for _, row := range rows {
			if row.Released {
				continue
			}
			add(row.Worktree)
		}
	}
	return nodeIDs
}

// adoptTeamworkScenes 把本会话团队现场里**磁盘上还有**的那些认领回注册表，返回认领
// 成功的现场数（诊断读数）。无 teamwork 装配 / 无现场 = 0（幂等 no-op）。
func (r *Runtime) adoptTeamworkScenes(sessionID string) int {
	if r == nil || r.worktreeMgr == nil {
		return 0
	}
	adopted := 0
	for _, nodeID := range r.teamSceneNodeIDs(sessionID) {
		if r.worktreeMgr.Adopt(nodeID) != nil {
			adopted++
		}
	}
	return adopted
}
