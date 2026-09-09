// v8 compact 模块：派生摘要帧（session/compact.jsonl）+ compact head。
//
// 事实模型（my_design §4）：
//   - 一行 = 一次压缩覆盖 [message_m, message_n]（含端点）；写序 =
//     message 已发布 → append 摘要帧 → 原子替换 compact.json（I5）；
//   - head 携带最新帧（R2 只读最新 frame 与其 message_to 之后的 tail）；
//   - 淘汰联动（M4）：compact 超阈值 → 用户确认 → LRU 行删除并前移
//     retention watermark。
package sessionstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// v8CompactFrame 是一行摘要帧。
type v8CompactFrame struct {
	FrameID string `json:"frame_id"`
	PrevID  string `json:"prev_id,omitempty"`
	// MessageFrom/MessageTo 是 message 坐标（含端点）；Seq 同坐标的序号
	// 形式，R2 tail 从 MessageToSeq+1 开始。
	MessageFrom    string    `json:"message_from"`
	MessageTo      string    `json:"message_to"`
	MessageFromSeq uint64    `json:"message_from_seq"`
	MessageToSeq   uint64    `json:"message_to_seq"`
	Summary        string    `json:"summary"`
	BoundaryStatus string    `json:"boundary_status"` // complete|open
	CommitID       string    `json:"commit_id"`
	CompressedAt   time.Time `json:"compressed_at"`
}

// v8CompactHead 是 metadata/compact.json payload：最新帧水位。
type v8CompactHead struct {
	SessionID     string `json:"session_id"`
	LastFrameID   string `json:"last_frame_id,omitempty"`
	LastMessageTo string `json:"last_message_to,omitempty"`
	LastSeq       uint64 `json:"last_message_to_seq"`
	FrameCount    int    `json:"frame_count"`
	// LatestFrame 是最近一帧的完整副本（head 冗余便于 R2 单读；append-only
	// 原文仍在 compact.jsonl）。
	LatestFrame *v8CompactFrame `json:"latest_frame,omitempty"`
}

func (store *v8Store) v8CompactFilePath(key Key) string {
	return filepath.Join(store.v8SessionRoot(key), "compact.jsonl")
}

// v8CompactCommit 追加摘要帧并原子发布 compact head。frame.MessageToSeq
// 必须 ≤ message head.LastSeq（只能压缩已发布行）。
func (store *v8Store) v8CompactCommit(key Key, frame v8CompactFrame) (v8CompactHead, error) {
	store.compactMu.Lock()
	defer store.compactMu.Unlock()
	if _, err := store.ensureV8Guide(key); err != nil {
		return v8CompactHead{}, err
	}
	if frame.FrameID == "" {
		frame.FrameID = "compact-" + randomID()
	}
	if frame.CommitID == "" {
		frame.CommitID = randomID()
	}
	current, currentErr := store.v8ReadCompactHeadLocked(key)
	if currentErr != nil {
		return v8CompactHead{}, currentErr
	}
	if current.LastFrameID == frame.FrameID {
		// 幂等：同一帧重复桥接不重复 append（同帧同水位视为已提交）。
		if current.LatestFrame != nil && current.LatestFrame.MessageToSeq == frame.MessageToSeq {
			return current, nil
		}
	}
	store.messageMu.Lock()
	messageHead, err := store.v8ReadMessageHeadLocked(key)
	store.messageMu.Unlock()
	if err != nil {
		return v8CompactHead{}, err
	}
	if frame.MessageToSeq > messageHead.LastSeq {
		return v8CompactHead{}, fmt.Errorf("v8: compact frame message_to=%d beyond message head %d", frame.MessageToSeq, messageHead.LastSeq)
	}
	frameCount := 1
	if current.FrameCount > 0 {
		frameCount = current.FrameCount + 1
		if frame.PrevID == "" {
			frame.PrevID = current.LastFrameID
		}
	}
	head := v8CompactHead{
		SessionID:     key.SessionID,
		LastFrameID:   frame.FrameID,
		LastMessageTo: frame.MessageTo,
		LastSeq:       frame.MessageToSeq,
		FrameCount:    frameCount,
		LatestFrame:   &frame,
	}
	// compact.jsonl 追加原文（崩溃残尾恢复语义与其它 JSONL 一致）。
	rows := []v8CompactFrame{frame}
	if err := appendV8CompactRows(store.v8CompactFilePath(key), rows); err != nil {
		return v8CompactHead{}, err
	}
	if _, err := store.publishV8ModuleHead(key, v8ModuleCompact, frame.CommitID, head, time.Now().UTC()); err != nil {
		return v8CompactHead{}, err
	}
	return head, store.registerV8Module(key, v8ModuleCompact, store.v8ModulePath(key, v8ModuleCompact))
}

func (store *v8Store) v8ReadCompactHeadLocked(key Key) (v8CompactHead, error) {
	headFile, err := store.readV8ModuleHeadFile(key, v8ModuleCompact)
	if errors.Is(err, fs.ErrNotExist) {
		return v8CompactHead{}, nil
	}
	if err != nil {
		return v8CompactHead{}, err
	}
	return decodeV8HeadPayload[v8CompactHead](headFile)
}

// v8ReadCompactHead 返回最新 compact head（缺失 = 零值，不报错）。
func (store *v8Store) v8ReadCompactHead(key Key) (v8CompactHead, error) {
	store.compactMu.Lock()
	defer store.compactMu.Unlock()
	return store.v8ReadCompactHeadLocked(key)
}

func appendV8CompactRows(path string, rows []v8CompactFrame) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := truncateRolloutCrashTail(file); err != nil {
		file.Close()
		return err
	}
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	if _, err := file.Seek(stat.Size(), 0); err != nil {
		file.Close()
		return err
	}
	for _, row := range rows {
		data, err := json.Marshal(row)
		if err != nil {
			file.Close()
			return err
		}
		if _, err := file.Write(append(data, '\n')); err != nil {
			file.Close()
			return err
		}
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
