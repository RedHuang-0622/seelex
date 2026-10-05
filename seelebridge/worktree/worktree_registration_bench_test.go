package worktree

// worktree_registration_bench_test.go — U2 落地后的**量级读数**：登记来源那一条判据
// （`Restore` 的"已在册不覆盖"）本身的代价。
//
// 用法：`go test ./seelebridge/worktree/ -run '^$' -bench SceneRegistration -benchmem -count=1`
//
// 两个子基准分别对应两条路：① 已在册（首判跳过：一次 map 读/记录）；② 空注册表（重建：
// 一次 map 写/记录）。判据自己应当落在**亚微秒到个位数微秒**这一档——真正的开销在
// `os.Stat` 与 git 子进程上，不含在本基准里。

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// sceneRecordsForBench 造 32 条"记录投影来源"的记录，每条都有真实存在的现场目录
// （`Restore` 判"目录真的在"）。
func sceneRecordsForBench(b *testing.B, dir string) []sessionstore.NodeSessionRecord {
	b.Helper()
	records := make([]sessionstore.NodeSessionRecord, 0, 32)
	for index := 0; index < 32; index++ {
		nodeID := fmt.Sprintf("exec-wi-%02d", index)
		path := filepath.Join(dir, nodeID)
		if err := os.Mkdir(path, 0o755); err != nil {
			b.Fatal(err)
		}
		records = append(records, sessionstore.NodeSessionRecord{
			SchemaVersion: sessionstore.NodeSessionSchemaVersion,
			NodeID:        nodeID,
			Worktree: sessionstore.NodeWorktreeRecord{
				Path: path, Branch: "seelex/" + nodeID, MainBranch: "main", BaseCommit: "0123456789",
			},
		})
	}
	return records
}

func BenchmarkSceneRegistrationRestore(b *testing.B) {
	dir := b.TempDir()
	records := sceneRecordsForBench(b, dir)

	b.Run("已在册（首判跳过）", func(b *testing.B) {
		manager := NewWorktreeManager(WorktreeManagerDeps{Root: func() string { return dir }})
		manager.Restore(records)
		b.ReportAllocs()
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			manager.Restore(records)
		}
	})

	b.Run("空注册表（重建）", func(b *testing.B) {
		manager := NewWorktreeManager(WorktreeManagerDeps{Root: func() string { return dir }})
		b.ReportAllocs()
		b.ResetTimer()
		for index := 0; index < b.N; index++ {
			manager.mu.Lock()
			manager.worktrees = map[string]*NodeWorktree{}
			manager.mu.Unlock()
			manager.Restore(records)
		}
	})
}
