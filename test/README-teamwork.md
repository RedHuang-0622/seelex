# test/teamwork（集成用例分卷）

## 生态位

teamwork / 子代理装配：headless 冒烟、插件装配与 worktree 生命周期。

覆盖：`teamwork_*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### teamwork_headless_smoke_test.go

- `func (r *smokeReport) add(name, format string, args ...any)`
- `func (r *smokeReport) text() string`
- `func (p *teamworkSmokeProvider) serve(t *testing.T, writer http.ResponseWriter, request *http.Request)`
- `func requestBodyText(raw json.RawMessage) (body string, lastUser string)` — requestBodyText 把请求体的 messages 解成一段可判定的正文（角色 + 文本）+ **最后一条
- `func messageContentText(content any) string` — messageContentText 取一条消息的文本（content 可能是字符串，也可能是分片数组）。
- `func requestSystemPrompt(raw json.RawMessage) string` — requestSystemPrompt 取请求里**第一条 system 消息**的正文：员工回合的装配面（装配
- `func roleFromWorkerInput(lastUser string) string` — roleFromWorkerInput 从 worker 的工作正文里取角色名（尾部 task 行
- `func (p *teamworkSmokeProvider) workerSamples() []workerSample` — workerSamples 是逐次 worker 回合的采样（装配落点的取证面）。
- `func (p *teamworkSmokeProvider) counts() (leader, worker int)`
- `func (p *teamworkSmokeProvider) triggeredCount() int` — triggeredCount 是"做完自动返回"起的回合数（脱锁读，供汇总用）。
- `func (p *teamworkSmokeProvider) leaderIndex() int`
- `func truncateBodyForLog(body string) string` — truncateBodyForLog 只截请求体前一段进日志（诊断用；正文可能很长）。
- `func newTeamworkSmoke(t *testing.T, provider *teamworkSmokeProvider) *teamworkSmoke` — newTeamworkSmoke 装配真实链路：真实 store / 工作区 / 引擎端口 / application，
- `func (s *teamworkSmoke) persistedItem(ctx context.Context, itemID string) (storeteamwork.WorkItem, bool)` — persistedItem 读**落盘**的计划里这一项（取证用：与看板投影分开读）。
- `func (s *teamworkSmoke) startProjectSession(t *testing.T, ctx context.Context) string` — startProjectSession 建一个绑定到项目工作区的会话（KeyFor 才有解）。
- `func (s *teamworkSmoke) waitBoard(t *testing.T, sessionID string, ready func(*dto.TeamworkBoardView) bool) *dto.TeamworkBoardView` — waitBoard 轮询看板直到条件满足（真实链路里投影是异步刷新的事件驱动）。
- `func (s *teamworkSmoke) waitJobTerminal(t *testing.T, handle string) dto.TeamworkJobCompletionRecord` — waitJobTerminal 轮询 teammate 作业表直到该句柄终态（或超时）。
- `func (s *teamworkSmoke) waitUserRow(t *testing.T, needle string) string` — waitUserRow 轮询可见会话直到出现含 needle 的用户行（自动返回的证据）。
- `func (s *teamworkSmoke) waitToolResult(t *testing.T, tool, needle string) string` — waitToolResult 轮询可见会话直到**某个工具**的结果含 needle（回合是异步跑的，断言要给窗口：
- `func (s *teamworkSmoke) toolStatuses(name string) []string` — toolStatuses 按顺序取某个工具每次调用的状态（success / error）。
- `func firstLineOf(text string) string` — firstLineOf 取文本首行（探针输出要短）。
- `func (s *teamworkSmoke) conversationDump() string` — conversationDump 把可见会话逐行摊开（探针失败时的取证：哪一行是谁、工具名与正文）。
- `func (s *teamworkSmoke) toolResultsOf(name string) []string` — toolResultsOf 从可见会话里按顺序取某个工具的每次结果文本（探针用；同名工具可能被调用
- `func (s *teamworkSmoke) toolResultOf(name string) string` — toolResultOf 取某个工具最后一次的结果文本。
- `func (s *teamworkSmoke) toolResultMatching(name, needle string) string` — toolResultMatching 取某个工具结果里含 needle 的那一次（负路径取证用）。
- `func initSmokeGitRepo(t *testing.T) string` — initSmokeGitRepo 造一个临时 git 仓库（冒烟要跑**真** worktree：一 Work Item 一套现场
- `func runGit(root string, args ...string) error`
- `func TestTeamworkHeadlessSmoke(t *testing.T)` — TestTeamworkHeadlessSmoke 是这一轮的**逐阶段冒烟**（阶段错位只会在真实装配上显形）。

### teamwork_plugin_assembly_headless_test.go

- `func TestTeamworkPluginAssemblyHeadlessSmoke(t *testing.T)`
- `func decodeAssemblyReceipt(t *testing.T, text string) assemblyReceipt`
- `func assemblyViewFor(t *testing.T, receipt assemblyReceipt, role string) dto.PluginAssemblyView`
- `func assemblyDispatchHandles(t *testing.T, receipts []string) []string` — assemblyDispatchHandles 从派发受理回执里取句柄（受理回执是 JSON，但工具结果可能被
- `func attachRealSkills(t *testing.T, runtime *assemble.Runtime)` — attachRealSkills 把**真插件根**的技能装上 Runtime 的员工目录读面。
- `func assemblySampleFor(t *testing.T, samples []workerSample, role, marker string) workerSample` — assemblySampleFor 取某个 worker 回合的采样：按角色名辨认，并**用探针标记再确认一次**
- `func assemblySameStrings(got, want []string) bool`

### teamwork_worktree_lifecycle_test.go

- `func (p *sceneProbe) record()`
- `func (p *sceneProbe) snapshot() (dirSeen, fileInScene, fileInMain, together bool, samples int)`
- `func TestTeamworkWorktreeLifecycleChain(t *testing.T)` — TestTeamworkWorktreeLifecycleChain 端到端验 teammate 那条链：
- `func hasNodeRecord(records []storeworkunit.Record, nodeID string) bool`
- `func dirExists(path string) bool`
- `func runGitOutput(root string, args ...string) (string, error)` — runGitOutput 取 git 的 stdout（`runGit` 只回错误码，诊断面要看原文）。
- `func gitBranch(root, branch string) string` — gitBranch 返回某个本地分支的原文（空串 = 分支不在）。
