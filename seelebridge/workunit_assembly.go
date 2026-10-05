package seelebridge

// workunit_assembly.go — **装配表**：生命周期实现（workunit_parent.go 的 lifecycleHost）只持
// 端口；端口背后是谁，只有本文件（装配处）知道。
//
// 口径：docs/arch/workunit-ports-and-assembly.md §2（父/端口清单）与 §3（装配矩阵），
// workunit-single-lifecycle-one-implementation.md §3（接口先行 + 依赖倒置）。落地一版：
//
//	生命周期实现持有的三格（+ 契约里的作业面 workunit.Jobs / 会话账本 workunit.SessionLedger）
//
//	  sceneFace   现场：建（两层）/ 合并（两层）/ 认领（子代理）/ 拆单体（子代理）/ 在册读数
//	  unitFace    编排闸门 + 账本：归属判定 / 重入判定 / 收尾回执 / 单个回收 / 重启回灌
//	  recordFace  会话记录：状态读数 / 会话级回灌读数 / 落盘 / 写终态 / 清记录
//
//	这三格的实现（hostPorts）是**唯一**知道 `teamwork` / `worktree` / `sessionstore` 的地方；
//	lifecycleHost 的方法体里因此搜不到任何一个具体类型，也搜不到按层分支（层差异只剩"装配哪
//	几格"与"什么时候回收"这一条策略，见 workunit.FinishPolicy）。
//
// 为什么编排/账本也做成一格端口，而不是"让生命周期实现直接调 Coordinator"：§2 第 5–7 条明确
// "编排闸门 / 装配 / 账本**不进 workunit**，以端口/回调接"。这里就是那条口子——端口的方法面
// 只用契约词汇（`workunit.Ownership` / `workunit.Resume` / 字符串），`teamwork.WorkerRequest`
// 这类实现类型一步都不出本文件。
//
// 作业面（`workunit.Jobs`）今天由**编排那一格**驱动（`Coordinator.reclaimStepsLocked` 步 1 是
// 全仓唯一的作业回收调用点），所以 lifecycleHost 没有第二个作业面字段——多一个调用点就是第二条
// 回收路径。把作业面直接接到宿主上属于步骤②（两张作业表合一）的落点，文件末尾有说明。

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	seetelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/seelebridge/teamwork"
	"github.com/RedHuang-0622/seelex/seelebridge/worktree"
	"github.com/RedHuang-0622/seelex/seelebridge/workunit"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// ── 端口（生命周期实现只认识这三个名字）──────────────────────────────────

// sceneFace 是**现场端口**：一份活的现场（建 / 合并 / 认领 / 拆 / 在册）。
//
// 建与合并是两层的动作，由实现在内部按**归属读数**选路（团队托管的现场归 team，其余归
// worktree 端口）；认领、拆单体与在册读数是子代理那一侧的动作——team 托管的现场由编排端口
// 的 Reclaim 一并拆、由编排端口的 Recover 一并认领（那里是那份"整队收口"的唯一实现）。
type sceneFace interface {
	// BeginScene 建（或复用）这一份现场：返回现场身份（指派名 + 现场 nodeID）。
	// 幂等，且**不得**对已存在的现场动手（现场是人的资产）。
	BeginScene(ctx context.Context, own workunit.Ownership, nodeID, sessionPath string) (workunit.Scene, error)
	// MergeScene 把这一份现场的改动合回主工作区**一次**（合并失败原文交回调用方分类）。
	MergeScene(ctx context.Context, own workunit.Ownership, nodeID, sessionPath string) error
	// ReleaseScene 拆掉**没有团队归属**的那一份现场（幂等；team 托管的那一份由编排端口回收）。
	ReleaseScene(nodeID string)
	// AdoptScenes 认领本会话在册的现场（重启回灌；**必须在 Prune 之前**）。
	AdoptScenes(ctx context.Context, sessionPath string) error
	// SceneRegistered 报告这一份现场此刻是否还在册（读数，不动现场）。
	SceneRegistered(nodeID string) bool
}

