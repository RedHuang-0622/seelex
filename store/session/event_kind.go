package session

// event_kind.go — transcript 行的显式类别判定（M9 从根 `store/sessionstore.go` 迁入）。
//
// 生态位：类别词表的**唯一**事实源是 `contract/protocol`（`TranscriptEventKind*`），
// 本文件只持有"如何判"的那份判据。根包保留同名常量别名与一层转发，供旧调用方使用。
//
// 为什么放在本包：Kind 是 transcript 行的语义（旧数据 Kind 为空时要按 Role/ToolCalls
// 回退），不是存储机制。它跟 `MessageRowsCommitID`、`ClassifyPendingTail` 一样属于
// 行语义，放在 store/root（原子写/哈希）或根 adapter 都会让判据漂出领域。

import protocol "github.com/RedHuang-0622/seelex/contract/protocol"

// EventKindOf 返回事件的显式类别；旧数据 Kind 为空时按 Role/ToolCalls 回退。
func EventKindOf(event protocol.TranscriptEvent) string {
	if event.Kind != "" {
		return event.Kind
	}
	switch event.Role {
	case "user":
		return protocol.TranscriptEventKindUserInput
	case "assistant":
		if len(event.ToolCalls) > 0 {
			return protocol.TranscriptEventKindToolCall
		}
		return protocol.TranscriptEventKindLLM
	case "tool":
		return protocol.TranscriptEventKindToolOutput
	case "system":
		return protocol.TranscriptEventKindSystem
	case "error":
		return protocol.TranscriptEventKindError
	default:
		return protocol.TranscriptEventKindNotice
	}
}
