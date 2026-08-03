// Package workspace manages project workspace directories, git remotes,
// and session-to-workspace associations.
package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Info holds a workspace definition.
type Info struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	RootPath  string    `json:"root_path"`
	GitRemote string    `json:"git_remote,omitempty"` // e.g. https://github.com/user/repo
	CreatedAt time.Time `json:"created_at"`
}

// SessionBinding ties a session to at most one workspace.
type SessionBinding struct {
	SessionID   string `json:"session_id"`
	WorkspaceID string `json:"workspace_id,omitempty"`
}

// ErrNotFound is returned when a workspace or binding is not found.
var ErrNotFound = errors.New("workspace: not found")

// repoSnapshot is the on-disk representation of the workspace repository.
type repoSnapshot struct {
	Workspaces map[string]Info   `json:"workspaces"`
	Bindings   map[string]string `json:"bindings"`
}

// Repo manages workspace CRUD and session bindings.
// When savePath is non-empty, every mutation auto-persists to disk.
type Repo struct {
	mu         sync.RWMutex
	workspaces map[string]Info
	bindings   map[string]string // sessionID → workspaceID
	savePath   string            // path to workspace_index.json (empty = no persistence)
}

// NewRepo creates an empty workspace repository without persistence.
func NewRepo() *Repo {
	return &Repo{
		workspaces: make(map[string]Info),
		bindings:   make(map[string]string),
	}
}

// NewRepoWithStore creates a workspace repository that persists to the given
// directory as workspace_index.json. Returns an error if the file exists but
// cannot be read.
func NewRepoWithStore(storePath string) (*Repo, error) {
	r := &Repo{
		workspaces: make(map[string]Info),
		bindings:   make(map[string]string),
		savePath:   filepath.Join(storePath, "workspace_index.json"),
	}
	if err := r.Load(); err != nil && !errors.Is(err, ErrNotFound) && !os.IsNotExist(err) {
		return nil, fmt.Errorf("workspace: load index: %w", err)
	}
	return r, nil
}

// Save writes the current workspaces and bindings to workspace_index.json.
func (r *Repo) Save() error {
	if r.savePath == "" {
		return nil
	}
	r.mu.RLock()
	snap := repoSnapshot{
		Workspaces: make(map[string]Info, len(r.workspaces)),
		Bindings:   make(map[string]string, len(r.bindings)),
	}
	for k, v := range r.workspaces {
		snap.Workspaces[k] = v
	}
	for k, v := range r.bindings {
		snap.Bindings[k] = v
	}
	r.mu.RUnlock()

	if err := os.MkdirAll(filepath.Dir(r.savePath), 0755); err != nil {
		return fmt.Errorf("workspace: create data dir: %w", err)
	}
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("workspace: marshal index: %w", err)
	}
	if err := writeFileAtomic(r.savePath, b, 0644); err != nil {
		return fmt.Errorf("workspace: write index atomically: %w", err)
	}
	return nil
}

