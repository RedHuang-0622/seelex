package dto

import "time"

// ScheduledTaskKind 周期任务类型。
type ScheduledTaskKind string

const (
	ScheduledTaskCommand ScheduledTaskKind = "command"
	ScheduledTaskPrompt  ScheduledTaskKind = "prompt"
)

// PeriodUnit 周期任务时间单位（空 = 使用 Interval 的秒级固定周期）。
// month 是日历月：月末日期自动钳制（如 1-31 加 1 月 → 2-28/29）。
type PeriodUnit string

const (
	PeriodHour  PeriodUnit = "hour"
	PeriodDay   PeriodUnit = "day"
	PeriodWeek  PeriodUnit = "week"
	PeriodMonth PeriodUnit = "month"
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
	Name        string
	Kind        ScheduledTaskKind
	Interval    time.Duration
	PeriodUnit  PeriodUnit // 可选：hour/day/week/month（空 = Interval）
	PeriodValue int        // 周期数值（>=1，配合 PeriodUnit 使用）
	Command     string     // kind=command：白名单键
	Prompt      string     // kind=prompt：提示词内容（非 secret，可进快照展示）
	SessionID   string     // 绑定会话（空 = 执行时当前 main session）
	Enabled     bool
}

// ScheduledTaskStatus 是周期任务只读快照（GUI 定时任务面板数据源）。
type ScheduledTaskStatus struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	IntervalSec int64     `json:"interval_seconds"`
	PeriodUnit  string    `json:"period_unit,omitempty"`
	PeriodValue int       `json:"period_value,omitempty"`
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
