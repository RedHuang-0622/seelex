package model

import "time"

// SessionInputIndex 是「会话内全量用户输入索引」（对话区右侧导航刻度数据源）：
//
//   - 只索引**用户输入**：助手步骤/思考/工具/系统行都不产生刻度；
//   - 覆盖**整会话**：包含尚未加载到前端窗口的早期轮次（索引来自持久事实源，
//     不依赖已加载的可见窗口）；
//   - 只携带有界摘要（Summary），不携带正文——整份索引可以安全下发。
//
// 前端据此把整条轨道铺成整个会话：已加载轮次用真实几何定位，未加载轮次按
// 确定性比例布点；点击未加载轮次时先按 Window/Offset 回读那一页再定位。
type SessionInputIndex struct {
	SessionID string `json:"session_id"`
	// Total 是会话可见消息总数（分页偏移空间；与 Snapshot.TotalMessages 同源）。
	Total int `json:"total"`
	// InputCount 是索引到的用户输入条数（= len(Items)）。
	InputCount int `json:"input_count"`
	// Window 是当前已加载窗口（前端据此判定「点击是否要先回读」）。
	Window SessionInputWindow     `json:"window"`
	Items  []SessionInputIndexRow `json:"items,omitempty"`
}

// SessionInputWindow 描述目标会话当前已加载的可见窗口（消息偏移空间）。
type SessionInputWindow struct {
	// Offset 是窗口起始消息偏移（0 = 窗口已含最早消息）。
	Offset int `json:"offset"`
	// Count 是窗口内可见消息数。
	Count int `json:"count"`
	// Total 是会话可见消息总数。
	Total int `json:"total"`
	// HasMore 表示窗口之前还有更早的消息可回读。
	HasMore bool `json:"has_more"`
	// WindowSize 是一次回读推进的消息数（LoadMoreHistory 的步长），
	// 前端用它把「目标偏移差」换算成回读页数。
	WindowSize int `json:"window_size"`
}

// SessionInputIndexRow 是一条用户输入的索引项（有界摘要 + 定位键）。
type SessionInputIndexRow struct {
	// Round 是会话内第几条用户输入（1 起，全量序）。
	Round int `json:"round"`
	// MessageID 是可见会话消息定位键（前端 data-conversation-key =
	// "message:<MessageID>"；空 = 旧数据未标注，前端按位置回退匹配）。
	MessageID string `json:"message_id,omitempty"`
	// Offset 是该输入在会话可见消息里的偏移（回读页数换算用）。
	Offset int `json:"offset"`
	// RoundID 是群聊轮次 ID（一条 user 输入开启一轮；0 = 未标注）。
	RoundID uint64 `json:"round_id,omitempty"`
	// Seq 是事件/单元序号（0 = 未标注）。
	Seq uint64 `json:"seq,omitempty"`
	// RoleName / RoleSessionID 是群聊角色归属（谁提的这条输入）。
	RoleName      string `json:"role_name,omitempty"`
	RoleSessionID string `json:"role_session_id,omitempty"`
	// CreatedAt 是输入时间。
	CreatedAt time.Time `json:"created_at,omitempty"`
	// Summary 是有界摘要（空白折叠 + 长度截断），供刻度悬停显示。
	Summary string `json:"summary"`
	// Chars 是原文的字符数（> len(Summary) 即摘要被截断）。
	Chars int `json:"chars"`
	// Loaded 表示该输入是否落在当前已加载窗口内。
	Loaded bool `json:"loaded"`
}
