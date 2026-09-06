# core/context

## 生态位

上下文控制相关集成测试

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### context_cache_smoke_test.go

- `func smokeService(t *testing.T, window int) (*Service, *fakeEngine)` — smokeService 构建一个可驱动生产装配路径的服务。
- `func appendSettledRound(service *Service, round int, toolResultToken int)` — appendSettledRound 追加一个"已定稿轮次"的 transcript 事件（user → assistant
- `func systemPromptFor(service *Service) string` — systemPromptFor 取当前 system（与 Prepare 内部使用的同一组装函数）。
- `func serText(label, text string) string` — serText 生成 JSON 前缀友好串行化片段（固定键序、无长度前缀；内容前缀相等
- `func serializeRequest(system string, history []EngineMessage, input string, tools []Tool) string` — serializeRequest 把一次请求（system + history + current input + tools）固化为
- `func lcpBytes(a, b string) int` — lcpBytes 返回两个字节串的最长公共前缀长度。
- `func blockFloor(shared, block int) int` — blockFloor 按缓存块粒度对齐（provider 只命中整块）。
- `func measure(prev, current string, totalTok int) (shared int, ratioRaw, ratio64, ratio1k float64)` — measure 计算本轮与上一轮请求的命中指标（block 粒度按 provider 整块缓存语义
- `func logSmokeRow(t *testing.T, row smokeTraceRow, input string)` — smokeCacheHitRow 汇总输出一行。
- `func TestContextCacheSmoke_SingleSessionLongRun(t *testing.T)` — TestContextCacheSmoke_SingleSessionLongRun 复现"单会话多轮长会话"的最优可达
- `func TestContextCacheSmoke_SkillActivationAppendOnly(t *testing.T)` — TestContextCacheSmoke_SkillActivationAppendOnly 对照场景：会话中段激活一个
- `func TestContextCacheSmoke_ProviderCacheContentionModel(t *testing.T)` — TestContextCacheSmoke_ProviderCacheContentionModel 是一个显式标注的推演模型：

### context_controller_test.go

- `func (runtime runtimeWithContextLimits) ContextWindow() int`
- `func (runtime runtimeWithContextLimits) MaxOutputTokens() int`
- `func TestRejectToolResultsPreservesPairingWithoutPreview(t *testing.T)`
- `func TestPrepareExecutionContextCountsActiveSystemPrompt(t *testing.T)`
- `func TestPrepareExecutionContextUsesRuntimeContextLimits(t *testing.T)`
- `func TestPreparedRequestNeverExceedsSafeBudget(t *testing.T)`
- `func TestPrepareExecutionContextOrderAndNoCheckpoint(t *testing.T)`
- `func TestTranscriptTailDropsIncompleteAndOrphanToolProtocols(t *testing.T)`
- `func TestTranscriptTailKeepsTrailingUnansweredUserInput(t *testing.T)`
- `func TestTranscriptTailAccumulatesAllSettledRoundsWhenUnlimited(t *testing.T)`
- `func TestPrepareExecutionContextAccumulatesAllSettledRoundsAndByteStable(t *testing.T)`
- `func historyContents(history []EngineMessage) []string`
- `func TestRejectToolResultsRecognizesFrameworkTruncationMarker(t *testing.T)`

### context_hook_test.go

- `func TestIterationHookDoesNotTriggerContextControl(t *testing.T)`
