package root

// 本文件是 `store/root`（基座机制层）**自己的**单元用例：只依赖标准库与被测包本身，
// 不经过 `store` 的 Router/Repository。
//
// 与 `store/` 侧用例的分工：`store/data_root_lock*_test.go`、`store/file_shared_read_*`
// 等钉的是"Router 打开数据根 → 锁行为 / 共享读与 rename 发布共存"的**集成面**；
// 这里钉的是机制本身的边界（原子发布的失败姿态、残尾裁定、存活判定、Settings 覆盖链）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAtomicCreatesOverwritesAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "head.json")

	if err := WriteAtomic(path, []byte("v1"), 0o600); err != nil {
		t.Fatalf("WriteAtomic(new): %v", err)
	}
	if got := readFile(t, path); got != "v1" {
		t.Fatalf("content after first write = %q, want %q", got, "v1")
	}

	if err := WriteAtomic(path, []byte("v2-longer"), 0o600); err != nil {
		t.Fatalf("WriteAtomic(overwrite): %v", err)
	}
	if got := readFile(t, path); got != "v2-longer" {
		t.Fatalf("content after overwrite = %q, want %q", got, "v2-longer")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "head.json" {
		t.Fatalf("原子发布后目录里应只剩目标文件（临时文件不得残留），实际 %v", entryNames(entries))
	}
}

// 失败姿态：目标不可替换（这里是"目标路径是个目录"）时，必须返回错误、
// 不破坏既有目标、也不留下 `.tmp` 残骸——半份文件会让读者读到坏 JSON。
func TestWriteAtomicFailureLeavesNoPartialTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "occupied")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	if err := WriteAtomic(target, []byte("payload"), 0o600); err == nil {
		t.Fatal("目标不可替换时必须显式失败（不静默吞掉）")
	}

	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Fatalf("失败后既有目标不得被破坏：info=%v err=%v", info, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("失败后不得留下临时文件，实际 %v", entryNames(entries))
	}
}

func TestHashIsStableAndDistinct(t *testing.T) {
	first := Hash("project-x")
	if first != Hash("project-x") {
		t.Fatal("Hash 必须稳定：同一输入同一次运行内两次调用应相等")
	}
	if first == Hash("project-y") {
		t.Fatal("Hash 必须区分不同输入")
	}
	if len(first) != 16 {
		t.Fatalf("Hash 取 sha256 前 8 字节的 hex（16 字符），实际 %q", first)
	}
}

func TestRandomIDIsHexAndUnique(t *testing.T) {
	seen := make(map[string]bool, 200)
	for i := 0; i < 200; i++ {
		id := RandomID()
		if len(id) != 16 {
			t.Fatalf("RandomID 应是 8 字节 hex（16 字符），实际 %q", id)
		}
		if seen[id] {
			t.Fatalf("RandomID 撞了：%q", id)
		}
		seen[id] = true
	}
}

func TestRemoveAllWithBackoff(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "f.jsonl"), []byte("x\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := RemoveAllWithBackoff(dir); err != nil {
		t.Fatalf("RemoveAllWithBackoff: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("目录应当已删除，stat err = %v", err)
	}
	// 目标不存在不是错误（可重入的回收动作）。
	if err := RemoveAllWithBackoff(dir); err != nil {
		t.Fatalf("对不存在的路径应返回 nil，实际 %v", err)
	}
}

// TruncateCrashTail 的残尾裁定：只有"末尾没有换行"才算残尾；残尾整段丢弃，
// 保证后续追加从完整行边界开始。
func TestTruncateCrashTailBoundaries(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{"完整行结尾（末尾换行）→ 原样", "{\"a\":1}\n{\"b\":2}\n", "{\"a\":1}\n{\"b\":2}\n"},
		{"末尾半行 → 截到最后一个换行之后", "{\"a\":1}\n{\"b\":", "{\"a\":1}\n"},
		{"整段都没有换行（纯残尾）→ 清空", "{\"a\":", ""},
		{"空文件 → 原样", "", ""},
		{"单字节换行 → 原样", "\n", "\n"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "stack.jsonl")
			if err := os.WriteFile(path, []byte(test.data), 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			file, err := os.OpenFile(path, os.O_RDWR, 0o600)
			if err != nil {
				t.Fatalf("OpenFile: %v", err)
			}
			defer file.Close()
			if err := TruncateCrashTail(file); err != nil {
				t.Fatalf("TruncateCrashTail: %v", err)
			}
			if got := readFile(t, path); got != test.want {
				t.Fatalf("截断后 = %q, want %q", got, test.want)
			}
		})
	}
}

func TestProcessAlive(t *testing.T) {
	if !ProcessAlive(os.Getpid()) {
		t.Fatal("本进程必然存活")
	}
	if ProcessAlive(0) || ProcessAlive(-1) {
		t.Fatal("0/负数不是合法 pid，必须判死（否则陈旧锁永不回收）")
	}
	// 一个几乎不可能存在的 pid：Windows 上限远小于它。
	if ProcessAlive(1 << 30) {
		t.Fatal("不存在的 pid 必须判死")
	}
}

func TestSettingsDefaultsAndOverlayChain(t *testing.T) {
	defaults := DefaultSettings()
	if err := ValidateSettings(defaults); err != nil {
		t.Fatalf("出厂默认必须自洽：%v", err)
	}
	if err := ValidateSettings(Settings{}); err == nil {
		t.Fatal("零值 Settings 不是合法配置（零值 = 未配置，要先补默认）")
	}

	// 覆盖链：零值/nil = 未覆盖；非零值覆盖；显式 false 用指针表达。
	overridden := MergeSettings(defaults, Settings{
		MessageShardRows: defaults.MessageShardRows + 7,
		RetentionMode:    "", // 空 = 不覆盖
	})
	if overridden.MessageShardRows != defaults.MessageShardRows+7 {
		t.Fatalf("非零值必须覆盖：got %d, want %d", overridden.MessageShardRows, defaults.MessageShardRows+7)
	}
	if overridden.RetentionMode != defaults.RetentionMode {
		t.Fatalf("空串不是覆盖（got %q）", overridden.RetentionMode)
	}
	if overridden.ShardRows() != overridden.MessageShardRows {
		t.Fatalf("ShardRows 必须等于 MessageShardRows：%d vs %d", overridden.ShardRows(), overridden.MessageShardRows)
	}

	keepFalse := false
	merged := MergeSettings(defaults, Settings{AutoRecover: &keepFalse})
	if merged.AutoRecover == nil || *merged.AutoRecover {
		t.Fatalf("指针项必须能表达显式 false：%v", merged.AutoRecover)
	}

	// ResolveSettings：默认 ← 各层（后者胜），层里的零值不覆盖。
	resolved := ResolveSettings(Settings{MessageShardRows: 11}, Settings{MessageShardRows: 0, WireRecentErrors: 3})
	if resolved.MessageShardRows != 11 {
		t.Fatalf("零值层不得覆盖前一层：%d", resolved.MessageShardRows)
	}
	if resolved.WireRecentErrors != 3 {
		t.Fatalf("非零层必须生效：%d", resolved.WireRecentErrors)
	}

	// 校验面：越界必须显式拒绝，不静默兜底。
	bad := defaults
	bad.MessageShardRows = 0
	if err := ValidateSettings(bad); err == nil || !strings.Contains(err.Error(), "shard_rows") {
		t.Fatalf("shard_rows 必须被拒且文案点名：%v", err)
	}
	bad = defaults
	bad.WireRecentErrors = 0
	if err := ValidateSettings(bad); err == nil {
		t.Fatal("wire_recent_errors = 0 必须被拒")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(data)
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
