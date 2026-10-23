package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

// role_permission.go 是**会话级角色权限登记**的持久化 + 进程内登记格。
//
// 生态位：角色（teammate / 群聊成员）的权限向量在会话里被显式定过之后必须记住，
// 跨重启、跨会话切换都还在。它是"用户/派发方给这个角色定了什么"，不是执行事实，
// 因此：
//   - 不进 SessionRecord（record 通道在建回合结束时整体重建，会把选择覆盖掉）；
//   - 不进会话 message/event 通道（权限不是对话内容，重放它等于把权限写进历史）；
//   - 独立成一份项目级 JSON（与 `session-meta.json` 同级同纪律：一个项目一份文件），
//     键是 (sessionID, role)。
//
// 三态纪律（与口令的"写入/清除/保持"同款）：一条登记的**存在**就是"显式定过"，
// 哪怕向量是空集——空集意味着"这个角色什么能力都不给"，绝不能被当成"没定过"而
// 回落到默认链（那是权限放大方向）。所以删除键与写入空集是两件事：`Put` 永远写
// 记录，只有 `Drop` 删记录。
//
// 并发：每个 (projectID, sessionID, role) 一格，**各自带锁**（rolePermissionEntry.mu）。
// 索引 map 只在 indexMu 下增删成员，条目内容只在自己的锁下改——没有"一把大锁串行
// 所有角色"，也没有任何裸 map 被并发写。落盘另有一把**按项目分配**的文件锁，
// 锁序固定为「条目锁 → 文件锁」（读路径不嵌套：先读文件、再进条目锁），不会成环。

const (
	rolePermissionFileName  = "role-permissions.json"
	rolePermissionLayoutVer = 1
	rolePermissionFileMode  = 0o600
	rolePermissionDirMode   = 0o700
)

