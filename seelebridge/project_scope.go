package seelebridge

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// ProjectScope resolves tool paths inside the project bound to the active
// session. It deliberately has no fallback root: an unbound session cannot
// access the process working directory.
type ProjectScope struct {
	mu       sync.RWMutex
	root     string
	realRoot string
}

func NewProjectScope() *ProjectScope { return &ProjectScope{} }

func (scope *ProjectScope) Bind(rootPath string) error {
	rootPath = strings.TrimSpace(rootPath)
	if rootPath == "" {
		return fmt.Errorf("project scope: root path is required")
	}
	absPath, err := filepath.Abs(rootPath)
	if err != nil {
		return fmt.Errorf("project scope: resolve root: %w", err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("project scope: inspect root: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("project scope: root is not a directory: %s", absPath)
	}
	realRoot, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return fmt.Errorf("project scope: resolve root links: %w", err)
	}
	scope.mu.Lock()
	scope.root = filepath.Clean(absPath)
	scope.realRoot = filepath.Clean(realRoot)
	scope.mu.Unlock()
	return nil
}

func (scope *ProjectScope) Unbind() {
	scope.mu.Lock()
	scope.root = ""
	scope.realRoot = ""
	scope.mu.Unlock()
}

func (scope *ProjectScope) Root() string {
	scope.mu.RLock()
	defer scope.mu.RUnlock()
	return scope.root
}

func (scope *ProjectScope) ResolveRead(path string) (string, error) {
	root, realRoot, err := scope.roots()
	if err != nil {
		return "", err
	}
	candidate, err := resolveInside(root, path)
	if err != nil {
		return "", err
	}
	realPath, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("project scope: resolve %q: %w", path, err)
	}
	if !withinRoot(realRoot, realPath) {
		return "", fmt.Errorf("project scope: path %q escapes the bound project", path)
	}
	return candidate, nil
}

// ResolveWrite permits a new path only when its nearest existing ancestor is
// inside the real project root. This prevents writes through a symlinked
// directory while still allowing write_file to create missing parents.
func (scope *ProjectScope) ResolveWrite(path string) (string, error) {
	root, realRoot, err := scope.roots()
	if err != nil {
		return "", err
	}
	candidate, err := resolveInside(root, path)
	if err != nil {
		return "", err
	}
	ancestor := candidate
	for {
		if _, statErr := os.Lstat(ancestor); statErr == nil {
			break
		} else if !os.IsNotExist(statErr) {
			return "", fmt.Errorf("project scope: inspect %q: %w", ancestor, statErr)
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", fmt.Errorf("project scope: no existing parent for %q", path)
		}
		ancestor = parent
	}
	realAncestor, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", fmt.Errorf("project scope: resolve parent for %q: %w", path, err)
	}
	if !withinRoot(realRoot, realAncestor) {
		return "", fmt.Errorf("project scope: path %q escapes the bound project", path)
	}
	return candidate, nil
}

func (scope *ProjectScope) ResolveWorkdir(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return scope.ResolveRead(".")
	}
	return scope.ResolveRead(path)
}

func (scope *ProjectScope) roots() (string, string, error) {
	scope.mu.RLock()
	defer scope.mu.RUnlock()
	if scope.root == "" || scope.realRoot == "" {
		return "", "", fmt.Errorf("project scope: no project is bound to this session")
	}
	return scope.root, scope.realRoot, nil
}

func resolveInside(root, rawPath string) (string, error) {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		rawPath = "."
	}
	candidate := rawPath
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	absPath, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("project scope: resolve path %q: %w", rawPath, err)
	}
	absPath = filepath.Clean(absPath)
	if !withinRoot(root, absPath) {
		return "", fmt.Errorf("project scope: path %q is outside the bound project", rawPath)
	}
	return absPath, nil
}

func withinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil || filepath.IsAbs(rel) || rel == ".." {
		return false
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(root), filepath.Clean(candidate)) || !strings.HasPrefix(strings.ToLower(rel), ".."+string(filepath.Separator))
	}
	return true
}
