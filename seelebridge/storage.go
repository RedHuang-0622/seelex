package seelebridge

import (
	"github.com/RedHuang-0622/Seele/seelectx/storage"
	"github.com/RedHuang-0622/Seele/types"
)

type Message = types.Message
type SessionMeta = storage.SessionMeta

// SessionStore adapts Seele storage without exposing it to Seelex business packages.
type SessionStore struct{ store *storage.Store }

func NewSessionStore(path string) (*SessionStore, error) {
	store, err := storage.NewStore(path)
	if err != nil {
		return nil, err
	}
	return &SessionStore{store: store}, nil
}

func (s *SessionStore) FrameworkStore() *storage.Store { return s.store }

func (s *SessionStore) Save(sessionID string, messages []Message) error {
	return s.store.Save(sessionID, messages)
}

func (s *SessionStore) Load(sessionID string) ([]Message, error) {
	return s.store.Load(sessionID)
}

func (s *SessionStore) List() []SessionMeta { return s.store.List() }

func (s *SessionStore) Delete(sessionID string) error { return s.store.Delete(sessionID) }
