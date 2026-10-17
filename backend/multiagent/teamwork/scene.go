package teamwork

// scene.go 是**团队现场（teammate / Work Item 级）的名单判据**（P21-26「域方法随类型走」：从
// `backend/engine/assemble/runtime_teamwork_scene.go` 搬进本域；plan §2.7 把 `runtime_teamwork*.go`
// 列为 `backend/multiagent/teamwork/` 的目标）。
//
// 它补上重启恢复链缺的一环：从**团队计划 + 绑定账本**得到 nodeID 列表，供宿主把磁盘上既有的
// worktree 现场认领回注册表（`Adopt` 那一步是宿主动作，留在装配层）。
//
// 为什么必须有这一段：子代理那条链的形状是 `beginNodeWorktree`（`Begin` → 立刻落持久节点记录）
// → 宿主恢复时 `Restore(records)` 回灌注册表 → `Prune()` 在后清理；teammate 现场走的却是
// `BindWorkspace → WorktreeManager.BeginNamed`，**没有任何持久登记**进入子代理的恢复名单。
// 于是重启后它既不在册（只能回退主会话根 = "去不了目标的 worktree"），又紧接着被 `Prune` 的
// "不在册 + 干净"判据当孤儿删掉——目录与 `seelex/<nodeID>` 分支一起没了。
//
// 持久事实 = **计划 + 账本**（不新造第二套目录扫描）：两边的 Worktree 栏存的都是**指派名**，
// 去前缀换回注册键的换算只有 `NodeIDFromWorktreeName` 一处（正向拼装见 `WorkItemWorktreeName`
// 与 `TeammateWorktreeName`——两个方向现在同处一域）。

import (
	"strings"

	"github.com/RedHuang-0622/seelex/store"
)

// NodeIDFromWorktreeName 把 worktree 指派名折成 worktree 管理器的节点 id（去 `seelex/` 前缀；
// 空白的指派名归一为空串 = "没有键"）。
func NodeIDFromWorktreeName(worktreeName string) string {
	return strings.TrimPrefix(strings.TrimSpace(worktreeName), worktreeNamePrefix)
}

// SceneEntry 是一个团队现场的名单项：nodeID（契约命名，`<role>[-<itemID>]`）+ 归属（角色 /
// 工作项）。归属只是**标注**（审计与契约侧定位要用），不是"该拆哪一级"的判据。
type SceneEntry struct {
	NodeID   string
	Role     string
	WorkItem string
}

// SceneIndex 返回团队现场的名单与归属（计划 + 账本原始行，按 nodeID 去重保序）。
//
// 两个来源都要读，缺一不可：
//   - **计划**：`item.Worktree`（派发时写进计划，是账本之外唯一的线索）与 `member.Worktree`
//     （teammate 级现场）；
//   - **账本**：绑定流水。已释放（`Released`）的行表示这一份绑定已经结束，不再认领；这里刻意
//     用**原始行**而不经 `store.TeamworkBindings` 的折叠结果——折叠以 work_item 为键，会把
//     WorkItem 留空的 teammate 级行整行丢掉。
//
// 次序 = 先计划（成员 → 工作项）后账本，去重保序：调用方的"先认领后清残留"依赖这份名单的
// 稳定性而不是顺序本身，但顺序稳定让读数可复现。
func SceneIndex(plan store.TeamworkPlan, bindings []store.TeamworkBinding) []SceneEntry {
	seen := make(map[string]bool, 4)
	entries := make([]SceneEntry, 0, 4)
	add := func(assigned, role, itemID string) {
		nodeID := NodeIDFromWorktreeName(assigned)
		if nodeID == "" || seen[nodeID] {
			return
		}
		seen[nodeID] = true
		entries = append(entries, SceneEntry{
			NodeID: nodeID, Role: strings.TrimSpace(role), WorkItem: strings.TrimSpace(itemID),
		})
	}
	for _, member := range plan.Members {
		add(member.Worktree, member.Role, "")
	}
	for _, milestone := range plan.Milestones {
		for _, item := range milestone.Items {
			add(item.Worktree, item.Role, item.ID)
		}
	}
	for _, row := range bindings {
		if row.Released {
			continue
		}
		add(row.Worktree, row.Role, row.WorkItem)
	}
	return entries
}

// SceneNodeIDs 是 SceneIndex 的只取名字的投影（认领现场只用得上名字）。
func SceneNodeIDs(entries []SceneEntry) []string {
	if len(entries) == 0 {
		return nil
	}
	nodeIDs := make([]string, 0, len(entries))
	for _, entry := range entries {
		nodeIDs = append(nodeIDs, entry.NodeID)
	}
	return nodeIDs
}

// SceneOwnsNode 报告一个 nodeID 是否在本会话团队的现场名单里（记录归属过滤的唯一判据）。
//
// 为什么需要它：teammate 的会话记录与子代理节点记录**共用**同一个 `subagents/` 目录，
// 读回时必须按现场名单过滤——只有落在名单里的记录才属于本团队的单元。名单本身来自
// `SceneIndex`（已去重、已 trim）；这里只回答"在不在这份名单里"，不另读持久事实。
func SceneOwnsNode(entries []SceneEntry, nodeID string) bool {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return false
	}
	for _, entry := range entries {
		if entry.NodeID == nodeID {
			return true
		}
	}
	return false
}