// Load reads workspace_index.json and populates the repo.
// Returns ErrNotFound if the file does not exist.
func (r *Repo) Load() error {
	if r.savePath == "" {
		return nil
	}
	data, err := os.ReadFile(r.savePath)
	if err != nil {
		if os.IsNotExist(err) {
			return err // caller checks os.IsNotExist
		}
		return fmt.Errorf("workspace: read index: %w", err)
	}
	var snap repoSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("workspace: unmarshal index: %w", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if snap.Workspaces != nil {
		r.workspaces = snap.Workspaces
	} else {
		r.workspaces = make(map[string]Info)
	}
	if snap.Bindings != nil {
		r.bindings = snap.Bindings
	} else {
		r.bindings = make(map[string]string)
	}
	return nil
}

// ── Workspace CRUD ──────────────────────────────────────────

func (r *Repo) Create(name, rootPath, gitRemote string) (Info, error) {
	name = strings.TrimSpace(name)
	rootPath = strings.TrimSpace(rootPath)
	if name == "" {
		return Info{}, fmt.Errorf("workspace name is required")
	}
	absPath, err := filepath.Abs(rootPath)
	if err != nil {
		return Info{}, fmt.Errorf("resolve workspace path: %w", err)
	}
	if info, statErr := os.Stat(absPath); statErr != nil || !info.IsDir() {
		return Info{}, fmt.Errorf("workspace path is not a directory: %s", absPath)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// deduplicate by path
	for _, w := range r.workspaces {
		if w.RootPath == absPath {
			return w, nil
		}
	}

	id := fmt.Sprintf("ws-%d", time.Now().UnixNano())
	for {
		if _, exists := r.workspaces[id]; !exists {
			break
		}
		id += "-1"
	}
	w := Info{
		ID:        id,
		Name:      name,
		RootPath:  absPath,
		GitRemote: strings.TrimSpace(gitRemote),
		CreatedAt: time.Now(),
	}
	r.workspaces[id] = w

	if err := r.saveLocked(); err != nil {
		delete(r.workspaces, id)
		return Info{}, err
	}
	return w, nil
}

func (r *Repo) Get(id string) (Info, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	w, ok := r.workspaces[id]
	if !ok {
		return Info{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return w, nil
}

func (r *Repo) List() []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Info, 0, len(r.workspaces))
	for _, w := range r.workspaces {
		result = append(result, w)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result
}

func (r *Repo) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.workspaces[id]; !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	workspace := r.workspaces[id]
	removedBindings := make(map[string]string)
	delete(r.workspaces, id)
	// remove all bindings to this workspace
	for sid, wid := range r.bindings {
		if wid == id {
			removedBindings[sid] = wid
			delete(r.bindings, sid)
		}
	}
	if err := r.saveLocked(); err != nil {
		r.workspaces[id] = workspace
		for sid, wid := range removedBindings {
			r.bindings[sid] = wid
		}
		return err
	}
	return nil
}

func (r *Repo) UpdateGitRemote(id, remote string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.workspaces[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	previousRemote := w.GitRemote
	w.GitRemote = strings.TrimSpace(remote)
	r.workspaces[id] = w
	if err := r.saveLocked(); err != nil {
		w.GitRemote = previousRemote
		r.workspaces[id] = w
		return err
	}
	return nil
}

// ── Session bindings ────────────────────────────────────────

func (r *Repo) BindSession(sessionID, workspaceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if workspaceID == "" {
		delete(r.bindings, sessionID)
	} else {
		r.bindings[sessionID] = workspaceID
	}
	_ = r.saveLocked()
}

func (r *Repo) UnbindSession(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.bindings, sessionID)
	_ = r.saveLocked()
}

func (r *Repo) SessionWorkspace(sessionID string) (Info, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	wid, ok := r.bindings[sessionID]
	if !ok {
		return Info{}, false
	}
	w, ok := r.workspaces[wid]
	return w, ok
}

// AllBindings returns all session→workspace bindings.
func (r *Repo) AllBindings() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]string, len(r.bindings))
	for sid, wid := range r.bindings {
		out[sid] = wid
	}
	return out
}

func (r *Repo) WorkspaceSessions(workspaceID string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var sessions []string
	for sid, wid := range r.bindings {
		if wid == workspaceID {
			sessions = append(sessions, sid)
		}
	}
	sort.Strings(sessions)
	return sessions
}

// saveLocked persists the repo without acquiring the lock (caller holds mu).
func (r *Repo) saveLocked() error {
	if r.savePath == "" {
		return nil
	}
	snap := repoSnapshot{
		Workspaces: make(map[string]Info, len(r.workspaces)),
		Bindings:   make(map[string]string, len(r.bindings)),
	}
	for k, v := range r.workspaces {
		snap.Workspaces[k] = v
	}
	for k, v := range r.bindings {
		snap.Bindings[k] = v
	}

	if err := os.MkdirAll(filepath.Dir(r.savePath), 0755); err != nil {
		return fmt.Errorf("workspace: create data dir: %w", err)
	}
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("workspace: marshal index: %w", err)
	}
	if err := writeFileAtomic(r.savePath, b, 0644); err != nil {
		return fmt.Errorf("workspace: write index atomically: %w", err)
	}
	return nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

// ── helpers ─────────────────────────────────────────────────

// DetectGitRemote 在给定目录中检测 git remote origin 地址。
// 如果目录不是 git 仓库或没有 remote，返回空字符串。
func DetectGitRemote(rootPath string) string {
	cmd := exec.Command("git", "-C", rootPath, "remote", "-v")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "origin\t") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1]
			}
		}
	}
	return ""
}
