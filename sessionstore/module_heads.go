// 会话存储布局：metadata 模块化 head + guide 读索引（my_design §2/§3）。
//
// 布局事实（新架构）：
//   - 会话目录内 metadata/guide.json 只做读索引/路由（低频），模块各自
//     head 独立成 metadata/<module>.json；
//   - 模块 head = 该模块提交发布点：先 append 数据，再原子替换所属模块
//     json（I5）；reader 打开模块文件后校验 schema/checksum，不匹配重读
//     一次（自愈读，I9）；
//   - 写锁按模块：message/event/compact/stack/lifecycle/retention 各自
//     独立锁，模块间不互相阻塞（T-M1-05）。
package sessionstore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// 会话存储布局版本常量（guide/schema 演进时递增；旧版本文件只读兼容由
// jsonRepository 布局分派承担，见 json_layout.go）。
const (
	layoutVersion = 1
	schemaVersion = 1
)

// module 是 metadata 模块 ID（模块文件路径一律由枚举名推导，
// guide 不再登记模块地址，§2.0 规则 3 / D9 / S14）。
type storageModule string

const (
	moduleMessage storageModule = "message"
	moduleEvent   storageModule = "event"
	moduleCompact storageModule = "compact"
	// §2.0/D6/S16：栈 head 按 kind 拆成三个模块文件与三把锁，跨 kind 提交
	// 不再共享串行点。
	moduleStackPlan storageModule = "stack_plan"
	moduleStackTask storageModule = "stack_task"
	moduleStackGoal storageModule = "stack_goal"
	moduleLifecycle storageModule = "lifecycle"
	moduleRetention storageModule = "retention"
	moduleSubagent  storageModule = "subagent"
	// §2.1/S19：会话侧 system prompt 快照（低频内容，规则 1 例外）。
	moduleSystem storageModule = "system"
	// §2.5.5/S24：引擎续跑快照（整份替换型内容，E.2 白名单）。
	moduleCheckpoint storageModule = "checkpoint"
	moduleMedia      storageModule = "media"
)

