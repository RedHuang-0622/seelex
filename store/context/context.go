// Package context owns durable session context and checkpoint storage
// mechanics. Prompt/stack interpretation remains in the application layer.
package context

import (
	"os"
	"path/filepath"

	storelayout "github.com/RedHuang-0622/seelex/store/internal/layout"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

const StateFile = "context.json"

// Store binds context state I/O to the shared session layout.
type Store struct {
	layout *storelayout.Layout
}

// New constructs a context state store.
func New(layout *storelayout.Layout) *Store {
	return &Store{layout: layout}
}

// StatePath returns <session>/context.json.
func (s *Store) StatePath(scope storelayout.Scope) string {
	return filepath.Join(s.layout.SessionRoot(scope), StateFile)
}

// WriteState atomically publishes an opaque context blob.
func (s *Store) WriteState(scope storelayout.Scope, payload []byte) error {
	path := s.StatePath(scope)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return storeroot.WriteAtomic(path, payload, 0o600)
}

// ReadState reads the opaque context blob.
func (s *Store) ReadState(scope storelayout.Scope) ([]byte, error) {
	return os.ReadFile(s.StatePath(scope))
}

// CheckpointStorageKey maps a workplan checkpoint ID to its safe storage key.
func CheckpointStorageKey(id string) string {
	if id == "" {
		return "checkpoint"
	}
	return "checkpoint-" + storeroot.Hash(id)
}
