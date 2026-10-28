# application/execution/toolhooks

## 生态位

`application/execution/toolhooks` 把 Seele loop hook 适配成 Application 的工具投影。
主要调用方是 [`application/assembly`](../../assembly/README-tool.md) 的
`ToolHookBridge`；owner 同时拥有 tool start/complete 的可见消息、Task transcript、
runtime 投影和发布顺序，assembly Host adapter 只提供现有状态机制。

## 职责

- 为并发 tool calls 分配稳定唯一 ID；
- 按 turn/name/arguments 将 start 与 complete 配对；
- 串联 LLM complete、tool start/complete 和 iteration complete；
- 固定 tool start/complete 的 active/background、request fallback、结果归档和事件顺序；
- 维护 Seele loop 的迭代闸门：ReAct budget、排队输入、provider history 修复与失败记录；
- 提供可选、metadata-only 的 hook 诊断事件，观察者 panic 不得穿过 hook 边界。

## 非职责

- 不保存工具结果或工具计数；
- 不拥有 Engine/Task/Runtime；
- 不判断工具权限、不执行工具；
- 不持有 Task、View 或持久化状态；结果预算/归档规则由注入端口执行；
- 不导入 `application/assembly`。

## 文件结构

- `bridge.go`：`Bridge`、`Deps`、诊断类型与 Seele hook 适配。
- `projection.go`：`ProjectionApplication`，唯一拥有 tool start/complete 投影顺序。
- `bridge_test.go`：并发 FIFO 配对、迭代闸门分支与诊断容错用例。
- `projection_test.go`：session fallback 与 start 发布顺序。

## 核心实现

```go
type Deps struct {
    RecordLLMComplete       func(context.Context, session.LLMInfo)
    RecordReActToolCall     func(context.Context)
    HandleToolStart         func(context.Context, string, string, string)
    HandleToolComplete      func(context.Context, session.ToolCallInfo, string, func(string))
    AllowNextReActIteration func(context.Context, int) bool
    SessionBacked           func() bool
    QueuedInputs            func() bool
    ActiveRequestID         func() string
    PrepareHistory          func(context.Context) error
    RecordContextFailure    func(requestID string, err error)
}
```

`Bridge` 是 start/complete 配对与诊断观察者的唯一所有权点。配对键为
`(turn, name, arguments)`，同键并发调用按 start 顺序分配 `tool-N`，complete 按 FIFO
取出；找不到在途配对时分配新 ID，保持旧行为“事件不丢”。

## 数据流与生命周期

1. Seele 调用 `Hooks().OnToolStart`；`Bridge` 分配 ID、保存待完成队列，再调用
   Application `HandleToolStart`。
2. Seele 调用 `OnToolComplete`；`Bridge` 取出同键 FIFO ID，并调用
   Application `HandleToolComplete`，后者负责工具消息、transcript、Runtime 与事件投影。
3. Session-backed 引擎在每次 ReAct 迭代结束时先应用 budget，再检查排队输入；
   非 Session-backed 路径在继续前修复 provider history，失败时记录 context failure 并停止。

## 依赖方向

- 允许依赖 `github.com/RedHuang-0622/Seele/session` 的 hook DTO；
- 只通过 `Deps` 回调依赖 Application 宿主；
- 禁止依赖 `application/assembly`、具体 Engine、Task、Store 或 frontend。

## 并发与错误语义

- `Bind`、`SetDiagnosticObserver`、配对表和诊断观察者由同一互斥锁保护；
- hook 调用可以并发进入，start/complete 的 ID 分配与 FIFO 取出是原子的；
- 诊断观察者是 best-effort：回调 panic 被吞掉，但不影响生产投影；
- `PrepareHistory` 的错误交给 `RecordContextFailure`，`OnIterationComplete` 返回 false。

## 扩展与 Review

- 新的 hook 回调先作为 `Deps` 窄端口加入，再在 `Hooks` 中固定顺序；
- 不得在 `Bridge` 内复制工具权限、结果截断、事件 revision 或持久化判据；
- Review 时重点检查 start/complete 是否保持唯一 owner、诊断是否泄露参数/结果、
  迭代闸门是否在 session-backed 路径错误地执行历史修复。

## 测试与验证

```text
go test ./application/execution/toolhooks -count=1
go test ./application/assembly -run 'ToolHook|HandleTool|IterationComplete' -count=1
```
