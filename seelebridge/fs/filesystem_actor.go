package fs

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ── 文件系统 Actor（资源操作的消息边界）─────────────────────
// 文件系统是多 actor（主代理 / 并行子代理 / 后台任务）共享的资源。按
// Actor 语义（actor.go 同构）：写路径（Write/Edit）经**按路径分片**的
// 串行队列——同一文件互斥、不同文件并行；读路径（Read）只读并发安全，
// 直读不进队列。
//
// 背景（docs/plan/file-operations-actorization.md P0）：并行子代理直接
// 调用 os.WriteFile 时，write 相互覆盖；edit_file 的 read-modify-write
// 非原子，后写者覆盖先写者。本 actor 把写操作串行化并原子化 edit。
//
// bash 写命令无法静态判定是否写文件——保守策略见 P1（本期未纳入，
// 工具层注释注明）。

// FileSystem 是文件系统操作的 Actor 消息边界。
// 实现必须遵守：Write/Edit 同路径串行（互斥），Read 并发安全。
type FileSystem interface {
	// Read 读取文件（并发安全，直读）。
	Read(path string) ([]byte, error)
	// Write 创建/覆盖文件（同路径串行）。
	Write(path string, content []byte) error
	// Edit 读-替换-写回（同路径串行且原子：锁内完成 read-modify-write）。
	// old_string 不存在时返回错误；成功返回替换次数。
	Edit(path, oldString, newString string) (int, error)
}

// fileSystemActor 是 FileSystem 的默认实现：per-path 写锁（分片串行化）。
// 锁表按需增长，路径数 = 活跃写文件数，有界。
type fileSystemActor struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex // path → 写锁（同文件串行、不同文件并行）
}

// NewFileSystemActor 构造文件系统 actor（写路径分片串行化）。
func NewFileSystemActor() FileSystem {
	return &fileSystemActor{locks: make(map[string]*sync.Mutex)}
}

func (a *fileSystemActor) Read(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (a *fileSystemActor) Write(path string, content []byte) error {
	lock := a.lockFor(path)
	lock.Lock()
	defer lock.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

func (a *fileSystemActor) Edit(path, oldString, newString string) (int, error) {
	lock := a.lockFor(path)
	lock.Lock()
	defer lock.Unlock()
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	text := string(content)
	if !strings.Contains(text, oldString) {
		return 0, os.ErrNotExist // 调用方转译为 "old_string not found"
	}
	count := strings.Count(text, oldString)
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(text, oldString, newString)), 0o644); err != nil {
		return 0, err
	}
	return count, nil
}

// lockFor 返回路径的写锁（按需创建）。
func (a *fileSystemActor) lockFor(path string) *sync.Mutex {
	a.mu.Lock()
	defer a.mu.Unlock()
	lock, ok := a.locks[path]
	if !ok {
		lock = &sync.Mutex{}
		a.locks[path] = lock
	}
	return lock
}
