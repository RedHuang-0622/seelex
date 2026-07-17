package approve

import "time"

// State 是审批面板的运行时状态。
type State struct {
	// ─ 问题内容（来自 approve.Question） ─
	Question string         // Question.Content
	Options  []ChoiceOption // Question.Options
	Timeout  time.Duration  // Question.Timeout

	// ─ 额外元信息 ─
	Risk      string // "low" | "medium" | "high"
	ToolName  string
	Arguments string
	Preview   string

	// ─ 运行时 ─
	StartTime time.Time   // 审批开始时间
	Selected  int         // 当前选中项
	Resolved  bool        // 是否已解决
	Result    string      // 用户选择的 key
	ch        chan string // 结果通道（未导出）
}

// newState 从 approve.Question 创建 State。
func newState(q Question, risk, toolName, preview string) State {
	return State{
		Question:  q.Content,
		Options:   q.Options,
		Timeout:   q.Timeout,
		Risk:      risk,
		ToolName:  toolName,
		Preview:   preview,
		StartTime: time.Now(),
	}
}

// ── 辅助方法 ─────────────────────────────────────────────────

// Elapsed 返回审批已过时间。
func (s *State) Elapsed() time.Duration {
	if s.StartTime.IsZero() {
		return 0
	}
	return time.Since(s.StartTime)
}

// Remaining 返回剩余超时时间。
func (s *State) Remaining() time.Duration {
	if s.Timeout <= 0 {
		return 0
	}
	rem := s.Timeout - s.Elapsed()
	if rem < 0 {
		return 0
	}
	return rem
}

// TimeoutRatio 返回超时进度比例（0.0 ~ 1.0）。
func (s *State) TimeoutRatio() float64 {
	if s.Timeout <= 0 {
		return 0
	}
	r := float64(s.Elapsed()) / float64(s.Timeout)
	if r > 1 {
		return 1
	}
	return r
}

func (s *State) isTimeout() bool {
	return s.Timeout > 0 && s.Elapsed() >= s.Timeout
}

// RiskLabel 返回风险等级中文标签。
func (s *State) RiskLabel() string {
	switch s.Risk {
	case "high":
		return "高危操作"
	case "medium":
		return "需要确认"
	default:
		return "确认"
	}
}

// FormatRemaining 格式化剩余时间。
func (s *State) FormatRemaining() string {
	r := s.Remaining()
	if r <= 0 {
		return "超时"
	}
	if r >= time.Second {
		return formatFloat(r.Seconds()) + "s"
	}
	return "即将超时"
}

func formatFloat(f float64) string {
	i := int(f)
	if f-float64(i) >= 0.5 {
		i++
	}
	if i < 0 {
		i = 0
	}
	return intToString(i)
}

// 简单整型转字符串（避免 strconv 依赖）
func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
