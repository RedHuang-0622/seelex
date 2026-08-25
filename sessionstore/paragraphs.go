package sessionstore

// EventParagraph 是事件流中的一个完整段落（轮次单元）的坐标摘要：
// fork 切断点定位、段落索引与整帧重写的公共结构。EventFrom/EventTo 是
// 段落的事件流坐标（含端点）；RequestID 只是关联字段，不承担截断语义。
type EventParagraph struct {
	EventFrom uint64 `json:"event_from"`
	EventTo   uint64 `json:"event_to"`
	RequestID string `json:"request_id,omitempty"`
	// MessageFrom/MessageTo 是段落覆盖的 UI 消息定位键范围。
	MessageFrom string `json:"message_from,omitempty"`
	MessageTo   string `json:"message_to,omitempty"`
}

// EventParagraphs 由 CompleteEventUnits 推导段落表：事件流 → 段落边界
// （fork 切断点、段落索引的派生事实源；RoundNo 体系启用前轮次 = 完整
// 协议单元）。孤儿 tool 事件与未知角色不构成段落。
func EventParagraphs(events []Event) []EventParagraph {
	units := CompleteEventUnits(events)
	paragraphs := make([]EventParagraph, 0, len(units))
	for _, unit := range units {
		if len(unit) == 0 {
			continue
		}
		paragraph := EventParagraph{
			EventFrom: unit[0].Seq,
			EventTo:   unit[len(unit)-1].Seq,
		}
		for _, event := range unit {
			if paragraph.RequestID == "" {
				paragraph.RequestID = event.TaskID
			}
			if paragraph.MessageFrom == "" {
				paragraph.MessageFrom = event.MessageID
			}
			if event.MessageID != "" {
				paragraph.MessageTo = event.MessageID
			}
		}
		paragraphs = append(paragraphs, paragraph)
	}
	return paragraphs
}

// ParagraphEnd 返回包含指定 EventSeq 的段落末端（含端点）；seq 不在任何
// 完整段落内时返回 (0, false)。seq == 0 表示 fork 起点（空继承），恒合法。
func ParagraphEnd(events []Event, seq uint64) (uint64, bool) {
	if seq == 0 {
		return 0, true
	}
	for _, paragraph := range EventParagraphs(events) {
		if seq >= paragraph.EventFrom && seq <= paragraph.EventTo {
			return paragraph.EventTo, true
		}
	}
	return 0, false
}

// IsParagraphBoundary 判断 EventSeq 是否落在段落边界（含端点末端）。
// fork 切断点必须落在段落边界，避免截出半截轮次。
func IsParagraphBoundary(events []Event, seq uint64) bool {
	if seq == 0 {
		return true
	}
	for _, paragraph := range EventParagraphs(events) {
		if seq == paragraph.EventTo {
			return true
		}
	}
	return false
}
