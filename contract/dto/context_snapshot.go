package dto

// context_snapshot.go —— 可序列化的会话上下文快照（跨层读形状）。
//
// 形状放契约层的原因：`contract.NodeContextSnapshot` 是门面/工具直接消费的端口，
// 返回类型若指向引擎上下文包，会把引擎闭包拖进契约层（P3）。快照的格式化、
// Builder 与校验都是**纯 stdlib** 方法，因此随形状一起放在这里；引擎包保留
// 同名别名（`seelectx/snapshot`），调用点与类型身份零改动。

import (
	"fmt"
	"strings"
	"time"
)

// ContextSnapshot 表示一个会话的可传递上下文摘要。
type ContextSnapshot struct {
	SourceSessionID string      `json:"source_session_id"`
	ExportedAt      time.Time   `json:"exported_at"`
	Goal            string      `json:"goal,omitempty"`
	Decisions       []Decision  `json:"decisions,omitempty"`
	Findings        []string    `json:"findings,omitempty"`
	Progress        string      `json:"progress,omitempty"`
	Constraints     []string    `json:"constraints,omitempty"`
	PendingWork     []string    `json:"pending_work,omitempty"`
	MessageCount    int         `json:"message_count"`
	TokenEstimate   int         `json:"token_estimate,omitempty"`
	Escape          *EscapeInfo `json:"escape,omitempty"`
}

// Decision 关键决策记录。
type Decision struct {
	What         string   `json:"what"`
	Why          string   `json:"why"`
	Alternatives []string `json:"alternatives,omitempty"`
}

// EscapeInfo 循环逃逸信息。
type EscapeInfo struct {
	Reason     string `json:"reason"`
	Message    string `json:"message"`
	Iterations int    `json:"iterations"`
	ParentGoal string `json:"parent_goal,omitempty"`
}

// Format 将快照格式化为可注入 system prompt 的结构化文本。
func (snap *ContextSnapshot) Format() string {
	var b strings.Builder
	b.WriteString("## 继承上下文 (Inherited Context)\n")
	b.WriteString(fmt.Sprintf("> 来源会话: %s | 导出时间: %s | 消息数: %d\n",
		snap.SourceSessionID, snap.ExportedAt.Format("15:04:05"), snap.MessageCount))

	if snap.Goal != "" {
		b.WriteString("\n### 目标 (Goal)\n")
		b.WriteString(snap.Goal + "\n")
	}

	if len(snap.Decisions) > 0 {
		b.WriteString("\n### 关键决策 (Decisions)\n")
		for i, d := range snap.Decisions {
			b.WriteString(fmt.Sprintf("%d. **%s** — %s\n", i+1, d.What, d.Why))
			if len(d.Alternatives) > 0 {
				b.WriteString(fmt.Sprintf("   备选: %s\n", strings.Join(d.Alternatives, ", ")))
			}
		}
	}

	if len(snap.Findings) > 0 {
		b.WriteString("\n### 重要发现 (Findings)\n")
		for _, f := range snap.Findings {
			b.WriteString(fmt.Sprintf("- %s\n", f))
		}
	}

	if snap.Progress != "" {
		b.WriteString("\n### 已完成进度 (Progress)\n")
		b.WriteString(snap.Progress + "\n")
	}

	if len(snap.Constraints) > 0 {
		b.WriteString("\n### 约束条件 (Constraints)\n")
		for _, c := range snap.Constraints {
			b.WriteString(fmt.Sprintf("- %s\n", c))
		}
	}

	if len(snap.PendingWork) > 0 {
		b.WriteString("\n### 待完成工作 (Pending)\n")
		for _, w := range snap.PendingWork {
			b.WriteString(fmt.Sprintf("- [ ] %s\n", w))
		}
	}

	if snap.Escape != nil {
		b.WriteString("\n### 逃逸信息 (Escape)\n")
		b.WriteString(fmt.Sprintf("- 原因: %s\n", snap.Escape.Reason))
		b.WriteString(fmt.Sprintf("- 说明: %s\n", snap.Escape.Message))
		b.WriteString(fmt.Sprintf("- 迭代: %d\n", snap.Escape.Iterations))
	}

	b.WriteString("\n---\n")
	b.WriteString("以上为继承的上下文。请基于这些信息继续工作，" +
		"在决策时引用上述目标和约束。\n")
	return b.String()
}

// SetGoal 设置目标。
func (snap *ContextSnapshot) SetGoal(goal string) *ContextSnapshot {
	snap.Goal = goal
	return snap
}

// AddDecision 追加一条关键决策。
func (snap *ContextSnapshot) AddDecision(what, why string) *ContextSnapshot {
	snap.Decisions = append(snap.Decisions, Decision{What: what, Why: why})
	return snap
}

// AddFinding 追加一条重要发现。
func (snap *ContextSnapshot) AddFinding(finding string) *ContextSnapshot {
	snap.Findings = append(snap.Findings, finding)
	return snap
}

// SetProgress 设置已完成进度。
func (snap *ContextSnapshot) SetProgress(progress string) *ContextSnapshot {
	snap.Progress = progress
	return snap
}

// AddConstraint 追加一条约束。
func (snap *ContextSnapshot) AddConstraint(constraint string) *ContextSnapshot {
	snap.Constraints = append(snap.Constraints, constraint)
	return snap
}

// AddPendingWork 追加一条待完成工作。
func (snap *ContextSnapshot) AddPendingWork(work string) *ContextSnapshot {
	snap.PendingWork = append(snap.PendingWork, work)
	return snap
}

// SetEscape 设置循环逃逸信息。
func (snap *ContextSnapshot) SetEscape(reason, message string, iterations int) *ContextSnapshot {
	snap.Escape = &EscapeInfo{Reason: reason, Message: message, Iterations: iterations}
	return snap
}

// SetParentGoal 设置父目标（无 escape 时不做事）。
func (snap *ContextSnapshot) SetParentGoal(parentGoal string) *ContextSnapshot {
	if snap.Escape != nil {
		snap.Escape.ParentGoal = parentGoal
	}
	return snap
}

// ValidationError 表示快照验证失败的具体原因。
type ValidationError struct {
	Field string
	Err   string
}

func (ve *ValidationError) Error() string {
	return fmt.Sprintf("snapshot: validation failed on %q: %s", ve.Field, ve.Err)
}

// Validate 检查必填字段。
func (snap *ContextSnapshot) Validate() error {
	if snap == nil {
		return &ValidationError{Field: "snapshot", Err: "nil snapshot"}
	}
	if snap.SourceSessionID == "" {
		return &ValidationError{Field: "SourceSessionID", Err: "must not be empty"}
	}
	if snap.ExportedAt.IsZero() {
		return &ValidationError{Field: "ExportedAt", Err: "must not be zero"}
	}
	if snap.Goal == "" {
		hasParent := snap.Escape != nil && snap.Escape.ParentGoal != ""
		if !hasParent {
			return &ValidationError{Field: "Goal", Err: "must not be empty when no parent goal"}
		}
	}
	return nil
}

// Truncate 截断字符串到指定长度。
func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
