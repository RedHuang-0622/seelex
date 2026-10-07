package main

// boot_prompt_test.go — 提示词资产目录的责任链钉子（口径同 boot_seed_test.go，
// 只是载荷从"一个配置文件"变成"一个目录"）。
//
// 钉住三件事：
//
//	1. 候选链形状：CWD 的 config/prompt/ 优先，<exe>/config/prompt/ 兜底，
//	   落盘根固定取后者；没有 exe 路径时无处落盘（返回 ""，回退内嵌词）。
//	2. 缺失即初始化：候选链全缺时用内嵌默认词落盘，入口内容与内嵌逐字相同，
//	   且整份清单都落全（少列一份 = 那份永远无法被外部覆盖）。
//	3. 存在即读：入口一旦存在就命中，之后一个字节都不写（用户改过的不被盖回去）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/internal/promptassets"
)

func TestPromptDirChainShape(t *testing.T) {
	entry := filepath.FromSlash(promptassets.Entry)

	candidates, seedRoot := promptDirChainAt("")
	if len(candidates) != 1 || candidates[0] != filepath.Join("config", "prompt", entry) {
		t.Fatalf("无 exe 路径时链上只该有 CWD 候选，得到 %v", candidates)
	}
	if seedRoot != "" {
		t.Fatalf("无 exe 路径时不该有落盘根，得到 %q", seedRoot)
	}

	exePath := filepath.Join(string(filepath.Separator)+"opt", "seelex", "seelex.exe")
	candidates, seedRoot = promptDirChainAt(exePath)
	if len(candidates) != 2 {
		t.Fatalf("链上该有 CWD 与包内两份，得到 %v", candidates)
	}
	wantSecond := filepath.Join(string(filepath.Separator)+"opt", "seelex", "config", "prompt", entry)
	if candidates[1] != wantSecond {
		t.Fatalf("第二候选 = %q，want %q", candidates[1], wantSecond)
	}
	wantSeed := filepath.Join(string(filepath.Separator)+"opt", "seelex", "config", "prompt")
	if seedRoot != wantSeed {
		t.Fatalf("落盘根 = %q，want %q", seedRoot, wantSeed)
	}
}

func TestResolvePromptDirSeedsDefaultsThenReadsThem(t *testing.T) {
	entry := filepath.FromSlash(promptassets.Entry)
	missing := filepath.Join(t.TempDir(), "config", "prompt", entry) // 链上全缺
	seedRoot := filepath.Join(t.TempDir(), "exe", "config", "prompt")

	got := resolvePromptDirAt([]string{missing}, seedRoot)
	if got != seedRoot {
		t.Fatalf("候选链全缺时应初始化到落盘根：期望 %s，得到 %q", seedRoot, got)
	}

	// 整份清单都要落全——少列一份，那一份就永远无法被外部覆盖。
	for _, rel := range promptassets.DefaultFiles() {
		data, err := os.ReadFile(filepath.Join(seedRoot, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("缺失即初始化：应落盘 %s（%v）", rel, err)
		}
		if len(data) == 0 {
			t.Fatalf("默认词 %s 不该是空文件（空文件会被当成未配置而回退内嵌）", rel)
		}
	}

	// 入口内容必须与内嵌默认逐字相同（落盘的是同一份字节，不是再写一遍的词）。
	seeded, err := os.ReadFile(filepath.Join(seedRoot, filepath.FromSlash(promptassets.Entry)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(seeded)) != promptassets.SystemInstructions() {
		t.Fatal("落盘的入口必须与内嵌 system/instructions.md 逐字相同")
	}

	// 存在即读：把候选指到已落盘的入口（生产里 seedRoot 与第二候选同址，这里
	// 显式模拟）——命中后一个字节都不写：用户改过的不被盖回去，缺的兄弟文件
	// 也不补齐（补齐会掩盖"只覆盖一份也能用"的语义）。
	hit := filepath.Join(seedRoot, filepath.FromSlash(promptassets.Entry))
	if err := os.WriteFile(hit, []byte("MINE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(seedRoot, "effort", "system.md")
	if err := os.Remove(sibling); err != nil {
		t.Fatal(err)
	}
	if again := resolvePromptDirAt([]string{missing, hit}, seedRoot); again != seedRoot {
		t.Fatalf("存在即读：路径要稳定，期望 %s，得到 %q", seedRoot, again)
	}
	kept, err := os.ReadFile(hit)
	if err != nil || string(kept) != "MINE\n" {
		t.Fatalf("存在即读：不得改写用户那份（得到 %q, err=%v）", kept, err)
	}
	if _, err := os.Stat(sibling); err == nil {
		t.Fatal("命中候选时不该再落盘：缺失的兄弟文件应留缺（交给 promptassets 逐文件回退）")
	}
}

func TestResolvePromptDirPrefersCWDCopy(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	// CWD 相对候选：显式给一份存在的入口文件。
	mine := filepath.Join("config", "prompt", filepath.FromSlash(promptassets.Entry))
	if err := os.MkdirAll(filepath.Dir(mine), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mine, []byte("CWD MINE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedRoot := filepath.Join(t.TempDir(), "config", "prompt")

	got := resolvePromptDirAt([]string{mine}, seedRoot)
	want := filepath.Join(dir, "config", "prompt")
	if got != want {
		t.Fatalf("候选链第一个存在的胜出（CWD 那份的提示词根）：期望 %s，得到 %q", want, got)
	}
	if _, err := os.Stat(seedRoot); err == nil {
		t.Fatal("命中候选时不该在落盘根造出目录来")
	}
}

func TestResolvePromptDirMissingWithoutSeedRoot(t *testing.T) {
	if got := resolvePromptDirAt([]string{filepath.Join(t.TempDir(), "nope.md")}, ""); got != "" {
		t.Fatalf("无处落盘应返回空目录（调用方回退内嵌词），得到 %q", got)
	}
}
