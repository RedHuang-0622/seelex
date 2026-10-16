# test/live（集成用例分卷）

## 生态位

真 API / 手工冒烟族：由 build tag 与环境变量双开关，缺凭证即 skip（默认集合不红）。

覆盖：`async_*.go`、`compact_live*.go`、`computer_use*.go`、`context_long*.go`、`job_backfill*.go`、`job_live*.go`、`manual_smoke*.go`、`prefix_chain*.go`、`real_api_*.go`、`scheduled_task*.go`、`smoke_*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### async_exec_ab_live_test.go

- `func abConfigFromEnv() abConfig`
- `func (s abSample) utilization() float64` — utilization = 兑现率：真实命中 ÷ 可命中上限。跨臂单位一致，所以两臂可直接比。
- `func (s abSample) ratio() float64`
- `func abPrompt(arm, taskID, command, sentinel string) string` — abPrompt 生成任务指令。两臂的任务、素材、提问完全一致，只差执行形状——
- `func abMessagesSpan(body []byte) []byte` — abMessagesSpan 取出请求体里 "messages" 数组的**元素区**字节（去掉外层方括号）。
- `func abCommonPrefixBytes(left, right []byte) int` — abCommonPrefixBytes 返回两段字节的公共前缀长度（探针的结构判据）。
- `func abRunTurn(t *testing.T, accountsSource, upstream, taskID, arm string, rep, seconds int, nonce string) abSample` — abRunTurn 跑一次完整回合并采集五项指标。每个样本一个独立 projectRoot/harness/代理，
- `func abAsyncReceipts(messages []prefixLiveMessage) int` — abAsyncReceipts 数一条请求里有几条 tool 消息的正文是异步载荷（解出来含 handle）。
- `func abAsyncTempFootprint() (int, int64)` — abAsyncTempFootprint 统计后台输出目录的个数与总字节：这是"目录有界"的实测面。
- `func abSleepProcesses() int` — abSleepProcesses 数一下当前存活的 sleep.exe（git-bash 的样本命令就是它）。
- `func abEfficiencyTask(taskID string) bool` — abEfficiencyTask 标出"用来比墙钟与总 prompt"的任务集合。T4 不在其中（它的回合形状
- `func abMedian(values []int64) int64`
- `func abOK(arm, task string, samples []abSample) []abSample`
- `func abWallMS(s []abSample) []int64`
- `func abPromptTotals(s []abSample) []int64`
- `func abRatioSum(s []abSample) (float64, int, int)`
- `func abJudge(cfg abConfig, samples []abSample) ([]abGate, []string)` — abJudge 按 §8.3 事先写死的门槛算五条判据。
- `func abScopeNote(applicable bool, detail string) string` — abScopeNote 在"本轮没跑效率任务"时把判据标成不适用（校准轮只跑 T4 是这种情形）。
- `func abPercent(a, b int64) float64`
- `func abBit(value bool) int` — abBit 把布尔判据打成 1/0，便于逐轮表里直接数破坏次数。
- `func abReportPath() string`
- `func abWriteReport(t *testing.T, cfg abConfig, samples []abSample, gates []abGate, notes []string) string`
- `func abFailureSuffix(sample abSample) string`
- `func abVerdict(pass bool) string`
- `func abHardMark(hard bool) string`
- `func TestManualSmokeAsyncAB(t *testing.T)`

### async_tool_wire_live_probe_test.go

- `func loadProbeAccount(t *testing.T) probeAccountEntry` — loadProbeAccount 读账号副本，优先 agent[0]，回退 subagent[0]。
- `func hostOf(raw string) string`
- `func user(content string) wireMessage`
- `func tool(callID, content string) wireMessage`
- `func assist(content string) wireMessage`
- `func requestWithToolCall(id, name, args string) wireMessage`
- `func stableFiller(tampered bool) string` — stableFiller 是确定性长填充：provider 侧前缀缓存通常要求共享前缀 ≥1024 token，
- `func pollRound(callID, args, result string) []wireMessage` — pollRound 是一对完整的取回：assistant 发起 bg_manage(op=fetch) 调用 + 紧邻其后的 tool 回执。
- `func asyncCompletionMessage() string` — asyncCompletionMessage 是 A 链路的补记：role=user、自带 call_id 信封、正文只给摘要+句柄。
- `func postJSON(ctx context.Context, t *testing.T, account probeAccountEntry, endpoint string, body map[string]any, label string) probeResult` — postJSON 发一次非流式请求，返回状态、usage 与错误片段（脱敏）。
- `func usageDump(usage map[string]any) string`
- `func readCacheCounters(usage map[string]any) (cached int, prompt int)` — readCacheCounters 兼容各家 OpenAI 兼容层的缓存字段命名。
- `func intOf(value any) int`
- `func redact(text, secret string) string`
- `func TestAsyncWireLiveProbe(t *testing.T)`
- `func probeResponsesAPI(ctx context.Context, t *testing.T, account probeAccountEntry, endpoint string)` — probeResponsesAPI 测有状态链上「迟到的 function_call_output」是否被接受。
- `func completionsEndpoint(base string) string` — completionsEndpoint 把 base_url 归一到 …/v1/chat/completions。
- `func responsesEndpointFor(base string) (string, bool)`
- `func joinEndpoint(base, suffix string) string`

### compact_live_smoke_test.go

- `func TestCompactLivePreflight(t *testing.T)` — TestCompactLivePreflight 是 compact 冒烟的前置探针：只做一次极小的真实回合，
- `func TestCompactLiveSmoke(t *testing.T)`
- `func compactSmokePiece(round, part int) string` — compactSmokePiece 生成**一小片**材料（第 round 轮第 part 片，带可检索的片号）。
- `func compactRecords(snapshot model.Snapshot) []model.ContextCompaction`
- `func lastAssistantContent(snapshot model.Snapshot) string`
- `func lastSystemNotice(conversation []model.Message) string` — lastSystemNotice 取最后一条 system 角色提示（命令结果的可见回执经
- `func describeConversationTail(conversation []model.Message, limit int) string` — describeConversationTail 汇总对话末尾若干条（role: 摘要），失败信息里用。
- `func truncateForLog(value string) string`
- `func copyAccountsOpaqueForCompact(t *testing.T, source, destination string)`
- `func rewriteAccountsContextLimits(t *testing.T, path string, window, maxTokens int)` — rewriteAccountsContextLimits 把账号副本的上下文预算调小（只动 t.TempDir() 的

### computer_use_live_smoke_test.go

- `func TestComputerUseLiveSmoke(t *testing.T)`
- `func describeStoreTree(t *testing.T, root string) string` — describeStoreTree 列出临时目录下与媒体相关的路径（诊断：定位媒体实际落点）。
- `func visibleToolNames(tools []model.Tool) []string` — visibleToolNames 列出本回合模型可见的工具名（诊断用：先排除"工具没进可见面"）。
- `func describeTail(conversation []model.Message, limit int) string` — describeTail 摘出末尾几条可见消息（角色 + 首行截断），失败诊断用，不含工具原始结果。
- `func copyAccountsOpaque(t *testing.T, source, destination string)` — copyAccountsOpaque 原样复制账号配置（不解析、不打印内容；与其它冒烟同一口径）。
- `func screenshotMediaRefs(conversation []model.Message) []string` — screenshotMediaRefs 从会话记录里收集 computer_screenshot 工具结果中的
- `func mediaRefOnDisk(t *testing.T, storeRoot, ref string) bool` — mediaRefOnDisk 校验媒体引用在会话存储里真实落盘：按内容 hash 目录定位
- `func latestVisibleAssistant(conversation []model.Message) string` — latestVisibleAssistant 取最后一条非空 assistant 正文。
- `func normalizeTitleText(value string) string` — normalizeTitleText 压缩空白与标点（标题里的连接符/空格在不同渲染下可能不同）。
- `func longestTitleRun(title string) string` — longestTitleRun 取标题里最长的连续非空白片段（≥6 字符）作为地面真值标记，
- `func (r *providerRecording) summary() (withImage, plain, maxImageBytes int)`
- `func (r *providerRecording) record(body []byte)`
- `func newProviderRecordingProxy(t *testing.T, targetBase string) (*providerRecording, *httptest.Server)` — newProviderRecordingProxy 起本地反向代理：转发到 targetBase 并记录随图情况。
- `func readAccountsBaseURL(t *testing.T, path string) string` — readAccountsBaseURL 取账号配置里第一个 base_url（仅作转发目标，不触碰其它字段）。
- `func rewriteAccountsBaseURL(t *testing.T, path, original, replacement string)` — rewriteAccountsBaseURL 只把账号副本里的 base_url 字段改写为记录代理地址。
- `func readAccountsFileText(t *testing.T, path string) string`

### context_long_live_test.go

- `func TestManualSmokeRealAccountLongContextRehydration(t *testing.T)` — TestManualSmokeRealAccountLongContextRehydration uses the real provider to
- `func snapshotAfterResume(app *application.Service) application.Snapshot`
- `func latestVisibleAssistant(snapshot application.Snapshot) string`

### job_backfill_live_smoke_test.go

- `func TestManualSmokeRealAccountJobBackfillWithoutManage(t *testing.T)` — TestManualSmokeRealAccountJobBackfillWithoutManage 见文件头注。
- `func newBackfillCollector(subscription application.Subscription) *backfillCollector`
- `func (c *backfillCollector) take() []liveToolCall` — take 取走本轮的调用记录并清空（轮次边界由 submit/WaitForIdle 划定）。
- `func waitForAsyncTerminal(t *testing.T, runtime *seelebridge.Runtime, handle string, budget time.Duration) dto.AsyncRunRecord` — waitForAsyncTerminal 轮询登记表直到该句柄落到终态。**不调用任何工具**：这条路径
- `func asyncHandlePresent(runtime *seelebridge.Runtime, handle string) bool` — asyncHandlePresent 报告句柄是否仍在登记表里。
- `func waitForAsyncWorkRow(app *application.Service, handle string, budget time.Duration) string` — waitForAsyncWorkRow 等投影发布器把 async:<handle> 行推出来（发布是异步的），

### job_live_smoke_test.go

- `func liveLongCommand() string` — liveLongCommand 给出一条"跑几分钟、每秒钟产出一行"的命令。
- `func TestManualSmokeRealAccountJobContract(t *testing.T)` — TestManualSmokeRealAccountJobContract 是**真实 API** 上的"子进程调用系"冒烟：
- `func jobHandleFromCalls(t *testing.T, calls []liveToolCall) string` — jobHandleFromCalls 从本轮真实调用里取出 bash_bg 的受理回执句柄。

### manual_smoke_test.go

- `func TestManualSmokeRealAccountPlan(t *testing.T)` — TestManualSmokeRealAccountPlan verifies the real account path without
- `func newSmokeObserver(hub *application.EventHub) *smokeObserver`
- `func (o *smokeObserver) consume()`
- `func (o *smokeObserver) record(format string, args ...interface{})`
- `func (o *smokeObserver) Close()` — Close 停止订阅并等待消费者退出（阶段结束时调用）。
- `func (o *smokeObserver) Dump(t *testing.T, phase string)` — Dump 输出本阶段的过程日志（失败诊断与断言引用）。
- `func smokePhaseContext() (context.Context, context.CancelFunc)`
- `func truncateSmokeReply(value string, limit int) string`
- `func copyOpaqueFile(t *testing.T, source, destination string)`
- `func workspaceSmokeRoot() string` — workspaceSmokeRoot 返回冒烟测试的项目根：优先仓库根（scoped 工具可读

### prefix_chain_live_test.go

- `func applyRetainFloorPercent(percent int)` — applyRetainFloorPercent 设置 limits.context_retain_floor_percent（进程内生效配置：
- `func prefixLiveParseMetadata(t *testing.T, stage, body string) prefixLiveMetadata` — prefixLiveParseMetadata 从帧正文抽出 ```json 元数据块并解析。抽不出/解析不了
- `func TestPrefixChainRetainFloorLiveSmoke(t *testing.T)`
- `func prefixLiveCompactAndReadFrame( t *testing.T, harness fullChainHarness, submit func(stage, prompt string) model.Snapshot, ctx context.Context, stage string, ) prefixLiveFrame` — prefixLiveCompactAndReadFrame 走显式 /compact 压缩一次并回读帧正文。
- `func subscribePrefixLiveProgress(hub *application.EventHub) *prefixLiveProgress` — subscribePrefixLiveProgress 订阅压缩门禁打点（compaction.progress），按会话外的
- `func (s *prefixLiveProgress) consume()`
- `func (s *prefixLiveProgress) Snapshot() []application.CompactionProgress` — Snapshot 返回已收到的全部门禁载荷（只到 Close 为止）。
- `func (s *prefixLiveProgress) Close()` — Close 停止订阅并等待消费者退出。