// unitFace 是**编排闸门 + 账本端口**：谁在编、哪一件、收尾往哪写、回收谁、重启怎么回灌。
//
// 它**不是**"第二份生命周期"：分类、重入、合并只做一次这些判据都在 lifecycleHost 里，这一格
// 只提供"编排面已有的读写"。方法面全部用契约词汇，`teamwork.WorkerRequest` 不出装配处。
type unitFace interface {
	// Owned 报告这一份是不是**归团队托管**（编排里认得这一件）。
	Owned(ctx context.Context, sessionPath string, own workunit.Ownership) bool
	// Settled 报告这一件事在计划里是否已经收过尾（收口过 / 被重派）。
	Settled(ctx context.Context, sessionPath, itemID string) (bool, error)
	// SettleUnit 交回收尾结论（分类 + 计划状态 + 回执）；分类是编排面那一份，调用方不替它判。
	SettleUnit(ctx context.Context, sessionPath string, own workunit.Ownership, resultErr, mergeErr error) (workunit.Outcome, error)
	// ReclaimUnit 回收一个 teammate 单元（作业 → 现场 → 会话内容；幂等）。
	ReclaimUnit(ctx context.Context, sessionPath, role string) error
	// RecoverUnits 重启回灌本会话的团队单元（认领现场 + 账本读数 + 回灌 + 恢复说明）。
	RecoverUnits(ctx context.Context, sessionPath string) (workunit.Resume, error)
}

// recordFace 是**会话记录端口**：一个工作单元 = 一条 `sessionstore.NodeSessionRecord`。
//
// 作用域的解析（团队账本键 vs 会话工作区绑定）只有这一格知道——它是"记录落在哪个项目/会话
// 目录"这件事的唯一一处实现（此前两个调用点各算一次，迟早算成两个目录）。
type recordFace interface {
	// Status 读回本单元那一份记录的 status（found=false = 没有这一格事实）。
	Status(ctx context.Context, own workunit.Ownership, nodeID, sessionPath string) (string, bool, error)
	// Readout 按**会话路径**读回该会话的单元记录并折成回灌读数（Sessions / Interrupted）。
	Readout(ctx context.Context, own workunit.Ownership, nodeID, sessionPath string) (workunit.Resume, error)
	// SaveRecord 落一条本单元的记录（整条写；未托管/未装配 = no-op）。
	SaveRecord(ctx context.Context, own workunit.Ownership, nodeID, sessionPath, status, summary, stage string)
	// SettleRecord 把收尾分类落成记录终态（未托管/空结论 = no-op）。
	SettleRecord(ctx context.Context, own workunit.Ownership, nodeID, sessionPath string, outcome workunit.Outcome)
	// Clear 清掉本单元的记录（幂等；没有记录可清 = no-op）。
	Clear(ctx context.Context, own workunit.Ownership, nodeID, sessionPath string) error
}

// ── 装配处：把宿主能力面接成上面三格 ─────────────────────────────────────

// hostPorts 是三格端口的**唯一一份**实现（宿主能力面的适配）。字段不导出：只有装配处
// （newLifecycleHost）拿得到它，生命周期实现只拿到上面那三个接口。
type hostPorts struct {
	r *Runtime
}

var (
	_ sceneFace  = (*hostPorts)(nil)
	_ unitFace   = (*hostPorts)(nil)
	_ recordFace = (*hostPorts)(nil)
)

// newHostPorts 把宿主接成端口（构造式注入，不读全局）。
func newHostPorts(r *Runtime) *hostPorts { return &hostPorts{r: r} }

// Owned 实现"是不是归团队托管"：归属读数里给了 TeamID 就是；只给了工作项 id 时，按它去
// **计划**里认一次——事实只有一个来源（计划），不猜、也不另立一份归属表。
//
// 这是本端口里唯一一处"归属判定"，现场/记录/编排三条路都从这里问，不各判一次。
func (p *hostPorts) Owned(ctx context.Context, sessionPath string, own workunit.Ownership) bool {
	if strings.TrimSpace(own.TeamID) != "" {
		return true
	}
	if strings.TrimSpace(own.ItemID) == "" {
		return false
	}
	if p == nil || p.r == nil {
		return false
	}
	coordinator, err := p.r.coordinatorForSession(sessionPath)
	if err != nil {
		return false
	}
	_, ok := coordinator.ItemStatus(ctx, own.ItemID)
	return ok
}

