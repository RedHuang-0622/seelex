package store

import (
	"errors"

	storerole "github.com/RedHuang-0622/seelex/store/role"
)

// role_permission.go 是根 Store 的迁移期兼容面。权限文件形状、规范化、缓存与
// 锁序的唯一实现位于 store/role；本文件只把 Router 装配成它的项目目录端口。

const rolePermissionFileName = "role-permissions.json"

type RolePermissionRecord = storerole.RolePermissionRecord
type RolePermissionStore = storerole.PermissionStore

// NewRolePermissionStore 构造会话级角色权限登记存储。
func NewRolePermissionStore(router *Router) *RolePermissionStore {
	if router == nil {
		return storerole.NewPermissionStore(nil)
	}
	return storerole.NewPermissionStore(rolePermissionLayout{router: router})
}

type rolePermissionLayout struct {
	router *Router
}

func (layout rolePermissionLayout) WithProjectDir(projectID string, fn func(dir string) error) error {
	if layout.router == nil {
		return errors.New("session storage: role permissions are not assembled")
	}
	return layout.router.withRepositoryAt(projectID, func(repository Repository, resolvedProjectID string) error {
		jsonRepository, ok := repository.(*jsonRepository)
		if !ok {
			return errors.New("session storage: role permissions require session layout")
		}
		return fn(jsonRepository.projectDir(resolvedProjectID))
	})
}
