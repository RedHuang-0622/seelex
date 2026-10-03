package seelebridge

// runtime_teamwork_board_archive.go — 团队看板**存档**（会话粒度元数据 + 重启快照恢复）。
//
// 契约：docs/arch/session-board-metadata-lifecycle.md（§3 存档形状 / §5 生命周期 /
// §6 重启恢复 / §7 指纹节流 / §8 收口 / §9 失败语义）。
//
// 两个方向：
//   - 写：team_* 动作成功后，用**与下发前端同一条组装路径**（buildTeamworkBoardView）
//     生成载荷，按生命周期规则刷进 metadata/board_team.json；
//   - 读：活体给不出看板（无计划 / 计划没有阶段）时，从存档恢复并标 Recovered。
//
// 失败语义（§9）：写存档失败**不改变控制流**——看板照常下发，只记一行日志。
// 存档是"锦上添花"的恢复面，不是控制面：它坏掉不该让 leader 的编排停下来。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/jobs"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	seeletelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// archiveTeamBoard 在 team_* 动作成功后刷新团队看板存档（best-effort）。
//
// 它自己**不判定生命周期**：读回现存档、按 §5 规则推进元数据、写回（nextTeamBoardMeta）。
// 载荷来自与下发完全相同的组装路径——存档与投影共用一条路径，重启恢复出来的看板
// 才与活体下发的同形。
func (r *Runtime) archiveTeamBoard(ctx context.Context) {
	if r == nil {
		return
	}
	r.teamworkMu.Lock()
	backend, manager := r.teamworkBackend, r.teamworkJobs
	r.teamworkMu.Unlock()
	if backend == nil || backend.Boards == nil || backend.Store == nil || backend.KeyFor == nil {
		return
	}
	sessionID := strings.TrimSpace(seeletelemetry.SessionIDFromContext(ctx))
	if sessionID == "" {
		return
	}
	key, ok := backend.KeyFor(sessionID)
	if !ok {
		return
	}
	plan, err := backend.Store.ReadPlan(ctx, key)
	if err != nil || !teamworkPlanHasOrchestration(plan) {
		return // 没有可看的编排：存档没有意义（与读侧的空壳口径一致）。
	}
	if plan.State.State == sessionstore.TeamworkStateClosed {
		// 整队已收口：连同**收口时的最终投影**一起封板（幂等）。**绝不能让下面的
		// "新开一版"逻辑把它复活**——复活一块已关闭的看板，就是把"已关闭"当成
		// "在册"卖给读侧（存档读侧只认 state=active）。收口之后 leader 仍可能再调
		// team_retire/team_milestone，那些调用也会走到这里，所以这道闸卡在写侧唯一入口上。
		if err := sealClosedTeamBoard(ctx, backend, manager, key, plan, time.Now().UTC()); err != nil {
			log.Printf("seelebridge: 团队看板封板失败（会话 %s）：%v", sessionID, err)
		}
		return
	}
	events, err := backend.Store.ReadEvents(ctx, key)
	if err != nil {
		events = nil // 审计读不出来不该让整份存档作废（与采集面同口径）。
	}
	var records []jobs.Record
	if manager != nil {
		records = manager.Snapshot(jobs.Scope{Session: key.SessionID})
	}
	bindings, bindErr := backend.Store.ReadBindings(ctx, key)
	if bindErr != nil {
		bindings = nil // 绑定读不出来不该让整份存档作废（与采集面同口径）。
	}
	payload, err := json.Marshal(buildTeamworkBoardView(plan, events, bindings, records, backend.MaxTeammates))
	if err != nil {
		log.Printf("seelebridge: 团队看板存档编码失败（会话 %s）：%v", sessionID, err)
		return
	}
	meta, write, err := nextTeamBoardMeta(ctx, backend.Boards, key, plan, payload, time.Now().UTC())
	if err != nil {
		log.Printf("seelebridge: 团队看板存档生命周期推进失败（会话 %s）：%v", sessionID, err)
		return
	}
	if !write {
		return // §7 指纹节流：载荷不变不写盘。
	}
	if err := backend.Boards.WriteTeamBoard(ctx, key, meta); err != nil {
		// §9：写存档失败不改变控制流——看板照常下发，只记一行。
		log.Printf("seelebridge: 团队看板存档写入失败（会话 %s）：%v", sessionID, err)
	}
}

