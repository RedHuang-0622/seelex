# prompt_layer

## 生态位

system prompt 层组装与引擎同步：identity/instructions/base 栈 + 活跃任务
skill 层 + Plan 执行策略；前缀缓存友好（内容不变不重复 `SetSystemPrompt`）。

## 职责与非职责

- 做：`BuildSystemPrompt`、`ApplyActiveTaskSystemPrompt`、
  `SystemPromptForActiveTaskLocked`。
- 不做：skill 请求信封编解码（根包）、prompt 栈实例所有权（组合根持有并
  注入）。

## 依赖方向

依赖 `state.Core` + 注入的 `PromptStack`/`EffortManager` 引用 +
`TaskContextView` 窄只读面（`task_context.Coordinator` 满足）。

## 并发/安全语义

`ApplyActiveTaskSystemPrompt` 锁内读任务状态，锁外同步引擎；plan 投影只放
稳定信息（`plan_ref`），不放随节点变化的 `current_node`。

## 扩展与 Review

新增 prompt 层改 `BuildSystemPrompt`。Review 重点：skill 内容不得成为持久
system 层、前缀缓存失效面、锁外 `SetSystemPrompt`。

## 测试

根包 `service_input_test.go` 等覆盖组装与缓存稳定性。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### coordinator.go

- `func NewCoordinator(deps Deps) *Coordinator` — NewCoordinator 构造 prompt 域协调器。
- `func (c *Coordinator) BuildSystemPrompt()` — BuildSystemPrompt 只组装 system 层（skill 内容留在请求信封，不持久化）。
- `func (c *Coordinator) ApplyActiveTaskSystemPrompt(requestID string)` — ApplyActiveTaskSystemPrompt 按活跃任务刷新 system prompt（锁内读取任务
- `func (c *Coordinator) ApplyActiveTaskSystemPromptFor(sessionID, requestID string)` — ApplyActiveTaskSystemPromptFor 按指定会话活跃任务刷新 system prompt（锁内
- `func (c *Coordinator) SystemPromptForActiveTaskLocked() string` — SystemPromptForActiveTaskLocked 组装活跃任务 system prompt（调用方持有
- `func (c *Coordinator) SystemPromptForActiveTaskLockedFor(sessionID string) string` — SystemPromptForActiveTaskLockedFor 组装指定会话活跃任务 system prompt
- `func (c *Coordinator) setEngineSystemPrompt(sessionID, promptText string)` — setEngineSystemPrompt 设置指定会话引擎的 system prompt（支持会话路由的
- `func (c *Coordinator) activeSessionID() string` — activeSessionID 返回当前活跃会话（快照归属会话）。

