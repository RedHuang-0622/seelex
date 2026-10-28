package store

// media.go is the migration-period API/capability adapter for artifact-owned
// media storage. The bytes, quota, GC, and metadata rules live in store/artifact.

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	storeartifact "github.com/RedHuang-0622/seelex/store/artifact"
)

const MediaRefPrefix = storeartifact.MediaRefPrefix

var (
	ErrMediaInvalid     = storeartifact.ErrMediaInvalid
	ErrMediaTooLarge    = storeartifact.ErrMediaTooLarge
	ErrMediaDimensions  = storeartifact.ErrMediaDimensions
	ErrMediaQuota       = storeartifact.ErrMediaQuota
	ErrMediaItemLimit   = storeartifact.ErrMediaItemLimit
	ErrMediaRefInvalid  = storeartifact.ErrMediaRefInvalid
	ErrMediaUnsupported = errors.New("session storage: media partition unavailable")
)

type MediaItem = storeartifact.MediaItem
type MediaRef = storeartifact.MediaRef
type mediaHead = storeartifact.MediaHead

// MediaStore is the optional media capability consumed by router/tools.
type MediaStore interface {
	WriteMedia(context.Context, Key, MediaItem) (MediaRef, error)
	ReadMedia(context.Context, Key, string) (MediaRef, []byte, error)
	ListMedia(context.Context, Key) ([]MediaRef, error)
	CollectMedia(context.Context, Key, map[string]bool, bool) ([]string, error)
}

func (router *Router) WriteMediaWorkspace(ctx context.Context, projectID, sessionID string, item MediaItem) (MediaRef, error) {
	if router == nil {
		return MediaRef{}, ErrMediaUnsupported
	}
	if strings.TrimSpace(sessionID) == "" {
		return MediaRef{}, fmt.Errorf("%w: session id required", ErrMediaInvalid)
	}
	var ref MediaRef
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		store, ok := repository.(MediaStore)
		if !ok {
			return ErrMediaUnsupported
		}
		written, writeErr := store.WriteMedia(ctx, Key{ProjectID: projectID, SessionID: sessionID}, item)
		if writeErr != nil {
			return writeErr
		}
		ref = written
		return nil
	})
	return ref, err
}

func (router *Router) ReadMediaWorkspace(ctx context.Context, projectID, sessionID, ref string) (MediaRef, []byte, error) {
	if router == nil {
		return MediaRef{}, nil, ErrMediaUnsupported
	}
	if strings.TrimSpace(sessionID) == "" {
		return MediaRef{}, nil, fmt.Errorf("%w: session id required", ErrMediaInvalid)
	}
	var (
		item MediaRef
		data []byte
	)
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		store, ok := repository.(MediaStore)
		if !ok {
			return ErrMediaUnsupported
		}
		read, readData, readErr := store.ReadMedia(ctx, Key{ProjectID: projectID, SessionID: sessionID}, ref)
		if readErr != nil {
			return readErr
		}
		item, data = read, readData
		return nil
	})
	return item, data, err
}

func MediaRefHash(ref string) string {
	return storeartifact.MediaRefHash(ref)
}

func ReferencedMediaHashes(results []ToolResult) map[string]bool {
	referenced := map[string]bool{}
	for _, result := range results {
		for _, item := range result.Multimodal {
			if hash := MediaRefHash(item.Ref); hash != "" {
				referenced[hash] = true
			}
		}
	}
	return referenced
}

func (repository *jsonRepository) WriteMedia(_ context.Context, key Key, item MediaItem) (MediaRef, error) {
	if err := key.Validate(); err != nil {
		return MediaRef{}, err
	}
	return repository.layout.artifacts.WriteMedia(layoutScope(key), item)
}

func (repository *jsonRepository) ReadMedia(_ context.Context, key Key, ref string) (MediaRef, []byte, error) {
	if err := key.Validate(); err != nil {
		return MediaRef{}, nil, err
	}
	return repository.layout.artifacts.ReadMedia(layoutScope(key), ref)
}

func (repository *jsonRepository) ListMedia(_ context.Context, key Key) ([]MediaRef, error) {
	if err := key.Validate(); err != nil {
		return nil, err
	}
	return repository.layout.artifacts.ListMedia(layoutScope(key))
}

func (repository *jsonRepository) CollectMedia(_ context.Context, key Key, referenced map[string]bool, dryRun bool) ([]string, error) {
	if err := key.Validate(); err != nil {
		return nil, err
	}
	return repository.layout.artifacts.CollectMedia(layoutScope(key), referenced, dryRun)
}

func (store *storeEngine) mediaLimits() (maxItemBytes, quotaBytes, maxItems, maxLongSide int) {
	return store.settings.MediaMaxItemBytes, store.settings.MediaSessionQuotaBytes,
		store.settings.MediaMaxItemsPerSession, store.settings.MediaMaxLongSide
}

func (store *storeEngine) mediaRoot(key Key) string {
	return store.artifacts.MediaRoot(layoutScope(key))
}

func (store *storeEngine) mediaItemDir(key Key, digest string) string {
	return filepath.Join(store.mediaRoot(key), digest)
}

func (store *storeEngine) mediaItemMetaPath(key Key, digest string) string {
	return filepath.Join(store.mediaRoot(key), digest, "meta.json")
}

func mediaHash(data []byte) string {
	return storeartifact.MediaHash(data)
}

func sanitizeMediaName(name, digest string) string {
	return storeartifact.SanitizeMediaName(name, digest)
}

func isReservedWindowsName(name string) bool {
	return storeartifact.IsReservedWindowsName(name)
}

func (store *storeEngine) writeMediaLocked(key Key, item MediaItem) (MediaRef, error) {
	return store.artifacts.WriteMedia(layoutScope(key), item)
}

func (store *storeEngine) readMediaLocked(key Key, ref string) (MediaRef, []byte, error) {
	return store.artifacts.ReadMedia(layoutScope(key), ref)
}

func (store *storeEngine) listMediaItems(key Key) ([]MediaRef, error) {
	return store.artifacts.ListMedia(layoutScope(key))
}

func (store *storeEngine) publishMediaIndexLocked(key Key) error {
	return store.artifacts.PublishMediaIndex(layoutScope(key))
}

func (store *storeEngine) mediaGarbageCollectLocked(key Key, referenced map[string]bool, dryRun bool) ([]string, error) {
	return store.artifacts.CollectMedia(layoutScope(key), referenced, dryRun)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
