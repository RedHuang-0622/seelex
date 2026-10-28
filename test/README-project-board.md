# test/project-board（集成用例分卷）

## 生态位

项目看板：读/写/刷新工具与装配接线。

覆盖：`project_board_*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### project_board_e2e_test.go

- `func newProjectBoardE2EServer(t *testing.T) *projectBoardE2EServer`
- `func (server *projectBoardE2EServer) setScript(script ...projectBoardE2EScript)` — setScript 覆盖后续调用脚本（测试在"回合之间"改脚本）。
- `func (server *projectBoardE2EServer) recorded() [][]byte`
- `func (server *projectBoardE2EServer) serve(t *testing.T, writer http.ResponseWriter, request *http.Request)`
- `func (script projectBoardE2EScript) argumentsOrDefault() string`
- `func projectBoardE2EReadArgs(sessionID, file string) string` — projectBoardE2EReadArgs 组 project_board_read 的入参（指定会话与文件）。
- `func TestProjectBoardEndToEndThroughToolFace(t *testing.T)`

### project_board_i7_test.go

- `func promptLayerBytes(t *testing.T, components assembly.Components) string` — promptLayerBytes 把装配前缀（PromptLayers）渲染成可比较的字节。
- `func projectBoardAccountsFor(t *testing.T, providerURL, root string) string` — projectBoardAccountsFor 写一份指向本地 mock provider 的 accounts.yaml。
- `func writeMarkedProjectBoardProjection(t *testing.T, harness fullChainHarness, projectRoot string)` — writeMarkedProjectBoardProjection 往存储面写一份带标记的投影（两份文件都写）。
- `func TestProjectBoardPrefixBytesIdentical(t *testing.T)` — TestProjectBoardPrefixBytesIdentical（T4.1）：开/关两臂的装配前缀逐字节相同。
- `func TestProjectBoardNeverReachesRequest(t *testing.T)` — TestProjectBoardNeverReachesRequest（T4.2）：带标记的投影不进**任何** provider 请求。

### project_board_wiring_test.go

- `func projectBoardWiringStore(t *testing.T) (router *store.Router, dataRoot, projectRoot string)` — projectBoardWiringStore 建一个临时 JSON 会话存储**与一个临时项目根**。
- `func projectBoardWiringDeps(projectRoot string) ( func(string) []model.WorkItem, func(string) *storeteamwork.Plan, func(string) *storeteamwork.State, func(string) string, )` — projectBoardWiringDeps 造一份最小活体投影：一条 todo + 一个里程碑，外加**落点解析器**。
- `func projectBoardMarkdown(t *testing.T, projectRoot string) string` — projectBoardMarkdown 读回人读件（`BOARD.md`，落在**看板根**、不分会话）：它是
- `func projectBoardArtifacts(t *testing.T, root string) []string` — projectBoardArtifacts 列出投影落点下的全部文件（相对数据根的斜杠路径）。
- `func waitForProjectBoardArtifacts(t *testing.T, root, file string) []string` — waitForProjectBoardArtifacts 轮询等投影落盘（去抖窗是时间驱动的，不能假设立即到）。
- `func TestProjectBoardDisabledInjectsNothing(t *testing.T)` — TestProjectBoardDisabledInjectsNothing：enabled=false ⇒ 零订阅口、零文件（两个根都没有）。
- `func TestProjectBoardRejectsUnknownHook(t *testing.T)` — TestProjectBoardRejectsUnknownHook：拼错的档名显式失败（装配根因此启动失败）。
- `func TestProjectBoardEnabledWritesArtifacts(t *testing.T)` — TestProjectBoardEnabledWritesArtifacts：开着时命中触发面才落盘；落点在**项目根**的
- `func (*projectBoardWiringError) Error() string`
- `func containsArtifact(artifacts []string, name string) bool`
- `func readProjectBoardArtifact(t *testing.T, root, name string) string` — readProjectBoardArtifact 读回落点文件正文（追加式 JSONL 的原始字节）。

### project_board_write_test.go

- `func TestProjectBoardWriteEndToEndFromTodoTool(t *testing.T)`
- `func waitForProjectBoardMarkdown(t *testing.T, projectRoot, needle string) string` — waitForProjectBoardMarkdown 等 `BOARD.md` 落到含 needle 的那一版（超时报当前目录内容）。
- `func projectBoardDirEntries(t *testing.T, projectRoot string) []string` — projectBoardDirEntries 列出看板落点目录的文件名（诊断用）。
