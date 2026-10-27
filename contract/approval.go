package contract

// 本文件是异步审批的**端口形状**：ApprovalBroker 端口的入参与返回形状、
// 以及它的两个哨兵错误。
//
// 为什么形状放这里而不是实现包：`contract` 是依赖倒置的边界，实现体
// （`backend/limit/approval.ApprovalBroker`）与消费方（`application/assembly`、
// `wiring/adapters`、前端）都只认这里的定义——实现包换落点/换实现时，
// 端口与形状不动。这也是把 `application/approval` 搬进 limit 板块时
// 必须先把形状留在底座的原因：契约层不得反过来依赖 backend 包。

import (
	"errors"
	"time"

	model "github.com/RedHuang-0622/seelex/contract/protocol"
)

var (
	// ErrInteractionNotFound 表示按 ID 找不到待批交互（可能已超时/已关闭）。
	ErrInteractionNotFound = errors.New("interaction not found")
	// ErrInteractionResolved 表示该待批交互已被解析过（幂等拒绝，不重复回执）。
	ErrInteractionResolved = errors.New("interaction already resolved")
)

// ApprovalRequest 是一次待批请求的正文（谁、哪个会话、问什么、有什么选项）。
type ApprovalRequest struct {
	ID string
	// SessionID 是审批的会话级归属（波 4：awaiting_approval/待批列表按
	// sid 分格；空 = 进程级/无会话路径——权限门与旧测试夹具的 legacy 面）。
	SessionID         string
	Question          string
	Options           []model.InteractionOption
	Risk              string
	ToolName          string
	Preview           string
	Timeout           time.Duration
	PermissionRequest bool
}

// ApprovalDecision 是用户对一次待批请求的决议（选项 ID）。
type ApprovalDecision struct {
	OptionID string `json:"option_id"`
}

// PendingApproval 是 broker 待批集合的会话归属视图（快照/目录待批列表用）。
type PendingApproval struct {
	SessionID   string
	Interaction model.Interaction
}