// RolePermissionRecord 是一条会话级角色权限登记：键 = (sessionID, role)。
//
// PermissionVector 没有 `omitempty`：写空集必须序列化成 `[]` 而不是丢字段，
// 否则"显式不给任何能力"与"没有这条记录"在盘上就分不开了。
type RolePermissionRecord struct {
	SessionID        string    `json:"session_id"`
	Role             string    `json:"role"`
	PermissionVector []string  `json:"permission_vector"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// rolePermissionFile 是磁盘形态：sessions[sessionID][role] = 记录。
type rolePermissionFile struct {
	Version  int                                        `json:"version"`
	Sessions map[string]map[string]RolePermissionRecord `json:"sessions,omitempty"`
}

// rolePermissionKey 是登记格的坐标（项目维度也进键：同一份 store 服务所有项目）。
type rolePermissionKey struct {
	projectID string
	sessionID string
	role      string
}

// rolePermissionEntry 是**一个角色**的登记格：自己的锁、自己的已加载标记与副本。
//
// 为什么缓存一份：角色权限在回合内被反复读（每次工具判定都要），逐次读盘既慢又
// 让"同一个值"在两次读之间可能被别的写者改掉（判定链会前后不一致）。缓存只在
// 本进程是权威的——数据根按设计是非共享的（多进程写入要显式开
// `limits.session_storage.allow_multi_process`，那是用户自担的形态）。
type rolePermissionEntry struct {
	mu     sync.Mutex
	loaded bool
	record RolePermissionRecord
}

// RolePermissionStore 读写会话级角色权限登记。
//
// router 为 nil 时读退化为"空登记"、写显式失败（没装配存储就不假装写成功）。
type RolePermissionStore struct {
	router *Router

	// indexMu 只保护 entries 的成员增删；条目内容不在这里保护。
	indexMu sync.RWMutex
	entries map[rolePermissionKey]*rolePermissionEntry

	// fileMu 按项目分配落盘互斥（sync.Map: projectID -> *sync.Mutex）。
	fileMu sync.Map
}

// NewRolePermissionStore 构造会话级角色权限登记存储。
func NewRolePermissionStore(router *Router) *RolePermissionStore {
	return &RolePermissionStore{router: router, entries: map[rolePermissionKey]*rolePermissionEntry{}}
}

// Vectors 返回该会话下**所有**已登记角色的权限向量（一次读盘，会话加载点用）。
//
// 只含"显式定过"的角色：缺的角色由调用方按四级链路解析——默认值属于判据层，
// 存储不猜。
func (store *RolePermissionStore) Vectors(projectID, sessionID string) (map[string][]string, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("session storage: role permissions require a session id")
	}
	file, err := store.snapshot(projectID)
	if err != nil {
		return nil, err
	}
	vectors := map[string][]string{}
	for role, record := range file.Sessions[sessionID] {
		vectors[role] = normalizedRoleVector(record.PermissionVector)
	}
	return vectors, nil
}

// Vector 读单个角色的权限向量；第二个返回值报告"是否显式登记过"（false 即从未
// 定过，调用方按链路解析）。
func (store *RolePermissionStore) Vector(projectID, sessionID, role string) ([]string, bool, error) {
	sessionID, role = strings.TrimSpace(sessionID), strings.TrimSpace(role)
	if sessionID == "" || role == "" {
		return nil, false, fmt.Errorf("session storage: role permissions require both session id and role")
	}
	entry := store.entry(rolePermissionKey{projectID: projectID, sessionID: sessionID, role: role})
	if err := store.loadEntry(entry, projectID, sessionID, role); err != nil {
		return nil, false, err
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if !entry.loaded {
		return nil, false, nil
	}
	// make 而非 append(nil, …)：显式空集必须交回**非 nil 空切片**，否则调用方
	// 分不清"没有登记"（ok=false）与"登记了但什么都不给"（ok=true, 空集）。
	vector := make([]string, len(entry.record.PermissionVector))
	copy(vector, entry.record.PermissionVector)
	return vector, true, nil
}

// Put 写入（或覆盖）一个角色的权限向量，返回落盘后的记录。
//
// vector 为空集是合法输入，含义是"这个角色显式不给任何能力"——它**不会**删除记录
// （删除请用 Drop）。
func (store *RolePermissionStore) Put(projectID, sessionID, role string, vector []string) (RolePermissionRecord, error) {
	sessionID, role = strings.TrimSpace(sessionID), strings.TrimSpace(role)
	if sessionID == "" || role == "" {
		return RolePermissionRecord{}, fmt.Errorf("session storage: role permissions require both session id and role")
	}
	key := rolePermissionKey{projectID: projectID, sessionID: sessionID, role: role}
	entry := store.entry(key)
	record := RolePermissionRecord{
		SessionID:        sessionID,
		Role:             role,
		PermissionVector: normalizedRoleVector(vector),
		UpdatedAt:        time.Now().UTC(),
	}
	// 锁序：先条目锁（同一个角色的并发写在这里排队），再文件锁（mutate 内）。
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if err := store.mutate(projectID, func(file *rolePermissionFile) {
		sessions := file.Sessions[sessionID]
		if sessions == nil {
			sessions = map[string]RolePermissionRecord{}
			file.Sessions[sessionID] = sessions
		}
		sessions[role] = record
	}); err != nil {
		return RolePermissionRecord{}, err
	}
	entry.loaded = true
	entry.record = record
	return record, nil
}

// Drop 删除单个角色的登记（= 下一次加载重新按链路解析）。
func (store *RolePermissionStore) Drop(projectID, sessionID, role string) error {
	sessionID, role = strings.TrimSpace(sessionID), strings.TrimSpace(role)
	if sessionID == "" || role == "" {
		return fmt.Errorf("session storage: role permissions require both session id and role")
	}
	entry := store.entry(rolePermissionKey{projectID: projectID, sessionID: sessionID, role: role})
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if err := store.mutate(projectID, func(file *rolePermissionFile) {
		sessions := file.Sessions[sessionID]
		if sessions == nil {
			return
		}
		delete(sessions, role)
		if len(sessions) == 0 {
			delete(file.Sessions, sessionID)
		}
	}); err != nil {
		return err
	}
	entry.loaded = false
	entry.record = RolePermissionRecord{}
	return nil
}

// DropSession 清掉一个会话的全部角色登记（会话删除时用；不做任何"保留最近一条"
// 之类的折衷——会话没了，登记就没有归属）。
func (store *RolePermissionStore) DropSession(projectID, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session storage: role permissions require a session id")
	}
	// 先使该会话的条目失效（顺序：条目锁 → 文件锁，与写路径同一锁序），再落盘。
	for _, key := range store.sessionKeys(projectID, sessionID) {
		if entry := store.entry(key); entry != nil {
			entry.mu.Lock()
			entry.loaded = false
			entry.record = RolePermissionRecord{}
			entry.mu.Unlock()
		}
	}
	return store.mutate(projectID, func(file *rolePermissionFile) {
		delete(file.Sessions, sessionID)
	})
}

// sessionKeys 枚举该会话当前已建格的坐标（索引读锁内复制）。
func (store *RolePermissionStore) sessionKeys(projectID, sessionID string) []rolePermissionKey {
	store.indexMu.RLock()
	defer store.indexMu.RUnlock()
	keys := make([]rolePermissionKey, 0, len(store.entries))
	for key := range store.entries {
		if key.projectID == projectID && key.sessionID == sessionID {
			keys = append(keys, key)
		}
	}
	return keys
}

// entry 取（必要时建）一个登记格；索引只在 indexMu 下改。
func (store *RolePermissionStore) entry(key rolePermissionKey) *rolePermissionEntry {
	store.indexMu.RLock()
	existing, ok := store.entries[key]
	store.indexMu.RUnlock()
	if ok {
		return existing
	}
	store.indexMu.Lock()
	defer store.indexMu.Unlock()
	if existing, ok := store.entries[key]; ok {
		return existing
	}
	created := &rolePermissionEntry{}
	store.entries[key] = created
	return created
}

// loadEntry 把该角色从盘上装进登记格（已装过则直接返回）。
//
// 锁序：**先把文件读出来**（不持条目锁，避免「条目 → 文件 → 条目」成环），再进
// 条目锁判重。并发两个读者可能各读一次盘，代价是多余的 IO，换的是不会死锁。
func (store *RolePermissionStore) loadEntry(entry *rolePermissionEntry, projectID, sessionID, role string) error {
	entry.mu.Lock()
	loaded := entry.loaded
	entry.mu.Unlock()
	if loaded {
		return nil
	}
	file, err := store.snapshot(projectID)
	if err != nil {
		return err
	}
	record, ok := file.Sessions[sessionID][role]
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.loaded {
		return nil
	}
	entry.loaded = ok
	if ok {
		record.PermissionVector = normalizedRoleVector(record.PermissionVector)
		entry.record = record
	}
	return nil
}

// snapshot 读一份项目文件的副本（文件锁内读，读完即放锁）。
func (store *RolePermissionStore) snapshot(projectID string) (rolePermissionFile, error) {
	if store == nil || store.router == nil {
		return rolePermissionFile{Version: rolePermissionLayoutVer, Sessions: map[string]map[string]RolePermissionRecord{}}, nil
	}
	lock := store.fileLock(projectID)
	lock.Lock()
	defer lock.Unlock()
	var file rolePermissionFile
	err := store.router.withRepositoryAt(projectID, func(repository Repository, resolvedProjectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: role permissions require session layout")
		}
		loaded, err := layout.readRolePermissionLayout(resolvedProjectID)
		if err != nil {
			return err
		}
		file = loaded
		return nil
	})
	if err != nil {
		return rolePermissionFile{}, err
	}
	return file, nil
}

// mutate 在项目文件锁内做一次"读-改-写"（一个项目一份文件，跨角色共用一把文件锁
// 是必须的：否则两个角色并发写会互相覆盖整份文件）。
func (store *RolePermissionStore) mutate(projectID string, apply func(*rolePermissionFile)) error {
	if store == nil || store.router == nil {
		return errors.New("session storage: role permissions are not assembled")
	}
	lock := store.fileLock(projectID)
	lock.Lock()
	defer lock.Unlock()
	return store.router.withRepositoryAt(projectID, func(repository Repository, resolvedProjectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: role permissions require session layout")
		}
		file, err := layout.readRolePermissionLayout(resolvedProjectID)
		if err != nil {
			return err
		}
		apply(&file)
		if file.Sessions == nil {
			file.Sessions = map[string]map[string]RolePermissionRecord{}
		}
		return layout.writeRolePermissionLayout(resolvedProjectID, file)
	})
}

// fileLock 按项目取落盘锁（不同项目可并发）。
func (store *RolePermissionStore) fileLock(projectID string) *sync.Mutex {
	if lock, ok := store.fileMu.Load(projectID); ok {
		return lock.(*sync.Mutex)
	}
	lock, _ := store.fileMu.LoadOrStore(projectID, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

// readRolePermissionLayout 读项目级角色权限文件；缺文件 = 空登记（不是错误）。
func (repository *jsonRepository) readRolePermissionLayout(projectID string) (rolePermissionFile, error) {
	empty := rolePermissionFile{Version: rolePermissionLayoutVer, Sessions: map[string]map[string]RolePermissionRecord{}}
	payload, err := os.ReadFile(repository.rolePermissionPath(projectID))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return empty, nil
		}
		return rolePermissionFile{}, err
	}
	if len(payload) == 0 {
		return empty, nil
	}
	decoded := rolePermissionFile{}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		// 坏文件按"空登记"处理：读不出来时让角色回落到链路解析，比让整个会话加载
		// 失败更安全（失败方向是收紧，不是放开）；坏文件不在这里自动覆盖，留给
		// 下一次写回。
		return empty, nil
	}
	if decoded.Sessions == nil {
		decoded.Sessions = map[string]map[string]RolePermissionRecord{}
	}
	decoded.Version = rolePermissionLayoutVer
	return decoded, nil
}

// writeRolePermissionLayout 原子写项目级角色权限文件（文件 0600，目录 0700）。
func (repository *jsonRepository) writeRolePermissionLayout(projectID string, file rolePermissionFile) error {
	file.Version = rolePermissionLayoutVer
	payload, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	directory := repository.projectDir(projectID)
	if err := os.MkdirAll(directory, rolePermissionDirMode); err != nil {
		return err
	}
	return storeroot.WriteAtomic(repository.rolePermissionPath(projectID), payload, rolePermissionFileMode)
}

// rolePermissionPath 是项目目录下的角色权限文件名。
func (repository *jsonRepository) rolePermissionPath(projectID string) string {
	return filepath.Join(repository.projectDir(projectID), rolePermissionFileName)
}

// normalizedRoleVector 归一一份向量：去空白、丢空项、保序去重，且**永不返回 nil**
// （显式空集在内存与盘上都是 `[]`，调用方不必区分 nil 与空）。
func normalizedRoleVector(vector []string) []string {
	normalized := make([]string, 0, len(vector))
	seen := make(map[string]struct{}, len(vector))
	for _, entry := range vector {
		tag := strings.TrimSpace(entry)
		if tag == "" {
			continue
		}
		if _, dup := seen[tag]; dup {
			continue
		}
		seen[tag] = struct{}{}
		normalized = append(normalized, tag)
	}
	return normalized
}
