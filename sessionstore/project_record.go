package sessionstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ProjectRecord is the project-level module semantics record (plan.md §3.7.1).
// It is shared across sessions and read-only for sessions: only the
// project_refresh tool writes it. Content is hash-versioned; when
// SourceHashes are unchanged the record is reused without rebuilding.
type ProjectRecord struct {
	Version      string            `json:"version"` // content hash derived from SourceHashes
	Modules      []ModuleSemantics `json:"modules"`
	SourceHashes []string          `json:"source_hashes"` // per-source hashes for incremental rebuild detection
	BuiltAt      time.Time         `json:"built_at"`
}

// ModuleSemantics describes one module's responsibility, boundary, and doc
// index. It is the model-facing unit of project knowledge.
type ModuleSemantics struct {
	Name    string   `json:"name"`
	Summary string   `json:"summary"`        // 语义说明（职责/边界）
	Path    string   `json:"path"`           // module path
	Docs    []string `json:"docs,omitempty"` // doc/interface index
}

// isProjectRecordNotFound reports whether the record has never been built
// (JSON missing manifest file / SQL no row / Redis nil converted to fs.ErrNotExist).
func isProjectRecordNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	return errors.Is(err, sql.ErrNoRows)
}

// ── 来源与构建（project_refresh 工具核心，plan.md §3.7.1）──────────────────

// ProjectKnowledgeSources 定义 project_refresh 的可插拔来源。
// 空字段使用约定默认值（相对 Root）。
type ProjectKnowledgeSources struct {
	Root         string // 项目根目录
	ModulesDir   string // 模块文档目录（默认 <root>/docs/modules）
	MetadataPath string // 模块元数据 JSON（默认 <root>/gui/module_dotting.json）
	ManifestPath string // 手写项目说明 seelex.project.md（默认 <root>/seelex.project.md）
}

// ProjectKnowledgeBuilder scans the sources and builds ProjectKnowledge.
// SourceHashes is deterministic (sorted doc files → metadata → manifest), so
// SameSources can decide reuse without rebuilding.
type ProjectKnowledgeBuilder struct {
	root         string
	modulesDir   string
	metadataPath string
	manifestPath string
}

