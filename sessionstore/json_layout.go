// jsonRepository 的 会话存储布局分派与适配（json_layout.go）。
//
// 布局判定（my_design §7）：
//   - 目录内存在 metadata/guide.json = 新布局；
//   - 目录内存在 manifest.json = 旧布局（rollout/manifest/generation/
//     transcript），只读兼容、不自动改写；
//   - 都不存在（新会话/record-only）→ 首次写按 v8 创建。
//
// 公开 Repository/Router 方法签名不变：会话在方法内部转接到 storeEngine
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

func newJSONRepositoryWithLayout(root string, shardSize int) (*jsonRepository, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("session storage: create JSON root: %w", err)
	}
	if shardSize <= 0 {
		shardSize = defaultMessageShardSize
	}
	repository := &jsonRepository{root: filepath.Clean(root), shardSize: shardSize, legacyCounts: make(map[string][]int)}
	repository.layout = newStoreEngine(root, shardSize)
	repository.attempts = NewAttemptCache(0, 0)
	return repository, nil
}

func (repository *jsonRepository) active(key Key) bool {
	return isLayoutSessionDir(repository.sessionDir(key))
}

// ---------- 写路径 ----------

// writeCommitLayout 是 WriteCommit 的 分支：
//   - commit.Events → message 事件行（append-only 正文事实源，增量去重）；
//   - commit.ProviderHistory → session/history.json 可替换缓存（只读兼容
//     Read/ReadRange 的"整段替换"语义；事件行为权威）；
//   - state/tool-results 沿用原通道文件（同会话目录）。
func (repository *jsonRepository) writeCommitLayout(key Key, commit Commit) error {
	if !repository.layout.sessionExists(key) {
		// 新会话先建 会话存储布局（空 head 发布，使目录可被枚举/读路径识别）。
		if _, err := repository.layout.messageCommit(key, "", nil); err != nil {
			return err
		}
	}
	if len(commit.Events) > 0 {
		if _, err := repository.layout.messageCommit(key, "", commit.Events); err != nil {
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
		if err := writeAtomic(filepath.Join(directory, historyCacheFile), data, 0o600); err != nil {
			return err
		}
	}
	state := commit.State
	if state != nil {
		if err := writeAtomic(filepath.Join(repository.sessionDir(key), "state.json"), state, 0o600); err != nil {
			return err
		}
	}
	refs, refsErr := repository.layout.readToolResultRefs(key)
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
		if err := repository.layout.publishToolResultRefs(key, refs); err != nil {
			return err
		}
	}
	return nil
}

// historyCacheFile 是 会话 provider history 缓存文件（可替换、可重建；
// message 事件行才是正文权威）。
const historyCacheFile = "history.json"

// toolResultHead 是 metadata/toolresult.json payload（refs 发布点）。
type toolResultHead struct {
	SessionID string   `json:"session_id"`
	Refs      []string `json:"refs,omitempty"`
}

// publishToolResultRefs 在全部结果文件写完后原子发布 refs 清单。
func (store *storeEngine) publishToolResultRefs(key Key, refs []string) error {
	store.mu(key, moduleToolResult).Lock()
	defer store.mu(key, moduleToolResult).Unlock()
	if _, err := store.ensureLayoutGuide(key); err != nil {
		return err
	}
	head := toolResultHead{SessionID: key.SessionID, Refs: refs}
	if _, err := store.publishModuleHead(key, moduleToolResult, "tool-"+randomID(), head, time.Now().UTC()); err != nil {
		return err
	}
	return store.registerModule(key, moduleToolResult, store.modulePath(key, moduleToolResult))
}

// readToolResultRefs 读取已发布 refs（缺失 = nil + fs.ErrNotExist）。
func (store *storeEngine) readToolResultRefs(key Key) ([]string, error) {
	head, err := readModuleHeadPayload[toolResultHead](store, key, moduleToolResult)
	if err != nil {
		return nil, err
	}
	return head.Refs, nil
}

// ---------- 运行期装配 API（R2 / compact / retention / lifecycle） ----------

