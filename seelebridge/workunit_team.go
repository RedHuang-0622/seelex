package seelebridge

// workunit_team.go — teammate 单元在统一契约（seelebridge/workunit）上的**注册点**。
//
// 生命周期**不在这里**：它在父实现（workunit_parent.go 的 lifecycleHost，契约面 =
// `workunit.Lifecycle`）里，只有一份。本文件只做两件事：
//
//	1. 注册：把"我是谁"（Kind / ID / SessionPath / Policy / 归属）声明成一份**读数**
//	   （teamUnitReadings，实现 `workunit.Unit`）；
//	2. 转发：四个动作一律 `return u.host.Xxx(ctx, u.read)`。
//
// 结构体只持有两样东西：`workunit.Lifecycle` 与自己的读数——拿不到 store / worktree /
// jobs / 账本任何一个端口（teammate 的会话记录面在 workunit_team_records.go，属父那一侧）。
// 两个注册点文件加起来一个写端口都不碰，这就是"没有第二份实现"的结构性证据。
//
// 层差异只有读数：teammate 归 team 托管（现场与会话活到 team_close，AtTeamClose），
// 归属是 `<role>-<itemID>`（角色级是 `<role>`），收尾回执与状态写回走团队计划那条路。

import (
	"context"
	"strings"

	"github.com/RedHuang-0622/seelex/seelebridge/teamwork"
	"github.com/RedHuang-0622/seelex/seelebridge/workunit"
)

// ── 记录词表与定位键（纯读数，不碰端口）───────────────────────────────────

// 会话记录的 status 词表（`NodeSessionRecord.Status` 的取值面）。只有"在跑"与两种
// 终态：记录回答的是"这一轮跑到哪"，不是"leader 验收了没有"（那是计划里 item.Status
// 的事——两处各记一份就会漂移）。
const (
	teamUnitStatusRunning = "running"
	teamUnitStatusDone    = "done"
	teamUnitStatusFailed  = "failed"
)

// teamUnitStatusFor 把收尾分类折成记录的终态：**不判死的那两类（未提交 / 主工作区挡路）
// 记 done**——这一轮跑完了、产出还在现场，记录不该把它说成"这一轮失败了"；未合并这件事
// 由计划里的 item.Status(待验收) + plan.State.Unmerged 承载（两处各记一份就会漂移）。
func teamUnitStatusFor(kind workunit.OutcomeKind) string {
	if kind == workunit.OutcomeFailed {
		return teamUnitStatusFailed
	}
	return teamUnitStatusDone
}

// teamUnitRecordKey 是一个 teammate 单元的身份：现场 nodeID + 角色会话 + 这一轮的目标。
type teamUnitRecordKey struct {
	NodeID        string
	RoleSessionID string
	Goal          string
}

// teamUnitNodeID 把一个 teammate 单元的现场折成契约的 nodeID（`<role>-<itemID>` /
// `<role>`）：有指派名就去前缀（换算只有 workItemNodeID 一处），降级共享工作区（没有
// 独立现场）时按归属拼——同一份命名由 teamwork.SceneNodeID 给出。
func teamUnitNodeID(request teamwork.WorkerRequest) string {
	if nodeID := workItemNodeID(request.Worktree); nodeID != "" {
		return nodeID
	}
	if itemID := strings.TrimSpace(request.WorkItemID); itemID != "" {
		return teamwork.SceneNodeID(request.Role, itemID)
	}
	return strings.TrimSpace(request.Role)
}

// teamUnitKeyFor 从一轮作业的载荷取出记录身份（NodeID 的口径见 teamUnitNodeID）。
func teamUnitKeyFor(request teamwork.WorkerRequest) teamUnitRecordKey {
	return teamUnitRecordKey{
		NodeID:        teamUnitNodeID(request),
		RoleSessionID: strings.TrimSpace(request.RoleSessionID),
		Goal:          strings.TrimSpace(request.Goal),
	}
}

// ── 注册点：teammate 层 = 在父实现上注册 ────────────────────────────────

// teamUnitReadings 是 teammate 层的**读数**（实现 `workunit.Unit`）：只有身份与策略。
//
// 粒度：Work Item 级单元的 nodeID = `<role>-<itemID>`（现场指派名
// `seelex/<role>-<itemID>`），角色级（老口径派发）单元 = `<role>`。它自己不认识 git、
// 不认识存储、也不认识作业表——那些都在父实现里。
type teamUnitReadings struct {
	mainSessionID string
	role          string
	scene         workunit.Scene
	request       teamwork.WorkerRequest
}

