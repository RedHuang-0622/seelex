# Project Store

## 生态位

`store/project` 拥有项目级语义记录、项目知识构建与 `project-known.md` 渲染。
调用方是 `project_refresh` 工具、组合根和需要项目知识的会话查询；它不拥有
session transcript、Board 行或 Teamwork 状态。

## 职责与非职责

- 负责：来源扫描、来源 hash、`ProjectRecord` 构建、复用/重建/回退判定，
  以及项目摘要的可提交 Markdown 渲染与原子发布。
- 非职责：会话目录、看板行、team 计划、任何 UI 运行态。
- `RecordStore` 由消费方实现；本包不知道 Router 或具体存储布局。

## 核心实现

`ProjectKnowledgeBuilder` 合并模块元数据、模块文档和手写项目说明。
`RefreshProjectKnowledge` 保持既有“来源不变复用、构建失败回退、首建失败报错”
语义。`ProjectKnownMarkdown` 是纯函数，输出不含时间戳、绝对路径或工作区 ID。

## 依赖方向

允许依赖 `store/board`（项目摘要目录身份）、`store/internal/textsanitize`、
`store/root` 和标准库。禁止 import 根 `store`、Application、Middle 或 Backend。

## 数据与验证

项目级 record 仍由根 Router 的现有 JSON 通道持久化；本包只经 `RecordStore`
读写。`project-known.md` 路径保持 `<项目根>/.seelex/board/project-known.md`。

```text
go test ./store/project ./store ./boot -count=1
go test ./test -run ProjectBoard -count=1
```

Review 重点：来源 hash 顺序、失败回退、渲染确定性、路径清洗、空输入拒绝。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### project_known.go

- `func ProjectKnownMarkdown(record ProjectRecord) []byte` — ProjectKnownMarkdown 把 `ProjectRecord` 渲染成人读 markdown。
- `func projectKnownPath(projectRoot string) string` — projectKnownPath 返回项目摘要渲染件的物理路径：
- `func KnownPath(projectRoot string) string` — KnownPath 返回项目摘要渲染件的物理路径。
- `func WriteKnown(projectRoot string, data []byte) error` — WriteKnown 落一份渲染件（空内容不落盘；同路径独立锁 + 原子发布）。
- `func ReadKnown(projectRoot string) ([]byte, error)` — ReadKnown 读回渲染件（不存在 → `fs.ErrNotExist`；空文件 → 显式报错）。

### project_record.go

- `func IsRecordNotFound(err error) bool` — IsRecordNotFound reports whether the record has never been built.
- `func NewProjectKnowledgeBuilder(sources ProjectKnowledgeSources) *ProjectKnowledgeBuilder` — NewProjectKnowledgeBuilder creates a builder with normalized absolute paths.
- `func resolveSourcePath(root, value, fallback string) string` — resolveSourcePath 把相对来源路径拼到 Root 下；空路径回退约定默认值。
- `func (b *ProjectKnowledgeBuilder) Root() string` — Root 返回项目根（同时是 Router 的项目作用域 ID）。
- `func (b *ProjectKnowledgeBuilder) ModulesDir() string` — ModulesDir / MetadataPath / ManifestPath 返回归一化后的来源路径（供诊断与测试）。
- `func (b *ProjectKnowledgeBuilder) MetadataPath() string`
- `func (b *ProjectKnowledgeBuilder) ManifestPath() string`
- `func (b *ProjectKnowledgeBuilder) SourceHashes() ([]string, error)` — SourceHashes computes hashes over all contributing sources in fixed order.
- `func (b *ProjectKnowledgeBuilder) collectSourceHashes() ([]string, error)`
- `func (b *ProjectKnowledgeBuilder) SameSources(record ProjectRecord) bool` — SameSources 判定已有记录的来源 hash 与当前一致（一致 → 复用，不重建）。
- `func (b *ProjectKnowledgeBuilder) Build() (ProjectRecord, error)` — Build 扫描来源构建 ProjectRecord；Version 由 SourceHashes 推导。
- `func (b *ProjectKnowledgeBuilder) buildWithHashes(hashes []string) (ProjectRecord, error)` — buildWithHashes 用已计算的来源 hash 构建记录（避免重复扫描）。
- `func (b *ProjectKnowledgeBuilder) scanModules() ([]ModuleSemantics, error)` — scanModules 合并三路来源：元数据（权威）→ 模块文档目录（补缺）→ 手工说明。
- `func (b *ProjectKnowledgeBuilder) readMetadataModules() ([]ModuleSemantics, error)`
- `func (b *ProjectKnowledgeBuilder) scanDocDirModules() ([]ModuleSemantics, error)` — scanDocDirModules 扫描模块文档目录：<dir>/<name>/ 目录或 <dir>/<name>.md 扁平文件，
- `func (b *ProjectKnowledgeBuilder) moduleDocFiles() ([]string, error)`
- `func (b *ProjectKnowledgeBuilder) markdownFiles(dir string) ([]string, error)`
- `func ApplyLimits(summaryChars int)` — ApplyLimits 注入 seele.yaml limits 段中 store 相关的配置
- `func truncateSummary(content string) string`
- `func firstMeaningfulLine(files []string) string` — firstMeaningfulLine 取首个非空内容行作摘要（跳过 frontmatter 与标题标记）。
- `func fileHash(path string) (string, error)`
- `func fileExists(path string) bool`
- `func FileExists(path string) bool` — FileExists reports whether path names a regular file.
- `func sameHashes(existing, current []string) bool`
- `func versionFromHashes(hashes []string) string` — versionFromHashes 由来源 hash 序列推导内容版本（顺序敏感）。
- `func RefreshProjectKnowledge(ctx context.Context, store RecordStore, projectID string, builder *ProjectKnowledgeBuilder, force bool) (ProjectRefreshResult, error)` — RefreshProjectKnowledge 是 project_refresh 工具的核心：来源 hash 未变 → 复用；
