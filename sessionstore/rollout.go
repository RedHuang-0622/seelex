// 会话 rollout 全序日志（session-order-log P2 垂直切片，JSON 后端）。
//
// 目标模型：每会话一条物理 append-only 的 rollout.jsonl 作为“发生顺序”的
// 事实源，类别混排、Ordinal 全局单调；state/history/transcript 逐步降级为
// 派生投影。P2 恢复改造最终从「最新压缩检查点后正序重放」重建内容与顺序。
//
// 本文件是第一批落地：rollout 条目/kind 契约 + JSONL 写读（崩溃残尾跳过、
// Ordinal 续写）+ JSON 后端 WriteCommit 双写接线（对话类事件与首条
// session_meta）。请求边界/usage/compacted 等 kind 与 resume 重放切换是
// 后续增量（见 docs/2026-09-08-session-rollout-p2/README.md）。
package sessionstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// rolloutLogFile 是会话 rollout 全序日志文件名（JSONL）。
const rolloutLogFile = "rollout.jsonl"

// ErrRolloutUnavailable 表示当前后端未实现 rollout 日志（回退旧通道读取）。
var ErrRolloutUnavailable = errors.New("session storage: rollout log unavailable for this backend")

// RolloutReader 是 rollout 全序日志的可选读取能力（JSON 后端实现；其它
// 后端未接入前返回 ErrRolloutUnavailable，上层回退旧三读）。
type RolloutReader interface {
	ReadRollout(context.Context, Key) ([]SessionLogEntry, error)
}

// SessionLogKind 是 rollout 条目类别（全序日志不分通道，统一按发生顺序混排）。
type SessionLogKind string

const (
	LogSessionMeta   SessionLogKind = "session_meta"
	LogUserInput     SessionLogKind = "user_input"
	LogInternalUser  SessionLogKind = "internal_user"
	LogAssistant     SessionLogKind = "assistant"
	LogReasoning     SessionLogKind = "reasoning"
	LogToolCall      SessionLogKind = "tool_call"
	LogToolOutput    SessionLogKind = "tool_output"
	LogRequestBegin  SessionLogKind = "request_begin"
	LogRequestEnd    SessionLogKind = "request_end"
	LogTurnBegin     SessionLogKind = "turn_begin"
	LogTurnEnd       SessionLogKind = "turn_end"
	LogWorldState    SessionLogKind = "world_state"
	LogContextChange SessionLogKind = "context_change"
	LogCompacted     SessionLogKind = "compacted"
	LogRolledBack    SessionLogKind = "rolled_back"
	LogTokenUsage    SessionLogKind = "token_usage"
	LogSubagent      SessionLogKind = "subagent"
)

// SessionLogEntry 是一条 rollout 条目。
// Ordinal 是会话内全局单调序号（append 顺序 = 发生顺序）；Seq 仅对话类
// 事件保留原 transcript Seq（UI/投影连续性），非对话事件不挤占 Seq。
type SessionLogEntry struct {
	Ordinal    uint64          `json:"ordinal"`
	Seq        uint64          `json:"seq,omitempty"`
	Kind       SessionLogKind  `json:"kind"`
	RequestID  string          `json:"request_id,omitempty"`
	TurnID     string          `json:"turn_id,omitempty"`
	MessageID  string          `json:"message_id,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	TokenCount int             `json:"token_count"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

// rolloutFingerprint 生成条目指纹（不含 CreatedAt/Ordinal），供幂等去重：
// 同一提交崩溃重试、重复持久化不会重复写生命周期条目。
func rolloutFingerprint(entry SessionLogEntry) string {
	var builder bytes.Buffer
	builder.WriteString(string(entry.Kind))
	builder.WriteByte(0)
	builder.WriteString(fmt.Sprintf("%d", entry.Seq))
	builder.WriteByte(0)
	builder.WriteString(entry.RequestID)
	builder.WriteByte(0)
	if entry.Payload != nil {
		builder.Write(entry.Payload)
	}
	sum := sha256.Sum256(builder.Bytes())
	return fmt.Sprintf("%x", sum)
}

// rolloutKindOf 把 transcript Event 映射为 rollout kind（显式类别优先，
// 与 EventKindOf 同源回退）。system 事件不在 rollout 对话区记录（由
// session_meta/派生投影承担）。
func rolloutKindOf(event Event) SessionLogKind {
	switch EventKindOf(event) {
	case EventKindUserInput:
		return LogUserInput
	case EventKindInternal:
		return LogInternalUser
	case EventKindLLM:
		return LogAssistant
	case EventKindToolCall:
		return LogToolCall
	case EventKindToolOutput:
		return LogToolOutput
	default:
		return SessionLogKind(event.Kind)
	}
}

// rolloutEntryFromEvent 生成对话类 rollout 条目（Payload 保留事件原文，
// 供重放重建 transcript/可见会话）。
func rolloutEntryFromEvent(ordinal uint64, event Event) (SessionLogEntry, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return SessionLogEntry{}, err
	}
	return SessionLogEntry{
		Ordinal:    ordinal,
		Seq:        event.Seq,
		Kind:       rolloutKindOf(event),
		RequestID:  event.TaskID,
		MessageID:  event.MessageID,
		CreatedAt:  event.CreatedAt,
		TokenCount: event.TokenCount,
		Payload:    payload,
	}, nil
}

