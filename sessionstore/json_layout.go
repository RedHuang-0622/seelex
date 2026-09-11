// jsonRepository 的 会话存储布局分派与适配（json_layout.go）。
//
// 布局判定（my_design §7）：
//   - 目录内存在 metadata/guide.json = 新布局；
//   - 只含 manifest.json 的旧布局目录（rollout/manifest/generation/
//     transcript）**彻底退役（D2/S12）**：读路径不再判定、不再打开，
//     目录枚举跳过（内容保留在磁盘）；都不存在 → 首次写按 v8 创建。
//
// 公开 Repository/Router 方法签名不变：会话在方法内部转接到 storeEngine
// 引擎（message 事件行 + 模块 head）。
package sessionstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	frameworkStorage "github.com/RedHuang-0622/Seele/seelectx/storage"
	"github.com/RedHuang-0622/Seele/types"
)

func newJSONRepositoryWithLayout(root string, settings storageSettings) (*jsonRepository, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("session storage: create JSON root: %w", err)
	}
	resolved := resolveStorageSettings(settings)
	// §9 数据根独占锁：单数据根 = 单进程写者（同一进程内按引用计数共享；
	// 跨进程被占用/陈旧按 D11 立即报错，不等待、不接管）。
	if err := acquireDataRootLock(root, resolved); err != nil {
		return nil, err
	}
	repository := &jsonRepository{
		root: filepath.Clean(root), settings: resolved,
	}
	repository.layout = newStoreEngine(root, resolved)
	repository.attempts = NewAttemptCache(resolved.RetryCacheMaxItems, resolved.RetryCacheMaxChars)
	return repository, nil
}

func (repository *jsonRepository) active(key Key) bool {
	return isLayoutSessionDir(repository.sessionDir(key))
}

// ---------- 写路径 ----------

// writeCommitLayout 是 WriteCommit 的 分支：
//   - commit.ToolResults → big_tool_result 单通道（S13/D9：旧 tool-results/
//     metadata/toolresult.json 链路退役）。写序 = 结果文件 → 可重建 refs
//     索引（metadata-index/toolresult.json）→ message 事件行，保证任何可见
//     行引用的结果与索引都已落盘；
//   - commit.Events → message 事件行（append-only 正文事实源，增量去重）；
//   - commit.ProviderHistory 随 history.json 一起退役（D9/S11）：v8 布局里
//     message 事件行是唯一正文事实源，整段替换式 provider 历史不再落盘
//     （append-only 行不允许整段替换；dev 阶段丢字段已接受，生产写路径
//     以 Events 为准）；
//   - state 沿用原通道文件（同会话目录）。
func (repository *jsonRepository) writeCommitLayout(key Key, commit Commit) error {
	if !repository.layout.sessionExists(key) {
		// 新会话先建 会话存储布局（空 head 发布，使目录可被枚举/读路径识别）。
		if _, err := repository.layout.messageCommit(key, "", nil); err != nil {
			return err
		}
	}
	// 1) 结果文件先进 big_tool_result（事件行可见之前）。
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
	// 2) 事件行（发布点：行可见 ⇒ 结果文件与 refs 索引已存在）。
	if len(commit.Events) > 0 {
		if _, err := repository.layout.messageCommit(key, "", commit.Events); err != nil {
			return err
		}
	}
	// D9/S20：state.json 通道停写（SESSION 无独立文件；可见状态按 §2.5.4
	// 从 message head.Meta + lifecycle 派生）。
	return nil
}

// toolResultHead 是 metadata-index/toolresult.json payload（可重建派生索引，
// §2.0 规则 5：refs 清单可从 big_tool_result 目录重扫重建；不属 metadata
// 白名单，也不是 blob 内容的发布点）。
type toolResultHead struct {
	SessionID string   `json:"session_id"`
	Refs      []string `json:"refs,omitempty"`
}

// publishToolResultRefs 在全部结果文件写完后原子发布 refs 索引（S13：不再
// 写 metadata/toolresult.json 模块 head，也不再使用随机 commit_id）。
func (store *storeEngine) publishToolResultRefs(key Key, refs []string) error {
	store.locks(key).toolRefsMu.Lock()
	defer store.locks(key).toolRefsMu.Unlock()
	head := toolResultHead{SessionID: key.SessionID, Refs: refs}
	data, err := json.MarshalIndent(head, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(store.sessionRoot(key), "metadata-index", "toolresult.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeAtomic(path, data, 0o600)
}

// readToolResultRefs 读取 refs 索引（缺失 = nil + fs.ErrNotExist；索引丢失时
// 由下一次带结果的提交重建，读侧不目录扫描以免撕裂）。
func (store *storeEngine) readToolResultRefs(key Key) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(store.sessionRoot(key), "metadata-index", "toolresult.json"))
	if err != nil {
		return nil, err
	}
	var head toolResultHead
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, err
	}
	return head.Refs, nil
}

