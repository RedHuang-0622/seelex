# core/tool（根包分卷）

## 生态位

工具事件钩子与诊断

覆盖：`tool*.go`；未归属文件由覆盖自检拦下。

## 实现要点

- 工具完成投影只有一条生产路径：`handleToolCompleteObserved`（`handleToolComplete`
  是 `observe=nil` 的薄包装）。可选诊断观察者（`ToolHookBridge.SetDiagnosticObserver`）
  只能**包围**阻塞边界（`toolhook.complete.lock.start/done` 等阶段打点），不改投影
  顺序、不带工具入参/输出；诊断事件是 metadata-only（`ToolHookDiagnosticEvent`）。
- 工具启动/完成按 `(turn, name, arguments)` 键在 `ToolHookBridge.pending` 里 FIFO
  配对：完成侧匹配不到在途 ID 时就地分配新 ID，事件不丢、不串会话。
  边界（未修，待独立改动）：该键**不含会话**，两个会话在同 turn 调同名同参工具时，
  完成侧可能取到另一会话的在途 ID，导致两侧视图的工具块都停在 `running`（不产生
  跨会话正文，但状态不落地）。见研究文档 §7.9「剩余边界」。
- 工具启动边界（`handleToolStart`）在 flush 流式缓冲、宣告事件就位后，把本轮**说明
  正文按迭代归位**到该工具调用的 assistant(tool_calls) 事件
  （`task_context.AttributeToolNarrationLocked`）：wire 上工具轮正文恒为空
  （Seele `session/loop.go:564`），说明文本只经 `onChunk` 进视图，不回填则
  record / 重启恢复只剩工具痕迹；归位必须按迭代增量做——回合收尾的事后填充会把
  叙述写到别的轮次（2026-09-12 修复）。目标调用 ID 必须按 `(name, arguments)` 在
  `pendingProviderCalls` 里解析成**框架真实 ID**：本卷传入的 `tool-N` 是合成 ID，
  直接用它定位 transcript 事件会**静默失效**（不报错，只是永不归位）。守卫用例：
  `TestToolNarrationStaysWithOwningIteration`、`TestConcurrentSessionsKeepOwnContent`。
- 空工具结果（`result == ""`）在 provider 投影里**保持为空**：wire 上本来就是空正文，
  补 `MissingHistoryContent` 会让下一轮重投影与已发出字节分叉（2026-09-12 修复；
  见 `context_runtime/history.go` 的 `RepairEmptyHistoryContent` 与
  `TestContextPrefixInvariant_EmptyToolResult`）。
- 快照截断链两端都在本卷：实时路径 `boundToolResultForSnapshot`（超
  `limits.snapshot_tool_output_chars`，默认 8000 字符、按 rune 边界安全切）只把预览
  放进可见会话，全文归档为 result_ref（前端「加载完整输出」再分页读回）；恢复路径
  `appendHistoryLocked` 复用同一预算。provider 侧历史不受该值影响，仍由
  `max_tool_result_chars` 约束（`tool_hooks_truncation_test.go` 两个用例各钉一端）。
- plan 工具入参在钩子边界规范化：`normalizePlanToolCallInfo` 让应用快照与 Seele
  执行使用同一 DAG 表示；非法入参原样保留，使工具错误对用户可见。
- 工具完成投影（`handleToolCompleteObserved`）在 `tool_result` 之后统一补一条空
  assistant 占位，不区分会话是否活跃：后台会话若缺占位，后续流式正文会被
  `appendVisibleDeltaBackground` 并入工具之前的旧 assistant 空消息，热切回该
  会话时工具会“后插入”到最终回复之后（2026-09-08 修复；回归用例
  `repro_hot_attach_background_tool_order_test.go` 钉住热/冷两种恢复顺序）。

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