// Kind 报告这一层：teammate（恒定读数：本注册点只服务这一层）。
func (u teamUnitReadings) Kind() workunit.Kind { return workunit.KindTeammate }

// ID 是本单元现场的身份（`<role>-<itemID>` / `<role>`，换算只有 teamUnitNodeID 一处）。
func (u teamUnitReadings) ID() string {
	if nodeID := strings.TrimSpace(u.scene.NodeID); nodeID != "" {
		return nodeID
	}
	return teamUnitNodeID(u.request)
}

// SessionPath 是本层的会话路径 = 归属主会话号（账本键由父按团队归属解析到团队账本）。
func (u teamUnitReadings) SessionPath() string { return u.mainSessionID }

// Policy 是本层的收尾策略：teammate 归 team_close（Finish 之后不动现场与会话）。
func (u teamUnitReadings) Policy() workunit.FinishPolicy { return workunit.AtTeamClose{} }

// Owns 是归属读数：把 `teamwork.WorkerRequest` 折成契约自己的小结构——折算只有这一处，
// 契约包因此不需要认识 teamwork（依赖方向是 teamwork → workunit，不是反过来）。
func (u teamUnitReadings) Owns() workunit.Ownership {
	return workunit.Ownership{
		TeamID:        strings.TrimSpace(u.scene.TeamID),
		ItemID:        strings.TrimSpace(u.request.WorkItemID),
		Role:          strings.TrimSpace(u.role),
		Milestone:     strings.TrimSpace(u.request.Milestone),
		RoleSessionID: strings.TrimSpace(u.request.RoleSessionID),
		Goal:          strings.TrimSpace(u.request.Goal),
		Worktree:      strings.TrimSpace(u.request.Worktree),
	}
}

// 编译期钉住"读数就是契约的 Unit"：契约漂移先红。
var _ workunit.Unit = teamUnitReadings{}

// teamUnit 是 teammate 层的注册点：父实现（契约面）+ 本层读数，方法体只转发。
type teamUnit struct {
	host workunit.Lifecycle
	read teamUnitReadings
}

// newTeamUnit 组装 teammate 层的注册点：父实现 + 本层读数。缺装配（runtime / 主会话 /
// 编排面）时**显式报错**：契约里的动作都落在编排面上，缺了它就没有"这一份现场"可言。
func newTeamUnit(r *Runtime, mainSessionID, role string, scene workunit.Scene, request teamwork.WorkerRequest) (*teamUnit, error) {
	if r == nil {
		return nil, errLifecycleHostUnavailable
	}
	if _, err := r.coordinatorForSession(mainSessionID); err != nil {
		return nil, err
	}
	scene.Kind = workunit.KindTeammate
	if strings.TrimSpace(scene.SessionID) == "" {
		scene.SessionID = strings.TrimSpace(request.RoleSessionID)
	}
	if strings.TrimSpace(scene.NodeID) == "" {
		scene.NodeID = teamUnitNodeID(request)
	}
	return &teamUnit{
		host: newLifecycleHost(r),
		read: teamUnitReadings{
			mainSessionID: strings.TrimSpace(mainSessionID),
			role:          strings.TrimSpace(role),
			scene:         scene,
			request:       request,
		},
	}, nil
}

// FinishPolicy 是本层声明的收尾策略读数（转发给读数；恒定的常量）。
func (u *teamUnit) FinishPolicy() workunit.FinishPolicy { return teamUnitReadings{}.Policy() }

// Begin 建（或复用）这个单元的现场：唯一实现在父里。
func (u *teamUnit) Begin(ctx context.Context) (workunit.Scene, error) {
	return u.host.Begin(ctx, u.read)
}

// Finish 收尾这一轮：唯一实现在父里（重入、只合一次、分类、策略都在那边）。
func (u *teamUnit) Finish(ctx context.Context, result workunit.Result, mergeErr error) (workunit.Outcome, error) {
	return u.host.Finish(ctx, u.read, result, mergeErr)
}

// Reclaim 拆掉这个单元自己这一份：唯一实现在父里。
func (u *teamUnit) Reclaim(ctx context.Context) error {
	return u.host.Reclaim(ctx, u.read)
}

// Recover 重启回灌（会话级：按会话路径读回整支团队的单元）：唯一实现在父里。
func (u *teamUnit) Recover(ctx context.Context) (workunit.Resume, error) {
	return u.host.Recover(ctx, u.read)
}
