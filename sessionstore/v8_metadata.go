// v8 会话存储布局：metadata 模块化 head + guide 读索引（my_design §2/§3）。
//
// 布局事实（v8.2）：
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

// v8 布局版本常量（guide/schema 演进时递增；旧版本文件只读兼容由
// jsonRepository 布局分派承担，见 v8_repository.go）。
const (
	v8LayoutVersion = 1
	v8SchemaVersion = 1
)

// v8Module 是 metadata 模块 ID（guide.module_index 的注册键）。
type v8Module string

const (
	v8ModuleMessage   v8Module = "message"
	v8ModuleEvent     v8Module = "event"
	v8ModuleCompact   v8Module = "compact"
	v8ModuleStack     v8Module = "stack"
	v8ModuleLifecycle v8Module = "lifecycle"
	v8ModuleRetention v8Module = "retention"
	v8ModuleSubagent  v8Module = "subagent"
	// v8ModuleToolResult 是 tool-results 通道的发布点（写文件 → 原子发布
	// refs 清单；读者按 head 全量可见，避免目录扫描撕裂，见 T-FK torn）。
	v8ModuleToolResult v8Module = "toolresult"
	v8ModuleMedia      v8Module = "media"
)

// v8Guide 是会话读索引/路由（不持有模块数据，I9）。模块清单变更（首写某
// 模块文件）时低频更新。
type v8Guide struct {
	LayoutVersion int                      `json:"layout_version"`
	SchemaVersion int                      `json:"schema_version"`
	SessionID     string                   `json:"session_id"`
	ModuleIndex   map[v8Module]v8ModuleRef `json:"module_index,omitempty"`
	CreatedAt     time.Time                `json:"created_at"`
	UpdatedAt     time.Time                `json:"updated_at"`
}

// v8ModuleRef 描述一个已注册模块 head 的物理文件与注册时间。
type v8ModuleRef struct {
	File         string    `json:"file"`
	Version      int       `json:"version"`
	RegisteredAt time.Time `json:"registered_at"`
}

// v8ModuleHeadFile 是 metadata/<module>.json 的统一信封：payload 是各模块
// 自有结构；checksum 由 writer 在发布时计算，reader 校验失败重读一次。
type v8ModuleHeadFile struct {
	SchemaVersion int             `json:"schema_version"`
	SessionID     string          `json:"session_id"`
	ModuleID      v8Module        `json:"module_id"`
	CommitID      string          `json:"commit_id,omitempty"`
	Checksum      string          `json:"checksum"`
	UpdatedAt     time.Time       `json:"updated_at"`
	Payload       json.RawMessage `json:"payload"`
}

// v8Store 是 v8 布局 JSON 引擎。root 与 jsonRepository.root 同语义
// （sessions-json 根目录）；shardRows <= 0 时取默认 100 行/片。
//
// 并发语义：单数据根单进程写者（I10），写锁按模块；读共享同一把模块锁。
// 模块间不设全会话元数据写锁（my_design §2 规则 2）。
type v8Store struct {
	root      string
	shardRows int

	messageMu   sync.Mutex
	eventMu     sync.Mutex
	compactMu   sync.Mutex
	stackMu     sync.Mutex
	lifecycleMu sync.Mutex
	retentionMu sync.Mutex
	subagentMu  sync.Mutex
	toolMu      sync.Mutex
	metaMu      sync.Mutex // metadata 目录/guide 自愈读与注册
}

// v8ReadSelfHealHook 是自愈读的测试钩子：head 首次校验失败后、重读前被
// 调用（生产路径不设置；测试用它模拟 writer 在两次读取之间完成原子替换）。
var v8ReadSelfHealHook func()

// newV8Store 构造 v8 布局引擎。root 不存在时延后到首个会话提交再创建
// （与 jsonRepository 惰性建目录一致）。
func newV8Store(root string, shardRows int) *v8Store {
	if shardRows <= 0 {
		shardRows = defaultMessageShardSize
	}
	return &v8Store{root: filepath.Clean(root), shardRows: shardRows}
}

// layout 判定：目录内存在 metadata/guide.json = v8 新布局。
func isV8SessionDir(sessionRoot string) bool {
	_, err := os.Stat(filepath.Join(sessionRoot, "metadata", "guide.json"))
	return err == nil
}

// v8SessionRoot 返回会话 v8 布局的物理根（与旧 manifest 布局同会话目录，
// 便于 List/Delete 同时枚举新旧会话）。
func (store *v8Store) v8SessionRoot(key Key) string {
	return filepath.Join(store.root, "project-"+hash(key.ProjectID), "session-"+hash(key.SessionID))
}

