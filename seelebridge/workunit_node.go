package seelebridge

import (
	"context"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelebridge/workunit"
)

// ── subagent 层的工作单元**注册点**（workunit.Unit 的一份实现）────────────────
//
// 生命周期**不在这里**：它在父实现（workunit_parent.go 的 lifecycleHost）里，只有一份。
// 本文件只做两件事：
//
//	1. 注册：把"我是谁"（Kind / NodeID / SessionPath / Policy / 归属）声明清楚；
//	2. 转发：四个动作一律 `return u.host.Xxx(ctx, u.layer)`。
//
// 方法体里**不许**出现 store / worktree / jobs / 账本的写入——本文件（以及 teammate 的
// 注册点）搜不到任何一个写端口，这就是"没有第二份实现"的结构证据。层差异只由下面这个
// nodeLifecycleLayer 的读数表达：subagent 的现场按角色给（entry 角色共享主工作区），
// 收尾即回收（Immediate），没有团队归属。

// nodeWorkUnit 是 subagent 层的一个工作单元（一件事 = 一份现场 + 一条会话）。
type nodeWorkUnit struct {
	host  *lifecycleHost
	layer nodeLifecycleLayer
}

// nodeLifecycleLayer 是 subagent 层的**读数**：没有逻辑，也不持有任何端口。
type nodeLifecycleLayer struct {
	mainSessionID string
	nodeID        string
	scope         model.NodeScope
}

// Kind 报告这一层：subagent（恒定读数：本注册点只服务这一层）。
func (l nodeLifecycleLayer) Kind() workunit.Kind { return workunit.KindSubagent }

// SessionPath 是本层的会话路径 = 主会话号（账本键的解析在父里）。
func (l nodeLifecycleLayer) SessionPath() string { return l.mainSessionID }

// Ownership 是归属读数：subagent 单元没有团队归属（TeamID 空），只有角色。
func (l nodeLifecycleLayer) Ownership() lifeOwnership {
	return lifeOwnership{Role: string(l.scope.Role)}
}

// NodeID 是本单元现场的身份：本层就是节点 id。
func (l nodeLifecycleLayer) NodeID() string { return l.nodeID }

// Policy 是本层的收尾策略：subagent 的现场是临时的（收尾即回收）。
func (l nodeLifecycleLayer) Policy() workunit.FinishPolicy { return workunit.Immediate{} }

// 编译期钉住"这就是 workunit.Unit 的一份实现"：契约漂移先红。
var _ workunit.Unit = (*nodeWorkUnit)(nil)
var _ lifecycleLayer = nodeLifecycleLayer{}

// newNodeWorkUnit 组装 subagent 层的注册点：父实现 + 本层读数（依赖构造式注入）。
func (r *Runtime) newNodeWorkUnit(mainSessionID, nodeID string, scope model.NodeScope) *nodeWorkUnit {
	return &nodeWorkUnit{host: newLifecycleHost(r), layer: nodeLifecycleLayer{
		mainSessionID: mainSessionID,
		nodeID:        nodeID,
		scope:         scope,
	}}
}

// Kind 报告这一层：subagent。
func (u *nodeWorkUnit) Kind() workunit.Kind { return workunit.KindSubagent }

// FinishPolicy 回答"什么时候回收"：subagent 的现场是临时的（收尾即回收）。
// 恒定的读数，因此 nil 接收者也能回答（编译期断言与面板读取不会踩空）。
func (u *nodeWorkUnit) FinishPolicy() workunit.FinishPolicy { return workunit.Immediate{} }

// Begin 建现场：唯一实现在父里。
func (u *nodeWorkUnit) Begin(ctx context.Context) (workunit.Scene, error) {
	return u.host.Begin(ctx, u.layer)
}

// Finish 收尾这一轮（分类 + 合并 + 回执；不拆现场）：唯一实现在父里。落定之后按策略
// 回收（`Immediate`）——驱动的是**这个单元自己**（契约的 AfterFinish 要一个 Unit）。
func (u *nodeWorkUnit) Finish(ctx context.Context, result workunit.Result, mergeErr error) (workunit.Outcome, error) {
	return u.host.Finish(ctx, u, u.layer, result, mergeErr)
}

// Reclaim 拆这一份（拆现场 + 清会话记录；幂等）：唯一实现在父里。
func (u *nodeWorkUnit) Reclaim(ctx context.Context) error {
	return u.host.Reclaim(ctx, u.layer)
}

// Recover 重启回灌（会话级读回 + 认领现场 + 中断读数）：唯一实现在父里。
func (u *nodeWorkUnit) Recover(ctx context.Context) (workunit.Resume, error) {
	return u.host.Recover(ctx, u.layer)
}
