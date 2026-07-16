// ── 审批组件：状态管理与键盘处理 ──────────────────────────
//
// 职责：
//   1. 审批状态的进入/退出
//   2. 键盘导航（↑↓ 选择、Enter 确认、Esc 取消）
//   3. 审批结果通道管理

package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ── tea.Msg ─────────────────────────────────────────────────────

// approveTickMsg 是审批倒计时的时钟滴答消息。
type approveTickMsg time.Time

// approveTickCmd 返回定时器命令。
func approveTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return approveTickMsg(t)
	})
}

// ── 审批启动入口 ─────────────────────────────────────────────

// startApprove 启动一个简单的审批（兼容旧版 ask_approve 工具）。
// 从 goroutine 中调用，阻塞直到用户确认或取消。
func (m *Model) startApprove(question string, choices []string) string {
	if m.approve.ch != nil {
		close(m.approve.ch)
	}
	ch := make(chan string, 1)
	m.approve = approveFromSimple(question, choices)
	m.approve.ch = ch
	m.approve.selected = 0
	m.approve.startTime = time.Now()
	// 触发 bubbletea 视图更新
	return <-ch
}

// startApproveFromRequest 从 PermissionApprovalRequest 启动审批。
func (m *Model) startApproveFromRequest(req PermissionApprovalRequest) string {
	if m.approve.ch != nil {
		close(m.approve.ch)
	}
	ch := make(chan string, 1)
	m.approve = approveFromRequest(req)
	m.approve.ch = ch
	m.approve.selected = 0
	return <-ch
}

// ── 检查是否有待处理的审批 ─────────────────────────────────

// checkPendingApprove 检查全局 pendingApproval 并激活审批模式。
// 在 Update 开头调用，与 checkPrompt 类似。
func (m *Model) checkPendingApprove() {
	if pendingApproval.ch != nil && !m.approvePrompting {
		m.approve = approveFromRequest(pendingApproval.req)
		m.approve.ch = pendingApproval.ch
		m.approve.selected = 0
		m.approve.startTime = time.Now()
		m.approvePrompting = true
		pendingApproval = pendingApprovalRequest{}
	}
}

// ── 审批键盘处理 ───────────────────────────────────────────

// handleApproveKey 处理审批模式下的键盘事件。
func (m Model) handleApproveKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// 如果已解决，不处理键盘
	if m.approve.resolved {
		return m, nil
	}

	switch msg.String() {
	case "enter":
		if m.approve.selected >= 0 && m.approve.selected < len(m.approve.options) {
			m.resolveApprove(m.approve.options[m.approve.selected].Key)
		}
		return m, nil

	case "up":
		if m.approve.selected > 0 {
			m.approve.selected--
		}
		return m, nil

	case "down":
		if m.approve.selected < len(m.approve.options)-1 {
			m.approve.selected++
		}
		return m, nil

	case "esc", "ctrl+c", "ctrl+d":
		m.resolveApprove("__CANCEL__")
		return m, nil

	default:
		s := msg.String()
		if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
			if idx := int(s[0] - '1'); idx < len(m.approve.options) {
				m.approve.selected = idx
				m.resolveApprove(m.approve.options[idx].Key)
			}
		}
		return m, nil
	}
}

// resolveApprove 解决审批，将选择发送到通道并清理状态。
func (m *Model) resolveApprove(choice string) {
	m.approve.resolved = true
	m.approve.result = choice
	m.approvePrompting = false
	if m.approve.ch != nil {
		select {
		case m.approve.ch <- choice:
		default:
		}
		// 不 close channel，防止重复发送 panic
		// channel 由垃圾回收处理
	}
}

// handleApproveTick 处理倒计时时钟滴答。
func (m Model) handleApproveTick(msg approveTickMsg) (tea.Model, tea.Cmd) {
	_ = msg
	if !m.approvePrompting {
		return m, nil
	}
	if m.approve.resolved {
		return m, nil
	}
	// 超时自动取消
	if m.approve.isTimeout() {
		m.resolveApprove("__TIMEOUT__")
		return m, nil
	}
	// 继续计时
	return m, approveTickCmd()
}

// ── 桥接函数（供 PermissionGate 回调使用） ─────────────────

// newApprovalHandler 创建一个审批处理器，桥接 Permission Gate 到 TUI。
// 返回的函数符合 permission.ApprovalHandler 签名。
// 注意：由于包依赖关系，此函数在 main.go 中通过闭包实现。
// 此辅助函数用于构建桥接通道。

// HandleApprovalBridge 是外部可调用的审批入口（兼容旧版 HandleApproval）。
// 返回用户选择的 key。
func HandleApprovalBridge(question string, choices []string) string {
	ch := make(chan string, 1)
	req := PermissionApprovalRequest{
		Question: question,
		Options:  choicesToOpts(choices),
		Risk:     "low",
	}
	pendingApproval = pendingApprovalRequest{req: req, ch: ch}
	return <-ch
}

func choicesToOpts(choices []string) []PermissionApprovalOpt {
	opts := make([]PermissionApprovalOpt, len(choices))
	for i, c := range choices {
		label := c
		desc := ""
		switch c {
		case "Yes":
			desc = "确认执行"
		case "No":
			desc = "取消操作"
		case "allow", "允许执行":
			label = "允许执行"
			desc = "放行此操作"
		case "always", "始终允许":
			label = "始终允许"
			desc = "记住此选择"
			opts[i].Style = "warning"
		case "deny", "拒绝":
			label = "拒绝"
			desc = "禁止执行"
			opts[i].Style = "danger"
		case "execute", "执行":
			desc = "按计划执行"
			opts[i].Style = "primary"
		case "skip", "跳过":
			desc = "跳过本次"
		case "abort", "终止":
			desc = "终止流程"
			opts[i].Style = "danger"
		case "confirm", "确认":
			desc = "确认操作"
			opts[i].Style = "primary"
		}
		opts[i] = PermissionApprovalOpt{
			Key:         c,
			Label:       label,
			Description: desc,
			Style:       opts[i].Style,
		}
	}
	return opts
}

// ── 全局待处理审批（桥接 goroutine → TUI Update） ────────

var (
	pendingApproval   pendingApprovalRequest
	approvalBridgeCh  chan string
)

type pendingApprovalRequest struct {
	req PermissionApprovalRequest
	ch  chan string
}

func initApprovalBridge() chan string {
	if approvalBridgeCh == nil {
		approvalBridgeCh = make(chan string, 1)
	}
	return approvalBridgeCh
}

// SetPendingApproval 设置待处理的审批请求（供 main.go 桥接 Permission Gate）。
func SetPendingApproval(req PermissionApprovalRequest, ch chan string) {
	pendingApproval = pendingApprovalRequest{req: req, ch: ch}
}

// ── 编译期常量引用 ──────────────────────────────────────────

var _ = fmt.Sprintf