// Settled 实现"计划里这一件事是否已经收过尾"（重入判据的编排侧一半）。
func (p *hostPorts) Settled(ctx context.Context, sessionPath, itemID string) (bool, error) {
	if p == nil || p.r == nil {
		return false, errLifecycleHostUnavailable
	}
	coordinator, err := p.r.coordinatorForSession(sessionPath)
	if err != nil {
		return false, err
	}
	return coordinator.ItemSettled(ctx, itemID), nil
}

// SettleUnit 交回收尾结论：编排面那一份分类在 `SettleWorkItemWith` 里，本方法只转发。
func (p *hostPorts) SettleUnit(ctx context.Context, sessionPath string, own workunit.Ownership, resultErr, mergeErr error) (workunit.Outcome, error) {
	if p == nil || p.r == nil {
		return workunit.Outcome{}, errLifecycleHostUnavailable
	}
	coordinator, err := p.r.coordinatorForSession(sessionPath)
	if err != nil {
		return workunit.Outcome{}, err
	}
	return coordinator.SettleWorkItemWith(ctx, workerRequest(sessionPath, own), resultErr, mergeErr)
}

// ReclaimUnit 回收一个 teammate 单元：收口四步的前三步（作业 → 现场 → 会话内容）只有那一份实现。
func (p *hostPorts) ReclaimUnit(ctx context.Context, sessionPath, role string) error {
	if p == nil || p.r == nil {
		return errLifecycleHostUnavailable
	}
	coordinator, err := p.r.coordinatorForSession(sessionPath)
	if err != nil {
		return err
	}
	return coordinator.Reclaim(ctx, role)
}

// RecoverUnits 重启回灌本会话的团队单元（认领现场 → 账本读数 → 会话回灌 → 注入恢复说明）。
func (p *hostPorts) RecoverUnits(ctx context.Context, sessionPath string) (workunit.Resume, error) {
	if p == nil || p.r == nil {
		return workunit.Resume{}, errLifecycleHostUnavailable
	}
	recovery, err := p.r.RecoverTeamworkUnits(ctx, sessionPath)
	if err != nil {
		return recovery.Resume, err
	}
	return recovery.Resume, nil
}

// BeginScene 建（或复用）一份现场。
//
//	团队托管的（teammate）→ 走团队现成的现场绑定路（幂等，绝不 force-remove 已存在的现场），
//	                         指派名由 teamwork 的两个派生函数算一次（命名只有一处来源）；
//	其余（subagent）       → 按角色读数给现场（entry 角色降级共享主工作区 = 没有独立现场）。
//
// 选路依据是**归属读数**（Owned），不是 Kind：契约里 Kind 是描述性的，不是分支判据。
func (p *hostPorts) BeginScene(ctx context.Context, own workunit.Ownership, nodeID, sessionPath string) (workunit.Scene, error) {
	if p == nil || p.r == nil {
		return workunit.Scene{}, errLifecycleHostUnavailable
	}
	scene := workunit.Scene{
		NodeID:    nodeID,
		TeamID:    own.TeamID,
		WorkItem:  own.ItemID,
		SessionID: own.RoleSessionID,
	}
	if p.Owned(ctx, sessionPath, own) {
		scene.Worktree = strings.TrimSpace(own.Worktree)
		if scene.Worktree == "" {
			scene.Worktree = teamwork.WorkItemWorktreeName(own.Role, own.ItemID)
			if strings.TrimSpace(own.ItemID) == "" {
				scene.Worktree = teamwork.TeammateWorktreeName(own.Role)
			}
		}
		bound, err := p.r.BindWorkspace(ctx, teamwork.WorkspaceBinding{
			MainSessionID: sessionPath,
			TeamID:        own.TeamID,
			Milestone:     own.Milestone,
			WorkItem:      own.ItemID,
			Role:          own.Role,
			SessionID:     own.RoleSessionID,
			Worktree:      scene.Worktree,
		})
		if err != nil {
			return workunit.Scene{}, err
		}
		if strings.TrimSpace(bound.Worktree) != "" {
			scene.Worktree = bound.Worktree
		}
		if mapped := workItemNodeID(scene.Worktree); mapped != "" {
			scene.NodeID = mapped
		}
		return scene, nil
	}
	scope := model.NodeScope{NodeID: scene.NodeID, Role: model.AccountRole(own.Role)}
	if wt := p.r.beginNodeWorktree(scope, scene.NodeID); wt != nil {
		scene.Worktree = wt.Branch
	}
	return scene, nil
}

