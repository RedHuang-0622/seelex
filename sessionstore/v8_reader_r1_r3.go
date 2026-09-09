// R1（前端/历史）与 R3（断点续跑）读取器。
//
// R1：读 message 分片分页（压缩不删正文；LRU 已淘汰区返回摘要占位；
// internal/context 行按展示规则标记，不影响读序）。
// R3：interrupted 锚点后的尾段；EVENT 只用于定位，message 完整但事件缺失
// 时不合成虚假 interrupted；消息残缺时合成 interrupted + 修复占位。
package sessionstore

import (
	"errors"
	"fmt"
)

// v8R1Row 是 R1 输出的一行：事件行 + 展示标记。
type v8R1Row struct {
	Event
	// Internal 标记 internal_user/context 行（前端可过滤或折叠展示）。
	Internal bool `json:"internal,omitempty"`
	// Placeholder 标记 LRU 已淘汰区摘要占位（无正文原文）。
	Placeholder bool `json:"placeholder,omitempty"`
}

// v8R1Page 返回 message 分页（offset/limit 基于行坐标 1..last_seq；
// 已淘汰区由占位行承接，返回空洞不错位）。
func (store *v8Store) v8R1Page(key Key, offset, limit int) ([]v8R1Row, int, error) {
	if offset < 0 {
		return nil, 0, errors.New("v8: R1 offset must be >= 0")
	}
	if limit <= 0 {
		limit = 20
	}
	store.messageMu.Lock()
	head, err := store.v8ReadMessageHeadLocked(key)
	if err != nil {
		store.messageMu.Unlock()
		return nil, 0, err
	}
	store.messageMu.Unlock()
	total := int(head.LastSeq)
	if offset >= total {
		return []v8R1Row{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	rows, err := store.v8ReadRows(key, uint64(offset+1), uint64(end))
	if err != nil {
		return nil, 0, err
	}
	bySeq := make(map[uint64]Event, len(rows))
	for _, row := range rows {
		bySeq[row.Seq] = row
	}
	out := make([]v8R1Row, 0, end-offset)
	for seq := offset + 1; seq <= end; seq++ {
		row, ok := bySeq[uint64(seq)]
		if !ok {
			if uint64(seq) <= head.WatermarkSeq {
				out = append(out, v8R1Row{
					Event:       Event{Seq: uint64(seq), Role: "context", Kind: EventKindNotice, Content: "（LRU 已淘汰区：原始正文已按用户确认删除，详见 compact 摘要）"},
					Placeholder: true,
				})
				continue
			}
			return nil, 0, fmt.Errorf("v8: R1 page hole at seq %d beyond watermark %d", seq, head.WatermarkSeq)
		}
		out = append(out, v8R1Row{
			Event:    row,
			Internal: EventKindOf(row) == EventKindInternal || row.Role == "context" || row.Role == "internal_user",
		})
	}
	return out, total, nil
}

// v8ResumePoint 是 R3 断点续跑结果。
type v8ResumePoint struct {
	// AnchorSeq / AnchorMessageID 是 resume 起点（该点之后的事件行）。
	AnchorSeq       uint64 `json:"anchor_seq"`
	AnchorMessageID string `json:"anchor_message_id,omitempty"`
	// Rows 是锚点后的尾段（含端点语义：anchor 行之后）。
	Rows []Event `json:"rows"`
	// Synthetic 表示 EVENT interrupted 缺失但消息残缺，恢复侧合成的锚点。
	Synthetic bool `json:"synthetic,omitempty"`
	// Repair 是残缺工具轮的修复占位。
	Repair []v8WireMessage `json:"repair,omitempty"`
	// Incomplete 表示消息自身残缺（缺 tool 结果）。
	Incomplete bool `json:"incomplete,omitempty"`
}

// v8ResumeTail 定位断点并返回尾段。不读 EVENT 也能正确恢复（EVENT 只加速
// 定位）；有 interrupted 事件以事件锚点为准。
func (store *v8Store) v8ResumeTail(key Key) (v8ResumePoint, error) {
	messageHead, err := store.v8ReadMessageHead(key)
	if err != nil {
		return v8ResumePoint{}, err
	}
	if messageHead.LastSeq == 0 {
		return v8ResumePoint{}, nil
	}
	// EVENT 定位（可缺失；缺失不改变正确性）。
	events, err := store.v8ReadEvents(key, 0, 0)
	if err != nil {
		return v8ResumePoint{}, err
	}
	anchorSeq := uint64(0)
	anchorMessageID := ""
	for index := len(events) - 1; index >= 0; index-- {
		if events[index].Kind == v8EventInterrupted {
			anchorSeq = events[index].AnchorSeq
			anchorMessageID = events[index].AnchorMessageID
			break
		}
	}
	if anchorSeq > 0 {
		rows, err := store.v8ReadRows(key, anchorSeq+1, 0)
		if err != nil {
			return v8ResumePoint{}, err
		}
		return v8ResumePoint{AnchorSeq: anchorSeq, AnchorMessageID: anchorMessageID, Rows: rows}, nil
	}
	// 无 interrupted 事件：消息完整 → 无续跑内容，不合成虚假 interrupted
	// （T-R3-02）；消息残缺 → 合成锚点 + 修复占位（T-R3-03）。
	open := store.v8FindOpenTail(key)
	if !open.open {
		return v8ResumePoint{AnchorSeq: messageHead.LastSeq}, nil
	}
	anchor := open.startSeq - 1
	if anchor > messageHead.LastSeq {
		anchor = 0
	}
	rows, err := store.v8ReadRows(key, open.startSeq, 0)
	if err != nil {
		return v8ResumePoint{}, err
	}
	repair := make([]v8WireMessage, 0, len(open.calls))
	for _, callID := range open.calls {
		repair = append(repair, v8WireMessage{
			Role:       v8WireRoleTool,
			ToolCallID: callID,
			Content:    "【缺失工具结果 · 修复占位】恢复检测到 open 工具轮，装配层补齐结果。",
			Repair:     true,
		})
	}
	return v8ResumePoint{
		AnchorSeq:  anchor,
		Rows:       rows,
		Synthetic:  true,
		Repair:     repair,
		Incomplete: true,
	}, nil
}

// v8OpenTailInfo 是流尾残缺检测结果。
type v8OpenTailInfo struct {
	open     bool
	startSeq uint64
	calls    []string
}

// v8FindOpenTail 从最后一个 user 单元后扫描（无 user 则从首行）判断流尾
// 是否存在未完成工具轮。
func (store *v8Store) v8FindOpenTail(key Key) v8OpenTailInfo {
	all, err := store.v8ReadRows(key, 0, 0)
	if err != nil {
		return v8OpenTailInfo{}
	}
	start := uint64(1)
	for index := len(all) - 1; index >= 0; index-- {
		if all[index].Role == "user" && EventKindOf(all[index]) == EventKindUserInput {
			start = all[index].Seq + 1
			break
		}
	}
	pending := make(map[string]bool)
	var order []string
	declared := make(map[string]bool)
	for _, row := range all {
		if row.Seq < start {
			continue
		}
		switch EventKindOf(row) {
		case EventKindToolCall:
			for _, call := range row.ToolCalls {
				declared[call.ID] = true
				pending[call.ID] = true
				order = append(order, call.ID)
			}
		case EventKindToolOutput:
			if row.ToolCallID != "" && declared[row.ToolCallID] {
				delete(pending, row.ToolCallID)
			}
		}
	}
	if len(pending) == 0 {
		return v8OpenTailInfo{}
	}
	calls := make([]string, 0, len(order))
	for _, callID := range order {
		if pending[callID] {
			calls = append(calls, callID)
		}
	}
	firstOpen := all[0].Seq
	for _, row := range all {
		if row.Seq < start {
			continue
		}
		open := false
		for _, call := range row.ToolCalls {
			if pending[call.ID] {
				open = true
				break
			}
		}
		if open {
			firstOpen = row.Seq
			break
		}
	}
	return v8OpenTailInfo{open: true, startSeq: firstOpen, calls: calls}
}
