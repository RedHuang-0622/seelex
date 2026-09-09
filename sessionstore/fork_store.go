// fork（M3）：fork session 与 fork subagent。
//
// 事实模型（my_design §4 I7/§8）：
//   - fork 起点必须 ≥ LRU watermark；拷贝 [watermark, from_message_id] 实际
//     行；更早区间由摘要承接（显式报错，不静默截断，T-FK-03）；
//   - fork session = 独立会话，子会话建立自己的 metadata；队列与 draft 不
//     拷贝（T-FK-01）；
//   - fork subagent = session/subagent_<hash>/ 子树（metadata/message/event
//     同构），无独立 big_tool_result（复用主会话 blob，T-FK-05/06）。
package sessionstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// subagentInfo 是 metadata/subagent.json 清单条目。
type subagentInfo struct {
	SubagentID  string    `json:"subagent_id"`
	Path        string    `json:"path"`
	Status      string    `json:"status"` // running|archived|merged
	ForkFromSeq uint64    `json:"fork_from_seq,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type subagentHead struct {
	SessionID string         `json:"session_id"`
	Items     []subagentInfo `json:"items,omitempty"`
}

// registerSubagent 把子代理登记进父会话 subagent 模块。
func (store *storeEngine) registerSubagent(key Key, info subagentInfo) error {
	store.mu(key, moduleSubagent).Lock()
	defer store.mu(key, moduleSubagent).Unlock()
	head, err := readModuleHeadPayload[subagentHead](store, key, moduleSubagent)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if head.SessionID == "" {
		head.SessionID = key.SessionID
	}
	head.Items = append(head.Items, info)
	if _, err := store.ensureLayoutGuide(key); err != nil {
		return err
	}
	if _, err := store.publishModuleHead(key, moduleSubagent, "subagent-"+randomID(), head, time.Now().UTC()); err != nil {
		return err
	}
	return store.registerModule(key, moduleSubagent, store.modulePath(key, moduleSubagent))
}

// readSubagents 读取父会话子代理清单。
func (store *storeEngine) readSubagents(key Key) ([]subagentInfo, error) {
	store.mu(key, moduleSubagent).Lock()
	defer store.mu(key, moduleSubagent).Unlock()
	head, err := readModuleHeadPayload[subagentHead](store, key, moduleSubagent)
	if err != nil && errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return head.Items, nil
}

// forkSession 从父会话 fromSeq 深拷贝独立子会话（起点 ≥ watermark）。
func (store *storeEngine) forkSession(parentKey, childKey Key, fromSeq uint64) error {
	retention, err := store.readRetentionHead(parentKey)
	if err != nil {
		return err
	}
	if fromSeq < retention.WatermarkSeq {
		return ErrForkBeforeWatermark
	}
	if childKey.SessionID == parentKey.SessionID && childKey.ProjectID == parentKey.ProjectID {
		return errors.New("session storage: fork child must differ from parent")
	}
	if _, err := store.readMessageHead(childKey); err == nil && store.sessionExists(childKey) {
		return errors.New("session storage: fork child already exists")
	}
	rows, err := store.readRows(parentKey, 1, fromSeq)
	if err != nil {
		return err
	}
	commitID := "fork-" + randomID()
	if len(rows) > 0 {
		copied := make([]Event, len(rows))
		for index := range rows {
			copied[index] = rows[index]
			copied[index].CommitID = commitID
		}
		if _, err := store.messageCommit(childKey, commitID, copied); err != nil {
			_ = os.RemoveAll(store.sessionRoot(childKey))
			return err
		}
	} else if _, err := store.messageCommit(childKey, commitID, nil); err != nil {
		return err
	}
	// 栈快照：只保留锚点 ≤ from 的条目（history 重建语义）。
	stack, err := readModuleHeadPayload[stackHead](store, parentKey, moduleStack)
	if err == nil && len(stack.Items) > 0 {
		seqOf := make(map[string]uint64)
		for _, row := range rows {
			if row.MessageID != "" {
				seqOf[row.MessageID] = row.Seq
			}
		}
		items := make([]stackItem, 0, len(stack.Items))
		for _, item := range stack.Items {
			itemSeq, ok := seqOf[item.ItemMessageID]
			if ok && itemSeq <= fromSeq {
				items = append(items, item)
			}
		}
		childStack := stackHead{SessionID: childKey.SessionID, Items: items}
		if err := store.commitModuleHead(childKey, moduleStack, "fork-"+randomID(), childStack); err != nil {
			_ = os.RemoveAll(store.sessionRoot(childKey))
			return err
		}
	}
	// 父会话写 fork EVENT（审计/血缘）。
	payload, _ := json.Marshal(map[string]any{
		"child_id": childKey.SessionID, "child_kind": "fork_session",
		"from_message": lastMessageID(rows),
		"copy_range":   map[string]any{"from_seq": uint64(1), "to_seq": fromSeq},
	})
	_, _ = store.structuralEventCommit(parentKey, "fork-event", []structuralEvent{{
		Kind: structuralEventFork, AnchorSeq: fromSeq, AnchorMessageID: lastMessageID(rows), Payload: payload,
	}})
	return nil
}

func lastMessageID(rows []Event) string {
	if len(rows) == 0 {
		return ""
	}
	return rows[len(rows)-1].MessageID
}

func (store *storeEngine) sessionExists(key Key) bool {
	_, err := os.Stat(filepath.Join(store.sessionRoot(key), "metadata", "guide.json"))
	return err == nil
}

// subagentRoot 返回子代理子树根（主会话目录下 subagent_<hash>/）。
func (store *storeEngine) subagentRoot(parentKey Key, subagentID string) string {
	return filepath.Join(store.sessionRoot(parentKey), "subagent_"+hash(subagentID))
}

// newSubagent 创建子代理现场（同构子树 + 无独立 blob）。
func (store *storeEngine) newSubagent(parentKey Key, subagentID string, fromSeq uint64) (*storeEngine, Key, error) {
	retention, err := store.readRetentionHead(parentKey)
	if err != nil {
		return nil, Key{}, err
	}
	if fromSeq < retention.WatermarkSeq {
		return nil, Key{}, ErrForkBeforeWatermark
	}
	subRoot := store.subagentRoot(parentKey, subagentID)
	childStore := newStoreEngine(subRoot, store.shardRows)
	childKey := Key{SessionID: subagentID}
	if childStore.sessionExists(childKey) {
		return nil, Key{}, errors.New("session storage: subagent session already exists")
	}
	rows, err := store.readRows(parentKey, 1, fromSeq)
	if err != nil {
		return nil, Key{}, err
	}
	commitID := "fork-sub-" + randomID()
	if len(rows) > 0 {
		copied := make([]Event, len(rows))
		for index := range rows {
			copied[index] = rows[index]
			copied[index].CommitID = commitID
		}
		if _, err := childStore.messageCommit(childKey, commitID, copied); err != nil {
			_ = os.RemoveAll(subRoot)
			return nil, Key{}, err
		}
	} else if _, err := childStore.messageCommit(childKey, commitID, nil); err != nil {
		return nil, Key{}, err
	}
	// 子代理 event 目录落地（同构子树含 event）。
	payload, _ := json.Marshal(map[string]any{"parent": parentKey.SessionID, "from_seq": fromSeq})
	if _, err := childStore.structuralEventCommit(childKey, "created", []structuralEvent{{
		Kind: structuralEventSubagent, AnchorSeq: fromSeq, Payload: payload,
	}}); err != nil {
		_ = os.RemoveAll(subRoot)
		return nil, Key{}, err
	}
	info := subagentInfo{
		SubagentID: subagentID, Path: filepath.Base(subRoot), Status: "running",
		ForkFromSeq: fromSeq, CreatedAt: time.Now().UTC(),
	}
	// 主会话写 subagent EVENT（血缘/审计；子代理结束可归档）。
	parentPayload, _ := json.Marshal(map[string]any{
		"subagent_id": subagentID, "path": filepath.Base(subRoot),
		"from_seq": fromSeq, "status": "running",
	})
	if _, err := store.structuralEventCommit(parentKey, "subagent-"+randomID(), []structuralEvent{{
		Kind: structuralEventSubagent, AnchorSeq: fromSeq, AnchorMessageID: lastMessageID(rows), Payload: parentPayload,
	}}); err != nil {
		return nil, Key{}, err
	}
	if err := store.registerSubagent(parentKey, info); err != nil {
		return nil, Key{}, err
	}
	return childStore, childKey, nil
}

// deleteSession 删除整个 会话目录（Reset/归档语义；显式键路径）。
func (store *storeEngine) deleteSession(key Key) error {
	root := store.sessionRoot(key)
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return os.RemoveAll(root)
}
