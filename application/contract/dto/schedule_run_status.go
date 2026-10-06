package dto

// ScheduleRunStatus 是**定时/周期任务上一次运行**的结果状态
// （`ScheduledTaskStatus.LastStatus`：GUI 定时任务面板的数据源）。
//
// 这一格回答的是"上一轮跑成什么样"，不是"这个任务现在开着没有"（后者是
// `ScheduledTaskStatus.Enabled` + `Running` 两个布尔位，不是词表）。
//
// 词表历史：它曾经是 `seelebridge/scheduler` 里的一份本地字符串常量
// （scheduledStatusPending|Running|OK|Failed|Skipped），而字段在契约 DTO 上——
// 写方在调度器、字段在契约、读方（面板/前端）按字面量比。合并后词只有这一份，
// 写方引枚举，面板读 `.String()`。
type ScheduleRunStatus uint8

const (
	// ScheduleRunUnknown 是零值：没跑过或被读回来的词认不得。
	ScheduleRunUnknown ScheduleRunStatus = iota
	// ScheduleRunPending = 还没跑过（刚建的任务）。
	ScheduleRunPending
	// ScheduleRunRunning = 正在跑。
	ScheduleRunRunning
	// ScheduleRunOK = 上一次跑成功。
	ScheduleRunOK
	// ScheduleRunFailed = 上一次跑失败。
	ScheduleRunFailed
	// ScheduleRunSkipped = 上一次被跳过（运行中被取消等）。
	ScheduleRunSkipped
)

// scheduleRunStatusWords 是"枚举 ↔ 对外词"的对照表（本格唯一一份）。
var scheduleRunStatusWords = [...]string{
	ScheduleRunUnknown: "unknown",
	ScheduleRunPending: "pending",
	ScheduleRunRunning: "running",
	ScheduleRunOK:      "ok",
	ScheduleRunFailed:  "failed",
	ScheduleRunSkipped: "skipped",
}

var scheduleRunStatusCodec = stateCodec{name: "定时任务上次运行结果", words: scheduleRunStatusWords[:]}

// String 给出对外词（面板 JSON 里的 last_status）。
func (s ScheduleRunStatus) String() string { return scheduleRunStatusCodec.word(uint8(s)) }

// ParseScheduleRunStatus 把对外词读回枚举；第二个返回值报告认不认得。
func ParseScheduleRunStatus(text string) (ScheduleRunStatus, bool) {
	ordinal, ok := scheduleRunStatusCodec.ordinal(text)
	return ScheduleRunStatus(ordinal), ok
}

// MarshalJSON 保住 wire 形状：JSON 里仍是 "ok" 这样的词。
func (s ScheduleRunStatus) MarshalJSON() ([]byte, error) {
	return scheduleRunStatusCodec.marshal(uint8(s))
}

// UnmarshalJSON 读回对外词；认不得的词报错，不动原值。
func (s *ScheduleRunStatus) UnmarshalJSON(data []byte) error {
	return scheduleRunStatusCodec.unmarshal(data, (*uint8)(s))
}
