package store

import (
	"context"
	"errors"
	"fmt"
	"sync"

	storeartifact "github.com/RedHuang-0622/seelex/store/artifact"
	storeboard "github.com/RedHuang-0622/seelex/store/board"
	storecontext "github.com/RedHuang-0622/seelex/store/context"
	storelayout "github.com/RedHuang-0622/seelex/store/internal/layout"
	storerecord "github.com/RedHuang-0622/seelex/store/record"
	storerole "github.com/RedHuang-0622/seelex/store/role"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
	storeteamwork "github.com/RedHuang-0622/seelex/store/teamwork"
	storeworkunit "github.com/RedHuang-0622/seelex/store/workunit"
)

// Stores is the composition-root value object. Every field is a concrete,
// already-scoped owner; consumers receive the narrow field they use instead of
// discovering capabilities from Router at runtime.
type Stores struct {
	Router          *Router
	Artifacts       *storeartifact.Store
	Context         *storecontext.Store
	Boards          *storeboard.SessionStore
	Teamwork        *storeteamwork.Store
	Roles           *storerole.Store
	RolePermissions *storerole.PermissionStore
	ToolRecords     *storerecord.Store
	WorkUnits       *storeworkunit.SessionStore
	Media           *storeartifact.SessionStore
	RoleSessions    *RoleSessionAdapter
}

// OpenStores opens the single JSON data root once and returns the shared owner
// bundle. Opening twice against the same root is rejected by the data-root lock.
func OpenStores(defaultPath string, limits ...storeroot.Settings) (*Stores, error) {
	config := Config{SessionStorage: storeroot.ResolveSettings(limits...)}
	config, err := config.Normalize(defaultPath)
	if err != nil {
		return nil, err
	}
	repository, err := Open(context.Background(), config)
	if err != nil {
		return nil, err
	}
	jsonRepository, ok := repository.(*jsonRepository)
	if !ok {
		_ = repository.Close()
		return nil, fmt.Errorf("session storage: unexpected repository implementation %T", repository)
	}
	router := &Router{repository: repository}
	router.opsCond = sync.NewCond(&router.opsMu)
	layout := jsonRepository.layout
	router.workUnits = storeworkunit.NewSessionStore(workunitSessionRoots{router: router})
	rolePermissions := storerole.NewPermissionStore(rolePermissionLayout{router: router})
	media := storeartifact.NewSessionStore(layout.artifacts, artifactScopeResolver{router: router})
	roleSessions := NewRoleSessionAdapter(router)
	return &Stores{
		Router:          router,
		Artifacts:       layout.artifacts,
		Context:         layout.contexts,
		Boards:          layout.boards,
		Teamwork:        layout.teamwork,
		Roles:           layout.roles,
		RolePermissions: rolePermissions,
		ToolRecords:     layout.records,
		WorkUnits:       router.workUnits,
		Media:           media,
		RoleSessions:    roleSessions,
	}, nil
}

// rolePermissionLayout 是 Session 项目布局到 Role 权限登记的唯一适配器。
// 权限文件形状、规范化、缓存与锁序都在 store/role；这里只回答项目目录。
type rolePermissionLayout struct {
	router *Router
}

func (layout rolePermissionLayout) WithProjectDir(projectID string, fn func(dir string) error) error {
	if layout.router == nil {
		return errors.New("session storage: role permissions are not assembled")
	}
	return layout.router.withRepositoryAt(projectID, func(repository Repository, resolvedProjectID string) error {
		sessionLayout, ok := repository.(*jsonRepository)
		if !ok {
			return errors.New("session storage: role permissions require session layout")
		}
		return fn(sessionLayout.projectDir(resolvedProjectID))
	})
}

// artifactScopeResolver is the only adapter from session layout to artifact scope.
type artifactScopeResolver struct {
	router *Router
}

func (resolver artifactScopeResolver) ArtifactScope(projectID, sessionID string) (storelayout.Scope, error) {
	if resolver.router == nil {
		return storelayout.Scope{}, errors.New("session storage: artifact scope is not assembled")
	}
	var scope storelayout.Scope
	err := resolver.router.withRepositoryAt(projectID, func(_ Repository, resolvedProjectID string) error {
		scope = layoutScope(Key{ProjectID: resolvedProjectID, SessionID: sessionID})
		return nil
	})
	return scope, err
}

func (stores *Stores) Close() error {
	if stores == nil || stores.Router == nil {
		return nil
	}
	return stores.Router.Close()
}
