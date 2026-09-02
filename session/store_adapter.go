package session

import "github.com/RedHuang-0622/seelex/sessionstore"

// StorePortAdapter 把 sessionstore.SessionGranularStore 适配为
// session.StorePort（类型为别名，转换零成本；project 作用域按会话解析：
// record/装配绑定的 workspace 优先，未绑定 = 默认项目 ""，绝不回退 Router
// 活跃写作用域）。
type StorePortAdapter struct {
	store *sessionstore.SessionGranularStore
}

// NewStorePort 构造 StorePort 实现。
func NewStorePort(store *sessionstore.SessionGranularStore) StorePort {
	if store == nil {
		return StorePortAdapter{}
	}
	return StorePortAdapter{store: store}
}

// resolve 返回会话归属项目（绑定的 workspace 或默认项目 ""）。
func (adapter StorePortAdapter) resolve(sessionID string) string {
	if adapter.store == nil {
		return ""
	}
	return adapter.store.ResolveProjectForSession(sessionID)
}

func (adapter StorePortAdapter) SaveSession(record SessionRecord) error {
	if adapter.store == nil {
		return nil
	}
	return adapter.store.SaveSession(record.Binding.WorkspaceID, record)
}

func (adapter StorePortAdapter) LoadSession(sessionID string) (SessionRecord, bool, error) {
	if adapter.store == nil {
		return SessionRecord{}, false, nil
	}
	return adapter.store.LoadSession(adapter.resolve(sessionID), sessionID)
}

func (adapter StorePortAdapter) History(sessionID string) *sessionstore.DurableHistory {
	if adapter.store == nil {
		return nil
	}
	return adapter.store.HistoryForProject(adapter.resolve(sessionID), sessionID)
}

func (adapter StorePortAdapter) Transcript(sessionID string) ([]TranscriptEvent, error) {
	if adapter.store == nil {
		return nil, nil
	}
	return adapter.store.Transcript(adapter.resolve(sessionID), sessionID)
}

func (adapter StorePortAdapter) ToolResults(sessionID string) ([]ToolResultRef, error) {
	if adapter.store == nil {
		return nil, nil
	}
	return adapter.store.ToolResults(adapter.resolve(sessionID), sessionID)
}

func (adapter StorePortAdapter) Context(sessionID string) (ContextStack, error) {
	if adapter.store == nil {
		return ContextStack{}, nil
	}
	return adapter.store.Context(adapter.resolve(sessionID), sessionID)
}

func (adapter StorePortAdapter) SessionsOf(projectID string) ([]SessionInfo, error) {
	if adapter.store == nil {
		return nil, nil
	}
	return adapter.store.SessionsOf(projectID)
}

func (adapter StorePortAdapter) Bind(sessionID string, binding SessionBinding) error {
	if adapter.store == nil {
		return nil
	}
	return adapter.store.Bind(binding.WorkspaceID, sessionID, binding)
}
