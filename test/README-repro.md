# test/repro（集成用例分卷）

## 生态位

会话/工具链回归复现：用户现象 → 会红的断言（含多会话并发与热切场景）。

覆盖：`repro_*.go` + 显式名单（见生成器 `TEST_VOLUMES`）；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### prefix_invariant_fullchain_test.go

- `func (message fullChainPrefixMessage) tag() string`
- `func fullChainPrefixMessages(t *testing.T, body []byte) []fullChainPrefixMessage`
- `func newFullChainPrefixServer(t *testing.T, tool, path string) *fullChainPrefixServer`
- `func (server *fullChainPrefixServer) recorded() [][]byte`
- `func (server *fullChainPrefixServer) serve(t *testing.T, writer http.ResponseWriter, request *http.Request)`
- `func TestFullChainPrefixInvariantAcrossTurns(t *testing.T)`
- `func fullChainAssistantToolContent(t *testing.T, body []byte, toolCallID string) (string, bool)` — fullChainAssistantToolContent 返回指定请求里带 tool_calls 的 assistant content。
- `func assertFullChainPrefixInvariant(t *testing.T, bodies [][]byte)` — assertFullChainPrefixInvariant 断言相邻两次 provider 请求保持消息前缀关系，
- `func fullChainWireContent(t *testing.T, body []byte, toolCallID string) (string, bool)` — fullChainWireContent 返回第 index 次（1 起）provider 请求里指定 tool_call_id
- `func TestFullChainPrefixInvariantToolErrorAcrossTurns(t *testing.T)` — TestFullChainPrefixInvariantToolErrorAcrossTurns 覆盖第二类跨轮「事后改写」：
- `func TestFullChainPrefixInvariantOversizedToolResultAcrossTurns(t *testing.T)` — TestFullChainPrefixInvariantOversizedToolResultAcrossTurns 覆盖同族的第三处
- `func clipForFailure(text string, limit int) string`

### repro_chain_ab_test.go

- `func runChainSwitchScenario(t *testing.T) chainABResult`
- `func (result *chainABResult) normalizeSessionIDs(ordinal map[string]string)` — normalizeSessionIDs 用事件指纹的序数映射归一化 activeID/sessions，消除
- `func TestChainSwitchABStable(t *testing.T)` — TestChainSwitchABStable 是 A/B 全链路对比：两遍同脚本行为一致且无死锁。
- `func sessionIDs(sessions []application.SessionInfo) []string`
- `func drainChainFingerprint(t *testing.T, sub application.Subscription) ([]string, map[string]string)` — drainChainFingerprint 把订阅事件归一化为指纹（kind + session/request/
- `func chainMessageID(raw json.RawMessage) string`

### repro_hot_attach_background_tool_order_test.go

- `func newToolInlineHarness(t *testing.T, AccountsPath, projectRoot string, toolTimeout time.Duration) fullChainHarness` — newToolInlineHarness 复制 full-chain harness 装配，并额外注册一个必然
- `func newToolOrderGateProvider(blocked ...int) *toolOrderGateProvider`
- `func (p *toolOrderGateProvider) waitBlocked(t *testing.T, ctx context.Context, number int)`
- `func (p *toolOrderGateProvider) releaseNumber(number int)`
- `func (p *toolOrderGateProvider) serve(t *testing.T, writer http.ResponseWriter, request *http.Request)`
- `func TestHotAttachBackgroundToolRoundKeepsToolBeforeFinalLLM(t *testing.T)`
- `func formatConversation(conversation []application.Message) string`
- `func toolOrderIssue(conversation []application.Message) string` — toolOrderIssue 返回顺序问题的描述；顺序正确（user → tool → tool_result →

### repro_multi_round_llm_preserved_test.go

- `func (p *multiRoundLLMProvider) serve(t *testing.T, writer http.ResponseWriter, request *http.Request)`
- `func visibleAssistantTexts(conversation []application.Message) []string`
- `func TestMultiRoundLLMPreservedAfterHotAndColdResume(t *testing.T)`

### repro_queued_input_switch_promotion_test.go

- `func TestReproQueuedInputThenSwitchSessions(t *testing.T)`
- `func containsAll(text string, needles ...string) bool`

### repro_second_round_engine_released_test.go

- `func (p *scriptedProvider) serve(t *testing.T, writer http.ResponseWriter, request *http.Request)`
- `func (p *scriptedProvider) count() int`
- `func errorMessages(conversation []application.Message) []string`
- `func toolResultTexts(conversation []application.Message) []string`
- `func newScriptedHarness(t *testing.T, responses []scriptedResponse) (*fullChainHarness, *scriptedProvider)`
- `func submitAndWait(t *testing.T, harness *fullChainHarness, ctx context.Context, text string)`
- `func TestSecondRoundStartsAfterSessionEngineReleased(t *testing.T)` — 情形 A：第一轮结束后会话引擎被释放（驻留 LRU 驱逐 / 空闲后未挂载），用户
- `func TestSecondRoundStartsAfterTerminalDecisionTool(t *testing.T)` — 情形 B：第一轮以终态工具 task_needs_user_decision 收口（任务停在

### repro_session_background_test.go

