package sessionstore

import (
	"errors"
	"time"
)

// lifecycle 队列的「消费凭据」协议（durable queue 写入侧接线，L4 层）。
//
// 为什么需要它：应用侧把**队列里所有待发送项合并成一条**提升为下一轮
// （`combineChatRequests` 用 "\n---\n" 拼接，chat.go 的收尾提升路径），并为
// 这一整批**新造一个 requestID**（= 该轮 message 行的 TaskID）。因此队列项的
// 身份与「轮」的身份不是一对一：N 项 → 1 轮。
//
// 朴素接线的后果（两种都不可接受）：
//   - 以 item.RequestID 确认 → 已发布行的 TaskID 与项 ID 无关 → 永远确认不了
//     → 重启后 queueRecover 把已发送项当"未确认"重发 = **重复输入**；
//   - 在提升时就出队 → 提升与 message 发布之间崩溃 → 输入**彻底丢失**。
//
// 本协议把「消费」显式记录在项上（`consumed_by = turn_id`），确认/失败/恢复
// 都以「该轮是否已发布」为唯一凭据（S17/D13：凭据来自既有事实，不现造）：
//
//	queueMarkConsumed(turnID)   提升时：全部待发项 → state=sent, consumed_by=turnID
//	queueConfirmConsumed(turnID) 该轮 message 已发布 → 消费项出队（幂等）
//	queueFailConsumed(turnID)    该轮未发布（失败）→ 内容回草稿 + 出队
//	queueRecover()               重启：consumed_by 的轮已发布 → 出队；
//	                             未发布 → 回 queued 重发（README「已发送未确认
//	                             重启后重发」），并清空 consumed_by
//
// 未消费项（consumed_by 为空）在任何路径下都保持原样：恢复不得误判用户还没
// 发出的输入为"已发送"。

