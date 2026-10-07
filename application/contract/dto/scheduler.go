package dto

import "time"

// ScheduledTaskKind 定时/周期任务类型。
type ScheduledTaskKind string

const (
	ScheduledTaskCommand ScheduledTaskKind = "command"
	ScheduledTaskPrompt  ScheduledTaskKind = "prompt"
)

// PeriodUnit 周期任务时间单位（空 = 使用 Interval 的秒级固定周期；
// 一次性定时任务使用 RunAt，不依赖 PeriodUnit）。
// month 是日历月：月末日期自动钳制（如 1-31 加 1 月 → 2-28/29）。
type PeriodUnit string

const (
	PeriodMinute PeriodUnit = "minute"
	PeriodHour   PeriodUnit = "hour"
	PeriodDay    PeriodUnit = "day"
	PeriodWeek   PeriodUnit = "week"
	PeriodMonth  PeriodUnit = "month"
)

// 周期锚点星期用 ISO 口径：1 = 周一 … 7 = 周日（0 = 未指定）。
// 不用 time.Weekday 是因为它的 0 是周日，与"0 = 未指定"的零值撞车。
const (
	WeekdayUnset  = 0
	WeekdayMonday = 1
	WeekdaySunday = 7
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

// ScheduledCommandInfo 是定时/周期任务白名单命令的展示信息（GUI 新建弹窗数据源）。
type ScheduledCommandInfo struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// ScheduledTaskSpec 是定时/周期任务的创建入参（变更入口）。
type ScheduledTaskSpec struct {
	Name        string
	Kind        ScheduledTaskKind
	Interval    time.Duration
	PeriodUnit  PeriodUnit // 可选：minute/hour/day/week/month（空 = Interval）
	PeriodValue int        // 周期数值（>=1，配合 PeriodUnit 使用）
	// StartClock 是周期锚点时刻 "HH:MM"（空 = 以创建时刻为锚点，即"每个周期
	// 走当前时间"）。只有 day/week/month 接受它：每天 09:00、每周一 09:00、
	// 每月同日 09:00；minute/hour 不接受（子日周期没有"几点开始"可言）。
	StartClock string
	// StartWeekday 是周周期的锚点星期（ISO 1=周一 … 7=周日；0 = 未指定）。
	// 只有 PeriodWeek 接受它，且必须与 StartClock 同时给出。
	StartWeekday int
	RunAt        time.Time // 一次性定时任务执行时间（零值 = 周期任务）
	Command      string    // kind=command：白名单键
	Prompt       string    // kind=prompt：提示词内容（非 secret，可进快照展示）
	Enabled      bool
	// SessionID 是显式的既有会话绑定；空 = **每次触发新建会话发起**（默认口径）。
	SessionID string
	// WorkspaceID 是触发时新会话要装配的工作区（空 = 不绑项目）。
	// 它只影响触发那次会话的项目装配；任务定义本身是全局资产，与项目无关。
	WorkspaceID string
	// PermissionTier 是触发时会话装配的权限档位（空 = **默认 full access**：
	// 定时任务在后台跑，没人能在审批弹窗上点"同意"）。取值见 dto.PermissionTierIDs，
	// 未识别的档位显式拒绝，不静默降级。
	PermissionTier string
	// Plugins 是触发时这一轮的能力包装配（空 = 不覆盖，继承宿主当前激活插件）。
	// 由 dto.NormalizePlugins 归一：去空白、重复声明显式拒绝、超上限显式拒绝。
	Plugins []string
}

// ScheduledTaskStatus 是定时/周期任务只读快照（GUI 定时任务面板数据源）。
type ScheduledTaskStatus struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Kind         string            `json:"kind"`
	IntervalSec  int64             `json:"interval_seconds"`
	PeriodUnit   string            `json:"period_unit,omitempty"`
	PeriodValue  int               `json:"period_value,omitempty"`
	StartClock   string            `json:"start_clock,omitempty"`   // 周期锚点 "HH:MM"（空 = 以创建时刻为锚点）
	StartWeekday int               `json:"start_weekday,omitempty"` // 周周期锚点星期（ISO 1=周一 … 7=周日）
	RunAt        time.Time         `json:"run_at,omitempty"`        // 一次性任务的预定执行时间（零值 = 周期任务）
	OneShot      bool              `json:"one_shot,omitempty"`      // 是否一次性定时任务（执行后自动停用）
	Command      string            `json:"command,omitempty"`
	Prompt       string            `json:"prompt,omitempty"`
	SessionID    string            `json:"session_id,omitempty"`
	Enabled      bool              `json:"enabled"`
	Running      bool              `json:"running"`
	NextRunAt    time.Time         `json:"next_run_at,omitempty"`
	LastRunAt    time.Time         `json:"last_run_at,omitempty"`
	LastStatus   ScheduleRunStatus `json:"last_status,omitempty"`
	LastResult   string            `json:"last_result,omitempty"`
	LastError    string            `json:"last_error,omitempty"`
	LogTail      []string          `json:"log_tail,omitempty"`
	RunCount     int64             `json:"run_count"`
	// WorkspaceID 是任务装配的工作区（空 = 无工作区）。
	WorkspaceID string `json:"workspace_id,omitempty"`
	// LastSessionID 是上一次触发真正落到的会话（空 = 未落会话/从未触发）。
	// 每次触发默认新建会话，这一格让面板与冒烟能指认"跑到哪儿去了"。
	LastSessionID string `json:"last_session_id,omitempty"`
	// PermissionTier / Plugins 是这条任务声明的装配（触发时落到那次会话/那一轮）。
	PermissionTier string   `json:"permission_tier,omitempty"`
	Plugins        []string `json:"plugins,omitempty"`
}
