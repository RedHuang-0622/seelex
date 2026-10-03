package dto

// teammate_session_live.go — **当前 teammate 会话**的实时只读投影（`TeammateSessionLive`）。
//
// 生态位（2026-10-04 用户口径：看板点开的要是"当前的 teammate 的会话"，不是员工的长期
// 历史会话）：员工的**角色会话**是落盘的（跨工作项、跨轮次的长期历史），而 teammate 的一个
// Work Item 有**自己的会话**——它是**进程内执行面**（刻意不接 DurableHistory，见
// seelebridge.Runtime.ResetSession 的注释），因此它的正文不在会话库里，只能在它活着的
// 时候从执行面读。
//
// 这个投影就是那个读面：谁在跑、跑到第几轮、这一轮说了什么（有界）。**没有任何写入口**，
// 也不落盘——它回答的是"此刻"，而不是"历史上"。
type TeammateSessionLiveView struct {
	// SessionID 是这件事自己的会话号（一 Work Item 一套 Session）。
	SessionID string
	// Role 是执行它的 teammate（角色名）。
	Role string
	// Live 表示这一轮**此刻**还在跑（false = 会话槽还在，但手上没有在飞的回合）。
	Live bool
	// Running 表示执行面本身还在（引擎槽在册）。false = 这个会话不在本进程里
	// （重启过 / 从未开过 / 已被退场清掉）——调用方据此不要假装"看到的是空的当前会话"。
	Running bool
	// Messages 是有界裁剪过的对话（最近若干条，最早的在前面）。
	Messages []TeammateSessionLiveMessage
	// Truncated 表示因为条数/长度上限裁掉了更早的内容。
	Truncated bool
}

// TeammateSessionLiveMessage 是实时对话里的一行。
type TeammateSessionLiveMessage struct {
	Role string `json:"role,omitempty"`
	Text string `json:"text,omitempty"`
}