// NewProjectKnowledgeBuilder creates a builder with normalized absolute paths.
func NewProjectKnowledgeBuilder(sources ProjectKnowledgeSources) *ProjectKnowledgeBuilder {
	root := sources.Root
	if root == "" {
		root = "."
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return &ProjectKnowledgeBuilder{
		root:         root,
		modulesDir:   resolveSourcePath(root, sources.ModulesDir, filepath.Join(root, "docs", "modules")),
		metadataPath: resolveSourcePath(root, sources.MetadataPath, filepath.Join(root, "gui", "module_dotting.json")),
		manifestPath: resolveSourcePath(root, sources.ManifestPath, filepath.Join(root, "seelex.project.md")),
	}
}

// resolveSourcePath 把相对来源路径拼到 Root 下；空路径回退约定默认值。
func resolveSourcePath(root, value, fallback string) string {
	if value == "" {
		return fallback
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(root, value)
}

// Root 返回项目根（同时是 Router 的项目作用域 ID）。
func (b *ProjectKnowledgeBuilder) Root() string { return b.root }

// ModulesDir / MetadataPath / ManifestPath 返回归一化后的来源路径（供诊断与测试）。
func (b *ProjectKnowledgeBuilder) ModulesDir() string   { return b.modulesDir }
func (b *ProjectKnowledgeBuilder) MetadataPath() string { return b.metadataPath }
func (b *ProjectKnowledgeBuilder) ManifestPath() string { return b.manifestPath }

// SourceHashes computes hashes over all contributing sources in fixed order.
// An error means no source is currently readable — the first build fails
// explicitly (首建失败显式跳过并提示), while an existing record falls back.
func (b *ProjectKnowledgeBuilder) SourceHashes() ([]string, error) {
	hashes, err := b.collectSourceHashes()
	if err != nil {
		return nil, err
	}
	if len(hashes) == 0 {
		return nil, fmt.Errorf("project knowledge: no sources found under %q", b.root)
	}
	return hashes, nil
}

func (b *ProjectKnowledgeBuilder) collectSourceHashes() ([]string, error) {
	paths := []string{}
	docs, err := b.moduleDocFiles()
	if err != nil {
		return nil, err
	}
	paths = append(paths, docs...)
	for _, optional := range []string{b.metadataPath, b.manifestPath} {
		if fileExists(optional) {
			paths = append(paths, optional)
		}
	}
	sort.Strings(paths)
	hashes := make([]string, 0, len(paths))
	for _, path := range paths {
		value, err := fileHash(path)
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, value)
	}
	return hashes, nil
}

// SameSources 判定已有记录的来源 hash 与当前一致（一致 → 复用，不重建）。
func (b *ProjectKnowledgeBuilder) SameSources(record ProjectRecord) bool {
	hashes, err := b.SourceHashes()
	if err != nil {
		return false
	}
	return sameHashes(record.SourceHashes, hashes)
}

// Build 扫描来源构建 ProjectRecord；Version 由 SourceHashes 推导。
func (b *ProjectKnowledgeBuilder) Build() (ProjectRecord, error) {
	hashes, err := b.SourceHashes()
	if err != nil {
		return ProjectRecord{}, err
	}
	return b.buildWithHashes(hashes)
}

// buildWithHashes 用已计算的来源 hash 构建记录（避免重复扫描）。
func (b *ProjectKnowledgeBuilder) buildWithHashes(hashes []string) (ProjectRecord, error) {
	modules, err := b.scanModules()
	if err != nil {
		return ProjectRecord{}, err
	}
	if len(modules) == 0 {
		return ProjectRecord{}, fmt.Errorf("project knowledge: no modules found under %q", b.root)
	}
	return ProjectRecord{
		Version:      versionFromHashes(hashes),
		Modules:      modules,
		SourceHashes: hashes,
		BuiltAt:      time.Now().UTC(),
	}, nil
}

// scanModules 合并三路来源：元数据（权威）→ 模块文档目录（补缺）→ 手工说明。
func (b *ProjectKnowledgeBuilder) scanModules() ([]ModuleSemantics, error) {
	modules := make([]ModuleSemantics, 0)
	seen := make(map[string]struct{})

	// 1. 模块元数据（module_dotting.json 的职责字段，权威来源）
	if entries, err := b.readMetadataModules(); err != nil {
		return nil, err
	} else {
		for _, module := range entries {
			key := strings.ToLower(module.Name)
			if key == "" {
				continue
			}
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			modules = append(modules, module)
		}
	}

	// 2. 模块文档目录扫描（补元数据未覆盖的模块）
	docModules, err := b.scanDocDirModules()
	if err != nil {
		return nil, err
	}
	for _, module := range docModules {
		key := strings.ToLower(module.Name)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		modules = append(modules, module)
	}

	// 3. 可选手工说明 seelex.project.md → project 模块
	if content, err := os.ReadFile(b.manifestPath); err == nil {
		modules = append(modules, ModuleSemantics{
			Name:    "project",
			Summary: truncateSummary(string(content)),
			Path:    b.root,
			Docs:    []string{"seelex.project.md"},
		})
	}

	sort.Slice(modules, func(i, j int) bool { return modules[i].Name < modules[j].Name })
	return modules, nil
}

// moduleDottingEntry 是 gui/module_dotting.json 模块条目（宽松解析，未知字段忽略）。
type moduleDottingEntry struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Status         string   `json:"status"`
	Responsibility string   `json:"responsibility"`
	Document       string   `json:"document"`
	Interfaces     []string `json:"interfaces"`
	PlannedPaths   []string `json:"planned_paths"`
	DependsOn      []string `json:"depends_on"`
}

type moduleDottingFile struct {
	SchemaVersion int                  `json:"schema_version"`
	System        string               `json:"system"`
	Modules       []moduleDottingEntry `json:"modules"`
}

func (b *ProjectKnowledgeBuilder) readMetadataModules() ([]ModuleSemantics, error) {
	data, err := os.ReadFile(b.metadataPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("project knowledge: read metadata %q: %w", b.metadataPath, err)
	}
	var file moduleDottingFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("project knowledge: parse metadata %q: %w", b.metadataPath, err)
	}
	modules := make([]ModuleSemantics, 0, len(file.Modules))
	for _, entry := range file.Modules {
		name := strings.TrimSpace(entry.ID)
		if name == "" {
			name = strings.TrimSpace(entry.Name)
		}
		if name == "" {
			continue
		}
		summary := strings.TrimSpace(entry.Responsibility)
		if summary == "" {
			summary = strings.TrimSpace(entry.Name)
		}
		module := ModuleSemantics{Name: name, Summary: summary}
		if len(entry.PlannedPaths) > 0 {
			module.Path = entry.PlannedPaths[0]
		}
		if strings.TrimSpace(entry.Document) != "" {
			module.Docs = append(module.Docs, entry.Document)
		}
		module.Docs = append(module.Docs, entry.Interfaces...)
		modules = append(modules, module)
	}
	return modules, nil
}

// scanDocDirModules 扫描模块文档目录：<dir>/<name>/ 目录或 <dir>/<name>.md 扁平文件，
// 每个文档取首个非空行作摘要。
func (b *ProjectKnowledgeBuilder) scanDocDirModules() ([]ModuleSemantics, error) {
	entries, err := os.ReadDir(b.modulesDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("project knowledge: scan modules dir %q: %w", b.modulesDir, err)
	}
	modules := make([]ModuleSemantics, 0)
	for _, entry := range entries {
		name, docs := "", []string(nil)
		if entry.IsDir() {
			name = entry.Name()
			files, err := b.markdownFiles(filepath.Join(b.modulesDir, entry.Name()))
			if err != nil {
				return nil, err
			}
			docs = files
		} else if strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			name = strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
			docs = []string{filepath.Join(b.modulesDir, entry.Name())}
		} else {
			continue
		}
		if name == "" {
			continue
		}
		modules = append(modules, ModuleSemantics{
			Name:    name,
			Summary: firstMeaningfulLine(docs),
			Path:    filepath.Join(b.modulesDir, entry.Name()),
			Docs:    docs,
		})
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Name < modules[j].Name })
	return modules, nil
}

