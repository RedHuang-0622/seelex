// 群聊角色会话的根包兼容面（M9）。
//
// 唯一实现在 store/role：draft WAL（append-only、文件存在即待同步、sync 成功
// 后立即删除、崩溃残尾截断）、角色会话子树路径与提交身份、permission 登记。
// 本文件只保留四类东西：
//   - 同身份 alias（类型与角色常量）；
//   - 组合根 lifecycle：建角色子树的装配步骤（建目录 → message 提交 →
//     role_session 结构化事件 → join_seq_id）；
//   - 观察面（RoleSnapshot / RoleWireSnapshot）：需要根包既有的 message、event、
//     compact 读通道，领域包不持这些机制；
//   - 向 store/role 的单层转发与 main message 端口适配（sequencer 唯一写入口）。
package store

import (
	"errors"
	"io/fs"
	"os"

	storerole "github.com/RedHuang-0622/seelex/store/role"
	storesession "github.com/RedHuang-0622/seelex/store/session"
)

type RoleSnapshot = storerole.Snapshot
type RoleWireMessage = storerole.WireMessage
type RoleWireSnapshot = storerole.WireSnapshot

// roleSessionEngine 返回角色会话的引擎与 key：main 复用当前引擎；非 main 以
// 角色子树为根装配**只含机制**的子引擎。子树不再递归装配 artifact/context/
// teamwork/role —— 那是同型子引擎的复制（M10 归零）。
func roleSessionEngine(store *storeEngine, mainKey Key, roleName, roleSessionID string) (*storeEngine, Key) {
	if roleName == storerole.RoleMain {
		return store, mainKey
	}
	root := storerole.SessionRoot(store.sessionRoot(mainKey), roleName, roleSessionID)
	return newSubsessionEngine(root, store.settings), Key{SessionID: roleSessionID}
}

// roleSessionRoot 返回角色会话子树的物理根（路径规则唯一实现在 store/role）。
func roleSessionRoot(store *storeEngine, mainKey Key, roleName, roleSessionID string) string {
	return storerole.SessionRoot(store.sessionRoot(mainKey), roleName, roleSessionID)
}

// mainSessionPort 把根 store 的 message 通道适配成 role.SessionPort：sequencer
// 唯一写 main message 的入口，floor 与 head 在同一提交内发布。
type mainSessionPort struct {
	store *storeEngine
	key   Key
}

func (port mainSessionPort) MessageHead() (storesession.MessageHead, error) {
	return port.store.readMessageHead(port.key)
}

func (port mainSessionPort) CommitSync(commitID string, rows []Event, floor *storesession.Floor) (storesession.MessageHead, error) {
	return port.store.messageCommitSync(port.key, commitID, rows, floor)
}

// syncRoleDraft 是 sequencer 的存储侧同步入口：读该角色 draft → 排序 → 幂等
// 判定 → append 进 main message 并原子发布 head + floor → 成功后删除 draft。
// 判据与写序全部在 store/role；本函数只把 main message 通道接成端口。
func syncRoleDraft(store *storeEngine, mainKey Key, roleName, roleSessionID string, order []string) (storerole.SyncResult, error) {
	roleStore, roleKey := roleSessionEngine(store, mainKey, roleName, roleSessionID)
	return storerole.SyncDraft(
		roleStore.sessionRoot(roleKey),
		roleName, roleSessionID, order,
		mainSessionPort{store: store, key: mainKey},
	)
}

// createRoleSession 创建非 main 角色会话（message/event/metadata 同构空现场），
// 并写 join_seq_id。main 角色不创建独立子树。这是组合根 lifecycle：目录与
// 提交顺序由这里定，draft/permission/路径判据都不在这里。
func createRoleSession(store *storeEngine, mainKey Key, roleName, roleSessionID string, joinSeq uint64) (*storeEngine, Key, error) {
	if roleName == "" || roleSessionID == "" {
		return nil, Key{}, errors.New("session storage: role_name and role_session_id are required")
	}
	if roleName == storerole.RoleMain {
		return nil, Key{}, errors.New("session storage: main role session is the main session itself")
	}
	if _, err := store.ensureLayoutGuide(mainKey); err != nil {
		return nil, Key{}, err
	}
	root := roleSessionRoot(store, mainKey, roleName, roleSessionID)
	childStore := newSubsessionEngine(root, store.settings)
	childKey := Key{SessionID: roleSessionID}
	if childStore.sessionExists(childKey) {
		return nil, Key{}, errors.New("session storage: role session already exists")
	}
	commitID := storerole.SessionCommitID(roleName, roleSessionID)
	if _, err := childStore.messageCommit(childKey, commitID, nil); err != nil {
		_ = os.RemoveAll(root)
		return nil, Key{}, err
	}
	if _, err := childStore.structuralEventCommit(childKey, commitID, []structuralEvent{{
		Kind:    structuralEventRoleSession,
		Payload: storerole.JoinPayload(mainKey.SessionID, roleName, roleSessionID, joinSeq),
	}}); err != nil {
		_ = os.RemoveAll(root)
		return nil, Key{}, err
	}
	if err := childStore.setRoleLifecycle(childKey, joinSeq, nil); err != nil {
		_ = os.RemoveAll(root)
		return nil, Key{}, err
	}
	return childStore, childKey, nil
}

