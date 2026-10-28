package store

import storerole "github.com/RedHuang-0622/seelex/store/role"

// Test-only names keep legacy root fixtures readable without reintroducing a
// production compatibility surface. New tests should import store/role.
const (
	RoleUser = storerole.RoleUser
	RoleMain = storerole.RoleMain
	RoleTL   = storerole.RoleTL
)

type RoleDraftRow = storerole.DraftRow
type RoleDraftSyncResult = storerole.SyncResult

func roleAdapter(router *Router) *RoleSessionAdapter { return NewRoleSessionAdapter(router) }
func stackAdapter(router *Router) *StackStore        { return NewStackStore(router) }
func wireAdapter(router *Router) *WireStore          { return NewWireStore(router) }
