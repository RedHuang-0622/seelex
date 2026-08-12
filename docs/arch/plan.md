# Seelex 上下文、Skill、Plugin 与 Seele 薄封装实现方案

## 设计目标

1. 把 `snapshot/provider/compactor/merger` 归并到 `seelexctx/` 命名空间，明确 context 领域边界。
2. 把 Skill 从单文件升级为目录包：`skills/<name>/SKILL.md`，同目录可携带 bash、模板、引用资料和任意资源。
3. 新增 `plugins/<name>/plugin.md` 和插件私有 `skills/`，让插件切换同时管理工具可见性、Skill 集和 MCP Server 生命周期。
4. 复用 Seele 的 Agent、PluginManager、MCP Provider/client、账号、Storage、Trace、Builtin Tool；Seelex 不重写 MCP 协议。
5. 除 `engine` 允许直接使用外，业务包不再理解 Seele 深层包的入参出参，统一通过 `seelebridge` 或调用方窄接口访问。
6. 恢复全仓可构建、可测试状态，并为 Loader/Manager/Adapter 建立回归测试。

## 框架与应用的所有权边界

Seele 的定位是通用 Agent 开发框架；Seelex 是使用该框架构建的具体产品。因此遵循以下边界：

| 能力 | 所有者 | 原因 |
|---|---|---|
| Agent 生命周期、LLM、Tool Provider、MCP 协议/client、通用 PluginManager、Engine | Seele | 通用 Agent Framework primitive |
| `skills/<name>/SKILL.md` 目录规范 | Seelex | 产品内容和文件组织约定 |
| `plugins/<name>/plugin.md`、插件私有 Skill | Seelex | 产品插件清单与工作流 |
| 插件切换事务、MCP 与 Skill 联动 | Seelex | 应用生命周期编排 |
| TUI、命令、补全、状态栏 | Seelex | 产品交互 |
| Session 使用策略、context 快照与合并 | Seelex | 产品上下文模型 |
| 权限规则和审批体验 | Seelex 应用层；执行钩子由 Seele 框架提供 | 策略属于产品，调度拦截点属于框架 |

`seelebridge` 只承担 Anti-Corruption Layer：把 Seele 的框架 primitive 转换成 Seelex 稳定接口，不解析 `plugin.md`，不扫描 Skill，不持有 TUI 状态，不实现 Agent/Engine/MCP 协议。

## 已确认的 Seele 能力

| 能力 | Seele v0.0.1/main | Seelex 策略 |
|---|:---:|---|
| PluginManager 工具过滤 | 有 | 薄封装并由文件 Loader 驱动 |
| MCP stdio/SSE client/provider | 有 | 直接复用 `agent.MCP()`，不自研 client |
| MCP Server Attach/Detach/Refresh | 有 | 封装为 Seelex MCPBackend |
| 文件型 plugin.md Loader | 无 | Seelex 实现 |
| `SKILL.md` 目录 Loader | 无 | Seelex 实现 |
| PermissionConfig/SetPermissionConfig | 无 | 移除当前不可构建接线，保留扩展接口 |
| Provider 更新通知 Holder rebuild | 无 | Seelex MCP adapter 集中刷新 |

## 设计模式选择

| 模式 | Go 实现 | 应用位置 | 理由 |
|---|---|---|---|
| Adapter | `seelebridge.Runtime/Tools/Plugins/MCP/Store` | Seele 边界 | 隐藏第三方深层类型和签名 |
| Facade | `seelebridge.Runtime` | main/TUI | 提供常用的高层入口，但不做万能对象 |
| Strategy | 调用方定义 `PluginBackend/MCPBackend/SkillBackend` | `plugin.Manager` | 可 mock、可替换，不反向依赖 Seele |
| Factory | `seelebridge.NewRuntime`、Loader constructors | 装配 | 集中参数校验和依赖创建 |
| Repository | `skill.Loader`、`plugin.Loader` | 文件系统目录 | 隔离发现、解析和持久化 |
| Transaction/State Machine | `plugin.Manager.Activate` | 插件切换 | MCP/工具/Skill 切换失败时回滚 |
| Compatibility Adapter | 旧 `switch_mode` -> `switch_plugin` | 工具层 | 保留现有调用兼容性 |

## 方案对比

