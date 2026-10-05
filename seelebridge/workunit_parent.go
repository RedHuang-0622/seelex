package seelebridge

// workunit_parent.go — 一件活的生命周期的**唯一一份实现**（父），以及两层各自的
// 注册点赖以工作的窄 hook。
//
// 口径（docs/arch/workunit-single-lifecycle-one-implementation.md）：一个工作单元的生命周期
// （建现场 → 收尾 → 回收 → 重启接着做）只有**一份实现**；subagent 与 teammate 只做
// **在父实现上注册 + 转发**。层与层之间唯一的差别是注册点交进来的**读数**
// （contract.go 的 `workunit.Unit`）：
//
//	Kind / ID（本单元现场的身份）/ SessionPath（自己的会话路径）/ Policy（什么时候回收）/
//	Owns（归属：团队 · 工作项 · 角色 · 这一轮的目标 · 显式现场名）
//
// 父持有全部端口（字段不导出，只有父能拿到）：
//
//	ledger  workunit.SessionLedger      // 会话快照（结构上就是 *sessionstore.NodeSessionStore）
//	spaces  *worktree.WorktreeManager   // 现场：fork / merge / 认领（一份）
//	r       *Runtime                    // 宿主：既有实现（BindWorkspace / beginNodeWorktree /
//	                                    //   Coordinator 的收尾与回收）都在它上面，父只调用它们
//
// 为什么不是"包一层"：四个动作的**判据与顺序**（重入怎么回答、合并只做一次、恢复的粒度、
// 拆的时候收不收作业）此前在两个实现里各写了一遍且互相漂移；现在它们只写在下面这四个
// 方法里，注册点的方法体一律是 `return u.host.Xxx(ctx, u.read)`。