### real_api_permission_smoke_test.go

- `func openApprovalPage(broker *application.ApprovalBroker) *approvalPage` — openApprovalPage 起一个审批页面观察者。必须在提交回合**之前**调用：页面是
- `func (p *approvalPage) note(id, tool string)`
- `func (p *approvalPage) snapshot() []string` — snapshot 返回本次统计窗口内打开的页面（工具名，按打开顺序）。
- `func (p *approvalPage) reset()` — reset 清空统计窗口（页面开合本身不再重放，只影响计数）。
- `func (p *approvalPage) Close()`
- `func TestRealAPIPermissionSmoke(t *testing.T)`
- `func toolEvents(harness fullChainHarness) *toolEventRecorder` — toolEvents 订阅应用事件面，返回"排空当前事件并抽取工具名"的读取函数：
- `func (r *toolEventRecorder) ran(tool string) bool` — ran 报告本回合是否出现过该工具的成功完成事件（真实执行面证据）。
- `func (r *toolEventRecorder) Close()`

### real_api_prefix_live_test.go

- `func newPrefixLiveProxy(t *testing.T, upstreamBase string) *prefixLiveProxy`
- `func (proxy *prefixLiveProxy) serve(writer http.ResponseWriter, request *http.Request)`
- `func (proxy *prefixLiveProxy) reserve() int`
- `func (proxy *prefixLiveProxy) snapshot() []*prefixLiveRecord`
- `func prefixLiveWithIncludeUsage(body []byte) []byte` — prefixLiveWithIncludeUsage 在流式请求体里补 stream_options.include_usage，
- `func prefixLiveUsageFromEventStream(raw []byte) *prefixLiveUsage` — prefixLiveUsageFromEventStream 从原始响应字节里取**最后一个**非空 usage
- `func (message prefixLiveMessage) tag() string` — tag 生成紧凑的消息指纹：role(+tc 表示携带工具调用)(内容字节数)。
- `func prefixLiveMessages(t *testing.T, body []byte) []prefixLiveMessage`
- `func prefixLiveContentText(raw json.RawMessage) string`
- `func prefixLiveCompact(raw json.RawMessage) string`
- `func prefixLiveGovernedStability(prev, cur []prefixLiveMessage) []string` — prefixLiveGovernedStability 报告本次修复管辖的两类消息的跨轮字节稳定性：
- `func prefixLiveToolWireFindings(t *testing.T, records []*prefixLiveRecord) (int, []string)` — prefixLiveToolWireFindings 报告 wire 上「工具失败结果」的条数与**应用呈现文本
- `func prefixLiveClip(text string, limit int) string`
- `func prefixLiveAnalyze(t *testing.T, records []*prefixLiveRecord) ([]prefixLivePair, []string)`
- `func prefixLiveReportPath() string`
- `func prefixLiveWriteReport(t *testing.T, sessionID string, records []*prefixLiveRecord, pairs []prefixLivePair, violations []string, failedTools int, presentedLeaks []string) string`
- `func TestManualSmokeRealAccountPrefixInvariant(t *testing.T)`
- `func prefixLiveUpstreamBaseURL(t *testing.T, accounts []byte) string`

