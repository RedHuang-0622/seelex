package core

import (
	"fmt"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// 草稿尾部（seq_draft）恢复的应用侧装配（A4）。
//
// 存储层早就提供了「探测 → 恢复 / 丢弃」的显式入口（sessionstore/pending_tail.go
// → Router → internal/adapters.SessionPort），但**应用侧此前没有任何调用方**：
// 草稿尾恢复只是"端口能力"，用户在会话加载时看不到任何行为差异。本文件把它接到
// 会话冷加载路径上，并把结果变成用户可感知的一条 system 行。
//
// 时序要求（为什么必须在读可见正文之前）：读者的可见性闸门就是**发布点**
// （message head.LastSeq）。先探测/恢复、再读 record/transcript，恢复出来的行才能
// 在本次加载里就进可见会话；反过来先读再恢复，用户要多切一次会话才看得到。
//
// 决策表（与 recovery-order.md §4 的 L2 一致）：
//
//	clean       → 无草稿，静默（不做任何写）
//	recoverable → 基座一致 ⇒ 显式恢复（推进发布点）并提示"已恢复 N 行"
//	gap         → 基座断裂 ⇒ **只报告**（红线 3：不发布、不清理、不猜测），
//	              提示内容仍在磁盘、需人工确认
//	 其它（recovered/discarded 等竞态窗口）→ 静默，避免误报
//
// 运行中不抢发布点：本回合正在跑的会话（ChatState.Running）的草稿尾属于在飞
// 写者，加载路径不得发布/丢弃它——否则就回到"两个写者互相 reap 草稿"的老问题。
func (service *Service) recoverPendingMessageTailAtLoad(sessionID string) string {
	if service == nil || service.components.sessions == nil {
		return ""
	}
	service.ViewMu.Lock()
	running := service.sessionUnitLocked(sessionID).ChatState().Running
	service.ViewMu.Unlock()
	if running {
		return ""
	}
	location := service.components.sessions.LocateSession(sessionID)
	probe, ok, err := service.components.sessions.PendingMessageTail(location, sessionID)
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
		recovered, ok, err := service.components.sessions.RecoverPendingMessageTail(location, sessionID)
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

// appendPendingTailNoticeLocked 把草稿尾恢复的结论作为一条 system 行补进目标会话
// 的可见投影（调用方持 Core.ViewMu，且在可见投影安装之后调用——过早追加会被
// SetSessionViewLocked 的整体替换抹掉）。用户由此在会话里直接看到"恢复了多少行"
// 或"有内容但没能安全恢复"，而不是只在日志里。
func (service *Service) appendPendingTailNoticeLocked(sessionID, notice string) {
	if notice == "" {
		return
	}
	service.appendSessionMessageLocked(sessionID, "system", notice, nil)
}