// guide 是会话读索引/路由（不持有模块数据，I9）。模块清单变更（首写某
// 模块文件）时低频更新。
type layoutGuide struct {
	LayoutVersion int       `json:"layout_version"`
	SchemaVersion int       `json:"schema_version"`
	SessionID     string    `json:"session_id"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// moduleHeadFile 是 metadata/<module>.json 的统一信封：payload 是各模块
// 自有结构；checksum 由 writer 在发布时计算，reader 校验失败重读一次。
type moduleHeadFile struct {
	SchemaVersion int             `json:"schema_version"`
	SessionID     string          `json:"session_id"`
	ModuleID      storageModule   `json:"module_id"`
	CommitID      string          `json:"commit_id,omitempty"`
	Checksum      string          `json:"checksum"`
	UpdatedAt     time.Time       `json:"updated_at"`
	Payload       json.RawMessage `json:"payload"`
}

// storeEngine 是 会话存储布局 JSON 引擎。root 与 jsonRepository.root 同语义
// （sessions-json 根目录）；settings 为零值时补 §11 默认值。
//
// 并发语义：写锁按“会话 × 模块”分片（单会话单写者、跨会话并行）；同会话
// 不同模块互不阻塞，不设全会话/全仓库写锁。sessionMu 注册表本身只承担
// 取锁指针的短临界区（registryMu），数据保护全部落在各会话模块锁上。
type storeEngine struct {
	root     string
	settings storageSettings

	registryMu sync.RWMutex
	sessionMu  map[string]*sessionModuleLocks
	// stack 是栈通道后端的延迟归因累加器（构造时创建，只读引用无竞争）。
	stack *stackStats
	// event 是 EVENT 写路径归因累加器（T-EV-05：shardReads 必须保持 0）。
	event *eventStats
}

// sessionModuleLocks 是单个会话的模块锁集合与只属于该会话的短临界区状态。
//
// 锁口径：
//   - 每个模块一把锁（§2.0 规则 2）→ 模块间互不阻塞；
//   - guideMu 只保护本会话 metadata 目录/guide 注册（原实现是仓库级
//     metaMu：任一会话注册模块会让所有会话的提交排队）；
//   - stackViews 是栈通道提交时发布的不可变读投影（actor 出口），读者既不
//     取锁也不打开文件句柄。
type sessionModuleLocks struct {
	messageMu    sync.Mutex
	eventMu      sync.Mutex
	compactMu    sync.Mutex
	stackPlanMu  sync.Mutex
	stackTaskMu  sync.Mutex
	stackGoalMu  sync.Mutex
	lifecycleMu  sync.Mutex
	retentionMu  sync.Mutex
	subagentMu   sync.Mutex
	toolRefsMu   sync.Mutex
	systemMu     sync.Mutex
	checkpointMu sync.Mutex
	guideMu      sync.Mutex

	stackViews [4]atomic.Pointer[stackView]
	// anchor 是 message 通道最近一次发布的坐标（栈通道取锚用，避免打开
	// metadata/message.json）。
	anchor atomic.Pointer[messageAnchorPoint]
}

// readSelfHealHook 是自愈读的测试钩子：head 首次校验失败后、重读前被
// 调用（生产路径不设置；测试用它模拟 writer 在两次读取之间完成原子替换）。
var readSelfHealHook func()

// newStoreEngine 构造 会话存储布局引擎。root 不存在时延后到首个会话提交再创建
// （与 jsonRepository 惰性建目录一致）。
func newStoreEngine(root string, settings storageSettings) *storeEngine {
	resolved := resolveStorageSettings(settings)
	return &storeEngine{
		root:      filepath.Clean(root),
		settings:  resolved,
		sessionMu: make(map[string]*sessionModuleLocks),
		stack:     &stackStats{},
		event:     &eventStats{},
	}
}

// locks 返回（必要时创建）该会话的模块锁与短临界区状态集合。
func (store *storeEngine) locks(key Key) *sessionModuleLocks {
	id := store.sessionRoot(key)
	store.registryMu.RLock()
	locks := store.sessionMu[id]
	store.registryMu.RUnlock()
	if locks != nil {
		return locks
	}
	store.registryMu.Lock()
	defer store.registryMu.Unlock()
	if locks = store.sessionMu[id]; locks != nil {
		return locks
	}
	locks = &sessionModuleLocks{}
	store.sessionMu[id] = locks
	return locks
}

// mu 返回指定会话 + 模块的锁（按会话分片；注册表短临界区，不参与 IO）。
func (store *storeEngine) mu(key Key, mod storageModule) *sync.Mutex {
	return store.locks(key).mutexFor(mod)
}

// moduleForStackKind 返回栈 kind 的模块 head 枚举（S16：head 按 kind 分文件）。
func moduleForStackKind(kind StackKind) storageModule {
	switch kind {
	case StackKindTask:
		return moduleStackTask
	case StackKindGoal:
		return moduleStackGoal
	case StackKindSubagent:
		return moduleSubagent
	default:
		return moduleStackPlan
	}
}

func (locks *sessionModuleLocks) mutexFor(mod storageModule) *sync.Mutex {
	switch mod {
	case moduleMessage:
		return &locks.messageMu
	case moduleEvent:
		return &locks.eventMu
	case moduleCompact:
		return &locks.compactMu
	case moduleStackPlan:
		return &locks.stackPlanMu
	case moduleStackTask:
		return &locks.stackTaskMu
	case moduleStackGoal:
		return &locks.stackGoalMu
	case moduleLifecycle:
		return &locks.lifecycleMu
	case moduleRetention:
		return &locks.retentionMu
	case moduleSubagent:
		return &locks.subagentMu
	case moduleSystem:
		return &locks.systemMu
	case moduleCheckpoint:
		return &locks.checkpointMu
	default:
		return &locks.messageMu
	}
}

// layout 判定：目录内存在 metadata/guide.json = 新布局。
func isLayoutSessionDir(sessionRoot string) bool {
	_, err := os.Stat(filepath.Join(sessionRoot, "metadata", "guide.json"))
	return err == nil
}

// sessionRoot 返回会话 会话存储布局的物理根（与旧 manifest 布局同会话目录，
// 便于 List/Delete 同时枚举新旧会话）。
func (store *storeEngine) sessionRoot(key Key) string {
	return filepath.Join(store.root, "project-"+hash(key.ProjectID), "session-"+hash(key.SessionID))
}

func (store *storeEngine) metadataDir(key Key) string {
	return filepath.Join(store.sessionRoot(key), "metadata")
}

func (store *storeEngine) modulePath(key Key, module storageModule) string {
	return filepath.Join(store.metadataDir(key), string(module)+".json")
}

func (store *storeEngine) guidePath(key Key) string {
	return filepath.Join(store.metadataDir(key), "guide.json")
}

// ensureLayoutGuide 创建会话 会话存储布局目录并写入 guide.json（幂等：已存在直接
// 读取返回）。guide 是唯一由本方法创建的模块注册点。
func (store *storeEngine) ensureLayoutGuide(key Key) (layoutGuide, error) {
	locks := store.locks(key)
	locks.guideMu.Lock()
	defer locks.guideMu.Unlock()
	path := store.guidePath(key)
	if data, err := os.ReadFile(path); err == nil {
		var guide layoutGuide
		if json.Unmarshal(data, &guide) == nil && guide.SessionID == key.SessionID {
			return guide, nil
		}
	}
	if err := os.MkdirAll(store.metadataDir(key), 0o700); err != nil {
		return layoutGuide{}, err
	}
	now := time.Now().UTC()
	guide := layoutGuide{
		LayoutVersion: layoutVersion,
		SchemaVersion: schemaVersion,
		SessionID:     key.SessionID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	data, err := json.MarshalIndent(guide, "", "  ")
	if err != nil {
		return layoutGuide{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return layoutGuide{}, err
	}
	if err := writeAtomic(path, data, 0o600); err != nil {
		return layoutGuide{}, err
	}
	return guide, nil
}

// headChecksum 计算模块 head 的校验指纹：对信封（不含 checksum 字段）
// 的稳定序列化取 sha256。reader 用解析后的信封重算比较，不依赖文件原始
// 字节序。
func headChecksum(head moduleHeadFile) (string, error) {
	copy := head
	copy.Checksum = ""
	data, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// publishModuleHead 原子发布模块 head（调用方持对应模块锁）。返回发布后
// 的 commit_id 与 checksum。
func (store *storeEngine) publishModuleHead(key Key, mod storageModule, commitID string, payload any, updatedAt time.Time) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("session storage: encode %s head payload: %w", mod, err)
	}
	head := moduleHeadFile{
		SchemaVersion: schemaVersion,
		SessionID:     key.SessionID,
		ModuleID:      mod,
		CommitID:      commitID,
		UpdatedAt:     updatedAt,
		Payload:       raw,
	}
	checksum, err := headChecksum(head)
	if err != nil {
		return "", err
	}
	head.Checksum = checksum
	data, err := json.MarshalIndent(head, "", "  ")
	if err != nil {
		return "", err
	}
	path := store.modulePath(key, mod)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := writeAtomic(path, data, 0o600); err != nil {
		return "", fmt.Errorf("session storage: publish %s head: %w", mod, err)
	}
	return commitID, nil
}

// readModuleHeadFile 读取模块 head（缺失返回 fs.ErrNotExist）。reader
// 校验 schema/session/module 与 checksum；一次不匹配（例如 writer 正在原子
// 替换、读到旧文件）则重读一次（guide 自愈读语义，I9）。
func (store *storeEngine) readModuleHeadFile(key Key, module storageModule) (moduleHeadFile, error) {
	path := store.modulePath(key, module)
	read := func() (moduleHeadFile, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			return moduleHeadFile{}, err
		}
		var head moduleHeadFile
		if err := json.Unmarshal(data, &head); err != nil {
			return moduleHeadFile{}, fmt.Errorf("session storage: decode %s head %q: %w", module, path, err)
		}
		if head.SchemaVersion != schemaVersion {
			return moduleHeadFile{}, fmt.Errorf("session storage: %s head schema=%d want %d", module, head.SchemaVersion, schemaVersion)
		}
		if head.SessionID != key.SessionID {
			return moduleHeadFile{}, fmt.Errorf("session storage: %s head session %q != %q", module, head.SessionID, key.SessionID)
		}
		if head.ModuleID != module {
			return moduleHeadFile{}, fmt.Errorf("session storage: head module %q != %q", head.ModuleID, module)
		}
		want, err := headChecksum(head)
		if err != nil {
			return moduleHeadFile{}, err
		}
		if head.Checksum != "" && want != head.Checksum {
			return moduleHeadFile{}, fmt.Errorf("session storage: %s head checksum mismatch", module)
		}
		return head, nil
	}
	head, err := read()
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return head, err
	}
	// 自愈读：先重读一次；仍失败才上报（校验失败可能来自并发替换的瞬时态）。
	if hook := readSelfHealHook; hook != nil {
		hook()
	}
	head, retryErr := read()
	if retryErr != nil {
		// §2.0 规则 3 / S15：第二次仍不匹配 → 按数据文件重建该模块 head
		// （修补），不返回旧值也不判损坏。message/event/compact/stack_* 有
		// 完整数据文件可重建；其余模块（lifecycle 自带 lc-repair、
		// retention/subagent/media 无独立数据文件）保留原错误。
		if repairErr := store.repairModuleHeadFromData(key, module); repairErr != nil {
			return moduleHeadFile{}, err
		}
		head, retryErr = read()
		if retryErr != nil {
			return moduleHeadFile{}, err
		}
		return head, nil
	}
	return head, nil
}

// repairModuleHeadFromData 按数据文件重建模块 head 并原子发布（S15）。
func (store *storeEngine) repairModuleHeadFromData(key Key, module storageModule) error {
	var payload any
	switch module {
	case moduleMessage:
		head, err := store.rebuildMessageHeadFromData(key)
		if err != nil {
			return err
		}
		payload = head
	case moduleEvent:
		head, err := store.rebuildEventHeadFromData(key)
		if err != nil {
			return err
		}
		payload = head
	case moduleCompact:
		head, err := store.rebuildCompactHeadFromData(key)
		if err != nil {
			return err
		}
		payload = head
	case moduleStackPlan, moduleStackTask, moduleStackGoal, moduleSubagent:
		kind := moduleStackKindOf(module)
		head, err := store.rebuildStackKindHeadFromData(key, kind)
		if err != nil {
			return err
		}
		payload = head
	default:
		return fmt.Errorf("session storage: no data-file rebuild for module %q", module)
	}
	_, err := store.publishModuleHead(key, module, "self-heal-repair", payload, time.Now().UTC())
	return err
}

// rebuildMessageHeadFromData 从 message/*.jsonl 分片重建 message head。
func (store *storeEngine) rebuildMessageHeadFromData(key Key) (messageHead, error) {
	head := emptyMessageHead(key)
	dir := store.messageDir(key)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return head, nil
		}
		return messageHead{}, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	now := time.Now().UTC()
	tokens := 0
	for _, entry := range entries {
		if entry.IsDir() || !isMessageShardFile(entry.Name()) {
			continue
		}
		rows, readErr := readMessageRowsFileAt(filepath.Join(dir, entry.Name()))
		if readErr != nil {
			return messageHead{}, readErr
		}
		if len(rows) == 0 {
			continue
		}
		shard := shardInfo{
			Path: entry.Name(), FromSeq: rows[0].Seq, ToSeq: rows[len(rows)-1].Seq,
			Count: len(rows),
		}
		head.Shards = append(head.Shards, shard)
		head.TotalRows += uint64(len(rows))
		if shard.ToSeq > head.LastSeq {
			head.LastSeq = shard.ToSeq
		}
		for _, row := range rows {
			tokens += row.TokenCount
			if row.MessageID != "" {
				head.LastMessageID = row.MessageID
			}
		}
	}
	head.Meta.TokenCount = tokens
	head.Meta.ShardCount = len(head.Shards)
	head.Meta.SessionID = key.SessionID
	head.Meta.CreatedAt = now
	head.Meta.UpdatedAt = now
	return head, nil
}

// rebuildEventHeadFromData 从 event/*.jsonl 分片重建 event head。
func (store *storeEngine) rebuildEventHeadFromData(key Key) (eventHeadRecord, error) {
	head := emptyEventHead(key)
	dir := store.structuralEventDir(key)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return head, nil
		}
		return eventHeadRecord{}, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !isEventShardFile(entry.Name()) {
			continue
		}
		rows, readErr := readStructuralEventsAt(filepath.Join(dir, entry.Name()))
		if readErr != nil {
			return eventHeadRecord{}, readErr
		}
		if len(rows) == 0 {
			continue
		}
		info := eventShardInfo{
			Path: entry.Name(), FromID: rows[0].EventID, ToID: rows[len(rows)-1].EventID,
			Count: len(rows), Bytes: 0,
		}
		head.Shards = append(head.Shards, info)
		head.Total += uint64(len(rows))
		if info.ToID > head.LastID {
			head.LastID = info.ToID
		}
		head.LastCommitID = rows[len(rows)-1].CommitID
	}
	return head, nil
}

// rebuildCompactHeadFromData 从 session/compact.jsonl 重建 compact head。
func (store *storeEngine) rebuildCompactHeadFromData(key Key) (compactHeadRecord, error) {
	head := compactHeadRecord{SessionID: key.SessionID}
	rows, err := readCompactFrameRows(store.compactFilePath(key))
	if err != nil {
		return compactHeadRecord{}, err
	}
	head.FrameCount = len(rows)
	if len(rows) > 0 {
		last := rows[len(rows)-1]
		head.LastFrameID = last.FrameID
		head.LastMessageTo = last.MessageTo
		head.LastSeq = last.MessageToSeq
		head.LatestFrame = &last
	}
	return head, nil
}

// moduleStackKindOf 反查栈模块对应的 kind。
func moduleStackKindOf(module storageModule) StackKind {
	switch module {
	case moduleStackTask:
		return StackKindTask
	case moduleStackGoal:
		return StackKindGoal
	case moduleSubagent:
		return StackKindSubagent
	default:
		return StackKindPlan
	}
}

// rebuildStackKindHeadFromData 从该 kind 的 active/history.jsonl 重建栈
// head 水位（revision 取行内最大；active_count/history_count 按行数）。
func (store *storeEngine) rebuildStackKindHeadFromData(key Key, kind StackKind) (stackModuleHead, error) {
	head := stackModuleHead{SessionID: key.SessionID, Kinds: make(map[StackKind]stackWatermark)}
	const unbounded uint64 = ^uint64(0)
	active, err := readStackRowsFile(store.stackActivePath(key, kind), unbounded)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return stackModuleHead{}, err
	}
	history, err := readStackRowsFile(store.stackHistoryPath(key, kind), unbounded)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return stackModuleHead{}, err
	}
	water := stackWatermark{ActiveCount: len(active), HistoryCount: uint64(len(history))}
	for _, row := range active {
		if row.Revision > water.HeadSeq {
			water.HeadSeq = row.Revision
		}
	}
	for _, row := range history {
		if row.Revision > water.HeadSeq {
			water.HeadSeq = row.Revision
		}
	}
	if size, statErr := fileSizeOrZero(store.stackHistoryPath(key, kind)); statErr == nil {
		water.HistoryBytes = size
	}
	head.HeadSeq = water.HeadSeq
	head.Kinds[kind] = water
	return head, nil
}

// readCompactFrameRows 读取 compact.jsonl 的完整帧行（崩溃残尾跳过）。
func readCompactFrameRows(path string) ([]compactFrameRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []compactFrameRecord{}, nil
		}
		return nil, err
	}
	segments := bytes.Split(data, []byte{'\n'})
	rows := make([]compactFrameRecord, 0, len(segments))
	for index, segment := range segments {
		if index == len(segments)-1 && len(bytes.TrimSpace(segment)) > 0 {
			continue // 崩溃残尾
		}
		segment = bytes.TrimSpace(segment)
		if len(segment) == 0 {
			continue
		}
		var row compactFrameRecord
		if json.Unmarshal(segment, &row) != nil {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// decodeHeadPayload 把模块 head 的 payload 解码到 target。
func decodeHeadPayload[T any](head moduleHeadFile) (T, error) {
	var out T
	if len(head.Payload) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(head.Payload, &out); err != nil {
		return out, fmt.Errorf("session storage: decode %s head payload: %w", head.ModuleID, err)
	}
	return out, nil
}

// commitModuleHead 是通用模块提交：先确保布局，再原子发布模块 head。
// 调用方传入对应模块锁（stack/lifecycle/retention/subagent 等无独立数据
// 文件的模块 head 也用此入口）。
func (store *storeEngine) commitModuleHead(key Key, mod storageModule, commitID string, payload any) error {
	if _, err := store.ensureLayoutGuide(key); err != nil {
		return err
	}
	if commitID == "" {
		commitID = modulePayloadCommitID(mod, payload)
	}
	moduleLock := store.mu(key, mod)
	moduleLock.Lock()
	defer moduleLock.Unlock()
	_, err := store.publishModuleHead(key, mod, commitID, payload, time.Now().UTC())
	return err
}

// commitContentModule 整份替换型内容模块（system/checkpoint）写入：模块 head
// 信封即内容（rename 成功 = 发布；不配独立数据文件，规则 1 例外）。
func (store *storeEngine) commitContentModule(key Key, mod storageModule, payload any) error {
	return store.commitModuleHead(key, mod, modulePayloadCommitID(mod, payload), payload)
}

// modulePayloadCommitID 按模块 head payload 内容确定性推导凭据（S17/D13：
// 存储层不现造随机号，也不使用常量）。
func modulePayloadCommitID(mod storageModule, payload any) string {
	raw, _ := json.Marshal(payload)
	return "head-" + string(mod) + "-" + hash(string(raw))
}

// readModuleHeadPayload 读取通用模块 head 并解码 payload；缺失时返回
// 零值 + fs.ErrNotExist。
func readModuleHeadPayload[T any](store *storeEngine, key Key, module storageModule) (T, error) {
	var zero T
	headFile, err := store.readModuleHeadFile(key, module)
	if err != nil {
		return zero, err
	}
	return decodeHeadPayload[T](headFile)
}

// deleteModule 删除模块 head 与对应数据目录（短窗口崩溃/审计模拟用；
// 数据通道由对应模块锁保护）。
func (store *storeEngine) deleteModule(key Key, module storageModule) error {
	if err := os.Remove(store.modulePath(key, module)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	switch module {
	case moduleMessage:
		return os.RemoveAll(store.messageDir(key))
	case moduleEvent:
		return os.RemoveAll(store.structuralEventDir(key))
	}
	return nil
}

// stackHead 的历史职责已迁出：条目内容落 session/{plan,task,goal}/
// {active,history}.jsonl，metadata/stack.json 只存水位，见 stack_channel.go。
