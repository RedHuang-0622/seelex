# Agent 调用 MCP 全链路流程图

> 文件索引：本文档展示从 LLM 决定调用工具到 MCP Server 响应返回的完整函数调用链、
> 包/文件位置、数据转换过程，以及熔断事件异步通道。
>
> 关联：`mcpstack/`, `seelebridge/mcp.go`, `Seele/agent/core/tool/mcp/`

---

## 一句话讲清楚

整个链路就三件事，按顺序：

**① 装 MCP Server**（做一次就够了）
> `seelebridge.Runtime.AttachMCP()` — 启动一个 MCP 进程（或连远程 SSE），跟它握手拿到工具列表，注册到 Agent 的工具箱。同时开个后台 goroutine 等着收熔断事件。

**② LLM 调工具**（每次对话反复发生）
> `mcp.Handler.Execute()` — LLM 说"调用 sketch_rectangle，参数 {...}"。请求过三道岗：**权限门控**（能不能调）→ **重试循环**（失败了最多重试 3 次）→ **熔断器检查**（服务器是不是挂了）。过了就发 JSON-RPC 给 MCP Server，等它返回结果。

**③ 熔断器自动保护**（跟②并行）
> `mcpBreaker.emit()` → `mcpstack.ListenBreaker()` — 如果 MCP Server 连续挂 3 次，熔断器自动打开，后面的请求直接在 Go 层拒掉，不去碰那个已经挂了的服务器。同时后台每 3 秒 ping 一次，通了自动恢复。开门/关门/恢复的动作都通过 channel 发出来，`mcpstack` 收到后记到 trace 里。

> 文件索引：本文档展示从 LLM 决定调用工具到 MCP Server 响应返回的完整函数调用链、
> 包/文件位置、数据转换过程，以及熔断事件异步通道。
>
> 关联：`mcpstack/`, `seelebridge/mcp.go`, `Seele/agent/core/tool/mcp/`

---

## 1. SETUP 阶段（一次性 MCP 服务器装配）

```
main.go (root)
  registerMCPServers()                          [mcpconfig.go:93]
    │
    ▼
seelebridge.Runtime.AttachMCPServer()           [seelebridge/mcp.go:65]
    │
    ▼
seelebridge.Runtime.AttachMCP(ctx, cfg)         [seelebridge/mcp.go:43]
    │
    ├─1. BreakerEvents()                        [seelebridge/mcp.go:33]
    │     └─ mcp.Provider.SetBreakerEventsChannel(ch)
    │           [Seele/agent/core/tool/mcp/provider.go:54]
    │         └─ mcpBreaker.SetEventsChannel(ch)
    │           [Seele/agent/core/tool/mcp/breaker.go:62]
    │
    ├─2. go mcpstack.ListenBreaker(stack, ch)   [mcpstack/breaker.go:21]
    │     └─ for evt := range ch {
    │          MCPStack.Record(call)
    │        }  ← 后台 goroutine 持续消费
    │
    ├─3. mcp.Provider.Attach(ctx, cfg)          [Seele/agent/core/tool/mcp/provider.go:93]
    │     ├─ transport: NewStdioMCPClient / NewSSEMCPClient
    │     ├─ client.Initialize()              ← MCP 握手
    │     └─ fetchTools() → ListTools()       ← 发现工具列表
    │
    └─4. refreshMCPTools(provider)              [seelebridge/mcp.go:121]
          └─ Holder.Unregister("mcp")
          └─ Holder.Register(provider)
                └─ provider.Tools()             [Seele/agent/core/tool/mcp/provider.go:175]
                      └─ 为每个工具创建 mcp.Handler {
                           Client:     conn.client,         ← mark3labs MCP 客户端
                           ToolName:   t.Function.Name,     ← 原始工具名
                           ServerName: serverName,          ← 用于熔断器 key
                           breaker:    p.breaker,           ← 共享熔断器
                         }
```

---

## 2. CALL 阶段（LLM 决定调用工具 → MCP Server 响应）

