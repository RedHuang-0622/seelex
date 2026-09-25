package dto

import "time"

// AsyncRunRecord 是一次后台命令（bash background=true）的**只读投影记录**。
//
// 生态位：seelebridge 的后台执行登记表是唯一事实源，core 只把它投影成工作表格行与
// 请求尾部打点块（见 application/core/work_table_async.go）。它**不是** task 注册表
// 条目：注册表随会话落盘（record.Tasks），而句柄表在内存——真进注册表就会在重启后
// 留下一条永远 running 的假行。所以这里的行随状态出现、随终态/驱逐消失，永不落盘
// （不变量 I-21）。
//
// 字段口径分两层，别混：
//   - Command / LogPath / StartedAt / EndedAt 只到 GUI（工作表格行、详情、附件列）；
//   - 进模型上下文的那份（尾部打点块）只允许用 Handle / State / LogBytes /
//     Description，且终态行不进块。绝对路径与时间戳进上下文既烧 token 也没信息量。
type AsyncRunRecord struct {
	SessionID string `json:"session_id"`
	Handle    string `json:"handle"` // 句柄原文（a3）；工作表格行 ID 为 async:a3
	// Description 是模型派发时写的一句话（background=true 必填）→ 工作表格行标题。
	Description string `json:"description,omitempty"`
	// Command 是命令行原文（单行截断后的形态）→ 描述列。它本来就在该会话历史里
	// （那次 bash 调用的入参），投影到界面不构成新增暴露面。
	Command string `json:"command,omitempty"`
	// State = running | done | failed | killed（与 seelebridge 的终态口径同源）。
	State    string `json:"state"`
	ExitCode int    `json:"exit_code"` // running 时为 -1，不是 0
	// LogBytes 是已落盘字节数：真实的进展信号。打点块用它，不用墙钟猜状态。
	LogBytes int64 `json:"log_bytes"`
	// Tail 是输出末行采样（探针读数，只到 GUI）；LastByteAt 是输出文件最后修改时间。
	Tail       string    `json:"tail,omitempty"`
	LastByteAt time.Time `json:"last_byte_at,omitempty"`
	// LogPath 是输出文件绝对路径（只上 GUI 的附件列，不进上下文）。
	LogPath string `json:"log_path,omitempty"`
	// Truncated = 输出已按 asyncLogCap 截断；Degraded = 进程树挂不上，终止只打到直接子进程。
	Truncated bool `json:"truncated,omitempty"`
	Degraded  bool `json:"degraded,omitempty"`
	// BatchID = 派发它的那次 chat 请求，工作表格据此把行归到正确批次。
	BatchID   string    `json:"batch_id,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
}

// AsyncStateRunning 是"还在跑"的唯一状态字面量（core 投影据此决定行进不进打点块）。
const AsyncStateRunning = "running"
