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
	"strings"
	"time"
)

// subagentInfo 是 subagent 栈条目的 payload（S18：条目落
// session/subagent/{active,history}.jsonl，head 只留水位）。
type subagentInfo struct {
	SubagentID  string    `json:"subagent_id"`
	Path        string    `json:"path"`
	Status      string    `json:"status"` // running|archived|merged
	ForkFromSeq uint64    `json:"fork_from_seq,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// registerSubagent 把子代理登记进父会话第四栈（§2.4/§8.2、S18）：一次派发
// 一条目（batch = dispatch:<subagent_id>），状态 running；全批（本批单条目）
// 完成后整批归档进 subagent/history.jsonl。
func (store *storeEngine) registerSubagent(key Key, operationID string, info subagentInfo) error {
	payload, err := json.Marshal(info)
	if err != nil {
		return err
	}
	journal := store.stackJournal()
	_, err = stackCommit(journal, key, StackKindSubagent, stackPushMessage(StackKindSubagent,
		"dispatch:"+operationID, []StackItemInput{{
			ItemID: subagentItemID(info.SubagentID), Kind: StackKindSubagent,
			Status: info.Status, Payload: payload,
		}}))
	return err
}

// updateSubagentStatus 更新子代理条目状态；批内全完成（单条目即整批）后整批
// 归档（S18）。
func (store *storeEngine) updateSubagentStatus(key Key, subagentID, status string) error {
	journal := store.stackJournal()
	_, err := stackCommit(journal, key, StackKindSubagent,
		stackSetStatusMessage(subagentItemID(subagentID), status))
	return err
}

func subagentItemID(subagentID string) string { return "subagent:" + subagentID }

// readSubagents 读取父会话子代理清单（active + 已归档，按 subagent 栈条目）。
func (store *storeEngine) readSubagents(key Key) ([]subagentInfo, error) {
	journal := store.stackJournal()
	active, err := stackReadActive(journal, key, StackKindSubagent)
	if err != nil {
		return nil, err
	}
	archived, err := stackReadHistory(journal, key, StackKindSubagent)
	if err != nil {
		return nil, err
	}
	rows := append(active, archived...)
	out := make([]subagentInfo, 0, len(rows))
	for _, row := range rows {
		var info subagentInfo
		if len(row.Payload) > 0 {
			if err := json.Unmarshal(row.Payload, &info); err != nil {
				return nil, err
			}
		}
		if info.SubagentID == "" {
			info.SubagentID = strings.TrimPrefix(row.ItemID, "subagent:")
		}
		out = append(out, info)
	}
	return out, nil
}

// forkCommitID / subagentCommitID 将「一次逻辑操作」映射成幂等凭据（§2.0 规则 4、
// D13）。凭据必须由操作身份推出而非存储层现造：随机 → 发布失败后的重放认不出
// 同一操作而留下重复行；常量 → 不同操作互判重复而整次丢弃。
func forkCommitID(childKey Key) string { return "fork-" + childKey.SessionID }

func subagentCommitID(subagentID string) string { return "subagent-" + subagentID }

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
	commitID := forkCommitID(childKey)
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
	// 栈快照：按 history 锚重建「该点」的栈（active 只含进入坐标 ≤ from 的条目，
	// 归档只含弹栈坐标 ≤ from 的条目，T-FK-02）。
	journal := store.stackJournal()
	store.dropSessionCaches(childKey)
	var snapshot []StackItemRecord
	for _, kind := range []StackKind{StackKindPlan, StackKindTask, StackKindGoal} {
		active, archived, err := stackSnapshotAt(journal, parentKey, kind, fromSeq)
		if err != nil {
			_ = os.RemoveAll(store.sessionRoot(childKey))
			return err
		}
		snapshot = append(snapshot, active...)
		if err := stackRestoreSnapshot(journal, childKey, kind, active, archived); err != nil {
			_ = os.RemoveAll(store.sessionRoot(childKey))
			return err
		}
	}
	// 父会话写 fork EVENT（审计/血缘）。
	payload, _ := json.Marshal(map[string]any{
		"child_id": childKey.SessionID, "child_kind": "fork_session",
		"from_message": lastMessageID(rows),
		"copy_range":   map[string]any{"from_seq": uint64(1), "to_seq": fromSeq},
		"stack_items":  len(snapshot),
	})
	_, _ = store.structuralEventCommit(parentKey, commitID, []structuralEvent{{
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
	childStore := newStoreEngine(subRoot, store.settings)
	childKey := Key{SessionID: subagentID}
	if childStore.sessionExists(childKey) {
		return nil, Key{}, errors.New("session storage: subagent session already exists")
	}
	rows, err := store.readRows(parentKey, 1, fromSeq)
	if err != nil {
		return nil, Key{}, err
	}
	commitID := subagentCommitID(subagentID)
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
	if _, err := childStore.structuralEventCommit(childKey, commitID, []structuralEvent{{
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
	if _, err := store.structuralEventCommit(parentKey, commitID, []structuralEvent{{
		Kind: structuralEventSubagent, AnchorSeq: fromSeq, AnchorMessageID: lastMessageID(rows), Payload: parentPayload,
	}}); err != nil {
		return nil, Key{}, err
	}
	if err := store.registerSubagent(parentKey, subagentID, info); err != nil {
		return nil, Key{}, err
	}
	return childStore, childKey, nil
}

// deleteSession 删除整个会话目录（Reset/归档语义；显式键路径）。目录删除后
// 会话级内存读投影必须作废，否则读者会继续看到已删除会话的栈投影。
func (store *storeEngine) deleteSession(key Key) error {
	root := store.sessionRoot(key)
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	defer store.dropSessionCaches(key)
	return removeAllWithBackoff(root)
}
