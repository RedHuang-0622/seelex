package workunit

import "context"

// ports.go — 生命周期的**宿主注入面**（三格窄端口）。生命周期实现（lifecycle.go 的 `Host`）
// 只持它们，不认识具体宿主；三格的实现留在装配处
// （`backend/engine/assemble/workunit_assembly.go` 的 hostPorts），因此在那边 import
// `worktree` / `teamwork` / `store` 再合适不过——**具体类型只允许出现在装配处**。
//
// 为什么三格接口出声在契约包而不是各写一份：这是"同一件事只有一份实现"的结构证据——
// 生命周期实现里因此搜不到任何一个 `teamwork.` / `worktree.` / `store.` 限定符，也搜不到
// 按层分支（`e2e/workunit_ports_test.go` 的机械门禁按这个判，带阴性对照）。
//
// 三格的划法不是"按层"（不是 subagent 一格 / teammate 一格），而是**按宿主能力**：
// 现场 / 编排闸门 + 账本 / 会话记录。层与层之间的差别因此只剩注册点交进来的 `Unit` 读数与
// `FinishPolicy`（见 contract.go）——层差异只允许出现在装配表与策略的调用点上。

// SceneFace 是**现场端口**：一份活的现场（建 / 合并 / 认领 / 拆 / 在册）。
//
// 建与合并是两层的动作，由实现在内部按**归属读数**选路（团队托管的现场归 team，其余归
// worktree 端口）；认领、拆单体与在册读数是子代理那一侧的动作——team 托管的现场由编排端口
// 的 Reclaim 一并拆、由编排端口的 Recover 一并认领（那里是那份"整队收口"的唯一实现）。
type SceneFace interface {
	// BeginScene 建（或复用）这一份现场：返回现场身份（指派名 + 现场 nodeID）。
	// 幂等，且**不得**对已存在的现场动手（现场是人的资产）。
	BeginScene(ctx context.Context, own Ownership, nodeID, sessionPath string) (Scene, error)
	// MergeScene 把这一份现场的改动合回主工作区**一次**（合并失败原文交回调用方分类）。
	MergeScene(ctx context.Context, own Ownership, nodeID, sessionPath string) error
	// ReleaseScene 拆掉**没有团队归属**的那一份现场（幂等；team 托管的那一份由编排端口回收）。
	ReleaseScene(nodeID string)
	// AdoptScenes 认领本会话在册的现场（重启回灌；**必须在 Prune 之前**）。
	AdoptScenes(ctx context.Context, sessionPath string) error
	// SceneRegistered 报告这一份现场此刻是否还在册（读数，不动现场）。
	SceneRegistered(nodeID string) bool
}

// UnitFace 是**编排闸门 + 账本端口**：谁在编、哪一件、收尾往哪写、回收谁、重启怎么回灌。
//
// 它**不是**"第二份生命周期"：分类、重入、合并最多成功一次这些判据都在 `Host` 里，这一格只提供
// "编排面已有的读写"。方法面全部用契约词汇，`teamwork.WorkerRequest` 不出装配处。
type UnitFace interface {
	// Owned 报告这一份是不是**归团队托管**（编排里认得这一件）。
	Owned(ctx context.Context, sessionPath string, own Ownership) bool
	// Settled 报告这一件事在计划里是否已经收过尾（收口过 / 被重派）。
	Settled(ctx context.Context, sessionPath, itemID string) (bool, error)
	// SettleUnit 交回收尾结论（分类 + 计划状态 + 回执）；分类是编排面那一份，调用方不替它判。
	SettleUnit(ctx context.Context, sessionPath string, own Ownership, resultErr, mergeErr error) (Outcome, error)
	// ReclaimUnit 回收一个 teammate 单元（作业 → 现场 → 会话内容；幂等）。
	ReclaimUnit(ctx context.Context, sessionPath, role string) error
	// RecoverUnits 重启回灌本会话的团队单元（认领现场 + 账本读数 + 回灌 + 恢复说明）。
	RecoverUnits(ctx context.Context, sessionPath string) (Resume, error)
}

// RecordFace 是**会话记录端口**：一个工作单元 = 一条 `storeworkunit.Record`。
//
// 作用域的解析（团队账本键 vs 会话工作区绑定）只有这一格知道——它是"记录落在哪个项目/会话
// 目录"这件事的唯一一处实现（此前两个调用点各算一次，迟早算成两个目录）。
type RecordFace interface {
	// Status 读回本单元那一份记录的 status（found=false = 没有这一格事实）。
	Status(ctx context.Context, own Ownership, nodeID, sessionPath string) (string, bool, error)
	// Readout 按**会话路径**读回该会话的单元记录并折成回灌读数（Sessions / Interrupted）。
	Readout(ctx context.Context, own Ownership, nodeID, sessionPath string) (Resume, error)
	// SaveRecord 落一条本单元的记录（整条写；未托管/未装配 = no-op）。
	SaveRecord(ctx context.Context, own Ownership, nodeID, sessionPath, status, summary, stage string)
	// SettleRecord 把收尾分类落成记录终态（未托管/空结论 = no-op）。
	SettleRecord(ctx context.Context, own Ownership, nodeID, sessionPath string, outcome Outcome)
	// Clear 清掉本单元的记录（幂等；没有记录可清 = no-op）。
	Clear(ctx context.Context, own Ownership, nodeID, sessionPath string) error
}
