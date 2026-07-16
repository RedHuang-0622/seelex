// ── 审批组件类型定义 ──────────────────────────────────────────
//
// 提供丰富的审批数据结构：
//   - 选项卡片描述
//   - 风险等级
//   - 超时倒计时
//   - "始终允许" 记忆

package tui

import (
	"fmt"
	"time"
)

// ── 审批状态 ───────────────────────────────────────────────────

// approveState 是独立于主 Model 的审批状态。
// 对应调研文档 Phase 2 的 ApproveModel 概念（以状态结构体而非完整 tea.Model 实现）。
type approveState struct {
	// ─ 请求内容 ─
	question  string         // 审批问题
	options   []approveOpt   // 选项列表（含描述、样式）
	risk      string         // "low" | "medium" | "high"
	timeout   time.Duration  // 超时时间（0 = 无超时）
	startTime time.Time      // 审批开始时间

	// ─ 当前选择 ─
	selected int

	// ─ 是否已解决 ─
	resolved bool
	result   string // 用户选择的 key

	// ─ 通道（goroutine → TUI 桥接） ─
	ch chan string

	// ─ 源信息（Permission Gate 场景） ─
	toolName  string
	arguments string
	preview   string
}

// approveOpt 是带描述的审批选项。
type approveOpt struct {
	Key         string // 选项标识
	Label       string // 显示标签
	Description string // 描述文字
	Style       string // "primary" | "secondary" | "danger" | "warning"
}

// ── 工厂 ──────────────────────────────────────────────────────

func newApproveState() approveState {
	return approveState{}
}

// approveFromSimple 从简单文本/选项列表创建审批状态（兼容现有的 ask_approve 工具）。
func approveFromSimple(question string, choices []string) approveState {
	opts := make([]approveOpt, len(choices))
	for i, c := range choices {
		opts[i] = approveOpt{
			Key:   c,
			Label: c,
		}
	}
	return approveState{
		question:  question,
		options:   opts,
		risk:      "low",
		startTime: time.Now(),
	}
}

// approveFromRequest 从 PermissionApprovalRequest 创建审批状态。
func approveFromRequest(req PermissionApprovalRequest) approveState {
	opts := make([]approveOpt, len(req.Options))
	for i, o := range req.Options {
		sty := o.Style
		if sty == "" {
			sty = "secondary"
		}
		opts[i] = approveOpt{
			Key:         o.Key,
			Label:       o.Label,
			Description: o.Description,
			Style:       sty,
		}
	}
	return approveState{
		question:  req.Question,
		options:   opts,
		risk:      req.Risk,
		timeout:   req.Timeout,
		startTime: time.Now(),
		toolName:  req.ToolName,
		arguments: req.Arguments,
		preview:   req.Preview,
	}
}

// ── 方法 ──────────────────────────────────────────────────────

// elapsed 返回审批已过去的时间。
func (a *approveState) elapsed() time.Duration {
	if a.startTime.IsZero() {
		return 0
	}
	return time.Since(a.startTime)
}

// remaining 返回剩余超时时间（无超时时返回 0）。
func (a *approveState) remaining() time.Duration {
	if a.timeout <= 0 {
		return 0
	}
	rem := a.timeout - a.elapsed()
	if rem < 0 {
		return 0
	}
	return rem
}

// timeoutRatio 返回超时比例（0.0 ~ 1.0），用于进度条渲染。
func (a *approveState) timeoutRatio() float64 {
	if a.timeout <= 0 {
		return 0
	}
	ratio := float64(a.elapsed()) / float64(a.timeout)
	if ratio > 1 {
		return 1
	}
	return ratio
}

// isTimeout 是否已超时。
func (a *approveState) isTimeout() bool {
	return a.timeout > 0 && a.elapsed() >= a.timeout
}

// formatRemaining 格式化显示剩余时间。
func (a *approveState) formatRemaining() string {
	r := a.remaining()
	if r <= 0 {
		return "超时"
	}
	if r >= time.Second {
		return fmt.Sprintf("%.0fs", r.Seconds())
	}
	return "即将超时"
}

// riskLabel 返回风险等级标签。
func (a *approveState) riskLabel() string {
	switch a.risk {
	case "high":
		return "高危操作"
	case "medium":
		return "需要确认"
	default:
		return "确认"
	}
}

// styleForOpt 返回选项的样式键。
func (a *approveState) styleForOpt(i int) string {
	if a.resolved {
		return "done"
	}
	if i == a.selected {
		return "active"
	}
	if i < len(a.options) {
		if a.options[i].Style == "danger" {
			return "danger"
		}
	}
	return "inactive"
}

// ── 导出类型（外部 API） ──────────────────────────────────────

// PermissionApprovalRequest 是外部（Permission Gate / ask_approve 工具）
// 传入的审批请求结构体。
type PermissionApprovalRequest struct {
	Question  string               // 向用户展示的问题
	Options   []PermissionApprovalOpt // 选项列表
	Risk      string               // "low" | "medium" | "high"
	Timeout   time.Duration        // 超时
	ToolName  string               // 工具名（Gate 场景）
	Arguments string               // 参数（Gate 场景）
	Preview   string               // 人类可读预览
}

// PermissionApprovalOpt 是外部传入的审批选项。
type PermissionApprovalOpt struct {
	Key         string
	Label       string
	Description string
	Style       string
}
