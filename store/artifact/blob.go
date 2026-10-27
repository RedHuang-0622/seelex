package artifact

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	storelayout "github.com/RedHuang-0622/seelex/store/internal/layout"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

const BlobRefPrefix = "blob:"

var (
	ErrBigToolResultTooLarge = errors.New("session storage: big tool result exceeds hard limit")
	ErrBigToolResultQuota    = errors.New("session storage: big tool result session quota exceeded")
)

// Blob is the stored oversized tool-output record.
type Blob struct {
	Hash      string    `json:"hash"`
	SessionID string    `json:"session_id"`
	Kind      string    `json:"kind"`
	Size      int       `json:"size"`
	Truncated bool      `json:"truncated,omitempty"`
	Content   string    `json:"content,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// BlobPath returns the content-addressed blob file.
func (s *Store) BlobPath(scope storelayout.Scope, hash string) string {
	return filepath.Join(s.BlobDir(scope), hash+".jsonl")
}

// ToolResultPath returns the immutable tool-result payload path.
func (s *Store) ToolResultPath(scope storelayout.Scope, ref string) string {
	return filepath.Join(s.BlobDir(scope), storeroot.Hash(ref)+".result.json")
}

// WriteBlob stores one oversized output record.
func (s *Store) WriteBlob(scope storelayout.Scope, toolName, content string) (Blob, error) {
	lock := s.lock(scope, BlobLockID)
	lock.Lock()
	defer lock.Unlock()
	if len(content) > s.settings.BlobHardLimitBytes {
		return Blob{}, ErrBigToolResultTooLarge
	}
	hash := storeroot.Hash(content)
	truncated := len(content) > s.settings.BlobSoftLimitChars
	if truncated {
		content = content[:s.settings.BlobSoftLimitChars]
	}
	blob := Blob{
		Hash: hash, SessionID: scope.SessionID, Kind: toolName,
		Size: len(content), Truncated: truncated, Content: content,
		CreatedAt: time.Now().UTC(),
	}
	dir := s.BlobDir(scope)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Blob{}, err
	}
	total := int64(0)
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, entry := range entries {
			if info, statErr := entry.Info(); statErr == nil {
				total += info.Size()
			}
		}
	}
	if total+int64(len(content)) > int64(s.settings.BlobSessionQuotaBytes) {
		return Blob{}, ErrBigToolResultQuota
	}
	data, err := json.Marshal(blob)
	if err != nil {
		return Blob{}, err
	}
	if err := storeroot.WriteAtomic(s.BlobPath(scope, hash), append(data, '\n'), 0o600); err != nil {
		return Blob{}, err
	}
	return blob, nil
}

// ReadBlob reads one blob payload.
func (s *Store) ReadBlob(scope storelayout.Scope, hash string) (string, error) {
	data, err := os.ReadFile(s.BlobPath(scope, hash))
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) == 0 {
		return "", errEmptyBlob
	}
	var blob Blob
	if err := json.Unmarshal([]byte(lines[0]), &blob); err != nil {
		return "", err
	}
	return blob.Content, nil
}

// ListBlobHashes lists stored blob hashes in stable order.
func (s *Store) ListBlobHashes(scope storelayout.Scope) ([]string, error) {
	entries, err := os.ReadDir(s.BlobDir(scope))
	if errors.Is(err, fs.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var hashes []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".jsonl") {
			hashes = append(hashes, strings.TrimSuffix(entry.Name(), ".jsonl"))
		}
	}
	sort.Strings(hashes)
	return hashes, nil
}

// CollectBlobs removes unreferenced blobs, or reports candidates for dry-run.
func (s *Store) CollectBlobs(scope storelayout.Scope, referenced map[string]bool, dryRun bool) ([]string, error) {
	lock := s.lock(scope, BlobLockID)
	lock.Lock()
	defer lock.Unlock()
	hashes, err := s.ListBlobHashes(scope)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, hash := range hashes {
		if referenced[hash] {
			continue
		}
		removed = append(removed, hash)
		if !dryRun {
			if err := os.Remove(s.BlobPath(scope, hash)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return removed, err
			}
		}
	}
	return removed, nil
}

// WriteToolResult atomically stores an immutable result JSON payload.
func (s *Store) WriteToolResult(scope storelayout.Scope, ref string, payload []byte) error {
	if strings.TrimSpace(ref) == "" {
		return errors.New("session storage: result ref is required")
	}
	path := s.ToolResultPath(scope, ref)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return storeroot.WriteAtomic(path, payload, 0o600)
}

// ReadToolResult reads an immutable result JSON payload.
func (s *Store) ReadToolResult(scope storelayout.Scope, ref string) ([]byte, error) {
	return os.ReadFile(s.ToolResultPath(scope, ref))
}
