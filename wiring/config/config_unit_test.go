package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestResolveStorePathReusesStaleLock 钉住"陈旧锁不该继续避让"：`.lock` 里的 PID 已经
// 不在（0 是哨兵值，任何平台都判死）时，基路径本身就是答案，而不是一路 +
// `_1`、`_2`……把用户的历史会话拆到看不出规律的新目录里。
func TestResolveStorePathReusesStaleLock(t *testing.T) {
	base := filepath.Join(t.TempDir(), "sessions")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, ".lock"), []byte("0"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ResolveStorePath(base); got != base {
		t.Fatalf("陈旧锁应复用基路径：want %q, got %q", base, got)
	}
	// 认领之后同一路径不该再被第二个实例拿走（本进程 PID 活着）。
	if got := ResolveStorePath(base); got != base+"_1" {
		t.Fatalf("活锁应避让到 _1：want %q, got %q", base+"_1", got)
	}
}

// TestResolveStorePathBumpsWhileLockIsHeld 钉住避让的**有界**形态：同一个基路径连续认领
// 之后是 `_1`、`_2`…，且每一档都真的落了锁文件（下一次启动才判得出占用）。
func TestResolveStorePathBumpsWhileLockIsHeld(t *testing.T) {
	base := filepath.Join(t.TempDir(), "sessions")
	held := ResolveStorePath(base)
	if held != base {
		t.Fatalf("首个实例应拿到基路径，得到 %q", held)
	}
	for i := 1; i <= 3; i++ {
		got := ResolveStorePath(base)
		want := base + "_" + strconv.Itoa(i)
		if got != want {
			t.Fatalf("第 %d 个实例 = %q, want %q", i+1, got, want)
		}
		data, err := os.ReadFile(filepath.Join(got, ".lock"))
		if err != nil {
			t.Fatalf("认领后必须留下锁文件：%v", err)
		}
		if _, err := strconv.Atoi(strings.TrimSpace(string(data))); err != nil {
			t.Fatalf("锁文件里该是本进程 PID，得到 %q", data)
		}
	}
}

// TestProcessLivenessHasOneImplementation 是"一份判据 = 一处函数"的机械门禁：
// "这个 PID 还活着吗"只允许有**一处**实现（`store/root/ProcessAlive`，平台实现两份文件）。
// 第二份实现会被这条用例点名——历史事故（2026-09-18：被强杀的上一份 GUI 被误判成活持有者，
// 数据根锁永远不判陈旧）正是"两处判据各自演化"的产物。
func TestProcessLivenessHasOneImplementation(t *testing.T) {
	root := repoRoot(t)
	markers := []string{"WaitForSingleObject", "GetExitCodeProcess", "syscall.Signal(0)"}
	allowed := map[string]bool{
		"store/root/process_alive_windows.go": true,
		"store/root/process_alive_unix.go":    true,
	}
	var found []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "dist", "tmp", "vendor", "node_modules", ".gocache", "_scratch", "local":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		// 只认代码：注释里的同名字样是散文（store/root/data_root_lock.go 的
		// 事故说明就写着 WaitForSingleObject），不该被当成第二份实现。
		code := codeOnly(string(data))
		for _, marker := range markers {
			if strings.Contains(code, marker) {
				rel, _ := filepath.Rel(root, path)
				found = append(found, filepath.ToSlash(rel))
				return nil
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range found {
		if !allowed[path] {
			t.Errorf("进程存活判据出现了第二份实现：%s（唯一实现是 store/root.ProcessAlive）", path)
		}
	}
	for path := range allowed {
		seen := false
		for _, got := range found {
			if got == path {
				seen = true
			}
		}
		if !seen {
			t.Errorf("%s 不再包含存活判据的实现——门禁的阳性对照失效了", path)
		}
	}
}

// codeOnly 去掉行注释与块注释，只留可编译的那部分源码。
func codeOnly(text string) string {
	var out []string
	inBlock := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if inBlock {
			if index := strings.Index(line, "*/"); index >= 0 {
				inBlock = false
				line = line[index+2:]
			} else {
				continue
			}
		}
		if index := strings.Index(line, "/*"); index >= 0 {
			rest := line[index+2:]
			if end := strings.Index(rest, "*/"); end >= 0 {
				line = line[:index] + rest[end+2:]
			} else {
				inBlock = true
				line = line[:index]
			}
		}
		if index := strings.Index(line, "//"); index >= 0 {
			line = line[:index]
		}
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// repoRoot 向上找到 go.mod（与 e2e 的 testutil.RepoRoot 同一判据，这里只本包自用）。
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("找不到仓库根（go.mod）")
		}
		dir = parent
	}
}
