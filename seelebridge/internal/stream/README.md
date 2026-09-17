# Stream

## 生态位

`seelebridge/internal/stream` 承载流式账号 Completer 适配器
（`NewStreamingCompleter`）：把账号池的同步 Completer 适配为流式
`agent.StreamCompleter`，租约覆盖整条流直到 EOF/错误/Close 才释放。
属于根 facade 的装配细节（仅 runtime.go 装配使用），置于 internal/；
单元测试随包（`stream_test.go`），runtime 装配集成测试保留在根包
（`stream_integration_test.go`）。

## 时序图

```mermaid
sequenceDiagram
    autonumber
    participant E as agent（流式请求方）
    participant S as stream.NewStreamingCompleter
    participant P as accountpool.P2CPool
    participant A as 具体账号 ChatClient

    E->>S: Stream(ctx, request)
    S->>P: 申请账号（取得租约）
    P-->>S: lease
    S->>A: 发起流式请求
    loop 流式输出
        A-->>S: chunk
        S-->>E: chunk
    end
    Note over S,P: 租约保持到 EOF / 错误 / 显式 Close，中途不被抢占
    S->>P: 释放 lease
    S-->>E: 流结束
```

## 验证

```text
go test ./seelebridge/internal/stream -count=1
```
