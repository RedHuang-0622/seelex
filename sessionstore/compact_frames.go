// compact 模块：派生摘要帧（session/compact.jsonl）+ compact head。
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

// compactFrameRecord 是一行摘要帧。
type compactFrameRecord struct {
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

// compactHeadRecord 是 metadata/compact.json payload：最新帧水位。
type compactHeadRecord struct {
	SessionID     string `json:"session_id"`
	LastFrameID   string `json:"last_frame_id,omitempty"`
	LastMessageTo string `json:"last_message_to,omitempty"`
	LastSeq       uint64 `json:"last_message_to_seq"`
	FrameCount    int    `json:"frame_count"`
	// LatestFrame 是最近一帧的完整副本（head 冗余便于 R2 单读；append-only
	// 原文仍在 compact.jsonl）。
	LatestFrame *compactFrameRecord `json:"latest_frame,omitempty"`
}

func (store *storeEngine) compactFilePath(key Key) string {
	return filepath.Join(store.sessionRoot(key), "compact.jsonl")
}

// compactCommit 追加摘要帧并原子发布 compact head。frame.MessageToSeq
// 必须 ≤ message head.LastSeq（只能压缩已发布行）。
func (store *storeEngine) compactCommit(key Key, frame compactFrameRecord) (compactHeadRecord, error) {
	store.mu(key, moduleCompact).Lock()
	defer store.mu(key, moduleCompact).Unlock()
	if _, err := store.ensureLayoutGuide(key); err != nil {
		return compactHeadRecord{}, err
	}
	if frame.FrameID == "" {
		frame.FrameID = "compact-" + randomID()
	}
	if frame.CommitID == "" {
		frame.CommitID = randomID()
	}
	current, currentErr := store.readCompactHeadLocked(key)
	if currentErr != nil {
		return compactHeadRecord{}, currentErr
	}
	if current.LastFrameID == frame.FrameID {
		// 幂等：同一帧重复桥接不重复 append（同帧同水位视为已提交）。
		if current.LatestFrame != nil && current.LatestFrame.MessageToSeq == frame.MessageToSeq {
			return current, nil
		}
	}
	store.mu(key, moduleMessage).Lock()
	messageHead, err := store.readMessageHeadLocked(key)
	store.mu(key, moduleMessage).Unlock()
	if err != nil {
		return compactHeadRecord{}, err
	}
	if frame.MessageToSeq > messageHead.LastSeq {
		return compactHeadRecord{}, fmt.Errorf("session storage: compact frame message_to=%d beyond message head %d", frame.MessageToSeq, messageHead.LastSeq)
	}
	frameCount := 1
	if current.FrameCount > 0 {
		frameCount = current.FrameCount + 1
		if frame.PrevID == "" {
			frame.PrevID = current.LastFrameID
		}
	}
	head := compactHeadRecord{
		SessionID:     key.SessionID,
		LastFrameID:   frame.FrameID,
		LastMessageTo: frame.MessageTo,
		LastSeq:       frame.MessageToSeq,
		FrameCount:    frameCount,
		LatestFrame:   &frame,
	}
	// compact.jsonl 追加原文（崩溃残尾恢复语义与其它 JSONL 一致）。
	rows := []compactFrameRecord{frame}
	if err := appendCompactFrameRows(store.compactFilePath(key), rows); err != nil {
		return compactHeadRecord{}, err
	}
	if _, err := store.publishModuleHead(key, moduleCompact, frame.CommitID, head, time.Now().UTC()); err != nil {
		return compactHeadRecord{}, err
	}
	return head, store.registerModule(key, moduleCompact, store.modulePath(key, moduleCompact))
}

func (store *storeEngine) readCompactHeadLocked(key Key) (compactHeadRecord, error) {
	headFile, err := store.readModuleHeadFile(key, moduleCompact)
	if errors.Is(err, fs.ErrNotExist) {
		return compactHeadRecord{}, nil
	}
	if err != nil {
		return compactHeadRecord{}, err
	}
	return decodeHeadPayload[compactHeadRecord](headFile)
}

// readCompactHead 返回最新 compact head（缺失 = 零值，不报错）。
func (store *storeEngine) readCompactHead(key Key) (compactHeadRecord, error) {
	store.mu(key, moduleCompact).Lock()
	defer store.mu(key, moduleCompact).Unlock()
	return store.readCompactHeadLocked(key)
}

func appendCompactFrameRows(path string, rows []compactFrameRecord) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := truncateCrashTail(file); err != nil {
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
