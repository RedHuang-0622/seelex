package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/RedHuang-0622/seelex/contract/dto"
	storelayout "github.com/RedHuang-0622/seelex/store/internal/layout"
	storerole "github.com/RedHuang-0622/seelex/store/role"
)

// RoleSessionAdapter owns the session-layout adapter surface for Role sessions.
// Role draft, lifecycle, projection and warning rules live in store/role; this
// type only supplies Router scope and concrete message/event/compact I/O.
type RoleSessionAdapter struct {
	router *Router
}

// NewRoleSessionAdapter constructs the Role session storage adapter.
func NewRoleSessionAdapter(router *Router) *RoleSessionAdapter {
	return &RoleSessionAdapter{router: router}
}

// CreateRoleSessionWorkspace 创建非 main 角色会话（TL/agent-team）。main 角色
// 直接复用主会话，本方法显式拒绝。返回角色会话基础信息。
func (adapter *RoleSessionAdapter) CreateRoleSessionWorkspace(projectID, mainSessionID, roleName, roleSessionID string, joinSeq uint64) (dto.RoleSessionInfo, error) {
	if roleName == storerole.RoleMain {
		return dto.RoleSessionInfo{}, fmt.Errorf("session storage: main role session is the main session itself")
	}
	var info dto.RoleSessionInfo
	err := adapter.router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: role sessions require session layout")
		}
		mainKey := Key{ProjectID: projectID, SessionID: mainSessionID}
		_, key, err := createRoleSession(layout.layout, mainKey, roleName, roleSessionID, joinSeq)
		if err != nil {
			return err
		}
		info = dto.RoleSessionInfo{
			MainSessionID: mainSessionID,
			RoleName:      roleName,
			RoleSessionID: roleSessionID,
			Root:          roleSessionRoot(layout.layout, mainKey, roleName, roleSessionID),
		}
		_ = key
		return nil
	})
	return info, err
}

// AppendRoleDraftWorkspace 向角色自己的 draft 追加未同步行。
func (adapter *RoleSessionAdapter) AppendRoleDraftWorkspace(projectID, mainSessionID, roleName, roleSessionID string, rows []storerole.DraftRow) error {
	return adapter.withRoleStore(projectID, mainSessionID, roleName, roleSessionID, func(roleStore *storeEngine, roleKey Key) error {
		return storerole.AppendDraft(roleStore.sessionRoot(roleKey), roleName, roleSessionID, rows)
	})
}

// ReadRoleDraftWorkspace 读取角色未同步 draft。
func (adapter *RoleSessionAdapter) ReadRoleDraftWorkspace(projectID, mainSessionID, roleName, roleSessionID string) ([]storerole.DraftRow, error) {
	var rows []storerole.DraftRow
	err := adapter.withRoleStore(projectID, mainSessionID, roleName, roleSessionID, func(roleStore *storeEngine, roleKey Key) error {
		var err error
		rows, err = storerole.ReadDraft(roleStore.sessionRoot(roleKey), roleName)
		return err
	})
	return rows, err
}

// SyncRoleDraftWorkspace 由 sequencer 同步角色 draft 到主会话 message，成功后
// 删除 draft。
func (adapter *RoleSessionAdapter) SyncRoleDraftWorkspace(projectID, mainSessionID, roleName, roleSessionID string, order []string) (storerole.SyncResult, error) {
	var result storerole.SyncResult
	err := adapter.router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: role draft sync requires session layout")
		}
		var err error
		result, err = syncRoleDraft(layout.layout, Key{ProjectID: projectID, SessionID: mainSessionID}, roleName, roleSessionID, order)
		return err
	})
	return result, err
}

// AppendRoleSessionRowsWorkspace 向角色会话写备份行（非 main 角色备份；main 会
// 写主会话 message）。
func (adapter *RoleSessionAdapter) AppendRoleSessionRowsWorkspace(projectID, mainSessionID, roleName, roleSessionID string, rows []Event) error {
	return adapter.router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: role session rows require session layout")
		}
		return appendRoleSessionRows(layout.layout, Key{ProjectID: projectID, SessionID: mainSessionID}, roleName, roleSessionID, rows)
	})
}

// ReadRoleSessionRowsWorkspace 读取角色会话备份行。
func (adapter *RoleSessionAdapter) ReadRoleSessionRowsWorkspace(projectID, mainSessionID, roleName, roleSessionID string) ([]Event, error) {
	var rows []Event
	err := adapter.router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: role session rows require session layout")
		}
		var err error
		rows, err = readRoleSessionRows(layout.layout, Key{ProjectID: projectID, SessionID: mainSessionID}, roleName, roleSessionID)
		return err
	})
	return rows, err
}

// RoleSnapshotWorkspace 返回角色会话的只读观察面（headless 冒烟/巡检用）。
func (adapter *RoleSessionAdapter) RoleSnapshotWorkspace(projectID, mainSessionID, roleName, roleSessionID string) (RoleSnapshot, error) {
	var snapshot RoleSnapshot
	err := adapter.router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: role snapshot requires session layout")
		}
		var err error
		snapshot, err = readRoleSnapshot(layout.layout, Key{ProjectID: projectID, SessionID: mainSessionID}, roleName, roleSessionID)
		return err
	})
	return snapshot, err
}

