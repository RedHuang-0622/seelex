package context

import "fmt"

// AppendCompact validates and appends one compact frame without performing IO.
func AppendCompact(stack []CompactFrame, frame CompactFrame) ([]CompactFrame, error) {
	if frame.SegmentID == "" {
		return nil, fmt.Errorf("session context: compact frame requires segment_id")
	}
	if frame.From < 0 || frame.To < frame.From {
		return nil, fmt.Errorf("session context: compact frame invalid range [%d,%d]", frame.From, frame.To)
	}
	if (frame.RoundFrom == 0) != (frame.RoundTo == 0) || frame.RoundFrom > frame.RoundTo {
		return nil, fmt.Errorf("session context: compact frame invalid round range [%d,%d]", frame.RoundFrom, frame.RoundTo)
	}
	if (frame.EventFrom == 0) != (frame.EventTo == 0) || frame.EventFrom > frame.EventTo {
		return nil, fmt.Errorf("session context: compact frame invalid event range [%d,%d]", frame.EventFrom, frame.EventTo)
	}
	if (frame.RequestFrom == "") != (frame.RequestTo == "") {
		return nil, fmt.Errorf("session context: compact frame request range must be both empty or both set")
	}
	if frame.RequestFrom != "" && frame.RequestFrom > frame.RequestTo {
		return nil, fmt.Errorf("session context: compact frame invalid request range [%q,%q]", frame.RequestFrom, frame.RequestTo)
	}
	if len(stack) == 0 {
		if frame.PrevSegmentID != "" {
			return nil, fmt.Errorf("session context: first compact frame must not carry prev_segment_id")
		}
		return append(stack, frame), nil
	}
	top := stack[len(stack)-1]
	if frame.PrevSegmentID == "" {
		return nil, fmt.Errorf("session context: non-first compact frame requires prev_segment_id")
	}
	if frame.PrevSegmentID != top.SegmentID {
		return nil, fmt.Errorf("session context: prev_segment_id %q must match stack top %q", frame.PrevSegmentID, top.SegmentID)
	}
	if frame.PrevRequestFrom != top.RequestFrom || frame.PrevRequestTo != top.RequestTo {
		return nil, fmt.Errorf("session context: prev request range [%q,%q] must match stack top [%q,%q]",
			frame.PrevRequestFrom, frame.PrevRequestTo, top.RequestFrom, top.RequestTo)
	}
	return append(stack, frame), nil
}