```
LLM 生成工具调用 → 工具名 + JSON 参数
         │
         ▼  argsJSON (string, raw JSON)
         │
┌──── Agent.Dispatch(ctx, name, argsJSON)       [Seele/agent/agent.go:245] ────┐
│    │  WaitGroup 注册 → 控制优雅关闭                                            │
│    ▼                                                                          │
│  DefaultGateway.Dispatch(ctx, name, argsJSON) [Seele/agent/gateway/tool/default.go:40]
│    │                                                                          │
│    ├─ checkPermission()                     ← 权限门控：allow / deny / ask    │
│    │                                                                          │
│    ▼                                                                          │
│  Holder.Dispatch(ctx, name, argsJSON)        [Seele/agent/core/tool/holder/holder.go:109]
│    │                                                                          │
│    ├─ 在 toolMap 中查找 ToolEntry              ← 按工具名查找                  │
│    ├─ 派生超时 context                        ← 超时控制                      │
│    ├─ 重试循环 (默认 3 次)                     ← 仅重试 ErrToolUnavailable     │
│    │                                                                          │
│    ▼                                                                          │
│  [重试循环内] entry.Handler.Execute(callCtx, argsJSON)
│    │                                                                          │
│    ▼                                                                          │
│  ┌──────────────────────────────────────────────────────┐                    │
│  │  mcp.Handler.Execute(ctx, argsJSON)                   │                    │
│  │  [Seele/agent/core/tool/mcp/handler.go:22]            │                    │
│  │                                                       │                    │
│  │  STEP 1 ─── 熔断器检查                                │                    │
│  │    mcpBreaker.beforeCall(serverName)                  │                    │
│  │    [Seele/agent/core/tool/mcp/breaker.go:80]          │                    │
│  │      ├─ 查 history                                    │                    │
│  │      ├─ 若未开门          → 放行，返回 nil             │                    │
│  │      ├─ 若开门 & 退避中   → emit("opened") → err      │                    │
│  │      └─ 若开门 & 退避到期 → emit("half_open") → 放行  │                    │
│  │              │                                       │                    │
│  │         if err != nil → return "", err              │                    │
│  │              │                                       │                    │
│  │  STEP 2 ─── 参数解析                                 │                    │
│  │    json.Unmarshal(argsJSON, &args)                   │                    │
│  │    argsJSON (string) → args (map[string]interface{}) │                    │
│  │              │                                       │                    │
│  │  STEP 3 ─── 构建 MCP 请求                             │                    │
│  │    mcp.CallToolRequest{Params: {Name, Arguments}}    │                    │
│  │              │                                       │                    │
│  │  STEP 4 ─── 实际 MCP 调用 (mark3labs 客户端)          │                    │
│  │    h.Client.CallTool(ctx, req)                       │                    │
│  │    [mark3labs/mcp-go 库]                             │                    │
│  │      ├─ stdio: JSON-RPC over stdin/stdout            │                    │
│  │      └─ sse:   JSON-RPC over HTTP SSE                │                    │
│  │              │                                       │                    │
│  │         ┌────┴────┐                                  │                    │
│  │         ▼         ▼                                  │                    │
│  │    成功 ✅     失败 ❌                               │                    │
│  │      │           │                                    │                    │
│  │  STEP 5a ───    STEP 5b ─── 熔断器更新                │                    │
│  │  afterCall(nil)  afterCall(isConnErr)                │                    │
│  │  → reset fail=0  → failures++                       │                    │
│  │  → emit("closed") │  if >= maxFails:                 │                    │
│  │                   │    open=true, emit("opened")     │                    │
│  │                   │    startRecovery()               │                    │
│  │                   │    [breaker.go:146]              │                    │
│  │                   │      emit("recovering")          │                    │
│  │                   │      └─ goroutine:               │                    │
│  │                   │          每 3s ping → 成功       │                    │
│  │                   │          emit("recovered")       │                    │
│  │                   │                                   │                    │
│  │  STEP 6 ─── 提取结果                                │                    │
│  │  mcp.GetTextFromContent(content)                     │                    │
│  │  → strings.Join(parts, "\n")                        │                    │
│  │              │                                       │                    │
│  └──────────────┼───────────────────────────────────────┘                    │
│                 ▼  result (string) / err                                    │
│                                                                              │
└──── Holder.Dispatch 返回 ───────────────────────────────────────────────────┘
      │
      ▼  result/err → Gateway → Agent → LLM session
```

---

## 3. 熔断事件异步通道

```
mcpBreaker (在 mcp.Handler.Execute 内部)
  │
  ├─ beforeCall 检开拒绝调用     → emit("opened", failures)
  ├─ beforeCall 退避到期半开      → emit("half_open", failures)
  ├─ afterCall  连续失败触发熔断  → emit("opened", failures)
  ├─ afterCall  成功调用归零     → emit("closed", 0)
  ├─ startRecovery 后台 ping 启动→ emit("recovering", failures)
  └─ recoverLoop 恢复成功       → emit("recovered", 0)
                                    │
                        events chan (chan<- BreakerEvent)
                                    │
  ┌─────────────────────────────────┘
  ▼  (后台 goroutine)
mcpstack.ListenBreaker(stack, ch)            [mcpstack/breaker.go:21]
  │
  ├─ 收到 BreakerEvent
  ├─ 构造 MCPCall {
  │     ServerName: evt.ServerName,
  │     ToolName:   "__breaker__<type>",      ← 特殊前缀区分
  │     Args:       {event, failures},
  │     Status:     StatusRolledBack,           ← 熔断标记
  │  }
  └─ MCPStack.Record(call)                   [mcpstack/stack.go:128]
        │
        └─ 追加到 Calls[] 数组
             CurrentIdx++
```