// assembleWireWorkspace 对 会话执行 wire 装配（frame 摘要 + tail + 最近 K
// 条尝试）；非 会话存储布局返回 ok=false（上层回退旧装配）。
func (repository *jsonRepository) assembleWireWorkspace(key Key, budget, k int) ([]types.Message, bool, error) {
	if err := key.validate(); err != nil {
		return nil, false, err
	}
	params := wireParams{Budget: budget, K: k}
	if repository.active(key) {
		result, err := repository.layout.assembleWire(key, repository.attempts, params)
		if err != nil {
			return nil, false, err
		}
		return wireToTypesMessages(result.Messages), true, nil
	}
	// legacy JSON 会话：把 transcript 事件行视为 message 行，走同一 R2
	// 纯装配（取代旧恢复组装；无 compact 帧摘要，与旧行为一致）。
	rows, err := repository.readTranscriptEventsLocked(repository.sessionDir(key))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if _, statErr := os.Stat(filepath.Join(repository.sessionDir(key), "manifest.json")); errors.Is(statErr, fs.ErrNotExist) {
		return nil, false, nil
	}
	result := assembleWireRows(nil, rows, repository.attempts, params)
	return wireToTypesMessages(result.Messages), true, nil
}

// wireToTypesMessages 把 R2 wire 消息映射为 provider types.Message。
func wireToTypesMessages(wire []wireMessage) []types.Message {
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

// commitCompactFrameWorkspace 把运行期 compact 帧桥接进 compact 通道
// （frame 摘要进入 wire 装配；非 v8 或无法解析 seq 坐标时返回 ok=false）。
func (repository *jsonRepository) commitCompactFrameWorkspace(key Key, frame CompactFrame) (bool, error) {
	if err := key.validate(); err != nil {
		return false, err
	}
	if !repository.active(key) {
		return false, nil
	}
	fromSeq, toSeq, ok := repository.resolveCompactRange(key, frame)
	if !ok {
		return false, nil
	}
	record := compactFrameRecord{
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
	if record.CompressedAt.IsZero() {
		record.CompressedAt = time.Now().UTC()
	}
	if _, err := repository.layout.compactCommit(key, record); err != nil {
		return false, err
	}
	return true, nil
}

// resolveCompactRange 把 ChatQueue 单元索引 [frame.From, frame.To] 映射为
// message 事件行 seq（EventFrom/EventTo 已给定时直接使用）。
func (repository *jsonRepository) resolveCompactRange(key Key, frame CompactFrame) (uint64, uint64, bool) {
	if frame.EventFrom > 0 && frame.EventTo > 0 && frame.EventFrom <= frame.EventTo {
		return frame.EventFrom, frame.EventTo, true
	}
	rows, err := repository.layout.readAllRows(key)
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

// retentionAdvisoryWorkspace 返回 会话 retention 水位建议（compact 帧数
// vs 阈值、原始字节 vs 告警线；mode=manual 默认不自动删）。
func (repository *jsonRepository) retentionAdvisoryWorkspace(key Key) (RetentionAdvisory, error) {
	advisory := RetentionAdvisory{Layout: "legacy"}
	if !repository.active(key) {
		return advisory, nil
	}
	advisory.Layout = "session"
	compactHead, err := repository.layout.readCompactHead(key)
	if err != nil {
		return advisory, err
	}
	retention, err := repository.layout.readRetentionHead(key)
	if err != nil {
		return advisory, err
	}
	messageHead, err := repository.layout.readMessageHeadLocked(key)
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

func rawBytesEstimate(head messageHead, alert uint64) uint64 {
	if alert == 0 {
		return 0
	}
	// 以行数 * 平均 1KB 粗估；精确字节由 verify/巡检提供（M5）。
	return head.TotalRows * 1024
}

// lruDeleteWorkspace 用户确认后的 LRU 前缀删除（会话）。
func (repository *jsonRepository) lruDeleteWorkspace(key Key, upToSeq uint64, confirmed bool) (bool, error) {
	if !repository.active(key) {
		return false, nil
	}
	if _, err := repository.layout.lRUDelete(key, upToSeq, confirmed); err != nil {
		return false, err
	}
	return true, nil
}

// lifecycleRecoverWorkspace 重启恢复 lifecycle 队列（发送未确认项回
// queued；message 已发布项出队）。返回恢复条数。
func (repository *jsonRepository) lifecycleRecoverWorkspace(key Key) (int, bool, error) {
	if !repository.active(key) {
		return 0, false, nil
	}
	resent, err := repository.layout.queueRecover(key)
	if err != nil {
		return 0, true, err
	}
	return len(resent), true, nil
}

// messagesToEventRows 把 provider history（无事件通道的兼容写）转成事件行。
func messagesToEventRows(messages []types.Message) []Event {
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

// rowsToProviderMessages 只把完整协议单元行映射为 provider messages
// （孤儿 tool/internal/context 等不构成单元的行不进 provider 上下文，
// 与 legacy ReadEventTail 语义一致）。
func rowsToProviderMessages(rows []Event) []types.Message {
	units := CompleteEventUnits(rows)
	flat := make([]Event, 0, len(rows))
	for _, unit := range units {
		flat = append(flat, unit...)
	}
	return eventsToMessages(flat)
}

func (repository *jsonRepository) readAllLayoutMessages(key Key) ([]types.Message, error) {
	messages, err := repository.readHistoryCache(key)
	if err == nil {
		return messages, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		// provider history 缓存缺失 = 会话只有事件行（窗口/恢复通道）：
		// 全量读语义由缓存承载，返回空（与 legacy "message blob 缺失为空"
		// 一致）。
		if repository.layout.sessionExists(key) {
			return []types.Message{}, nil
		}
		return nil, fs.ErrNotExist
	}
	return nil, err
}

func (repository *jsonRepository) readHistoryCache(key Key) ([]types.Message, error) {
	data, err := os.ReadFile(filepath.Join(repository.sessionDir(key), historyCacheFile))
	if err != nil {
		return nil, err
	}
	var messages []types.Message
	if err := json.Unmarshal(data, &messages); err != nil {
		return nil, err
	}
	return messages, nil
}

func (repository *jsonRepository) readRangeLayout(key Key, offset, limit int) ([]types.Message, int, error) {
	if offset < 0 {
		return nil, 0, errors.New("session storage: invalid range")
	}
	if limit <= 0 {
		// total-only 语义（与 legacy ReadRange 一致）。
		total := 0
		if messages, err := repository.readHistoryCache(key); err == nil {
			total = len(messages)
		} else if rows, rowErr := repository.layout.readAllRows(key); rowErr == nil {
			total = len(rowsToProviderMessages(rows))
		} else {
			return nil, 0, rowErr
		}
		if offset > total {
			return nil, total, errors.New("session storage: range offset exceeds history")
		}
		return nil, total, nil
	}
	if messages, err := repository.readHistoryCache(key); err == nil {
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
	rows, err := repository.layout.readAllRows(key)
	if err != nil {
		return nil, 0, err
	}
	messages := rowsToProviderMessages(rows)
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

func (repository *jsonRepository) readEventTailLayout(key Key, tokenBudget, maxUnits int) ([]Event, error) {
	rows, err := repository.layout.readTailRowsForSelection(key, tokenBudget, maxUnits)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []Event{}, nil
		}
		return nil, err
	}
	return stripRowFields(selectEventTail(rows, tokenBudget, maxUnits)), nil
}

// stripRowFields 去掉 行扩展字段（commit_id/in_out_json/wire_material），
// 保持公开 Event 读取接口的旧契约（返回调用方当初提交的行）。
func stripRowFields(rows []Event) []Event {
	out := make([]Event, len(rows))
	for index, row := range rows {
		row.CommitID = ""
		row.InOutJSON = nil
		row.WireMaterial = false
		out[index] = row
	}
	return out
}

func (repository *jsonRepository) currentGenerationLayout(key Key) (string, error) {
	headFile, err := repository.layout.readModuleHeadFile(key, moduleMessage)
	if errors.Is(err, fs.ErrNotExist) {
		return "", fs.ErrNotExist
	}
	if err != nil {
		return "", err
	}
	if headFile.CommitID == "" {
		return "layout-0", nil
	}
	return "layout-" + headFile.CommitID, nil
}

// readMetaFromDir 读取 会话目录枚举 meta（message head.Meta；显式目录
// 路径，不经过 hash 回算）。
func readMetaFromDir(sessionRoot string) (frameworkStorage.SessionMeta, bool) {
	headPath := filepath.Join(sessionRoot, "metadata", "message.json")
	data, err := os.ReadFile(headPath)
	if err != nil {
		return frameworkStorage.SessionMeta{}, false
	}
	var envelope moduleHeadFile
	if json.Unmarshal(data, &envelope) != nil {
		return frameworkStorage.SessionMeta{}, false
	}
	head, err := decodeHeadPayload[messageHead](envelope)
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

// listMeta 返回项目内 会话的枚举 meta。
func (repository *jsonRepository) listMeta(projectID string) []frameworkStorage.SessionMeta {
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
		if meta, ok := readMetaFromDir(filepath.Join(repository.projectDir(projectID), entry.Name())); ok {
			result = append(result, meta)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UpdatedAt.After(result[j].UpdatedAt) })
	return result
}
