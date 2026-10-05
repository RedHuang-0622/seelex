package seelebridge

// workunit_parent.go — 一件活的生命周期的**唯一一份实现**（父）。本文件只写"怎么判"，不写
// "谁去做"：它持有的三个字段全是**端口**（sceneFace / unitFace / recordFace，实现在装配处
// workunit_assembly.go），因此文件里搜不到任何一个 `teamwork` / `worktree` / `sessionstore`
// 的具体类型，也搜不到按层分支。
//
// 口径（docs/arch/workunit-single-lifecycle-one-implementation.md §3 接口先行 + §7 纪律，
// docs/arch/workunit-ports-and-assembly.md §2 端口清单 + §3 装配矩阵）：一个工作单元的生命周期
// （建现场 → 收尾 → 回收 → 重启接着做）只有**一份实现**；subagent 与 teammate 只做
// **在父实现上注册 + 转发**。层与层之间唯一的差别是注册点交进来的**读数**
// （contract.go 的 `workunit.Unit`）：
//
//	Kind / ID（本单元现场的身份）/ SessionPath（自己的会话路径）/ Policy（什么时候回收）/
//	Owns（归属：团队 · 工作项 · 角色 · 这一轮的目标 · 显式现场名）
//
// 父持有三格端口（字段不导出，只有父能拿到）：
//
//	scenes   sceneFace    // 现场：建 / 合并 / 认领 / 拆单体 / 在册读数
//	units    unitFace     // 编排闸门 + 账本：归属 / 重入 / 回执 / 单个回收 / 回灌
//	records  recordFace   // 会话记录：状态读数 / 回灌读数 / 落盘 / 写终态 / 清记录
//
// 为什么不是"包一层"：四个动作的**判据与顺序**（重入怎么回答、合并只做一次、恢复的粒度、
// 拆的时候收不收作业）此前在两个实现里各写了一遍且互相漂移；现在它们只写在下面这四个
// 方法里，注册点的方法体一律是 `return u.host.Xxx(ctx, u.read)`。
//
// 唯一允许出现的"按层判断"是**归属读数**（`teamOwned`：这一份归不归 team 托管）——它是数据
// 判据（TeamID / 工作项在不在计划里），不是 `if kind ==`：契约里 Kind 是描述性的，不是分支
// 判据（见 contract.go）。

import (
	"context"
	"errors"
	"log"
	"strings"

	"github.com/RedHuang-0622/seelex/seelebridge/workunit"
)

// ── 父：唯一实现，持有端口（字段不导出）──────────────────────────────────

// lifecycleHost 是一件活的生命周期的唯一实现。端口**不导出**：两个注册点只持有
// `workunit.Lifecycle`（就是它）与自己的 `workunit.Unit` 读数，拿不到下面任何一个端口。
type lifecycleHost struct {
	scenes  sceneFace
	units   unitFace
	records recordFace
}

// 编译期钉住"这就是契约那份父实现"：契约漂移先红。
var _ workunit.Lifecycle = (*lifecycleHost)(nil)

// errLifecycleHostUnavailable 是缺装配时的显式错误（缺的是宿主，不是"静默降级"）。
var errLifecycleHostUnavailable = errors.New("workunit: 生命周期宿主未装配")

// newLifecycleHost 是**装配处**：把宿主能力面接成端口（构造式注入，不读全局）。
//
// 缺宿主（r == nil）时三格都不装：动作随即以 `errLifecycleHostUnavailable` 显式失败——
// 缺装配是错误，不是"静默降级成没有现场"。
func newLifecycleHost(r *Runtime) *lifecycleHost {
	if r == nil {
		return &lifecycleHost{}
	}
	ports := newHostPorts(r)
	return &lifecycleHost{scenes: ports, units: ports, records: ports}
}

// teamOwned 报告这一份是不是**归团队托管**（生命周期里唯一允许出现的"按层"判断，依据是
// **归属读数**：TeamID，或"这件工作项在计划里"）。归属判定只有一份实现（装配处的
// `hostPorts.Owned`），这里只是转发。
func (h *lifecycleHost) teamOwned(ctx context.Context, u workunit.Unit) bool {
	if h == nil || h.units == nil {
		return false
	}
	return h.units.Owned(ctx, u.SessionPath(), u.Owns())
}

// Begin 建（或复用）这一份现场：唯一实现。
//
//	团队托管的（teammate）→ 走编排面上的现场绑定（幂等，绝不 force-remove 已存在的现场），
//	                         并把"这一轮开始了、现场在哪"落盘；
//	其余（subagent）      → 按角色读数给现场（entry 角色降级共享主工作区 = 没有独立现场）。
//
// 选路依据是**归属读数**，不是 Kind；具体走哪条路由端口实现（装配处）决定。
func (h *lifecycleHost) Begin(ctx context.Context, u workunit.Unit) (workunit.Scene, error) {
	if h == nil || h.scenes == nil {
		return workunit.Scene{}, errLifecycleHostUnavailable
	}
	own := u.Owns()
	scene, err := h.scenes.BeginScene(ctx, own, u.ID(), u.SessionPath())
	if err != nil {
		return workunit.Scene{}, err
	}
	scene.Kind = u.Kind()
	if h.teamOwned(ctx, u) {
		h.records.SaveRecord(ctx, own, u.ID(), u.SessionPath(), teamUnitStatusRunning, own.Goal, "running")
	}
	return scene, nil
}