### real_api_smoke_test.go

- `func TestRealAPISessionRestoreSmoke(t *testing.T)`
- `func (harness *fullChainHarness) submitLive(ctx context.Context, t *testing.T, prompt string)`
- `func latestVisibleAssistantManual(snapshot application.Snapshot) string`
- `func copyOpaqueFileManual(t *testing.T, source, dest string)`

### scheduled_task_live_smoke_test.go

- `func TestManualSmokeRealAccountScheduledPromptChain(t *testing.T)` — TestManualSmokeRealAccountScheduledPromptChain 是**真实 API** 上的定时任务链路
- `func waitForConversationMarker(t *testing.T, ctx context.Context, bridge *gui.Bridge, sessionID, marker string, budget time.Duration) string` — waitForConversationMarker 等到**指定会话**里出现标记（提示词原文或助手回复），
- `func sessionConversationText(snapshot application.SessionSnapshot) string` — sessionConversationText 把会话快照的可见会话拼成一段文本（判据只看"出现过没有"）。
- `func scheduledTaskByID(tasks []seelebridge.ScheduledTaskStatus, id string) *seelebridge.ScheduledTaskStatus`
- `func scheduledTaskVisible(tasks []seelebridge.ScheduledTaskStatus, id string) bool`
- `func tailLine(text string) string` — tailLine 取最后一行非空内容（日志用，避免把整段会话刷进测试输出）。

### smoke_test.go

- `func TestSmokeLLM(t *testing.T)`