func (store *v8Store) v8MetadataDir(key Key) string {
	return filepath.Join(store.v8SessionRoot(key), "metadata")
}

func (store *v8Store) v8ModulePath(key Key, module v8Module) string {
	return filepath.Join(store.v8MetadataDir(key), string(module)+".json")
}

func (store *v8Store) v8GuidePath(key Key) string {
	return filepath.Join(store.v8MetadataDir(key), "guide.json")
}

// ensureV8Guide 创建会话 v8 布局目录并写入 guide.json（幂等：已存在直接
// 读取返回）。guide 是唯一由本方法创建的模块注册点。
func (store *v8Store) ensureV8Guide(key Key) (v8Guide, error) {
	store.metaMu.Lock()
	defer store.metaMu.Unlock()
	path := store.v8GuidePath(key)
	if data, err := os.ReadFile(path); err == nil {
		var guide v8Guide
		if json.Unmarshal(data, &guide) == nil && guide.SessionID == key.SessionID {
			return guide, nil
		}
	}
	if err := os.MkdirAll(store.v8MetadataDir(key), 0o700); err != nil {
		return v8Guide{}, err
	}
	now := time.Now().UTC()
	guide := v8Guide{
		LayoutVersion: v8LayoutVersion,
		SchemaVersion: v8SchemaVersion,
		SessionID:     key.SessionID,
		ModuleIndex:   make(map[v8Module]v8ModuleRef),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	data, err := json.MarshalIndent(guide, "", "  ")
	if err != nil {
		return v8Guide{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return v8Guide{}, err
	}
	if err := writeAtomic(path, data, 0o600); err != nil {
		return v8Guide{}, err
	}
	return guide, nil
}

// registerV8Module 把模块注册进 guide.module_index（幂等；guide 低频更新，
// 不随每次提交重写）。
func (store *v8Store) registerV8Module(key Key, module v8Module, modulePath string) error {
	store.metaMu.Lock()
	defer store.metaMu.Unlock()
	path := store.v8GuidePath(key)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var guide v8Guide
	if err := json.Unmarshal(data, &guide); err != nil {
		return err
	}
	if ref, ok := guide.ModuleIndex[module]; ok && ref.File != "" {
		return nil
	}
	if guide.ModuleIndex == nil {
		guide.ModuleIndex = make(map[v8Module]v8ModuleRef)
	}
	guide.ModuleIndex[module] = v8ModuleRef{
		File:         modulePath,
		Version:      v8SchemaVersion,
		RegisteredAt: time.Now().UTC(),
	}
	guide.UpdatedAt = time.Now().UTC()
	encoded, err := json.MarshalIndent(guide, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, encoded, 0o600)
}

// v8HeadChecksum 计算模块 head 的校验指纹：对信封（不含 checksum 字段）
// 的稳定序列化取 sha256。reader 用解析后的信封重算比较，不依赖文件原始
// 字节序。
func v8HeadChecksum(head v8ModuleHeadFile) (string, error) {
	copy := head
	copy.Checksum = ""
	data, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// publishV8ModuleHead 原子发布模块 head（调用方持对应模块锁）。返回发布后
// 的 commit_id 与 checksum。
func (store *v8Store) publishV8ModuleHead(key Key, module v8Module, commitID string, payload any, updatedAt time.Time) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("v8: encode %s head payload: %w", module, err)
	}
	head := v8ModuleHeadFile{
		SchemaVersion: v8SchemaVersion,
		SessionID:     key.SessionID,
		ModuleID:      module,
		CommitID:      commitID,
		UpdatedAt:     updatedAt,
		Payload:       raw,
	}
	checksum, err := v8HeadChecksum(head)
	if err != nil {
		return "", err
	}
	head.Checksum = checksum
	data, err := json.MarshalIndent(head, "", "  ")
	if err != nil {
		return "", err
	}
	path := store.v8ModulePath(key, module)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := writeAtomic(path, data, 0o600); err != nil {
		return "", fmt.Errorf("v8: publish %s head: %w", module, err)
	}
	return commitID, nil
}

// readV8ModuleHeadFile 读取模块 head（缺失返回 fs.ErrNotExist）。reader
// 校验 schema/session/module 与 checksum；一次不匹配（例如 writer 正在原子
// 替换、读到旧文件）则重读一次（guide 自愈读语义，I9）。
func (store *v8Store) readV8ModuleHeadFile(key Key, module v8Module) (v8ModuleHeadFile, error) {
	path := store.v8ModulePath(key, module)
	read := func() (v8ModuleHeadFile, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			return v8ModuleHeadFile{}, err
		}
		var head v8ModuleHeadFile
		if err := json.Unmarshal(data, &head); err != nil {
			return v8ModuleHeadFile{}, fmt.Errorf("v8: decode %s head %q: %w", module, path, err)
		}
		if head.SchemaVersion != v8SchemaVersion {
			return v8ModuleHeadFile{}, fmt.Errorf("v8: %s head schema=%d want %d", module, head.SchemaVersion, v8SchemaVersion)
		}
		if head.SessionID != key.SessionID {
			return v8ModuleHeadFile{}, fmt.Errorf("v8: %s head session %q != %q", module, head.SessionID, key.SessionID)
		}
		if head.ModuleID != module {
			return v8ModuleHeadFile{}, fmt.Errorf("v8: head module %q != %q", head.ModuleID, module)
		}
		want, err := v8HeadChecksum(head)
		if err != nil {
			return v8ModuleHeadFile{}, err
		}
		if head.Checksum != "" && want != head.Checksum {
			return v8ModuleHeadFile{}, fmt.Errorf("v8: %s head checksum mismatch", module)
		}
		return head, nil
	}
	head, err := read()
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return head, err
	}
	// 自愈读：先重读一次；仍失败才上报（校验失败可能来自并发替换的瞬时态）。
	if hook := v8ReadSelfHealHook; hook != nil {
		hook()
	}
	head, retryErr := read()
	if retryErr != nil {
		return v8ModuleHeadFile{}, err
	}
	return head, nil
}

