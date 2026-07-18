# 设计决策记录：MCP 中间件与存储解耦

> 记录从「CAD 命令栈」到「通用 MCP 中间件」再到「框架-应用存储解耦」的完整设计推演过程。
> 日期：2026-07-18

---

## 目录

1. [起点：CAD 命令栈](#1-起点cad-命令栈)
2. [第一次纠正：这是通用中间件](#2-第一次纠正这是通用中间件)
3. [第二次纠正：MCP Server 都是现成的](#3-第二次纠正mcp-server-都是现成的)
4. [第三次纠正：不应该造轮子](#4-第三次纠正不应该造轮子)
5. [熔断器事件通道](#5-熔断器事件通道)
6. [导入循环的处理](#6-导入循环的处理)
7. [存储解耦：框架与应用的责任边界](#7-存储解耦框架与应用的责任边界)
8. [最终架构总览](#8-最终架构总览)

---

## 1. 起点：CAD 命令栈

### 初始假设

CAD 是 Seelex 的重点场景，需要一个「可追溯、可重放」的命令历史。于是建了 `commandstack/`：

```
commandstack/
├── commandstack.go    — CommandStack, Command, CommandMetadata (含 FreeCADVersion)
├── ops.go             — 22 种 CAD 操作常量 (OpSketchRectangle, OpPadExtrude ...)
├── persist.go         — 原子写入 .seelex/cad/
├── provider.go        — 实现 provider.Provider，喂给 LLM 上下文
└── stack.go           — Push/Undo/Redo/Current/Peek
```

配套还写了：
- `freecad/ops.go` — CAD 参数类型
- `freecad/schema.go` — AllSchemas 注册表
- `freecad/validate.go` — 参数边界验证
- `freecad/server/` — 自研 Python MCP Server（11 个文件）

### 问题

**这是把 CAD 当成了特殊项目，大费周章造定制轮子。** 实际上其他场景（化学、建筑、医学）也需要同样的追溯能力，但 `commandstack` 的 API 和类型全是 CAD 的。

---

## 2. 第一次纠正：这是通用中间件

### 纠正内容

`commandstack` 不应该认识 CAD。把它改成一个通用的 MCP 调用日志，**所有 MCP 调用都必须经过的中间件**。

### 决策

| 决策 | 之前 | 之后 |
|------|------|------|
| 包名 | `commandstack/` | `mcpstack/` |
| 核心类型 | `CommandStack` (CAD 专属) | `MCPStack` (通用 trace) |
| 操作标识 | `Op` 字段 + 22 个 CAD 常量 | `ToolName` + `ServerName` 字符串 |
| 元数据 | `FreeCADVersion` 等 CAD 字段 | `StackMetadata{SessionGoal, Domain}` |
| 拦截器 | 无 | `BeforeCall/AfterCall` 生命周期 |

### 关键代码

```go
// mcpstack/stack.go — 没有任何 CAD 引用
type MCPCall struct {
    ID         string          // UUID
    Seq        int             // 单调递增
    ServerName string          // 目标 MCP 服务器 ("freecad", "chem-sim", ...)
    ToolName   string          // 工具名 ("sketch_rectangle", "simulate", ...)
    Args       json.RawMessage // 参数
    Result     json.RawMessage // 结果
    Status     CallStatus      // pending / success / failed / rolled_back
}
```

---

## 3. 第二次纠正：MCP Server 都是现成的

### 纠正内容

FreeCAD MCP Server **已经有现成的开源实现**，跟 WebSearch 用的是同一个生态位——都是配一个 MCP Server，不是自己写一个。

### 决策

| 决策 | 之前 | 之后 |
|------|------|------|
| `freecad/server/` | 自研 11 个 Python 文件 | ❌ 删除，用现有 FreeCAD MCP |
| `freecad/` 包定位 | CAD MCP Server 的 Go 端 | CAD 参数**验证层**（仅 PreValidate/PostValidate） |
| 插件注册 | 指向自研 server | `plugin.md` 指向现有 MCP Server |
| 配置方式 | 硬编码 | `account-openai.yaml` 的 `mcp_servers` 段，跟 WebSearch 一样 |

### 配置格式

```yaml
# account-openai.yaml — 与 websearch 同一生态位
mcp_servers:
  - name: freecad
    transport: stdio
    command: FreeCADCmd
    args: ["-c", "freecad_mcp_server.py"]

websearch:
  provider: tavily
  api_key: sk-xxx
```

加载方式也是一致的：

```go
// websearch.go — 读取 websearch 段注册工具
func registerWebSearchTool(runtime, accountsPath)

// mcpconfig.go — 读取 mcp_servers 段注册 MCP
func registerMCPServers(runtime, accountsPath)
```

---

## 4. 第三次纠正：不应该造轮子

### 纠正内容

Seele 框架已经提供了完整的 MCP 生命周期管理（`mcp.Provider` — Attach/Detach/Refresh、熔断器、心跳），以及在 `seelebridge` 里封装好的 `AttachMCP`。应该**复用这些**，而不是在 seelex 层再包一层。

### 做的改动

| 之前 | 之后 |
|------|------|
| `mcpconfig.go` 直接调 `runtime.AttachMCPServer` | 框架的 `mcp.Provider` 负责连接和工具发现 |
| `mcpstack` 独立于 `seelebridge` | `seelebridge.Runtime` 内部持有 `MCPStack` |
| 熔断事件无通知 | `breaker.emit()` → channel → `mcpstack.ListenBreaker()` |

### 最终集成链

```
NewRuntime() → 创建 MCPStack
AttachMCP()  → 注入熔断事件 channel + 启动 ListenBreaker
              → 框架的 Provider.Attach() 连接 MCP Server
              → refreshMCPTools 注册工具到 Agent
```

---

## 5. 熔断器事件通道

### 为什么需要

熔断器的三种状态变化（开门、关门、恢复）原来对外完全不可见。`mcpstack` 只能记录到「调用了 → 返回了 error」，**不知道这 error 是熔断器开的还是 MCP 真的挂了**。

### 设计

```go
// Seele/agent/core/tool/mcp/breaker.go

type BreakerEvent struct {
    ServerName string
    Type       BreakerEventType  // "opened" | "half_open" | "closed" | "recovering" | "recovered"
    Failures   int
}

type mcpBreaker struct {
    events chan<- BreakerEvent  // nil = 不启用，完全向后兼容
}
```

### 6 个埋点

| 位置 | emit 事件 | 含义 |
|------|----------|------|
| `beforeCall` 检开拒绝 | `BreakerOpened` | 熔断器开着，请求被短路 |
| `beforeCall` 退避到期 | `BreakerHalfOpen` | 熔断超时到期，允许探测 |
| `afterCall` 失败开门 | `BreakerOpened` | 刚触发的熔断 |
| `afterCall` 成功归零 | `BreakerClosed` | 恢复正常 |
| `startRecovery` | `BreakerRecovering` | 后台 ping 已启动 |
| `recoverLoop` 成功 | `BreakerRecovered` | 后台 ping 恢复成功 |

### 消费者

```go
// mcpstack/breaker.go
func ListenBreaker(stack *MCPStack, ch <-chan BreakerEvent) {
    for evt := range ch {
        // 每条熔断事件 → 记录为一条 MCPCall (StatusRolledBack)
        stack.Record(MCPCall{
            ToolName:   "__breaker__" + evt.Type,
            Status:     StatusRolledBack,
            ServerName: evt.ServerName,
        })
    }
}
```

---

## 6. 导入循环的处理

### 问题

```
seelebridge → mcpstack/provider.go → seelexctx/provider → seelebridge (trace.go)
                                       ↑
                               provider.Provider 接口定义在这
```

`mcpstack.TraceProvider` 要实现 `provider.Provider` 接口，而这个接口定义在 `seelexctx/provider` 包中，该包又因为 `trace.go` 导入了 `seelebridge`，造成了循环。

### 解决方案

**不导入接口包**。让 `mcpstack` 定义一个纯数据的 `BuildSnapshot()` 方法，返回 `*snapshot.ContextSnapshot`。真正的接口实现在 `seelebridge` 层组装。

```go
// mcpstack/provider.go — 不 import seelexctx/provider
type TraceProvider struct {
    stack *MCPStack
}

func (p *TraceProvider) BuildSnapshot() (*snapshot.ContextSnapshot, error) {
    // 纯数据构造，无接口依赖
}

// seelebridge 层包装为 provider.Provider:
type mcpTraceAdapter struct {
    inner *mcpstack.TraceProvider
}
func (a *mcpTraceAdapter) Export(ctx context.Context) (*snapshot.ContextSnapshot, error) {
    return a.inner.BuildSnapshot()
}
```

### 教训

> Go 的导入循环经常由「接口定义在实现者的包里」引起。修复办法：接口定义在使用方，或者让中间层只产数据不产接口适配。

---

## 7. 存储解耦：框架与应用的责任边界

### 问题

Seele 框架的 `seelectx/storage.Store` 是一个具体的文件系统存储实现，而且还有个硬编码默认路径 `.seele/sessions/`：

```go
// store.go
func NewStore(baseDir string) (*Store, error) {
    if baseDir == "" {
        baseDir = ".seele/sessions/"  // ← 用户没传参就默默建目录
    }
```

导致的问题：

| 场景 | 问题 |
|------|------|
| seelex 用 `.seelex/sessions/` | 框架另建个 `.seele/sessions/`，两边不一致 |
| 用户想存数据库 | 框架给了具体实现，没法换 |
| 用户不想持久化 | 传空字符串也会建目录 |
| 测试 | 要 mock 文件系统 |

### 解决方案

**框架只定义接口，不提供实现**。

```go
// storage.Storage — 框架只定义契约
type Storage interface {
    Save(sessionID string, messages []types.Message) error
    Load(sessionID string) ([]types.Message, error)
    List() []SessionMeta
    Delete(sessionID string) error
}

// storage.FileStore — 内置实现，可替换
type FileStore struct { ... }  // 原 Store 改名
```

Engine 从不调用 Store 的方法，它只是持有引用并传给 Loop。所以改成接口零成本：

```go
// engine.go
type Engine struct {
    store storage.Storage  // 不再是 *storage.Store
}
```

### 新旧对比

| 维度 | 旧 | 新 |
|------|-----|-----|
| 框架提供 | 具体实现 + 默认路径 | 接口 + 一个内置实现 |
| 应用自由度 | 必须用文件系统 | 可换数据库/S3/内存 |
| 默认行为 | 不传参就建 `.seele/` | 不传参就 no-op |
| 耦合方向 | 应用 ← 框架实现 | 应用 ← 接口契约 |

### 向后兼容

加了别名确保旧代码继续编译：

```go
type Store = FileStore      // 旧名 → 新名
func NewStore(baseDir string) (*FileStore, error) {
    return NewFileStore(baseDir)
}
```

---

## 8. 最终架构总览

```
┌─────────────────────────────────────────────────────────────┐
│                      Seelex Application                      │
│                                                             │
│  mcp_servers: [...]  (account YAML)                         │
│       ↓                                                     │
│  seelebridge.Runtime.AttachMCP()  ← 复用框架 MCP 生命周期   │
│       │                                                    │
│       ├── mcpstack.BeforeCall/AfterCall  ← 调用 trace       │
│       ├── mcpstack.ListenBreaker         ← 熔断事件 trace   │
│       └── mcpstack.TraceProvider         ← LLM 上下文注入    │
│                                                             │
│  会话存储: SessionStore → storage.Storage (接口)             │
│       ├── storage.FileStore (内置，可替换)                    │
│       └── 未实现: DBStore, S3Store, MemoryStore             │
│                                                             │
│  plugins/freecad/plugin.md  ← 跟 WebSearch 同一生态位        │
│                                                             │
│  freecad.PreValidate()  ← 仅做参数验证，不做 MCP 通信       │
└─────────────────────────────────────────────────────────────┘
```

### 关键决策速查

| 决策 | 方向 | 理由 |
|------|------|------|
| MCP trace 是不是 CAD 专属 | ❌ 通用 | 所有 MCP 调用都需要追溯 |
| FreeCAD MCP Server 自己写还是用现成 | ❌ 用现成 | 跟 WebSearch 同一生态位 |
| 熔断事件要不要暴露 | ✅ 要 | 否则 trace 有盲区 |
| 熔断事件通道会不会阻塞 | ❌ 非阻塞 + 丢弃 | 熔断器不能因为 channel 满而卡死 |
| 框架还是应用负责存储 | ✅ 框架定接口，应用选实现 | 解耦，可测试，可替换 |
| 存储默认路径 | ✅ 无默认路径 | 不传就不存，不偷偷建目录 |
