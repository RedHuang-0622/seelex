// lifecycle 模块：draft / queue（引擎待发送队列）。
//
// 事实模型（my_design §2.5.1/§2.5.2 + §3.2 + §4）：
//   - **权威在数据文件**：草稿 = `session/input/draft.json`（ER DRAFT 一行），
//     队列 = `session/queue/queue.jsonl`（ER QUEUE 的 items，按序一行一条）；
//     `metadata/lifecycle.json` 只装水位（draft_revision / queue_count /
//     queue_head_seq / head_state），不参与事实（§2.0 规则 1）；
//   - draft ↔ queue 迁移在 lifecycle 模块内原子完成（I8）：同一次提交里 queue
//     含 D 且 draft 清空（T-LC-01），因此两者共用一把模块锁与一次发布；
//   - 队列 FIFO：队首可发即发，否则等待；队列空才直发，队列非空时直发也入队尾
//     （T-LC-03/04）；
//   - 发送失败 → 内容回草稿；message.json 发布 = 发送成功与出队判定
//     （T-LC-05/06/08）；已发送未确认（崩溃）重启重发（T-LC-07）；
//   - 非法状态迁移拒绝（T-LC-09）；
//   - 写序：数据文件（原子替换）→ 模块 head。head 落后于数据时按数据修补
//     （§2.0 规则 4 的恢复修补），队列条目不会因为 head 未发布而丢失。
package sessionstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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

// draftItem 是输入草稿（ER DRAFT 一行；state = 未发送/队列中/发送失败退回）。
type draftItem struct {
	SessionID string    `json:"session_id"`
	Content   string    `json:"content"`
	State     string    `json:"state,omitempty"`
	SavedAt   time.Time `json:"saved_at"`
}

// queueItem 是引擎待发送请求（ER QUEUE.items 的一行）。
type queueItem struct {
	Seq       uint64         `json:"seq"`
	ItemID    string         `json:"item_id"`
	RequestID string         `json:"request_id,omitempty"`
	Content   string         `json:"content"`
	State     queueState     `json:"state"`
	CreatedAt time.Time      `json:"created_at"`
	SendCount int            `json:"send_count,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}

// lifecycleState 是 draft/queue 的权威投影（来自数据文件，不是 head）。
type lifecycleState struct {
	Draft *draftItem
	Queue []queueItem
}

// lifecycleHead 是 metadata/lifecycle.json payload：只有水位与队首状态。
type lifecycleHead struct {
	SessionID     string     `json:"session_id"`
	DraftRevision uint64     `json:"draft_revision"`
	QueueCount    int        `json:"queue_count"`
	QueueHeadSeq  uint64     `json:"queue_head_seq"`
	HeadState     queueState `json:"head_state,omitempty"`
	SendingItemID string     `json:"sending_item_id,omitempty"`
	CommitID      string     `json:"commit_id,omitempty"`
	// ArchivedAt 是 §2.5.4 的已归档判据（列表过滤不看 EVENT）。
	ArchivedAt time.Time `json:"archived_at,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (store *storeEngine) lifecycleInputDir(key Key) string {
	return filepath.Join(store.sessionRoot(key), "input")
}

func (store *storeEngine) lifecycleQueueDir(key Key) string {
	return filepath.Join(store.sessionRoot(key), "queue")
}

func (store *storeEngine) lifecycleDraftPath(key Key) string {
	return filepath.Join(store.lifecycleInputDir(key), "draft.json")
}

func (store *storeEngine) lifecycleQueuePath(key Key) string {
	return filepath.Join(store.lifecycleQueueDir(key), "queue.jsonl")
}

// readLifecycleStateLocked 读权威投影（调用方持 lifecycle 模块锁）。
func (store *storeEngine) readLifecycleStateLocked(key Key) (lifecycleState, error) {
	var state lifecycleState
	data, err := os.ReadFile(store.lifecycleDraftPath(key))
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return state, err
	default:
		var draft draftItem
		if err := json.Unmarshal(data, &draft); err != nil {
			return state, errors.New("session storage: decode draft record")
		}
		state.Draft = &draft
	}
	rows, err := os.ReadFile(store.lifecycleQueuePath(key))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return state, err
	}
	for _, segment := range bytes.Split(rows, []byte{'\n'}) {
		segment = bytes.TrimSpace(segment)
		if len(segment) == 0 {
			continue
		}
		var item queueItem
		if err := json.Unmarshal(segment, &item); err != nil {
			continue // 崩溃残尾（未换行收尾）按未提交丢弃
		}
		state.Queue = append(state.Queue, item)
	}
	return state, nil
}