// AssembleRoleWireWorkspace 构造角色可见物化 wire（main compact_ref + 已发布
// 行 where seq > 切点 + 自身 pending draft）。user 角色显式拒绝。
func (adapter *RoleSessionAdapter) AssembleRoleWireWorkspace(projectID, mainSessionID, roleName, roleSessionID string, budget, k int) (RoleWireSnapshot, error) {
	var snapshot RoleWireSnapshot
	err := adapter.router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: role wire requires session layout")
		}
		var err error
		snapshot, err = assembleRoleWire(layout.layout, Key{ProjectID: projectID, SessionID: mainSessionID}, roleName, roleSessionID, budget, k)
		return err
	})
	return snapshot, err
}

// SetLifecycleOrderWorkspace 写主会话（或角色会话）的群聊顺序策略。
func (adapter *RoleSessionAdapter) SetLifecycleOrderWorkspace(projectID, sessionID, policy string, roles []string) error {
	return adapter.router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: lifecycle order requires session layout")
		}
		return layout.layout.setLifecycleOrder(Key{ProjectID: projectID, SessionID: sessionID}, policy, roles)
	})
}

// SetRoleLifecycleWorkspace 写角色会话的 join_seq_id 与 compact_ref。
func (adapter *RoleSessionAdapter) SetRoleLifecycleWorkspace(projectID, mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *CompactRef) error {
	return adapter.withRoleStore(projectID, mainSessionID, roleName, roleSessionID, func(roleStore *storeEngine, roleKey Key) error {
		return roleStore.setRoleLifecycle(roleKey, joinSeq, ref)
	})
}

func (adapter *RoleSessionAdapter) withRoleStore(projectID, mainSessionID, roleName, roleSessionID string, fn func(*storeEngine, Key) error) error {
	return adapter.router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: role session operations require session layout")
		}
		mainKey := Key{ProjectID: projectID, SessionID: strings.TrimSpace(mainSessionID)}
		roleStore, roleKey := roleSessionEngine(layout.layout, mainKey, roleName, roleSessionID)
		return fn(roleStore, roleKey)
	})
}

// ListRoleSessionsWorkspace 枚举主会话下的角色会话目录名（goal_<hash>/ 与
// role_<hash>/）。仅用于审计/测试，目录名不承载排序语义。
func (adapter *RoleSessionAdapter) ListRoleSessionsWorkspace(projectID, mainSessionID string) ([]string, error) {
	var names []string
	err := adapter.router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: role sessions require session layout")
		}
		mainKey := Key{ProjectID: projectID, SessionID: mainSessionID}
		entries, err := readDirNames(layout.layout.sessionRoot(mainKey))
		if err != nil {
			return err
		}
		for _, name := range entries {
			if strings.HasPrefix(name, "goal_") || strings.HasPrefix(name, "role_") {
				names = append(names, name)
			}
		}
		return nil
	})
	return names, err
}

// ListRoleSessions is the non-workspace variant using the active project.
func (adapter *RoleSessionAdapter) ListRoleSessions(mainSessionID string) ([]string, error) {
	return adapter.ListRoleSessionsWorkspace(adapter.router.Workspace(), mainSessionID)
}

func readDirNames(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names, nil
}

// EnsureRoleSessionWorkspace 幂等创建角色会话：已存在返回 created=false。
func (adapter *RoleSessionAdapter) EnsureRoleSessionWorkspace(projectID, mainSessionID, roleName, roleSessionID string, joinSeq uint64) (bool, error) {
	created := false
	err := adapter.router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: role sessions require session layout")
		}
		mainKey := Key{ProjectID: projectID, SessionID: strings.TrimSpace(mainSessionID)}
		roleStore, roleKey := roleSessionEngine(layout.layout, mainKey, roleName, roleSessionID)
		if roleStore.sessionExists(roleKey) {
			return nil
		}
		if _, _, err := createRoleSession(layout.layout, mainKey, roleName, roleSessionID, joinSeq); err != nil {
			if roleStore.sessionExists(roleKey) {
				return nil
			}
			return err
		}
		created = true
		return nil
	})
	return created, err
}

// ReadMessageFloorWorkspace 读 message head 的 floor；空会话返回 nil。
func (adapter *RoleSessionAdapter) ReadMessageFloorWorkspace(projectID, sessionID string) (*Floor, error) {
	var floor *Floor
	err := adapter.router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: message floor requires session layout")
		}
		head, err := layout.layout.readMessageHead(Key{ProjectID: projectID, SessionID: strings.TrimSpace(sessionID)})
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		floor = cloneFloor(head.Floor)
		return nil
	})
	return floor, err
}

// ReadLifecycleOrderWorkspace 读 legacy lifecycle 顺序；缺失 head = 空顺序。
func (adapter *RoleSessionAdapter) ReadLifecycleOrderWorkspace(projectID, sessionID string) (string, []string, error) {
	var policy string
	var roles []string
	err := adapter.router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: lifecycle order requires session layout")
		}
		head, err := layout.layout.readLifecycleHead(Key{ProjectID: projectID, SessionID: strings.TrimSpace(sessionID)})
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		policy = head.OrderPolicy
		roles = append([]string(nil), head.OrderRoles...)
		return nil
	})
	return policy, roles, err
}

// registryScope 保留给旧 roles.json fixture 冷读测试与兼容诊断。
func registryScope(projectID, sessionID string) storelayout.Scope {
	return layoutScope(Key{ProjectID: projectID, SessionID: strings.TrimSpace(sessionID)})
}
