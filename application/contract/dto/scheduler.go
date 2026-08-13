package dto

import "time"

// ScheduledTaskKind 周期任务类型。
type ScheduledTaskKind string

const (
	ScheduledTaskCommand ScheduledTaskKind = "command"
	ScheduledTaskPrompt  ScheduledTaskKind = "prompt"
)

// ScheduledCommand 白名单命令描述（登记即信任；argv 固定直传，不解析用户文本）。
type ScheduledCommand struct {
	Key         string   // 白名单键（任务引用，如 "auto_get_jobs"）
	Label       string   // 展示名
	Description string   // 说明（GUI 弹窗展示）
	WorkingDir  string   // 固定工作目录（脚本相对文件所在）
	Argv        []string // 固定参数（argv[0] 为可执行文件）
	TimeoutSec  int      // 单次运行超时（0 = 默认 10 分钟）
}

// ScheduledCommandInfo 是周期任务白名单命令的展示信息（GUI 新建弹窗数据源）。
type ScheduledCommandInfo struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// ScheduledTaskSpec 是周期任务的创建入参（变更入口）。
type ScheduledTaskSpec struct {
	Name      string
	Kind      ScheduledTaskKind
	Interval  time.Duration
	Command   string // kind=command：白名单键
	Prompt    string // kind=prompt：提示词内容（非 secret，可进快照展示）
	SessionID string // 绑定会话（空 = 执行时当前 main session）
	Enabled   bool
}

// ScheduledTaskStatus 是周期任务只读快照（GUI 定时任务面板数据源）。
type ScheduledTaskStatus struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	IntervalSec int64     `json:"interval_seconds"`
	Command     string    `json:"command,omitempty"`
	Prompt      string    `json:"prompt,omitempty"`
	SessionID   string    `json:"session_id,omitempty"`
	Enabled     bool      `json:"enabled"`
	Running     bool      `json:"running"`
	NextRunAt   time.Time `json:"next_run_at,omitempty"`
	LastRunAt   time.Time `json:"last_run_at,omitempty"`
	LastStatus  string    `json:"last_status,omitempty"`
	LastResult  string    `json:"last_result,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
	LogTail     []string  `json:"log_tail,omitempty"`
	RunCount    int64     `json:"run_count"`
}