---

## 4. mcpstack 调用记录（可选：手动 BeforeCall/AfterCall 包装）

```
调用方 (future wrapper in seelebridge/tool handler)
  │
  ├─ mcpstack.BeforeCall(stack, server, tool, args, aiBacklink)
  │   [mcpstack/interceptor.go:48]
  │   ├─ 创建 MCPCall {Status: StatusPending}
  │   └─ MCPStack.Record(call)
  │
  ├─ (实际 MCP 调用) → mcp.Handler.Execute(...)
  │
  └─ mcpstack.CallRecorder.AfterCall(result, err)
      [mcpstack/interceptor.go:90]
        ├─ 成功 → StatusSuccess + result
        └─ 失败 → StatusFailed + error_msg
        └─ 更新 Calls[] 中的对应条目
```

---

## 5. 数据转换全景

```
LLM 侧                          Seelex 侧                      MCP Server 侧
─────────                      ──────────                      ────────────
                                argsJSON (string)
"sketch_rect({                  '{"plane":"XY",...}'                   '
  plane: XY,                        │                                  '
  length: 100                       ▼                                  '
})"                            json.Unmarshal                           '
                               → map[string]interface{}                 '
                                      │                                '
                                      ▼                                '
                               CallToolRequest{                        '
                                 Name: "sketch_rect",                  '
                                 Arguments: {plane, length}            '
                               }                                       '
                                      │                                '
                                      ▼                                '
                               mark3labs MCP 客户端 ──── stdio ──────►  FreeCAD
                               (JSON-RPC 2.0) ◄─────── result ──────  MCP Server
                                      │                                '
                                      ▼                                '
                               mcp.GetTextFromContent                  '
                               → strings.Join(parts, "\n")            '
                                      │                                '
                                      ▼                                '
result (string) ◄────────── 返回给 LLM                                 '
'{"sketch_id":1,"status":"ok"}'                                        '
```

---

## 关键文件索引

| 函数 | 包 | 文件 |
|------|----|------|
| `Agent.Dispatch` | Seele | `agent/agent.go:245` |
| `DefaultGateway.Dispatch` | Seele | `agent/gateway/tool/default.go:40` |
| `Holder.Dispatch` | Seele | `agent/core/tool/holder/holder.go:109` |
| `Handler.Execute` | Seele | `agent/core/tool/mcp/handler.go:22` |
| `mcpBreaker.beforeCall` | Seele | `agent/core/tool/mcp/breaker.go:80` |
| `mcpBreaker.afterCall` | Seele | `agent/core/tool/mcp/breaker.go:101` |
| `mcpBreaker.startRecovery` | Seele | `agent/core/tool/mcp/breaker.go:146` |
| `mcpBreaker.emit` | Seele | `agent/core/tool/mcp/breaker.go:68` |
| `mcpBreaker.SetEventsChannel` | Seele | `agent/core/tool/mcp/breaker.go:62` |
| `Provider.SetBreakerEventsChannel` | Seele | `agent/core/tool/mcp/provider.go:54` |
| `Provider.Attach` | Seele | `agent/core/tool/mcp/provider.go:93` |
| `Provider.Tools` | Seele | `agent/core/tool/mcp/provider.go:175` |
| `Runtime.AttachMCP` | seelebridge | `seelebridge/mcp.go:43` |
| `Runtime.BreakerEvents` | seelebridge | `seelebridge/mcp.go:33` |
| `MCPStack.Record` | mcpstack | `mcpstack/stack.go:128` |
| `BeforeCall` | mcpstack | `mcpstack/interceptor.go:48` |
| `CallRecorder.AfterCall` | mcpstack | `mcpstack/interceptor.go:90` |
| `ListenBreaker` | mcpstack | `mcpstack/breaker.go:21` |
| `Runtime.AttachMCPServer` | seelebridge | `seelebridge/mcp.go:65` |
| `mcpConfig.go:registerMCPServers` | main | `mcpconfig.go:93` |
