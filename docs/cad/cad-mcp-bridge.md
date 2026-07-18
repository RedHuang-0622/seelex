# MCP 连接桥 — mcp 包详细设计

> **⚠️ 本文已过时**
>
> 本文描述的自研 MCP 客户端方案已废弃。Seelex 复用 Seele 框架的 `mcp.Provider`（Attach/Detach/Refresh + 熔断器），通过 `seelebridge.Runtime.AttachMCP()` 装配。MCP Server 的调用链路见 [`../arch/mcp-call-chain-flowchart.md`](../arch/mcp-call-chain-flowchart.md)。

> 版本: v1.0  
> 创建日期: 2025-07-18  
> 状态: ❌ 已废弃（复用框架 mcp.Provider，不自研）  
> 关联文档: `cad-architecture-overview.md`, `cad-command-stack.md`, `cad-freecad-executor.md`

---

## 目录

1. [设计目标](#1-设计目标)
2. [MCP 协议概要](#2-mcp-协议概要)
3. [客户端架构](#3-客户端架构)
4. [工具注册](#4-工具注册)
5. [Transport 层](#5-transport-层)
6. [生命周期管理](#6-生命周期管理)
7. [错误处理](#7-错误处理)
8. [现有依赖](#8-现有依赖)
9. [完整代码骨架](#9-完整代码骨架)
10. [与 FreeCAD Server 的约定](#10-与-freecad-server-的约定)

---

## 1. 设计目标

- **标准协议**：遵循 Anthropic MCP 规范（JSON-RPC 2.0 over stdio）
- **松耦合**：Go 端不依赖 FreeCAD 的任何库，通过 MCP Client 调用
- **可替换**：切换不同 CAD 后端只需换 MCP Server（FreeCAD → SolidWorks → Blender）
- **工具即服务**：每个 CAD 操作对应一个 MCP Tool，注册到 Seele 的 ToolHolder
- **异步高效**：进程管理 + 请求超时 + 优雅关闭

### 非目标

- 不实现 MCP Server 端（那是 Python 的职责）
- 不处理 FreeCAD 内部状态（Server 自己管理 FreeCAD 文档）
- 不实现传输加密（stdio 本地通信，不需要 TLS）

---

## 2. MCP 协议概要

MCP 基于 JSON-RPC 2.0，使用 stdio 作为传输层。

### 2.1 协议流程

```
Seelex (Go)                          FreeCAD MCP Server (Python)
    │                                        │
    │  1. initialize (能力协商)               │
    │ ──────────────────────────────────────►│
    │ ◄──────────────────────────────────────│  {serverInfo, capabilities}
    │                                        │
    │  2. tools/list (获取工具列表)           │
    │ ──────────────────────────────────────►│
    │ ◄──────────────────────────────────────│  [{name, description, inputSchema}, ...]
    │                                        │
    │  3. 循环：tools/call (执行操作)          │
    │ ──────────────────────────────────────►│
    │    {name, arguments}                   │
    │ ◄──────────────────────────────────────│
    │    {content: [{type, text}]}           │
    │                                        │
    │  4. 退出                               │
    │ ──────────────────────────────────────►│  shutdown
```

### 2.2 消息格式

```json
// 请求
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "sketch_rectangle",
    "arguments": {
      "plane": "XY",
      "length": 100,
      "width": 50
    }
  }
}

// 响应
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "{\"sketch_id\": 1, \"status\": \"ok\"}"
      }
    ]
  }
}

// 错误响应
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32000,
    "message": "sketch_rectangle failed",
    "data": {"detail": "Invalid plane: XY_typo"}
  }
}
```

---

## 3. 客户端架构

### 3.1 核心类型

```go
package mcp

import (
    "bufio"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "os/exec"
    "sync"
    "time"

    "github.com/google/uuid"
)

// Client MCP 客户端，管理一个 MCP Server 子进程。
type Client struct {
    mu       sync.Mutex
    cmd      *exec.Cmd         // MCP Server 子进程
    stdin    io.WriteCloser    // 子进程 stdin
    stdout   *bufio.Scanner    // 子进程 stdout（逐行读取）
    stderr   io.ReadCloser     // 子进程 stderr（日志）
    requests map[string]chan *Response // 等待响应的请求
    caps     Capabilities      // 服务器能力（initialize 后获取）
    info     ServerInfo        // 服务器信息
    done     chan struct{}     // 进程退出通知
}

// Capabilities MCP 服务器能力声明
type Capabilities struct {
    Tools    *ToolsCapability    `json:"tools,omitempty"`
    Resources *ResourcesCapability `json:"resources,omitempty"`
}

type ToolsCapability struct {
    ListChanged bool `json:"listChanged"`
}

type ServerInfo struct {
    Name    string `json:"name"`
    Version string `json:"version"`
}

// Response MCP 响应
type Response struct {
    ID      string           `json:"id"`
    Result  *json.RawMessage `json:"result,omitempty"`
    Error   *MCPError        `json:"error,omitempty"`
}

type MCPError struct {
    Code    int             `json:"code"`
    Message string          `json:"message"`
    Data    json.RawMessage `json:"data,omitempty"`
}
```

### 3.2 关键接口

```go
// Connector 定义 MCP 连接的生命周期。
type Connector interface {
    // Start 启动 MCP Server 子进程并完成初始化握手。
    Start(ctx context.Context) error

    // CallTool 调用一个 MCP 工具，返回结果文本。
    // timeout 控制单次调用的最大等待时间。
    CallTool(ctx context.Context, name string, args map[string]interface{}, timeout time.Duration) (string, error)

    // ListTools 获取服务器支持的工具列表。
    ListTools(ctx context.Context) ([]ToolDefinition, error)

    // Close 关闭连接并终止子进程。
    Close() error

    // Info 返回服务器信息。
    Info() ServerInfo
}

// ToolDefinition MCP 工具定义 —— 将注册到 Seele 的 ToolHolder。
type ToolDefinition struct {
    Name        string      `json:"name"`
    Description string      `json:"description"`
    InputSchema any         `json:"inputSchema"`
}
```

---

## 4. 工具注册

### 4.1 MCP 工具 → Seele 工具的映射

```go
package mcp

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/RedHuang-0622/Seele/agent/core/tool/holder"
)

// RegisterAll 将 MCP Server 的所有工具注册到 Seele 的 ToolHolder。
// 在 Start() 之后调用，因为需要先获取工具列表。
func (c *Client) RegisterAll(tools *holder.Holder) error {
    defs, err := c.ListTools(context.Background())
    if err != nil {
        return fmt.Errorf("mcp: list tools: %w", err)
    }

    for _, def := range defs {
        // 闭包捕获 def
        d := def
        tools.Register(&holder.ToolDef{
            Name:        "cad_" + d.Name,     // 带 cad_ 前缀避免命名冲突
            Description: d.Description,
            Parameters:  d.InputSchema,
            Handler: func(ctx context.Context, argsJSON string) (string, error) {
                var args map[string]interface{}
                if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
                    return "", fmt.Errorf("mcp: %s: parse args: %w", d.Name, err)
                }
                return c.CallTool(ctx, d.Name, args, 120*time.Second)
            },
        })
    }
    return nil
}

// RegisterSelective 选择性注册（只注册白名单中的工具）。
func (c *Client) RegisterSelective(tools *holder.Holder, whitelist []string) error {
    defs, err := c.ListTools(context.Background())
    if err != nil {
        return fmt.Errorf("mcp: list tools: %w", err)
    }

    whiteSet := make(map[string]bool, len(whitelist))
    for _, name := range whitelist {
        whiteSet[name] = true
    }

    for _, def := range defs {
        if !whiteSet[def.Name] {
            continue
        }
        d := def
        tools.Register(&holder.ToolDef{
            Name:        "cad_" + d.Name,
            Description: d.Description,
            Parameters:  d.InputSchema,
            Handler: func(ctx context.Context, argsJSON string) (string, error) {
                var args map[string]interface{}
                if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
                    return "", fmt.Errorf("mcp: %s: parse args: %w", d.Name, err)
                }
                return c.CallTool(ctx, d.Name, args, 120*time.Second)
            },
        })
    }
    return nil
}
```

### 4.2 注册后的工具命名示例

| MCP 工具名 | Seele 工具名 | 描述 |
|------------|-------------|------|
| `sketch_rectangle` | `cad_sketch_rectangle` | 创建矩形草图 |
| `sketch_circle` | `cad_sketch_circle` | 创建圆形草图 |
| `pad_extrude` | `cad_pad_extrude` | 拉伸凸台 |
| `pocket_circular` | `cad_pocket_circular` | 圆形开孔 |
| `fillet` | `cad_fillet` | 倒圆角 |
| `chamfer` | `cad_chamfer` | 倒角 |
| `export_stl` | `cad_export_stl` | 导出 STL |

---

## 5. Transport 层

### 5.1 stdio Transport

```go
// Start 启动 MCP Server 子进程并完成初始化。
func (c *Client) Start(ctx context.Context) error {
    c.mu.Lock()
    defer c.mu.Unlock()

    // 构造命令
    c.cmd = exec.CommandContext(ctx, "python3", "-m", "freecad_mcp_server")
    // 或者直接指定路径
    // c.cmd = exec.Command("python3", "freecad/server/server.py")

    stdin, err := c.cmd.StdinPipe()
    if err != nil {
        return fmt.Errorf("mcp: stdin pipe: %w", err)
    }
    c.stdin = stdin

    stdout, err := c.cmd.StdoutPipe()
    if err != nil {
        return fmt.Errorf("mcp: stdout pipe: %w", err)
    }
    c.stdout = bufio.NewScanner(stdout)
    c.stdout.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB 缓冲区

    stderr, err := c.stderrPipe()
    if err != nil {
        return fmt.Errorf("mcp: stderr pipe: %w", err)
    }
    c.stderr = stderr

    // 启动进程
    if err := c.cmd.Start(); err != nil {
        return fmt.Errorf("mcp: start: %w", err)
    }

    // 启动响应读取 goroutine
    go c.readResponses()

    // 初始化握手
    if err := c.initialize(ctx); err != nil {
        c.cmd.Process.Kill()
        return fmt.Errorf("mcp: initialize: %w", err)
    }

    return nil
}
```

### 5.2 JSON-RPC 消息收发

```go
// sendRequest 发送 JSON-RPC 请求并等待响应。
func (c *Client) sendRequest(ctx context.Context, method string, params interface{}, timeout time.Duration) (*Response, error) {
    id := uuid.New().String()

    req := map[string]interface{}{
        "jsonrpc": "2.0",
        "id":      id,
        "method":  method,
        "params":  params,
    }

    data, err := json.Marshal(req)
    if err != nil {
        return nil, fmt.Errorf("mcp: marshal request: %w", err)
    }

    // 注册等待通道
    ch := make(chan *Response, 1)
    c.mu.Lock()
    c.requests[id] = ch
    c.mu.Unlock()

    defer func() {
        c.mu.Lock()
        delete(c.requests, id)
        c.mu.Unlock()
    }()

    // 发送
    if _, err := fmt.Fprintf(c.stdin, "%s\n", data); err != nil {
        return nil, fmt.Errorf("mcp: write: %w", err)
    }

    // 等待响应（带超时）
    select {
    case resp := <-ch:
        return resp, nil
    case <-ctx.Done():
        return nil, ctx.Err()
    case <-time.After(timeout):
        return nil, fmt.Errorf("mcp: timeout after %v", timeout)
    }
}

// readResponses 持续读取子进程 stdout，将响应分发给对应的等待通道。
func (c *Client) readResponses() {
    for c.stdout.Scan() {
        line := c.stdout.Text()
        var msg json.RawMessage
        if err := json.Unmarshal([]byte(line), &msg); err != nil {
            continue // 忽略非 JSON 行
        }

        // 解析 ID
        var header struct {
            ID string `json:"id"`
        }
        if err := json.Unmarshal(line, &header); err != nil || header.ID == "" {
            continue // 忽略没有 ID 的消息
        }

        c.mu.Lock()
        ch, ok := c.requests[header.ID]
        c.mu.Unlock()

        if !ok {
            continue // 没有对应的等待者
        }

        var resp Response
        json.Unmarshal([]byte(line), &resp)
        ch <- &resp
    }

    // stdout 关闭，通知所有等待者
    c.mu.Lock()
    for _, ch := range c.requests {
        close(ch)
    }
    c.mu.Unlock()
    close(c.done)
}
```

### 5.3 初始化握手

```go
// initialize 完成 MCP 协议初始化。
func (c *Client) initialize(ctx context.Context) error {
    params := map[string]interface{}{
        "protocolVersion": "2024-11-05",
        "capabilities": map[string]interface{}{},
        "clientInfo": map[string]interface{}{
            "name":    "seelex",
            "version": "1.0.0",
        },
    }

    resp, err := c.sendRequest(ctx, "initialize", params, 10*time.Second)
    if err != nil {
        return err
    }

    if resp.Error != nil {
        return fmt.Errorf("mcp: initialize error: %s", resp.Error.Message)
    }

    // 解析服务器信息
    var result struct {
        ProtocolVersion string       `json:"protocolVersion"`
        Capabilities    Capabilities `json:"capabilities"`
        ServerInfo      ServerInfo   `json:"serverInfo"`
    }
    if err := json.Unmarshal(*resp.Result, &result); err != nil {
        return fmt.Errorf("mcp: parse initialize result: %w", err)
    }

    c.caps = result.Capabilities
    c.info = result.ServerInfo

    return nil
}
```

---

## 6. 生命周期管理

### 6.1 状态机

```
IDLE ──Start()──► INITIALIZING ──成功──► READY
                      │                    │
                      │ 失败               │ Close()
                      ▼                    ▼
                   ERROR                 CLOSED
```

### 6.2 优雅关闭

```go
// Close 优雅关闭 MCP 连接。
func (c *Client) Close() error {
    c.mu.Lock()
    defer c.mu.Unlock()

    // 发送 shutdown 通知
    shutdownMsg := map[string]interface{}{
        "jsonrpc": "2.0",
        "method":  "shutdown",
        "params":  map[string]interface{}{},
    }
    data, _ := json.Marshal(shutdownMsg)
    fmt.Fprintf(c.stdin, "%s\n", data)

    // 等待进程退出
    done := make(chan error, 1)
    go func() {
        done <- c.cmd.Wait()
    }()

    select {
    case <-done:
        // 正常退出
    case <-time.After(5 * time.Second):
        // 超时，强制 kill
        c.cmd.Process.Kill()
        <-done
    }

    return nil
}
```

---

## 7. 错误处理

### 7.1 错误分类

| 错误类型 | 原因 | 处理方式 |
|----------|------|----------|
| 连接错误 | 子进程启动失败 | 返回给调用者，记录日志 |
| 超时错误 | 操作执行超过时限 | 重试（可选）或失败 |
| 协议错误 | Server 返回 error 响应 | 解析错误信息，返回给 LLM |
| 进程崩溃 | Server 异常退出 | 自动重启（最多 N 次） |

### 7.2 自动重连

```go
// Client.WithAutoReconnect 启用自动重连。
// 当子进程意外退出时，自动重启并重新注册工具。
func (c *Client) WithAutoReconnect(tools *holder.Holder, maxRetries int) {
    go func() {
        <-c.done // 等待进程退出
        for i := 0; i < maxRetries; i++ {
            time.Sleep(time.Second * time.Duration(i+1))
            if err := c.Start(context.Background()); err != nil {
                continue
            }
            if err := c.RegisterAll(tools); err != nil {
                continue
            }
            return // 重连成功
        }
    }()
}
```

---

## 8. 现有依赖

`go.mod` 中已有 `github.com/mark3labs/mcp-go v0.54.0`。这个库提供了 MCP 协议的基础构建块，但我们的实现需要精简的 stdio 客户端，可以选择：

| 方案 | 评价 |
|------|------|
| ✅ **自实现精简客户端** | 基于 `mark3labs/mcp-go` 的底层类型，自己实现 stdio transport。代码量 ~300 行，无额外依赖 |
| ❌ 直接使用 `mark3labs/mcp-go` 的 Client | 该库设计偏向 HTTP/SSE，stdio 支持不完善 |
| ❌ 使用其他 MCP Go 库 | 额外依赖，不一定更稳定 |

**推荐方案**：自实现 stdio Client，参考 `mark3labs/mcp-go` 的底层类型定义（`InitializeRequest`, `CallToolRequest` 等）。

---

## 9. 完整代码骨架

```
mcp/
├── client.go          # Client 核心类型 + Start/Close
├── client_test.go     # 模拟子进程的单元测试
├── transport.go       # stdio 读写 + JSON-RPC 消息收发
├── transport_test.go  # 消息收发测试
├── tools.go           # ListTools / CallTool / RegisterAll / RegisterSelective
├── tools_test.go      # 工具注册测试（mock holder）
├── types.go           # ToolDefinition, Capabilities, Response 等类型
├── errors.go          # 自定义错误类型
└── reconnect.go       # 自动重连逻辑
```

---

## 10. 与 FreeCAD Server 的约定

### 10.1 进程接口

| 项目 | 约定 |
|------|------|
| 启动命令 | `python3 -m freecad_mcp_server` |
| 工作目录 | `freecad/server/` |
| 传输方式 | stdio（stdin=请求, stdout=响应, stderr=日志） |
| 协议版本 | MCP 2024-11-05 |

### 10.2 工具命名与参数

所有工具名使用蛇形命名（snake_case），参数使用 JSON 对象。

详细约定见 `cad-freecad-executor.md` 的 [工具契约] 部分。

### 10.3 错误代码

| 错误码 | 含义 |
|--------|------|
| -32700 | Parse error |
| -32600 | Invalid request |
| -32601 | Method not found |
| -32602 | Invalid params |
| -32603 | Internal error |
| -32000 | CAD operation failed |
| -32001 | FreeCAD not initialized |
| -32002 | Document not found |