import (
	"context"
	"errors"
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

// ── 父：唯一实现，持有全部端口（字段不导出）──────────────────────────────

// lifecycleHost 是一件活的生命周期的唯一实现。端口**不导出**：两个注册点只持有
// `workunit.Lifecycle`（就是它）与自己的 `workunit.Unit` 读数，拿不到下面任何一个端口。
type lifecycleHost struct {
	r      *Runtime
	ledger workunit.SessionLedger
	spaces *worktree.WorktreeManager
}

// 编译期钉住"这就是契约那份父实现"：契约漂移先红。
var _ workunit.Lifecycle = (*lifecycleHost)(nil)

// errLifecycleHostUnavailable 是缺装配时的显式错误（缺的是宿主，不是"静默降级"）。
var errLifecycleHostUnavailable = errors.New("workunit: 生命周期宿主未装配")

// newLifecycleHost 把宿主能力面接成端口（构造式注入，不读全局）。
func newLifecycleHost(r *Runtime) *lifecycleHost {
	host := &lifecycleHost{r: r}
	if r == nil {
		return host
	}
	host.ledger = r.teamUnitLedger()
	host.spaces = r.worktreeMgr
	return host
}

// teamOwned 报告这一份是不是**归团队托管**（生命周期里的数据判据：有作业面与团队账本的
// 那一层）。归属读数里没有 TeamID 时（老口径派发的载荷不带它），按工作项 id 去计划里认
// 一次——事实只有一个来源（计划），不猜、也不另立一份归属表。
func (h *lifecycleHost) teamOwned(ctx context.Context, u workunit.Unit) bool {
	own := u.Owns()
	if strings.TrimSpace(own.TeamID) != "" {
		return true
	}
	if strings.TrimSpace(own.ItemID) == "" {
		return false
	}
	coordinator, err := h.r.coordinatorForSession(u.SessionPath())
	if err != nil {
		return false
	}
	_, ok := coordinator.ItemStatus(ctx, own.ItemID)
	return ok
}

// sessionKey 解析本层的会话账本作用域（**唯一一处解析**）：团队归属的单元记在团队账本
// 同一个键上（组合根注入的 KeyFor），subagent 单元记在会话自己的工作区绑定上。
func (h *lifecycleHost) sessionKey(ctx context.Context, u workunit.Unit) (sessionstore.Key, bool) {
	if h == nil || h.r == nil {
		return sessionstore.Key{}, false
	}
	sessionID := strings.TrimSpace(u.SessionPath())
	if sessionID == "" {
		return sessionstore.Key{}, false
	}
	if h.teamOwned(ctx, u) {
		return h.r.teamUnitScope(sessionID)
	}
	key := sessionstore.Key{ProjectID: h.r.sessionProjectIDFor(sessionID), SessionID: sessionID}
	return key, true
}

// Begin 建（或复用）这一份现场：唯一实现。
//
//	团队归属（teammate）  → 走团队现成的现场绑定路（幂等，绝不 force-remove 已存在的现场），
//	                         并把"这一轮开始了、现场在哪"落盘；
//	没有团队归属（subagent）→ 按角色读数给现场（entry 角色降级共享主工作区 = 没有独立现场）。
//
// 分支依据是**归属读数**，不是 Kind：契约里 Kind 是描述性的，不是分支判据。
func (h *lifecycleHost) Begin(ctx context.Context, u workunit.Unit) (workunit.Scene, error) {
	if h == nil || h.r == nil {
		return workunit.Scene{}, errLifecycleHostUnavailable
	}
	own := u.Owns()
	scene := workunit.Scene{
		Kind:      u.Kind(),
		NodeID:    u.ID(),
		TeamID:    own.TeamID,
		WorkItem:  own.ItemID,
		SessionID: own.RoleSessionID,
	}
	if h.teamOwned(ctx, u) {
		// 现场指派名：调用方显式给了就用它（同一个命名约定派生的结果），否则由父按
		// teamwork 的两个派生函数算一次——命名只有一处来源，不在这里另拼一次。
		scene.Worktree = strings.TrimSpace(own.Worktree)
		if scene.Worktree == "" {
			scene.Worktree = teamwork.WorkItemWorktreeName(own.Role, own.ItemID)
			if strings.TrimSpace(own.ItemID) == "" {
				scene.Worktree = teamwork.TeammateWorktreeName(own.Role)
			}
		}
		bound, err := h.r.BindWorkspace(ctx, teamwork.WorkspaceBinding{
			MainSessionID: u.SessionPath(),
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
		if nodeID := workItemNodeID(scene.Worktree); nodeID != "" {
			scene.NodeID = nodeID
		}
		h.record(ctx, u, teamUnitStatusRunning, own.Goal, "running")
		return scene, nil
	}
	scope := model.NodeScope{NodeID: scene.NodeID, Role: model.AccountRole(own.Role)}
	if wt := h.r.beginNodeWorktree(scope, scene.NodeID); wt != nil {
		scene.Worktree = wt.Branch
	}
	return scene, nil
}

// AlreadySettled 回答"这件事已经收过尾了吗"——**重入**的唯一判据，两层共用。
//
//	团队归属：计划里这一件事已经不是 running（收口过 / 被重派）⇒ 已经收过尾；
//	没有归属：会话账本里这一份的记录已落终态（不再是 running/queued）⇒ 已经收过尾。
//
// 已经收过尾时 `Finish` 交回**零值结论**：不重复合并、不重复回执、不重复判定——
// 结论已经落盘（计划 / 会话记录里读得到）。
func (h *lifecycleHost) AlreadySettled(ctx context.Context, u workunit.Unit) (bool, error) {
	if h == nil || h.r == nil {
		return false, errLifecycleHostUnavailable
	}
	own := u.Owns()
	if h.teamOwned(ctx, u) {
		coordinator, err := h.r.coordinatorForSession(u.SessionPath())
		if err != nil {
			return false, err
		}
		return coordinator.ItemSettled(ctx, own.ItemID), nil
	}
	record, found, err := h.recordFor(ctx, u)
	if err != nil {
		return false, err
	}
	if !found {
		// 没有记录：只有"现场也不在了"才算收过尾（现场在 = 这一轮还没收过；两者都不在
		// = 已经回收过，没有可收的尾）。
		return h.r.nodeWorktreeFor(u.ID()) == nil, nil
	}
	return !workunit.InFlight(record.Status), nil
}

// Finish 只回答"这一轮怎么结束的"：分类 + 合并 + 回执；不拆现场。唯一实现。
//
// 四条此前各写一遍的判据集中在这里：
//
//	① 重入：已经收过尾 ⇒ 零值结论（AlreadySettled）；
//	② 合并**只做一次**：调用方已经拿到的合并结果非空即采信，不再合第二次；
//	③ 分类只有一份（workunit.ClassifyFinish）：未提交 / 挡路不判死、其余合并错误判死；
//	④ 落定之后才按策略回收（FinishPolicy.AfterFinish 恰一次，调的是同一个 Reclaim）——
//	   未落定时现场保留，因为"未提交改动"是人的资产（契约不变式 1）。
func (h *lifecycleHost) Finish(ctx context.Context, u workunit.Unit, result workunit.Result, mergeErr error) (workunit.Outcome, error) {
	if h == nil || h.r == nil {
		return workunit.Outcome{}, errLifecycleHostUnavailable
	}
	settled, err := h.AlreadySettled(ctx, u)
	if err != nil {
		// 读不回来 = 不知道，按"还没收过尾"继续（与既有口径一致）：一次账本读失败
		// 不该被当成"这件事已经收过尾"而静默丢掉收尾动作，也不该判死整件事。
		log.Printf("seelebridge: 判定 %q 是否已收尾失败（按未收尾继续）：%v", u.ID(), err)
	} else if settled {
		return workunit.Outcome{}, nil
	}
	own := u.Owns()
	if mergeErr == nil {
		mergeErr = h.merge(ctx, u)
	}
	if h.teamOwned(ctx, u) {
		// 团队托管：回执（尾插）与状态写回计划是团队那条路的事（结束事实各有各的载体）。
		// 分类仍然只有一份（workunit.ClassifyFinish，就在它的写态半段里），这里只交回读数。
		coordinator, err := h.r.coordinatorForSession(u.SessionPath())
		if err != nil {
			return workunit.Outcome{}, err
		}
		outcome, err := coordinator.SettleWorkItemWith(ctx, teamUnitRequest(u), result.Err, mergeErr)
		if err != nil {
			return outcome, err
		}
		if outcome.Kind == "" {
			return outcome, nil // 再确认一次：收尾期间已被收口（幂等）
		}
		h.r.settleTeamUnitRecord(u.SessionPath(), teamUnitRecordKey{
			NodeID:        u.ID(),
			RoleSessionID: own.RoleSessionID,
			Goal:          own.Goal,
		}, outcome)
		// 合并失败**不在这里报错**：团队托管的结束事实是回执 + 计划状态，收尾分类已经
		// 把"没合进去"写进其中；把它再当一次"尾插失败"报出去，会让一条正常结论看起来
		// 像收尾崩了（执行体对尾插错误只会记一行 note，但那一行是误导）。
		return outcome, nil
	}
	outcome := workunit.ClassifyFinish(result, mergeErr)
	if outcome.Settled() {
		if err := u.Policy().AfterFinish(ctx, h, u); err != nil {
			return outcome, err
		}
	}
	// 没有团队归属的层（subagent）：合并失败原文交回调用方——节点域用它写产出警告、
	// 并对"收尾撞了别的错"那一类判死（ClassifyFinish 同口径）。
	return outcome, mergeErr
}

// Reclaim 拆这一份：唯一入口，幂等。唯一实现。
//
//	有作业面的层（团队托管）：作业 → 现场 → 会话内容（收口四步的**前三步**，唯一实现是
//	   `Coordinator.Reclaim`；步 4 的名册动作只属于整队收口）；
//	没有作业面的层（subagent）：拆现场 + 清会话记录（一件活跑完就结束，它名下没有作业
//	  面，所以这里**不收作业**——"要不要收作业"是数据判据：有没有作业面）。
//
// **两个策略调的都是这一个函数**，差别只在调用点：`Immediate` 在 Finish 落定时调它，
// `AtTeamClose` 留到整队收口调它（那条路落在 `Coordinator.reclaimStepsLocked` 上，
// 与这里共用同一份"拆现场 + 清会话 + 回收作业"）。
func (h *lifecycleHost) Reclaim(ctx context.Context, u workunit.Unit) error {
	if h == nil || h.r == nil {
		return errLifecycleHostUnavailable
	}
	own := u.Owns()
	if h.teamOwned(ctx, u) {
		coordinator, err := h.r.coordinatorForSession(u.SessionPath())
		if err != nil {
			return err
		}
		if err := coordinator.Reclaim(ctx, own.Role); err != nil {
			return err
		}
		h.r.clearTeamUnitRecord(u.SessionPath(), own.RoleSessionID)
		return nil
	}
	if h.spaces != nil {
		h.spaces.Release(u.ID())
	}
	record, found, err := h.recordFor(ctx, u)
	if err != nil {
		return err
	}
	if !found {
		return nil // 没有会话记录可删（未装配持久化，或已经回收过）
	}
	if h.ledger == nil {
		return nil
	}
	key, ok := h.sessionKey(ctx, u)
	if !ok {
		return nil
	}
	if err := h.ledger.Delete(key.ProjectID, key.SessionID, record.SessionID); err != nil {
		return fmt.Errorf("workunit: reclaim session record for %q: %w", u.ID(), err)
	}
	return nil
}

// Recover 重启回灌：先认领现场（**必须在 Prune 之前**），再按**会话级**粒度读回这一层的
// 全部单元记录；本单元那一份才是结论（中断清单只报它）。唯一实现。
//
//	团队托管：`RecoverTeamworkUnits`（认领团队现场 → 团队账本读数 → 会话回灌 → 注入恢复说明）；
//	subagent：`RestoreSubagentAnchors`（认领 + 回灌 + Prune）+ 读回该会话全部单元记录。
func (h *lifecycleHost) Recover(ctx context.Context, u workunit.Unit) (workunit.Resume, error) {
	if h == nil || h.r == nil {
		return workunit.Resume{}, errLifecycleHostUnavailable
	}
	sessionID := strings.TrimSpace(u.SessionPath())
	if sessionID == "" {
		return workunit.Resume{}, nil
	}
	if h.teamOwned(ctx, u) {
		recovery, err := h.r.RecoverTeamworkUnits(ctx, sessionID)
		if err != nil {
			return recovery.Resume, err
		}
		return recovery.Resume, nil
	}
	if err := h.r.RestoreSubagentAnchors(sessionID); err != nil {
		return workunit.Resume{}, err
	}
	records, err := h.sessionRecords(ctx, u)
	if err != nil {
		return workunit.Resume{}, err
	}
	resume := workunit.Resume{Sessions: len(records)}
	if h.r.nodeWorktreeFor(u.ID()) != nil {
		resume.Scenes = 1
	}
	for _, record := range records {
		if record.NodeID != u.ID() || !workunit.InFlight(record.Status) {
			continue
		}
		// 记录说在跑、而本进程已无它的执行面 ⇒ 中断（交上层重跑或人工处置）。
		// 判定复用既有的 recordBelongsToCurrentMain（不另立第二条判据）。
		if h.r.recordBelongsToCurrentMain(record) {
			resume.Interrupted = []string{u.ID()}
		}
	}
	return resume, nil
}

// Notice 生成给人看的说明：**非落定必带处置办法**（合同见 workunit.Outcome）。
//
// 处置办法不是在这里现编的：它就是分类器（workunit.ClassifyFinish）拼进 Notice 的那一段，
// 父只负责把它交回调用方（节点警告 / 回执 / 看板）。分类一旦漂移，这里立刻看得出来。
func (h *lifecycleHost) Notice(outcome workunit.Outcome) string {
	if strings.TrimSpace(outcome.Notice) != "" {
		return outcome.Notice
	}
	if outcome.Settled() {
		return "跑完待验收"
	}
	return "收尾未落定：" + string(outcome.Kind)
}

// ── 父的内部动作（只有父能拿到端口）──────────────────────────────────────

// merge 合并这一份现场一次（**只做一次**）：团队托管走团队的合并面，其余走现场端口。
func (h *lifecycleHost) merge(ctx context.Context, u workunit.Unit) error {
	if h.teamOwned(ctx, u) {
		coordinator, err := h.r.coordinatorForSession(u.SessionPath())
		if err != nil {
			return err
		}
		return coordinator.MergeWorkItem(ctx, teamUnitRequest(u))
	}
	return h.r.finishNodeWorktree(ctx, u.ID(), h.r.nodeWorktreeFor(u.ID()))
}

// record 落一条本层的会话记录（"这一轮跑到哪、现场在哪"）：团队那条路复用既有的
// 记录写面（`saveTeamUnitRecord`，唯一实现），不在父里再写第二份记录格式。
func (h *lifecycleHost) record(ctx context.Context, u workunit.Unit, status, summary, stage string) {
	own := u.Owns()
	if !h.teamOwned(ctx, u) {
		return // subagent 的记录由节点会话面自己写（RegisterNodeSession 那条路）
	}
	h.r.saveTeamUnitRecord(u.SessionPath(), teamUnitRecordKey{
		NodeID:        u.ID(),
		RoleSessionID: own.RoleSessionID,
		Goal:          own.Goal,
	}, status, summary, stage)
}

// recordFor 读回**本单元**的会话记录（会话级读回的一个切片；没有存储/没有记录 = false）。
func (h *lifecycleHost) recordFor(ctx context.Context, u workunit.Unit) (sessionstore.NodeSessionRecord, bool, error) {
	records, err := h.sessionRecords(ctx, u)
	if err != nil {
		return sessionstore.NodeSessionRecord{}, false, err
	}
	nodeID := u.ID()
	for _, record := range records {
		if record.NodeID == nodeID {
			return record, true, nil
		}
	}
	return sessionstore.NodeSessionRecord{}, false, nil
}

// sessionRecords 按**会话路径**读回该会话的全部单元记录（恢复的粒度是会话级：
// 两层读的是同一份记录清单，差别只在账本键怎么解析——解析在 sessionKey 一处）。
func (h *lifecycleHost) sessionRecords(ctx context.Context, u workunit.Unit) ([]sessionstore.NodeSessionRecord, error) {
	if h.ledger == nil {
		return nil, nil
	}
	key, ok := h.sessionKey(ctx, u)
	if !ok {
		return nil, nil
	}
	records, err := h.ledger.List(key.ProjectID, key.SessionID)
	if err != nil {
		return nil, fmt.Errorf("workunit: list session records for %q: %w", u.SessionPath(), err)
	}
	return records, nil
}

// teamUnitRequest 把归属读数折成团队收尾那条路认识的载荷（它只读 WorkItemID 与归属，
// 因此不需要重新搬一遍派发时的全部字段）。
func teamUnitRequest(u workunit.Unit) teamwork.WorkerRequest {
	own := u.Owns()
	return teamwork.WorkerRequest{
		MainSessionID: u.SessionPath(),
		TeamID:        own.TeamID,
		Role:          own.Role,
		RoleSessionID: own.RoleSessionID,
		WorkItemID:    own.ItemID,
		Milestone:     own.Milestone,
		Goal:          own.Goal,
	}
}

// ── 生产驱动点：subagent 层（node 域只认这三个回调）─────────────────────

// beginNodeUnit 驱动 subagent 层工作单元的 Begin（现场的唯一建法仍在父里）。
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
