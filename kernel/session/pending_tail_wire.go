package session_runtime

import (
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// 草稿尾部（seq_draft）在会话域的窄端口面。
//
// 语义（见 store/pending_tail.go 与 recovery-order.md）：
//   - 发布点 = message head.LastSeq；seq > LastSeq 的行是**未提交草稿**；
//   - 探测（PendingMessageTail）只读，不改任何状态；
//   - 恢复（RecoverPendingMessageTail）是**唯一的发布入口**，且要求草稿尾部
//     恰好是发布点的连续后续（基座一致）才发布——基座断裂只报告不发布、不清理
//     （红线 3：把决策留给应用/人工）；
//   - 丢弃（DiscardPendingMessageTail）是显式清理，不隐式发生。
//
// 全部是**可选能力**：存储未装配（非 v8 布局）时返回 ok=false，应用侧行为与
// 没有该能力时完全一致（恢复语义是纯增强）。
type SessionPendingTailPort interface {
	// PendingMessageTailWorkspace 只读探测草稿尾部。
	PendingMessageTailWorkspace(projectID, sessionID string) (dto.PendingMessageTailReport, bool, error)
	// RecoverPendingMessageTailWorkspace 基座一致时显式发布草稿尾部。
	RecoverPendingMessageTailWorkspace(projectID, sessionID string) (dto.PendingMessageTailReport, bool, error)
	// DiscardPendingMessageTailWorkspace 显式丢弃草稿尾部。
	DiscardPendingMessageTailWorkspace(projectID, sessionID string) (dto.PendingMessageTailReport, bool, error)
}

// SessionDraftSnapshotPort 把运行期尚未落快照的 transcript 后缀追加到
// message 草稿尾；不推进发布点，最终仍由完整会话提交接管。
type SessionDraftSnapshotPort interface {
	AppendDraftSnapshotWorkspace(projectID, sessionID string, events []model.TranscriptEvent) (int, bool, error)
}

// pendingTailPort 解析存储侧草稿尾部能力（未装配时 ok=false）。
func (c *Coordinator) pendingTailPort() (SessionPendingTailPort, bool) {
	if c == nil || c.Core == nil {
		return nil, false
	}
	port, ok := c.Core.Deps.Sessions.(SessionPendingTailPort)
	if !ok {
		return nil, false
	}
	return port, true
}

// PendingMessageTail 探测目标会话的草稿尾部（只读；ok=false = 能力未装配）。
func (c *Coordinator) PendingMessageTail(location Location, sessionID string) (dto.PendingMessageTailReport, bool, error) {
	port, ok := c.pendingTailPort()
	if !ok {
		return dto.PendingMessageTailReport{}, false, nil
	}
	return port.PendingMessageTailWorkspace(location.WorkspaceID, sessionID)
}

// RecoverPendingMessageTail 显式恢复草稿尾部：基座一致才推进发布点，
// 返回发布后的报告（Status=recovered）；基座断裂返回 gap 且不发布、不清理。
//
// 幂等：发布凭据取草稿行自带的 commit_id（存储层实现），重复调用第二次即 clean。
func (c *Coordinator) RecoverPendingMessageTail(location Location, sessionID string) (dto.PendingMessageTailReport, bool, error) {
	port, ok := c.pendingTailPort()
	if !ok {
		return dto.PendingMessageTailReport{}, false, nil
	}
	return port.RecoverPendingMessageTailWorkspace(location.WorkspaceID, sessionID)
}

// DiscardPendingMessageTail 显式丢弃草稿尾部（清理未提交行）。
func (c *Coordinator) DiscardPendingMessageTail(location Location, sessionID string) (dto.PendingMessageTailReport, bool, error) {
	port, ok := c.pendingTailPort()
	if !ok {
		return dto.PendingMessageTailReport{}, false, nil
	}
	return port.DiscardPendingMessageTailWorkspace(location.WorkspaceID, sessionID)
}

// AppendDraftSnapshot 在会话单写者内追加 draft 后缀，避免与回合收尾快照并发。
func (c *Coordinator) AppendDraftSnapshot(location Location, sessionID string, events []model.TranscriptEvent) (int, bool, error) {
	port, ok := c.Core.Deps.Sessions.(SessionDraftSnapshotPort)
	if !ok {
		return 0, false, nil
	}
	var (
		appended  int
		supported bool
	)
	err := c.RunSessionWrite(sessionID, func() error {
		var inner error
		appended, supported, inner = port.AppendDraftSnapshotWorkspace(location.WorkspaceID, sessionID, events)
		return inner
	})
	return appended, supported, err
}
