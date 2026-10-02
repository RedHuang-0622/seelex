package seelebridge

// runtime_teamwork_board_close.go — 团队看板存档的**封板**面（team_close 的下游一侧）。
//
// 生态位：整队收口的领域事实在计划（sessionstore.TeamworkPlan.State，U3 裁决），
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
	"errors"
	"io/fs"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/jobs"

	seeletelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/sessionstore"
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
	r.teamworkMu.Lock()
	backend := r.teamworkBackend
	r.teamworkMu.Unlock()
	if backend == nil || backend.Boards == nil || backend.KeyFor == nil {
		return nil
	}
	sessionID := strings.TrimSpace(seeletelemetry.SessionIDFromContext(ctx))
	if sessionID == "" {
		return nil
	}
	key, ok := backend.KeyFor(sessionID)
	if !ok {
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
func sealClosedTeamBoard(ctx context.Context, backend *TeamworkBackend, manager jobs.Manager, key sessionstore.Key, plan sessionstore.TeamworkPlan, now time.Time) error {
	if backend == nil || backend.Boards == nil {
		return nil
	}
	var events []sessionstore.TeamworkEvent
	if backend.Store != nil {
		events, _ = backend.Store.ReadEvents(ctx, key) // 审计读不出来不该让封板作废（与采集面同口径）。
	}
	var records []jobs.Record
	if manager != nil {
		records = manager.Snapshot(jobs.Scope{Session: key.SessionID})
	}
	// 与下发/存档**同一条组装路径**（buildTeamworkBoardView）：封板存档也要与活体投影同形。
	payload, err := json.Marshal(buildTeamworkBoardView(plan, events, records, backend.MaxTeammates))
	if err != nil {
		return err
	}
	return sealTeamBoard(ctx, backend.Boards, key, payload, now)
}

// sealTeamBoard 把一版**在册的**看板存档显式收口；payload 非空时顺带刷新快照内容。
//
// 幂等：已 closed / 无存档（没写过，或上一次收口已把它退场）都原样返回——封板是
// "把在册的那一版关掉"，没有在册的一版就没有可关的东西（不是失败）。
func sealTeamBoard(ctx context.Context, repository sessionstore.BoardRepository, key sessionstore.Key, payload json.RawMessage, now time.Time) error {
	if repository == nil {
		return nil
	}
	existing, err := repository.ReadTeamBoard(ctx, key)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if existing.Closed() {
		return nil
	}
	if len(payload) > 0 {
		existing.Snapshot = payload
	}
	existing.State = sessionstore.BoardStateClosed
	existing.ClosedAt = now.Unix()
	existing.ClosedReason = sessionstore.BoardCloseTeamClose
	existing.UpdatedAt = now.Unix()
	return repository.WriteTeamBoard(ctx, key, existing)
}
