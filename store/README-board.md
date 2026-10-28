# store/board（根包分卷）

## 生态位

项目看板投影、会话看板元数据、项目知识与项目记录的根包分卷。M1 起，追加、折叠、
压实、路径校验、按路径锁和 `BOARD.md` 渲染的唯一实现位于
[`board/README.md`](board/README.md)；根包 `project_board_compat.go` 只保留同身份
alias 与单层 Router 转发。

M10 起，goal/team 会话看板存档的形状位于 `contract/dto/session_board.go`，校验与
head 发布位于 `board/session.go`。生产装配通过 typed `store.Stores.Boards` 注入，
不再调用 `Router.BoardsFor*`；根 `board.go` 只保留兼容 alias。

覆盖：`board*.go`、`project_board_compat*.go`、`project_known*.go`、
`project_record*.go`；`project_board.go` / `project_board_markdown.go` 已在 M1
迁出根包。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### board.go

- `func ValidateGoalBoardMeta(meta GoalBoardMeta) error` — ValidateGoalBoardMeta 校验 goal 看板存档。
- `func ValidateTeamBoardMeta(meta TeamBoardMeta) error` — ValidateTeamBoardMeta 校验团队看板存档。

### board/session.go

- `func NewSessionStore(layout *storelayout.Layout) *SessionStore`
- `func (s *SessionStore) Bind(key dto.SessionKey) *BoundStore`
- `func (s *SessionStore) WriteGoalBoard(ctx, key, meta) error`
- `func (s *SessionStore) ReadGoalBoard(ctx, key) (dto.GoalBoardMeta, error)`
- `func (s *SessionStore) WriteTeamBoard(ctx, key, meta) error`
- `func (s *SessionStore) ReadTeamBoard(ctx, key) (dto.TeamBoardMeta, error)`
- `func ValidateGoalBoardMeta(meta dto.GoalBoardMeta) error`
- `func ValidateTeamBoardMeta(meta dto.TeamBoardMeta) error`

### board_test.go

- `func boardFixture(t *testing.T) (*jsonRepository, Key)`
- `func sampleGoalBoard() GoalBoardMeta`
- `func sampleTeamBoard(t *testing.T) TeamBoardMeta`
- `func TestGoalBoardRoundTrip(t *testing.T)` — TestGoalBoardRoundTrip：active seq 的 goal 与 history goals 各自完整往返。
- `func TestTeamBoardRoundTrip(t *testing.T)` — TestTeamBoardRoundTrip：团队看板的载荷是不透明视图快照，原样往返（存储层不解析它）。
- `func TestBoardReadMissingIsEmptyNotError(t *testing.T)` — TestBoardReadMissingIsEmptyNotError：没写过 = "还没有存档"这个**状态**，读面返回零值 + nil
- `func TestBoardArchiveSurvivesReopen(t *testing.T)` — TestBoardArchiveSurvivesReopen：**重启恢复**的存储面证据——换一个 repository 实例
- `func TestBoardModulesAreSeparateFiles(t *testing.T)` — TestBoardModulesAreSeparateFiles：两块看板是**两个模块文件**（各自 head、各自锁），
- `func TestGoalBoardRejectsSameGoalInActiveAndHistory(t *testing.T)` — TestGoalBoardRejectsSameGoalInActiveAndHistory：§3.1 的硬不变式。
- `func TestGoalBoardRejectsMalformedLifecycle(t *testing.T)` — TestGoalBoardRejectsMalformedLifecycle：生命周期头是硬约束（close 必须说明原因与时间）。
- `func TestTeamBoardRejectsActiveWithoutSnapshot(t *testing.T)` — TestTeamBoardRejectsActiveWithoutSnapshot：在册的团队看板没有快照 = 重启后什么都没有。
- `func TestBoardClosedKeepsClosedFacts(t *testing.T)` — TestBoardClosedKeepsClosedFacts：close 之后存档保留（§9 第 3 条），
- `func TestBoardsNotInRepositoryMainInterface(t *testing.T)` — TestBoardsNotInRepositoryMainInterface：看板存档是**可选能力面**（与 teamwork 同一形态），

### project_board_compat.go

- `const ProjectBoardSchema` / `ProjectBoardMaxRows` / `ProjectBoardWorkFile` / `ProjectBoardTeamworkFile` / `ProjectBoardMarkdownFile` — 同身份常量 alias，事实源在 `contract/dto`。
- `type ProjectBoardScope` / `ProjectBoardRow` / `ProjectBoardReadOptions` — 同身份 DTO alias。
- `func ProjectBoardKinds() []string` — 转发 `store/board.Kinds`。
- `func FoldProjectBoardRows(rows []ProjectBoardRow, logf func(string, ...any)) []ProjectBoardRow` — 转发 `store/board.Fold`。
- `func ProjectBoardDir(projectRoot string) string` / `ProjectBoardSessionDir(projectRoot, sessionID string) string` — 转发看板路径派生。
- `func ProjectBoardMarkdown(rows []ProjectBoardRow) []byte` — 转发 `store/board.Markdown`。
- `func (router *Router) AppendProjectBoardRows(projectRoot, sessionID, file string, rows []ProjectBoardRow) (uint64, error)` — 单层转发 `store/board.Append`。
- `func (router *Router) ReadProjectBoardRows(projectRoot, sessionID, file string) ([]ProjectBoardRow, error)` — 单层转发 `store/board.ReadRows`。
- `func (router *Router) ProjectBoardWatermark(projectRoot, sessionID, file string) (uint64, error)` — 单层转发 `store/board.Watermark`。
- `func (router *Router) ReadProjectBoard(projectRoot string, opts ProjectBoardReadOptions) ([]ProjectBoardRow, bool, error)` — 单层转发 `store/board.Read`。
- `func (router *Router) ReclaimProjectBoard(_ /*projectID*/, sessionID string) error` — 解析注入的项目根后转发 `store/board.Reclaim`。
- `func (router *Router) ReadProjectBoardMarkdown(projectRoot string) ([]byte, error)` — 单层转发 `store/board.ReadMarkdown`。

