// Package layout owns the session storage mechanism shared by store domains:
// physical paths, guide creation, module-head envelopes, checksums, and
// per-session module lock identity.
package layout

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

const (
	LayoutVersion = 1
	SchemaVersion = 1
)

// Scope is the physical project/session partition key.
type Scope struct {
	ProjectID string
	SessionID string
}

// ModuleID identifies one independently published metadata head.
type ModuleID string

// Guide is the stable session layout routing envelope.
type Guide struct {
	LayoutVersion int       `json:"layout_version"`
	SchemaVersion int       `json:"schema_version"`
	SessionID     string    `json:"session_id"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Head is the stable metadata/<module>.json envelope.
type Head struct {
	SchemaVersion int             `json:"schema_version"`
	SessionID     string          `json:"session_id"`
	ModuleID      ModuleID        `json:"module_id"`
	CommitID      string          `json:"commit_id,omitempty"`
	Checksum      string          `json:"checksum"`
	UpdatedAt     time.Time       `json:"updated_at"`
	Payload       json.RawMessage `json:"payload"`
}

// Layout is the composition-root-owned mechanism instance.
type Layout struct {
	root string

	mu          sync.Mutex
	moduleLocks map[string]map[ModuleID]*sync.Mutex
	guideLocks  map[string]*sync.Mutex
}

// New constructs a layout rooted at the session JSON data root.
func New(root string) *Layout {
	return &Layout{
		root:        filepath.Clean(root),
		moduleLocks: make(map[string]map[ModuleID]*sync.Mutex),
		guideLocks:  make(map[string]*sync.Mutex),
	}
}

// SessionRoot returns project-<hash>/session-<hash> beneath the data root.
func (l *Layout) SessionRoot(scope Scope) string {
	return filepath.Join(l.root, "project-"+storeroot.Hash(scope.ProjectID), "session-"+storeroot.Hash(scope.SessionID))
}

// MetadataDir returns the session metadata directory.
func (l *Layout) MetadataDir(scope Scope) string {
	return filepath.Join(l.SessionRoot(scope), "metadata")
}

// ModulePath returns metadata/<module>.json.
func (l *Layout) ModulePath(scope Scope, module ModuleID) string {
	return filepath.Join(l.MetadataDir(scope), string(module)+".json")
}

// GuidePath returns metadata/guide.json.
func (l *Layout) GuidePath(scope Scope) string {
	return filepath.Join(l.MetadataDir(scope), "guide.json")
}

// IsSessionDir reports whether a directory contains the JSON v8 guide.
func IsSessionDir(sessionRoot string) bool {
	_, err := os.Stat(filepath.Join(sessionRoot, "metadata", "guide.json"))
	return err == nil
}

// Lock returns one stable mutex for a session/module pair. Distinct module IDs
// never alias.
func (l *Layout) Lock(scope Scope, module ModuleID) *sync.Mutex {
	if module == "" {
		panic("store layout: empty module ID")
	}
	session := l.SessionRoot(scope)
	l.mu.Lock()
	defer l.mu.Unlock()
	locks := l.moduleLocks[session]
	if locks == nil {
		locks = make(map[ModuleID]*sync.Mutex)
		l.moduleLocks[session] = locks
	}
	lock := locks[module]
	if lock == nil {
		lock = &sync.Mutex{}
		locks[module] = lock
	}
	return lock
}

func (l *Layout) guideLock(scope Scope) *sync.Mutex {
	session := l.SessionRoot(scope)
	l.mu.Lock()
	defer l.mu.Unlock()
	lock := l.guideLocks[session]
	if lock == nil {
		lock = &sync.Mutex{}
		l.guideLocks[session] = lock
	}
	return lock
}

// EnsureGuide idempotently creates or reads the session guide.
func (l *Layout) EnsureGuide(scope Scope) (Guide, error) {
	lock := l.guideLock(scope)
	lock.Lock()
	defer lock.Unlock()
	path := l.GuidePath(scope)
	if data, err := os.ReadFile(path); err == nil {
		var guide Guide
		if json.Unmarshal(data, &guide) == nil && guide.SessionID == scope.SessionID {
			return guide, nil
		}
	}
	if err := os.MkdirAll(l.MetadataDir(scope), 0o700); err != nil {
		return Guide{}, err
	}
	now := time.Now().UTC()
	guide := Guide{
		LayoutVersion: LayoutVersion,
		SchemaVersion: SchemaVersion,
		SessionID:     scope.SessionID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	data, err := json.MarshalIndent(guide, "", "  ")
	if err != nil {
		return Guide{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Guide{}, err
	}
	if err := storeroot.WriteAtomic(path, data, 0o600); err != nil {
		return Guide{}, err
	}
	return guide, nil
}

// Checksum returns the checksum of the envelope excluding its checksum field.
func Checksum(head Head) (string, error) {
	copy := head
	copy.Checksum = ""
	data, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// PayloadCommitID derives a deterministic commit id from a module head payload.
// Storage never invents random ids or reuses constants (S17/D13).
func PayloadCommitID(module ModuleID, payload any) string {
	raw, _ := json.Marshal(payload)
	return "head-" + string(module) + "-" + storeroot.Hash(string(raw))
}

// Commit is the whole-replacement write of one module head: ensure the guide,
// derive the commit id when the caller has none, then publish under the
// session/module lock. Domain stores own their payload; layout owns the order.
func (l *Layout) Commit(scope Scope, module ModuleID, commitID string, payload any, updatedAt time.Time) error {
	if _, err := l.EnsureGuide(scope); err != nil {
		return err
	}
	if commitID == "" {
		commitID = PayloadCommitID(module, payload)
	}
	lock := l.Lock(scope, module)
	lock.Lock()
	defer lock.Unlock()
	_, err := l.Publish(scope, module, commitID, payload, updatedAt)
	return err
}

// Publish atomically writes a module head.
func (l *Layout) Publish(scope Scope, module ModuleID, commitID string, payload any, updatedAt time.Time) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("session storage: encode %s head payload: %w", module, err)
	}
	head := Head{
		SchemaVersion: SchemaVersion,
		SessionID:     scope.SessionID,
		ModuleID:      module,
		CommitID:      commitID,
		UpdatedAt:     updatedAt,
		Payload:       raw,
	}
	checksum, err := Checksum(head)
	if err != nil {
		return "", err
	}
	head.Checksum = checksum
	data, err := json.MarshalIndent(head, "", "  ")
	if err != nil {
		return "", err
	}
	path := l.ModulePath(scope, module)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := storeroot.WriteAtomic(path, data, 0o600); err != nil {
		return "", fmt.Errorf("session storage: publish %s head: %w", module, err)
	}
	return commitID, nil
}

// ReadHead reads and validates one module head.
func (l *Layout) ReadHead(scope Scope, module ModuleID) (Head, error) {
	path := l.ModulePath(scope, module)
	data, err := storeroot.ReadSharedFile(path)
	if err != nil {
		return Head{}, err
	}
	var head Head
	if err := json.Unmarshal(data, &head); err != nil {
		return Head{}, fmt.Errorf("session storage: decode %s head %q: %w", module, path, err)
	}
	if head.SchemaVersion != SchemaVersion {
		return Head{}, fmt.Errorf("session storage: %s head schema=%d want %d", module, head.SchemaVersion, SchemaVersion)
	}
	if head.SessionID != scope.SessionID {
		return Head{}, fmt.Errorf("session storage: %s head session %q != %q", module, head.SessionID, scope.SessionID)
	}
	if head.ModuleID != module {
		return Head{}, fmt.Errorf("session storage: head module %q != %q", head.ModuleID, module)
	}
	want, err := Checksum(head)
	if err != nil {
		return Head{}, err
	}
	if head.Checksum != "" && want != head.Checksum {
		return Head{}, fmt.Errorf("session storage: %s head checksum mismatch", module)
	}
	return head, nil
}

// ReadLenientHead reads only enough of the envelope to recover repair metadata.
func (l *Layout) ReadLenientHead(scope Scope, module ModuleID) (Head, bool) {
	data, err := storeroot.ReadSharedFile(l.ModulePath(scope, module))
	if err != nil {
		return Head{}, false
	}
	var head Head
	if json.Unmarshal(data, &head) != nil {
		return Head{}, false
	}
	if head.ModuleID != module || head.SessionID != scope.SessionID {
		return Head{}, false
	}
	return head, true
}

// ReadWithHeal performs the bounded invalid-head recovery sequence:
// raw read -> beforeRetry -> retry -> optional lock -> domain repair -> read.
// repair owns payload-specific rebuild semantics; layout owns only the order.
func (l *Layout) ReadWithHeal(
	scope Scope,
	module ModuleID,
	lockHeld bool,
	lock func() *sync.Mutex,
	beforeRetry func(),
	repair func(error) error,
) (Head, error) {
	head, err := l.ReadHead(scope, module)
	if err == nil || IsNotExist(err) {
		return head, err
	}
	if beforeRetry != nil {
		beforeRetry()
	}
	head, retryErr := l.ReadHead(scope, module)
	if retryErr == nil || IsNotExist(retryErr) {
		return head, retryErr
	}
	if lockHeld {
		if repair != nil && repair(err) == nil {
			return l.ReadHead(scope, module)
		}
		return Head{}, err
	}
	moduleLock := lock()
	if !moduleLock.TryLock() {
		head, finalErr := l.ReadHead(scope, module)
		if finalErr == nil || IsNotExist(finalErr) {
			return head, finalErr
		}
		return Head{}, err
	}
	defer moduleLock.Unlock()
	if head, retryErr = l.ReadHead(scope, module); retryErr == nil || IsNotExist(retryErr) {
		return head, retryErr
	}
	if repair != nil && repair(err) == nil {
		return l.ReadHead(scope, module)
	}
	return Head{}, err
}

// IsNotExist reports the shared missing-state classification.
func IsNotExist(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
