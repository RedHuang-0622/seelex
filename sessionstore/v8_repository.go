// jsonRepository 的 v8 布局分派与适配（v8_repository.go）。
//
// 布局判定（my_design §7）：
//   - 目录内存在 metadata/guide.json = v8 新布局；
//   - 目录内存在 manifest.json = 旧布局（rollout/manifest/generation/
//     transcript），只读兼容、不自动改写；
//   - 都不存在（新会话/record-only）→ 首次写按 v8 创建。
//
// 公开 Repository/Router 方法签名不变：v8 会话在方法内部转接到 v8Store
// 引擎（message 事件行 + 模块 head），旧会话继续走原实现。
package sessionstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	frameworkStorage "github.com/RedHuang-0622/Seele/seelectx/storage"
	"github.com/RedHuang-0622/Seele/types"
)

func newJSONRepositoryWithV8(root string, shardSize int, forceLegacy bool) (*jsonRepository, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("session storage: create JSON root: %w", err)
	}
	if shardSize <= 0 {
		shardSize = defaultMessageShardSize
	}
	repository := &jsonRepository{root: filepath.Clean(root), shardSize: shardSize, legacyCounts: make(map[string][]int), forceLegacy: forceLegacy}
	repository.v8 = newV8Store(root, shardSize)
	repository.v8Attempts = NewV8AttemptCache(0, 0)
	return repository, nil
}

// jsonRepository 新增字段：v8 引擎与测试专用 legacy 强制开关。
func (repository *jsonRepository) v8Active(key Key) bool {
	if repository.forceLegacy {
		return false
	}
	return isV8SessionDir(repository.sessionDir(key))
}

// v8Writable 判断本次写应走 v8 创建：目录无 manifest（新会话或已有 v8
// guide）。
func (repository *jsonRepository) v8Writable(key Key) bool {
	if repository.forceLegacy {
		return false
	}
	directory := repository.sessionDir(key)
	if _, err := os.Stat(filepath.Join(directory, "manifest.json")); err == nil {
		return false
	}
	return true
}

// ---------- 写路径 ----------

// writeCommitV8 是 WriteCommit 的 v8 分支：
//   - commit.Events → message 事件行（append-only 正文事实源，增量去重）；
//   - commit.ProviderHistory → session/history.json 可替换缓存（只读兼容
//     Read/ReadRange 的"整段替换"语义；事件行为权威）；
//   - state/tool-results 沿用原通道文件（同会话目录）。
func (repository *jsonRepository) writeCommitV8(key Key, commit Commit) error {
	if !repository.v8.v8SessionExists(key) {
		// 新会话先建 v8 布局（空 head 发布，使目录可被枚举/读路径识别）。
		if _, err := repository.v8.v8MessageCommit(key, "", nil); err != nil {
			return err
		}
	}
	if len(commit.Events) > 0 {
		if _, err := repository.v8.v8MessageCommit(key, "", commit.Events); err != nil {
			return err
		}
	}
	if commit.ProviderHistory != nil {
		directory := repository.sessionDir(key)
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return err
		}
		data, err := json.Marshal(commit.ProviderHistory)
		if err != nil {
			return err
		}
		if err := writeAtomic(filepath.Join(directory, v8HistoryCacheFile), data, 0o600); err != nil {
			return err
		}
	}
	state := commit.State
	if state != nil {
		if err := writeAtomic(filepath.Join(repository.sessionDir(key), "state.json"), state, 0o600); err != nil {
			return err
		}
	}
	refs, refsErr := repository.v8.v8ReadToolResultRefs(key)
	if refsErr != nil && !errors.Is(refsErr, fs.ErrNotExist) {
		return refsErr
	}
	if refs == nil {
		refs = []string{}
	}
	refsChanged := false
	for _, result := range commit.ToolResults {
		if err := repository.writeToolResultLocked(key, result); err != nil {
			return err
		}
		if !containsValue(refs, result.Ref) {
			refs = append(refs, result.Ref)
			refsChanged = true
		}
	}
	if refsChanged {
		if err := repository.v8.v8PublishToolResultRefs(key, refs); err != nil {
			return err
		}
	}
	return nil
}

// v8HistoryCacheFile 是 v8 会话 provider history 缓存文件（可替换、可重建；
// message 事件行才是正文权威）。
const v8HistoryCacheFile = "history.json"

// v8ToolResultHead 是 metadata/toolresult.json payload（refs 发布点）。
type v8ToolResultHead struct {
	SessionID string   `json:"session_id"`
	Refs      []string `json:"refs,omitempty"`
}