// decodeV8HeadPayload 把模块 head 的 payload 解码到 target。
func decodeV8HeadPayload[T any](head v8ModuleHeadFile) (T, error) {
	var out T
	if len(head.Payload) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(head.Payload, &out); err != nil {
		return out, fmt.Errorf("v8: decode %s head payload: %w", head.ModuleID, err)
	}
	return out, nil
}

// v8CommitModuleHead 是通用模块提交：先确保布局，再原子发布模块 head。
// 调用方传入对应模块锁（stack/lifecycle/retention/subagent 等无独立数据
// 文件的模块 head 也用此入口）。
func (store *v8Store) v8CommitModuleHead(key Key, module v8Module, mu *sync.Mutex, commitID string, payload any) error {
	if _, err := store.ensureV8Guide(key); err != nil {
		return err
	}
	if commitID == "" {
		commitID = randomID()
	}
	mu.Lock()
	defer mu.Unlock()
	if _, err := store.publishV8ModuleHead(key, module, commitID, payload, time.Now().UTC()); err != nil {
		return err
	}
	return store.registerV8Module(key, module, store.v8ModulePath(key, module))
}

// v8ReadModuleHeadPayload 读取通用模块 head 并解码 payload；缺失时返回
// 零值 + fs.ErrNotExist。
func v8ReadModuleHeadPayload[T any](store *v8Store, key Key, module v8Module) (T, error) {
	var zero T
	headFile, err := store.readV8ModuleHeadFile(key, module)
	if err != nil {
		return zero, err
	}
	return decodeV8HeadPayload[T](headFile)
}

// deleteV8Module 删除模块 head 与对应数据目录（短窗口崩溃/审计模拟用；
// 数据通道由对应模块锁保护）。
func (store *v8Store) deleteV8Module(key Key, module v8Module) error {
	if err := os.Remove(store.v8ModulePath(key, module)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	switch module {
	case v8ModuleMessage:
		return os.RemoveAll(store.v8MessageDir(key))
	case v8ModuleEvent:
		return os.RemoveAll(store.v8EventDir(key))
	}
	return nil
}

// v8StackHead 是 plan/task/goal 栈模块 head（M3 fork 栈快照、T-M1-05 并发
// 用；当前只保存 head_seq 与批次锚点摘要）。
type v8StackHead struct {
	SessionID string `json:"session_id"`
	HeadSeq   uint64 `json:"head_seq"`
	// Items 是活跃批次条目摘要（item_message_id/batch_message_* 锚）。
	Items []v8StackItem `json:"items,omitempty"`
}

// v8StackItem 是栈条目摘要（history 锚，my_design §4）。
type v8StackItem struct {
	ItemID           string `json:"item_id"`
	ItemMessageID    string `json:"item_message_id"`
	BatchMessageFrom string `json:"batch_message_from,omitempty"`
	BatchMessageTo   string `json:"batch_message_to,omitempty"`
	Status           string `json:"status,omitempty"`
}
