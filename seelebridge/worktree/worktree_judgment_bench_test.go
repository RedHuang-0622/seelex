package worktree

// worktree_judgment_bench_test.go — ③ A/B/D 收口后的**量级读数**：现场清理 / 脏判定 / 在册比较
// 各自一份实现，它们本身的代价应当落在**亚微秒到个位数微秒**这一档——真正的开销在那些
// `git` 子进程上（本文件用 fake runner，因此量到的是**判据自己**的成本，不含进程启动）。
//
// 用法：`go test ./seelebridge/worktree/ -run '^$' -bench Judgment -benchmem -count=1`

import (
	"strings"
	"testing"
)

func BenchmarkJudgmentPathDirtyWith(b *testing.B) {
	fake := newFakeGit()
	fake.reply["status --porcelain"] = " M a.txt\n?? b.txt\n"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		dirty, err := pathDirtyWith(fake.run, "scene")
		if err != nil || !dirty {
			b.Fatalf("pathDirtyWith = %v err=%v", dirty, err)
		}
	}
}

func BenchmarkJudgmentWorktreePathEqual(b *testing.B) {
	left := `C:\Program\go\seelex\seelex-exec-wi-1`
	right := `c:/program/go/seelex/seelex-exec-wi-1/`
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !worktreePathEqual(left, right) {
			b.Fatal("同一路径的不同写法必须判等")
		}
	}
}

func BenchmarkJudgmentParseWorktreeList(b *testing.B) {
	var builder strings.Builder
	for i := 0; i < 16; i++ {
		builder.WriteString("worktree C:/Program/go/seelex-seelex-exec-wi-")
		builder.WriteString(string(rune('a' + i)))
		builder.WriteString("\nHEAD 0123456789abcdef\nbranch refs/heads/seelex/exec-wi-")
		builder.WriteString(string(rune('a' + i)))
		builder.WriteString("\n\n")
	}
	out := builder.String()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if entries := parseWorktreeList(out); len(entries) != 16 {
			b.Fatalf("porcelain 解开 %d 条，想要 16 条", len(entries))
		}
	}
}

func BenchmarkJudgmentCleanupWorktreeWith(b *testing.B) {
	fake := newFakeGit()
	fake.reply["worktree list --porcelain"] = "" // 目录与登记都没了 = 已经释放过（幂等路径）
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if err := cleanupWorktreeWith(fake.run, b.TempDir(), &NodeWorktree{
			Path:   b.TempDir(),
			Branch: "seelex/exec-wi-1",
		}); err != nil {
			b.Fatalf("已释放的现场必须幂等成功：%v", err)
		}
	}
}