// rolloutPath 返回会话 rollout 日志路径。
func (repository *jsonRepository) rolloutPath(key Key) string {
	return filepath.Join(repository.sessionDir(key), rolloutLogFile)
}

// ReadRollout 返回会话 rollout 全序日志（缺失 = 空）。
func (repository *jsonRepository) ReadRollout(_ context.Context, key Key) ([]SessionLogEntry, error) {
	if err := key.validate(); err != nil {
		return nil, err
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	entries, _, err := repository.readRolloutLocked(repository.sessionDir(key))
	return entries, err
}

// readRolloutLocked 读取完整 rollout（调用方持 repository.mu）。缺失视为
// 空；未换行收尾的半行按崩溃残尾跳过。返回 (entries, headOrdinal)。
func (repository *jsonRepository) readRolloutLocked(directory string) ([]SessionLogEntry, uint64, error) {
	data, err := os.ReadFile(filepath.Join(directory, rolloutLogFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	segments := bytes.Split(data, []byte{'\n'})
	entries := make([]SessionLogEntry, 0, len(segments))
	head := uint64(0)
	for index, segment := range segments {
		if index == len(segments)-1 && len(bytes.TrimSpace(segment)) > 0 {
			continue // 崩溃残尾
		}
		segment = bytes.TrimSpace(segment)
		if len(segment) == 0 {
			continue
		}
		var entry SessionLogEntry
		if err := json.Unmarshal(segment, &entry); err != nil {
			return nil, 0, fmt.Errorf("session storage: decode rollout log: %w", err)
		}
		entries = append(entries, entry)
		if entry.Ordinal > head {
			head = entry.Ordinal
		}
	}
	return entries, head, nil
}

// appendRolloutLocked 追加 rollout 条目并分配连续 Ordinal（调用方持
// repository.mu）。返回实际写入条目数。
func (repository *jsonRepository) appendRolloutLocked(directory string, entries []SessionLogEntry) (int, error) {
	if len(entries) == 0 {
		return 0, nil
	}
	path := filepath.Join(directory, rolloutLogFile)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return 0, err
	}
	if err := truncateRolloutCrashTail(file); err != nil {
		file.Close()
		return 0, err
	}
	buffer := make([]byte, 0, 4096)
	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			file.Close()
			return 0, err
		}
		buffer = append(buffer, data...)
		buffer = append(buffer, '\n')
	}
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		file.Close()
		return 0, err
	}
	if _, err := file.Write(buffer); err != nil {
		file.Close()
		return 0, err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return 0, err
	}
	if err := file.Close(); err != nil {
		return 0, err
	}
	return len(entries), nil
}

// truncateRolloutCrashTail 把文件末尾未换行收尾的半行截断，保证后续追加
// 从完整行边界开始（崩溃恢复语义：残尾视为未提交，直接续写不拼坏 JSON）。
func truncateRolloutCrashTail(file *os.File) error {
	stat, err := file.Stat()
	if err != nil {
		return err
	}
	if stat.Size() == 0 {
		return nil
	}
	last := make([]byte, 1)
	if _, err := file.ReadAt(last, stat.Size()-1); err != nil {
		return err
	}
	if last[0] == '\n' {
		return nil
	}
	// 从文件尾向前找最后一个 '\n'；找不到则整段视为残尾。
	const scanChunk = 1 << 20
	position := stat.Size()
	for position > 0 {
		start := position - scanChunk
		if start < 0 {
			start = 0
		}
		chunk := make([]byte, position-start)
		if _, err := file.ReadAt(chunk, start); err != nil {
			return err
		}
		if index := bytes.LastIndexByte(chunk, '\n'); index >= 0 {
			return file.Truncate(start + int64(index) + 1)
		}
		position = start
	}
	return file.Truncate(0)
}

// rolloutMetaEntry 生成首条 session_meta 条目。
func rolloutMetaEntry(ordinal uint64, key Key) (SessionLogEntry, error) {
	payload, err := json.Marshal(map[string]any{
		"session_id": key.SessionID,
		"schema":     "rollout-v1",
		"created_at": time.Now().UTC(),
	})
	if err != nil {
		return SessionLogEntry{}, err
	}
	return SessionLogEntry{
		Ordinal:   ordinal,
		Kind:      LogSessionMeta,
		CreatedAt: time.Now().UTC(),
		Payload:   payload,
	}, nil
}

