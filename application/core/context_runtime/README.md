# context_runtime

## 生态位

上下文装配与控制：provider 上下文 token 预算/压缩/result-ref
（`ContextController`）与 provider 缓存归一化（`HistoryCoordinator`，空
content 修复）。

## 职责与非职责

- 做：`PrepareExecutionContext`、`CompactTaskContext`、超限工具结果拒绝、
  `RemoveTaskContextCheckpoints`、`PrepareProviderHistory`。装配顺序为
  system → 累积 context（达峰前 append-only 全量已定稿轮次）→ plan 尾部；
  达到软阈值时压缩（折叠 compact 栈顶 + context 窗口，发布
  `RecordContextCompactionLocked`），压缩后为有界新鲜窗口。checkpoint 正常
  路径不再注入 LLM 上下文，只保留恢复路径（根包 `history_safety.go` 的
  provider 504 / history-safety 信封）与持久化数据面（`RememberCheckpointLocked`）。
- 不做：chat 主循环、provider 失败重试工作流（根包 `history_safety.go`）。

## 关键文件

| 文件 | 职责 |
|---|---|
| `ports.go` | `TaskPort`/`SessionPort`/`PromptPort`/`ViewPort`/`HistoryPort` + `Deps`。 |
| `coordinator.go` | 上下文装配/压缩协调器与纯 helper（警告文本、checkpoint 判定）。 |
| `history.go` | provider 历史归一化（`HistoryCoordinator`）。 |

## 依赖方向

依赖 `state.Core` + 消费方窄端口；token 预算/transcript 收敛复用
`task_context` 纯函数（`ContextBudgetFor`/`TranscriptTailHistory`）。

## 并发/安全语义

锁内只做内存态变更与 Snapshot 一致发布；锁外收集投影、调用 Engine。
`PrepareExecutionContext` 锁内读 task 权威状态 → 锁外 ReplaceHistory →
锁内记 checkpoint → 锁外 Publish。

## 扩展与 Review

新增压缩策略改 `fitExecutionHistory`；替换 token 估算走 `TaskPort` 计数面。
Review 重点：持锁不得调用外部端口、压缩后历史必须保留 system 前缀缓存
友好性、内部标记不得进入可见会话、正常路径不得重新注入 checkpoint（恢复
路径由 `history_safety.go` 单独负责）、累积段字节稳定（已定稿轮次不重排/
不改写，压缩是唯一使前缀失效的事件）、plan/task 尾部不参与压缩。

## 测试

```text
go test ./application/core/context_runtime -count=1
```

根包 `context_controller_test.go`/`history_safety_test.go` 覆盖跨域集成。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### coordinator.go

