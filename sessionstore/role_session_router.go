package sessionstore

import (
	"fmt"
	"os"
	"strings"
)

// CreateRoleSessionWorkspace 创建非 main 角色会话（TL/agent-team）。main 角色
// 直接复用主会话，本方法显式拒绝。返回角色会话基础信息。
func (router *Router) CreateRoleSessionWorkspace(projectID, mainSessionID, roleName, roleSessionID string, joinSeq uint64) (RoleSessionInfo, error) {
	if roleName == RoleMain {
		return RoleSessionInfo{}, fmt.Errorf("session storage: main role session is the main session itself")
	}
	var info RoleSessionInfo
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: role sessions require session layout")
		}
		mainKey := Key{ProjectID: projectID, SessionID: mainSessionID}
		_, key, err := layout.layout.createRoleSession(mainKey, roleName, roleSessionID, joinSeq)
		if err != nil {
			return err
		}
		info = RoleSessionInfo{
			MainSessionID: mainSessionID,
			RoleName:      roleName,
			RoleSessionID: roleSessionID,
			Root:          layout.layout.roleSessionRoot(mainKey, roleName, roleSessionID),
		}
		_ = key
		return nil
	})
	return info, err
}

// AppendRoleDraftWorkspace 向角色自己的 draft 追加未同步行。
func (router *Router) AppendRoleDraftWorkspace(projectID, mainSessionID, roleName, roleSessionID string, rows []RoleDraftRow) error {
	return router.withRoleStore(projectID, mainSessionID, roleName, roleSessionID, func(roleStore *storeEngine, roleKey Key) error {
		return appendRoleDraft(roleStore, roleKey, roleName, rows)
	})
}

// ReadRoleDraftWorkspace 读取角色未同步 draft。
func (router *Router) ReadRoleDraftWorkspace(projectID, mainSessionID, roleName, roleSessionID string) ([]RoleDraftRow, error) {
	var rows []RoleDraftRow
	err := router.withRoleStore(projectID, mainSessionID, roleName, roleSessionID, func(roleStore *storeEngine, roleKey Key) error {
		var err error
		rows, err = readRoleDraft(roleStore, roleKey, roleName)
		return err
	})
	return rows, err
}

// SyncRoleDraftWorkspace 由 sequencer 同步角色 draft 到主会话 message，成功后
// 删除 draft。
func (router *Router) SyncRoleDraftWorkspace(projectID, mainSessionID, roleName, roleSessionID string, order []string) (RoleDraftSyncResult, error) {
	var result RoleDraftSyncResult
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: role draft sync requires session layout")
		}
		var err error
		result, err = layout.layout.syncRoleDraft(Key{ProjectID: projectID, SessionID: mainSessionID}, roleName, roleSessionID, order)
		return err
	})
	return result, err
}

// AppendRoleSessionRowsWorkspace 向角色会话写备份行（非 main 角色备份；main 会
// 写主会话 message）。
func (router *Router) AppendRoleSessionRowsWorkspace(projectID, mainSessionID, roleName, roleSessionID string, rows []Event) error {
	return router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: role session rows require session layout")
		}
		return layout.layout.appendRoleSessionRows(Key{ProjectID: projectID, SessionID: mainSessionID}, roleName, roleSessionID, rows)
	})
}

// ReadRoleSessionRowsWorkspace 读取角色会话备份行。
func (router *Router) ReadRoleSessionRowsWorkspace(projectID, mainSessionID, roleName, roleSessionID string) ([]Event, error) {
	var rows []Event
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: role session rows require session layout")
		}
		var err error
		rows, err = layout.layout.readRoleSessionRows(Key{ProjectID: projectID, SessionID: mainSessionID}, roleName, roleSessionID)
		return err
	})
	return rows, err
}

// RoleSnapshotWorkspace 返回角色会话的只读观察面（headless 冒烟/巡检用）。
func (router *Router) RoleSnapshotWorkspace(projectID, mainSessionID, roleName, roleSessionID string) (RoleSnapshot, error) {
	var snapshot RoleSnapshot
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: role snapshot requires session layout")
		}
		var err error
		snapshot, err = layout.layout.readRoleSnapshot(Key{ProjectID: projectID, SessionID: mainSessionID}, roleName, roleSessionID)
		return err
	})
	return snapshot, err
}

// AssembleRoleWireWorkspace 构造角色可见物化 wire（main compact_ref + 已发布
// 行 where seq > 切点 + 自身 pending draft）。user 角色显式拒绝。
func (router *Router) AssembleRoleWireWorkspace(projectID, mainSessionID, roleName, roleSessionID string, budget, k int) (RoleWireSnapshot, error) {
	var snapshot RoleWireSnapshot
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: role wire requires session layout")
		}
		var err error
		snapshot, err = layout.layout.assembleRoleWire(Key{ProjectID: projectID, SessionID: mainSessionID}, roleName, roleSessionID, budget, k)
		return err
	})
	return snapshot, err
}

// SetLifecycleOrderWorkspace 写主会话（或角色会话）的群聊顺序策略。
func (router *Router) SetLifecycleOrderWorkspace(projectID, sessionID, policy string, roles []string) error {
	return router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: lifecycle order requires session layout")
		}
		return layout.layout.setLifecycleOrder(Key{ProjectID: projectID, SessionID: sessionID}, policy, roles)
	})
}

// SetRoleLifecycleWorkspace 写角色会话的 join_seq_id 与 compact_ref。
func (router *Router) SetRoleLifecycleWorkspace(projectID, mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *CompactRef) error {
	return router.withRoleStore(projectID, mainSessionID, roleName, roleSessionID, func(roleStore *storeEngine, roleKey Key) error {
		return roleStore.setRoleLifecycle(roleKey, joinSeq, ref)
	})
}

func (router *Router) withRoleStore(projectID, mainSessionID, roleName, roleSessionID string, fn func(*storeEngine, Key) error) error {
	return router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: role session operations require session layout")
		}
		mainKey := Key{ProjectID: projectID, SessionID: strings.TrimSpace(mainSessionID)}
		roleStore, roleKey := layout.layout.roleStore(mainKey, roleName, roleSessionID)
		return fn(roleStore, roleKey)
	})
}

// ListRoleSessionsWorkspace 枚举主会话下的角色会话目录名（goal_<hash>/ 与
// role_<hash>/）。仅用于审计/测试，目录名不承载排序语义。
func (router *Router) ListRoleSessionsWorkspace(projectID, mainSessionID string) ([]string, error) {
	var names []string
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
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
func (router *Router) ListRoleSessions(mainSessionID string) ([]string, error) {
	return router.ListRoleSessionsWorkspace(router.Workspace(), mainSessionID)
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
