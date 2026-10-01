package goal

// board_archive.go — goal 看板的存档写侧：会话粒度元数据 + active/history 分离。
//
// 契约：docs/arch/session-board-metadata-lifecycle.md（§3/§3.1/§5/§7）。
//
// 为什么要有存档：goal 第五栈是**活栈投影**——finish/abort 弹栈即删除，初始
// 与终态都为空。于是"这台会话上一轮目标干到哪、哪些目标已经收口、为什么收口"
// 在栈里不留痕。看板存档（metadata/board_goal.json）把这两件事分开存：
//
//   - Active = 当前栈顶帧的**快照**（active/history 分离的那一半）；
//   - History = **只追加**的收口账本：存档已有条目 ∪ 由 goal 审计终态条目
//     （goal.finish/goal.abort）派生的条目，按 goal_id 去重，只追加不重写。
//
// 刷新时机有两个（不是"第二份事实"，而是同一份派生快照的两个必经点）：
//
//  1. Store.Save（栈变更的唯一必经点）：栈落盘成功后刷新；
//  2. 终态审计条目落账后（goal.finish/goal.abort）：goal 域的写序是
//     "先 persistLocked(Save) → 再 appendAudit"，弹栈那一刻账本里还没有终态
//     条目——只看 Save 会让收口条目漏账（重启后 history 少一条），close 的原因
//     也只能用 kind 兜底。所以终态审计落账后再刷一次（权威 Status/At/Reason）。
//
// 指纹节流：载荷内容（去掉 Seq/UpdatedAt 后）不变就不写盘——重复的刷新不会
// 让 Seq 前进，也不会让文件 mtime 变化。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// boardClosedReasonFallback 是"栈空但审计里还没有终态条目"时的关闭原因兜底。
//
// 为什么需要兜底：看板存档的 closed 状态**必须有** closed_reason（见
// sessionstore.ValidateGoalBoardMeta），而弹栈那一刻权威原因还没落账。这里先
// 落一个非空占位（语义 = "关闭了，原因待权威条目覆写"），紧随其后的终态审计
// 刷新会用 goal.finish/goal.abort 的 reason/kind 覆写它。
const boardClosedReasonFallback = "goal.closed"

// isTerminalAuditKind 报告审计条目是否 goal 终态（收口账本的派生来源）。
func isTerminalAuditKind(kind string) bool {
	return kind == string(EventFinish) || kind == string(EventAbort)
}

// auditLedger 返回本会话的 goal 审计账本（未装配会话存储 → nil）。
func (s *ContextStateStore) auditLedger() []sessionstore.GoalAuditEntry {
	if s == nil || s.session == nil {
		return nil
	}
	return s.session.GoalAuditSnapshot()
}

// refreshGoalBoard 组装并（按指纹节流后）写入 goal 看板存档。
//
// 失败语义（显式决定，§7）见 reportBoardWriteFailure；这里只负责"读前值 →
// 组载荷 → 原子替换"，前值读不出来时**不覆盖**（宁可留着旧快照，也不拿一份
// 丢了账本的快照去替换它）。
func (s *ContextStateStore) refreshGoalBoard(ctx context.Context, records []*GoalRecord) error {
	boards, ok := s.boardRepository()
	if !ok {
		// 未装配存档面：按"没有存档"降级，不报错、不阻断。
		return nil
	}
	active := activeFrame(records)
	state := sessionstore.BoardStateActive
	if active == nil {
		state = sessionstore.BoardStateClosed
	}
	prev, err := boards.ReadGoalBoard(ctx)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		// 前值读不出来（存档损坏/后端报错）：**不覆盖**——宁可留着旧快照，也不拿
		// 一份丢了账本的快照去替换它；close 方向按 §7 上报（见下）。
		return reportBoardWriteFailure(state, err)
	}
	if err != nil {
		// 还没有存档（首次写）：seq/opened_at 从头算。
		prev = sessionstore.GoalBoardMeta{}
	}
	meta, write := planGoalBoard(prev, active, s.auditLedger(), time.Now())
	if !write {
		return nil
	}
	if err := boards.WriteGoalBoard(ctx, meta); err != nil {
		return reportBoardWriteFailure(state, err)
	}
	return nil
}

// refreshBoardAfterAudit 在审计追加成功后刷新看板：只有终态条目
// （goal.finish/goal.abort）需要——它们既是收口账本的权威来源，也是 close
// 原因/时间的权威来源；begin/update/restore 的栈变化已由 Save 那次刷新覆盖。
func (s *ContextStateStore) refreshBoardAfterAudit(ctx context.Context, entry AuditEntry) error {
	if !isTerminalAuditKind(string(entry.Kind)) {
		return nil
	}
	if s == nil || s.session == nil {
		return nil
	}
	return s.refreshGoalBoard(ctx, recordsFromGoalFrames(s.session.GoalStackSnapshot()))
}

