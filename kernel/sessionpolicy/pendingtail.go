package switching

// pendingtail.go — 草稿尾部（seq_draft）恢复的**应用侧决策表**（A4）。
//
// 落点沿革：2026-10-16 随 M5-R6-pending-tail **接收者转移**从
// `application/assembly/session_pending_tail.go` 归位（plan §2.4 的 `session_pending_tail.go` → `middle/switch/`）。
//
// 存储层早就提供「探测 → 恢复 / 丢弃」的显式入口（`store/pending_tail.go` → Router →
// `wiring/adapters.SessionPort`），但**应用侧此前没有任何调用方**：草稿尾恢复只是"端口能力"，
// 用户在会话加载时看不到任何行为差异。本落点把它接到会话冷加载路径上，并把结果变成用户可感知的
// 一条 system 行。
//
// 决策表（与 `recovery-order.md` §4 的 L2 一致）：
//
//	clean       → 无草稿，静默（不做任何写）
//	recoverable → 基座一致 ⇒ 显式恢复（推进发布点）并提示"已恢复 N 行"
//	gap         → 基座断裂 ⇒ **只报告**（红线 3：不发布、不清理、不猜测），
//	              提示内容仍在磁盘、需人工确认
//	 其它（recovered/discarded 等竞态窗口）→ 静默，避免误报
//
// 运行中不抢发布点：本回合正在跑的会话（`ChatState.Running`）的草稿尾属于在飞写者，加载路径不得
// 发布/丢弃它——否则就回到"两个写者互相 reap 草稿"的老问题。

import (
	"fmt"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// PendingTailDeps 是草稿尾决策需要的宿主面（全部窄口；定位与端口解析留在宿主）。
type PendingTailDeps struct {
	// Running 报告目标会话是否正在跑（在飞写者；加载路径不得抢它的发布点）。
	Running func(sessionID string) bool
	// Probe 探测草稿尾（`ok=false` = 存储未装配 / 非 v8 布局：能力未启用，与没有草稿尾语义一致）。
	Probe func(sessionID string) (report dto.PendingMessageTailReport, ok bool, err error)
	// Recover 显式恢复草稿尾（推进发布点）。
	Recover func(sessionID string) (report dto.PendingMessageTailReport, ok bool, err error)
}

// RecoverPendingTail 在会话冷加载时探测/恢复草稿尾，返回**要补进可见会话的 system 提示**
// （空串 = 静默，不写任何东西）。
//
// 时序要求（为什么必须在读可见正文之前调用）：读者的可见性闸门就是**发布点**
// （message head.LastSeq）。先探测/恢复、再读 record/transcript，恢复出来的行才能在本次加载里
// 就进可见会话；反过来先读再恢复，用户要多切一次会话才看得到。
func RecoverPendingTail(deps PendingTailDeps, sessionID string) string {
	if deps.Running != nil && deps.Running(sessionID) {
		return ""
	}
	if deps.Probe == nil {
		return ""
	}
	probe, ok, err := deps.Probe(sessionID)
	if err != nil {
		return fmt.Sprintf("Failed to probe this session's uncommitted tail: %v", err)
	}
	if !ok {
		// 存储未装配 / 非 v8 布局：能力未启用，与没有草稿尾语义时行为一致（静默）。
		return ""
	}
	switch probe.Status {
	case dto.PendingTailClean, "":
		return ""
	case dto.PendingTailRecoverable:
		if deps.Recover == nil {
			return ""
		}
		recovered, ok, err := deps.Recover(sessionID)
		if err != nil {
			return fmt.Sprintf("This session has %d uncommitted row(s) that could not be recovered (they are still on disk, not discarded): %v",
				probe.RowCount, err)
		}
		if !ok || recovered.Status != dto.PendingTailRecovered {
			// 探测与恢复之间状态已变（同进程另一路径先发布/丢弃）：不宣称恢复。
			return ""
		}
		return fmt.Sprintf("Recovered %d uncommitted row(s) from before the interruption (publish point %d → %d).",
			recovered.RowCount, probe.HeadSeq, recovered.HeadSeq)
	case dto.PendingTailGap:
		return fmt.Sprintf("This session has %d uncommitted row(s) that cannot be recovered safely: they are not contiguous with the last commit point (tail starts at %d, commit point %d). The rows are kept on disk — review before discarding or rebuilding.",
			probe.RowCount, probe.TailFrom, probe.HeadSeq)
	default:
		return ""
	}
}
