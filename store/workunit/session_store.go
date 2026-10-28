package workunit

import (
	"errors"
	"strings"
)

// SessionRootResolver 把显式项目/主会话作用域解析成主会话存储根。
//
// 端口由 workunit 消费方定义：本包只认识“根目录”，不认识 Router、Repository
// 或具体布局。实现放在组合根适配层，解析结果必须按 projectID 与 sessionID
// 共同确定，不能回退到调用时的活跃写作用域。
type SessionRootResolver interface {
	CurrentProjectID() string
	WithSessionRoot(projectID, sessionID string, fn func(root string) error) error
}

// SessionStore 是 node/subagent/teammate 会话记录的会话作用域入口。
//
// 记录形状、文件名、原子写与缺失语义仍由同包 Store 拥有；本类型只负责每次
// 调用把显式 scope 交给路径端口，并在该主会话根下构造短生命周期 Store。
type SessionStore struct {
	roots SessionRootResolver
}

// NewSessionStore 构造会话作用域记录入口。
func NewSessionStore(roots SessionRootResolver) *SessionStore {
	return &SessionStore{roots: roots}
}

// ProjectID 返回解析器当前的稳定项目键（未装配时为空）。
func (s *SessionStore) ProjectID() string {
	if s == nil || s.roots == nil {
		return ""
	}
	return s.roots.CurrentProjectID()
}

// Save 写入一条记录，所有坐标经同一个路径端口解析。
func (s *SessionStore) Save(projectID, mainSessionID string, record Record) error {
	if s == nil || s.roots == nil {
		return errors.New("session storage: node session roots are unavailable")
	}
	mainSessionID = strings.TrimSpace(mainSessionID)
	if mainSessionID == "" {
		return errors.New("session storage: node session requires a main session ID")
	}
	if record.MainSessionID == "" {
		record.MainSessionID = mainSessionID
	}
	return s.roots.WithSessionRoot(projectID, mainSessionID, func(root string) error {
		return New(root).Write(record)
	})
}

// Load 读取一条记录；缺失语义由 Store 统一返回 ErrNotFound。
func (s *SessionStore) Load(projectID, mainSessionID, nodeSessionID string) (Record, error) {
	if s == nil || s.roots == nil {
		return Record{}, errors.New("session storage: node session roots are unavailable")
	}
	var record Record
	err := s.roots.WithSessionRoot(projectID, strings.TrimSpace(mainSessionID), func(root string) error {
		var readErr error
		record, readErr = New(root).Read(strings.TrimSpace(mainSessionID), nodeSessionID)
		return readErr
	})
	return record, err
}

// List 从主会话索引列举全部记录。
func (s *SessionStore) List(projectID, mainSessionID string) ([]Record, error) {
	if s == nil || s.roots == nil {
		return nil, errors.New("session storage: node session roots are unavailable")
	}
	var records []Record
	err := s.roots.WithSessionRoot(projectID, strings.TrimSpace(mainSessionID), func(root string) error {
		var listErr error
		records, listErr = New(root).List()
		return listErr
	})
	return records, err
}

// Delete 删除一条记录；重复删除是幂等 no-op。
func (s *SessionStore) Delete(projectID, mainSessionID, nodeSessionID string) error {
	if s == nil || s.roots == nil {
		return errors.New("session storage: node session roots are unavailable")
	}
	return s.roots.WithSessionRoot(projectID, strings.TrimSpace(mainSessionID), func(root string) error {
		return New(root).Delete(strings.TrimSpace(mainSessionID), nodeSessionID)
	})
}