// commitRolloutLocked 在 WriteCommit 内把本次提交追加进 rollout（旧通道
// 保留双写；rollout 失败则提交失败且 manifest 尚未切换，目录可视为未提交）。
// 写入顺序 = 生命周期 kind 单写点（request_begin/turn_begin → 对话事件 →
// request_end/turn_end/token_usage/compacted），全部按指纹幂等。调用方持
// repository.mu。
func (repository *jsonRepository) commitRolloutLocked(directory string, key Key, existing, merged []Event, state []byte) error {
	entries, head, err := repository.readRolloutLocked(directory)
	if err != nil {
		return err
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		seen[rolloutFingerprint(entry)] = true
	}
	var pending []SessionLogEntry
	appendPending := func(entry SessionLogEntry) {
		if seen[rolloutFingerprint(entry)] {
			return
		}
		head++
		entry.Ordinal = head
		pending = append(pending, entry)
		seen[rolloutFingerprint(entry)] = true
	}
	if head == 0 {
		meta, err := rolloutMetaEntry(1, key)
		if err != nil {
			return err
		}
		pending = append(pending, meta)
		head = 1
	}
	delta := eventsAfterLogHead(eventLogHead(existing), merged)
	lifecycle := rolloutLifecycleFromState(state)
	opened := make(map[string]bool)
	for _, event := range delta {
		if event.Role == "user" && event.TaskID != "" && !opened[event.TaskID] {
			opened[event.TaskID] = true
			for _, kind := range []SessionLogKind{LogRequestBegin, LogTurnBegin} {
				begin := SessionLogEntry{
					Kind: kind, RequestID: event.TaskID, CreatedAt: event.CreatedAt,
					Payload: mustJSON(map[string]any{"request_id": event.TaskID}),
				}
				appendPending(begin)
			}
		}
		entry, err := rolloutEntryFromEvent(head+1, event)
		if err != nil {
			return err
		}
		appendPending(entry)
	}
	for requestID := range opened {
		for _, kind := range []SessionLogKind{LogRequestEnd, LogTurnEnd} {
			end := SessionLogEntry{
				Kind: kind, RequestID: requestID, CreatedAt: time.Now().UTC(),
				Payload: mustJSON(map[string]any{"request_id": requestID}),
			}
			appendPending(end)
		}
	}
	for _, compaction := range lifecycle.Compactions {
		appendPending(SessionLogEntry{
			Kind:      LogCompacted,
			RequestID: lifecycle.RequestID,
			CreatedAt: compaction.CompactedAt,
			Payload:   mustJSON(compaction),
		})
	}
	if len(delta) > 0 {
		tokens := 0
		for _, event := range delta {
			tokens += event.TokenCount
		}
		appendPending(SessionLogEntry{
			Kind:       LogTokenUsage,
			RequestID:  lifecycle.RequestID,
			CreatedAt:  time.Now().UTC(),
			TokenCount: tokens,
			Payload:    mustJSON(map[string]any{"derived_events": len(delta), "tokens": tokens}),
		})
	}
	_, err = repository.appendRolloutLocked(directory, pending)
	return err
}

// rolloutLifecycleFromState 从 state blob 提取 compacted/request 元数据
// （opaque JSON 只解最小子树，不解析会话正文）。
func rolloutLifecycleFromState(state []byte) struct {
	RequestID   string
	Compactions []rolloutCompactionMeta
} {
	out := struct {
		RequestID   string
		Compactions []rolloutCompactionMeta
	}{}
	if len(state) == 0 {
		return out
	}
	var record struct {
		Execution struct {
			Task *struct {
				RequestID          string                  `json:"request_id,omitempty"`
				ContextCompactions []rolloutCompactionMeta `json:"context_compactions,omitempty"`
			} `json:"task,omitempty"`
		} `json:"execution,omitempty"`
	}
	if json.Unmarshal(state, &record) != nil || record.Execution.Task == nil {
		return out
	}
	out.RequestID = record.Execution.Task.RequestID
	out.Compactions = append(out.Compactions, record.Execution.Task.ContextCompactions...)
	return out
}

type rolloutCompactionMeta struct {
	Version         uint64    `json:"version"`
	Reason          string    `json:"reason,omitempty"`
	MessagesBefore  int       `json:"messages_before"`
	EstimatedTokens int       `json:"estimated_tokens"`
	CompactedAt     time.Time `json:"compacted_at"`
}

func mustJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return data
}
