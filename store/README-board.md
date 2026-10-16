# store/board（根包分卷）

## 生态位

项目看板投影：行文件、会话分目录、项目知识记录与渲染件

覆盖 10 个文件（前缀 `board`/`project_board`/`project_known`/`project_record`）。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_store_readme_volumes.py`。

### board.go

- `func (l BoardLifecycle) Closed() bool` — Closed 报告这块看板是否已关闭。
- `func Boards(repository Repository) (BoardRepository, bool)` — Boards 把 Repository 收窄到看板存档面（JSON 后端实现它；其他后端返回 false）。
- `func (router *Router) BoardsFor() (BoardRepository, bool)` — BoardsFor 返回**当前活跃后端**的看板存档读/写面。
- `func (router *Router) BoardsForSession(sessionID string) (SessionBoards, bool)` — BoardsForSession 返回绑定到某会话的看板存档面（Key = Router 当前写作用域 +
- `func (b sessionBoards) WriteGoalBoard(ctx context.Context, meta GoalBoardMeta) error`
- `func (b sessionBoards) ReadGoalBoard(ctx context.Context) (GoalBoardMeta, error)`
- `func (b sessionBoards) WriteTeamBoard(ctx context.Context, meta TeamBoardMeta) error`
- `func (b sessionBoards) ReadTeamBoard(ctx context.Context) (TeamBoardMeta, error)`
- `func ValidateGoalBoardMeta(meta GoalBoardMeta) error` — ValidateGoalBoardMeta 校验 goal 看板存档。
- `func ValidateTeamBoardMeta(meta TeamBoardMeta) error` — ValidateTeamBoardMeta 校验团队看板存档。
- `func validateBoardLifecycle(lifecycle BoardLifecycle, kind string) error` — validateBoardLifecycle 校验两块看板共用的生命周期头。
- `func (repository *jsonRepository) WriteGoalBoard(_ context.Context, key Key, meta GoalBoardMeta) error` — WriteGoalBoard 写入（整份替换）goal 看板存档。
- `func (repository *jsonRepository) ReadGoalBoard(_ context.Context, key Key) (GoalBoardMeta, error)` — ReadGoalBoard 读回 goal 看板存档。缺失 = 这个会话还没有存档（正常状态）：
- `func (repository *jsonRepository) WriteTeamBoard(_ context.Context, key Key, meta TeamBoardMeta) error` — WriteTeamBoard 写入（整份替换）团队看板存档。
- `func (repository *jsonRepository) ReadTeamBoard(_ context.Context, key Key) (TeamBoardMeta, error)` — ReadTeamBoard 读回团队看板存档。缺失 = 还没有存档：返回**零值 + nil**

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

### project_board.go

- `func ProjectBoardKinds() []string` — ProjectBoardKinds 返回本契约承认的 kind 全集（有序，便于测试与文档同步）。
- `func isProjectBoardKind(kind string) bool` — isProjectBoardKind 报告 kind 是否在本契约的 kind 全集内。
- `func projectBoardSessionFile(file string) (string, error)` — projectBoardSessionFile 校验并归一化**会话目录内**的投影文件名（只允许两份 jsonl，
- `func sessionBoardSegment(sessionID string) (string, error)` — sessionBoardSegment 把会话 ID 校验并归一化成**目录名**（落点是按会话分目录的）。
- `func validateProjectRoot(projectRoot string) error` — validateProjectRoot 校验项目根（落点的唯一输入）。
- `func FoldProjectBoardRows(rows []ProjectBoardRow, logf func(string, ...any)) []ProjectBoardRow` — FoldProjectBoardRows 是投影行**唯一的读法**（折叠读）：把追加日志折成当前视图。
- `func projectBoardRowLess(left, right ProjectBoardRow) bool` — projectBoardRowLess 是投影行的**唯一排序口径**：(Kind, Key, Session)。
- `func ProjectBoardDir(projectRoot string) string` — ProjectBoardDir 返回某项目根的看板目录：`<项目根>/.seelex/board`。
- `func ProjectBoardSessionDir(projectRoot, sessionID string) string` — ProjectBoardSessionDir 返回某会话在看板里的**自己的目录**：
- `func projectBoardSessionPath(projectRoot, sessionID, file string) (string, error)` — projectBoardSessionPath 返回某会话某投影文件的物理路径（唯一带校验的派生口）。
- `func projectBoardMarkdownPath(projectRoot string) string` — projectBoardMarkdownPath 返回人读件的路径（看板根下，**不分会话**：它是全项目汇总）。
- `func projectBoardLock(path string) *sync.Mutex` — projectBoardLock 返回某投影文件路径的独立锁（按路径分片，互不阻塞）。
- `func readProjectBoardFile(path string) ([]ProjectBoardRow, error)` — readProjectBoardFile 读回投影文件的**原始行**（未折叠）：跳过空行与崩溃残尾
- `func decodeProjectBoardRows(data []byte) []ProjectBoardRow` — decodeProjectBoardRows 解析 JSONL 投影行（残尾 / 空行 / 坏行跳过）。
- `func encodeProjectBoardRows(rows []ProjectBoardRow) ([]byte, error)` — encodeProjectBoardRows 把行集编码成 JSONL 字节（每行一条、换行收尾）。
- `func projectBoardBatchAlreadyAppended(existing, rows []ProjectBoardRow) bool` — projectBoardBatchAlreadyAppended 报告这一批是否**已经落过**：文件里已有同一个 Seq 的行。
- `func projectBoardStampSession(sessionID string, rows []ProjectBoardRow) error` — projectBoardStampSession 给整批行盖会话归属，并挡下"把别的会话的行写进这个会话的目录"。
- `func projectBoardWatermark(rows []ProjectBoardRow) uint64` — projectBoardWatermark 返回投影文件的水位（= 最大 Seq）。
- `func projectBoardMaxSeq(rows []ProjectBoardRow) uint64` — projectBoardMaxSeq 返回一批行的 Seq 最大值。
- `func readProjectBoardRowsForAppend(path string) ([]ProjectBoardRow, error)` — readProjectBoardRowsForAppend 读回投影文件的**完整行**：先截崩溃残尾（复用
- `func compactProjectBoardFile(path string, existing []ProjectBoardRow) ([]ProjectBoardRow, error)` — compactProjectBoardFile 把追加流**压实**成"每个 (会话, Key) 的最新一行"并原子重写。
- `func appendProjectBoardRows(projectRoot, sessionID, file string, rows []ProjectBoardRow) (uint64, error)` — appendProjectBoardRows 是投影追加的**唯一实现**：
- `func reclaimProjectBoard(projectRoot, sessionID string) error` — reclaimProjectBoard 回收某会话自己的投影目录（删除会话时同批调用）。
- `func readProjectBoardFoldedRows(projectRoot string) ([]ProjectBoardRow, error)` — readProjectBoardFoldedRows 读回**本项目的全部折叠行**（逐会话目录合并两份投影）。
- `func projectBoardSessionDirs(projectRoot string) []string` — projectBoardSessionDirs 列出看板根下**按会话分出来的子目录名**（有序；不存在 ⇒ 空）。
- `func refreshProjectBoardMarkdown(projectRoot string)` — refreshProjectBoardMarkdown 按当前投影重建人读件；没有行 ⇒ 删掉它（I6：空则退场）。
- `func (router *Router) SetProjectBoardRootFor(resolve func(sessionID string) string)` — SetProjectBoardRootFor 注入「会话 → 项目根」的解析器（装配期一次；nil = 未装配）。
- `func (router *Router) projectBoardRootOf(sessionID string) string` — projectBoardRootOf 读当前解析器并解析会话的项目根（未装配 / 未绑定 ⇒ ""）。
- `func (router *Router) AppendProjectBoardRows(projectRoot, sessionID, file string, rows []ProjectBoardRow) (uint64, error)` — AppendProjectBoardRows 追加一批投影行，返回追加后的水位（= 文件内最大 Seq）。
- `func (router *Router) ReadProjectBoardRows(projectRoot, sessionID, file string) ([]ProjectBoardRow, error)` — ReadProjectBoardRows 读回**某会话某投影文件**的折叠读（唯一读法）。
- `func (router *Router) ProjectBoardWatermark(projectRoot, sessionID, file string) (uint64, error)` — ProjectBoardWatermark 返回某会话某投影文件的水位（= 文件内最大 Seq）。
- `func (router *Router) ReadProjectBoard(projectRoot string, opts ProjectBoardReadOptions) ([]ProjectBoardRow, bool, error)` — ReadProjectBoard 读回一个项目**跨会话**的全部折叠行（白名单文件之一），有界、只读。
- `func (router *Router) ReclaimProjectBoard(_ /*projectID*/, sessionID string) error` — ReclaimProjectBoard 回收某会话自己的投影目录（幂等：目录不在不算错）。
- `func (router *Router) ReadProjectBoardMarkdown(projectRoot string) ([]byte, error)` — ReadProjectBoardMarkdown 读回人读件（不存在 ⇒ `fs.ErrNotExist`）。

### project_board_landing_test.go

- `func TestProjectBoardMustNotLandUnderDataRoot(t *testing.T)` — TestProjectBoardMustNotLandUnderDataRoot：投影不得再出现在数据根下。
- `func TestProjectBoardLandsInProjectBoardDir(t *testing.T)` — TestProjectBoardLandsInProjectBoardDir：正面判据——会话的投影出现在它自己的目录下，

### project_board_markdown.go

- `func ProjectBoardMarkdown(rows []ProjectBoardRow) []byte` — ProjectBoardMarkdown 把**折叠后的投影行**渲染成人读看板。
- `func newBoardFacts(write func(string) (int, error)) func(key, value string)` — newBoardFacts 返回一个"写一条 - key：value"的小写入器（空值不写，保持确定性）。
- `func boardText(payload map[string]any, key string) string` — boardText 取载荷里的文本字段并清洗（非字符串 / 空 ⇒ 空串）。
- `func boardTextList(payload map[string]any, key string) []string` — boardTextList 取载荷里的字符串数组字段并清洗（保持原顺序，空项剔除）。
- `func boardCell(value string) string` — boardCell 把一段文本规整成 markdown 表格单元格（换行折成空格、竖线转义）。

### project_board_test.go

- `func projectBoardRow(kind, key string, seq uint64, removed bool) ProjectBoardRow` — projectBoardRow 造一条投影行。
- `func projectBoardFixture(t *testing.T) (*Router, string)` — projectBoardFixture 提供一个 router（存储面宿主）与一个临时项目根。
- `func projectBoardLineCount(t *testing.T, path string) int` — projectBoardLineCount 数投影文件里的非空行数（-1 表示文件不存在）。
- `func projectBoardTestPath(t *testing.T, projectRoot, sessionID, file string) string` — projectBoardTestPath 取某会话某投影文件的路径（走存储面**唯一**的带校验派生口；
- `func TestProjectBoardAppendIdempotent(t *testing.T)` — TestProjectBoardAppendIdempotent 对应 T1.1：同一批行重复追加是幂等空操作。
- `func TestFoldProjectBoardRowsLatestWins(t *testing.T)` — TestFoldProjectBoardRowsLatestWins 对应 T1.2：同一 (会话, Key) 取 Seq 最大的行。
- `func TestFoldProjectBoardRowsKeepsCrossSessionKeys(t *testing.T)` — TestFoldProjectBoardRowsKeepsCrossSessionKeys 是本轮新增的口径：落点是**项目级
- `func withBoardSession(row ProjectBoardRow, sessionID string) ProjectBoardRow` — withBoardSession 给一行盖会话章（测试用）。
- `func TestFoldProjectBoardRowsRemovedDropped(t *testing.T)` — TestFoldProjectBoardRowsRemovedDropped 对应 tombstone：最新行 removed=true ⇒ 该 Key 消失。
- `func TestFoldProjectBoardRowsUnknownSkipped(t *testing.T)` — TestFoldProjectBoardRowsUnknownSkipped 对应 T1.3：未知 schema / kind 跳过且记一行日志。
- `func TestProjectBoardCrashTailSelfHeals(t *testing.T)` — TestProjectBoardCrashTailSelfHeals 对应 T1.4：残尾行丢弃、追加从完整行边界续写。
- `func TestProjectBoardPathFollowsProjectRoot(t *testing.T)` — TestProjectBoardPathFollowsProjectRoot 对应 T1.5：路径钉在**项目目录 + 会话目录**之下
- `func TestProjectBoardPathNoWorkspaceIDNoAbsolute(t *testing.T)` — TestProjectBoardPathNoWorkspaceIDNoAbsolute 对应 T1.6：**行内**不出现工作区 ID
- `func TestProjectBoardLocksDistinct(t *testing.T)` — TestProjectBoardLocksDistinct 对应 T1.7：投影用独立锁（不新增模块枚举、不与
- `func TestProjectBoardRowsBounded(t *testing.T)` — TestProjectBoardRowsBounded 对应 T1.8：行数越界显式报错（不静默截断）。
- `func TestProjectBoardEmptyNoFile(t *testing.T)` — TestProjectBoardEmptyNoFile 对应 T1.9：空批不建文件（空则退场）。
- `func TestProjectBoardDeleteBindsSession(t *testing.T)` — TestProjectBoardDeleteBindsSession 对应 T1.10：删除会话同批回收它**自己那个目录**
- `func TestProjectBoardReadProjectBoardAggregates(t *testing.T)` — TestProjectBoardReadProjectBoardAggregates 覆盖聚合读：逐会话目录读、按行数/会话数
- `func TestProjectBoardMarkdownRenderedAndDeterministic(t *testing.T)` — TestProjectBoardMarkdownRenderedAndDeterministic 是新落点的**人读件**判据：
- `func TestProjectBoardSessionsOwnTheirFiles(t *testing.T)` — TestProjectBoardSessionsOwnTheirFiles 钉住"按会话分目录"要换来的三件事：
- `func TestProjectBoardRejectsForeignSessionRows(t *testing.T)` — TestProjectBoardRejectsForeignSessionRows：**串写防线**——一批行盖着别的会话的章，
- `func TestProjectBoardSessionSegmentGuards(t *testing.T)` — TestProjectBoardSessionSegmentGuards：会话 ID 当目录名前的**唯一一道闸**。
- `func workRow(kind, key string, payload map[string]any) ProjectBoardRow` — workRow 造一条带完整载荷的投影行（渲染用例用）。
- `func TestProjectBoardKeepsUpdatingAfterBoundReached(t *testing.T)` — TestProjectBoardKeepsUpdatingAfterBoundReached 是 R-1 的红灯用例：同一个项目文件在

### project_known.go

- `func ProjectKnownMarkdown(record ProjectRecord) []byte` — ProjectKnownMarkdown 把 `ProjectRecord` 渲染成人读 markdown。
- `func sanitizeKnownText(value string) string` — sanitizeKnownText 清洗人读文本：剔除 `ws-<nano>` 与盘符绝对路径（渲染件要能提交）。
- `func repoRelativePath(value string) (string, bool)` — repoRelativePath 判定并归一化**仓库相对路径**；绝对路径一律丢弃（ok=false）。
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