- `func IsActiveSkillContent(content string) bool` — IsActiveSkillContent 判定内容是否为激活技能 internal 事件（Append-only
- `func NewCoordinator(deps Deps) *Coordinator` — NewCoordinator 构造 context 域协调器。
- `func (c *Coordinator) Ports() Ports` — Ports 是装配端口图的只读快照（组装校验/诊断用）。
- `func (c *Coordinator) CompactTaskContext(requestID string) error` — CompactTaskContext 把整个可变 transcript 替换为一个私有、有界的 checkpoint
- `func (c *Coordinator) CompactTaskContextFor(sessionID, requestID string) error` — CompactTaskContextFor 把指定会话整个可变 transcript 替换为一个私有、有界
- `func (c *Coordinator) sessionLocationLocked(sessionID string) session_runtime.Location` — sessionLocationLocked 返回指定会话的持久化定位（workspace 绑定优先；
- `func (c *Coordinator) PrepareExecutionContext(requestID, currentInput string) (string, error)` — PrepareExecutionContext 从 durable task 状态与完整 transcript 单元重建
- `func (c *Coordinator) PrepareExecutionContextFor(sessionID, requestID, currentInput string) (string, error)` — PrepareExecutionContextFor 从 durable task 状态与完整 transcript 单元重建
- `func (c *Coordinator) fitExecutionHistory( systemPrompt string, systems []contract.EngineMessage, planMessage string, events []model.TranscriptEvent, currentInput string, tools []model.Tool, target int, contextMaxUnits int, ) ([]contract.EngineMessage, int)` — fitExecutionHistory 按目标预算装配 provider 历史：稳定前缀（system）→
- `func (c *Coordinator) tryFitExecutionHistory( systemPrompt string, systems []contract.EngineMessage, planMessage string, events []model.TranscriptEvent, currentInput string, tools []model.Tool, target int, contextMaxUnits int, ) ([]contract.EngineMessage, int)` — tryFitExecutionHistory 装配一次 system → context → plan 历史并估算 token。
- `func (c *Coordinator) planContextMessageLocked(sessionID string) string`
- `func currentPlanSlice(arguments, currentNode string) any`
- `func excludeCurrentInputEvent(events []model.TranscriptEvent, requestID, currentInput string) []model.TranscriptEvent`
- `func (c *Coordinator) protectOversizedCurrentInputLocked(sessionID, requestID, currentInput string, budget task_context.ContextBudget) string`
- `func ContentReferenceWarning(resultRef string) string` — ContentReferenceWarning 是超限用户输入归档引用警告文本。
- `func (c *Coordinator) rejectOversizedToolResults(sessionID string, maxChars int) (bool, error)` — rejectOversizedToolResults 把超限输出替换为显式重试指令（不给头部/尾部
- `func RejectToolResults(history []contract.EngineMessage, maxChars int) ([]contract.EngineMessage, bool)` — RejectToolResults 替换超限工具结果为显式引用警告（纯函数面）。
- `func rejectToolResultsWithRefs(history []contract.EngineMessage, maxChars int, refs map[string]string) ([]contract.EngineMessage, bool)`
- `func IsOversizedToolResult(content string, maxChars int) bool` — IsOversizedToolResult 判定工具结果是否超限（或带框架截断标记）。
- `func OversizedToolResultWarning(name, resultRef string) string` — OversizedToolResultWarning 是超限工具结果归档警告文本。
- `func ProviderSafeToolResult(name, result string, toolErr error) string` — ProviderSafeToolResult 把超限工具结果替换为警告（provider 路径）。
- `func EstimateEngineHistoryTokens(history []contract.EngineMessage) int` — EstimateEngineHistoryTokens 估算引擎历史 token 数（纯函数面）。
- `func TaskContextRecoveryHistory(history []contract.EngineMessage, checkpoint string) []contract.EngineMessage` — TaskContextRecoveryHistory 保留 system 指令并把可变协议记录替换为 checkpoint。
- `func RetainedSystemHistory(history []contract.EngineMessage) []contract.EngineMessage` — RetainedSystemHistory 保留稳定前缀 + 已定稿轮次的 append-only 累积段：
- `func RetainedSystemOnly(history []contract.EngineMessage) []contract.EngineMessage` — RetainedSystemOnly 保留一条产品指令（框架侧摘要也是 system 消息，全保留
- `func isDynamicTailMessage(message contract.EngineMessage) bool` — isDynamicTailMessage 判定消息是否为动态尾部/控制消息（plan 上下文、
- `func retainedContextEventCount(retained []contract.EngineMessage) int` — retainedContextEventCount 返回保留段中已定稿轮次的 message 数（与
- `func (c *Coordinator) RemoveTaskContextCheckpoints() error` — RemoveTaskContextCheckpoints 阻止 Application 控制消息被持久化/重建为
- `func (c *Coordinator) RemoveTaskContextCheckpointsFor(sessionID string) error` — RemoveTaskContextCheckpointsFor 阻止 Application 控制消息被持久化/重建为
- `func IsTaskContextCheckpoint(content string) bool` — IsTaskContextCheckpoint 判定内容是否为上下文 checkpoint 标记。
- `func (c *Coordinator) RecordContextControlFailure(requestID string, err error)` — RecordContextControlFailure 把 hook 失败转移给 runChat（委托 task 域）。
- `func (c *Coordinator) TakeContextControlFailure(requestID string) error` — TakeContextControlFailure 取走当前请求的 context 控制失败（委托 task 域）。
- `func (c *Coordinator) engineHistory(sessionID string) []contract.EngineMessage` — engineHistory 返回指定会话引擎历史（会话路由引擎用 HistoryFor，否则活跃
- `func (c *Coordinator) setEngineSystemPrompt(sessionID, prompt string)` — setEngineSystemPrompt 设置指定会话引擎 system prompt（会话路由引擎用
- `func (c *Coordinator) replaceEngineHistory(sessionID string, history []contract.EngineMessage) error` — replaceEngineHistory 会话内替换指定会话引擎历史（会话路由引擎用
- `func (c *Coordinator) clearEngineHistory(sessionID string)` — clearEngineHistory 清空指定会话引擎历史（会话路由引擎用 ClearHistoryFor，
- `func (c *Coordinator) appendEngineHistory(sessionID string, msg types.Message)` — appendEngineHistory 追加消息到指定会话引擎历史（会话路由引擎用

### history.go

- `func NewHistoryCoordinator(core *state.Core) *HistoryCoordinator` — NewHistoryCoordinator 构造 history 域协调器。
- `func (h *HistoryCoordinator) PrepareProviderHistory() error` — PrepareProviderHistory 使每条持久化消息对拒绝空 content 的 provider 安全
- `func (h *HistoryCoordinator) PrepareProviderHistoryFor(sessionID string) error` — PrepareProviderHistoryFor 使每条持久化消息对拒绝空 content 的 provider
- `func (h *HistoryCoordinator) replaceEngineHistory(sessionID string, history []contract.EngineMessage) error` — replaceEngineHistory 会话内替换指定会话引擎历史（会话路由引擎用
- `func (h *HistoryCoordinator) engineHistory(sessionID string) []contract.EngineMessage` — engineHistory 返回指定会话引擎历史（会话路由引擎用 HistoryFor，否则活跃
- `func RepairEmptyHistoryContent(history []contract.EngineMessage) ([]contract.EngineMessage, bool)` — RepairEmptyHistoryContent 修复空 content 消息（assistant 工具调用 /
- `func IsProviderOnlyHistoryContent(content string) bool` — IsProviderOnlyHistoryContent 识别仅用于满足 provider 非空 content 要求的

### history_test.go

- `func TestRepairEmptyHistoryContentRepairsToolCallAssistantContent(t *testing.T)`
- `func TestRetainedSystemHistoryKeepsStablePrefixAndSettledContext(t *testing.T)`
- `func retainedContents(history []contract.EngineMessage) []string`
- `func TestRetainedSystemHistoryKeepsActiveSkillEvent(t *testing.T)` — TestRetainedSystemHistoryKeepsActiveSkillEvent：激活技能事件是 append-only

