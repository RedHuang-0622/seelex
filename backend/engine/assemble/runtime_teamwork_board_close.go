package assemble

// runtime_teamwork_board_close.go — 团队看板存档的**封板**面（team_close 的下游一侧）。
//
// 生态位：整队收口的领域事实在计划（store.TeamworkPlan.State，U3 裁决），
// 本文件只把**存档那一版**关掉——存档是历史/恢复面，它若不封板就会在重启恢复时
// 冒充"在册"（readTeamBoardArchive 只恢复 state=active）。
//
// 两个入口共用同一个实现（sealTeamBoard），因为"封板"只有一个语义：
//   - Coordinator.Close 经 teamwork.BoardCloser 端口调过来（收口这条主路径）；
//   - archiveTeamBoard 在计划已 closed 时调（收口之后 leader 再调别的 team_* 工具，
//     不能让刷新逻辑把已关闭的一版重新开成 active）。

import (
	"context"
	"encoding/json"
	"time"

	"github.com/RedHuang-0622/Seele/jobs"

	"github.com/RedHuang-0622/seelex/backend/multiagent/teamwork"
	"github.com/RedHuang-0622/seelex/contract/dto"
	seeletelemetry "github.com/RedHuang-0622/seelex/internal/telemetry"
	"github.com/RedHuang-0622/seelex/store"
)

// CloseTeamBoard 实现 teamwork.BoardCloser：把当前会话的团队看板存档封板
// （state=closed / closed_reason=team.close）。
//
// 未装配存档面 / 会话归属缺失 → 直接放行（"有就有、没有就是没装配"）：域内 closed 与
// 审计照常落，收口不该因为读不到一块存档而失败。真·写入失败**上抛**——收口方
// （Coordinator.Close）据此保留"域内尚未标 closed"的可重试状态。
func (r *Runtime) CloseTeamBoard(ctx context.Context) error {
	if r == nil {
		return nil
	}
	// 作用域解析只有一份判据（runtime_teamwork_scope.go）；封板面额外要求 Boards。
	backend, _, key, err := r.resolveTeamworkScope(seeletelemetry.SessionIDFromContext(ctx))
	if err != nil || backend.Boards == nil {
		return nil
	}
	// payload=nil：这一条路径不刷新快照，只关在册的那一版（收口瞬间计划还没标 closed，
	// 刷新会写出一份"看起来未收口"的载荷；最终投影由 sealClosedTeamBoard 补上）。
	return sealTeamBoard(ctx, backend.Boards, key, nil, time.Now().UTC())
}

// sealClosedTeamBoard 封板**已收口**团队的那一版存档，并把收口时的最终投影写进去。
//
// 与 CloseTeamBoard 的分工：后者在"计划尚未标 closed"时被收口流程调用（只关板），
// 本函数在计划已 closed 的**刷新**路径上被调用（关板 + 刷新快照），两者共用 sealTeamBoard。
func sealClosedTeamBoard(ctx context.Context, backend *TeamworkBackend, manager jobs.Manager, key store.Key, plan store.TeamworkPlan, now time.Time, assemblies []dto.PluginAssemblyView) error {
	if backend == nil || backend.Boards == nil {
		return nil
	}
	var events []store.TeamworkEvent
	if backend.Store != nil {
		events, _ = backend.Store.ReadEvents(ctx, key) // 审计读不出来不该让封板作废（与采集面同口径）。
	}
	var bindings []store.TeamworkBinding
	if backend.Store != nil {
		bindings, _ = backend.Store.ReadBindings(ctx, key) // 绑定读不出来同理：少了"现场在不在"这一列。
	}
	var records []jobs.Record
	if manager != nil {
		records = manager.Snapshot(jobs.Scope{Session: key.SessionID})
	}
	// 与下发/存档**同一条组装路径**（buildTeamworkBoardView）：封板存档也要与活体投影同形。
	payload, err := json.Marshal(buildTeamworkBoardView(plan, events, bindings, records, backend.MaxTeammates, assemblies))
	if err != nil {
		return err
	}
	return sealTeamBoard(ctx, backend.Boards, key, payload, now)
}

// 同身份别名：封板判据只有一份实现（团队域 board_archive.go，P21-24 归位）。
var sealTeamBoard = teamwork.SealBoard
