// lifecycle 模块：draft / queue（引擎待发送队列）。
//
// 事实模型（my_design §4/§5.2）：
//   - draft ↔ queue 迁移在 lifecycle 模块内原子完成（I8）：D→入队时同一次
//     lifecycle 提交里 queue 含 D 且 draft 清空（T-LC-01）；
//   - 队列 FIFO：队首可发即发，否则等待；队列空才直发，队列非空时直发也
//     入队尾（T-LC-03/04）；
//   - 发送失败 → 内容回草稿；message.json 发布 = 发送成功与出队判定
//     （T-LC-05/06/08）；已发送未确认（崩溃）重启重发（T-LC-07）；
//   - 非法状态迁移拒绝（T-LC-09）。
package sessionstore

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// queueState 是队列条目状态机。
type queueState string

const (
	queueQueued  queueState = "queued"
	queueSending queueState = "sending"
	queueSent    queueState = "sent"
	queueFailed  queueState = "failed"
)

// draftItem 是输入草稿（state = 未发送/队列中/发送失败退回）。
type draftItem struct {
	Content string    `json:"content"`
	State   string    `json:"state,omitempty"`
	SavedAt time.Time `json:"saved_at"`
}

// queueItem 是引擎待发送队列条目。
type queueItem struct {
	ItemID    string         `json:"item_id"`
	RequestID string         `json:"request_id,omitempty"`
	Content   string         `json:"content"`
	State     queueState     `json:"state"`
	CreatedAt time.Time      `json:"created_at"`
	SendCount int            `json:"send_count,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}

// lifecycleHead 是 metadata/lifecycle.json payload。
type lifecycleHead struct {
	SessionID     string      `json:"session_id"`
	Draft         *draftItem  `json:"draft,omitempty"`
	Queue         []queueItem `json:"queue,omitempty"`
	DraftRevision uint64      `json:"draft_revision"`
	UpdatedAt     time.Time   `json:"updated_at"`
}

func (store *storeEngine) readLifecycleHeadLocked(key Key) (lifecycleHead, error) {
	return readModuleHeadPayload[lifecycleHead](store, key, moduleLifecycle)
}

func (store *storeEngine) readLifecycleHead(key Key) (lifecycleHead, error) {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	return store.readLifecycleHeadLocked(key)
}

// lifecycleHeadOrEmpty 读取 lifecycle head，缺失视为空（首写前调用）。
func (store *storeEngine) lifecycleHeadOrEmpty(key Key) (lifecycleHead, error) {
	head, err := store.readLifecycleHeadLocked(key)
	if errors.Is(err, fs.ErrNotExist) {
		return lifecycleHead{SessionID: key.SessionID}, nil
	}
	return head, err
}

// publishLifecycle 原子发布 lifecycle head 并镜像 draft/queue 数据文件。
func (store *storeEngine) publishLifecycleLocked(key Key, commitID string, head lifecycleHead) error {
	if _, err := store.ensureLayoutGuide(key); err != nil {
		return err
	}
	head.SessionID = key.SessionID
	head.UpdatedAt = time.Now().UTC()
	if _, err := store.publishModuleHead(key, moduleLifecycle, commitID, head, head.UpdatedAt); err != nil {
		return err
	}
	// 物理镜像（非权威）：input/draft.json + queue/queue.json(l)。
	root := store.sessionRoot(key)
	if err := os.MkdirAll(filepath.Join(root, "input"), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, "queue"), 0o700); err != nil {
		return err
	}
	if head.Draft != nil {
		data, err := json.Marshal(head.Draft)
		if err == nil {
			_ = writeAtomic(filepath.Join(root, "input", "draft.json"), data, 0o600)
		}
	} else {
		_ = os.Remove(filepath.Join(root, "input", "draft.json"))
	}
	buffer := make([]byte, 0, 1024)
	for _, item := range head.Queue {
		data, _ := json.Marshal(item)
		buffer = append(buffer, data...)
		buffer = append(buffer, '\n')
	}
	if len(buffer) > 0 {
		_ = os.WriteFile(filepath.Join(root, "queue", "queue.jsonl"), buffer, 0o600)
	} else {
		_ = os.Remove(filepath.Join(root, "queue", "queue.jsonl"))
	}
	return store.registerModule(key, moduleLifecycle)
}

// draftToQueue 草稿 D → 入队（同一次 lifecycle 提交：queue 含 D，draft
// 清空）。T-LC-01。
func (store *storeEngine) draftToQueue(key Key, content string) error {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	head, err := store.lifecycleHeadOrEmpty(key)
	if err != nil {
		return err
	}
	head.Draft = nil
	head.DraftRevision++
	head.Queue = append(head.Queue, queueItem{
		ItemID: randomID(), Content: content, State: queueQueued, CreatedAt: time.Now().UTC(),
	})
	return store.publishLifecycleLocked(key, "lc-"+randomID(), head)
}

// saveDraft 保存/更新草稿。
func (store *storeEngine) saveDraft(key Key, content string) error {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	head, err := store.lifecycleHeadOrEmpty(key)
	if err != nil {
		return err
	}
	head.Draft = &draftItem{Content: content, State: "未发送", SavedAt: time.Now().UTC()}
	head.DraftRevision++
	return store.publishLifecycleLocked(key, "lc-"+randomID(), head)
}

// draftDirectSend 草稿直发：
//   - 队列空 → 立即发送（不进队，T-LC-03）；
//   - 队列非空 → 入队尾（不插队，T-LC-04）。
func (store *storeEngine) draftDirectSend(key Key, content string) error {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	head, err := store.lifecycleHeadOrEmpty(key)
	if err != nil {
		return err
	}
	head.Draft = nil
	head.DraftRevision++
	if len(head.Queue) == 0 {
		return store.publishLifecycleLocked(key, "lc-"+randomID(), head)
	}
	head.Queue = append(head.Queue, queueItem{
		ItemID: randomID(), Content: content, State: queueQueued, CreatedAt: time.Now().UTC(),
	})
	return store.publishLifecycleLocked(key, "lc-"+randomID(), head)
}

// queueEnqueue 队尾入队（草稿直发失败/外部入队）。
func (store *storeEngine) queueEnqueue(key Key, requestID, content string) error {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	head, err := store.lifecycleHeadOrEmpty(key)
	if err != nil {
		return err
	}
	head.Queue = append(head.Queue, queueItem{
		ItemID: randomID(), RequestID: requestID, Content: content,
		State: queueQueued, CreatedAt: time.Now().UTC(),
	})
	return store.publishLifecycleLocked(key, "lc-"+randomID(), head)
}

// queueFront 返回队首（不出队）。
func (store *storeEngine) queueFront(key Key) (queueItem, bool) {
	head, err := store.readLifecycleHead(key)
	if err != nil || len(head.Queue) == 0 {
		return queueItem{}, false
	}
	return head.Queue[0], true
}

// queueSendFront 队首进入发送态（可发即发；Q1 阻塞时 Q2 不插队）。
func (store *storeEngine) queueSendFront(key Key) (queueItem, error) {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	head, err := store.lifecycleHeadOrEmpty(key)
	if err != nil {
		return queueItem{}, err
	}
	if len(head.Queue) == 0 {
		return queueItem{}, errors.New("session storage: queue empty")
	}
	item := head.Queue[0]
	if item.State == queueSending || item.State == queueSent {
		return queueItem{}, errors.New("session storage: queue head already sending")
	}
	item.State = queueSending
	item.SendCount++
	head.Queue[0] = item
	if err := store.publishLifecycleLocked(key, "lc-"+randomID(), head); err != nil {
		return queueItem{}, err
	}
	return item, nil
}

// queueConfirmSent 确认发送成功（message.json 已发布对应 request）后出队。
// 非法迁移 queued→sent（无发送记录）拒绝（T-LC-09）。
func (store *storeEngine) queueConfirmSent(key Key, requestID string) error {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	head, err := store.lifecycleHeadOrEmpty(key)
	if err != nil {
		return err
	}
	if len(head.Queue) == 0 {
		return errors.New("session storage: queue empty")
	}
	front := head.Queue[0]
	if front.State != queueSending && front.State != queueSent {
		return errors.New("session storage: illegal queued -> sent without send record")
	}
	if requestID != "" && front.RequestID != "" && front.RequestID != requestID {
		return errors.New("session storage: queue head request mismatch")
	}
	head.Queue = head.Queue[1:]
	return store.publishLifecycleLocked(key, "lc-"+randomID(), head)
}

// queueSendFailed 队首发送失败 → 内容回草稿，队列移除该项（T-LC-05）。
func (store *storeEngine) queueSendFailed(key Key) error {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	head, err := store.lifecycleHeadOrEmpty(key)
	if err != nil {
		return err
	}
	if len(head.Queue) == 0 {
		return errors.New("session storage: queue empty")
	}
	front := head.Queue[0]
	head.Queue = head.Queue[1:]
	head.Draft = &draftItem{Content: front.Content, State: "发送失败退回", SavedAt: time.Now().UTC()}
	head.DraftRevision++
	return store.publishLifecycleLocked(key, "lc-"+randomID(), head)
}

// directSendFailed 草稿直发失败 → 内容回草稿（T-LC-06）。
func (store *storeEngine) directSendFailed(key Key, content string) error {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	head, err := store.lifecycleHeadOrEmpty(key)
	if err != nil {
		return err
	}
	head.Draft = &draftItem{Content: content, State: "发送失败退回", SavedAt: time.Now().UTC()}
	head.DraftRevision++
	return store.publishLifecycleLocked(key, "lc-"+randomID(), head)
}

// queueRecover 重启恢复：已发送但 message.json 未发布的项回 queued 重发
// 一次（T-LC-07）；message.json 已发布的项直接出队（T-LC-08）。
func (store *storeEngine) queueRecover(key Key) ([]queueItem, error) {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	head, err := store.lifecycleHeadOrEmpty(key)
	if err != nil {
		return nil, err
	}
	// 读取 message head 中已发布 request（TaskID）集合。
	store.mu(key, moduleMessage).Lock()
	rows, err := store.readAllRowsLocked(key)
	store.mu(key, moduleMessage).Unlock()
	if err != nil {
		return nil, err
	}
	published := make(map[string]bool)
	for _, row := range rows {
		if row.TaskID != "" {
			published[row.TaskID] = true
		}
	}
	var resent []queueItem
	kept := head.Queue[:0]
	for _, item := range head.Queue {
		if item.RequestID != "" && published[item.RequestID] {
			continue // 已发布 = 最终确认，出队
		}
		if item.State == queueSending || item.State == queueSent {
			item.State = queueQueued
			resent = append(resent, item)
		}
		kept = append(kept, item)
	}
	head.Queue = kept
	if len(kept) == 0 {
		head.Queue = nil
	}
	if err := store.publishLifecycleLocked(key, "lc-"+randomID(), head); err != nil {
		return nil, err
	}
	return resent, nil
}

func (store *storeEngine) readAllRowsLocked(key Key) ([]Event, error) {
	return store.readRowsLocked(key, 1, 0)
}