| 维度 | 方案 A：领域 Loader + 窄 Adapter（推荐） | 方案 B：单个 Runtime 大封装 |
|---|---|---|
| 耦合度 | 低：plugin/skill 定义接口，seelebridge 实现 | 中：所有包依赖一个 Runtime |
| 内聚性 | 高：目录解析、生命周期、第三方适配分离 | 低：Runtime 同时承担解析和业务状态 |
| 可测试性 | 高：Loader、Manager、Adapter 分别 mock | 中：测试需要构造较大的 Runtime |
| 实现成本 | 中高：文件和接口较多 | 中低：初期代码少 |
| 改动面 | 可按垂直切片迁移 | 一次性替换大量签名 |
| 可回滚性 | 高：每个切片独立 | 低：Runtime 成为中心依赖 |
| 后续 CAD Plugin | 易扩展 MCP/artifact/domain service | 容易继续膨胀 |

## 推荐：方案 A

理由：用户需要的是稳定的 Seelex 能力边界，而不是把 Seele 的所有类型换一个包名重新导出，更不是在 Seelex 再造一套 Agent Framework。Loader、插件事务和 Seele 适配应保持独立，调用方只依赖自己真正需要的方法。

最大风险：当前 permission 接线不存在于任何可获取的 Seele 版本。方案会先删除无法编译的 `permission` import/装配，保留审批组件和 `PermissionBackend` 扩展点；在 Seele 增加 gateway middleware 后恢复实际门控。该行为变化必须在最终报告中明确标红。

## 配置与目录契约

### Skill

```text
skills/<skill-name>/
├── SKILL.md             # 唯一入口，必需
├── scripts/             # 可选
├── references/          # 可选
└── 任意其他资源
```

规则：

- name 默认取目录名；`SKILL.md` 可用 YAML frontmatter 覆盖 description，不覆盖稳定 name。
- `Skill.RootDir` 是所有资源解析的安全根。
- `Create` 只创建新格式；Loader 在一个兼容周期内仍可读取旧 `skills/*.md`，同名时新格式优先。
- name 只允许小写字母、数字、`-`、`_`；resolved path 必须位于配置根目录内。

### Plugin

```text
plugins/<plugin-name>/
├── plugin.md
└── skills/<skill-name>/SKILL.md
```

`plugin.md`：

```markdown
---
schema_version: 1
name: mechanical-design
description: FreeCAD mechanical design
include: [switch_plugin, cad_*]
exclude: []
mcp_servers:
  - name: freecad
    transport: stdio
    command: FreeCADCmd
    args: ["-c", "server.py"]
---

# Mechanical Design
正文作为插件说明/提示词。
```

MCP `command` 和相对文件参数相对插件根目录解析；环境变量使用字符串列表，敏感值只允许从进程环境引用，不写入 plugin.md。

## 循环依赖检查

```text
main
├── engine (允许直接使用 Seele)
├── seelebridge
├── plugin
├── skill
├── session
└── tui

plugin -> 自己定义 PluginBackend/MCPBackend/SkillBackend interfaces
seelebridge -> Seele packages，仅实现上述接口，不 import plugin
tui/session -> 自己定义窄接口，不 import Seele 深层包
seelexctx -> 子包；子包不反向 import seelexctx 根包
```

- [x] 无 `plugin <-> seelebridge` 循环。
- [x] 无 `seelexctx <-> seelexctx/subpackage` 反向循环。
- [x] `seelebridge` 不依赖 TUI、Session、Plugin 业务实现。
- [x] Engine 保持装配入口和需要读取 History/Trace 的 context provider 直接使用。

## 核心接口

```go
// plugin 包：接口属于调用方。
type ToolPluginBackend interface {
    DefinePlugin(name, description string, include, exclude []string) error
    ActivatePlugin(name string) error
    DeactivatePlugin()
    ActivePlugin() string
}

type MCPBackend interface {
    Attach(ctx context.Context, cfg MCPServer) error
    Detach(name string) error
    Refresh(ctx context.Context, name string) error
    ServerNames() []string
}

type SkillBackend interface {
    ReplacePluginSkills(pluginName string, skills []skill.Skill)
    ClearPluginSkills(pluginName string)
}

type Manager interface {
    Load() error
    Activate(ctx context.Context, name string) error
    Deactivate(ctx context.Context) error
    Current() (Plugin, bool)
    All() []Plugin
}
```

```go
// session 包：不依赖 Seele storage 具体类型。
type Store interface {
    Save(sessionID string, messages []seelebridge.Message) error
    Load(sessionID string) ([]seelebridge.Message, error)
    List() []seelebridge.SessionMeta
    Delete(sessionID string) error
}
```

```go
// TUI 只看高层运行时视图。
type RuntimeView interface {
    VisibleTools(ctx context.Context) []seelebridge.Tool
    ActivePlugin() string
    Accounts() []seelebridge.Account
    SelectAccount(name string) bool
    Provider() string
}
```

