package bootseed

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEmbeddedConfigDefaultsMatchRepository 是这次的"防漂移钉子"：内嵌默认档必须
// 与仓库 `config/` 下的规范档**逐字节相同**。规范档是唯一事实源，内嵌那份只是
// "缺失时就地初始化"的副本；两边一旦分叉，初始化出来的配置就和仓库里那份不是
// 一回事（2026-09-29 折叠厚摘要开关就是被"包内旧档"吞掉的）。
//
// 红了就同步，不要改默认数据本身：
//
//	scripts/sync-bootseed-defaults.ps1
func TestEmbeddedConfigDefaultsMatchRepository(t *testing.T) {
	for _, pack := range []Pack{RuntimeConfigPack(), PermissionConfigPack()} {
		if len(pack.Files) != 1 {
			t.Fatalf("配置默认数据包 %s 应恰好一份文件，实际 %d", pack.Name, len(pack.Files))
		}
		for _, file := range pack.Files {
			if file.Rel != pack.Name {
				t.Fatalf("入口名要与文件名一致：Rel=%q Name=%q", file.Rel, pack.Name)
			}
			embedded, err := Decode(Assets(), file)
			if err != nil {
				t.Fatalf("读内嵌默认数据 %s: %v", file.Source, err)
			}
			repo, err := os.ReadFile(filepath.Join("..", "..", "config", filepath.FromSlash(file.Rel)))
			if err != nil {
				t.Fatalf("读仓库规范档 config/%s: %v", file.Rel, err)
			}
			if string(embedded) != string(repo) {
				t.Fatalf("内嵌默认档与仓库规范档 config/%s 不一致（跑 scripts/sync-bootseed-defaults.ps1 同步）", file.Rel)
			}
		}
	}
}

// TestEmbeddedPacksAreAllReadable：每个默认数据包里的每一份都要能读出来、且非空。
// 它同时钉住 `//go:embed all:assets` 的 `all:` 前缀（少了它会静默丢掉点开头的文件）。
func TestEmbeddedPacksAreAllReadable(t *testing.T) {
	for _, pack := range []Pack{RuntimeConfigPack(), PermissionConfigPack(), AutoGetJobsPack()} {
		if len(pack.Files) == 0 {
			t.Fatalf("默认数据包 %s 是空的", pack.Name)
		}
		for _, file := range pack.Files {
			data, err := Decode(Assets(), file)
			if err != nil {
				t.Fatalf("默认数据包 %s 的 %s: %v", pack.Name, file.Source, err)
			}
			if strings.TrimSpace(string(data)) == "" {
				t.Fatalf("默认数据包 %s 的 %s 是空文件", pack.Name, file.Source)
			}
		}
	}
}

func TestAutoGetJobsSkeletonShipsDotEnvExample(t *testing.T) {
	pack := AutoGetJobsPack()
	var dotenv *File
	for i := range pack.Files {
		if pack.Files[i].Rel == ".env.example" {
			dotenv = &pack.Files[i]
		}
	}
	if dotenv == nil {
		t.Fatal("骨架要带 .env.example：环境变量模板是这套脚本里唯一说得清「要配什么」的东西")
	}
	data, err := Decode(Assets(), *dotenv)
	if err != nil {
		t.Fatalf("点开头的默认数据必须也能内嵌（go:embed 的 all: 前缀）: %v", err)
	}
	for _, key := range []string{"API_KEY", "API_URL", "MODEL"} {
		if !strings.Contains(string(data), key) {
			t.Fatalf(".env.example 应列出脚本读的环境变量 %s", key)
		}
	}
}