// reportBoardWriteFailure 决定一次看板存档失败是否上报（§7 的显式口径）。
//
// 必守的一条：**close 写不进去就不能当关闭**。栈已经空了，而存档仍旧写着
// active 时，重启后看板会把一个已经收口的 goal 复活成活目标——假的活目标比
// 没有看板更糟。因此 close 方向的失败必须上报，调用方（goal_finish/goal_abort）
// 因此看到 error：这次关闭没有被持久记账。
//
// active 方向相反：活栈通道本身就是权威（Reload 能从它重建看板），存档只是
// 派生快照；写不进去只意味着"重启后少一份快照"，不值得把一次成功的 goal 变更
// 报成失败——尽力而为，不阻断。
//
// 两个方向都**不改变栈的保存语义**：栈在这之前已经落盘，这里既不回滚也不跳过
// 栈写入（存档是扇出，不是前置条件）。
func reportBoardWriteFailure(state string, err error) error {
	if state == sessionstore.BoardStateClosed {
		return fmt.Errorf("goal: 关闭未记账（goal 看板存档写失败，不把这次关闭当已存档）: %w", err)
	}
	return nil
}

// planGoalBoard 组装看板载荷并判断是否需要写盘（指纹节流）。
//   - active 非 nil → state=active；
//   - active 为 nil → state=closed + closed_at/closed_reason（看板退场，不留空壳）。
func planGoalBoard(
	prev sessionstore.GoalBoardMeta,
	active *sessionstore.GoalBoardActive,
	audit []sessionstore.GoalAuditEntry,
	now time.Time,
) (sessionstore.GoalBoardMeta, bool) {
	meta := sessionstore.GoalBoardMeta{
		Active:  active,
		History: mergeBoardHistory(prev, active, audit),
	}
	// board_kind 由类型侧校验（ValidateGoalBoardMeta）：这里显式写死，避免存档
	// 在没有校验的路径上被别处"顺手"改掉。
	meta.Kind = sessionstore.BoardKindGoal
	if active != nil {
		meta.State = sessionstore.BoardStateActive
	} else {
		meta.State = sessionstore.BoardStateClosed
		meta.ClosedAt, meta.ClosedReason = closedInfo(prev, audit, now)
	}
	// seq/opened_at：首次 open 置 seq=1 与 opened_at；此后每次**实际写盘**
	// seq+1；opened_at 不动（关闭再开也不重置——它记的是看板这一次的一生）。
	meta.Seq = prev.Seq + 1
	meta.OpenedAt = prev.OpenedAt
	if meta.OpenedAt == 0 {
		meta.OpenedAt = now.Unix()
	}
	meta.UpdatedAt = now.Unix()
	meta.Fingerprint = boardFingerprint(meta)
	if prev.Seq >= 1 && prev.Fingerprint != "" && prev.Fingerprint == meta.Fingerprint {
		// 载荷内容（去掉 seq/updated_at 后）没变：不写盘，seq/updated_at 也不动。
		return sessionstore.GoalBoardMeta{}, false
	}
	return meta, true
}

