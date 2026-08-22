# core/context

## 生态位

上下文控制相关集成测试

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### context_controller_test.go

- `func (runtime runtimeWithContextLimits) ContextWindow() int`
- `func (runtime runtimeWithContextLimits) MaxOutputTokens() int`
- `func TestRejectToolResultsPreservesPairingWithoutPreview(t *testing.T)`
- `func TestPrepareExecutionContextCountsActiveSystemPrompt(t *testing.T)`
- `func TestPrepareExecutionContextUsesRuntimeContextLimits(t *testing.T)`
- `func TestPreparedRequestNeverExceedsSafeBudget(t *testing.T)`
- `func TestTranscriptTailDropsIncompleteAndOrphanToolProtocols(t *testing.T)`
- `func TestTranscriptTailKeepsTrailingUnansweredUserInput(t *testing.T)`
- `func TestRejectToolResultsRecognizesFrameworkTruncationMarker(t *testing.T)`

### context_hook_test.go

- `func TestIterationHookDoesNotTriggerContextControl(t *testing.T)`
