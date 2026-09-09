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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// 会话存储布局版本常量（guide/schema 演进时递增；旧版本文件只读兼容由
// jsonRepository 布局分派承担，见 json_layout.go）。
const (
	layoutVersion = 1
	schemaVersion = 1
)

// module 是 metadata 模块 ID（guide.module_index 的注册键）。
type storageModule string

const (
	moduleMessage   storageModule = "message"
	moduleEvent     storageModule = "event"
	moduleCompact   storageModule = "compact"
	moduleStack     storageModule = "stack"
	moduleLifecycle storageModule = "lifecycle"
	moduleRetention storageModule = "retention"
	moduleSubagent  storageModule = "subagent"
	// moduleToolResult 是 tool-results 通道的发布点（写文件 → 原子发布
	// refs 清单；读者按 head 全量可见，避免目录扫描撕裂，见 T-FK torn）。
	moduleToolResult storageModule = "toolresult"
	moduleMedia      storageModule = "media"
)

// guide 是会话读索引/路由（不持有模块数据，I9）。模块清单变更（首写某
// 模块文件）时低频更新。
type layoutGuide struct {
	LayoutVersion int                         `json:"layout_version"`
	SchemaVersion int                         `json:"schema_version"`
	SessionID     string                      `json:"session_id"`
	ModuleIndex   map[storageModule]moduleRef `json:"module_index,omitempty"`
	CreatedAt     time.Time                   `json:"created_at"`
	UpdatedAt     time.Time                   `json:"updated_at"`
}

// moduleRef 描述一个已注册模块 head 的物理文件与注册时间。
type moduleRef struct {
	File         string    `json:"file"`
	Version      int       `json:"version"`
	RegisteredAt time.Time `json:"registered_at"`
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
// （sessions-json 根目录）；shardRows <= 0 时取默认 100 行/片。
//
// 并发语义：写锁按“会话 × 模块”分片（单会话单写者、跨会话并行）；同会话
// 不同模块互不阻塞，不设全会话/全仓库写锁。sessionMu 注册表本身只承担
// 取锁指针的短临界区（registryMu），数据保护全部落在各会话模块锁上。
type storeEngine struct {
	root      string
	shardRows int

	registryMu sync.RWMutex
	sessionMu  map[string]*sessionModuleLocks
	metaMu     sync.Mutex // metadata 目录/guide 自愈读与注册（低频短临界区）
}

// sessionModuleLocks 是单个会话的模块锁集合。
type sessionModuleLocks struct {
	messageMu   sync.Mutex
	eventMu     sync.Mutex
	compactMu   sync.Mutex
	stackMu     sync.Mutex
	lifecycleMu sync.Mutex
	retentionMu sync.Mutex
	subagentMu  sync.Mutex
	toolMu      sync.Mutex
}

// readSelfHealHook 是自愈读的测试钩子：head 首次校验失败后、重读前被
// 调用（生产路径不设置；测试用它模拟 writer 在两次读取之间完成原子替换）。
var readSelfHealHook func()

// newStoreEngine 构造 会话存储布局引擎。root 不存在时延后到首个会话提交再创建
// （与 jsonRepository 惰性建目录一致）。
func newStoreEngine(root string, shardRows int) *storeEngine {
	if shardRows <= 0 {
		shardRows = defaultMessageShardSize
	}
	return &storeEngine{
		root:      filepath.Clean(root),
		shardRows: shardRows,
		sessionMu: make(map[string]*sessionModuleLocks),
	}
}

// mu 返回指定会话 + 模块的锁（按会话分片；注册表短临界区，不参与 IO）。
func (store *storeEngine) mu(key Key, mod storageModule) *sync.Mutex {
	id := store.sessionRoot(key)
	store.registryMu.RLock()
	locks := store.sessionMu[id]
	store.registryMu.RUnlock()
	if locks != nil {
		return locks.mutexFor(mod)
	}
	store.registryMu.Lock()
	locks = store.sessionMu[id]
	if locks == nil {
		locks = &sessionModuleLocks{}
		store.sessionMu[id] = locks
	}
	store.registryMu.Unlock()
	return locks.mutexFor(mod)
}

func (locks *sessionModuleLocks) mutexFor(mod storageModule) *sync.Mutex {
	switch mod {
	case moduleMessage:
		return &locks.messageMu
	case moduleEvent:
		return &locks.eventMu
	case moduleCompact:
		return &locks.compactMu
	case moduleStack:
		return &locks.stackMu
	case moduleLifecycle:
		return &locks.lifecycleMu
	case moduleRetention:
		return &locks.retentionMu
	case moduleSubagent:
		return &locks.subagentMu
	case moduleToolResult:
		return &locks.toolMu
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
	store.metaMu.Lock()
	defer store.metaMu.Unlock()
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
		ModuleIndex:   make(map[storageModule]moduleRef),
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

// registerModule 把模块注册进 guide.module_index（幂等；guide 低频更新，
// 不随每次提交重写）。
func (store *storeEngine) registerModule(key Key, mod storageModule, modulePath string) error {
	store.metaMu.Lock()
	defer store.metaMu.Unlock()
	path := store.guidePath(key)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var guide layoutGuide
	if err := json.Unmarshal(data, &guide); err != nil {
		return err
	}
	if ref, ok := guide.ModuleIndex[mod]; ok && ref.File != "" {
		return nil
	}
	if guide.ModuleIndex == nil {
		guide.ModuleIndex = make(map[storageModule]moduleRef)
	}
	guide.ModuleIndex[mod] = moduleRef{
		File:         modulePath,
		Version:      schemaVersion,
		RegisteredAt: time.Now().UTC(),
	}
	guide.UpdatedAt = time.Now().UTC()
	encoded, err := json.MarshalIndent(guide, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, encoded, 0o600)
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
		return moduleHeadFile{}, err
	}
	return head, nil
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
		commitID = randomID()
	}
	moduleLock := store.mu(key, mod)
	moduleLock.Lock()
	defer moduleLock.Unlock()
	if _, err := store.publishModuleHead(key, mod, commitID, payload, time.Now().UTC()); err != nil {
		return err
	}
	return store.registerModule(key, mod, store.modulePath(key, mod))
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

// stackHead 是 plan/task/goal 栈模块 head（M3 fork 栈快照、T-M1-05 并发
// 用；当前只保存 head_seq 与批次锚点摘要）。
type stackHead struct {
	SessionID string `json:"session_id"`
	HeadSeq   uint64 `json:"head_seq"`
	// Items 是活跃批次条目摘要（item_message_id/batch_message_* 锚）。
	Items []stackItem `json:"items,omitempty"`
}

// stackItem 是栈条目摘要（history 锚，my_design §4）。
type stackItem struct {
	ItemID           string `json:"item_id"`
	ItemMessageID    string `json:"item_message_id"`
	BatchMessageFrom string `json:"batch_message_from,omitempty"`
	BatchMessageTo   string `json:"batch_message_to,omitempty"`
	Status           string `json:"status,omitempty"`
}