// QueueItem 是 lifecycle 队列条目的对外只读投影（不含内部 payload）。
type QueueItem struct {
	Seq       uint64 `json:"seq"`
	ItemID    string `json:"item_id"`
	RequestID string `json:"request_id,omitempty"`
	// TurnID 是消费该项的那一轮 requestID（= 该轮 message 行的 TaskID）；
	// 空 = 尚未被任何一轮消费。
	TurnID    string    `json:"turn_id,omitempty"`
	Content   string    `json:"content"`
	State     string    `json:"state"`
	SendCount int       `json:"send_count,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// QueueRecoveryReport 是重启恢复的结果：Resent 是"已发送未确认"需重发的项，
// Dropped 是"该轮已发布"已出队的项，Pending 是恢复后仍在队列的项。
type QueueRecoveryReport struct {
	SessionID string      `json:"session_id"`
	Resent    []QueueItem `json:"resent,omitempty"`
	Dropped   []QueueItem `json:"dropped,omitempty"`
	Pending   []QueueItem `json:"pending,omitempty"`
}

func queueItemView(item queueItem) QueueItem {
	return QueueItem{
		Seq:       item.Seq,
		ItemID:    item.ItemID,
		RequestID: item.RequestID,
		TurnID:    item.ConsumedBy,
		Content:   item.Content,
		State:     string(item.State),
		SendCount: item.SendCount,
		CreatedAt: item.CreatedAt,
	}
}

func queueItemsView(items []queueItem) []QueueItem {
	if len(items) == 0 {
		return nil
	}
	views := make([]QueueItem, 0, len(items))
	for _, item := range items {
		views = append(views, queueItemView(item))
	}
	return views
}

// queueMarkConsumed 把当前全部待发送项标记为被 turnID 这一轮消费（提升批次
// 时调用）。已消费项（consumed_by 非空）保持原样——它们属于更早的未确认轮，
// 由恢复路径处理，不得被本轮"接管"。
func (store *storeEngine) queueMarkConsumed(key Key, turnID string) error {
	if turnID == "" {
		return errors.New("session storage: queue consume requires a turn id")
	}
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	state, err := store.readLifecycleStateLocked(key)
	if err != nil {
		return err
	}
	marked := false
	for index := range state.Queue {
		item := state.Queue[index]
		if item.State != queueQueued || item.ConsumedBy != "" {
			continue
		}
		item.State = queueSent
		item.ConsumedBy = turnID
		item.SendCount++
		state.Queue[index] = item
		marked = true
	}
	if !marked {
		return nil // 幂等：没有待发项就不提交
	}
	// 草稿与队列的原子迁移（README「先队列后草稿」）：提升即清草稿。
	state.Draft = nil
	return store.publishLifecycleLocked(key, "lc-"+randomID(), state)
}

// queueConfirmConsumed 该轮 message 已发布 → 该轮的消费项出队（幂等）。
func (store *storeEngine) queueConfirmConsumed(key Key, turnID string) error {
	if turnID == "" {
		return errors.New("session storage: queue confirm requires a turn id")
	}
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	state, err := store.readLifecycleStateLocked(key)
	if err != nil {
		return err
	}
	kept := make([]queueItem, 0, len(state.Queue))
	removed := false
	for _, item := range state.Queue {
		if item.ConsumedBy == turnID {
			removed = true
			continue
		}
		kept = append(kept, item)
	}
	if !removed {
		return nil
	}
	state.Queue = kept
	return store.publishLifecycleLocked(key, "lc-"+randomID(), state)
}

// queueFailConsumed 该轮未发布（失败）→ 该轮的消费项内容回草稿并出队
// （T-LC-05 的批次版：入队时"先队列后草稿"，失败时回到草稿）。
func (store *storeEngine) queueFailConsumed(key Key, turnID string) error {
	if turnID == "" {
		return errors.New("session storage: queue fail requires a turn id")
	}
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	state, err := store.readLifecycleStateLocked(key)
	if err != nil {
		return err
	}
	kept := make([]queueItem, 0, len(state.Queue))
	failed := make([]string, 0, len(state.Queue))
	for _, item := range state.Queue {
		if item.ConsumedBy == turnID {
			failed = append(failed, item.Content)
			continue
		}
		kept = append(kept, item)
	}
	if len(failed) == 0 {
		return nil
	}
	state.Queue = kept
	if len(kept) == 0 {
		state.Queue = nil
	}
	state.Draft = &draftItem{
		SessionID: key.SessionID,
		Content:   joinQueueContent(failed),
		State:     "发送失败退回",
		SavedAt:   time.Now().UTC(),
	}
	return store.publishLifecycleLocked(key, "lc-"+randomID(), state)
}

// queueItems 只读投影（UI/诊断用）。
func (store *storeEngine) queueItems(key Key) ([]queueItem, error) {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	state, err := store.readLifecycleStateLocked(key)
	if err != nil {
		return nil, err
	}
	return state.Queue, nil
}

// queueRecoverItems 重启恢复（返回条目而非仅计数）：把"已发送未确认"的项
// 交回调用方重发，把"该轮已发布"的项出队。
func (store *storeEngine) queueRecoverItems(key Key) (QueueRecoveryReport, error) {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	report := QueueRecoveryReport{SessionID: key.SessionID}
	resend, err := store.queueRecoverLocked(key, &report)
	if err != nil {
		return QueueRecoveryReport{}, err
	}
	report.Resent = queueItemsView(resend)
	return report, nil
}

// joinQueueContent 把失败批次的多个内容拼回一份草稿（与提升时的拼接口径
// 一致：separator 固定，便于用户可辨识地收回自己发过的内容）。
func joinQueueContent(contents []string) string {
	if len(contents) == 1 {
		return contents[0]
	}
	joined := ""
	for index, content := range contents {
		if index > 0 {
			joined += "\n---\n"
		}
		joined += content
	}
	return joined
}

// ---------- 仓库层（布局门控） ----------

// queueEnqueueWorkspace 把一条排队输入写入 lifecycle 队列（布局门控）。
func (repository *jsonRepository) queueEnqueueWorkspace(key Key, requestID, content string) (bool, error) {
	if !repository.active(key) {
		return false, nil
	}
	if err := repository.layout.queueEnqueue(key, requestID, content); err != nil {
		return true, err
	}
	return true, nil
}

func (repository *jsonRepository) queueMarkConsumedWorkspace(key Key, turnID string) (bool, error) {
	if !repository.active(key) {
		return false, nil
	}
	if err := repository.layout.queueMarkConsumed(key, turnID); err != nil {
		return true, err
	}
	return true, nil
}

func (repository *jsonRepository) queueConfirmConsumedWorkspace(key Key, turnID string) (bool, error) {
	if !repository.active(key) {
		return false, nil
	}
	if err := repository.layout.queueConfirmConsumed(key, turnID); err != nil {
		return true, err
	}
	return true, nil
}

func (repository *jsonRepository) queueFailConsumedWorkspace(key Key, turnID string) (bool, error) {
	if !repository.active(key) {
		return false, nil
	}
	if err := repository.layout.queueFailConsumed(key, turnID); err != nil {
		return true, err
	}
	return true, nil
}

func (repository *jsonRepository) queueItemsWorkspace(key Key) ([]QueueItem, bool, error) {
	if !repository.active(key) {
		return nil, false, nil
	}
	items, err := repository.layout.queueItems(key)
	if err != nil {
		return nil, true, err
	}
	return queueItemsView(items), true, nil
}

func (repository *jsonRepository) queueRecoverItemsWorkspace(key Key) (QueueRecoveryReport, bool, error) {
	if !repository.active(key) {
		return QueueRecoveryReport{}, false, nil
	}
	report, err := repository.layout.queueRecoverItems(key)
	if err != nil {
		return QueueRecoveryReport{}, true, err
	}
	return report, true, nil
}
