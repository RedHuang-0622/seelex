package store

import protocol "github.com/RedHuang-0622/seelex/contract/protocol"

// EventParagraph is the compatibility name for the protocol paragraph.
type EventParagraph = protocol.EventParagraph

// EventParagraphs 由 CompleteEventUnits 推导段落表：事件流 → 段落边界
// （fork 切断点、段落索引的派生事实源；RoundNo 体系启用前轮次 = 完整
// 协议单元）。孤儿 tool 事件与未知角色不构成段落。
func EventParagraphs(events []Event) []EventParagraph {
	return protocol.EventParagraphs(events)
}

// ParagraphEnd 返回包含指定 EventSeq 的段落末端（含端点）；seq 不在任何
// 完整段落内时返回 (0, false)。seq == 0 表示 fork 起点（空继承），恒合法。
func ParagraphEnd(events []Event, seq uint64) (uint64, bool) {
	return protocol.ParagraphEnd(events, seq)
}

// IsParagraphBoundary 判断 EventSeq 是否落在段落边界（含端点末端）。
// fork 切断点必须落在段落边界，避免截出半截轮次。
func IsParagraphBoundary(events []Event, seq uint64) bool {
	return protocol.IsParagraphBoundary(events, seq)
}
