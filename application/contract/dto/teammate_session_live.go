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
//
// **字段名是 wire 契约**（2026-10-04 修）：GUI 经 Wails 绑定拿的是 JSON，前端
// `renderTeammateLiveSession` 按 snake_case 读（`session_id` / `role` / `live` /
// `running` / `messages` / `truncated`）。没有 json tag 时 `encoding/json` 按 Go 字段名
// 出键（`SessionID` / `Running` / …），前端拿到的是"全是 undefined"的一份对象——
// 于是这条读面在 GUI 上永远走"执行面不在本进程"的分支，而 Go 侧用例（读结构体）与
// 前端 .mjs 用例（喂 snake_case 夹具）**两边都绿**。守卫见
// `e2e/teammate_session_live_wire_test.go`。
type TeammateSessionLiveView struct {
	// SessionID 是这件事自己的会话号（一 Work Item 一套 Session）。
	SessionID string `json:"session_id"`
	// Role 是执行它的 teammate（角色名）。
	Role string `json:"role"`
	// Live 表示这一轮**此刻**还在跑（false = 会话槽还在，但手上没有在飞的回合）。
	Live bool `json:"live"`
	// Running 表示执行面本身还在（引擎槽在册）。false = 这个会话不在本进程里
	// （重启过 / 从未开过 / 已被退场清掉）——调用方据此不要假装"看到的是空的当前会话"。
	Running bool `json:"running"`
	// Messages 是**这一页**的对话（窗口内由旧到新）。默认页（`TeammateSessionLive`）
	// 给的是最近若干条；翻页读法（`TeammateSessionLivePage`）按 offset/limit 定位。
	Messages []TeammateSessionLiveMessage `json:"messages"`
	// Truncated 表示这一页之外还有被裁掉的内容（offset>0 = 前面还有；或某条正文被按
	// 长度上限截断）——"读的人以为这就是全部"是这件事的反面。
	Truncated bool `json:"truncated"`
	// Offset 是这一页的起点（从**该会话可投影的第一条**算起，0 = 最旧）。
	Offset int `json:"offset"`
	// Limit 是归一化后的页大小（<=0 → 默认 40，即 teammateSessionLiveMaxMessages）。
	Limit int `json:"limit"`
	// Total 是该会话里可投影的 user/assistant 条数（不是"这一页有几条"）。
	// 引擎历史本来就在内存里，分页因此不必靠"尾部截断"丢内容。
	Total int `json:"total"`
	// HasMore 表示这一页之后还有消息（offset+len(messages) < total）。
	HasMore bool `json:"has_more"`
}

// TeammateSessionLiveMessage 是实时对话里的一行。
type TeammateSessionLiveMessage struct {
	Role string `json:"role,omitempty"`
	Text string `json:"text,omitempty"`
}
