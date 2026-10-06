package dto

import "time"

// AsyncRunRecord 是一次**作业**（bash_bg / read_batch / subagent）的**只读投影记录**。
//
// 生态位：seelebridge 的后台执行登记表是唯一事实源，core 只把它投影成工作表格行与
// 请求尾部打点块（见 application/core/work_table_async.go）。它**不是** task 注册表
// 条目：注册表随会话落盘（record.Tasks），而句柄表在内存——真进注册表就会在重启后
// 留下一条永远 running 的假行。所以这里的行随状态出现、随终态/驱逐消失，永不落盘
// （不变量 I-21）。
//
// 字段口径分三层，别混：
//   - Command / LogPath / StartedAt / EndedAt 只到 GUI（工作表格行、详情、附件列）；
//   - 进模型上下文的那份（尾部打点块）只允许用 Handle / Kind / State / LogBytes /
//     Description / **有界 Summary**：在途行带标题，完成行带摘要（退出码 + 行数 +
//     字节数 + 有界末行）。绝对路径、末行原文与时间戳进上下文既烧 token 也没信息量；
//   - Summary 有硬上限（seelebridge 侧 ≤512B）：完成行随打点块**每轮重播**，
//     无界即按轮数线性烧 token。
type AsyncRunRecord struct {
	SessionID string `json:"session_id"`
	Handle    string `json:"handle"` // 句柄原文（a3）；工作表格行 ID 为 async:a3
	// Kind = process | inline | subagent：决定读取口与 kill 语义（句柄是同一套）。
	Kind string `json:"kind,omitempty"`
	// Description 是模型派发时写的一句话（bash_bg 的 description 必填）→ 行标题。
	Description string `json:"description,omitempty"`
	// Command 是命令行原文（单行截断后的形态）→ 描述列。它本来就在该会话历史里
	// （那次 bash 调用的入参），投影到界面不构成新增暴露面。
	Command string `json:"command,omitempty"`
	// State = running | done | failed | killed（与 seelebridge 的终态口径同源）。
	State    string `json:"state"`
	ExitCode int    `json:"exit_code"` // running 时为 -1，不是 0
	// LogBytes 是已落盘字节数：真实的进展信号。打点块用它，不用墙钟猜状态。
	LogBytes int64 `json:"log_bytes"`
	// Summary 是终态**有界摘要**（≤512B）：完成行回填进打点块的就是它，全文只走 fetch。
	// Lines 是输出行数（摘要的一部分，单独成列便于前端直接用）。
	Summary string `json:"summary,omitempty"`
	Lines   int    `json:"lines,omitempty"`
	// Notified = 该作业的终态已回填过一次（幂等键）：重复 finish / 重复 done 不得产生
	// 第二次回填，因此这个位只会被置一次。
	Notified bool `json:"notified,omitempty"`
	// Index 是同批内 wire 下标（inline 扇出）：排序键，不是完成序。
	Index int `json:"index,omitempty"`
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

// ── 后台作业状态词表（唯一一份）───────────────────────────────────────────
//
// 这一格回答"这个作业还在不在跑"。取值面就是 AsyncRunRecord.State 注释里的四个词；四个词
// 只在这里定义**一次**——登记表（seelebridge/tools 的 async_exec.go）、探针与作业面、
// 触发口径（application/core 的 async_completion.go 与 work_table_async.go）都引它，不再
// 各写一份字面量（③U6：跨包的同一份词靠这一处 + e2e 的源码门禁互锁）。
const (
	AsyncStateRunning = "running"
	AsyncStateDone    = "done"
	AsyncStateFailed  = "failed"
	// AsyncStateKilled = 由 job_manage(op=kill) 或会话销毁终止，与"命令自己退非零"可分。
	AsyncStateKilled = "killed"
)
