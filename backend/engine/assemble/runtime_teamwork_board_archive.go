package assemble

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
	"encoding/json"
	"log"
	"time"

	"github.com/RedHuang-0622/Seele/jobs"

	"github.com/RedHuang-0622/seelex/backend/multiagent/teamwork"
	"github.com/RedHuang-0622/seelex/contract/dto"
	seeletelemetry "github.com/RedHuang-0622/seelex/internal/telemetry"
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
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
	// 作用域解析只有一份判据（runtime_teamwork_scope.go）；存档面额外要求 Boards + Store。
	backend, manager, key, err := r.resolveTeamworkScope(seeletelemetry.SessionIDFromContext(ctx))
	if err != nil || backend.Boards == nil || backend.Store == nil {
		return
	}
	plan, err := backend.Store.ReadPlan(ctx, key)
	if err != nil || !teamworkPlanHasOrchestration(plan) {
		return // 没有可看的编排：存档没有意义（与读侧的空壳口径一致）。
	}
	if plan.State.State == storeteamwork.StateClosed {
		// 整队已收口：连同**收口时的最终投影**一起封板（幂等）。**绝不能让下面的
		// "新开一版"逻辑把它复活**——复活一块已关闭的看板，就是把"已关闭"当成
		// "在册"卖给读侧（存档读侧只认 state=active）。收口之后 leader 仍可能再调
		// team_milestone 之类的团队级工具，那些调用也会走到这里，所以这道闸卡在写侧唯一入口上。
		if err := sealClosedTeamBoard(ctx, backend, manager, key, plan, time.Now().UTC(), r.teamBoardAssemblies(plan)); err != nil {
			log.Printf("seelex: 团队看板封板失败（会话 %s）：%v", key.SessionID, err)
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
	payload, err := json.Marshal(buildTeamworkBoardView(plan, events, bindings, records, backend.MaxTeammates, teamworkJobLimit(backend.MaxJobs), r.teamBoardAssemblies(plan)))
	if err != nil {
		log.Printf("seelex: 团队看板存档编码失败（会话 %s）：%v", key.SessionID, err)
		return
	}
	meta, write, err := nextTeamBoardMeta(ctx, backend.Boards, key, plan, payload, time.Now().UTC())
	if err != nil {
		log.Printf("seelex: 团队看板存档生命周期推进失败（会话 %s）：%v", key.SessionID, err)
		return
	}
	if !write {
		return // §7 指纹节流：载荷不变不写盘。
	}
	if err := backend.Boards.WriteTeamBoard(ctx, key, meta); err != nil {
		// §9：写存档失败不改变控制流——看板照常下发，只记一行。
		log.Printf("seelex: 团队看板存档写入失败（会话 %s）：%v", key.SessionID, err)
	}
}

// 同身份别名：判据只有一份实现（团队域 board_archive.go，P21-24 归位）。
var (
	nextTeamBoardMeta = teamwork.NextBoardMeta
	boardFingerprint  = teamwork.BoardFingerprint
)

// readTeamBoardArchive 读存档并还原成看板投影（判据在域包）。
func (r *Runtime) readTeamBoardArchive(backend *TeamworkBackend, key dto.SessionKey) *dto.TeamworkBoardView {
	if backend == nil || backend.Boards == nil {
		return nil
	}
	return teamwork.ReadBoardArchive(backend.Boards, key)
}
