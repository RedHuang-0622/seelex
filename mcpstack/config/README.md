# MCP Config

## 生态位

`mcpstack/config` 从**独立的 MCP 配置文件 `mcp.yaml`** 加载 MCP 服务器清单
（根级 `mcp_servers:` 列表，2026-10 从账号档拆出）。
它只负责解析与校验，不依赖运行时；注册由 composition root（`main.go`）
转换为 `seelebridge.MCPServer` 并调用 `RegisterLazyMCP`，从而保持
`mcpstack → seelebridge` 单向依赖。

路径不由本包决定：`main.go` 用 `bootseed` 责任链（`config/mcp.yaml` →
`mcp.yaml` → `<exe>/config/mcp.yaml`）解析，四处全缺时用内嵌默认档就地初始化
一份空清单，之后只读用户那份。详见 `config/README.md`。

## 数据流图

```mermaid
flowchart LR
    YAML["config/mcp.yaml 的 mcp_servers 列表"] --> LOAD["mcpstack/config：解析与校验"]
    LOAD --> CFG["MCP 服务器配置（传输中立）"]
    CFG --> MAIN["main.go：转换为 seelebridge.MCPServer"]
    MAIN --> LAZY["RegisterLazyMCP<br/>冷启动只登记，不建连接"]
    LAZY --> MGR["seelebridge/mcp.Manager"]
    NOTE["方向：mcpstack → seelebridge 单向<br/>本包不依赖运行时"] -.-> CFG
```

## 验证

```text
go test ./mcpstack/config -count=1
```