// AlreadySettled 回答"这件事已经收过尾了吗"——**重入**的唯一判据，两层共用。
//
//	团队托管：计划里这一件事已经不是 running（收口过 / 被重派）⇒ 已经收过尾；
//	其余    ：会话账本里这一份的记录已落终态（不再是 running/queued）⇒ 已经收过尾。
//
// 已经收过尾时 `Finish` 交回**零值结论**：不重复合并、不重复回执、不重复判定——
// 结论已经落盘（计划 / 会话记录里读得到）。
func (h *lifecycleHost) AlreadySettled(ctx context.Context, u workunit.Unit) (bool, error) {
	if h == nil || h.records == nil {
		return false, errLifecycleHostUnavailable
	}
	own := u.Owns()
	if h.teamOwned(ctx, u) {
		return h.units.Settled(ctx, u.SessionPath(), own.ItemID)
	}
	status, found, err := h.records.Status(ctx, own, u.ID(), u.SessionPath())
	if err != nil {
		return false, err
	}
	if !found {
		// 没有记录：只有"现场也不在了"才算收过尾（现场在 = 这一轮还没收过；两者都不在
		// = 已经回收过，没有可收的尾）。
		return !h.scenes.SceneRegistered(u.ID()), nil
	}
	return !workunit.InFlight(status), nil
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
	if h == nil || h.scenes == nil {
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
		mergeErr = h.scenes.MergeScene(ctx, own, u.ID(), u.SessionPath())
	}
	if h.teamOwned(ctx, u) {
		// 团队托管：回执（尾插）与状态写回计划是编排那条路的事（结束事实各有各的载体）。
		// 分类仍然只有一份（workunit.ClassifyFinish，就在它的写态半段里），这里只交回读数。
		outcome, err := h.units.SettleUnit(ctx, u.SessionPath(), own, result.Err, mergeErr)
		if err != nil {
			return outcome, err
		}
		if outcome.Kind == "" {
			return outcome, nil // 再确认一次：收尾期间已被收口（幂等）
		}
		h.records.SettleRecord(ctx, own, u.ID(), u.SessionPath(), outcome)
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
//	归团队托管：作业 → 现场 → 会话内容（收口四步的**前三步**，唯一实现在编排端口；
//	  步 4 的名册动作只属于整队收口）；
//	其余（subagent）：拆现场 + 清会话记录（一件活跑完就结束，它名下没有作业面，所以这里
//	  **不收作业**——"要不要收作业"是数据判据：有没有作业面）。
//
// **两个策略调的都是这一个函数**，差别只在调用点：`Immediate` 在 Finish 落定时调它，
// `AtTeamClose` 留到整队收口调它（那条路落在编排端口的 Reclaim 上，与这里共用同一份
// "拆现场 + 清会话 + 回收作业"）。
func (h *lifecycleHost) Reclaim(ctx context.Context, u workunit.Unit) error {
	if h == nil || h.records == nil {
		return errLifecycleHostUnavailable
	}
	own := u.Owns()
	if h.teamOwned(ctx, u) {
		if err := h.units.ReclaimUnit(ctx, u.SessionPath(), own.Role); err != nil {
			return err
		}
		return h.records.Clear(ctx, own, u.ID(), u.SessionPath())
	}
	h.scenes.ReleaseScene(u.ID())
	return h.records.Clear(ctx, own, u.ID(), u.SessionPath())
}

// Recover 重启回灌：先认领现场（**必须在 Prune 之前**），再按**会话级**粒度读回这一层的
// 全部单元记录；本单元那一份才是结论（中断清单只报它）。唯一实现。
//
//	归团队托管：编排端口那一份回灌（认领团队现场 → 账本读数 → 会话回灌 → 注入恢复说明）；
//	其余（subagent）：认领现场（+ 回灌 + Prune）+ 读回该会话全部单元记录。
func (h *lifecycleHost) Recover(ctx context.Context, u workunit.Unit) (workunit.Resume, error) {
	if h == nil || h.scenes == nil || h.records == nil {
		return workunit.Resume{}, errLifecycleHostUnavailable
	}
	sessionPath := strings.TrimSpace(u.SessionPath())
	if sessionPath == "" {
		return workunit.Resume{}, nil
	}
	if h.teamOwned(ctx, u) {
		return h.units.RecoverUnits(ctx, sessionPath)
	}
	if err := h.scenes.AdoptScenes(ctx, sessionPath); err != nil {
		return workunit.Resume{}, err
	}
	resume, err := h.records.Readout(ctx, u.Owns(), u.ID(), sessionPath)
	if err != nil {
		return workunit.Resume{}, err
	}
	if h.scenes.SceneRegistered(u.ID()) {
		resume.Scenes = 1
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
