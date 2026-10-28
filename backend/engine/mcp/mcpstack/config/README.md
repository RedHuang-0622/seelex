# MCP Config

## 生态位

`mcpstack/config` 从**独立的 MCP 配置文件 `mcp.yaml`** 加载 MCP 服务器清单
（根级 `mcp_servers:` 列表，2026-10 从账号档拆出）。
它只负责解析与校验，不依赖运行时；注册由 [`boot`](../../../../../boot/README.md)
转换为 `backend/engine/mcp` 的 `Server` 并调 `Runtime` 的登记入口，
从而保持本包与生命周期包之间的单向依赖。

路径不由本包决定：启动装配用 `internal/bootseed` 责任链（`config/mcp.yaml` →
`mcp.yaml` → `<exe>/config/mcp.yaml`）解析，四处全缺时用内嵌默认档就地初始化
一份空清单，之后只读用户那份。详见 `config/README.md`。

## 数据流图

```mermaid
flowchart LR
    YAML["config/mcp.yaml 的 mcp_servers 列表"] --> LOAD["mcpstack/config：解析与校验"]
    LOAD --> CFG["MCP 服务器配置（传输中立）"]
    CFG --> MAIN["boot：转换为 backend/engine/mcp.Server"]
    MAIN --> LAZY["RegisterLazyMCP<br/>冷启动只登记，不建连接"]
    LAZY --> MGR["backend/engine/mcp.Manager"]
    NOTE["方向：本包不依赖运行时<br/>由装配侧单向调用"] -.-> CFG
```

## 验证

```text
go test ./backend/engine/mcp/mcpstack/config -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### mcpconfig.go

- `func Load(path string) []MCPServerConfig` — Load 从独立的 mcp.yaml 读取 mcp_servers 列表；文件缺失或解析失败返回 nil。

### mcpconfig_test.go

- `func TestLoadMCPServersConfig_NoFile(t *testing.T)`
- `func TestLoadMCPServersConfig_InvalidYAML(t *testing.T)`
- `func TestLoadMCPServersConfig_EmptyServers(t *testing.T)`
- `func TestLoadMCPServersConfig_FiltersEmptyNames(t *testing.T)`
- `func TestLoadMCPServersConfig_FullConfig(t *testing.T)`
- `func TestLoadMCPServersConfig_NoMCPServersSection(t *testing.T)`
- `func TestLoadMCPServersConfig_ToolNotes(t *testing.T)`
- `func TestRegisterMCPServers_EmptyServers(t *testing.T)`
