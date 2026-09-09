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

// historyReadRow 是 R1 输出的一行：事件行 + 展示标记。
type historyReadRow struct {
	Event
	// Internal 标记 internal_user/context 行（前端可过滤或折叠展示）。
	Internal bool `json:"internal,omitempty"`
	// Placeholder 标记 LRU 已淘汰区摘要占位（无正文原文）。
	Placeholder bool `json:"placeholder,omitempty"`
}

// pageHistoryRows 返回 message 分页（offset/limit 基于行坐标 1..last_seq；
// 已淘汰区由占位行承接，返回空洞不错位）。
func (store *storeEngine) pageHistoryRows(key Key, offset, limit int) ([]historyReadRow, int, error) {
	if offset < 0 {
		return nil, 0, errors.New("session storage: history offset must be >= 0")
	}
	if limit <= 0 {
		limit = 20
	}
	store.messageMu.Lock()
	head, err := store.readMessageHeadLocked(key)
	if err != nil {
		store.messageMu.Unlock()
		return nil, 0, err
	}
	store.messageMu.Unlock()
	total := int(head.LastSeq)
	if offset >= total {
		return []historyReadRow{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	rows, err := store.readRows(key, uint64(offset+1), uint64(end))
	if err != nil {
		return nil, 0, err
	}
	bySeq := make(map[uint64]Event, len(rows))
	for _, row := range rows {
		bySeq[row.Seq] = row
	}
	out := make([]historyReadRow, 0, end-offset)
	for seq := offset + 1; seq <= end; seq++ {
		row, ok := bySeq[uint64(seq)]
		if !ok {
			if uint64(seq) <= head.WatermarkSeq {
				out = append(out, historyReadRow{
					Event:       Event{Seq: uint64(seq), Role: "context", Kind: EventKindNotice, Content: "（LRU 已淘汰区：原始正文已按用户确认删除，详见 compact 摘要）"},
					Placeholder: true,
				})
				continue
			}
			return nil, 0, fmt.Errorf("session storage: history page hole at seq %d beyond watermark %d", seq, head.WatermarkSeq)
		}
		out = append(out, historyReadRow{
			Event:    row,
			Internal: EventKindOf(row) == EventKindInternal || row.Role == "context" || row.Role == "internal_user",
		})
	}
	return out, total, nil
}

// resumePoint 是 R3 断点续跑结果。
type resumePoint struct {
	// AnchorSeq / AnchorMessageID 是 resume 起点（该点之后的事件行）。
	AnchorSeq       uint64 `json:"anchor_seq"`
	AnchorMessageID string `json:"anchor_message_id,omitempty"`
	// Rows 是锚点后的尾段（含端点语义：anchor 行之后）。
	Rows []Event `json:"rows"`
	// Synthetic 表示 EVENT interrupted 缺失但消息残缺，恢复侧合成的锚点。
	Synthetic bool `json:"synthetic,omitempty"`
	// Repair 是残缺工具轮的修复占位。
	Repair []wireMessage `json:"repair,omitempty"`
	// Incomplete 表示消息自身残缺（缺 tool 结果）。
	Incomplete bool `json:"incomplete,omitempty"`
}

// resumeTail 定位断点并返回尾段。不读 EVENT 也能正确恢复（EVENT 只加速
// 定位）；有 interrupted 事件以事件锚点为准。
func (store *storeEngine) resumeTail(key Key) (resumePoint, error) {
	messageHead, err := store.readMessageHead(key)
	if err != nil {
		return resumePoint{}, err
	}
	if messageHead.LastSeq == 0 {
		return resumePoint{}, nil
	}
	// EVENT 定位（可缺失；缺失不改变正确性）。
	events, err := store.readEvents(key, 0, 0)
	if err != nil {
		return resumePoint{}, err
	}
	anchorSeq := uint64(0)
	anchorMessageID := ""
	for index := len(events) - 1; index >= 0; index-- {
		if events[index].Kind == structuralEventInterrupted {
			anchorSeq = events[index].AnchorSeq
			anchorMessageID = events[index].AnchorMessageID
			break
		}
	}
	if anchorSeq > 0 {
		rows, err := store.readRows(key, anchorSeq+1, 0)
		if err != nil {
			return resumePoint{}, err
		}
		return resumePoint{AnchorSeq: anchorSeq, AnchorMessageID: anchorMessageID, Rows: rows}, nil
	}
	// 无 interrupted 事件：消息完整 → 无续跑内容，不合成虚假 interrupted
	// （T-R3-02）；消息残缺 → 合成锚点 + 修复占位（T-R3-03）。
	open := store.findOpenTail(key)
	if !open.open {
		return resumePoint{AnchorSeq: messageHead.LastSeq}, nil
	}
	anchor := open.startSeq - 1
	if anchor > messageHead.LastSeq {
		anchor = 0
	}
	rows, err := store.readRows(key, open.startSeq, 0)
	if err != nil {
		return resumePoint{}, err
	}
	repair := make([]wireMessage, 0, len(open.calls))
	for _, callID := range open.calls {
		repair = append(repair, wireMessage{
			Role:       wireRoleTool,
			ToolCallID: callID,
			Content:    "【缺失工具结果 · 修复占位】恢复检测到 open 工具轮，装配层补齐结果。",
			Repair:     true,
		})
	}
	return resumePoint{
		AnchorSeq:  anchor,
		Rows:       rows,
		Synthetic:  true,
		Repair:     repair,
		Incomplete: true,
	}, nil
}

// openTailInfo 是流尾残缺检测结果。
type openTailInfo struct {
	open     bool
	startSeq uint64
	calls    []string
}

// findOpenTail 从最后一个 user 单元后扫描（无 user 则从首行）判断流尾
// 是否存在未完成工具轮。
func (store *storeEngine) findOpenTail(key Key) openTailInfo {
	all, err := store.readRows(key, 0, 0)
	if err != nil {
		return openTailInfo{}
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
		return openTailInfo{}
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
	return openTailInfo{open: true, startSeq: firstOpen, calls: calls}
}