// MergeScene 把这一份现场的改动合回主工作区一次：team 托管的走编排面的合并，其余走现场面。
func (p *hostPorts) MergeScene(ctx context.Context, own workunit.Ownership, nodeID, sessionPath string) error {
	if p == nil || p.r == nil {
		return errLifecycleHostUnavailable
	}
	if p.Owned(ctx, sessionPath, own) {
		coordinator, err := p.r.coordinatorForSession(sessionPath)
		if err != nil {
			return err
		}
		return coordinator.MergeWorkItem(ctx, workerRequest(sessionPath, own))
	}
	return p.r.finishNodeWorktree(ctx, nodeID, p.r.nodeWorktreeFor(nodeID))
}

// ReleaseScene 拆掉没有团队归属的那一份现场（幂等）。
func (p *hostPorts) ReleaseScene(nodeID string) {
	if p == nil || p.r == nil || p.r.worktreeMgr == nil {
		return
	}
	p.r.worktreeMgr.Release(nodeID)
}

// AdoptScenes 认领本会话在册的现场（重启回灌；**必须在 Prune 之前**，顺序是判据的一部分）。
func (p *hostPorts) AdoptScenes(ctx context.Context, sessionPath string) error {
	if p == nil || p.r == nil {
		return errLifecycleHostUnavailable
	}
	return p.r.RestoreSubagentAnchors(sessionPath)
}

// SceneRegistered 报告这一份现场是否还在册（读数，不动现场）。
func (p *hostPorts) SceneRegistered(nodeID string) bool {
	return p.r.nodeWorktreeFor(nodeID) != nil
}

// Status 读回本单元那一份记录的 status（会话记录面的一格读数；折算走 `ProgressOf` 那一份）。
func (p *hostPorts) Status(ctx context.Context, own workunit.Ownership, nodeID, sessionPath string) (string, bool, error) {
	reader, _, ok := p.unitReader(ctx, own, sessionPath)
	if !ok {
		return "", false, nil
	}
	progress, found, err := reader.Read(nodeID)
	if err != nil || !found {
		return "", false, err
	}
	return progress.Status, true, nil
}

// Readout 按会话路径读回该会话的全部单元记录，并折成回灌读数：
//
//	Sessions    —— 读回了几条记录（**会话级**粒度：契约的 Recover 是按会话路径回灌的）；
//	Interrupted —— 记录说在跑、而本进程已无它的执行面的那一条（只报本单元）。
//
// 读法与折算都不在这里：List/定位是 `workunit.UnitReader`，折算（含 `InFlight`）是
// `workunit.ProgressOf`——本格只做"这一层要不要更上一层判据"（recordBelongsToCurrentMain）。
//
// 认领现场（`AdoptScenes`）不在这里：它是**现场**那一格的动作，而"认领先于 Prune"的顺序由
// 调用方（lifecycleHost.Recover）保证——顺序是判据的一部分，因此写在读得见的地方。
func (p *hostPorts) Readout(ctx context.Context, own workunit.Ownership, nodeID, sessionPath string) (workunit.Resume, error) {
	reader, _, ok := p.unitReader(ctx, own, sessionPath)
	if !ok {
		return workunit.Resume{}, nil
	}
	list, err := reader.List()
	if err != nil {
		return workunit.Resume{}, fmt.Errorf("workunit: list session records for %q: %w", sessionPath, err)
	}
	resume := workunit.Resume{Sessions: len(list)}
	record, found, err := reader.Record(nodeID)
	if err != nil {
		return workunit.Resume{}, fmt.Errorf("workunit: read session record for %q: %w", nodeID, err)
	}
	if !found {
		return resume, nil
	}
	progress := workunit.ProgressOf("", record)
	if !progress.InFlight {
		return resume, nil
	}
	// 记录说在跑、而本进程已无它的执行面 ⇒ 中断（交上层重跑或人工处置）。
	// 判定复用既有的 recordBelongsToCurrentMain（不另立第二条判据）。
	if p.r.recordBelongsToCurrentMain(record) {
		resume.Interrupted = []string{nodeID}
	}
	return resume, nil
}

