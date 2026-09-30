package dto

// SeatJobOutcome 是一轮 goal 座位循环（jobs.KindSeat 作业）的终态读数，跨层传递
// 的纯 DTO：goal 域声明端口（SeatJobs），seelebridge 的 Runtime 实现"派发 + 汇合"。
//
// 为什么类型放在 dto 而不是 goal 域：端口两端分居 application/core 与 seelebridge，
// 而 seelebridge **不能**反向 import application/core（core 已 import seelebridge，
// 会成环）——dto 是双方都能 import 的叶子包，类型因此只有一份。
//
//   - State 是作业终态（running / done / failed / killed，与 Seele jobs.State 同字面量；
//     端口只带最小面字符串，不把框架类型拉进 goal 域）；
//   - ExitCode 是终态退出码（失败 1 / 硬上限 124 / 被杀 137，随作业面语义）；
//   - Summary 是终态有界摘要（≤512B：它按轮重播，必须严格有界）；
//   - Known 报告有没有真的读到记录（false = 句柄已不在册，终态未知）。
type SeatJobOutcome struct {
	State    string `json:"state,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
	Summary  string `json:"summary,omitempty"`
	Known    bool   `json:"known"`
}
