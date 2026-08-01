package sessionstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProjectRecordRoundTripAcrossLocalBackends(t *testing.T) {
	for _, config := range []Config{
		{Backend: BackendJSON, Path: filepath.Join(t.TempDir(), "json")},
		{Backend: BackendSQLite, Path: filepath.Join(t.TempDir(), "sessions.db")},
	} {
		t.Run(string(config.Backend), func(t *testing.T) {
			repository, err := Open(context.Background(), config)
			if err != nil {
				t.Fatal(err)
			}
			defer repository.Close()
			want := ProjectRecord{
				Version: "abc",
				Modules: []ModuleSemantics{
					{Name: "gui", Summary: "GUI 前端职责", Path: "gui", Docs: []string{"gui/README.md"}},
					{Name: "core", Summary: "core 职责", Path: "application/core"},
				},
				SourceHashes: []string{"h1", "h2"},
				BuiltAt:      time.Unix(100, 0).UTC(),
			}
			if err := repository.WriteProjectRecord(context.Background(), "project", want); err != nil {
				t.Fatal(err)
			}
			got, err := repository.ReadProjectRecord(context.Background(), "project")
			if err != nil {
				t.Fatal(err)
			}
			wantJSON, _ := json.Marshal(want)
			gotJSON, _ := json.Marshal(got)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("record = %s, want %s", gotJSON, wantJSON)
			}
			// 项目级记录按 projectID 隔离：其它项目读不到。
			if _, err := repository.ReadProjectRecord(context.Background(), "other"); err == nil || !isProjectRecordNotFound(err) {
				t.Fatalf("missing record error = %v, want not found", err)
			}
		})
	}
}

func TestProjectRecordEmptyProjectIDRejected(t *testing.T) {
	for _, config := range []Config{
		{Backend: BackendJSON, Path: filepath.Join(t.TempDir(), "json")},
		{Backend: BackendSQLite, Path: filepath.Join(t.TempDir(), "sessions.db")},
	} {
		repository, err := Open(context.Background(), config)
		if err != nil {
			t.Fatal(err)
		}
		if err := repository.WriteProjectRecord(context.Background(), "  ", ProjectRecord{}); err == nil {
			t.Fatalf("%s: empty project ID succeeded", config.Backend)
		}
		if _, err := repository.ReadProjectRecord(context.Background(), ""); err == nil {
			t.Fatalf("%s: empty project ID read succeeded", config.Backend)
		}
		repository.Close()
	}
}

func TestRedisProjectRecordKeySharesProjectHashTag(t *testing.T) {
	repository := &redisRepository{namespace: "seelex"}
	first := repository.projectRecordKey("project-a")
	if tag := repository.projectKey("project-a"); !containsHashTag(first, tag) {
		t.Fatalf("project record key %q does not share project hash tag %q", first, tag)
	}
	if !strings.HasSuffix(first, ":project-record") {
		t.Fatalf("unexpected project record key shape %q", first)
	}
	// 不同项目 → 不同 key（按项目隔离）。
	if second := repository.projectRecordKey("project-b"); second == first {
		t.Fatalf("project record keys must be project-scoped: %q", first)
	}
}

