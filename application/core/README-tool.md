# core/tool

## 生态位

工具事件钩子与诊断

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### tool_hook_diagnostic_test.go

- `func TestToolHookDiagnosticObserverMarksCompletionProjection(t *testing.T)`

### tool_hooks.go

- `func (service *Service) handleToolStart(ctx context.Context, name, id, arguments string)`
- `func (service *Service) planBranchBindingLocked() dto.PlanBranchBinding`
- `func (service *Service) handleToolComplete(name, id, result string, toolErr error, duration time.Duration)`
- `func (service *Service) handleToolCompleteObserved(ctx context.Context, name, id, arguments, result string, toolErr error, duration time.Duration, observe func(string))` — handleToolCompleteObserved 把生产完成投影保持在一处，同时允许 ToolHookBridge
- `func NewToolHookBridge() *ToolHookBridge`
- `func (bridge *ToolHookBridge) Bind(service *Service)`
- `func (bridge *ToolHookBridge) SetDiagnosticObserver(observer ToolHookDiagnosticObserver)` — SetDiagnosticObserver 安装可选、尽力而为的生命周期诊断。传入 nil 关闭。
- `func (bridge *ToolHookBridge) observeDiagnostic(event ToolHookDiagnosticEvent)`
- `func (bridge *ToolHookBridge) Hooks() *session.LoopHooks`
- `func normalizePlanToolCallInfo(info session.ToolCallInfo) session.ToolCallInfo` — normalizePlanToolCallInfo 保持应用快照与 Seele 执行的同一规范 DAG 表示。
- `func (bridge *ToolHookBridge) beginTool(info session.ToolCallInfo) (*Service, string)`
- `func (bridge *ToolHookBridge) completeTool(info session.ToolCallInfo) (*Service, string)`
- `func (bridge *ToolHookBridge) nextToolIDLocked() string`
- `func toolHookKey(info session.ToolCallInfo) string`

### tool_hooks_truncation_test.go

- `func TestToolCompleteTruncatesSnapshotOutput(t *testing.T)` — TestToolCompleteTruncatesSnapshotOutput 验证实时截断链端到端：大工具
- `func TestAppendHistoryLockedTruncatesRestoredOutput(t *testing.T)` — TestAppendHistoryLockedTruncatesRestoredOutput 验证会话恢复路径：恢复的