### project_board_compat_test.go

- `func TestProjectBoardRouterCompatibilityForwardsToBoardOwner(t *testing.T)` — 验证根 Router 薄转发仍能写读投影与人读件。
- `func TestProjectBoardReclaimUsesInjectedProjectRootButLeavesModuleLocksAlone(t *testing.T)` — 验证回收注入根且看板锁不与任何模块锁别名。

### project_known.go

- `func ProjectKnownMarkdown(record ProjectRecord) []byte` — ProjectKnownMarkdown 把 `ProjectRecord` 渲染成人读 markdown。
- 文本清洗已迁到 `store/internal/textsanitize`（`KnownText` / `RepoRelativePath`），本文件不再保留第二份正则或路径判据。
- `func projectKnownPath(projectRoot string) string` — projectKnownPath 返回项目摘要渲染件的物理路径：
- `func writeProjectKnown(projectRoot string, data []byte) error` — writeProjectKnown 落一份渲染件（空内容不落盘；同路径独立锁 + 原子发布）。
- `func readProjectKnown(projectRoot string) ([]byte, error)` — readProjectKnown 读回渲染件（不存在 → `fs.ErrNotExist`；空文件 → 显式报错）。
- `func (router *Router) WriteProjectKnown(projectRoot string, data []byte) error` — WriteProjectKnown 原子写一份项目摘要渲染件（projectRoot = 项目根目录）。
- `func (router *Router) ReadProjectKnown(projectRoot string) ([]byte, error)` — ReadProjectKnown 读回项目摘要渲染件；从未渲染过 ⇒ `fs.ErrNotExist`；空根显式报错。

### project_known_test.go

- `func projectKnownAbsPath(t *testing.T) string` — projectKnownAbsPath 造一个"带用户名 + 盘符"的绝对路径（本机 tempdir 之外也可复现）。
- `func projectKnownRecord(t *testing.T, builtAt time.Time) (ProjectRecord, string)` — projectKnownRecord 造一条"含所有脏字段"的记录：未排序模块 + 绝对 Path/Docs +
- `func TestProjectKnownRenderDeterministic(t *testing.T)` — TestProjectKnownRenderDeterministic 对应 T5.3：同一 record 两次渲染字节相同；
- `func TestProjectKnownPassiveOnly(t *testing.T)` — TestProjectKnownPassiveOnly 对应 T5.4（R6）：**无显式调用 → 不产文件**。
- `func TestProjectKnownReusesExistingChain(t *testing.T)` — TestProjectKnownReusesExistingChain 对应 T5.5：**命中即处理、未命中传递** ——
- `func TestProjectKnownRebuildFailureKeepsLast(t *testing.T)` — TestProjectKnownRebuildFailureKeepsLast 对应 T5.6：重建失败保留上一版本；
- `func TestProjectKnownPathFollowsProjectRoot(t *testing.T)` — TestProjectKnownPathFollowsProjectRoot 覆盖"落点跟**项目根**走"：换一个项目根，

### project_record.go

- `func isProjectRecordNotFound(err error) bool` — isProjectRecordNotFound reports whether the record has never been built.
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
- `func sameHashes(existing, current []string) bool`
- `func versionFromHashes(hashes []string) string` — versionFromHashes 由来源 hash 序列推导内容版本（顺序敏感）。
- `func RefreshProjectKnowledge(ctx context.Context, store *Router, projectID string, builder *ProjectKnowledgeBuilder, force bool) (ProjectRefreshResult, error)` — RefreshProjectKnowledge 是 project_refresh 工具的核心：来源 hash 未变 → 复用；

### project_record_test.go

- `func TestProjectRecordRoundTripAcrossLocalBackends(t *testing.T)`
- `func TestProjectRecordEmptyProjectIDRejected(t *testing.T)`
- `func newKnowledgeProject(t *testing.T) string` — newKnowledgeProject 构造含三路来源的临时项目：
- `func TestProjectKnowledgeBuilderBuildsModules(t *testing.T)`
- `func TestProjectKnowledgeBuilderSameSourcesDetectsChanges(t *testing.T)`
- `func TestProjectKnowledgeBuilderFailsWithoutSources(t *testing.T)`
- `func TestRefreshProjectKnowledgeReusesWhenUnchanged(t *testing.T)`
- `func TestRefreshProjectKnowledgeFallsBackOnRebuildFailure(t *testing.T)`
- `func TestIsProjectRecordNotFound(t *testing.T)`