// mergeBoardHistory 计算收口账本的**并集**：存档已有条目 ∪ 由审计终态条目
// （goal.finish/goal.abort）派生的条目。
//
//   - 按 goal_id 去重、**只追加不重写**：已有条目原样保留（先落账的说法为准），
//     派生条目按审计顺序（账本 Seq 顺序）追加；
//   - 派生条目取审计条目的 Status/At/Reason（Reason 空时按 kind 填 goal.finish /
//     goal.abort）；
//   - progress_count 审计条目自己不承载：只有"存档里那一份 active 帧正是这个
//     goal"（即上一次刷新时它还在栈顶）时才判定得出，否则为 0——不猜、不留第二
//     份事实。
func mergeBoardHistory(
	prev sessionstore.GoalBoardMeta,
	active *sessionstore.GoalBoardActive,
	audit []sessionstore.GoalAuditEntry,
) []sessionstore.GoalBoardHistory {
	merged := make([]sessionstore.GoalBoardHistory, 0, len(prev.History)+len(audit))
	seen := make(map[string]bool, len(prev.History)+len(audit))
	for _, entry := range prev.History {
		if entry.GoalID == "" || seen[entry.GoalID] {
			continue
		}
		seen[entry.GoalID] = true
		merged = append(merged, entry)
	}
	for _, entry := range audit {
		if !isTerminalAuditKind(entry.Kind) || entry.GoalID == "" || seen[entry.GoalID] {
			continue
		}
		seen[entry.GoalID] = true
		derived := sessionstore.GoalBoardHistory{
			GoalID: entry.GoalID, Title: entry.Title, Status: entry.Status,
			ClosedAt: entry.At, ClosedReason: entry.Reason,
		}
		if derived.ClosedReason == "" {
			// 审计条目没给原因时按 kind 兜底：goal.finish / goal.abort 本身就是
			// 收口原因的分类（比空串可读，也不引入第二种词表）。
			derived.ClosedReason = entry.Kind
		}
		if prev.Active != nil && prev.Active.GoalID == entry.GoalID {
			derived.ProgressCount = len(prev.Active.Progress)
		}
		merged = append(merged, derived)
	}
	// goal_id 复用（会话重启后 seq 归零 → 新 goal 又拿到 g-1）时**活体帧优先**：
	// 账本里同 id 的旧条目让位。否则同一 goal_id 会同时在 active 与 history，
	// 存档不满足不变式 → 整份快照都写不进去 → 重启后看板更糟（§7）。
	if active != nil {
		filtered := merged[:0]
		for _, entry := range merged {
			if entry.GoalID == active.GoalID {
				continue
			}
			filtered = append(filtered, entry)
		}
		merged = filtered
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

// closedInfo 返回关闭时间（unix 秒）与原因：权威来源是**刚离开活动栈那个 goal**
// 的终态审计条目（Status/At/Reason 里的 At/Reason）；取不到（弹栈那一刻账本里还
// 没有终态条目，或本次关闭没有任何终态条目）就沿用前值，保持指纹稳定，等紧随其后
// 的终态审计刷新用权威值覆写。
func closedInfo(prev sessionstore.GoalBoardMeta, audit []sessionstore.GoalAuditEntry, now time.Time) (int64, string) {
	departed := ""
	if prev.Active != nil {
		departed = prev.Active.GoalID
	}
	if entry, ok := terminalAuditFor(audit, departed); ok {
		at := now.Unix()
		if entry.At > 0 {
			at = entry.At
		}
		reason := entry.Reason
		if reason == "" {
			reason = entry.Kind
		}
		return at, reason
	}
	at := now.Unix()
	if prev.ClosedAt > 0 {
		at = prev.ClosedAt
	}
	reason := prev.ClosedReason
	if reason == "" {
		reason = boardClosedReasonFallback
	}
	return at, reason
}

// terminalAuditFor 在审计账本里找终态条目：优先 goalID 匹配的那条，否则退回
// 最后一条终态条目（账本按 Seq 追加顺序，取最后一条即最近一次收口）。
func terminalAuditFor(audit []sessionstore.GoalAuditEntry, goalID string) (sessionstore.GoalAuditEntry, bool) {
	last, found := sessionstore.GoalAuditEntry{}, false
	for _, entry := range audit {
		if !isTerminalAuditKind(entry.Kind) {
			continue
		}
		if goalID != "" && entry.GoalID == goalID {
			return entry, true
		}
		last, found = entry, true
	}
	if goalID != "" {
		// 指定的 goal 没有终态条目：不用别的 goal 的条目冒充原因。
		return sessionstore.GoalAuditEntry{}, false
	}
	return last, found
}

// activeFrame 把栈顶（records 末元素）投影成看板当前帧快照；空栈 → nil。
func activeFrame(records []*GoalRecord) *sessionstore.GoalBoardActive {
	for index := len(records) - 1; index >= 0; index-- {
		record := records[index]
		if record == nil {
			continue
		}
		frame := &sessionstore.GoalBoardActive{
			GoalID:     record.ID,
			Title:      record.Title,
			Statement:  record.Statement,
			Acceptance: append([]string(nil), record.Acceptance...),
			OutOfScope: append([]string(nil), record.OutOfScope...),
			Status:     string(record.Status),
			CreatedAt:  record.CreatedAt,
			UpdatedAt:  record.UpdatedAt,
		}
		for _, item := range record.Progress {
			frame.Progress = append(frame.Progress, sessionstore.GoalProgress{
				At: item.At, Kind: string(item.Kind), Content: item.Content,
			})
		}
		return frame
	}
	return nil
}

// boardFingerprint 计算看板载荷的内容指纹：对"去掉 Seq/UpdatedAt/Fingerprint"
// 的载荷取 sha256（字段顺序由结构体定义固定，无 map，序列化稳定）。
func boardFingerprint(meta sessionstore.GoalBoardMeta) string {
	meta.Seq = 0
	meta.UpdatedAt = 0
	meta.Fingerprint = ""
	data, err := json.Marshal(meta)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
