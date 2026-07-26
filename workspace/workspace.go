// Package workspace manages project workspace directories, git remotes,
// and session-to-workspace associations.
package workspace

import (
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

// Repo manages workspace CRUD and session bindings in memory.
// Persistence is injected via Save/Load callbacks (see Manager).
type Repo struct {
	mu         sync.RWMutex
	workspaces map[string]Info
	bindings   map[string]string // sessionID → workspaceID
}

// NewRepo creates an empty workspace repository.
func NewRepo() *Repo {
	return &Repo{
		workspaces: make(map[string]Info),
		bindings:   make(map[string]string),
	}
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

	id := slug(name)
	w := Info{
		ID:        id,
		Name:      name,
		RootPath:  absPath,
		GitRemote: strings.TrimSpace(gitRemote),
		CreatedAt: time.Now(),
	}
	r.workspaces[id] = w
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
	delete(r.workspaces, id)
	// remove all bindings to this workspace
	for sid, wid := range r.bindings {
		if wid == id {
			delete(r.bindings, sid)
		}
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
	w.GitRemote = strings.TrimSpace(remote)
	r.workspaces[id] = w
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
}

func (r *Repo) UnbindSession(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.bindings, sessionID)
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

// ── helpers ─────────────────────────────────────────────────

func slug(name string) string {
	s := strings.ToLower(name)
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, s)
	s = strings.Trim(s, "-")
	if s == "" {
		s = "ws"
	}
	return s
}

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
