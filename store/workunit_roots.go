package store

import (
	"errors"

	storeworkunit "github.com/RedHuang-0622/seelex/store/workunit"
)

// workunitSessionRoots 是 Session 存储布局到 WorkUnit 记录存储的唯一适配器。
// 它只解析显式作用域对应的主会话根；记录编码、原子写与缺失语义都在
// store/workunit。
type workunitSessionRoots struct {
	router *Router
}

func (roots workunitSessionRoots) CurrentProjectID() string {
	if roots.router == nil {
		return ""
	}
	return roots.router.Workspace()
}

func (roots workunitSessionRoots) WithSessionRoot(projectID, sessionID string, fn func(root string) error) error {
	if roots.router == nil {
		return errors.New("session storage: workunit roots are not assembled")
	}
	return roots.router.withRepositoryAt(projectID, func(repository Repository, resolvedProjectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return errors.New("session storage: workunit records require session layout")
		}
		key := Key{ProjectID: resolvedProjectID, SessionID: sessionID}
		if err := key.Validate(); err != nil {
			return err
		}
		return fn(layout.sessionDir(key))
	})
}

// NewWorkUnitSessionStore 构造绑定 Router 的 WorkUnit 会话记录入口。
//
// 生产装配优先使用 Stores.WorkUnits；该构造器保留给独立测试基座和显式
// composition root 使用，不暴露任何记录读写实现。
func NewWorkUnitSessionStore(router *Router) *storeworkunit.SessionStore {
	return storeworkunit.NewSessionStore(workunitSessionRoots{router: router})
}
