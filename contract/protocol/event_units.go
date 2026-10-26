package model

// EventParagraph is a complete transcript unit used by fork cuts, paragraph
// indexes, and bounded transcript assembly. EventFrom/EventTo are inclusive
// event coordinates; RequestID is descriptive and does not define truncation.
type EventParagraph struct {
	EventFrom   uint64 `json:"event_from"`
	EventTo     uint64 `json:"event_to"`
	RequestID   string `json:"request_id,omitempty"`
	MessageFrom string `json:"message_from,omitempty"`
	MessageTo   string `json:"message_to,omitempty"`
}

// CompleteEventUnits splits a transcript into visible protocol units: user
// turns, assistant text turns, and assistant tool chains paired by call ID.
// Orphan tool rows and unknown roles remain archive evidence but do not become
// units. Interrupted tool chains and unanswered user rows remain open units so
// cold-loaded context does not lose UI-visible turns.
func CompleteEventUnits(events []TranscriptEvent) [][]TranscriptEvent {
	units := make([][]TranscriptEvent, 0, len(events))
	for index := 0; index < len(events); {
		event := events[index]
		switch {
		case event.Role == "user":
			unit, next := userEventUnit(events, index)
			if len(unit) > 0 {
				units = append(units, unit)
			}
			index = next
		case event.Role == "assistant" && len(event.ToolCalls) == 0:
			units = append(units, append([]TranscriptEvent(nil), event))
			index++
		case event.Role == "assistant" && len(event.ToolCalls) > 0:
			unit, next, _ := toolEventUnit(events, index)
			if len(unit) > 0 {
				units = append(units, unit)
			}
			index = next
		default:
			index++
		}
	}
	return units
}

// EventParagraphs derives paragraph boundaries from CompleteEventUnits.
func EventParagraphs(events []TranscriptEvent) []EventParagraph {
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

// ParagraphEnd returns the inclusive end of the paragraph containing seq.
// seq == 0 is the empty fork origin and is always valid.
func ParagraphEnd(events []TranscriptEvent, seq uint64) (uint64, bool) {
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

// IsParagraphBoundary reports whether seq ends a complete paragraph.
func IsParagraphBoundary(events []TranscriptEvent, seq uint64) bool {
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

func userEventUnit(events []TranscriptEvent, start int) ([]TranscriptEvent, int) {
	unit := []TranscriptEvent{events[start]}
	index := start + 1
	for index < len(events) && events[index].Role != "user" {
		event := events[index]
		if event.Role != "assistant" {
			return unit, index
		}
		if len(event.ToolCalls) == 0 {
			unit = append(unit, event)
			return unit, index + 1
		}
		toolUnit, next, _ := toolEventUnit(events, index)
		unit = append(unit, toolUnit...)
		index = next
	}
	return unit, index
}

func toolEventUnit(events []TranscriptEvent, start int) ([]TranscriptEvent, int, bool) {
	assistant := events[start]
	wanted := make(map[string]struct{}, len(assistant.ToolCalls))
	for _, call := range assistant.ToolCalls {
		if call.ID == "" {
			return nil, start + 1, false
		}
		if _, duplicate := wanted[call.ID]; duplicate {
			return nil, start + 1, false
		}
		wanted[call.ID] = struct{}{}
	}
	unit := []TranscriptEvent{assistant}
	seen := make(map[string]struct{}, len(wanted))
	index := start + 1
	for index < len(events) && len(seen) < len(wanted) {
		event := events[index]
		if event.Role != "tool" {
			break
		}
		if _, ok := wanted[event.ToolCallID]; !ok {
			break
		}
		if _, duplicate := seen[event.ToolCallID]; duplicate {
			break
		}
		seen[event.ToolCallID] = struct{}{}
		unit = append(unit, event)
		index++
	}
	return unit, index, len(seen) == len(wanted)
}
