package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
	storelayout "github.com/RedHuang-0622/seelex/store/internal/layout"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

const MediaRefPrefix = "media:"

var (
	ErrMediaInvalid    = errors.New("session storage: media item requires name, mime type and bytes")
	ErrMediaTooLarge   = errors.New("session storage: media item exceeds max item bytes")
	ErrMediaDimensions = errors.New("session storage: media long side exceeds max long side")
	ErrMediaQuota      = errors.New("session storage: media session quota exceeded")
	ErrMediaItemLimit  = errors.New("session storage: media item count limit reached")
	ErrMediaRefInvalid = errors.New("session storage: media ref invalid")
)

// MediaItem is one media write request.
type MediaItem struct {
	Name     string
	MimeType string
	Kind     string
	Data     []byte
	Width    int
	Height   int
	Scale    float64
}

// MediaRef is one immutable content-addressed media asset.
type MediaRef = protocol.MediaRef

// MediaHead is metadata/media.json payload.
type MediaHead struct {
	SessionID  string     `json:"session_id"`
	Items      []MediaRef `json:"items"`
	TotalBytes int        `json:"total_bytes"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func (s *Store) mediaItemDir(scope storelayout.Scope, digest string) string {
	return filepath.Join(s.MediaRoot(scope), digest)
}

func (s *Store) mediaMetaPath(scope storelayout.Scope, digest string) string {
	return filepath.Join(s.mediaItemDir(scope, digest), "meta.json")
}

// WriteMedia stores an idempotent media asset.
func (s *Store) WriteMedia(scope storelayout.Scope, item MediaItem) (MediaRef, error) {
	lock := s.lock(scope, MediaLockID)
	lock.Lock()
	defer lock.Unlock()
	return s.writeMediaLocked(scope, item)
}

// ReadMedia reads media metadata and bytes.
func (s *Store) ReadMedia(scope storelayout.Scope, ref string) (MediaRef, []byte, error) {
	lock := s.lock(scope, MediaLockID)
	lock.Lock()
	defer lock.Unlock()
	return s.readMediaLocked(scope, ref)
}

// MediaPath returns one stored media asset's on-disk path inside the session
// partition (read-only fact; the layout owner is this file).
//
// 用途与边界：GUI 的"点开缩略图，用系统默认图片查看器看原图"需要真实文件位置。
// 路径只在宿主进程内使用，不回渲染层。ref 非法、或该分区里没有这条资产，都显式
// 失败——绝不返回一条不存在的路径让上层去猜。
func (s *Store) MediaPath(scope storelayout.Scope, ref string) (string, error) {
	digest := MediaRefHash(ref)
	if digest == "" {
		return "", fmt.Errorf("%w: %q", ErrMediaRefInvalid, ref)
	}
	lock := s.lock(scope, MediaLockID)
	lock.Lock()
	defer lock.Unlock()
	item, err := s.readMediaMetaFile(scope, digest)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.mediaItemDir(scope, digest), item.Name), nil
}

// ListMedia lists media assets in stable creation/hash order.
func (s *Store) ListMedia(scope storelayout.Scope) ([]MediaRef, error) {
	lock := s.lock(scope, MediaLockID)
	lock.Lock()
	defer lock.Unlock()
	return s.listMediaItems(scope)
}

// CollectMedia removes unreferenced media directories.
func (s *Store) CollectMedia(scope storelayout.Scope, referenced map[string]bool, dryRun bool) ([]string, error) {
	lock := s.lock(scope, MediaLockID)
	lock.Lock()
	defer lock.Unlock()
	removed, err := s.mediaGarbageCollectLocked(scope, referenced, dryRun)
	if err != nil {
		return nil, err
	}
	if !dryRun && len(removed) > 0 {
		_ = s.publishMediaIndexLocked(scope)
	}
	return removed, nil
}

// PublishMediaIndex republishes metadata/media.json from the directory source.
func (s *Store) PublishMediaIndex(scope storelayout.Scope) error {
	lock := s.lock(scope, MediaLockID)
	lock.Lock()
	defer lock.Unlock()
	return s.publishMediaIndexLocked(scope)
}

func (s *Store) writeMediaLocked(scope storelayout.Scope, item MediaItem) (MediaRef, error) {
	if strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.MimeType) == "" || len(item.Data) == 0 {
		return MediaRef{}, ErrMediaInvalid
	}
	if s.settings.MediaMaxItemBytes > 0 && len(item.Data) > s.settings.MediaMaxItemBytes {
		return MediaRef{}, fmt.Errorf("%w: %d > %d", ErrMediaTooLarge, len(item.Data), s.settings.MediaMaxItemBytes)
	}
	if s.settings.MediaMaxLongSide > 0 && item.Width > 0 && item.Height > 0 {
		if longest := max(item.Width, item.Height); longest > s.settings.MediaMaxLongSide {
			return MediaRef{}, fmt.Errorf("%w: %dx%d > %d", ErrMediaDimensions, item.Width, item.Height, s.settings.MediaMaxLongSide)
		}
	}
	digest := mediaHash(item.Data)
	if existing, err := s.readMediaMetaFile(scope, digest); err == nil {
		if name := sanitizeMediaName(item.Name, digest); name != existing.Name && !containsString(existing.Aliases, name) {
			existing.Aliases = append(existing.Aliases, name)
			if err := s.writeMediaMetaFile(scope, existing); err != nil {
				return MediaRef{}, err
			}
			if err := s.publishMediaIndexLocked(scope); err != nil {
				return MediaRef{}, err
			}
		}
		return existing, nil
	}
	items, err := s.listMediaItems(scope)
	if err != nil {
		return MediaRef{}, err
	}
	if s.settings.MediaMaxItemsPerSession > 0 && len(items) >= s.settings.MediaMaxItemsPerSession {
		return MediaRef{}, fmt.Errorf("%w: %d >= %d", ErrMediaItemLimit, len(items), s.settings.MediaMaxItemsPerSession)
	}
	usage, err := s.mediaUsageBytes(scope)
	if err != nil {
		return MediaRef{}, err
	}
	if s.settings.MediaSessionQuotaBytes > 0 && usage+len(item.Data) > s.settings.MediaSessionQuotaBytes {
		return MediaRef{}, fmt.Errorf("%w: %d + %d > %d", ErrMediaQuota, usage, len(item.Data), s.settings.MediaSessionQuotaBytes)
	}
	ref := MediaRef{
		Ref: MediaRefPrefix + digest, Hash: digest, Digest: "sha256:" + digest,
		Name: sanitizeMediaName(item.Name, digest), MimeType: item.MimeType, Kind: item.Kind,
		Bytes: len(item.Data), Width: item.Width, Height: item.Height, Scale: item.Scale,
		SessionID: scope.SessionID, CreatedAt: time.Now().UTC(),
	}
	dir := s.mediaItemDir(scope, digest)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return MediaRef{}, err
	}
	if err := storeroot.WriteAtomic(filepath.Join(dir, ref.Name), item.Data, 0o600); err != nil {
		return MediaRef{}, err
	}
	if err := s.writeMediaMetaFile(scope, ref); err != nil {
		_ = os.RemoveAll(dir)
		return MediaRef{}, err
	}
	if err := s.publishMediaIndexLocked(scope); err != nil {
		_ = os.RemoveAll(dir)
		return MediaRef{}, err
	}
	return ref, nil
}

func (s *Store) readMediaLocked(scope storelayout.Scope, ref string) (MediaRef, []byte, error) {
	digest := MediaRefHash(ref)
	if digest == "" {
		return MediaRef{}, nil, fmt.Errorf("%w: %q", ErrMediaRefInvalid, ref)
	}
	item, err := s.readMediaMetaFile(scope, digest)
	if err != nil {
		return MediaRef{}, nil, err
	}
	data, err := os.ReadFile(filepath.Join(s.mediaItemDir(scope, digest), item.Name))
	if err != nil {
		return MediaRef{}, nil, err
	}
	if len(data) != item.Bytes {
		return MediaRef{}, nil, fmt.Errorf("session storage: media %s size mismatch: %d != %d", item.Name, len(data), item.Bytes)
	}
	return item, data, nil
}

func (s *Store) readMediaMetaFile(scope storelayout.Scope, digest string) (MediaRef, error) {
	data, err := os.ReadFile(s.mediaMetaPath(scope, digest))
	if err != nil {
		return MediaRef{}, err
	}
	var item MediaRef
	if err := json.Unmarshal(data, &item); err != nil {
		return MediaRef{}, err
	}
	if item.Hash != digest || item.Name == "" {
		return MediaRef{}, fmt.Errorf("session storage: media index corrupt for %s", digest)
	}
	return item, nil
}

func (s *Store) writeMediaMetaFile(scope storelayout.Scope, item MediaRef) error {
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return err
	}
	return storeroot.WriteAtomic(s.mediaMetaPath(scope, item.Hash), data, 0o600)
}

func (s *Store) listMediaItems(scope storelayout.Scope) ([]MediaRef, error) {
	entries, err := os.ReadDir(s.MediaRoot(scope))
	if errors.Is(err, fs.ErrNotExist) {
		return []MediaRef{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]MediaRef, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		item, err := s.readMediaMetaFile(scope, entry.Name())
		if err != nil {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].Hash < items[j].Hash
	})
	return items, nil
}

func (s *Store) mediaUsageBytes(scope storelayout.Scope) (int, error) {
	total := 0
	err := filepath.WalkDir(s.MediaRoot(scope), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() || entry.Name() == "meta.json" {
			return nil
		}
		if info, infoErr := entry.Info(); infoErr == nil {
			total += int(info.Size())
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return 0, err
	}
	return total, nil
}

func (s *Store) publishMediaIndexLocked(scope storelayout.Scope) error {
	items, err := s.listMediaItems(scope)
	if err != nil {
		return err
	}
	usage, err := s.mediaUsageBytes(scope)
	if err != nil {
		return err
	}
	head := MediaHead{SessionID: scope.SessionID, Items: items, TotalBytes: usage, UpdatedAt: time.Now().UTC()}
	raw, _ := json.Marshal(head)
	commitID := "head-media-" + storeroot.Hash(string(raw))
	_, err = s.layout.Publish(scope, "media", commitID, head, head.UpdatedAt)
	return err
}

func (s *Store) mediaGarbageCollectLocked(scope storelayout.Scope, referenced map[string]bool, dryRun bool) ([]string, error) {
	items, err := s.listMediaItems(scope)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, item := range items {
		if referenced[item.Hash] {
			continue
		}
		removed = append(removed, item.Hash)
		if !dryRun {
			if err := os.RemoveAll(s.mediaItemDir(scope, item.Hash)); err != nil {
				return removed, err
			}
		}
	}
	return removed, nil
}

// MediaHead reads the published session media index.
func (s *Store) ReadMediaHead(scope storelayout.Scope) (MediaHead, error) {
	head, err := s.layout.ReadHead(scope, "media")
	if err != nil {
		return MediaHead{}, err
	}
	var decoded MediaHead
	if err := json.Unmarshal(head.Payload, &decoded); err != nil {
		return MediaHead{}, err
	}
	return decoded, nil
}

func mediaHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// MediaHash exposes the content-addressing digest for compatibility adapters.
func MediaHash(data []byte) string { return mediaHash(data) }

// SanitizeMediaName exposes the single media filename sanitizer.
func SanitizeMediaName(name, digest string) string { return sanitizeMediaName(name, digest) }

// IsReservedWindowsName reports Windows device names handled by the sanitizer.
func IsReservedWindowsName(name string) bool { return isReservedWindowsName(name) }

// MediaRefHash normalizes media:<sha256>.
func MediaRefHash(ref string) string {
	trimmed := strings.TrimSpace(ref)
	if !strings.HasPrefix(trimmed, MediaRefPrefix) {
		return ""
	}
	return strings.TrimPrefix(trimmed, MediaRefPrefix)
}

func sanitizeMediaName(name, digest string) string {
	base := strings.TrimSpace(filepath.Base(strings.ReplaceAll(name, "\\", "/")))
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case 0, '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		if r < 0x20 {
			return '_'
		}
		return r
	}, base)
	cleaned = strings.Trim(cleaned, " .")
	if cleaned == "" || cleaned == "." {
		return digest + ".bin"
	}
	if len(cleaned) > 120 {
		ext := filepath.Ext(cleaned)
		stem := strings.TrimSuffix(cleaned, ext)
		if len(stem) > 120-len(ext) {
			stem = stem[:120-len(ext)]
		}
		cleaned = stem + ext
	}
	if isReservedWindowsName(cleaned) {
		return "_" + cleaned
	}
	return cleaned
}

func isReservedWindowsName(name string) bool {
	stem := strings.ToUpper(strings.TrimSuffix(name, filepath.Ext(name)))
	switch stem {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(stem) == 4 {
		prefix, digit := stem[:3], stem[3]
		if digit >= '1' && digit <= '9' && (prefix == "COM" || prefix == "LPT") {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
