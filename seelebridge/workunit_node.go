package seelebridge

import (
	"context"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelebridge/workunit"
)

// ── subagent 层的**注册点**（在契约那份父实现上注册 + 转发）────────────────
//
// 生命周期**不在这里**：它在父实现（workunit_parent.go 的 lifecycleHost，契约面 =
// `workunit.Lifecycle`）里，只有一份。本文件只做两件事：
//
//	1. 注册：把"我是谁"（Kind / ID / SessionPath / Policy / 归属）声明成一份**读数**
//	   （nodeUnitReadings，实现 `workunit.Unit`）；
//	2. 转发：四个动作一律 `return u.host.Xxx(ctx, u.read)`。
//
// 结构体只持有两样东西：`workunit.Lifecycle` 与自己的读数——拿不到 store / worktree /
// jobs / 账本任何一个端口，这就是"没有第二份实现"的结构证据。层差异只由读数表达：
// subagent 的现场按角色给（entry 角色共享主工作区），收尾即回收（Immediate），
// 没有团队归属。

// nodeUnitReadings 是 subagent 层的**读数**（实现 `workunit.Unit`）：只有身份与策略。
type nodeUnitReadings struct {
	mainSessionID string
	nodeID        string
	role          string
}

// Kind 报告这一层：subagent（恒定读数：本注册点只服务这一层）。
func (u nodeUnitReadings) Kind() workunit.Kind { return workunit.KindSubagent }

// ID 是本单元现场的身份：本层就是节点 id。
func (u nodeUnitReadings) ID() string { return u.nodeID }

// SessionPath 是本层的会话路径 = 主会话号（账本键的解析在父里）。
func (u nodeUnitReadings) SessionPath() string { return u.mainSessionID }

// Policy 是本层的收尾策略：subagent 的现场是临时的（收尾即回收）。
func (u nodeUnitReadings) Policy() workunit.FinishPolicy { return workunit.Immediate{} }

// Owns 是归属读数：subagent 单元没有团队归属（TeamID / ItemID 留空），只有角色。
func (u nodeUnitReadings) Owns() workunit.Ownership {
	return workunit.Ownership{Role: u.role}
}

// 编译期钉住"读数就是契约的 Unit"：契约漂移先红。
var _ workunit.Unit = nodeUnitReadings{}

// nodeWorkUnit 是 subagent 层的注册点：父实现（契约面）+ 本层读数，方法体只转发。
type nodeWorkUnit struct {
	host workunit.Lifecycle
	read nodeUnitReadings
}

// newNodeWorkUnit 组装 subagent 层的注册点：父实现 + 本层读数（依赖构造式注入）。
func (r *Runtime) newNodeWorkUnit(mainSessionID, nodeID string, scope model.NodeScope) *nodeWorkUnit {
	return &nodeWorkUnit{
		host: newLifecycleHost(r),
		read: nodeUnitReadings{
			mainSessionID: mainSessionID,
			nodeID:        nodeID,
			role:          string(scope.Role),
		},
	}
}

// FinishPolicy 是本层声明的收尾策略读数（转发给读数；恒定的常量，因此 nil 接收者也能回答）。
func (u *nodeWorkUnit) FinishPolicy() workunit.FinishPolicy { return nodeUnitReadings{}.Policy() }

// Begin 建现场：唯一实现在父里。
func (u *nodeWorkUnit) Begin(ctx context.Context) (workunit.Scene, error) {
	return u.host.Begin(ctx, u.read)
}

// Finish 收尾这一轮（分类 + 合并 + 回执；不拆现场）：唯一实现在父里。落定之后按策略
// 回收（`Immediate`）——调的是**同一个** `Lifecycle.Reclaim`。
func (u *nodeWorkUnit) Finish(ctx context.Context, result workunit.Result, mergeErr error) (workunit.Outcome, error) {
	return u.host.Finish(ctx, u.read, result, mergeErr)
}

// Reclaim 拆这一份（拆现场 + 清会话记录；幂等）：唯一实现在父里。
func (u *nodeWorkUnit) Reclaim(ctx context.Context) error {
	return u.host.Reclaim(ctx, u.read)
}

// Recover 重启回灌（会话级读回 + 认领现场 + 中断读数）：唯一实现在父里。
func (u *nodeWorkUnit) Recover(ctx context.Context) (workunit.Resume, error) {
	return u.host.Recover(ctx, u.read)
}
