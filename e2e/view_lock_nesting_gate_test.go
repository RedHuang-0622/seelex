package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// view_lock_nesting_gate_test.go 是 P20 的**机械门禁**：禁止"持 `Core.ViewMu` 再取同一把锁"。
//
// 现场（2026-10-17）：3 个 `go test ./application/core/` 并发跑，第一轮就挂死两个——
// `TestTurnStatusSpeaksOneWordOnSnapshotAndArchive` 先 `ViewMu.RLock()` 再调 `Snapshot()`
// （内部又 `RLock()`），而目录刷新 worker 正排在 `Coordinator.publishCatalogMirror` 的
// `ViewMu.Lock()` 后面；`sync.RWMutex` 有写者排队时**新读者一律阻塞**，于是那条嵌套读锁
// 永久阻塞（dump 见 `tmp/reshape/p20-r1-0.txt`）。
//
// 这条门禁按**函数体**扫源码：同一个函数里既有 `ViewMu.RLock()` 又有 `service.Snapshot()`
// 即算嵌套嫌疑（生产侧 8 处 `Snapshot()` 调用点都在解锁之后，只有测试踩过这一格）。
// 判定允许显式豁免：在函数上方一行写 `// view-lock-nesting: allow`（并有理由）。

var (
	nestedGateReadLock = regexp.MustCompile(`ViewMu\.(R?Lock)\(\)`)
	nestedGateUnlock   = regexp.MustCompile(`ViewMu\.(R?Unlock)\(\)`)
	nestedGateSnapshot = regexp.MustCompile(`\.Snapshot\(\)`)
)

func TestNoNestedViewLockInApplicationCore(t *testing.T) {
	root := repoRoot()
	dir := filepath.Join(root, "application", "core")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("读不到 %s：%v", dir, err)
	}
	var offenders []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读 %s：%v", path, err)
		}
		for name, body := range splitFuncs(string(source)) {
			if strings.Contains(body, "view-lock-nesting: allow") {
				continue
			}
			if holdsViewLockAtSnapshot(body) {
				offenders = append(offenders, entry.Name()+" "+name)
			}
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("这些函数在持有 Core.ViewMu 读锁时又调 Snapshot()（RWMutex 不可重入，有写者排队即死锁）：\n  %s\n"+
			"改法：先调 Snapshot() 再取锁，或改用 `service.Core.Snapshot` / `*Locked` 读面；"+
			"确有必要的写 `// view-lock-nesting: allow` 并说明理由", strings.Join(offenders, "\n  "))
	}
}

// holdsViewLockAtSnapshot 报告函数体里是否在**持锁期间**（没先 Unlock）调用了 `Snapshot()`。
// 计数法：`ViewMu.RLock()/Lock()` 加一、`ViewMu.RUnlock()/Unlock()` 减一；遇到 `.Snapshot()`
// 时计数 > 0 即命中（"先读数据、解锁、再 Snapshot"的常见写法因此不会误报）。
func holdsViewLockAtSnapshot(body string) bool {
	depth := 0
	for _, line := range strings.Split(stripComments(body), "\n") {
		if nestedGateSnapshot.MatchString(line) && depth > 0 {
			return true
		}
		for _, match := range nestedGateReadLock.FindAllStringSubmatch(line, -1) {
			_ = match
			depth++
		}
		for range nestedGateUnlock.FindAllStringSubmatch(line, -1) {
			if depth > 0 {
				depth--
			}
		}
	}
	return false
}

// stripComments 去掉行注释与块注释（门禁只看代码，不数注释里提到的锁调用）。
func stripComments(body string) string {
	var out strings.Builder
	inBlock := false
	for _, line := range strings.Split(body, "\n") {
		if inBlock {
			if end := strings.Index(line, "*/"); end >= 0 {
				line = line[end+2:]
				inBlock = false
			} else {
				continue
			}
		}
		if start := strings.Index(line, "/*"); start >= 0 {
			if end := strings.Index(line[start:], "*/"); end >= 0 {
				line = line[:start] + line[start+end+2:]
			} else {
				line = line[:start]
				inBlock = true
			}
		}
		if index := strings.Index(line, "//"); index >= 0 {
			line = line[:index]
		}
		out.WriteString(line)
		out.WriteString("\n")
	}
	return out.String()
}

// splitFuncs 把源码粗切成「func 头一行（含紧邻上方注释） + 函数体」，按函数名索引。
func splitFuncs(source string) map[string]string {
	lines := strings.Split(source, "\n")
	result := map[string]string{}
	current := ""
	var body []string
	flush := func() {
		if current != "" {
			result[current] = strings.Join(body, "\n")
		}
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "func ") {
			flush()
			current = line
			body = []string{line}
			continue
		}
		if current != "" {
			body = append(body, line)
		}
	}
	flush()
	return result
}
