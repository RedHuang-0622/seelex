# core/history（根包分卷）

## 生态位

历史检索与 provider 失败恢复

覆盖：`history*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### history_safety.go

- `func nonEmptyProviderInput(input string) string`
- `func (service *Service) recoverProviderContext(err error, originalRequest string) error` — recoverProviderContext 在 provider 因超出上下文窗口拒绝累积 transcript 后，
- `func (service *Service) recoverProviderFailure(err error, originalRequest string) (bool, error)` — recoverProviderFailure 仅在 provider 拒绝请求后，把不可用 transcript 替换
- `func (service *Service) recoverProviderFailureFor(ctx context.Context, err error, originalRequest string) (bool, error)` — recoverProviderFailureFor 与 recoverProviderFailure 相同，但 ctx 携带会话
- `func (service *Service) retryContextRecovery(ctx context.Context, requestID string, onChunk func(string)) error` — retryContextRecovery 在 provider 于执行前拒绝请求（上下文长度 / 记录不合法）
- `func classifyProviderFailure(err error) providerFailureKind`
- `func retryableAfterRecovery(err error) bool` — retryableAfterRecovery 判定"被 provider 拒绝的这次请求"能否安全地重放一次
- `func providerRecoveryDetails(kind providerFailureKind) (prefix, heading, summary string)`
- `func isProviderContextExhaustion(err error) bool`
- `func (service *Service) removeProviderContextRecovery() error`
- `func (service *Service) removeProviderContextRecoveryFor(sessionID string) error` — removeProviderContextRecoveryFor 清理引擎私有的上下文控制信封：provider

### history_safety_test.go

- `func TestNonEmptyProviderInputExplainsEmptySubmission(t *testing.T)`
- `func TestPrepareProviderHistoryRepairsBeforeChat(t *testing.T)`
- `func TestRecoverProviderContextReplacesRejectedTranscriptWithPrivateCheckpoint(t *testing.T)`
- `func TestRecoverProviderContextIgnoresOtherProviderErrors(t *testing.T)`
- `func TestRecoverProviderTimeoutCreatesPrivateResumeCheckpoint(t *testing.T)`
- `func TestContextExhaustionPersistsInterruptedProjectionAfterBoundedRetryFails(t *testing.T)`
- `func containsRecoveryHistory(history []EngineMessage, prefix string) bool`
- `func TestContextExhaustionReturnsBoundedRecoveryInstructionToAgent(t *testing.T)`
- `func TestEmptyProviderContentRejectionResumesFromBoundedCheckpoint(t *testing.T)` — TestEmptyProviderContentRejectionResumesFromBoundedCheckpoint 覆盖“记录不合法”
- `func TestNonRecoverableProviderFailureMarksTaskFailed(t *testing.T)`
- `func TestIterationRepairsNewlyAddedEmptyToolHistory(t *testing.T)`
- `func TestServerFailuresAreRecoverableWithoutAutomaticReplay(t *testing.T)`
- `func TestToolProtocolRejectionsAreHistoryFailures(t *testing.T)` — TestToolProtocolRejectionsAreHistoryFailures 覆盖 2026-09-20 现场那条 400：
- `func TestInsufficientToolMessagesIsAHistoryFailure(t *testing.T)` — TestInsufficientToolMessagesIsAHistoryFailure 覆盖 tool 配对协议的另一半措辞：
- `func TestRetryableAfterRecoveryOnlyReplaysPreExecutionRejections(t *testing.T)` — TestRetryableAfterRecoveryOnlyReplaysPreExecutionRejections 钉住"什么时候可以
- `func liveSessionLoopToolPairingFailure() error` — liveSessionLoopToolPairingFailure 是 2026-09-28 现场原文：会话循环第一次请求
- `func TestHistoryProtocolFailureResumesInsteadOfKillingTheSession(t *testing.T)` — TestHistoryProtocolFailureResumesInsteadOfKillingTheSession 覆盖现场后果：恢复

### history_search.go

- `func (service *Service) SearchHistory(ctx context.Context, query string, limit int) (seelexctxsearch.Result, error)` — SearchHistory 返回会话历史检索结果（GUI 历史检索面板数据源；query 非空
- `func (service *Service) SearchHistoryHandler(_ context.Context, argsJSON string) (string, error)` — SearchHistoryHandler 实现 search_history 工具：模型在上下文缺少相关历史

### history_search_test.go

- `func (runtime *searchRuntime) SearchHistory(_ context.Context, query string, limit int) (seelexctxsearch.Result, error)`
- `func TestSearchHistoryHandlerReturnsStructuredHits(t *testing.T)`
- `func TestSearchHistoryHandlerRejectsEmptyQuery(t *testing.T)`
- `func TestSearchHistoryHandlerPropagatesRuntimeError(t *testing.T)`
- `func TestSearchHistoryRejectsEmptyQueryAtServiceBoundary(t *testing.T)`