func (b *ProjectKnowledgeBuilder) moduleDocFiles() ([]string, error) {
	entries, err := os.ReadDir(b.modulesDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("project knowledge: scan modules dir %q: %w", b.modulesDir, err)
	}
	files := []string(nil)
	for _, entry := range entries {
		if entry.IsDir() {
			sub, err := b.markdownFiles(filepath.Join(b.modulesDir, entry.Name()))
			if err != nil {
				return nil, err
			}
			files = append(files, sub...)
			continue
		}
		if strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			files = append(files, filepath.Join(b.modulesDir, entry.Name()))
		}
	}
	return files, nil
}

func (b *ProjectKnowledgeBuilder) markdownFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("project knowledge: scan %q: %w", dir, err)
	}
	files := []string(nil)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}

// ── 辅助 ──────────────────────────────────────────────────────────────

// summaryLimit 是项目记录摘要截断字符数（seele.yaml limits 段 summary_chars
// 经 ApplyLimits 注入；默认 800）。
var summaryLimit = 800

// ApplyLimits 注入 seele.yaml limits 段中 sessionstore 相关的配置
// （message_shard_size 已由 Config 支持，这里注入 summary_chars）。
func ApplyLimits(summaryChars int) {
	if summaryChars > 0 {
		summaryLimit = summaryChars
	}
}

func truncateSummary(content string) string {
	content = strings.TrimSpace(content)
	if len(content) <= summaryLimit {
		return content
	}
	return content[:summaryLimit] + "…"
}

// firstMeaningfulLine 取首个非空内容行作摘要（跳过 frontmatter 与标题标记）。
func firstMeaningfulLine(files []string) string {
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(strings.TrimLeft(line, "#-*_ "))
			if line == "" || strings.HasPrefix(line, "---") {
				continue
			}
			return truncateSummary(line)
		}
	}
	return ""
}

func fileHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("project knowledge: hash %q: %w", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func sameHashes(existing, current []string) bool {
	if len(existing) != len(current) {
		return false
	}
	for index := range current {
		if existing[index] != current[index] {
			return false
		}
	}
	return true
}

// versionFromHashes 由来源 hash 序列推导内容版本（顺序敏感）。
func versionFromHashes(hashes []string) string {
	sum := sha256.Sum256([]byte(strings.Join(hashes, "\n")))
	return hex.EncodeToString(sum[:])
}

// ── project_refresh 判定（存储层语义，plan.md §3.7.1）──────────────────

// ProjectRefreshResult 是 project_refresh 工具的判定结果。
type ProjectRefreshResult struct {
	Record   ProjectRecord
	Reused   bool   // 来源 hash 未变，直接复用已有记录（未重建）
	Fallback bool   // 重建失败，回退保留上一版本（可回退）
	Note     string // 回退/复用的补充说明
}

// RefreshProjectKnowledge 是 project_refresh 工具的核心：来源 hash 未变 → 复用；
// 已变/首建 → 重建并落盘；重建失败 → 保留上一版本（可回退）；首建失败返回错误
// （显式跳过并提示）。force=true 强制重建。
func RefreshProjectKnowledge(ctx context.Context, store *Router, builder *ProjectKnowledgeBuilder, force bool) (ProjectRefreshResult, error) {
	existing, readErr := store.LoadProjectRecord(builder.Root())
	hasExisting := readErr == nil
	if readErr != nil && !isProjectRecordNotFound(readErr) {
		return ProjectRefreshResult{}, fmt.Errorf("project knowledge: read existing record: %w", readErr)
	}
	hashes, err := builder.SourceHashes()
	if err != nil {
		if hasExisting {
			return ProjectRefreshResult{Record: existing, Fallback: true, Note: err.Error()}, nil
		}
		return ProjectRefreshResult{}, err
	}
	if !force && hasExisting && sameHashes(existing.SourceHashes, hashes) {
		return ProjectRefreshResult{Record: existing, Reused: true}, nil
	}
	record, err := builder.buildWithHashes(hashes)
	if err != nil {
		if hasExisting {
			return ProjectRefreshResult{Record: existing, Fallback: true, Note: err.Error()}, nil
		}
		return ProjectRefreshResult{}, err
	}
	if err := store.SaveProjectRecord(builder.Root(), record); err != nil {
		if hasExisting {
			return ProjectRefreshResult{Record: existing, Fallback: true, Note: err.Error()}, nil
		}
		return ProjectRefreshResult{}, err
	}
	return ProjectRefreshResult{Record: record}, nil
}