// readRoleSessionRows 读取角色会话备份行（冷恢复/审计用）。main 角色读主会话。
func readRoleSessionRows(store *storeEngine, mainKey Key, roleName, roleSessionID string) ([]Event, error) {
	roleStore, roleKey := roleSessionEngine(store, mainKey, roleName, roleSessionID)
	return roleStore.readAllRows(roleKey)
}

// readRoleSnapshot 汇总角色会话观察面（只读）。
func readRoleSnapshot(store *storeEngine, mainKey Key, roleName, roleSessionID string) (RoleSnapshot, error) {
	if roleName == "" || roleSessionID == "" {
		return RoleSnapshot{}, errors.New("session storage: role_name and role_session_id are required")
	}
	roleStore, roleKey := roleSessionEngine(store, mainKey, roleName, roleSessionID)
	roleLifecycle, err := roleStore.readLifecycleHead(roleKey)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return RoleSnapshot{}, err
	}
	mainLifecycle, err := store.readLifecycleHead(mainKey)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return RoleSnapshot{}, err
	}
	mainHead, err := store.readMessageHead(mainKey)
	if err != nil {
		return RoleSnapshot{}, err
	}
	mainRows, err := store.readAllRows(mainKey)
	if err != nil {
		return RoleSnapshot{}, err
	}
	roleRows, err := roleStore.readAllRows(roleKey)
	if err != nil {
		return RoleSnapshot{}, err
	}
	draftRows, err := storerole.ReadDraft(roleStore.sessionRoot(roleKey), roleName)
	if err != nil {
		return RoleSnapshot{}, err
	}
	return storerole.BuildSnapshot(
		mainKey.SessionID, roleName, roleSessionID,
		roleSessionRoot(store, mainKey, roleName, roleSessionID),
		roleLifecycle, mainLifecycle, mainHead, mainRows, roleRows, draftRows,
	), nil
}

// assembleRoleWire 构造角色可见 wire：main compact_ref + seq > 切点的 main 行
// + 该角色自身 pending draft（未 sync 不进主文档）。user draft 不在此路径。
func assembleRoleWire(store *storeEngine, mainKey Key, roleName, roleSessionID string, budget, k int) (RoleWireSnapshot, error) {
	if roleName == storerole.RoleUser {
		return RoleWireSnapshot{}, errors.New("session storage: user draft is input-box material, not role wire")
	}
	roleStore, roleKey := roleSessionEngine(store, mainKey, roleName, roleSessionID)
	lifecycle, err := roleStore.readLifecycleHead(roleKey)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return RoleWireSnapshot{}, err
	}
	compactHead, err := store.readCompactHead(mainKey)
	if err != nil {
		return RoleWireSnapshot{}, err
	}
	applied, frame, err := storerole.ResolveWireWindow(roleName, lifecycle, compactHead)
	if err != nil {
		return RoleWireSnapshot{}, err
	}
	rows, err := store.readRows(mainKey, applied+1, 0)
	if err != nil {
		return RoleWireSnapshot{}, err
	}
	head, err := store.readMessageHead(mainKey)
	if err != nil {
		return RoleWireSnapshot{}, err
	}
	draftRows, err := storerole.ReadDraft(roleStore.sessionRoot(roleKey), roleName)
	if err != nil {
		return RoleWireSnapshot{}, err
	}
	combined, pending, warnings := storerole.MergeWirePending(roleName, roleSessionID, rows, head, draftRows)
	result := assembleWireRows(frame, combined, nil, applyWireBudget(store.settings, wireParams{Budget: budget, K: k}))
	return storerole.BuildWireSnapshot(
		mainKey.SessionID, roleName, roleSessionID, applied, result, pending, warnings,
	), nil
}

func cloneCompactRef(ref *CompactRef) *CompactRef {
	if ref == nil {
		return nil
	}
	copy := *ref
	return &copy
}

func cloneFloor(floor *Floor) *Floor {
	if floor == nil {
		return nil
	}
	copy := *floor
	return &copy
}

// appendRoleSessionRows 向角色会话写备份行。main 角色写主会话 message；非 main
// 写各自子树。调用方保证只写该角色自己的行。
func appendRoleSessionRows(store *storeEngine, mainKey Key, roleName, roleSessionID string, rows []Event) error {
	if len(rows) == 0 {
		return nil
	}
	roleStore, roleKey := roleSessionEngine(store, mainKey, roleName, roleSessionID)
	roleHead, err := roleStore.readMessageHead(roleKey)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	nextSeq := roleHead.LastSeq
	for index := range rows {
		if rows[index].Seq != 0 {
			continue
		}
		nextSeq++
		rows[index].Seq = nextSeq
	}
	commitID := storerole.BackupCommitID(roleName, roleSessionID, rows)
	_, err = roleStore.messageCommit(roleKey, commitID, rows)
	return err
}
