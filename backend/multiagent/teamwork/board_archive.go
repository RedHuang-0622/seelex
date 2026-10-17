package teamwork

// board_archive.go 是团队看板**存档的生命周期判据**（P21-24「域方法随类型走」：从
// `backend/engine/assemble/runtime_teamwork_board_archive.go` 与 `..._board_close.go`
// 把判据搬进本域；plan §2.7 把 `runtime_teamwork*.go` 列为 `backend/multiagent/teamwork/`）。
//
// 契约：docs/arch/session-board-metadata-lifecycle.md（§3 存档形状 / §5 生命周期 /
// §6 重启恢复 / §7 指纹节流 / §8 收口 / §9 失败语义）。
//
// 本文件只吃**已经读好的值**与两个端口（`store.BoardRepository` / 计划）：装配层负责"什么时候读、
// 读哪些、失败了怎么记一行日志"，判据（推进 / 节流 / 恢复 / 封板）在这里。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/store"
)

// NextBoardMeta 按生命周期规则推进存档元数据（§5），并报告"是否需要写盘"（§7）。
//
// 三条规则：
//   - 无存档 / 已退场 → 新开一版 active（opened_at=now、seq=1）；
//   - 已有 active 且版本不同 → 旧版显式收口（closed_reason=team.replan），再写
//     新的 active（opened_at=now、seq=1）——**先关旧、后开新**，写序即事实；
//   - 已有 active 且版本相同 → 只 seq+1 与 updated_at（opened_at 不动）。
//
// 指纹不变（且版本相同）时返回 write=false：载荷不变不写盘。
func NextBoardMeta(ctx context.Context, repository store.BoardRepository, key store.Key, plan store.TeamworkPlan, payload json.RawMessage, now time.Time) (store.TeamBoardMeta, bool, error) {
	meta := store.TeamBoardMeta{
		BoardLifecycle: store.BoardLifecycle{
			Kind: store.BoardKindTeam, State: store.BoardStateActive,
			Seq: 1, OpenedAt: now.Unix(), UpdatedAt: now.Unix(), Fingerprint: BoardFingerprint(payload),
		},
		TeamID: plan.TeamID, Version: plan.Version, Snapshot: payload,
	}
	existing, err := repository.ReadTeamBoard(ctx, key)
	if err != nil {
		// 无存档（fs.ErrNotExist）与读失败同解：按"第一次写"处理。读不出来时
		// 覆盖写也是唯一的自愈路径（存档没有独立数据文件可重建）。
		return meta, true, nil
	}
	if existing.State != store.BoardStateActive {
		return meta, true, nil // 已退场的那一版不再续命：新开一版。
	}
	if existing.Version == plan.Version && existing.Fingerprint == meta.Fingerprint {
		return store.TeamBoardMeta{}, false, nil // §7 节流。
	}
	if existing.Version != plan.Version {
		closed := existing
		closed.State = store.BoardStateClosed
		closed.ClosedAt = now.Unix()
		closed.ClosedReason = store.BoardCloseTeamReplan
		closed.UpdatedAt = now.Unix()
		if err := repository.WriteTeamBoard(ctx, key, closed); err != nil {
			return store.TeamBoardMeta{}, false, err
		}
		return meta, true, nil // 新版本：opened_at/seq=1（meta 已就位）。
	}
	// 同版本刷新：续接 opened_at，只推进 seq 与 updated_at。
	meta.OpenedAt = existing.OpenedAt
	meta.Seq = existing.Seq + 1
	return meta, true, nil
}

// ReadBoardArchive 读存档并还原成看板投影（§6 重启恢复）。
//
// 只有 state=active 才恢复：state=closed / 无存档 / 快照坏了都返回 nil——
// 看板退场就是退场（与"活体没有阶段就不留空壳"同一口径）。
//
// 恢复出的看板 Recovered=true，且 Stale=true：存档里的作业行来自上一个进程，
// jobs 句柄只在内存（jobs I-4），恢复出来的句柄一律视为过期。
func ReadBoardArchive(repository store.BoardRepository, key store.Key) *dto.TeamworkBoardView {
	if repository == nil {
		return nil
	}
	meta, err := repository.ReadTeamBoard(context.Background(), key)
	if err != nil || meta.State != store.BoardStateActive || len(meta.Snapshot) == 0 {
		return nil
	}
	var view dto.TeamworkBoardView
	if err := json.Unmarshal(meta.Snapshot, &view); err != nil {
		return nil
	}
	if len(view.Milestones) == 0 {
		return nil // 存档里也没有可看的编排：不留空壳。
	}
	view.Recovered = true
	view.Stale = true
	return &view
}

// SealBoard 把一版**在册的**看板存档显式收口；payload 非空时顺带刷新快照内容。
//
// 幂等：已 closed / 无存档（没写过，或上一次收口已把它退场）都原样返回——封板是
// "把在册的那一版关掉"，没有在册的一版就没有可关的东西（不是失败）。
func SealBoard(ctx context.Context, repository store.BoardRepository, key store.Key, payload json.RawMessage, now time.Time) error {
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
	existing.State = store.BoardStateClosed
	existing.ClosedAt = now.Unix()
	existing.ClosedReason = store.BoardCloseTeamClose
	existing.UpdatedAt = now.Unix()
	return repository.WriteTeamBoard(ctx, key, existing)
}

// BoardFingerprint 是存档载荷的指纹（§7 节流判据）：同一份载荷 → 同一个值。
func BoardFingerprint(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