// checkpointContent 是 metadata/checkpoint.json 的 payload（§2.5.5/S24：
// 引擎续跑快照整份替换，latest-wins）。
type checkpointContent struct {
	SessionID string `json:"session_id"`
	Payload   []byte `json:"payload"`
}

// systemPromptSnapshot 是 metadata/system.json 的 payload（§2.1/S19：会话
// 初始化时写入当次生效 prompt + 来源版本，之后只读；fork 子会话自包含）。
type systemPromptSnapshot struct {
	SessionID string    `json:"session_id"`
	Prompt    string    `json:"prompt"`
	Version   string    `json:"version,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (repository *jsonRepository) writeCheckpointLayout(key Key, payload []byte) error {
	return repository.layout.commitContentModule(key, moduleCheckpoint,
		checkpointContent{SessionID: key.SessionID, Payload: payload})
}

func (repository *jsonRepository) readCheckpointLayout(key Key) ([]byte, error) {
	content, err := readModuleHeadPayload[checkpointContent](repository.layout, key, moduleCheckpoint)
	if err != nil {
		return nil, err
	}
	if len(content.Payload) == 0 {
		return nil, fs.ErrNotExist
	}
	return content.Payload, nil
}

func (repository *jsonRepository) writeSystemPromptLayout(key Key, snapshot systemPromptSnapshot) error {
	return repository.layout.commitContentModule(key, moduleSystem, snapshot)
}

func (repository *jsonRepository) readSystemPromptLayout(key Key) (systemPromptSnapshot, error) {
	return readModuleHeadPayload[systemPromptSnapshot](repository.layout, key, moduleSystem)
}

// appendGoalAuditLayout 把 goal 审计条目写进 EVENT 通道（S19：GoalAudit 权威
// 在 EVENT goal.*，payload = 条目本体；Seq 由读取顺序派生）。
func (repository *jsonRepository) appendGoalAuditLayout(key Key, entry GoalAuditEntry) error {
	entry.Seq = 0
	payload, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	commitID := "goal-audit-" + hash(string(payload))
	_, err = repository.layout.structuralEventCommit(key, commitID, []structuralEvent{{
		Kind:     structuralEventKind(entry.Kind),
		CommitID: commitID,
		Payload:  payload,
	}})
	return err
}

// readGoalAuditLayout 读取 EVENT 通道中的 goal.* 审计条目。
func (repository *jsonRepository) readGoalAuditLayout(key Key) ([]GoalAuditEntry, error) {
	events, err := repository.layout.readEvents(key, 0, 0)
	if err != nil {
		return nil, err
	}
	out := make([]GoalAuditEntry, 0, len(events))
	for _, event := range events {
		if !strings.HasPrefix(string(event.Kind), "goal.") {
			continue
		}
		var entry GoalAuditEntry
		if len(event.Payload) > 0 {
			if err := json.Unmarshal(event.Payload, &entry); err != nil {
				return nil, err
			}
		}
		if entry.Kind == "" {
			entry.Kind = string(event.Kind)
		}
		entry.Seq = uint64(len(out) + 1)
		out = append(out, entry)
	}
	return out, nil
}

// sessionMeta 返回该会话的枚举 meta（message head.Meta；S20 会话枚举面）。
func (repository *jsonRepository) sessionMeta(key Key) (frameworkStorage.SessionMeta, bool) {
	return readMetaFromDir(repository.sessionDir(key))
}

// hasDraft 报告会话是否存在未发送草稿（§2.5.4 草稿行判据，S20）。
func (repository *jsonRepository) hasDraft(key Key) bool {
	_, err := os.Stat(repository.layout.lifecycleDraftPath(key))
	return err == nil
}

// writeSessionDisplayMetaLayout / readSessionDisplayMetaLayout 是项目级会话
// 展示元数据（置顶/别名/排序位，S20：不再借用 state 通道）。
func (repository *jsonRepository) writeSessionDisplayMetaLayout(projectID string, payload []byte) error {
	directory := repository.projectDir(projectID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	return writeAtomic(filepath.Join(directory, "session-meta.json"), payload, 0o600)
}

func (repository *jsonRepository) readSessionDisplayMetaLayout(projectID string) ([]byte, error) {
	return os.ReadFile(filepath.Join(repository.projectDir(projectID), "session-meta.json"))
}

// derivedRecordPayload 按 §2.5.4 派生 SessionRecord 形状的 JSON（version/id/
// status/conversation），供应用侧 record 消费者在 state 通道退役后继续工作。
func (repository *jsonRepository) derivedRecordPayload(key Key) ([]byte, error) {
	meta, ok := repository.sessionMeta(key)
	if !ok {
		return nil, fs.ErrNotExist
	}
	rows, err := repository.layout.readRows(key, 1, 0)
	if err != nil {
		return nil, err
	}
	messages := derivedConversationMessages(rows)
	status := "idle"
	if archivedAt, err := repository.layout.lifecycleArchivedAt(key); err == nil && !archivedAt.IsZero() {
		status = "archived"
	} else if repository.hasDraft(key) {
		status = "draft"
	}
	payload := struct {
		Version      int       `json:"version"`
		ID           string    `json:"id"`
		Status       string    `json:"status,omitempty"`
		UpdatedAt    time.Time `json:"updated_at,omitempty"`
		Conversation struct {
			Messages  []ConversationMessage `json:"messages,omitempty"`
			UpdatedAt time.Time             `json:"updated_at,omitempty"`
		} `json:"conversation"`
	}{Version: 3, ID: key.SessionID, Status: status, UpdatedAt: meta.UpdatedAt}
	payload.Conversation.Messages = messages
	payload.Conversation.UpdatedAt = meta.UpdatedAt
	return json.Marshal(payload)
}

// derivedConversationMessages 把 message 事件行派生为 conversation 消息，
// 形状与运行期可见投影一致（application/core appendHistoryLockedFor）：
//
//   - 有正文或思考的行 → 一条消息（正文与 ReasoningContent 分离携带）；
//   - 行内每个 tool_call → 一条 role=tool 的调用消息（保留 Arguments）；
//   - role=tool 的输出行 → 一条 role=tool_result 的结果消息（Result 带正文）。
//
// 逐行保序、一行可以有多个调用：只取第一个 tool_call 会让恢复后的可见会话
// 丢掉调用却留着它们的结果（工具对不上号），把 tool 行拆成「调用 + 结果」
// 两条则会把同一次调用重复计入窗口。两者叠加就是长会话恢复时看到的
// 「工具挤成一坨、助手正文掉队」。
func derivedConversationMessages(rows []Event) []ConversationMessage {
	messages := make([]ConversationMessage, 0, len(rows))
	for _, row := range rows {
		id := row.MessageID
		if id == "" {
			id = fmt.Sprintf("seq-%d", row.Seq)
		}
		if row.Role != "tool" && (row.Content != "" || row.ReasoningContent != "") {
			messages = append(messages, ConversationMessage{
				ID: id, Role: row.Role, Content: row.Content,
				ReasoningContent: row.ReasoningContent, CreatedAt: row.CreatedAt,
			})
		}
		for index, call := range row.ToolCalls {
			messages = append(messages, ConversationMessage{
				ID: derivedToolCallMessageID(id, index), Role: "tool", CreatedAt: row.CreatedAt,
				Tool: &ConversationToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments, Status: "success"},
			})
		}
		if row.Role == "tool" {
			messages = append(messages, ConversationMessage{
				ID: id, Role: "tool_result", Content: row.Content, CreatedAt: row.CreatedAt,
				Tool: &ConversationToolCall{ID: row.ToolCallID, Name: row.Name, Status: "success", Result: row.Content},
			})
		}
	}
	return messages
}

// derivedToolCallMessageID 给同一行派生出的调用消息分配稳定且唯一的 ID。
// 直接用行 ID 会与同行的正文消息撞键（前端按 ID 建 DOM key，撞键会让工具
// 行互相覆盖）；加序号后缀既稳定又不参与 message-N 派号解析。
func derivedToolCallMessageID(rowID string, index int) string {
	return fmt.Sprintf("%s#tool-%d", rowID, index+1)
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
	// D2/S12：旧 manifest 布局会话不再打开。
	return nil, false, fs.ErrNotExist
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

// readAllLayoutMessages 全量读由 message 事件行派生（S11：history.json
// provider 缓存已退役；Read 的 layout 分支不再"缓存优先"）。
func (repository *jsonRepository) readAllLayoutMessages(key Key) ([]types.Message, error) {
	if !repository.layout.sessionExists(key) {
		return nil, fs.ErrNotExist
	}
	rows, err := repository.layout.readAllRows(key)
	if err != nil {
		return nil, err
	}
	return rowsToProviderMessages(rows), nil
}

func (repository *jsonRepository) readRangeLayout(key Key, offset, limit int) ([]types.Message, int, error) {
	if offset < 0 {
		return nil, 0, errors.New("session storage: invalid range")
	}
	messages, err := repository.readAllLayoutMessages(key)
	if err != nil {
		return nil, 0, err
	}
	total := len(messages)
	if offset > total {
		return nil, total, errors.New("session storage: range offset exceeds history")
	}
	if limit <= 0 {
		// total-only 语义（与 legacy ReadRange 一致）。
		return nil, total, nil
	}
	end := offset + limit
	if end > total {
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
	// §5.1 / D13 / S17b：公开读接口原样回传行（commit_id /
	// wire_material / in_out_json 是幂等与装配凭据，不得擦除）。
	return selectEventTail(rows, tokenBudget, maxUnits), nil
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
