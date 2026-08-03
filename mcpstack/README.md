# MCP Stack

## 模块定位

`mcpstack` 为 MCP 调用提供可回放的调用栈、持久化、prompt 摘要和 breaker 事件记录。它补充 Seele MCP runtime 的可观测性，不负责建立或管理 MCP 连接。

## 核心实现

- `MCPCall`：server/tool、args/result、状态、耗时、错误、AI backlink 和时间戳。
- `MCPStack`：线性 history + cursor，支持 Record、Undo、Redo、Peek、Latest 和条件查询。
- `BeforeCall`/`CallRecorder.AfterCall`：拦截器式调用计时与结果记录。
- `ListenBreaker`：把 Seele breaker channel 状态写入调用轨迹。
- `Save`/`Load`/`Marshal`：原子持久化调用栈。
- `ForPrompt`：在 token budget 内生成模型可读摘要。
- `TraceProvider`：把 MCP history 转为 `seelexctx/snapshot`。

## 状态语义

cursor 指向当前 active call。Undo 不删除历史，Redo 只在未发生新 Record 时有效；在 undo 后 Record 会截断 redo branch。`Snapshot` 返回深拷贝，调用方不能修改内部 args/result/tags。

## 生态位与依赖

上游由 `seelebridge.Runtime` 在 MCP tool 调用周围接入；下游可供 trace、context export 和诊断 UI 使用。禁止反向依赖 Application 或 frontend。

## Review 指南

- Record/Undo/Redo/Snapshot 并发访问是否都受锁保护。
- JSON RawMessage、map 和 slice 是否深拷贝。
- auto-save 是否原子写入，失败是否返回而不是静默丢失。
- prompt 摘要是否严格遵守 budget，避免把秘密或无限结果送回模型。
- breaker listener 是否能随 channel 关闭退出。

## 测试

```text
go test ./mcpstack -count=1
```
