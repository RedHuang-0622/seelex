// v8 lifecycle 模块：draft / queue（引擎待发送队列）。
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

// v8QueueState 是队列条目状态机。
type v8QueueState string

const (
	v8QueueQueued  v8QueueState = "queued"
	v8QueueSending v8QueueState = "sending"
	v8QueueSent    v8QueueState = "sent"
	v8QueueFailed  v8QueueState = "failed"
)

// v8DraftItem 是输入草稿（state = 未发送/队列中/发送失败退回）。
type v8DraftItem struct {
	Content string    `json:"content"`
	State   string    `json:"state,omitempty"`
	SavedAt time.Time `json:"saved_at"`
}

// v8QueueItem 是引擎待发送队列条目。
type v8QueueItem struct {
	ItemID    string         `json:"item_id"`
	RequestID string         `json:"request_id,omitempty"`
	Content   string         `json:"content"`
	State     v8QueueState   `json:"state"`
	CreatedAt time.Time      `json:"created_at"`
	SendCount int            `json:"send_count,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}

// v8LifecycleHead 是 metadata/lifecycle.json payload。
type v8LifecycleHead struct {
	SessionID     string        `json:"session_id"`
	Draft         *v8DraftItem  `json:"draft,omitempty"`
	Queue         []v8QueueItem `json:"queue,omitempty"`
	DraftRevision uint64        `json:"draft_revision"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

func (store *v8Store) v8ReadLifecycleHeadLocked(key Key) (v8LifecycleHead, error) {
	return v8ReadModuleHeadPayload[v8LifecycleHead](store, key, v8ModuleLifecycle)
}

func (store *v8Store) v8ReadLifecycleHead(key Key) (v8LifecycleHead, error) {
	store.lifecycleMu.Lock()
	defer store.lifecycleMu.Unlock()
	return store.v8ReadLifecycleHeadLocked(key)
}

// v8LifecycleHeadOrEmpty 读取 lifecycle head，缺失视为空（首写前调用）。
func (store *v8Store) v8LifecycleHeadOrEmpty(key Key) (v8LifecycleHead, error) {
	head, err := store.v8ReadLifecycleHeadLocked(key)
	if errors.Is(err, fs.ErrNotExist) {
		return v8LifecycleHead{SessionID: key.SessionID}, nil
	}
	return head, err
}

// v8PublishLifecycle 原子发布 lifecycle head 并镜像 draft/queue 数据文件。
func (store *v8Store) v8PublishLifecycleLocked(key Key, commitID string, head v8LifecycleHead) error {
	if _, err := store.ensureV8Guide(key); err != nil {
		return err
	}
	head.SessionID = key.SessionID
	head.UpdatedAt = time.Now().UTC()
	if _, err := store.publishV8ModuleHead(key, v8ModuleLifecycle, commitID, head, head.UpdatedAt); err != nil {
		return err
	}
	// 物理镜像（非权威）：input/draft.json + queue/queue.json(l)。
	root := store.v8SessionRoot(key)
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
	return store.registerV8Module(key, v8ModuleLifecycle, store.v8ModulePath(key, v8ModuleLifecycle))
}

// v8DraftToQueue 草稿 D → 入队（同一次 lifecycle 提交：queue 含 D，draft
// 清空）。T-LC-01。
func (store *v8Store) v8DraftToQueue(key Key, content string) error {
	store.lifecycleMu.Lock()
	defer store.lifecycleMu.Unlock()
	head, err := store.v8LifecycleHeadOrEmpty(key)
	if err != nil {
		return err
	}
	head.Draft = nil
	head.DraftRevision++
	head.Queue = append(head.Queue, v8QueueItem{
		ItemID: randomID(), Content: content, State: v8QueueQueued, CreatedAt: time.Now().UTC(),
	})
	return store.v8PublishLifecycleLocked(key, "lc-"+randomID(), head)
}

// v8SaveDraft 保存/更新草稿。
func (store *v8Store) v8SaveDraft(key Key, content string) error {
	store.lifecycleMu.Lock()
	defer store.lifecycleMu.Unlock()
	head, err := store.v8LifecycleHeadOrEmpty(key)
	if err != nil {
		return err
	}
	head.Draft = &v8DraftItem{Content: content, State: "未发送", SavedAt: time.Now().UTC()}
	head.DraftRevision++
	return store.v8PublishLifecycleLocked(key, "lc-"+randomID(), head)
}

// v8DraftDirectSend 草稿直发：
//   - 队列空 → 立即发送（不进队，T-LC-03）；
//   - 队列非空 → 入队尾（不插队，T-LC-04）。
func (store *v8Store) v8DraftDirectSend(key Key, content string) error {
	store.lifecycleMu.Lock()
	defer store.lifecycleMu.Unlock()
	head, err := store.v8LifecycleHeadOrEmpty(key)
	if err != nil {
		return err
	}
	head.Draft = nil
	head.DraftRevision++
	if len(head.Queue) == 0 {
		return store.v8PublishLifecycleLocked(key, "lc-"+randomID(), head)
	}
	head.Queue = append(head.Queue, v8QueueItem{
		ItemID: randomID(), Content: content, State: v8QueueQueued, CreatedAt: time.Now().UTC(),
	})
	return store.v8PublishLifecycleLocked(key, "lc-"+randomID(), head)
}

// v8QueueEnqueue 队尾入队（草稿直发失败/外部入队）。
func (store *v8Store) v8QueueEnqueue(key Key, requestID, content string) error {
	store.lifecycleMu.Lock()
	defer store.lifecycleMu.Unlock()
	head, err := store.v8LifecycleHeadOrEmpty(key)
	if err != nil {
		return err
	}
	head.Queue = append(head.Queue, v8QueueItem{
		ItemID: randomID(), RequestID: requestID, Content: content,
		State: v8QueueQueued, CreatedAt: time.Now().UTC(),
	})
	return store.v8PublishLifecycleLocked(key, "lc-"+randomID(), head)
}

// v8QueueFront 返回队首（不出队）。
func (store *v8Store) v8QueueFront(key Key) (v8QueueItem, bool) {
	head, err := store.v8ReadLifecycleHead(key)
	if err != nil || len(head.Queue) == 0 {
		return v8QueueItem{}, false
	}
	return head.Queue[0], true
}

// v8QueueSendFront 队首进入发送态（可发即发；Q1 阻塞时 Q2 不插队）。
func (store *v8Store) v8QueueSendFront(key Key) (v8QueueItem, error) {
	store.lifecycleMu.Lock()
	defer store.lifecycleMu.Unlock()
	head, err := store.v8LifecycleHeadOrEmpty(key)
	if err != nil {
		return v8QueueItem{}, err
	}
	if len(head.Queue) == 0 {
		return v8QueueItem{}, errors.New("v8: queue empty")
	}
	item := head.Queue[0]
	if item.State == v8QueueSending || item.State == v8QueueSent {
		return v8QueueItem{}, errors.New("v8: queue head already sending")
	}
	item.State = v8QueueSending
	item.SendCount++
	head.Queue[0] = item
	if err := store.v8PublishLifecycleLocked(key, "lc-"+randomID(), head); err != nil {
		return v8QueueItem{}, err
	}
	return item, nil
}

// v8QueueConfirmSent 确认发送成功（message.json 已发布对应 request）后出队。
// 非法迁移 queued→sent（无发送记录）拒绝（T-LC-09）。
func (store *v8Store) v8QueueConfirmSent(key Key, requestID string) error {
	store.lifecycleMu.Lock()
	defer store.lifecycleMu.Unlock()
	head, err := store.v8LifecycleHeadOrEmpty(key)
	if err != nil {
		return err
	}
	if len(head.Queue) == 0 {
		return errors.New("v8: queue empty")
	}
	front := head.Queue[0]
	if front.State != v8QueueSending && front.State != v8QueueSent {
		return errors.New("v8: illegal queued -> sent without send record")
	}
	if requestID != "" && front.RequestID != "" && front.RequestID != requestID {
		return errors.New("v8: queue head request mismatch")
	}
	head.Queue = head.Queue[1:]
	return store.v8PublishLifecycleLocked(key, "lc-"+randomID(), head)
}

// v8QueueSendFailed 队首发送失败 → 内容回草稿，队列移除该项（T-LC-05）。
func (store *v8Store) v8QueueSendFailed(key Key) error {
	store.lifecycleMu.Lock()
	defer store.lifecycleMu.Unlock()
	head, err := store.v8LifecycleHeadOrEmpty(key)
	if err != nil {
		return err
	}
	if len(head.Queue) == 0 {
		return errors.New("v8: queue empty")
	}
	front := head.Queue[0]
	head.Queue = head.Queue[1:]
	head.Draft = &v8DraftItem{Content: front.Content, State: "发送失败退回", SavedAt: time.Now().UTC()}
	head.DraftRevision++
	return store.v8PublishLifecycleLocked(key, "lc-"+randomID(), head)
}

// v8DirectSendFailed 草稿直发失败 → 内容回草稿（T-LC-06）。
func (store *v8Store) v8DirectSendFailed(key Key, content string) error {
	store.lifecycleMu.Lock()
	defer store.lifecycleMu.Unlock()
	head, err := store.v8LifecycleHeadOrEmpty(key)
	if err != nil {
		return err
	}
	head.Draft = &v8DraftItem{Content: content, State: "发送失败退回", SavedAt: time.Now().UTC()}
	head.DraftRevision++
	return store.v8PublishLifecycleLocked(key, "lc-"+randomID(), head)
}

// v8QueueRecover 重启恢复：已发送但 message.json 未发布的项回 queued 重发
// 一次（T-LC-07）；message.json 已发布的项直接出队（T-LC-08）。
func (store *v8Store) v8QueueRecover(key Key) ([]v8QueueItem, error) {
	store.lifecycleMu.Lock()
	defer store.lifecycleMu.Unlock()
	head, err := store.v8LifecycleHeadOrEmpty(key)
	if err != nil {
		return nil, err
	}
	// 读取 message head 中已发布 request（TaskID）集合。
	store.messageMu.Lock()
	rows, err := store.v8ReadAllRowsLocked(key)
	store.messageMu.Unlock()
	if err != nil {
		return nil, err
	}
	published := make(map[string]bool)
	for _, row := range rows {
		if row.TaskID != "" {
			published[row.TaskID] = true
		}
	}
	var resent []v8QueueItem
	kept := head.Queue[:0]
	for _, item := range head.Queue {
		if item.RequestID != "" && published[item.RequestID] {
			continue // 已发布 = 最终确认，出队
		}
		if item.State == v8QueueSending || item.State == v8QueueSent {
			item.State = v8QueueQueued
			resent = append(resent, item)
		}
		kept = append(kept, item)
	}
	head.Queue = kept
	if len(kept) == 0 {
		head.Queue = nil
	}
	if err := store.v8PublishLifecycleLocked(key, "lc-"+randomID(), head); err != nil {
		return nil, err
	}
	return resent, nil
}

func (store *v8Store) v8ReadAllRowsLocked(key Key) ([]Event, error) {
	return store.v8ReadRowsLocked(key, 1, 0)
}
