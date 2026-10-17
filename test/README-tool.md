# test/tool（集成用例分卷）

## 生态位

工具面全链：装配基座与上下文读工具、工具记录写路径。

覆盖：`tool_full_chain*.go`；未归属文件由覆盖自检拦下。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### tool_full_chain_live_test.go

- `func TestManualSmokeRealAccountBashFullChain(t *testing.T)` — TestManualSmokeRealAccountBashFullChain is an opt-in paid-provider smoke
- `func TestManualSmokeRealAccountGUIBridgeBashFullChain(t *testing.T)` — TestManualSmokeRealAccountGUIBridgeBashFullChain is the opt-in real API
- `func TestManualSmokeRealAccountTodoFullChain(t *testing.T)` — TestManualSmokeRealAccountTodoFullChain reproduces the first internal tool

### tool_full_chain_test.go

- `func requireGitBash(t *testing.T)` — requireGitBash 断言环境提供真实 bash（git-bash 或非 WSL 的 PATH bash）。
- `func TestFullAccessBashToolCompletionReachesApplication(t *testing.T)` — TestFullAccessBashToolCompletionReachesApplication exercises the production
- `func TestFullAccessUnboundBashFailureReachesApplication(t *testing.T)` — TestFullAccessUnboundBashFailureReachesApplication keeps project scope
- `func newFullChainHarness(t *testing.T, AccountsPath, projectRoot string, toolTimeout time.Duration) fullChainHarness`
- `func newUnboundFullChainHarness(t *testing.T, AccountsPath, projectRoot string, toolTimeout time.Duration) fullChainHarness`
- `func newFullChainHarnessWithProjectBinding(t *testing.T, AccountsPath, projectRoot string, toolTimeout time.Duration, bindProject bool) fullChainHarness`
- `func newFullChainHarnessWithLimits(t *testing.T, AccountsPath, projectRoot string, toolTimeout time.Duration, bindProject bool, limits limit.Limits, installers ...fullChainBackendInstaller) fullChainHarness` — newFullChainHarnessWithLimits 是同一套装配，只多交出 limits 段：A/B 两臂要求
- `func newBashToolChainServer(t *testing.T) *bashToolChainServer`
- `func (server *bashToolChainServer) serve(t *testing.T, writer http.ResponseWriter, request *http.Request)`
- `func writeSSE(t *testing.T, writer http.ResponseWriter, flusher http.Flusher, payload any)`
- `func waitForToolCompleted(t *testing.T, ctx context.Context, events <-chan application.Event, name string) application.Event`
- `func waitForToolStatus(t *testing.T, ctx context.Context, events <-chan application.Event, name, status string) application.Event`
- `func allGoroutineStacks() string`
- `func providerMessagesContainToolResult(messages []struct { Role string `json:"role"` Content any `json:"content"` ToolCallID string `json:"tool_call_id"` }, toolCallID, contentPart string) bool`
- `func conversationContainsAssistant(messages []application.Message, content string) bool`
