package bootseed

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// configDefaultSources 是"内嵌默认档 ← 仓库规范档"的对照表。
//
// RepoFile 指**仓库里那份唯一事实源**，资产名一律取 pack.Name。两者不总是同名：
// 含凭据/密钥的档（mcp.yaml、search_engine.yaml 的 env 与 api_key）进 .gitignore，
// 版本库里留的是同目录的 `*.example.yaml`，所以内嵌默认档的字节来源是 example。
// 这条规则与 accounts.yaml / accounts.example.yaml 是同一套纪律。
var configDefaultSources = []struct {
	Pack     Pack
	RepoFile string
}{
	{RuntimeConfigPack(), "config/seelex.yaml"},
	{PermissionConfigPack(), "config/seele.yaml"},
	{MCPConfigPack(), "config/mcp.example.yaml"},
	{SearchEngineConfigPack(), "config/search_engine.example.yaml"},
}

// TestEmbeddedConfigDefaultsMatchRepository 是这次的"防漂移钉子"：内嵌默认档必须
// 与仓库 `config/` 下的规范档**逐字节相同**。规范档是唯一事实源，内嵌那份只是
// "缺失时就地初始化"的副本；两边一旦分叉，初始化出来的配置就和仓库里那份不是
// 一回事（2026-09-29 压缩厚摘要开关就是被"包内旧档"吞掉的）。
//
// 红了就同步，不要改默认数据本身：
//
//	scripts/sync-bootseed-defaults.ps1
func TestEmbeddedConfigDefaultsMatchRepository(t *testing.T) {
	for _, entry := range configDefaultSources {
		pack := entry.Pack
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
			repo, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(entry.RepoFile)))
			if err != nil {
				t.Fatalf("读仓库规范档 %s: %v", entry.RepoFile, err)
			}
			if string(embedded) != string(repo) {
				t.Fatalf("内嵌默认档 %s 与仓库规范档 %s 不一致（跑 scripts/sync-bootseed-defaults.ps1 同步）",
					file.Source, entry.RepoFile)
			}
		}
	}
}

// TestSearchEngineSeedCarriesNoCredential 钉住一条安全性质：search_engine.yaml
// **参与自愈**（三处全缺就落盘），所以种子档里绝不能有凭据——否则一台新机器
// 凭空就会"看起来已配好搜索引擎"，然后用一个占位 key 去打真实端点。
// 空 provider 才是诚实的"未配置"。
func TestSearchEngineSeedCarriesNoCredential(t *testing.T) {
	data, err := Decode(Assets(), SearchEngineConfigPack().Files[0])
	if err != nil {
		t.Fatalf("读内嵌 search_engine.yaml: %v", err)
	}
	text := string(data)
	// 所有会带上密钥的行都必须是注释（# 开头）；生效行里不许出现 api_key。
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "api_key:") || strings.HasPrefix(trimmed, "provider:") {
			t.Fatalf("search_engine.yaml 种子档不允许生效的 %q（会把占位 key 当成真配置）", trimmed)
		}
	}
}

// TestEmbeddedPacksAreAllReadable：每个默认数据包里的每一份都要能读出来、且非空。
// 它同时钉住 `//go:embed all:assets` 的 `all:` 前缀（少了它会静默丢掉点开头的文件）。
func TestEmbeddedPacksAreAllReadable(t *testing.T) {
	packs := []Pack{RuntimeConfigPack(), PermissionConfigPack(), MCPConfigPack(), SearchEngineConfigPack(), AutoGetJobsPack()}
	for _, pack := range packs {
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
