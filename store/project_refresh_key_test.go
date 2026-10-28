package store

import (
	"context"
	storeproject "github.com/RedHuang-0622/seelex/store/project"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeModuleDotting 在 root 下按相对路径写出模块元数据（docs/gui/module_dotting.json
// 的真实形状，只保留本用例关心的字段）。
func writeModuleDotting(t *testing.T, root string, relative ...string) {
	t.Helper()
	path := filepath.Join(append([]string{root}, relative...)...)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	metadata := `{"schema_version":1,"system":"test","modules":[{"id":"core","responsibility":"拥有单会话状态机"}]}`
	if err := os.WriteFile(path, []byte(metadata), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestProjectRefreshWriteReadSameKey 钉住「写入键 = 读取键 = 调用方给的工作区 ID」。
//
// 断点②现象（修复前）：storeproject.RefreshProjectKnowledge 用 builder.Root()（绝对路径）当 projectID
// 读写，而项目级读面（wiring/bootstrap 的 RuntimeDeps.ProjectKnowledge）用
// store.Workspace()（ws-<nano>）读 ⇒ 写进去的永远读不到，全仓 **/project-record.json 零命中。
func TestProjectRefreshWriteReadSameKey(t *testing.T) {
	router := newTestRouter(t)
	// 形状与 workspace.New 生成的 ID 一致（workspace.go: `ws-%d` UnixNano）。
	const workspaceID = "ws-1700000000000000000"
	router.SetWorkspace(workspaceID)

	root := newKnowledgeProject(t)
	builder := storeproject.NewProjectKnowledgeBuilder(storeproject.ProjectKnowledgeSources{Root: root})

	refreshed, err := storeproject.RefreshProjectKnowledge(context.Background(), router, workspaceID, builder, false)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if len(refreshed.Record.Modules) == 0 {
		t.Fatal("refresh built no modules")
	}

	// 空键必须显式报错（否则又会在一个谁也读不到的键下落盘）。
	if _, err := storeproject.RefreshProjectKnowledge(context.Background(), router, "  ", builder, false); err == nil {
		t.Fatal("空 projectID 必须显式报错")
	}

	// 读面：与 wiring/bootstrap 的 RuntimeDeps.ProjectKnowledge 同一个键（store.Workspace()）。
	got, readErr := router.LoadProjectRecord(router.Workspace())
	if readErr != nil {
		t.Fatalf("refresh 写下的记录按工作区键读不到（写键≠读键）：%v", readErr)
	}
	if got.Version != refreshed.Record.Version {
		t.Fatalf("version = %q, want %q", got.Version, refreshed.Record.Version)
	}

	// 扫描根（绝对路径）不得再被当成项目键：否则盘上又是两份键。
	if _, err := router.LoadProjectRecord(builder.Root()); err == nil {
		t.Fatalf("记录在扫描根键 %q 下也可读到：写入键不是工作区 ID", builder.Root())
	}
}

// TestProjectRefreshSourcesConfigurable 钉住来源口径：默认 MetadataPath 对齐真实路径
// <root>/docs/gui/module_dotting.json（模块登记表在 docs/gui，见 docs/README.md）；
// storeproject.ProjectKnowledgeSources 三字段仍可覆盖；无来源时给出明确错误而不是静默空记录。
func TestProjectRefreshSourcesConfigurable(t *testing.T) {
	root := t.TempDir()
	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	builder := storeproject.NewProjectKnowledgeBuilder(storeproject.ProjectKnowledgeSources{Root: root})
	if got, want := builder.MetadataPath(), filepath.Join(absRoot, "docs", "gui", "module_dotting.json"); got != want {
		t.Fatalf("默认 MetadataPath = %q, want %q", got, want)
	}
	if got, want := builder.ModulesDir(), filepath.Join(absRoot, "docs", "modules"); got != want {
		t.Fatalf("默认 ModulesDir = %q, want %q", got, want)
	}
	if got, want := builder.ManifestPath(), filepath.Join(absRoot, "seelex.project.md"); got != want {
		t.Fatalf("默认 ManifestPath = %q, want %q", got, want)
	}

	// 只有默认元数据来源（docs/gui/module_dotting.json）的项目也要能建出记录。
	metadataOnly := t.TempDir()
	writeModuleDotting(t, metadataOnly, "docs", "gui", "module_dotting.json")
	metadataBuilder := storeproject.NewProjectKnowledgeBuilder(storeproject.ProjectKnowledgeSources{Root: metadataOnly})
	record, err := metadataBuilder.Build()
	if err != nil {
		t.Fatalf("默认元数据来源未生效：%v", err)
	}
	if len(record.Modules) != 1 || record.Modules[0].Name != "core" {
		t.Fatalf("modules = %+v, want 单条 core", record.Modules)
	}

	// 三字段仍可覆盖：显式来源指向非约定路径同样生效。
	overrideRoot := t.TempDir()
	writeModuleDotting(t, overrideRoot, "meta", "registry.json")
	override := storeproject.NewProjectKnowledgeBuilder(storeproject.ProjectKnowledgeSources{
		Root:         overrideRoot,
		MetadataPath: filepath.Join(overrideRoot, "meta", "registry.json"),
	})
	if _, err := override.Build(); err != nil {
		t.Fatalf("显式 MetadataPath 覆盖未生效：%v", err)
	}

	// 无来源 → 明确错误（不是「空记录」）。
	empty := storeproject.NewProjectKnowledgeBuilder(storeproject.ProjectKnowledgeSources{Root: t.TempDir()})
	emptyRecord, err := empty.Build()
	if err == nil {
		t.Fatalf("无来源必须显式报错，实际得到记录 %+v", emptyRecord)
	}
	if !strings.Contains(err.Error(), "no sources") {
		t.Fatalf("无来源错误应点明来源缺失，实际：%v", err)
	}
}

// TestProjectRefreshRealRepositorySources 把默认来源钉在**本仓库真实布局**上：
// 模块登记表在 docs/gui/module_dotting.json（见 docs/README.md 索引）。断点①
// 的表现就是三路默认来源在本仓库全落空（`no sources`），这条用例用真实仓库根复现/防回归。
func TestProjectRefreshRealRepositorySources(t *testing.T) {
	repositoryRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	builder := storeproject.NewProjectKnowledgeBuilder(storeproject.ProjectKnowledgeSources{Root: repositoryRoot})
	if !storeproject.FileExists(builder.MetadataPath()) {
		t.Fatalf("默认 MetadataPath %q 未命中本仓库的模块登记表", builder.MetadataPath())
	}
	record, err := builder.Build()
	if err != nil {
		t.Fatalf("按本仓库真实来源构建失败：%v", err)
	}
	if len(record.Modules) == 0 {
		t.Fatal("按本仓库真实来源构建出 0 个模块")
	}
}
