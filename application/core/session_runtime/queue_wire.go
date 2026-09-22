package session_runtime

import (
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// durable queue（lifecycle 队列）在会话域的窄端口面。
//
// 语义（见 sessionstore/queue_consume.go 的凭据协议）：应用侧把队列中**全部**
// 待发项合并成一条提升为下一轮，因此"确认/失败/恢复"都以**该轮是否已发布**
// 为凭据，而不是以队列项自己的 request_id。
//
// 所有方法都是**可选能力**：存储未装配（非 v8 布局）时一律 no-op / ok=false，
// 应用侧行为与没有 durable queue 时完全一致（队列语义是纯增强，不改变本轮仍
// 走得通的路径）。
type SessionQueuePort interface {
	// QueueEnqueueWorkspace 把排队输入镜像落盘（先队列后草稿）。
	QueueEnqueueWorkspace(projectID, sessionID, requestID, content string) error
	// QueueMarkConsumedWorkspace 提升批次时把全部待发项标记为该轮消费。
	QueueMarkConsumedWorkspace(projectID, sessionID, turnID string) error
	// QueueConfirmConsumedWorkspace 该轮已发布 → 消费项出队。
	QueueConfirmConsumedWorkspace(projectID, sessionID, turnID string) error
	// QueueFailConsumedWorkspace 该轮未发布 → 消费项内容回草稿。
	QueueFailConsumedWorkspace(projectID, sessionID, turnID string) error
	// QueueRecoverItemsWorkspace 重启恢复（条目级结果）。
	QueueRecoverItemsWorkspace(projectID, sessionID string) (sessionstore.QueueRecoveryReport, bool, error)
}

// queuePort 解析存储侧队列能力（未装配时 ok=false）。
func (c *Coordinator) queuePort() (SessionQueuePort, bool) {
	if c == nil || c.Core == nil {
		return nil, false
	}
	port, ok := c.Core.Deps.Sessions.(SessionQueuePort)
	if !ok {
		return nil, false
	}
	return port, true
}

// QueueEnqueueInput 把一条排队输入镜像到 durable queue（失败不回滚内存队列：
// durable queue 是崩溃兜底，不是权威；权威始终是会话域队列）。
func (c *Coordinator) QueueEnqueueInput(location Location, sessionID, content string) error {
	port, ok := c.queuePort()
	if !ok {
		return nil
	}
	return port.QueueEnqueueWorkspace(location.WorkspaceID, sessionID, "", content)
}

// QueueMarkConsumedInput 提升批次为下一轮时标记消费。
func (c *Coordinator) QueueMarkConsumedInput(location Location, sessionID, turnID string) error {
	port, ok := c.queuePort()
	if !ok || turnID == "" {
		return nil
	}
	return port.QueueMarkConsumedWorkspace(location.WorkspaceID, sessionID, turnID)
}

// QueueConfirmConsumedInput 该轮快照已发布 → 消费项出队。
func (c *Coordinator) QueueConfirmConsumedInput(location Location, sessionID, turnID string) error {
	port, ok := c.queuePort()
	if !ok || turnID == "" {
		return nil
	}
	return port.QueueConfirmConsumedWorkspace(location.WorkspaceID, sessionID, turnID)
}

// QueueFailConsumedInput 该轮未发布（失败）→ 消费项内容回草稿。
func (c *Coordinator) QueueFailConsumedInput(location Location, sessionID, turnID string) error {
	port, ok := c.queuePort()
	if !ok || turnID == "" {
		return nil
	}
	return port.QueueFailConsumedWorkspace(location.WorkspaceID, sessionID, turnID)
}

// QueueRecoverInputs 重启恢复生命周期的待发项：返回"已发送未确认需重发"的
// 输入正文（顺序保持队列顺序）。ok=false = 存储未装配/非 v8（正常空操作）。
func (c *Coordinator) QueueRecoverInputs(location Location, sessionID string) ([]sessionstore.QueueItem, bool, error) {
	port, ok := c.queuePort()
	if !ok {
		return nil, false, nil
	}
	report, ok, err := port.QueueRecoverItemsWorkspace(location.WorkspaceID, sessionID)
	if err != nil || !ok {
		return nil, ok, err
	}
	return report.Resent, true, nil
}
