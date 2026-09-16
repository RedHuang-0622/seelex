package goal

// escape.go — 环逃生（round_limit / no_progress / empty_ring / no_executor）的
// **唯一收口口**。
//
// 背景（2026-09-16 复核）：逃生此前只在 application 层调 `governor.Break(reason)`，
// goal 仍停在 `active`——治理循环已经停了，goal 却还在等一个永远不会来的
// ADVISOR 回合：面板恒 0 轮、用户看不到收口、同一会话的下一次 goal 还可能带着
// 上一轮 b 的锚点/帧继续跑（`headless.go` 的 goal_finish/goal_abort 与任何直接
// `Controller.Finish/Abort` 的路径都不 reap peer，见 techleader.go 的
// `unbindIfTerminal` 调用点）。结果就是"loop 不动了，但没人知道它停了"。
//
// 本文件的语义：**逃生 = 这一轮 goal 结束**。
//  1. 先把 b（ADVISOR）累积的会话历史归档留痕——它只存在于进程内
//     `AdvisorSession`（anchor + frames + rounds），不归档就随 goal 一起消失；
//  2. 再把 goal 落成终态 `aborted`（**不过 gate**：gate 要 b 裁决，而 b 正是被
//     逃生判定"不值得再问"的那一侧，再去问它等于自锁）；
//  3. 最后 reap 掉 b 的 peer，让**下一个 goal 重建 ADVISOR**（清空锚点/帧/回合）。
//
// 与 gate 路径（`CloseTopGoalOnTerminal`）的关系：那是 b 主动给终态裁决的收口，
// 本文件是 b 那一侧已经不可用/不值得再用时的收口。两条路都必须 reap peer，否则
// 下一个 goal 会继承上一轮 goal 的审查上下文。

import (
	"context"
	"fmt"
	"strings"
)

// MaxArchiveRunes 是单条 b 历史归档正文的上限（runes）。归档是"留痕"不是
// "全文备份"：超限截断，并显式标注截断，避免把一次逃生变成一次大写入。
const MaxArchiveRunes = 6000

// ArchiveKindEscape 是逃生归档行的 kind（写进 tl 角色历史，供前端/审计按
// 行区分"这是收口归档"而不是一次普通 ADVISOR 回合）。
const ArchiveKindEscape = "goal_archive"

// TLArchiveRecord 是一次 b 会话历史归档的素材：全部来自 `AdvisorSession` 的进程内
// 状态，归档之后这些状态不再被后续 goal 复用。
type TLArchiveRecord struct {
	Kind      string // 归档来源（逃生原因，如 "round_limit"）
	GoalID    string
	GoalTitle string
	Reason    string // 写入 goal 终态的逃生原因
	Rounds    int
	Frames    int
	Trigger   string
	Content   string // 渲染后的可读原文（[advisor]/[goal-anchor]/[frames]/[your-rounds]）
	Truncated bool
}

// TLHistoryArchiver 是"归档 b 会话历史"的写面。
//
// 用类型断言（而非新增 dep）接进 Supervisor：`TLRoundRecorder` 的实现若同时实现
// 本接口，逃生收口时就会多落一条归档行——归档是记录器的**可选增强**，不逼所有
// 实现跟上（测试里的最小记录器可以继续只实现 TLRoundRecorder）。
type TLHistoryArchiver interface {
	ArchiveTLHistory(ctx context.Context, record TLArchiveRecord) error
}

// EscapeResult 描述一次逃生收口的结果（供装配层写日志/断言）。
type EscapeResult struct {
	Closed   bool        // 是否真的收口了一个 active goal
	Goal     *GoalRecord // 收口后的终态记录
	Archived bool        // 归档是否真的写下去了
	Archive  TLArchiveRecord
}

// AbortOnEscape 收口当前 active goal，并把 b 侧会话历史归档。
//
// 幂等：没有 active goal 时原样返回（不报错）——逃生可能被多处触发（环的记账 +
// 上层观察），重复调用不该把已收口的 goal 再动一次。
// reason 用环的逃生原因，会同时落进 goal 终态 reason（`escape:<reason>`）与归档行，
// 便于事后按 StopReason 归因（"到底是被轮次上限还是无进展判掉的"）。
func (s *Supervisor) AbortOnEscape(ctx context.Context, reason string) (EscapeResult, error) {
	result := EscapeResult{}
	if s == nil || s.ctl == nil {
		return result, nil
	}
	active, ok := s.ctl.ActiveGoal()
	if !ok || active == nil {
		return result, nil
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "escape"
	}

	// 1. 读快照 + reap（一次加锁完成：读到的状态与 reap 之间不能被另一个回合改写）。
	archive := s.reapForArchive(reason)
	result.Archive = archive

	// 2. 归档（可选增强；失败不阻断收口——逃生是安全路径，留痕是加分项）。
	archiveNote := fmt.Sprintf("已归档 b 历史 %d 轮 / %d 帧", archive.Rounds, archive.Frames)
	if archiver, ok := s.recorder.(TLHistoryArchiver); ok && archiver != nil {
		if err := archiver.ArchiveTLHistory(ctx, archive); err != nil {
			archiveNote = "b 历史归档失败: " + err.Error()
		} else {
			result.Archived = true
		}
	}

	// 3. 落终态（不过 gate：b 正是被判"不值得再问"的一侧）。
	record, err := s.ctl.Abort(ctx, FinishRequest{
		Reason: fmt.Sprintf("escape:%s", reason),
		Result: boundedFinishResult(fmt.Sprintf("环逃生收口（%s）：%s", reason, archiveNote)),
	})
	if err != nil {
		return result, err
	}
	result.Closed = true
	result.Goal = record
	return result, nil
}

// reapForArchive 读取 b 的会话历史快照并把 peer 标成 reaped。返回的归档素材在
// peer 不存在时也有效（只有 reason），保证调用方无需分支。
func (s *Supervisor) reapForArchive(reason string) TLArchiveRecord {
	archive := TLArchiveRecord{
		Kind:    reason,
		Reason:  reason,
		Trigger: "archive:" + reason,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	peer := s.advisor
	if peer == nil {
		return archive
	}
	archive.GoalID = peer.GoalID
	archive.GoalTitle = peer.Anchor.Title
	archive.Rounds = len(peer.Rounds)
	archive.Frames = len(peer.Frames)
	embed := TLSessionEmbed{
		PeerID:   peer.PeerID,
		Goal:     peer.Anchor,
		Frames:   append([]Frame(nil), peer.Frames...),
		TLMemory: peer.RoundMemories(MaxEmbedRounds),
		Trigger:  archive.Trigger,
	}
	archive.Content, archive.Truncated = boundedArchiveText(embed.RenderText())
	if peer.State != PeerReaped {
		peer.State = PeerReaped
		peer.Reason = reason
	}
	return archive
}

// boundedArchiveText 按 MaxArchiveRunes 截断归档正文，并报告是否截断。
func boundedArchiveText(text string) (string, bool) {
	runes := []rune(text)
	if len(runes) <= MaxArchiveRunes {
		return text, false
	}
	return string(runes[:MaxArchiveRunes]) + "\n…（归档正文已截断）", true
}