// SaveRecord 落一条本单元的记录（**团队托管**那一层的写面；子代理的记录由节点会话面自己写）。
func (p *hostPorts) SaveRecord(ctx context.Context, own workunit.Ownership, nodeID, sessionPath, status, summary, stage string) {
	if p == nil || p.r == nil || !p.Owned(ctx, sessionPath, own) {
		return
	}
	p.r.saveTeamUnitRecord(sessionPath, recordKeyFor(own, nodeID), status, summary, stage)
}

// SettleRecord 把收尾分类落成记录终态。
func (p *hostPorts) SettleRecord(ctx context.Context, own workunit.Ownership, nodeID, sessionPath string, outcome workunit.Outcome) {
	if p == nil || p.r == nil || !p.Owned(ctx, sessionPath, own) {
		return
	}
	p.r.settleTeamUnitRecord(sessionPath, recordKeyFor(own, nodeID), outcome)
}

// Clear 清掉本单元的记录：
//
//	团队托管 → 按角色会话号清（记录描述的是"这一份现场跑到哪"）；
//	其余     → 先按 nodeID 找到那一条，再按它自己的会话号删（会话号才是记录的键）。
func (p *hostPorts) Clear(ctx context.Context, own workunit.Ownership, nodeID, sessionPath string) error {
	if p == nil || p.r == nil {
		return errLifecycleHostUnavailable
	}
	if p.Owned(ctx, sessionPath, own) {
		p.r.clearTeamUnitRecord(sessionPath, own.RoleSessionID)
		return nil
	}
	reader, key, ok := p.unitReader(ctx, own, sessionPath)
	if !ok {
		return nil
	}
	record, found, err := reader.Record(nodeID)
	if err != nil {
		return err
	}
	if !found {
		return nil // 没有会话记录可删（未装配持久化，或已经回收过）
	}
	ledger := p.r.teamUnitLedger()
	if ledger == nil {
		return nil
	}
	if err := ledger.Delete(key.ProjectID, key.SessionID, record.SessionID); err != nil {
		return fmt.Errorf("workunit: reclaim session record for %q: %w", nodeID, err)
	}
	return nil
}

// unitReader 组装本层的记录读面：**作用域解析仍只有 `sessionScope` 一处**，而
// "List → 按 nodeID 定位 → 折算"全部由 `workunit.UnitReader` / `ProgressOf` 提供——本格
// 此前自己列一遍账本、自己线性查找、自己取字段，是同一件事的第二份实现。
//
// Kind 留空（`Progress.Kind` 是描述性标注）：本格是**两层共用**的记录端口（父实现的服务面），
// 给读数贴一个层标注就是说谎——调用方不需要它，需要时按自己的层给。
func (p *hostPorts) unitReader(ctx context.Context, own workunit.Ownership, sessionPath string) (*workunit.UnitReader, sessionstore.Key, bool) {
	if p == nil || p.r == nil {
		return nil, sessionstore.Key{}, false
	}
	ledger := p.r.teamUnitLedger()
	if ledger == nil {
		return nil, sessionstore.Key{}, false
	}
	key, ok := p.sessionScope(ctx, own, sessionPath)
	if !ok {
		return nil, sessionstore.Key{}, false
	}
	return workunit.NewUnitReader(ledger, key.ProjectID, key.SessionID, "", nil), key, true
}

// sessionScope 解析本层的会话账本作用域（**唯一一处解析**）：团队归属的单元记在团队账本
// 同一个键上（组合根注入的 KeyFor），其余记在会话自己的工作区绑定上。
func (p *hostPorts) sessionScope(ctx context.Context, own workunit.Ownership, sessionPath string) (sessionstore.Key, bool) {
	if p == nil || p.r == nil {
		return sessionstore.Key{}, false
	}
	sessionID := strings.TrimSpace(sessionPath)
	if sessionID == "" {
		return sessionstore.Key{}, false
	}
	if p.Owned(ctx, sessionID, own) {
		return p.r.teamUnitScope(sessionID)
	}
	key := sessionstore.Key{ProjectID: p.r.sessionProjectIDFor(sessionID), SessionID: sessionID}
	return key, true
}

// recordKeyFor 把归属读数折成记录身份（现场 nodeID + 角色会话 + 这一轮的目标）：
// 记录写面的键只有这一处拼法。
func recordKeyFor(own workunit.Ownership, nodeID string) teamUnitRecordKey {
	return teamUnitRecordKey{
		NodeID:        nodeID,
		RoleSessionID: own.RoleSessionID,
		Goal:          own.Goal,
	}
}

