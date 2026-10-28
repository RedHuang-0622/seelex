package artifact

import (
	"context"
	"fmt"
	"strings"

	storelayout "github.com/RedHuang-0622/seelex/store/internal/layout"
)

// ScopeResolver resolves an explicit project/session key to a layout scope.
type ScopeResolver interface {
	ArtifactScope(projectID, sessionID string) (storelayout.Scope, error)
}

// SessionStore is the session-scoped entry for artifact-owned media.
type SessionStore struct {
	store    *Store
	resolver ScopeResolver
}

// NewSessionStore constructs a session-scoped media entry.
func NewSessionStore(store *Store, resolver ScopeResolver) *SessionStore {
	return &SessionStore{store: store, resolver: resolver}
}

func (s *SessionStore) scope(projectID, sessionID string) (storelayout.Scope, error) {
	if s == nil || s.store == nil || s.resolver == nil {
		return storelayout.Scope{}, fmt.Errorf("%w: media partition unavailable", ErrMediaInvalid)
	}
	if strings.TrimSpace(sessionID) == "" {
		return storelayout.Scope{}, fmt.Errorf("%w: session id required", ErrMediaInvalid)
	}
	return s.resolver.ArtifactScope(projectID, sessionID)
}

// WriteMedia stores an idempotent media asset.
func (s *SessionStore) WriteMedia(_ context.Context, projectID, sessionID string, item MediaItem) (MediaRef, error) {
	scope, err := s.scope(projectID, sessionID)
	if err != nil {
		return MediaRef{}, err
	}
	return s.store.WriteMedia(scope, item)
}

// ReadMedia reads media metadata and bytes.
func (s *SessionStore) ReadMedia(_ context.Context, projectID, sessionID, ref string) (MediaRef, []byte, error) {
	scope, err := s.scope(projectID, sessionID)
	if err != nil {
		return MediaRef{}, nil, err
	}
	return s.store.ReadMedia(scope, ref)
}

// MediaPath resolves one stored media asset to its on-disk path (宿主用系统默认
// 查看器打开原图；路径不回渲染层)。归属解析与其它媒体读写同一套 scope 规则。
func (s *SessionStore) MediaPath(_ context.Context, projectID, sessionID, ref string) (string, error) {
	scope, err := s.scope(projectID, sessionID)
	if err != nil {
		return "", err
	}
	return s.store.MediaPath(scope, ref)
}

// MediaMeta reads one stored media asset's metadata without its bytes (写一行
// 「这条用户消息带了哪几张图」只需要名字/类型/尺寸，不需要图本体)。
func (s *SessionStore) MediaMeta(_ context.Context, projectID, sessionID, ref string) (MediaRef, error) {
	scope, err := s.scope(projectID, sessionID)
	if err != nil {
		return MediaRef{}, err
	}
	return s.store.MediaMeta(scope, ref)
}

// ListMedia lists media assets.
func (s *SessionStore) ListMedia(_ context.Context, projectID, sessionID string) ([]MediaRef, error) {
	scope, err := s.scope(projectID, sessionID)
	if err != nil {
		return nil, err
	}
	return s.store.ListMedia(scope)
}

// CollectMedia removes unreferenced media assets.
func (s *SessionStore) CollectMedia(_ context.Context, projectID, sessionID string, referenced map[string]bool, dryRun bool) ([]string, error) {
	scope, err := s.scope(projectID, sessionID)
	if err != nil {
		return nil, err
	}
	return s.store.CollectMedia(scope, referenced, dryRun)
}
