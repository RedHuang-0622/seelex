// Package artifact owns immutable content-addressed tool results, oversized
// text blobs, media assets, and the derived per-session search index.
package artifact

import (
	"errors"
	"path/filepath"
	"sync"

	storelayout "github.com/RedHuang-0622/seelex/store/internal/layout"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

const (
	BlobLockID   storelayout.ModuleID = "artifact.blob"
	MediaLockID  storelayout.ModuleID = "media"
	SearchLockID storelayout.ModuleID = "artifact.search"
)

// Store is the artifact implementation bound to the shared session layout.
type Store struct {
	layout   *storelayout.Layout
	settings storeroot.Settings
}

// New constructs an artifact store.
func New(layout *storelayout.Layout, settings storeroot.Settings) *Store {
	return &Store{layout: layout, settings: storeroot.ResolveSettings(settings)}
}

// SessionRoot returns the session root used by artifact partitions.
func (s *Store) SessionRoot(scope storelayout.Scope) string {
	return s.layout.SessionRoot(scope)
}

// BlobDir returns <session>/big_tool_result.
func (s *Store) BlobDir(scope storelayout.Scope) string {
	return filepath.Join(s.SessionRoot(scope), "big_tool_result")
}

// MediaRoot returns <session>/meta.
func (s *Store) MediaRoot(scope storelayout.Scope) string {
	return filepath.Join(s.SessionRoot(scope), "meta")
}

// SearchPath returns <session>/metadata-index/search.json.
func (s *Store) SearchPath(scope storelayout.Scope) string {
	return filepath.Join(s.SessionRoot(scope), "metadata-index", "search.json")
}

func (s *Store) lock(scope storelayout.Scope, id storelayout.ModuleID) *sync.Mutex {
	return s.layout.Lock(scope, id)
}

var errEmptyBlob = errors.New("session storage: empty blob")
