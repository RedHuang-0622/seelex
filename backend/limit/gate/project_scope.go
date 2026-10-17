package security

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// ProjectScope resolves tool paths inside the project bound to the executing
// session. 根按**会话键**分格（见 BindFor）：多项目并行时，每个会话的
// read_file/write_file/bash 只解析自己的项目根；会话键为空（""）是进程默认
// 根（当前视图会话），供未声明会话键的调用面回退。
//
// It deliberately has no fallback root: an unbound scope cannot access the
// process working directory.
type ProjectScope struct {
	mu    sync.RWMutex
	roots map[string]scopeRoot
}

// scopeRoot 是一个已解析的项目根（原始绝对路径 + 真实路径，后者用于
// symlink 越界判定）。
type scopeRoot struct {
	root     string
	realRoot string
}

// DefaultScopeKey 是进程默认根（当前视图会话）的会话键。
const DefaultScopeKey = ""

func NewProjectScope() *ProjectScope { return &ProjectScope{roots: make(map[string]scopeRoot)} }

func (scope *ProjectScope) Bind(rootPath string) error {
	return scope.BindFor(DefaultScopeKey, rootPath)
}

// BindFor 绑定指定会话键的项目根；sessionKey 为空 = 进程默认根。同键重绑
// 覆盖（切项目/恢复会话），不同键互不影响——这是"后台会话不再借用视图会话
// 项目根"的落点。
func (scope *ProjectScope) BindFor(sessionKey, rootPath string) error {
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
	resolved := scopeRoot{root: filepath.Clean(absPath), realRoot: filepath.Clean(realRoot)}
	scope.mu.Lock()
	if scope.roots == nil {
		scope.roots = make(map[string]scopeRoot)
	}
	scope.roots[sessionKey] = resolved
	scope.mu.Unlock()
	return nil
}

func (scope *ProjectScope) Unbind() {
	scope.UnbindFor(DefaultScopeKey)
}

// UnbindFor 清空指定会话键的项目根（会话解绑工作区/归档时调用；其它会话的
// 根不受影响）。
func (scope *ProjectScope) UnbindFor(sessionKey string) {
	scope.mu.Lock()
	if scope.roots != nil {
		delete(scope.roots, sessionKey)
	}
	scope.mu.Unlock()
}

func (scope *ProjectScope) Root() string {
	return scope.RootFor(DefaultScopeKey)
}

// RootFor 返回指定会话键的项目根；该键未绑定时回退进程默认根（未绑定同样
// 返回空串）。
func (scope *ProjectScope) RootFor(sessionKey string) string {
	root, _, err := scope.rootsFor(sessionKey)
	if err != nil {
		return ""
	}
	return root
}

func (scope *ProjectScope) ResolveRead(path string) (string, error) {
	return scope.ResolveReadFor(DefaultScopeKey, path)
}

// ResolveReadFor 在指定会话键的项目根内解析读路径。
func (scope *ProjectScope) ResolveReadFor(sessionKey, path string) (string, error) {
	root, realRoot, err := scope.rootsFor(sessionKey)
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
	return scope.ResolveWriteFor(DefaultScopeKey, path)
}

// ResolveWriteFor 在指定会话键的项目根内解析写路径（语义同 ResolveWrite）。
func (scope *ProjectScope) ResolveWriteFor(sessionKey, path string) (string, error) {
	root, realRoot, err := scope.rootsFor(sessionKey)
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
	return scope.ResolveWorkdirFor(DefaultScopeKey, path)
}

// ResolveWorkdirFor 解析指定会话键下的工作目录（bash 的 cwd）。
func (scope *ProjectScope) ResolveWorkdirFor(sessionKey, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return scope.ResolveReadFor(sessionKey, ".")
	}
	return scope.ResolveReadFor(sessionKey, path)
}

// rootsFor 返回会话键对应的根：该键已绑定用该键，否则回退进程默认根（旧会话
// 与未声明会话键的调用面保持原语义）；两者都没有则 fail closed。
func (scope *ProjectScope) rootsFor(sessionKey string) (string, string, error) {
	scope.mu.RLock()
	defer scope.mu.RUnlock()
	if resolved, ok := scope.roots[sessionKey]; ok {
		return resolved.root, resolved.realRoot, nil
	}
	if resolved, ok := scope.roots[DefaultScopeKey]; ok {
		return resolved.root, resolved.realRoot, nil
	}
	return "", "", fmt.Errorf("project scope: no project is bound to this session")
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

// Relative 返回 path 相对项目根的显示路径；path 在根内时返回相对形式。
func (scope *ProjectScope) Relative(path string) (string, error) {
	return scope.RelativeFor(DefaultScopeKey, path)
}

// RelativeFor 返回指定会话键下 path 相对其项目根的显示路径。
func (scope *ProjectScope) RelativeFor(sessionKey, path string) (string, error) {
	root, _, err := scope.rootsFor(sessionKey)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return filepath.Clean(path), err
	}
	return rel, nil
}

// ResolveInside 解析任意路径到根内绝对路径（供根包 scoped_tools 复用）。
func ResolveInside(root, rawPath string) (string, error) {
	return resolveInside(root, rawPath)
}
