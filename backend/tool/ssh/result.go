package ssh

// result.go —— 返回信封：三种形态 + 分段归因（docs/arch/ssh-tool.md §4 / §5）。
//
// 这里刻意**没有** `success` 字段：工具只报告传输与退出码事实，"业务是否成功"
// 的判据留在调用方手里。上游那次"健康检查对 404 也报通过"就是判据错位的产物
// （exit 0 ≠ 成功），本包不提供替人下结论的能力。

// Status 是一次执行/探针的终态。
type Status string

const (
	// StatusCompleted 表示远端命令自行结束，通道拿到了退出状态。
	StatusCompleted Status = "completed"
	// StatusDetached 表示已 setsid 脱钩并返回日志路径，远端仍在跑。
	StatusDetached Status = "detached"
	// StatusChannelTimeout 表示**本端不再等待**；远端可能仍在跑。
	// 它与失败是两件事：拿它当失败会诱发"改对的代码被推翻"。
	StatusChannelTimeout Status = "channel_timeout"
	// StatusFailed 表示命令根本没跑起来（传输/认证/指纹/参数类）。
	StatusFailed Status = "failed"
)

// Segment 是四段超时 / 分段的归因名。
type Segment string

const (
	// SegConnect 是 TCP 建连段。
	SegConnect Segment = "connect"
	// SegBanner 是版本交换 + 算法协商 + banner 段。
	SegBanner Segment = "banner"
	// SegAuth 是认证段。
	SegAuth Segment = "auth"
	// SegChannel 是单条命令的通道段。
	SegChannel Segment = "channel"
)

// Result 是返回信封。JSON 字段名即工具层契约，改动要同步工具 schema。
type Result struct {
	Status       Status  `json:"status"`
	ExitCode     int     `json:"exit_code"`
	Stdout       string  `json:"stdout"`
	Stderr       string  `json:"stderr"`
	Truncated    bool    `json:"truncated"`
	LossyUTF8    bool    `json:"encoding_lossy"`
	AuthUsed     string  `json:"auth_used"`
	Segment      Segment `json:"timed_out_segment,omitempty"`
	StillRunning bool    `json:"still_running"`
	Handle       string  `json:"handle,omitempty"`
	LogPath      string  `json:"log_path,omitempty"`
	DurationMS   int64   `json:"duration_ms"`
	// Class / Hint 只在 StatusFailed 时有值：Class 给机器判定，Hint 给人读的下一步。
	Class string `json:"class,omitempty"`
	Hint  string `json:"hint,omitempty"`
	// Note 是"事实提醒"（例如忽略型失败、口令提示），不含秘密。
	Note string `json:"note,omitempty"`
}

// Ran 报告命令是否真的在远端跑过（channel_timeout 也算跑过）。
func (r Result) Ran() bool {
	return r.Status == StatusCompleted || r.Status == StatusChannelTimeout
}

// Failed 报告本次是否属于"根本没跑起来"。
func (r Result) Failed() bool { return r.Status == StatusFailed }