// readLifecycleState 返回权威投影，并在 head 落后于数据时修补水位。
func (store *storeEngine) readLifecycleState(key Key) (lifecycleState, error) {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	state, err := store.readLifecycleStateLocked(key)
	if err != nil {
		return state, err
	}
	head, err := store.readLifecycleHeadLocked(key)
	switch {
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return state, err
	case errors.Is(err, fs.ErrNotExist) && lifecycleStateEmpty(state):
		// 从未写过 lifecycle 的会话：读取不得留下任何文件。
		return state, nil
	}
	if !lifecycleHeadMatches(head, state) {
		if err := store.publishLifecycleLocked(key, "lc-repair-"+randomID(), state); err != nil {
			return state, err
		}
	}
	return state, nil
}

func lifecycleStateEmpty(state lifecycleState) bool {
	return state.Draft == nil && len(state.Queue) == 0
}

// lifecycleHeadMatches 判定 head 水位是否与权威数据一致（缺失即不一致）。
func lifecycleHeadMatches(head lifecycleHead, state lifecycleState) bool {
	return head.QueueCount == len(state.Queue) &&
		head.QueueHeadSeq == queueMaxSeq(state.Queue) &&
		head.HeadState == queueHeadState(state.Queue) &&
		head.DraftRevision != 0
}

func queueMaxSeq(items []queueItem) uint64 {
	var top uint64
	for _, item := range items {
		if item.Seq > top {
			top = item.Seq
		}
	}
	return top
}

func queueHeadState(items []queueItem) queueState {
	if len(items) == 0 {
		return ""
	}
	return items[0].State
}

