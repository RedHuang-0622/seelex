package core

import (
	"context"
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/middle/residency"
	switching "github.com/RedHuang-0622/seelex/middle/switch"
)

// sessionQueryDeps 是只读会话查询的窄依赖集。查询对象不持有 *Service；
// 需要现有会话可见状态的步骤由调用方以闭包注入。
type sessionQueryDeps struct {
	ViewMu       *sync.RWMutex
	CatalogRows  func() []model.SessionInfo
	WithLiveRows func([]model.SessionInfo) []model.SessionInfo
	EnrichRows   func([]model.SessionInfo) []model.SessionInfo
	Meta         func() switching.MetaDeps
	Workspace    contract.WorkspacePort
	WaitCatalog  func(context.Context) error
}

// sessionQuery 是 AP-004 的会话只读查询实现。目录补行与状态富化仍然复用
// Service 的唯一判据，查询对象只负责读锁范围和调用顺序。
type sessionQuery struct {
	deps sessionQueryDeps
}

func newSessionQuery(deps sessionQueryDeps) *sessionQuery {
	return &sessionQuery{deps: deps}
}

// ListSessions 返回当前权威会话目录；读锁只覆盖内存快照和投影组合。
func (query *sessionQuery) ListSessions() []model.SessionInfo {
	if query == nil || query.deps.ViewMu == nil {
		return nil
	}
	query.deps.ViewMu.RLock()
	defer query.deps.ViewMu.RUnlock()
	rows := append([]model.SessionInfo(nil), query.deps.CatalogRows()...)
	if query.deps.WithLiveRows != nil {
		rows = query.deps.WithLiveRows(rows)
	}
	if query.deps.EnrichRows != nil {
		rows = query.deps.EnrichRows(rows)
	}
	return rows
}

// ProjectRootFor 从会话绑定事实读取项目根，不回退到当前视图工作区。
func (query *sessionQuery) ProjectRootFor(sessionID string) string {
	if query == nil || query.deps.Workspace == nil {
		return ""
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	if workspace, ok := query.deps.Workspace.SessionWorkspace(sessionID); ok {
		return strings.TrimSpace(workspace.RootPath)
	}
	return ""
}

// SessionMeta 读取会话展示元数据；元数据端口缺失的语义由 middle/switch 统一返回。
func (query *sessionQuery) SessionMeta(sessionID string) (model.SessionMeta, error) {
	return switching.ReadMeta(query.deps.Meta(), sessionID)
}

// WaitCatalogRefresh 等待目录 worker 覆盖本轮刷新；等待期间不持有 ViewMu。
func (query *sessionQuery) WaitCatalogRefresh(ctx context.Context) error {
	if query == nil || query.deps.WaitCatalog == nil {
		return nil
	}
	return query.deps.WaitCatalog(ctx)
}

// newServiceSessionQuery 将既有唯一判据装配成查询端口。
func newServiceSessionQuery(service *Service) *sessionQuery {
	return newSessionQuery(sessionQueryDeps{
		ViewMu: &service.ViewMu,
		CatalogRows: func() []model.SessionInfo {
			return service.Core.Snapshot.Sessions
		},
		WithLiveRows: service.withLiveSessionRowsLocked,
		EnrichRows:   service.enrichDirectoryRowsLocked,
		Meta:         service.metaDeps,
		Workspace:    service.Deps.Workspace,
		WaitCatalog:  service.waitCatalogRefresh,
	})
}

// waitCatalogRefresh 是既有一行宿主转发的内部入口，供无状态查询对象注入。
func (service *Service) waitCatalogRefresh(ctx context.Context) error {
	return residency.WaitCatalogRefresh(ctx, service.components.sessions)
}