// nextTeamBoardMeta 按生命周期规则推进存档元数据（§5），并报告"是否需要写盘"（§7）。
//
// 三条规则：
//   - 无存档 / 已退场 → 新开一版 active（opened_at=now、seq=1）；
//   - 已有 active 且版本不同 → 旧版显式收口（closed_reason=team.replan），再写
//     新的 active（opened_at=now、seq=1）——**先关旧、后开新**，写序即事实；
//   - 已有 active 且版本相同 → 只 seq+1 与 updated_at（opened_at 不动）。
//
// 指纹不变（且版本相同）时返回 write=false：载荷不变不写盘。
func nextTeamBoardMeta(ctx context.Context, repository sessionstore.BoardRepository, key sessionstore.Key, plan sessionstore.TeamworkPlan, payload json.RawMessage, now time.Time) (sessionstore.TeamBoardMeta, bool, error) {
	meta := sessionstore.TeamBoardMeta{
		BoardLifecycle: sessionstore.BoardLifecycle{
			Kind: sessionstore.BoardKindTeam, State: sessionstore.BoardStateActive,
			Seq: 1, OpenedAt: now.Unix(), UpdatedAt: now.Unix(), Fingerprint: boardFingerprint(payload),
		},
		TeamID: plan.TeamID, Version: plan.Version, Snapshot: payload,
	}
	existing, err := repository.ReadTeamBoard(ctx, key)
	if err != nil {
		// 无存档（fs.ErrNotExist）与读失败同解：按"第一次写"处理。读不出来时
		// 覆盖写也是唯一的自愈路径（存档没有独立数据文件可重建）。
		return meta, true, nil
	}
	if existing.State != sessionstore.BoardStateActive {
		return meta, true, nil // 已退场的那一版不再续命：新开一版。
	}
	if existing.Version == plan.Version && existing.Fingerprint == meta.Fingerprint {
		return sessionstore.TeamBoardMeta{}, false, nil // §7 节流。
	}
	if existing.Version != plan.Version {
		closed := existing
		closed.State = sessionstore.BoardStateClosed
		closed.ClosedAt = now.Unix()
		closed.ClosedReason = sessionstore.BoardCloseTeamReplan
		closed.UpdatedAt = now.Unix()
		if err := repository.WriteTeamBoard(ctx, key, closed); err != nil {
			return sessionstore.TeamBoardMeta{}, false, err
		}
		return meta, true, nil // 新版本：opened_at/seq=1（meta 已就位）。
	}
	// 同版本刷新：续接 opened_at，只推进 seq 与 updated_at。
	meta.OpenedAt = existing.OpenedAt
	meta.Seq = existing.Seq + 1
	return meta, true, nil
}

// readTeamBoardArchive 读存档并还原成看板投影（§6 重启恢复）。
//
// 只有 state=active 才恢复：state=closed / 无存档 / 快照坏了都返回 nil——
// 看板退场就是退场（与"活体没有阶段就不留空壳"同一口径）。
//
// 恢复出的看板 Recovered=true，且 Stale=true：存档里的作业行来自上一个进程，
// jobs 句柄只在内存（jobs I-4），恢复出来的句柄一律视为过期。
func (r *Runtime) readTeamBoardArchive(backend *TeamworkBackend, key sessionstore.Key) *dto.TeamworkBoardView {
	if backend == nil || backend.Boards == nil {
		return nil
	}
	meta, err := backend.Boards.ReadTeamBoard(context.Background(), key)
	if err != nil || meta.State != sessionstore.BoardStateActive || len(meta.Snapshot) == 0 {
		return nil
	}
	var view dto.TeamworkBoardView
	if err := json.Unmarshal(meta.Snapshot, &view); err != nil {
		return nil
	}
	if len(view.Stages) == 0 {
		return nil // 存档里也没有可看的编排：不留空壳。
	}
	view.Recovered = true
	view.Stale = true
	return &view
}

// boardFingerprint 是存档载荷的指纹（§7 节流判据）：同一份载荷 → 同一个值。
func boardFingerprint(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