- `func startBackgroundCompletionScenario(t *testing.T, bindProject bool) *backgroundScenario` — startBackgroundCompletionScenario 执行与 repro_session_disappear_test 相同
- `func loadStoredRecord(t *testing.T, st *store.Router, workspaceID, sessionID string) (application.SessionRecord, bool)` — loadStoredRecord 读取 workspace 下会话的持久化 record。
- `func recordConversationTexts(record application.SessionRecord) []string`
- `func TestBackgroundCompletionPreservesARecordDomains(t *testing.T)` — TC-A1-02：A 后台完成落盘后，record 的 Title/Conversation/transcript/Tasks
- `func newBlockingProvider(blockedNumbers ...int) *blockingProvider`
- `func (p *blockingProvider) serve(t *testing.T, writer http.ResponseWriter, request *http.Request)`
- `func TestBackgroundCompletionKeepsActiveSessionQueue(t *testing.T)` — TC-A1-03：A 后台完成收尾不得影响活跃会话 B 的队列与运行态。
- `func TestBackgroundCompletionWhileSwitchingToC(t *testing.T)` — TC-A2-01：A 收尾与 C 运行并发（真实交错）；A 的 ReleaseWorkingHistoryFor
- `func dumpBackgroundTurnStall(t *testing.T, harness fullChainHarness, reason string)` — dumpBackgroundTurnStall 在"C 的第二轮没到 provider"时留一份现场：快照读数 +

### repro_session_disappear_test.go

- `func newRequestCountingProvider() *requestCountingProvider`
- `func (p *requestCountingProvider) serve(t *testing.T, writer http.ResponseWriter, request *http.Request)`
- `func conversationTexts(conversation []application.Message) []string`
- `func containsUserText(conversation []application.Message, needle string) bool`
- `func TestBackgroundSessionCompletionMustNotPolluteOwner(t *testing.T)` — TestBackgroundSessionCompletionMustNotPolluteOwner 复现：A 运行中切到 B，

### repro_session_race_test.go

- `func TestWorkspaceSwitchConcurrentWithBackgroundPersist(t *testing.T)` — TestWorkspaceSwitchConcurrentWithBackgroundPersist（TC-R-01）：A 后台完成

### repro_session_switch_real_test.go

- `func newBlockingAfterFirstProvider() *blockingAfterFirstProvider`
- `func (p *blockingAfterFirstProvider) serve(t *testing.T, writer http.ResponseWriter, request *http.Request)`
- `func waitReal(t *testing.T, done <-chan error, what string) error`
- `func TestRealSwitchWhileChattingRepro(t *testing.T)` — TestRealSwitchWhileChattingRepro 场景 1：会话 A 运行中（provider 阻塞）恢复会话 B。
- `func TestRealBeginNewWhileChattingRepro(t *testing.T)` — TestRealBeginNewWhileChattingRepro 场景 2：会话 A 运行中新建会话。
- `func TestRealNewSessionDraftRetainedOnSwitchRepro(t *testing.T)` — TestRealNewSessionDraftRetainedOnSwitchRepro 验证“新建会话（草稿）→ 切换会话”

### repro_session_workspace_test.go

- `func startCrossWorkspaceScenario(t *testing.T) (harness fullChainHarness, sessionA, sessionB, wsX, wsY string, st *store.Router)` — startCrossWorkspaceScenario：A 绑定 workspace X；切到 workspace Y 建 B；
- `func TestBackgroundPersistUsesSessionWorkspaceKey(t *testing.T)` — TC-A4-01/02：A 的 record 落在自身 workspace X；Y 下无 A 键；Router 写作用域
- `func TestBackgroundPersistDoesNotMutateWriteScope(t *testing.T)` — TC-A4-02：后台完成落盘不改变当前 Router 写作用域（Snapshot.CurrentWorkspace
- `func TestNoPhantomSessionInForeignWorkspace(t *testing.T)` — TC-A4-03：workspace Y 的 catalog 不含 A 会话条目（无幽灵会话）。
- `func TestForkDoesNotShiftOtherSessionPersistKey(t *testing.T)` — TC-A5-02：fork 不改变其它会话落盘键（fork 后 C 仍落自身 workspace）。

### repro_three_sessions_running_switch_test.go

- `func newBlockingFromRequestNProvider(quickCount int) *blockingFromRequestNProvider`
- `func (p *blockingFromRequestNProvider) serve(t *testing.T, writer http.ResponseWriter, request *http.Request)`
- `func (p *blockingFromRequestNProvider) releaseAllNow()`
- `func TestThreeRunningSessionsSwitchDoesNotWaitAndKeepsContext(t *testing.T)`

### repro_two_running_view_third_then_switched_finishes_test.go

- `func newPerRequestGateProvider(quick int) *perRequestGateProvider`
- `func (p *perRequestGateProvider) serve(t *testing.T, writer http.ResponseWriter, request *http.Request)`
- `func (p *perRequestGateProvider) release(number int)`
- `func (p *perRequestGateProvider) releaseAll()`
- `func (p *perRequestGateProvider) debug() string`
- `func (p *perRequestGateProvider) waitBlocked(t *testing.T, ctx context.Context, number int)`
- `func runTwoRunningViewThirdScenario(t *testing.T, cold bool)` — runTwoRunningViewThirdScenario 驱动一轮完整时序；cold 变体把 C 卸载为冷。
- `func conversationText(messages []application.Message) string`
- `func waitSessionIdle(t *testing.T, ctx context.Context, harness fullChainHarness, sid string)`
- `func stillRunning(t *testing.T, ctx context.Context, harness fullChainHarness, sid string) bool`
- `func TestTwoRunningViewThirdThenSwitchedFinishesHot(t *testing.T)`
- `func TestTwoRunningViewThirdThenSwitchedFinishesCold(t *testing.T)`
- `func TestTwoRunningViewThirdSwitchStress(t *testing.T)` — TestTwoRunningViewThirdSwitchStress 把同一时序高频重复，配合 -race 放大
- `func TestSwitchToIdleThirdBlocksWhileAliasEngineSessionRuns(t *testing.T)` — TestSwitchToIdleThirdBlocksWhileAliasEngineSessionRuns 复现用户报告的
