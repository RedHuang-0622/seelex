package role

// permission.go 是**会话级角色权限登记**的持久化 + 进程内登记格。
//
// M9 从根权限适配器迁入；实现逐字保持，只把 `*Router` /
// `*jsonRepository` 耦合换成注入的窄端口 `ProjectLayoutPort`（根 store 实现）。
// 本包不 import 根 `store`：它只需要"项目目录在哪 + 这段代码在谁的互斥窗口里跑"，
// 不需要 Repository / storeEngine / 布局枚举。
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
// 并发：每个 (projectID, sessionID, role) 一格，**各自带锁**（permissionEntry.mu）。
// 索引 map 只在 indexMu 下增删成员，条目内容只在自己的锁下改——没有"一把大锁串行
// 所有角色"，也没有任何裸 map 被并发写。落盘另有一把**按项目分配**的文件锁，
// 锁序固定为「条目锁 → 文件锁」（读路径不嵌套：先读文件、再进条目锁），不会成环。

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
	Account          string    `json:"account,omitempty"`
	InheritMainAgent *bool     `json:"inherit_main_agent,omitempty"`
	SSHUserSpaces    []string  `json:"ssh_user_spaces,omitempty"`
	SSHUserSpacesSet bool      `json:"ssh_user_spaces_set,omitempty"`
	PermissionVector []string  `json:"permission_vector"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// permissionFile 是磁盘形态：sessions[sessionID][role] = 记录。
//
// RoleDefaults 是**角色级默认向量**（2026-10-23 新增）：键 = 角色名（agent /
// subagent / goalplan / teammate …），值 = 该角色在**所有会话**里的默认权限向量。
// 它与 sessions 分开放，是因为两者的键空间不同（一个按会话，一个按角色），混在
// 一张表里要靠哨兵 sessionID 才分得开，那是自找的 bug。
//
// 语义边界：这里存的是"默认"——会话里显式登记过（sessions 里那条）永远优先。
type permissionFile struct {
	Version      int                                        `json:"version"`
	Sessions     map[string]map[string]RolePermissionRecord `json:"sessions,omitempty"`
	RoleDefaults map[string]RolePermissionRecord            `json:"role_defaults,omitempty"`
}

// permissionKey 是登记格的坐标（项目维度也进键：同一份 store 服务所有项目）。
type permissionKey struct {
	projectID string
	sessionID string
	role      string
}

// permissionEntry 是**一个角色**的登记格：自己的锁、自己的已加载标记与副本。
//
// 为什么缓存一份：角色权限在回合内被反复读（每次工具判定都要），逐次读盘既慢又
// 让"同一个值"在两次读之间可能被别的写者改掉（判定链会前后不一致）。缓存只在
// 本进程是权威的——数据根按设计是非共享的（多进程写入要显式开
// `limits.session_storage.allow_multi_process`，那是用户自担的形态）。
type permissionEntry struct {
	mu     sync.Mutex
	loaded bool
	record RolePermissionRecord
}

// ProjectLayoutPort 提供项目级文件的目录解析与互斥窗口（由根 store 实现）。
// 回调返回前窗口必须保持，读-改-写都在窗口内完成。
//
// 为什么只要"目录 + 窗口"：本包的文件名、JSON 形状、协议都是自己的判据，唯一
// 需要外部提供的是"这个 projectID 的项目目录在哪"以及"切换项目时不要与旧的
// 读写并发"。把 Repository 整个塞进来会让领域包拿到它用不到的落盘能力。
type ProjectLayoutPort interface {
	WithProjectDir(projectID string, fn func(dir string) error) error
}

// PermissionStore 读写会话级角色权限登记。
//
// layout 为 nil 时读退化为"空登记"、写显式失败（没装配存储就不假装写成功）。
type PermissionStore struct {
	layout ProjectLayoutPort

	// indexMu 只保护 entries 的成员增删；条目内容不在这里保护。
	indexMu sync.RWMutex
	entries map[permissionKey]*permissionEntry

	// fileMu 按项目分配落盘互斥（sync.Map: projectID -> *sync.Mutex）。
	fileMu sync.Map
}

// NewPermissionStore 构造会话级角色权限登记存储。
func NewPermissionStore(layout ProjectLayoutPort) *PermissionStore {
	return &PermissionStore{layout: layout, entries: map[permissionKey]*permissionEntry{}}
}

// Vectors 返回该会话下**所有**已登记角色的权限向量（一次读盘，会话加载点用）。
//
// 只含"显式定过"的角色：缺的角色由调用方按四级链路解析——默认值属于判据层，
// 存储不猜。
func (store *PermissionStore) Vectors(projectID, sessionID string) (map[string][]string, error) {
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
func (store *PermissionStore) Vector(projectID, sessionID, role string) ([]string, bool, error) {
	sessionID, role = strings.TrimSpace(sessionID), strings.TrimSpace(role)
	if sessionID == "" || role == "" {
		return nil, false, fmt.Errorf("session storage: role permissions require both session id and role")
	}
	entry := store.entry(permissionKey{projectID: projectID, sessionID: sessionID, role: role})
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
func (store *PermissionStore) Put(projectID, sessionID, role string, vector []string) (RolePermissionRecord, error) {
	sessionID, role = strings.TrimSpace(sessionID), strings.TrimSpace(role)
	if sessionID == "" || role == "" {
		return RolePermissionRecord{}, fmt.Errorf("session storage: role permissions require both session id and role")
	}
	key := permissionKey{projectID: projectID, sessionID: sessionID, role: role}
	entry := store.entry(key)
	var record RolePermissionRecord
	// 锁序：先条目锁（同一个角色的并发写在这里排队），再文件锁（mutate 内）。
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if err := store.mutate(projectID, func(file *permissionFile) {
		sessions := file.Sessions[sessionID]
		if sessions == nil {
			sessions = map[string]RolePermissionRecord{}
			file.Sessions[sessionID] = sessions
		}
		record = sessions[role]
		record.SessionID = sessionID
		record.Role = role
		record.PermissionVector = normalizedRoleVector(vector)
		record.UpdatedAt = time.Now().UTC()
		sessions[role] = record
	}); err != nil {
		return RolePermissionRecord{}, err
	}
	entry.loaded = true
	entry.record = record
	return record, nil
}

// Drop 删除单个角色的登记（= 下一次加载重新按链路解析）。
func (store *PermissionStore) Drop(projectID, sessionID, role string) error {
	sessionID, role = strings.TrimSpace(sessionID), strings.TrimSpace(role)
	if sessionID == "" || role == "" {
		return fmt.Errorf("session storage: role permissions require both session id and role")
	}
	entry := store.entry(permissionKey{projectID: projectID, sessionID: sessionID, role: role})
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if err := store.mutate(projectID, func(file *permissionFile) {
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
func (store *PermissionStore) DropSession(projectID, sessionID string) error {
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
	return store.mutate(projectID, func(file *permissionFile) {
		delete(file.Sessions, sessionID)
	})
}

// Defaults 返回项目下**所有**角色级默认向量（键 = 角色名）。
//
// 与 Vectors 的分工：Vectors 是"会话里显式登记过什么"，Defaults 是"这个角色在所有
// 会话里的默认是什么"。读侧不猜，缺的角色由调用方按四级链路解析。
func (store *PermissionStore) Defaults(projectID string) (map[string][]string, error) {
	file, err := store.snapshot(projectID)
	if err != nil {
		return nil, err
	}
	defaults := map[string][]string{}
	for role, record := range file.RoleDefaults {
		defaults[role] = normalizedRoleVector(record.PermissionVector)
	}
	return defaults, nil
}

// Default 读单个角色的默认向量；第二个返回值报告"是否显式定过默认"。
func (store *PermissionStore) Default(projectID, role string) ([]string, bool, error) {
	role = strings.TrimSpace(role)
	if role == "" {
		return nil, false, fmt.Errorf("session storage: role default requires a role")
	}
	file, err := store.snapshot(projectID)
	if err != nil {
		return nil, false, err
	}
	record, ok := file.RoleDefaults[role]
	if !ok {
		return nil, false, nil
	}
	// 显式空集必须交回非 nil 空切片（同 Vector 的理由）。
	vector := make([]string, len(record.PermissionVector))
	copy(vector, normalizedRoleVector(record.PermissionVector))
	return vector, true, nil
}

// RoleAccount 读取角色默认使用的账号名；第二个返回值报告是否显式设置过。
func (store *PermissionStore) RoleAccount(projectID, role string) (string, bool, error) {
	role = strings.TrimSpace(role)
	if role == "" {
		return "", false, fmt.Errorf("session storage: role account requires a role")
	}
	file, err := store.snapshot(projectID)
	if err != nil {
		return "", false, err
	}
	record, ok := file.RoleDefaults[role]
	if !ok || strings.TrimSpace(record.Account) == "" {
		return "", false, nil
	}
	return strings.TrimSpace(record.Account), true, nil
}

// SetRoleAccount 设置角色默认账号。账号与权限默认共用同一条记录，
// 因此写入账号时必须保留已有权限向量。
func (store *PermissionStore) SetRoleAccount(projectID, role, account string) error {
	role, account = strings.TrimSpace(role), strings.TrimSpace(account)
	if role == "" || account == "" {
		return fmt.Errorf("session storage: role account requires role and account")
	}
	return store.mutate(projectID, func(file *permissionFile) {
		if file.RoleDefaults == nil {
			file.RoleDefaults = map[string]RolePermissionRecord{}
		}
		record := file.RoleDefaults[role]
		record.Role = role
		record.Account = account
		record.PermissionVector = normalizedRoleVector(record.PermissionVector)
		record.UpdatedAt = time.Now().UTC()
		file.RoleDefaults[role] = record
	})
}

// RoleInheritance 读角色是否继承 mainagent 的默认权限。第二个返回值报告是否显式
// 设置过；subagent / teammate 的“未设置即继承”由上层口径决定。
func (store *PermissionStore) RoleInheritance(projectID, role string) (bool, bool, error) {
	role = strings.TrimSpace(role)
	if role == "" {
		return false, false, fmt.Errorf("session storage: role inheritance requires a role")
	}
	file, err := store.snapshot(projectID)
	if err != nil {
		return false, false, err
	}
	record, ok := file.RoleDefaults[role]
	if !ok || record.InheritMainAgent == nil {
		return false, false, nil
	}
	return *record.InheritMainAgent, true, nil
}

// SetRoleInheritance 设置角色默认权限是否继承 mainagent；不影响会话显式登记。
func (store *PermissionStore) SetRoleInheritance(projectID, role string, inherit bool) error {
	role = strings.TrimSpace(role)
	if role == "" {
		return fmt.Errorf("session storage: role inheritance requires a role")
	}
	return store.mutate(projectID, func(file *permissionFile) {
		if file.RoleDefaults == nil {
			file.RoleDefaults = map[string]RolePermissionRecord{}
		}
		record := file.RoleDefaults[role]
		record.Role = role
		record.InheritMainAgent = &inherit
		record.PermissionVector = normalizedRoleVector(record.PermissionVector)
		record.UpdatedAt = time.Now().UTC()
		file.RoleDefaults[role] = record
	})
}

// RoleSSHUserSpaces 读取 SSH 能力可访问的远端路径前缀。
func (store *PermissionStore) RoleSSHUserSpaces(projectID, role string) ([]string, error) {
	role = strings.TrimSpace(role)
	if role == "" {
		return nil, fmt.Errorf("session storage: role user spaces require a role")
	}
	file, err := store.snapshot(projectID)
	if err != nil {
		return nil, err
	}
	return append([]string{}, file.RoleDefaults[role].SSHUserSpaces...), nil
}

// SetRoleSSHUserSpaces 写 SSH 路径前缀。空切片表示显式不放行任何路径；
// 规范化与校验由调用方完成，本层保持原字节语义。
func (store *PermissionStore) SetRoleSSHUserSpaces(projectID, role string, paths []string) error {
	role = strings.TrimSpace(role)
	if role == "" {
		return fmt.Errorf("session storage: role user spaces require a role")
	}
	return store.mutate(projectID, func(file *permissionFile) {
		if file.RoleDefaults == nil {
			file.RoleDefaults = map[string]RolePermissionRecord{}
		}
		record := file.RoleDefaults[role]
		record.Role = role
		record.SSHUserSpaces = append([]string{}, paths...)
		record.SSHUserSpacesSet = true
		record.PermissionVector = normalizedRoleVector(record.PermissionVector)
		record.UpdatedAt = time.Now().UTC()
		file.RoleDefaults[role] = record
	})
}

// RoleEffectiveSSHUserSpaces 返回某会话中角色生效的 SSH 路径白名单：
// 会话显式保存过覆盖时用它，否则回落到项目级角色默认。
func (store *PermissionStore) RoleEffectiveSSHUserSpaces(projectID, sessionID, role string) ([]string, error) {
	sessionID, role = strings.TrimSpace(sessionID), strings.TrimSpace(role)
	if sessionID == "" || role == "" {
		return nil, fmt.Errorf("session storage: role user spaces require session and role")
	}
	file, err := store.snapshot(projectID)
	if err != nil {
		return nil, err
	}
	if record, ok := file.Sessions[sessionID][role]; ok && record.SSHUserSpacesSet {
		return append([]string{}, record.SSHUserSpaces...), nil
	}
	return append([]string{}, file.RoleDefaults[role].SSHUserSpaces...), nil
}

// SetRoleSessionSSHUserSpaces 写入某会话中单个角色的 SSH 路径覆盖。
// 空切片是合法的显式覆盖，表示该会话不给这个角色任何远端根外路径。
func (store *PermissionStore) SetRoleSessionSSHUserSpaces(projectID, sessionID, role string, paths []string) error {
	sessionID, role = strings.TrimSpace(sessionID), strings.TrimSpace(role)
	if sessionID == "" || role == "" {
		return fmt.Errorf("session storage: role user spaces require session and role")
	}
	return store.mutate(projectID, func(file *permissionFile) {
		if file.Sessions == nil {
			file.Sessions = map[string]map[string]RolePermissionRecord{}
		}
		sessions := file.Sessions[sessionID]
		if sessions == nil {
			sessions = map[string]RolePermissionRecord{}
			file.Sessions[sessionID] = sessions
		}
		record := sessions[role]
		record.SessionID = sessionID
		record.Role = role
		record.SSHUserSpaces = append([]string{}, paths...)
		record.SSHUserSpacesSet = true
		record.PermissionVector = normalizedRoleVector(record.PermissionVector)
		record.UpdatedAt = time.Now().UTC()
		sessions[role] = record
	})
}

// PutDefault 写入一个角色的默认向量（空集 = 该角色默认什么都不给，不删记录）。
func (store *PermissionStore) PutDefault(projectID, role string, vector []string) (RolePermissionRecord, error) {
	role = strings.TrimSpace(role)
	if role == "" {
		return RolePermissionRecord{}, fmt.Errorf("session storage: role default requires a role")
	}
	var record RolePermissionRecord
	if err := store.mutate(projectID, func(file *permissionFile) {
		if file.RoleDefaults == nil {
			file.RoleDefaults = map[string]RolePermissionRecord{}
		}
		record = file.RoleDefaults[role]
		record.Role = role
		record.PermissionVector = normalizedRoleVector(vector)
		record.UpdatedAt = time.Now().UTC()
		file.RoleDefaults[role] = record
	}); err != nil {
		return RolePermissionRecord{}, err
	}
	return record, nil
}

// DropDefault 删除一个角色的默认向量（= 该角色回到链路解析）。
func (store *PermissionStore) DropDefault(projectID, role string) error {
	role = strings.TrimSpace(role)
	if role == "" {
		return fmt.Errorf("session storage: role default requires a role")
	}
	return store.mutate(projectID, func(file *permissionFile) {
		delete(file.RoleDefaults, role)
	})
}

// sessionKeys 枚举该会话当前已建格的坐标（索引读锁内复制）。
func (store *PermissionStore) sessionKeys(projectID, sessionID string) []permissionKey {
	store.indexMu.RLock()
	defer store.indexMu.RUnlock()
	keys := make([]permissionKey, 0, len(store.entries))
	for key := range store.entries {
		if key.projectID == projectID && key.sessionID == sessionID {
			keys = append(keys, key)
		}
	}
	return keys
}

// entry 取（必要时建）一个登记格；索引只在 indexMu 下改。
func (store *PermissionStore) entry(key permissionKey) *permissionEntry {
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
	created := &permissionEntry{}
	store.entries[key] = created
	return created
}

// loadEntry 把该角色从盘上装进登记格（已装过则直接返回）。
//
// 锁序：**先把文件读出来**（不持条目锁，避免「条目 → 文件 → 条目」成环），再进
// 条目锁判重。并发两个读者可能各读一次盘，代价是多余的 IO，换的是不会死锁。
func (store *PermissionStore) loadEntry(entry *permissionEntry, projectID, sessionID, role string) error {
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
func (store *PermissionStore) snapshot(projectID string) (permissionFile, error) {
	if store == nil || store.layout == nil {
		return permissionFile{Version: rolePermissionLayoutVer, Sessions: map[string]map[string]RolePermissionRecord{}}, nil
	}
	lock := store.fileLock(projectID)
	lock.Lock()
	defer lock.Unlock()
	var file permissionFile
	err := store.layout.WithProjectDir(projectID, func(dir string) error {
		loaded, err := readPermissionFile(dir)
		if err != nil {
			return err
		}
		file = loaded
		return nil
	})
	if err != nil {
		return permissionFile{}, err
	}
	return file, nil
}

// mutate 在项目文件锁内做一次"读-改-写"（一个项目一份文件，跨角色共用一把文件锁
// 是必须的：否则两个角色并发写会互相覆盖整份文件）。
func (store *PermissionStore) mutate(projectID string, apply func(*permissionFile)) error {
	if store == nil || store.layout == nil {
		return errors.New("session storage: role permissions are not assembled")
	}
	lock := store.fileLock(projectID)
	lock.Lock()
	defer lock.Unlock()
	return store.layout.WithProjectDir(projectID, func(dir string) error {
		file, err := readPermissionFile(dir)
		if err != nil {
			return err
		}
		apply(&file)
		if file.Sessions == nil {
			file.Sessions = map[string]map[string]RolePermissionRecord{}
		}
		return writePermissionFile(dir, file)
	})
}

// fileLock 按项目取落盘锁（不同项目可并发）。
func (store *PermissionStore) fileLock(projectID string) *sync.Mutex {
	if lock, ok := store.fileMu.Load(projectID); ok {
		return lock.(*sync.Mutex)
	}
	lock, _ := store.fileMu.LoadOrStore(projectID, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

// readPermissionFile 读项目级角色权限文件；缺文件 = 空登记（不是错误）。
func readPermissionFile(directory string) (permissionFile, error) {
	empty := permissionFile{Version: rolePermissionLayoutVer, Sessions: map[string]map[string]RolePermissionRecord{}}
	payload, err := os.ReadFile(filepath.Join(directory, rolePermissionFileName))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return empty, nil
		}
		return permissionFile{}, err
	}
	if len(payload) == 0 {
		return empty, nil
	}
	decoded := permissionFile{}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		// 坏文件按"空登记"处理：读不出来时让角色回落到链路解析，比让整个会话加载
		// 失败更安全（失败方向是收紧，不是放开）；坏文件不在这里自动覆盖，留给
		// 下一次写回。
		return empty, nil
	}
	if decoded.Sessions == nil {
		decoded.Sessions = map[string]map[string]RolePermissionRecord{}
	}
	if decoded.RoleDefaults == nil {
		decoded.RoleDefaults = map[string]RolePermissionRecord{}
	}
	decoded.Version = rolePermissionLayoutVer
	return decoded, nil
}

// writePermissionFile 原子写项目级角色权限文件（文件 0600，目录 0700）。
func writePermissionFile(directory string, file permissionFile) error {
	file.Version = rolePermissionLayoutVer
	payload, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, rolePermissionDirMode); err != nil {
		return err
	}
	return storeroot.WriteAtomic(filepath.Join(directory, rolePermissionFileName), payload, rolePermissionFileMode)
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
