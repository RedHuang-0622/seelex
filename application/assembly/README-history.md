# assembly/history（根包分卷）

## 生态位

历史检索与 provider 失败恢复

覆盖：`history*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### history_safety_test.go

- `func nonEmptyProviderInput(input string) string`
- `func (service *Service) recoverProviderContext(err error, originalRequest string) error`
- `func (service *Service) recoverProviderFailure(err error, originalRequest string) (bool, error)`
- `func (service *Service) recoverProviderFailureFor(ctx context.Context, err error, originalRequest string) (bool, error)`
- `func retryableAfterRecovery(err error) bool`
- `func (service *Service) removeProviderContextRecovery() error`
- `func TestNonEmptyProviderInputExplainsEmptySubmission(t *testing.T)`
- `func TestPrepareProviderHistoryRepairsBeforeChat(t *testing.T)`
- `func TestRecoverProviderContextReplacesRejectedTranscriptWithPrivateCheckpoint(t *testing.T)`
- `func TestRecoverProviderContextIgnoresOtherProviderErrors(t *testing.T)`
- `func TestRecoverProviderTimeoutCreatesPrivateResumeCheckpoint(t *testing.T)`
- `func TestContextExhaustionPersistsInterruptedProjectionAfterBoundedRetryFails(t *testing.T)`
- `func containsRecoveryHistory(history []contract.EngineMessage, prefix string) bool`
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

### history_search_test.go

- `func TestHistorySearchApplicationIsAssembled(t *testing.T)`
- `func (runtime *searchRuntime) SearchHistory(_ context.Context, query string, limit int) (seelexctxsearch.Result, error)`
- `func TestSearchHistoryHandlerReturnsStructuredHits(t *testing.T)`
- `func TestSearchHistoryHandlerRejectsEmptyQuery(t *testing.T)`
- `func TestSearchHistoryHandlerPropagatesRuntimeError(t *testing.T)`
- `func TestSearchHistoryRejectsEmptyQueryAtServiceBoundary(t *testing.T)`
