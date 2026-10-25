# agentsession

## 生态位

`backend/multiagent/agentsession` 提供子代理与 teammate 共用的独立 Agent
Session 构造原语。调用方是 `backend/multiagent/subagent.SessionFactory` 和
`backend/engine/assemble` 的 teammate 角色回合执行面。

## 职责与非职责

职责：

- 要求显式 SessionID；
- 为每次构造补 Model、Hooks、SessionID；
- 在历史路由可用时装配该 SessionID 自己的 `store.DurableHistory`；
- 为 DurableHistory 装配会话绑定的 workspace resolver。
- 冷恢复时通过 `InitialHistory` / `NewWithIDWithHistory` 把中断前快照注入
  下一次历史加载，使继续 Chat 或重跑从同一上下文开始。

非职责：

- 不管理子代理树、注册表、fork、merge-back；
- 不管理 teammate 的 team plan、WorkItem、worktree 或作业生命周期；
- 不读取或覆盖主会话上下文。

## 依赖方向

允许依赖 Seele `session` 与 Seelex `store`。子代理、teammate 与装配层可以依赖
本包，本包不得反向依赖它们。

## 会话边界

一个 SessionID 对应一份独立 transcript。子代理与 teammate 可使用不同模板（例如
teammate 的工具面会移除 `fork_subagents`），但共享同一套显式身份与 DurableHistory
装配判据。主会话继续使用自己的装配路径。

## 扩展方式

宿主通过 `Deps.Components` 注入完整模板；任何会话专属字段必须在 `NewWithID`
中补齐，不得由调用方复制一份 DurableHistory 装配逻辑。

## Review 指南

- 是否出现第二份 DurableHistory 装配；
- WorkItem Session 是否误用主会话 SessionID 或主会话 Context store；
- 是否把注册表、团队计划等生命周期职责塞进本包。

## 测试与验证

```text
go test ./backend/multiagent/agentsession ./backend/multiagent/subagent -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### factory.go

- `func New(deps Deps) *Factory` — New assembles a factory over host-owned session components.
- `func (factory *Factory) NewWithID(sessionID string, hooks *frameworksession.LoopHooks) (*frameworksession.Session, error)` — NewWithID creates one session with an explicit identity. When a history
- `func (factory *Factory) NewWithIDWithHistory(sessionID string, hooks *frameworksession.LoopHooks, initial []types.Message) (*frameworksession.Session, error)` — NewWithIDWithHistory 创建会话并把一次性的恢复快照交给框架下一次历史加载。

### factory_test.go

- `func (testCompleter) Complete(context.Context, []types.Message, []types.Tool) (types.Message, error)`
- `func (testCompleter) CompleteStream(context.Context, []types.Message, []types.Tool, func(string)) (string, string, []types.ToolCall, error)`
- `func (testCompleter) CompleteStreamEvents(context.Context, []types.Message, []types.Tool, func(types.StreamEvent)) (string, string, []types.ToolCall, error)`
- `func (testAgent) VisibleTools(context.Context) []types.Tool`
- `func (testAgent) Dispatch(context.Context, string, string) (string, error)`
- `func (testAgent) LLM() types.ChatCompleter`
- `func testDeps() Deps`
- `func TestFactoryRequiresExplicitSessionID(t *testing.T)`
- `func TestFactoryBuildsFrameworkSessionWithRequestedIdentity(t *testing.T)`
- `func (c *captureCompleter) capture(messages []types.Message)`
- `func (c *captureCompleter) Complete(_ context.Context, messages []types.Message, _ []types.Tool) (types.Message, error)`
- `func (c *captureCompleter) CompleteStream(_ context.Context, messages []types.Message, _ []types.Tool, _ func(string)) (string, string, []types.ToolCall, error)`
- `func (c *captureCompleter) CompleteStreamEvents(_ context.Context, messages []types.Message, _ []types.Tool, _ func(types.StreamEvent)) (string, string, []types.ToolCall, error)`
- `func (a captureAgent) VisibleTools(context.Context) []types.Tool`
- `func (a captureAgent) Dispatch(context.Context, string, string) (string, error)`
- `func (a captureAgent) LLM() types.ChatCompleter`
- `func TestFactorySeedsColdSessionWithInterruptedSnapshot(t *testing.T)`