// workerRequest 把归属读数折成编排那条路认识的载荷（它只读 WorkItemID 与归属，因此不需要
// 重新搬一遍派发时的全部字段）。`teamwork.WorkerRequest` 因此**不出本文件**。
func workerRequest(sessionPath string, own workunit.Ownership) teamwork.WorkerRequest {
	return teamwork.WorkerRequest{
		MainSessionID: sessionPath,
		TeamID:        own.TeamID,
		Role:          own.Role,
		RoleSessionID: own.RoleSessionID,
		WorkItemID:    own.ItemID,
		Milestone:     own.Milestone,
		Goal:          own.Goal,
	}
}

// ── 生产驱动点：subagent 层（node 域只认这三个回调）─────────────────────

// beginNodeUnit 驱动 subagent 层工作单元的 Begin（现场的唯一建法仍在生命周期实现里）。
//
// 没有 ctx：node 域的 Deps 建现场回调本来就不带 ctx（建现场是纯现场动作），Begin 里
// subagent 那一路也不写账本。
func (r *Runtime) beginNodeUnit(scope model.NodeScope, nodeID string) *worktree.NodeWorktree {
	if r == nil {
		return nil
	}
	unit := r.newNodeWorkUnit(r.MainSessionID(), nodeID, scope)
	if _, err := unit.Begin(context.Background()); err != nil {
		log.Printf("seelebridge: 建节点现场 %q 失败：%v", nodeID, err)
	}
	return r.nodeWorktreeFor(nodeID)
}

// finishNodeUnit 驱动 subagent 层工作单元的 Finish：合并只做一次（调用方没有合并结果）、
// 分类取自契约、落定之后按策略（Immediate）回收。返回的是**合并失败原文**，调用方据此
// 决定节点要不要判死（ClassifyFinish 同口径）。
func (r *Runtime) finishNodeUnit(ctx context.Context, nodeID string, _ *worktree.NodeWorktree) error {
	if r == nil {
		return nil
	}
	mainSessionID := strings.TrimSpace(seetelemetry.SessionIDFromContext(ctx))
	if mainSessionID == "" {
		mainSessionID = r.MainSessionID()
	}
	scope := resolvedNodeScope(ctx, nodeID)
	unit := r.newNodeWorkUnit(mainSessionID, nodeID, scope)
	_, err := unit.Finish(ctx, workunit.Result{}, nil)
	return err
}

// resolvedNodeScope 取当前节点作用域的读数（ctx 上有节点作用域时用它，否则按子代理角色
// 兜底：现场端口只按角色判"要不要一份独立现场"）。
func resolvedNodeScope(ctx context.Context, nodeID string) model.NodeScope {
	if scope, ok := model.NodeScopeFromContext(ctx); ok {
		if strings.TrimSpace(scope.NodeID) == "" {
			scope.NodeID = nodeID
		}
		return scope
	}
	return model.NodeScope{NodeID: nodeID, Role: model.RoleSubAgent}
}

// nodeWorktreeFor 返回节点在册的现场（无管理器/无现场 → nil）。
func (r *Runtime) nodeWorktreeFor(nodeID string) *worktree.NodeWorktree {
	if r == nil || r.worktreeMgr == nil {
		return nil
	}
	return r.worktreeMgr.WorktreeForNode(nodeID)
}

// ── 作业面（workunit.Jobs）在装配表里的位置 ─────────────────────────────
//
// 作业面端口已在契约里定形（`workunit.Jobs`：提交 / 状态 / 增量读 / 全量读 / 取消 / 销项 /
// 按作用域回收 / 变更信号），并由 `jobs.Manager` **结构上满足**（编译期断言在
// workunit/contract.go）。它今天由**编排那一格**驱动——`Coordinator.reclaimStepsLocked` 步 1
// 是全仓唯一的作业回收调用点，本文件的 `ReclaimUnit` 转发到那里。
//
// 因此 lifecycleHost **不**再挂一个作业面字段：接上它就是第二条回收调用点（同一件事两份实现，
// 还会与编排那一份抢时机）。把作业面直接接到宿主上 = 步骤②（两张作业表合一）落地之后的装配
// 变更：那时 `ReclaimUnit` 那一格只清账本，作业回收由宿主按 scope 直接驱动。
