# core/error（根包分卷）

## 生态位

错误码与面向用户的错误呈现

覆盖：`error*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### error_codes.go

- `func wrapError(err error, code string) error` — wrapError 给错误附加结构化上下文（seeleerrors.Wrap 语义）：
- `func structuredErrorCode(err error) string` — structuredErrorCode 返回错误链上的结构化错误码（seeleerrors.From）；
- `func classifyStructuredError(err error) (presentedError, bool)` — classifyStructuredError 按结构化定位字段（Function/Step/Path）推断分类
- `func persistenceFailurePresentation() presentedError`
- `func planPreflightPresentation() presentedError`
- `func reactBudgetPresentation() presentedError`
- `func resultRefUnavailablePresentation() presentedError` — resultRefUnavailablePresentation 是 read_tool_result 结果引用不可读的
- `func contextExhaustedPresentation() presentedError`
- `func unclassifiedPresentation() presentedError`

### error_presentation.go

- `func (presentation presentedError) String() string`
- `func presentUserError(err error) string`
- `func isUnclassifiedRunChatError(err error) bool`
- `func classifyPresentedError(err error) presentedError`
- `func presentToolError(toolName string, err error) string`
- `func safeToolName(name string) string`

### error_presentation_test.go

- `func (failingSnapshotSessions) SaveSessionSnapshot(string, []EngineMessage, SessionRecord, []TranscriptEvent, []StoredToolResult) error`
- `func (failingSnapshotSessions) SaveSessionSnapshotWorkspace(string, string, []EngineMessage, SessionRecord, []TranscriptEvent, []StoredToolResult) error`
- `func TestPresentUserErrorHidesProviderDetailsAndIdentifiesSource(t *testing.T)`
- `func TestClassifyStructuredErrorsByCode(t *testing.T)` — TestClassifyStructuredErrorsByCode 验证 slice 8 的结构化错误分类
- `func TestRunChatAndToolProjectionUsePresentedErrors(t *testing.T)`
- `func TestRunChatLogsRawUnclassifiedError(t *testing.T)`
- `func TestPersistenceFailureDoesNotClaimProgressWasSaved(t *testing.T)`
- `func conversationContains(conversation []Message, role, fragment string) bool`
