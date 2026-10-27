package context

import (
	"encoding/json"
	"fmt"
	"time"
)

// ForkOptions 描述子会话从父 context 分叉时的裁剪输入。
type ForkOptions struct {
	ForkedFrom   json.RawMessage
	CutEventSeq  uint64
	CutTime      time.Time
	CutMessageID string
}

// ForkState 把父 context payload 重写为独立子会话 payload，并返回重写后的
// compact frames。函数无 I/O：父 payload 读取和子 payload 写入由调用方负责。
func ForkState(payload []byte, opts ForkOptions) ([]byte, []CompactFrame, error) {
	record := SessionContextRecord{SchemaVersion: SessionContextSchemaVersion}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &record); err != nil {
			return nil, nil, fmt.Errorf("decode parent context: %w", err)
		}
		if record.SchemaVersion != SessionContextSchemaVersion {
			return nil, nil, fmt.Errorf(
				"parent context has unsupported schema version %d (want %d)",
				record.SchemaVersion, SessionContextSchemaVersion,
			)
		}
	}
	record.PlanStack = nil
	record.TaskStack = nil
	record.GoalStack = nil
	record.GoalAudit = nil
	record.ForkedFrom = append(json.RawMessage(nil), opts.ForkedFrom...)
	record.SkillStack = forkSkillFrames(record.SkillStack, opts.CutTime)
	frames := RewriteForkCompactStack(record.CompactStack, opts.CutEventSeq, opts.CutMessageID)
	record.CompactStack = frames
	data, err := json.Marshal(record)
	if err != nil {
		return nil, nil, fmt.Errorf("encode child context: %w", err)
	}
	return data, frames, nil
}

func forkSkillFrames(frames []SkillFrame, cutTime time.Time) []SkillFrame {
	result := make([]SkillFrame, 0, len(frames))
	for _, frame := range frames {
		if frame.ActivatedAt.IsZero() || !frame.ActivatedAt.After(cutTime) {
			result = append(result, frame)
		}
	}
	return result
}

// RewriteForkCompactStack 在分叉点裁剪继承的 compact frames：无事件坐标的旧帧
// 保守保留；完全在切点前的帧原样保留；完全在切点后的帧丢弃；跨切点帧把事件
// 与消息终点截到切点身份。
func RewriteForkCompactStack(frames []CompactFrame, cut uint64, cutMessageID string) []CompactFrame {
	result := make([]CompactFrame, 0, len(frames))
	for _, frame := range frames {
		if frame.EventFrom == 0 && frame.EventTo == 0 {
			result = append(result, frame)
			continue
		}
		if frame.EventTo <= cut {
			result = append(result, frame)
			continue
		}
		if frame.EventFrom > cut {
			continue
		}
		frame.EventTo = cut
		if cutMessageID != "" {
			frame.MessageTo = cutMessageID
		}
		result = append(result, frame)
	}
	return result
}