## Plugin 激活事务

```text
Load target definition
  -> validate name/schema/MCP configs
  -> attach target-only MCP servers
  -> define/activate Seele tool plugin
  -> publish target plugin skills
  -> refresh Holder/TUI suggestions
  -> commit current plugin

任何一步失败：
  -> 清理本次新 attach 的 MCP
  -> 恢复旧 tool plugin
  -> 恢复旧 plugin skills
  -> 返回带阶段信息的 error
```

第一版 Manager 使用 mutex 串行 Activate/Deactivate；不使用包级全局状态。

## 实现步骤

| # | 步骤 | 文件 | 设计模式 | 验证 |
|---:|---|---|---|---|
| 1 | 恢复可构建框架边界，移除指向不存在 Seele API 的 permission 接线 | `main.go`, `seelebridge/*` | Adapter | `go build ./...` |
| 2 | 迁移 context 四包到 `seelexctx/` | `seelexctx/{snapshot,provider,compactor,merger}` | Package boundary | context 单测 |
| 3 | 实现目录型 Skill Loader，修复锁与路径安全 | `skill/*`, `skills/*` | Repository | skill 单测/并发测试 |
| 4 | 实现 Seele Runtime/accounts/tools/storage/trace facade | `seelebridge/*` | Adapter/Facade | adapter 单测 |
| 5 | 实现 MCP facade 并处理 Holder refresh | `seelebridge/mcp.go` | Adapter | fake provider/状态测试 |
| 6 | 实现 plugin.md Loader 与 schema 校验 | `plugin/loader.go` | Repository | table tests |
| 7 | 实现事务型 Plugin Manager | `plugin/manager.go` | State machine | 激活/回滚/并发测试 |
| 8 | 迁移 hardcoded modes 为目录插件 | `plugins/*/plugin.md` | Configuration | loader 集成测试 |
| 9 | 接入 main、TUI、命令和补全 | `main.go`, `tui/*` | DI | `/plugins`, `/plugin`, tool switch tests |
| 10 | 更新 README/docs/CI | 文档、workflow | — | diff/check |

## 测试策略

### 单元

- Skill：新旧格式、缺失 SKILL.md、同名优先级、frontmatter、资源根、Create/Delete、路径逃逸。
- Plugin Loader：合法/非法 schema、重复名、相对 MCP 路径、插件私有 Skill。
- Plugin Manager：成功切换、Attach 失败、Activate 失败、Skill publish 失败、回滚、重复激活、Deactivate。
- Seelebridge：账号空池、插件映射、MCP refresh、Storage DTO、Runtime shutdown。

### 集成

- 使用 fake MCPBackend，不依赖真实 MCP/LLM。
- 从临时目录加载两套插件，切换后断言工具过滤和 Skill 集合。
- Engine 相关测试使用现有 engine API，但不访问真实 LLM。

### 并发

- 多 goroutine 并发 Activate/Deactivate，验证串行化和最终一致性。
- Skill/Plugin Loader 并发读取、Reload。
- Linux/可用 CGO 环境运行 `go test -race`。

### 覆盖率目标

- `skill`、`plugin`、`seelebridge` 新代码 ≥85%。
- context 迁移不降低现有单包覆盖率。
- 全仓覆盖率会受 TUI 旧代码 0% 影响，在报告中单列，不伪报达标。

## 回滚方案

1. context 迁移按独立 commit，回滚只恢复 import path。
2. Skill Loader 保留旧格式只读兼容，目录迁移可单独回滚。
3. Plugin Manager 接入前保留 hardcoded definitions 的兼容构造器；若目录加载失败可启动内置安全插件。
4. MCP facade 不修改 Seele 源码；可回滚为直接调用 `Agent.MCP()`。
5. `switch_mode` 兼容别名保留一个版本，避免旧提示词/工具调用失效。

## 明确不在本次范围

- FreeCAD MCP Server 和机械设计命令栈实现。
- 修改或发布 Seele 仓库。
- 在 Seelex 内重写 Agent、Engine、MCP client 或 Tool Holder。
- 完整重做权限系统；本次只移除不存在 API 导致的构建阻断，保留 Seelex 权限策略/审批 UI 和框架执行钩子接口。
- 一次性封装所有从未使用的 Seele API。

## 计划确认点

编码将按上述十个垂直步骤进行。需要特别确认：当前可获取的 Seele tag 和 main 都没有 permission API，因此本次会暂时移除不可构建的权限接线，但不会删除审批 UI；待 Seele 提供 gateway middleware 后再通过 `seelebridge` 恢复。