// newKnowledgeProject 构造含三路来源的临时项目：
// docs/modules/<name>/README.md、gui/module_dotting.json、seelex.project.md。
func newKnowledgeProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	docs := filepath.Join(root, "docs", "modules")
	if err := os.MkdirAll(filepath.Join(docs, "gui"), 0o700); err != nil {
		t.Fatal(err)
	}
	readme := "# GUI 前端\n\nGUI 职责说明\n"
	if err := os.WriteFile(filepath.Join(docs, "gui", "README.md"), []byte(readme), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "flat.md"), []byte("Flat 模块\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	metadata := `{
  "schema_version": 1,
  "system": "test",
  "modules": [
    {
      "id": "core",
      "name": "Core engine",
      "responsibility": "拥有单会话状态机并暴露快照",
      "document": "modules/core.md",
      "interfaces": ["application.Service"],
      "planned_paths": ["application/core/chat.go"]
    }
  ]
}`
	if err := os.MkdirAll(filepath.Join(root, "gui"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "gui", "module_dotting.json"), []byte(metadata), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "seelex.project.md"), []byte("项目总体说明\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestProjectKnowledgeBuilderBuildsModules(t *testing.T) {
	root := newKnowledgeProject(t)
	builder := NewProjectKnowledgeBuilder(ProjectKnowledgeSources{Root: root})

	record, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	// 元数据 core + 文档目录 gui/flat + 手工 project = 4 个模块。
	if len(record.Modules) != 4 {
		t.Fatalf("modules = %d, want 4: %+v", len(record.Modules), record.Modules)
	}
	byName := map[string]ModuleSemantics{}
	for _, module := range record.Modules {
		byName[module.Name] = module
	}
	core := byName["core"]
	if core.Summary != "拥有单会话状态机并暴露快照" || core.Path != "application/core/chat.go" {
		t.Fatalf("core module = %+v", core)
	}
	if len(core.Docs) != 2 || core.Docs[0] != "modules/core.md" {
		t.Fatalf("core docs = %+v", core.Docs)
	}
	if gui := byName["gui"]; gui.Summary != "GUI 前端" {
		t.Fatalf("gui module summary = %q", gui.Summary)
	}
	if flat := byName["flat"]; flat.Summary != "Flat 模块" {
		t.Fatalf("flat module summary = %q", flat.Summary)
	}
	if project := byName["project"]; !strings.Contains(project.Summary, "项目总体说明") {
		t.Fatalf("project module summary = %q", project.Summary)
	}
	if record.Version == "" {
		t.Fatal("version must be derived from source hashes")
	}
	if len(record.SourceHashes) == 0 {
		t.Fatal("source hashes must be recorded")
	}
}

func TestProjectKnowledgeBuilderSameSourcesDetectsChanges(t *testing.T) {
	root := newKnowledgeProject(t)
	builder := NewProjectKnowledgeBuilder(ProjectKnowledgeSources{Root: root})
	record, err := builder.Build()
	if err != nil {
		t.Fatal(err)
	}
	if !builder.SameSources(record) {
		t.Fatal("sources unchanged must be reused")
	}
	// 修改任一来源 → hash 变化 → 不可复用。
	if err := os.WriteFile(filepath.Join(root, "seelex.project.md"), []byte("修改后的说明\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if builder.SameSources(record) {
		t.Fatal("changed sources must not be reused")
	}
}

func TestProjectKnowledgeBuilderFailsWithoutSources(t *testing.T) {
	builder := NewProjectKnowledgeBuilder(ProjectKnowledgeSources{Root: t.TempDir()})
	if _, err := builder.SourceHashes(); err == nil {
		t.Fatal("no sources must fail explicitly (first build)")
	}
	if _, err := builder.Build(); err == nil {
		t.Fatal("build without sources must fail explicitly")
	}
}

func TestRefreshProjectKnowledgeReusesWhenUnchanged(t *testing.T) {
	router := newTestRouter(t)
	root := newKnowledgeProject(t)
	builder := NewProjectKnowledgeBuilder(ProjectKnowledgeSources{Root: root})

	first, err := RefreshProjectKnowledge(context.Background(), router, builder, false)
	if err != nil {
		t.Fatal(err)
	}
	if first.Reused || first.Fallback || len(first.Record.Modules) != 4 {
		t.Fatalf("first refresh = %+v", first)
	}

	// 来源未变 → 直接复用，不重建（Version/BuiltAt 不变）。
	second, err := RefreshProjectKnowledge(context.Background(), router, builder, false)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Reused || second.Fallback {
		t.Fatalf("second refresh = %+v", second)
	}
	if second.Record.Version != first.Record.Version || !second.Record.BuiltAt.Equal(first.Record.BuiltAt) {
		t.Fatalf("reused record changed: first=%+v second=%+v", first.Record, second.Record)
	}

	// 强制重建 → 落盘新记录（BuiltAt 前进）。
	forced, err := RefreshProjectKnowledge(context.Background(), router, builder, true)
	if err != nil {
		t.Fatal(err)
	}
	if forced.Reused || forced.Fallback {
		t.Fatalf("forced refresh = %+v", forced)
	}

	// 来源变化 → 重建。
	if err := os.WriteFile(filepath.Join(root, "gui", "module_dotting.json"), []byte(`{"modules":[{"id":"core","responsibility":"新职责"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := RefreshProjectKnowledge(context.Background(), router, builder, false)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Reused {
		t.Fatal("changed sources must rebuild")
	}
}

func TestRefreshProjectKnowledgeFallsBackOnRebuildFailure(t *testing.T) {
	router := newTestRouter(t)
	root := newKnowledgeProject(t)
	builder := NewProjectKnowledgeBuilder(ProjectKnowledgeSources{Root: root})
	first, err := RefreshProjectKnowledge(context.Background(), router, builder, false)
	if err != nil {
		t.Fatal(err)
	}
	// 元数据损坏 → 重建失败 → 保留上一版本（可回退）。
	broken := filepath.Join(root, "gui", "module_dotting.json")
	if err := os.WriteFile(broken, []byte(`not-json`), 0o600); err != nil {
		t.Fatal(err)
	}
	fallback, err := RefreshProjectKnowledge(context.Background(), router, builder, false)
	if err != nil {
		t.Fatal(err)
	}
	if !fallback.Fallback {
		t.Fatalf("rebuild failure must fall back, got %+v", fallback)
	}
	if fallback.Record.Version != first.Record.Version {
		t.Fatalf("fallback record version = %s, want %s", fallback.Record.Version, first.Record.Version)
	}
	// 首建失败 → 显式错误。
	emptyBuilder := NewProjectKnowledgeBuilder(ProjectKnowledgeSources{Root: t.TempDir()})
	if _, err := RefreshProjectKnowledge(context.Background(), router, emptyBuilder, false); err == nil {
		t.Fatal("first build failure must fail explicitly")
	}
}

func TestIsProjectRecordNotFound(t *testing.T) {
	if isProjectRecordNotFound(nil) {
		t.Fatal("nil is not not-found")
	}
	if !isProjectRecordNotFound(fs.ErrNotExist) {
		t.Fatal("fs.ErrNotExist must be not-found")
	}
	if !isProjectRecordNotFound(sql.ErrNoRows) {
		t.Fatal("sql.ErrNoRows must be not-found")
	}
	if isProjectRecordNotFound(errors.New("boom")) {
		t.Fatal("other errors are not not-found")
	}
}
