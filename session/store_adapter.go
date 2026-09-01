package session

import "github.com/RedHuang-0622/seelex/sessionstore"

// StorePortAdapter 把 sessionstore.SessionGranularStore 适配为
// session.StorePort（类型为别名，转换零成本；project 作用域取 record
// 的绑定 workspace，未绑定回退 store 当前 active scope）。
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
	return adapter.store.LoadSession("", sessionID)
}

func (adapter StorePortAdapter) History(sessionID string) *sessionstore.DurableHistory {
	if adapter.store == nil {
		return nil
	}
	return adapter.store.History(sessionID)
}

func (adapter StorePortAdapter) Transcript(sessionID string) ([]TranscriptEvent, error) {
	if adapter.store == nil {
		return nil, nil
	}
	return adapter.store.Transcript("", sessionID)
}

func (adapter StorePortAdapter) ToolResults(sessionID string) ([]ToolResultRef, error) {
	if adapter.store == nil {
		return nil, nil
	}
	return adapter.store.ToolResults("", sessionID)
}

func (adapter StorePortAdapter) Context(sessionID string) (ContextStack, error) {
	if adapter.store == nil {
		return ContextStack{}, nil
	}
	return adapter.store.Context("", sessionID)
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
