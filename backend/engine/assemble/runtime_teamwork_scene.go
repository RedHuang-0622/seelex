package assemble

// runtime_teamwork_scene.go — 团队现场（teammate / Work Item 级）**认领**的宿主面。
//
// 落点沿革（P21-26「域方法随类型走」）：**名单判据**（计划 + 账本 → nodeID 与归属，去重保序、
// 指派名去前缀的换算）搬进团队域 [`backend/multiagent/teamwork`](../../multiagent/teamwork/README.md)
// 的 `scene.go`（plan §2.7）；本文件只剩三件事：
//
//   - `teamSceneIndex` / `teamSceneNodeIDs`：**读**哪两份持久事实（计划 + 账本原始行）——读法与
//     容错（读不出来就当那一份为空）是宿主读数；
//   - `adoptTeamworkScenes`：把名单逐个交给 worktree 管理器**认领**（宿主动作）。
//
// 顺序提醒（`RestoreSubagentAnchors` 内）：先 `Restore`（子代理锚点）、再**认领**（团队现场）、
// 最后 `Prune`。认领必须早于 `Prune`，否则认领之前现场就已经被当孤儿删掉了。

import (
	"context"

	"github.com/RedHuang-0622/seelex/backend/multiagent/teamwork"
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
)

// teamSceneEntry 是域包 `SceneEntry` 的**同身份别名**（既有调用点与用例零改动）。
type teamSceneEntry = teamwork.SceneEntry

// teamSceneIndex 返回本会话团队现场的名单与归属（判据在域包；这里只读两份持久事实）。
//
// 容错与迁移前一致：计划读不出来就当它为空（只认账本），账本读不出来同理。
func (r *Runtime) teamSceneIndex(sessionID string) []teamSceneEntry {
	// 作用域解析只有一份判据（runtime_teamwork_scope.go）：会话号归一、装配检查与键完整性
	// 都不在这里重写。
	backend, _, key, err := r.resolveTeamworkScope(sessionID)
	if err != nil || backend.Store == nil {
		return nil
	}
	ctx := context.Background()
	plan, err := backend.Store.ReadPlan(ctx, key)
	if err != nil {
		// 计划读不出来就当它为空（只认账本），与迁移前的容错一致。
		plan = storeteamwork.Plan{}
	}
	bindings, err := backend.Store.ReadBindings(ctx, key)
	if err != nil {
		bindings = nil
	}
	return teamwork.SceneIndex(plan, bindings)
}

// teamSceneNodeIDs 返回本会话团队现场的全部 nodeID（判据在域包；这里一行转发）。
func (r *Runtime) teamSceneNodeIDs(sessionID string) []string {
	return teamwork.SceneNodeIDs(r.teamSceneIndex(sessionID))
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