// v8PublishToolResultRefs 在全部结果文件写完后原子发布 refs 清单。
func (store *v8Store) v8PublishToolResultRefs(key Key, refs []string) error {
	store.toolMu.Lock()
	defer store.toolMu.Unlock()
	if _, err := store.ensureV8Guide(key); err != nil {
		return err
	}
	head := v8ToolResultHead{SessionID: key.SessionID, Refs: refs}
	if _, err := store.publishV8ModuleHead(key, v8ModuleToolResult, "tool-"+randomID(), head, time.Now().UTC()); err != nil {
		return err
	}
	return store.registerV8Module(key, v8ModuleToolResult, store.v8ModulePath(key, v8ModuleToolResult))
}

// v8ReadToolResultRefs 读取已发布 refs（缺失 = nil + fs.ErrNotExist）。
func (store *v8Store) v8ReadToolResultRefs(key Key) ([]string, error) {
	head, err := v8ReadModuleHeadPayload[v8ToolResultHead](store, key, v8ModuleToolResult)
	if err != nil {
		return nil, err
	}
	return head.Refs, nil
}

// ---------- 运行期装配 API（R2 / compact / retention / lifecycle） ----------

// assembleWireWorkspace 对 v8 会话执行 R2 装配（frame 摘要 + tail + 最近 K
// 条尝试）；非 v8 布局返回 ok=false（上层回退旧装配）。
func (repository *jsonRepository) assembleWireWorkspace(key Key, budget, k int) ([]types.Message, bool, error) {
	if err := key.validate(); err != nil {
		return nil, false, err
	}
	if !repository.v8Active(key) {
		return nil, false, nil
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	result, err := repository.v8.v8AssembleWire(key, repository.v8Attempts, v8R2Params{Budget: budget, K: k})
	if err != nil {
		return nil, false, err
	}
	return wireToTypesMessages(result.Messages), true, nil
}

// wireToTypesMessages 把 R2 wire 消息映射为 provider types.Message。
func wireToTypesMessages(wire []v8WireMessage) []types.Message {
	messages := make([]types.Message, 0, len(wire))
	for _, message := range wire {
		out := types.Message{Role: message.Role, Content: strPtrOrNil(message.Content)}
		for _, call := range message.ToolCalls {
			out.ToolCalls = append(out.ToolCalls, types.ToolCall{
				ID: call.ID, Type: "function", Function: types.ToolCallFunction{Name: call.Name, Arguments: call.Arguments},
			})
		}
		out.ToolCallID = message.ToolCallID
		out.Name = message.Name
		messages = append(messages, out)
	}
	return messages
}

// commitCompactFrameWorkspace 把运行期 compact 帧桥接进 v8 compact 通道
// （frame 摘要进入 R2 装配；非 v8 或无法解析 seq 坐标时返回 ok=false）。
func (repository *jsonRepository) commitCompactFrameWorkspace(key Key, frame CompactFrame) (bool, error) {
	if err := key.validate(); err != nil {
		return false, err
	}
	if !repository.v8Active(key) {
		return false, nil
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	fromSeq, toSeq, ok := repository.v8ResolveCompactRange(key, frame)
	if !ok {
		return false, nil
	}
	v8Frame := v8CompactFrame{
		FrameID:        frame.SegmentID,
		PrevID:         frame.PrevSegmentID,
		MessageFrom:    frame.MessageFrom,
		MessageTo:      frame.MessageTo,
		MessageFromSeq: fromSeq,
		MessageToSeq:   toSeq,
		Summary:        frame.Summary,
		BoundaryStatus: "complete",
		CompressedAt:   frame.CompressedAt,
	}
	if v8Frame.CompressedAt.IsZero() {
		v8Frame.CompressedAt = time.Now().UTC()
	}
	if _, err := repository.v8.v8CompactCommit(key, v8Frame); err != nil {
		return false, err
	}
	return true, nil
}

// v8ResolveCompactRange 把 ChatQueue 单元索引 [frame.From, frame.To] 映射为
// message 事件行 seq（EventFrom/EventTo 已给定时直接使用）。
func (repository *jsonRepository) v8ResolveCompactRange(key Key, frame CompactFrame) (uint64, uint64, bool) {
	if frame.EventFrom > 0 && frame.EventTo > 0 && frame.EventFrom <= frame.EventTo {
		return frame.EventFrom, frame.EventTo, true
	}
	rows, err := repository.v8.v8ReadAllRows(key)
	if err != nil {
		return 0, 0, false
	}
	// provider 可见消息顺序（完整协议单元行序）与 ChatQueue 单元索引对齐。
	units := CompleteEventUnits(rows)
	var visible []Event
	for _, unit := range units {
		visible = append(visible, unit...)
	}
	if frame.From < 0 || frame.To < frame.From || frame.To >= len(visible) {
		return 0, 0, false
	}
	return visible[frame.From].Seq, visible[frame.To].Seq, true
}

// retentionAdvisoryWorkspace 返回 v8 会话 retention 水位建议（compact 帧数
// vs 阈值、原始字节 vs 告警线；mode=manual 默认不自动删）。
func (repository *jsonRepository) retentionAdvisoryWorkspace(key Key) (V8RetentionAdvisory, error) {
	advisory := V8RetentionAdvisory{Layout: "legacy"}
	if !repository.v8Active(key) {
		return advisory, nil
	}
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	advisory.Layout = "v8"
	compactHead, err := repository.v8.v8ReadCompactHead(key)
	if err != nil {
		return advisory, err
	}
	retention, err := repository.v8.v8ReadRetentionHead(key)
	if err != nil {
		return advisory, err
	}
	messageHead, err := repository.v8.v8ReadMessageHeadLocked(key)
	if err != nil {
		return advisory, err
	}
	advisory.FrameCount = compactHead.FrameCount
	advisory.Threshold = retention.CompactThreshold
	advisory.RawBytes = rawBytesEstimate(messageHead, retention.RawBytesAlert)
	advisory.AlertBytes = retention.RawBytesAlert
	advisory.Mode = retention.Mode
	advisory.Eligible = compactHead.FrameCount >= retention.CompactThreshold || advisory.RawBytes >= retention.RawBytesAlert
	return advisory, nil
}

func rawBytesEstimate(head v8MessageHead, alert uint64) uint64 {
	if alert == 0 {
		return 0
	}
	// 以行数 * 平均 1KB 粗估；精确字节由 verify/巡检提供（M5）。
	return head.TotalRows * 1024
}

// lruDeleteWorkspace 用户确认后的 LRU 前缀删除（v8 会话）。
func (repository *jsonRepository) lruDeleteWorkspace(key Key, upToSeq uint64, confirmed bool) (bool, error) {
	if !repository.v8Active(key) {
		return false, nil
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if _, err := repository.v8.v8LRUDelete(key, upToSeq, confirmed); err != nil {
		return false, err
	}
	return true, nil
}

// lifecycleRecoverWorkspace 重启恢复 v8 lifecycle 队列（发送未确认项回
// queued；message 已发布项出队）。返回恢复条数。
func (repository *jsonRepository) lifecycleRecoverWorkspace(key Key) (int, bool, error) {
	if !repository.v8Active(key) {
		return 0, false, nil
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	resent, err := repository.v8.v8QueueRecover(key)
	if err != nil {
		return 0, true, err
	}
	return len(resent), true, nil
}

// messagesToV8Rows 把 provider history（无事件通道的兼容写）转成事件行。
func messagesToV8Rows(messages []types.Message) []Event {
	rows := make([]Event, 0, len(messages))
	for _, message := range messages {
		row := Event{
			Role:             message.Role,
			ReasoningContent: message.ReasoningContent,
			Content:          strOrEmpty(message.Content),
			ToolCallID:       message.ToolCallID,
			Name:             message.Name,
			CreatedAt:        time.Now().UTC(),
		}
		for _, call := range message.ToolCalls {
			row.ToolCalls = append(row.ToolCalls, EventToolCall{
				ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments,
			})
		}
		row.Kind = EventKindOf(row)
		rows = append(rows, row)
	}
	return rows
}

func strOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// ---------- 读路径 ----------

// v8RowsToProviderMessages 只把完整协议单元行映射为 provider messages
// （孤儿 tool/internal/context 等不构成单元的行不进 provider 上下文，
// 与 legacy ReadEventTail 语义一致）。
func v8RowsToProviderMessages(rows []Event) []types.Message {
	units := CompleteEventUnits(rows)
	flat := make([]Event, 0, len(rows))
	for _, unit := range units {
		flat = append(flat, unit...)
	}
	return eventsToMessages(flat)
}

func (repository *jsonRepository) readAllV8Messages(key Key) ([]types.Message, error) {
	messages, err := repository.readV8HistoryCache(key)
	if err == nil {
		return messages, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		// provider history 缓存缺失 = 会话只有事件行（窗口/恢复通道）：
		// 全量读语义由缓存承载，返回空（与 legacy "message blob 缺失为空"
		// 一致）。
		if repository.v8.v8SessionExists(key) {
			return []types.Message{}, nil
		}
		return nil, fs.ErrNotExist
	}
	return nil, err
}

func (repository *jsonRepository) readV8HistoryCache(key Key) ([]types.Message, error) {
	data, err := os.ReadFile(filepath.Join(repository.sessionDir(key), v8HistoryCacheFile))
	if err != nil {
		return nil, err
	}
	var messages []types.Message
	if err := json.Unmarshal(data, &messages); err != nil {
		return nil, err
	}
	return messages, nil
}

func (repository *jsonRepository) readRangeV8(key Key, offset, limit int) ([]types.Message, int, error) {
	if offset < 0 {
		return nil, 0, errors.New("session storage: invalid range")
	}
	if limit <= 0 {
		// total-only 语义（与 legacy ReadRange 一致）。
		total := 0
		if messages, err := repository.readV8HistoryCache(key); err == nil {
			total = len(messages)
		} else if rows, rowErr := repository.v8.v8ReadAllRows(key); rowErr == nil {
			total = len(v8RowsToProviderMessages(rows))
		} else {
			return nil, 0, rowErr
		}
		if offset > total {
			return nil, total, errors.New("session storage: range offset exceeds history")
		}
		return nil, total, nil
	}
	if messages, err := repository.readV8HistoryCache(key); err == nil {
		total := len(messages)
		if offset > total {
			return nil, total, errors.New("session storage: range offset exceeds history")
		}
		end := offset + limit
		if end > total {
			end = total
		}
		return append([]types.Message(nil), messages[offset:end]...), total, nil
	}
	rows, err := repository.v8.v8ReadAllRows(key)
	if err != nil {
		return nil, 0, err
	}
	messages := v8RowsToProviderMessages(rows)
	total := len(messages)
	if offset > total {
		return nil, total, errors.New("session storage: range offset exceeds history")
	}
	end := offset + limit
	if limit <= 0 || end > total {
		end = total
	}
	return append([]types.Message(nil), messages[offset:end]...), total, nil
}

func (repository *jsonRepository) readEventTailV8(key Key, tokenBudget, maxUnits int) ([]Event, error) {
	rows, err := repository.v8.v8ReadAllRows(key)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []Event{}, nil
		}
		return nil, err
	}
	return stripV8RowFields(selectEventTail(rows, tokenBudget, maxUnits)), nil
}

// stripV8RowFields 去掉 v8 行扩展字段（commit_id/in_out_json/wire_material），
// 保持公开 Event 读取接口的旧契约（返回调用方当初提交的行）。
func stripV8RowFields(rows []Event) []Event {
	out := make([]Event, len(rows))
	for index, row := range rows {
		row.CommitID = ""
		row.InOutJSON = nil
		row.WireMaterial = false
		out[index] = row
	}
	return out
}

func (repository *jsonRepository) currentGenerationV8(key Key) (string, error) {
	headFile, err := repository.v8.readV8ModuleHeadFile(key, v8ModuleMessage)
	if errors.Is(err, fs.ErrNotExist) {
		return "", fs.ErrNotExist
	}
	if err != nil {
		return "", err
	}
	if headFile.CommitID == "" {
		return "v8-0", nil
	}
	return "v8-" + headFile.CommitID, nil
}

// readV8MetaFromDir 读取 v8 会话目录枚举 meta（message head.Meta；显式目录
// 路径，不经过 hash 回算）。
func readV8MetaFromDir(sessionRoot string) (frameworkStorage.SessionMeta, bool) {
	headPath := filepath.Join(sessionRoot, "metadata", "message.json")
	data, err := os.ReadFile(headPath)
	if err != nil {
		return frameworkStorage.SessionMeta{}, false
	}
	var envelope v8ModuleHeadFile
	if json.Unmarshal(data, &envelope) != nil {
		return frameworkStorage.SessionMeta{}, false
	}
	head, err := decodeV8HeadPayload[v8MessageHead](envelope)
	if err != nil {
		return frameworkStorage.SessionMeta{}, false
	}
	meta := head.Meta
	if meta.SessionID == "" {
		meta.SessionID = head.SessionID
	}
	if meta.UpdatedAt.IsZero() {
		meta.UpdatedAt = envelope.UpdatedAt
	}
	return meta, true
}

// v8RolloutUnavailable 用于 v8 会话的 rollout 通道：返回 ErrRolloutUnavailable，
// 上层回退 message/event 读。
func (repository *jsonRepository) readRolloutV8(key Key) ([]SessionLogEntry, error) {
	return nil, ErrRolloutUnavailable
}

// v8ListMeta 返回项目内 v8 会话的枚举 meta。
func (repository *jsonRepository) v8ListMeta(projectID string) []frameworkStorage.SessionMeta {
	entries, err := os.ReadDir(repository.projectDir(projectID))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return nil
	}
	var result []frameworkStorage.SessionMeta
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if meta, ok := readV8MetaFromDir(filepath.Join(repository.projectDir(projectID), entry.Name())); ok {
			result = append(result, meta)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UpdatedAt.After(result[j].UpdatedAt) })
	return result
}