// publishLifecycleLocked 提交一次 lifecycle 变更：先落权威数据（原子替换），
// 再发布水位 head。draft 清空 = 删除 draft.json，queue 清空 = 删除 queue.jsonl。
func (store *storeEngine) publishLifecycleLocked(key Key, _ string, state lifecycleState) error {
	if _, err := store.ensureLayoutGuide(key); err != nil {
		return err
	}
	if err := os.MkdirAll(store.lifecycleInputDir(key), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(store.lifecycleQueueDir(key), 0o700); err != nil {
		return err
	}
	if state.Draft == nil {
		if err := os.Remove(store.lifecycleDraftPath(key)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	} else {
		draft := *state.Draft
		draft.SessionID = key.SessionID
		data, err := json.Marshal(draft)
		if err != nil {
			return err
		}
		if err := writeAtomic(store.lifecycleDraftPath(key), data, 0o600); err != nil {
			return err
		}
	}
	if len(state.Queue) == 0 {
		if err := os.Remove(store.lifecycleQueuePath(key)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	} else {
		buffer := make([]byte, 0, len(state.Queue)*128)
		for _, item := range state.Queue {
			data, err := json.Marshal(item)
			if err != nil {
				return err
			}
			buffer = append(buffer, data...)
			buffer = append(buffer, '\n')
		}
		if err := writeAtomic(store.lifecycleQueuePath(key), buffer, 0o600); err != nil {
			return err
		}
	}
	previous, err := store.readLifecycleHeadLocked(key)
	draftRevision := uint64(1)
	if err == nil {
		draftRevision = previous.DraftRevision
		if draftRevision == 0 {
			draftRevision = 1
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if !lifecycleHeadMatches(previous, state) {
		draftRevision++
	}
	head := lifecycleHead{
		SessionID:     key.SessionID,
		DraftRevision: draftRevision,
		QueueCount:    len(state.Queue),
		QueueHeadSeq:  queueMaxSeq(state.Queue),
		HeadState:     queueHeadState(state.Queue),
		ArchivedAt:    previous.ArchivedAt,
		UpdatedAt:     time.Now().UTC(),
	}
	// S17/D13：凭据按落盘状态内容确定性推导（重放同一操作得到同一值），
	// 不采用调用侧随机号。
	commitID := modulePayloadCommitID(moduleLifecycle, head)
	head.CommitID = commitID
	if len(state.Queue) > 0 && state.Queue[0].State == queueSending {
		head.SendingItemID = state.Queue[0].ItemID
	}
	if _, err := store.publishModuleHead(key, moduleLifecycle, commitID, head, head.UpdatedAt); err != nil {
		return err
	}
	return nil
}

// readLifecycleHead 返回水位 head（缺失 = fs.ErrNotExist）。
func (store *storeEngine) readLifecycleHead(key Key) (lifecycleHead, error) {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	return store.readLifecycleHeadLocked(key)
}

// setLifecycleArchived 写入/清除 §2.5.4 的会话归档标记（S20：record 状态
// 通道退役后，已归档判据落在 lifecycle head）。
func (store *storeEngine) setLifecycleArchived(key Key, archived bool) error {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	if _, err := store.ensureLayoutGuide(key); err != nil {
		return err
	}
	head, err := store.readLifecycleHeadLocked(key)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	head.SessionID = key.SessionID
	if archived {
		head.ArchivedAt = time.Now().UTC()
	} else {
		head.ArchivedAt = time.Time{}
	}
	head.UpdatedAt = time.Now().UTC()
	_, err = store.publishModuleHead(key, moduleLifecycle,
		modulePayloadCommitID(moduleLifecycle, head), head, head.UpdatedAt)
	return err
}

// lifecycleArchivedAt 返回归档时间（未归档 = 零值）。
func (store *storeEngine) lifecycleArchivedAt(key Key) (time.Time, error) {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	head, err := store.readLifecycleHeadLocked(key)
	if errors.Is(err, fs.ErrNotExist) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return head.ArchivedAt, nil
}

func (store *storeEngine) readLifecycleHeadLocked(key Key) (lifecycleHead, error) {
	return readModuleHeadPayload[lifecycleHead](store, key, moduleLifecycle)
}

// ---------- 状态迁移（draft ↔ queue） ----------

// nextQueueSeq 返回新条目的 seq。
func nextQueueSeq(items []queueItem) uint64 {
	return queueMaxSeq(items) + 1
}

func newQueueItem(state lifecycleState, requestID, content string) queueItem {
	itemID := "q:" + requestID
	if requestID == "" {
		itemID = "q:" + hash(content+"|"+fmt.Sprintf("%d", nextQueueSeq(state.Queue)))
	}
	return queueItem{
		Seq: nextQueueSeq(state.Queue), ItemID: itemID, RequestID: requestID,
		Content: content, State: queueQueued, CreatedAt: time.Now().UTC(),
	}
}

// draftToQueue 草稿 D → 入队（同一次 lifecycle 提交：queue 含 D，draft
// 清空）。T-LC-01。
func (store *storeEngine) draftToQueue(key Key, content string) error {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	state, err := store.readLifecycleStateLocked(key)
	if err != nil {
		return err
	}
	state.Draft = nil
	state.Queue = append(state.Queue, newQueueItem(state, "", content))
	return store.publishLifecycleLocked(key, "lc-"+randomID(), state)
}

// saveDraft 保存/更新草稿。
func (store *storeEngine) saveDraft(key Key, content string) error {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	state, err := store.readLifecycleStateLocked(key)
	if err != nil {
		return err
	}
	state.Draft = &draftItem{
		SessionID: key.SessionID, Content: content, State: "未发送", SavedAt: time.Now().UTC(),
	}
	return store.publishLifecycleLocked(key, "lc-"+randomID(), state)
}

// draftDirectSend 草稿直发：
//   - 队列空 → 立即发送（不进队，T-LC-03）；
//   - 队列非空 → 入队尾（不插队，T-LC-04）。
func (store *storeEngine) draftDirectSend(key Key, content string) error {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	state, err := store.readLifecycleStateLocked(key)
	if err != nil {
		return err
	}
	state.Draft = nil
	if len(state.Queue) > 0 {
		state.Queue = append(state.Queue, newQueueItem(state, "", content))
	}
	return store.publishLifecycleLocked(key, "lc-"+randomID(), state)
}

// queueEnqueue 队尾入队（草稿直发失败/外部入队）。
func (store *storeEngine) queueEnqueue(key Key, requestID, content string) error {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	state, err := store.readLifecycleStateLocked(key)
	if err != nil {
		return err
	}
	for _, item := range state.Queue {
		if requestID != "" && item.RequestID == requestID {
			return nil // 同 requestID 重放幂等（S17/D10）
		}
	}
	state.Queue = append(state.Queue, newQueueItem(state, requestID, content))
	return store.publishLifecycleLocked(key, "lc-"+randomID(), state)
}

// queueFront 返回队首（不出队）。
func (store *storeEngine) queueFront(key Key) (queueItem, bool) {
	state, err := store.readLifecycleState(key)
	if err != nil || len(state.Queue) == 0 {
		return queueItem{}, false
	}
	return state.Queue[0], true
}

// queueSendFront 队首进入发送态（可发即发；Q1 阻塞时 Q2 不插队）。
func (store *storeEngine) queueSendFront(key Key) (queueItem, error) {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	state, err := store.readLifecycleStateLocked(key)
	if err != nil {
		return queueItem{}, err
	}
	if len(state.Queue) == 0 {
		return queueItem{}, errors.New("session storage: queue empty")
	}
	item := state.Queue[0]
	if item.State == queueSending || item.State == queueSent {
		return queueItem{}, errors.New("session storage: queue head already sending")
	}
	item.State = queueSending
	item.SendCount++
	state.Queue[0] = item
	if err := store.publishLifecycleLocked(key, "lc-"+randomID(), state); err != nil {
		return queueItem{}, err
	}
	return item, nil
}

// queueConfirmSent 确认发送成功（message.json 已发布对应 request）后出队。
// 非法迁移 queued→sent（无发送记录）拒绝（T-LC-09）。
func (store *storeEngine) queueConfirmSent(key Key, requestID string) error {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	state, err := store.readLifecycleStateLocked(key)
	if err != nil {
		return err
	}
	if len(state.Queue) == 0 {
		return errors.New("session storage: queue empty")
	}
	front := state.Queue[0]
	if front.State != queueSending && front.State != queueSent {
		return errors.New("session storage: illegal queued -> sent without send record")
	}
	if requestID != "" && front.RequestID != "" && front.RequestID != requestID {
		return errors.New("session storage: queue head request mismatch")
	}
	state.Queue = state.Queue[1:]
	return store.publishLifecycleLocked(key, "lc-"+randomID(), state)
}

// queueSendFailed 队首发送失败 → 内容回草稿，队列移除该项（T-LC-05）。
func (store *storeEngine) queueSendFailed(key Key) error {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	state, err := store.readLifecycleStateLocked(key)
	if err != nil {
		return err
	}
	if len(state.Queue) == 0 {
		return errors.New("session storage: queue empty")
	}
	front := state.Queue[0]
	state.Queue = state.Queue[1:]
	state.Draft = &draftItem{
		SessionID: key.SessionID, Content: front.Content,
		State: "发送失败退回", SavedAt: time.Now().UTC(),
	}
	return store.publishLifecycleLocked(key, "lc-"+randomID(), state)
}

// directSendFailed 草稿直发失败 → 内容回草稿（T-LC-06）。
func (store *storeEngine) directSendFailed(key Key, content string) error {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	state, err := store.readLifecycleStateLocked(key)
	if err != nil {
		return err
	}
	state.Draft = &draftItem{
		SessionID: key.SessionID, Content: content,
		State: "发送失败退回", SavedAt: time.Now().UTC(),
	}
	return store.publishLifecycleLocked(key, "lc-"+randomID(), state)
}

// queueRecover 重启恢复：已发送但 message.json 未发布的项回 queued 重发
// 一次（T-LC-07）；message.json 已发布的项直接出队（T-LC-08）。
func (store *storeEngine) queueRecover(key Key) ([]queueItem, error) {
	store.mu(key, moduleLifecycle).Lock()
	defer store.mu(key, moduleLifecycle).Unlock()
	state, err := store.readLifecycleStateLocked(key)
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
	kept := state.Queue[:0]
	for _, item := range state.Queue {
		if item.RequestID != "" && published[item.RequestID] {
			continue // 已发布 = 最终确认，出队
		}
		if item.State == queueSending || item.State == queueSent {
			item.State = queueQueued
			resent = append(resent, item)
		}
		kept = append(kept, item)
	}
	state.Queue = kept
	if len(kept) == 0 {
		state.Queue = nil
	}
	if err := store.publishLifecycleLocked(key, "lc-"+randomID(), state); err != nil {
		return nil, err
	}
	return resent, nil
}

func (store *storeEngine) readAllRowsLocked(key Key) ([]Event, error) {
	return store.readRowsLocked(key, 1, 0)
}
