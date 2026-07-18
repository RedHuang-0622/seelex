# Seelex 架构说明书 & 硬伤清单

> 版本: v1.0  
> 创建日期: 2025-07-18  
> 状态: 初稿  
> 覆盖范围: 项目主体架构、依赖拓扑、已知硬伤及改进建议

---

## 目录

1. [项目概述](#1-项目概述)
2. [架构全景](#2-架构全景)
3. [第 1 层：入口与依赖装配](#3-第-1-层入口与依赖装配)
4. [第 2 层：Agent & 工具系统](#4-第-2-层agent--工具系统)
5. [第 3 层：引擎与会话持久化](#5-第-3-层引擎与会话持久化)
6. [第 4 层：会话管理](#6-第-4-层会话管理)
7. [第 5 层：Skill 系统](#7-第-5-层skill-系统)
8. [第 6 层：TUI 模块](#8-第-6-层tui-模块)
9. [第 7 层：命令注册](#9-第-7-层命令注册)
10. [数据流](#10-数据流)
11. [硬伤清单](#11-硬伤清单)
12. [改进路线图](#12-改进路线图)

---

## 1. 项目概述

**Seelex** 是 [Seele 引擎](https://github.com/RedHuang-0622/Seele) 的 TUI 客户端。项目使用 [Bubble Tea](https://github.com/charmbracelet/bubbletea) 框架，遵循 Elm 架构（Model → Update → View），为 Seele 的 Agent、Engine、工作流系统提供交互式终端界面。

### 技术栈

| 层次 | 依赖 |
|------|------|
| TUI 框架 | `charmbracelet/bubbletea` + `bubbles` + `lipgloss` |
| 后端引擎 | `github.com/RedHuang-0622/Seele` 的 engine、agent、types |
| 上下文系统 | `seelectx`（storage、tracer、compactor、merger、snapshot） |
| 会话管理 | 本地 `session` 包 |
| 技能系统 | 本地 `skill` 包（加载器 + 注册表模式） |
| 构建 | Go 1.22+（使用 `slog`、`maps`、`slices` 等 stdlib 改进） |

### 仓库根目录文件拓扑

```
seelex/
├── main.go                     # 入口、8 层装配
├── go.mod / go.sum
├── README.md / DESIGN.md
├── seele.yaml                  # 技能声明
├── smoke_test.go               # 冒烟测试
├── config/                     # LLM 账户配置（YAML）
│   └── account-openai.yaml
├── session/                    # 会话管理器（薄封装）
│   └── manager.go
├── skill/                      # 技能系统（注册表 + 加载器）
│   ├── registry.go
│   └── loader.go
├── tui/                        # TUI 核心（Bubble Tea Model/Update/View）
│   ├── model.go / model_*.go
│   ├── update.go / update_*.go
│   ├── view.go / view_*.go
│   ├── command.go / command_*.go
│   ├── state.go
│   ├── stream.go
│   ├── tui.go
│   ├── keys.go
│   ├── constants.go
│   ├── styles.go
│   ├── cmds/                   # builtin 命令集合
│   │   └── builtin.go
│   ├── approve/                # 审批 UI（Bubble Tea 子模型）
│   │   └── approve.go
│   └── components/             # 可复用 UI 组件
│       ├── command_executor.go
│       ├── command_suggestions.go
│       └── spinner.go
├── context/                    # 上下文工具函数（有待改进）
│   ├── context.go
│   └── context_test.go
├── compactor/                  # 上下文压缩器
│   └── compactor.go
├── merger/                     # 上下文合并器
│   └── merger.go
├── snapshot/                   # 快照管理
│   └── snapshot.go
├── provider/                   # LLM Provider 适配
│   └── provider.go
├── seelexctx/                  # 上下文工具链入口
│   └── ctx.go
└── docs/                       # 架构文档
    ├── architecture-and-flaws.md  # ← 本文档
    ├── approve-research.md
    └── context-improvement-plan.md
```

---

## 2. 架构全景

```
┌─────────────────────────────────────────────────────────────────────┐
│                      main.go (8 层装配)                             │
│                                                                     │
│  Layer 1 ─►  LLM 配置加载 + Agent 创建                              │
│  Layer 2 ─►  工具注册 (builtin + switch_mode + ask_approve)        │
│  Layer 3 ─►  Engine + 存储 + Tracer                                │
│  Layer 4 ─►  会话管理 (session.Manager)                             │
│  Layer 5 ─►  Skill 系统 (Registry + Loader)                        │
│  Layer 6 ─►  TUI Model 创建                                         │
│  Layer 7 ─►  命令注册 + Suggestion 同步                             │
│  Layer 8 ─►  Bubble Tea Program 启动                                 │
└─────────────────────────────────────────────────────────────────────┘

                         ▼
┌─────────────────────────────────────────────────────────────────────┐
│                     TUI (Bubble Tea)                                │
│  ┌──────────────┐    ┌──────────────┐    ┌───────────────────┐     │
│  │  Model        │    │  Update      │    │  View             │     │
│  │  (state)      │───►│  (msg→cmd)   │───►│  (渲染)            │     │
│  └──────┬───────┘    └──────┬───────┘    └───────────────────┘     │
│         │                   │                                       │
│         ▼                   ▼                                       │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │  Commands: SendMessage / ExecuteCommand / Suggest           │  │
│  │  Components: Executor / Suggester / Spinner / Approve      │  │
│  └──────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────┘

                         ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      Seele Engine                                   │
│  ┌──────────┐  ┌──────────┐  ┌────────────────────────────────┐    │
│  │  Agent    │  │  Store   │  │  Context Pipeline:             │    │
│  │  (LLM+)   │  │  (KV)    │  │  Compactor → Merger → Snapshot │    │
│  └──────────┘  └──────────┘  └────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 3. 第 1 层：入口与依赖装配

### 位置
`main.go` (全部逻辑在 `main()` 函数中)

### 职责
1. 加载 LLM 账户配置（YAML）
2. 创建 `agent.Agent` 实例
3. 创建 `api.ChatClient` 并注入账户池

### 关键代码

```go
result, err := api.LoadFullAccountsConfig(*configPath)
agt, err := agent.New(agent.Options{...})
chatClient := agt.LLM().(*api.ChatClient)  // 类型断言
chatClient.WithAccountPool(pool)
```

### 依赖关系
- `agent.New` → 需要 `LLMConfig`
- `chatClient` → 需要 `AccountPool`
- 无抽象接口，直接使用具体类型

### ⚠️ 硬伤

| ID | 问题 | 位置 | 严重性 |
|----|------|------|--------|
| ARC-001 | **`main()` 函数过长**：整个 8 层装配全部在单一函数中，约 200+ 行 | main.go | 中 |
| ARC-002 | **硬类型断言**：`agt.LLM().(*api.ChatClient)` 假设 Agent 内部必定是 `*api.ChatClient` | main.go L38 | 高 |
| ARC-003 | **无依赖注入容器**：全部手动穿线，增加新组件需修改 main.go | main.go | 低 |

---

## 4. 第 2 层：Agent & 工具系统

### 位置
`main.go`（工具注册逻辑）+ Seele 项目的 `agent` 包

### 职责
1. 注册 builtin 工具（文件读写、搜索等）
2. 注册 `switch_mode` 工具（控制工具可见性）
3. 注册 `ask_approve` 工具（权限审批）
4. 初始化权限门控（Permission Gate）

### 关键模式

**工具注册**：所有工具通过 `agt.RegisterTool()` 注册，每个工具需提供 name、description、JSON schema、handler。

**switch_mode 工具**：通过 `agt.Tools().ActivatePlugin(mode)` 动态切换可见工具集。但注意——工具注册后会**常驻在 Agent 的工具管理中**，不可移除，只能隐藏。

**ask_approve 工具**：复用 Seele 的 `sugar/approve.Question` 结构体，调用 `tuiApprove.Ask()` 弹出 Bubble Tea 子模型进行交互。

### ⚠️ 硬伤

| ID | 问题 | 位置 | 严重性 |
|----|------|------|--------|
| ARC-004 | **`switch_mode` handler 返回值不规范**：错误时返回 JSON string 而非 error；`ActivatePlugin` 失败时返回包含 error 的 JSON | main.go ~L110 | 中 |
| ARC-005 | **`ask_approve` IDs 使用 `time.Now().UnixNano()`**：非确定性、低碰撞风险但不可读 | main.go ~L145 | 低 |
| ARC-006 | **没有工具注册的单元测试**：smoke_test.go 仅验证构建和 LLM 连接 | smoke_test.go | 高 |

---

## 5. 第 3 层：引擎与会话持久化

### 位置
`main.go`（装配） + `engine` 包（Seele 项目）

### 职责
1. 创建 `storage.Store`（KV 存储，默认使用当前目录）
2. 创建 `engine.Engine`，注入 Store 和 Tracer
3. 配置 `engine.WithHooks(tui.CreateToolHooks())`

### 关键代码

```go
store, err := storage.NewStore("")
eng := engine.New(agt, engine.WithStore(store),
    engine.WithTracer(tracer.NewSimpleTracer()),
    engine.WithHooks(tui.CreateToolHooks()),
)
```

### ⚠️ 硬伤

| ID | 问题 | 位置 | 严重性 |
|----|------|------|--------|
| ARC-007 | **存储路径硬编码**：`storage.NewStore("")` 使用空字符串，默认存储位置不明确 | main.go L161 | 中 |
| ARC-008 | **`InjectSaveLoad` 回调模式**：session 通过注入回调与 engine 耦合，而非直接依赖接口 | session/manager.go | 中 |

---

## 6. 第 4 层：会话管理

### 位置
`session/manager.go`

### 职责
1. 管理会话列表（创建、选择、删除）
2. 提供 `InjectSaveLoad` 方法注入持久化回调
3. 与 TUI Model 交互（当前会话 ID、会话切换）

### 关键类型

```go
type Manager struct {
    store      *storage.Store
    saveFn     func(string) error
    loadFn     func(string) error
    currentID  string
    sessions   []string
}
```

### 数据流
```
TUI Model ──► session.Manager ──► store.Save/Load ──► Engine.History()
```

### ⚠️ 硬伤

| ID | 问题 | 位置 | 严重性 |
|----|------|------|--------|
| ARC-009 | **`InjectSaveLoad` 破坏可测试性**：通过 setter 注入行为而非构造函数 | session/manager.go | 高 |
| ARC-010 | **`loadFn` 实现为空循环**：`for _, msg := range messages { _ = msg }`，数据未实际加载到 Engine | main.go L172-176 | ⚠️ 严重 |
| ARC-010a | **`loadFn` 副作用不完整**：调用 `store.Load` 但未将消息注入 Engine 历史 | main.go L172-176 | 严重 |

---

## 7. 第 5 层：Skill 系统

### 位置
`skill/registry.go` + `skill/loader.go`

### 职责
1. `Registry`：全局技能注册表，支持添加 Loader、按名称查找
2. `Loader`：从文件系统加载技能定义（YAML）

### 关键类型

```go
type Registry struct {
    skills   map[string]*types.Skill
    loaders  []Loader
}

type Loader struct {
    name     string
    paths    []string
}
```

### 装配

```go
skillReg := skill.NewRegistry()
skillLoader := skill.NewLoader("skills", "cmd/repl/skills")
_ = skillReg.AddLoader(skillLoader)
```

### 注意
- `AddLoader` 返回值被忽略（`_`），错误被丢弃
- 技能加载路径：`"skills"` 和 `"cmd/repl/skills"` 两个路径

### ⚠️ 硬伤

| ID | 问题 | 位置 | 严重性 |
|----|------|------|--------|
| ARC-011 | **`AddLoader` 错误被丢弃**：如果加载失败，静默忽略 | main.go L187 | 高 |
| ARC-012 | **路径硬编码**：`"skills"`, `"cmd/repl/skills"` 直接写在 main.go | main.go L185 | 中 |
| ARC-013 | **Skill 系统无单元测试** | skill/*.go | 中 |

---

## 8. 第 6 层：TUI 模块

### 位置
`tui/` 包，核心文件：`model.go`, `update.go`, `view.go`, `state.go`, `tui.go`, `stream.go`

### 架构模式
Bubble Tea Elm 架构：

```
Model (State)
  │
  ├── Init() → Cmd
  ├── Update(msg) → (Model, Cmd)
  └── View() → string
```

### Model 结构（关键字段）

```go
type Model struct {
    // 引擎依赖
    eng             *engine.Engine          // ← 具体类型
    client          *api.ChatClient         // ← 具体类型
    agt             *agent.Agent            // ← 具体类型
    sessionMgr      *session.Manager        // ← 具体类型
    skillReg        *skill.Registry         // ← 具体类型

    // UI 状态
    messages        []Message
    input           tea.Model              // textarea
    spinner         tea.Model              // spinner
    help            tea.Model              // help
    suggEng         *SuggestionEngine

    // 内部状态
    mode            Mode
    width, height   int
    err             error
    state           *State
    // ...
}
```

### View 结构
```
┌──────────────────────────────────────────────┐
│  StatusBar (模式 + 会话 + 模型 + 消息数)      │
├──────────────────────────────────────────────┤
│                                              │
│  消息列表 (ChatMessages)                      │
│  ┌────────────────────────────────────────┐  │
│  │  User: ...                             │  │
│  │  Assistant: ...                        │  │
│  │  Tool: ...                             │  │
│  └────────────────────────────────────────┘  │
│                                              │
│  Input Area (textarea + 自动调整)             │
│                                              │
│  HelpBar (快捷键提示)                         │
└──────────────────────────────────────────────┘
```

### ⚠️ 硬伤

| ID | 问题 | 位置 | 严重性 |
|----|------|------|--------|
| ARC-014 | **Model 依赖具体类型而非接口**：`eng *engine.Engine`, `client *api.ChatClient`, `agt *agent.Agent` 等全部使用具体类型 | tui/model.go | ⚠️ 严重 |
| ARC-015 | **Model 字段膨胀**：~20+ 字段，违反单一职责 | tui/model.go | 中 |
| ARC-016 | **`tui.go` 职责模糊**：包含事件总线、ToolHooks 创建等横切逻辑 | tui/tui.go | 中 |
| ARC-017 | **无 Model 单元测试**：Bubble Tea 的 model 可通过 `tea.NewProgram` 模拟测试 | tui/model.go | 高 |
| ARC-018 | **`stream.go` 的流处理可能阻塞**：在 Update 中直接处理流数据未使用 goroutine | tui/stream.go | 中 |
| ARC-019 | **`state.go` 状态管理不统一**：部分状态在 Model 字段，部分在 State 结构体 | tui/state.go | 中 |

---

## 9. 第 7 层：命令注册

### 位置
`tui/command.go` + `tui/cmds/builtin.go`

### 职责
1. 注册用户可执行的命令（`/help`, `/clear`, `/session`, `/skill` 等）
2. 提供命令建议引擎（`SuggestionEngine`）

### 装配

```go
tui.RegisterCommands(eng, chatClient, first.Model, sessionMgr, skillReg, skillLoader)
tui.SyncCommandSuggestions(m.SuggEng)
```

### ⚠️ 硬伤

| ID | 问题 | 位置 | 严重性 |
|----|------|------|--------|
| ARC-020 | **`RegisterCommands` 参数过多**：6 个参数，违反"少参数"原则 | tui/command.go | 中 |
| ARC-021 | **命令注册与 Model 紧耦合**：`RegisterCommands` 内部直接操作全局状态 | tui/command.go | 中 |
| ARC-022 | **`SyncCommandSuggestions` 是可变参数方法**：未来变更易破坏兼容性 | tui/command.go | 低 |

---

## 10. 数据流

### 主数据流：用户输入 → AI 响应

```
User Input
    │
    ▼
tui/update.go ──► HandleUserMessage()
    │
    ▼
tui/command.go ──► ExecuteCommand()  ← 若以 '/' 开头
    │
    ▼  (否则)
tui/command.go ──► SendMessage()
    │
    ▼
engine.Engine ──► agent.Agent ──► LLM API
    │                               │
    │                               ▼
    │                          Tool Calls (循环)
    │                               │
    │                               ▼
    │                          Stream Response
    │                               │
    ▼                               ▼
tui/stream.go ◄─────────────── streaming events
    │
    ▼
tui/update.go ──► 消息追加到 Model.messages
    │
    ▼
tui/view.go ──► 渲染到终端
```

### 上下文管道数据流（Seele 端）

```
Agent History
    │
    ▼
Compactor ──► 压缩冗余消息（合并连续 user/assistant）
    │
    ▼
Merger ────► 合并上下文窗口（按 token 限制裁剪）
    │
    ▼
Snapshot ──► 快照管理（保存/恢复上下文）
```

### 会话持久化数据流

```
Session Switch
    │
    ▼
session.Manager ──► loadFn ──► store.Load(sessionID)
    │                               │
    │                               ▼
    │                          Engine.ClearHistory()
    │                               │
    │                               ▼
    │                          (空循环，消息未回填 — ARC-010a)
    │
    ▼
session.Manager ──► saveFn ──► store.Save(sessionID)
                                    │
                                    ▼
                               Engine.History() → KV Store
```

---

## 11. 硬伤清单

所有硬伤按严重性分组，完整列表：

### 🔴 严重 (必须修复)

| ID | 描述 | 位置 | 状态 |
|----|------|------|------|
| ARC-014 | Model 依赖具体类型 | tui/model.go | ❌ |
| ARC-010a | loadFn 空循环 → eng.AppendHistory | main.go | ✅ |
| ARC-002 | 硬类型断言 → 安全断言 | main.go | ✅ |
| ARC-010 | InjectSaveLoad 破坏可测试性 | session/manager.go | ❌ |

### 🟡 高 (建议尽快修复)

| ID | 描述 | 位置 | 状态 |
|----|------|------|------|
| ARC-006 | 无单元测试覆盖工具注册 | smoke_test.go | ❌ |
| ARC-011 | AddLoader 错误不忽略 | main.go | ✅ |
| ARC-017 | 无 Model 单元测试 | tui/model.go | ❌ |
| ARC-001 | main() 函数过长 → 拆分为 initXXX | main.go | ✅ |

### 🔵 中 (可排期修复)

| ID | 描述 | 位置 | 状态 |
|----|------|------|------|
| ARC-004 | switch_mode 返回 error 而非 JSON | main.go | ✅ |
| ARC-007 | 存储路径 -store 标志 | main.go | ✅ |
| ARC-008 | 回调模式耦合 | session/manager.go | ❌ |
| ARC-012 | 技能路径 -skills 标志 | main.go | ✅ |
| ARC-015 | Model 字段膨胀 → 子包拆分 | tui/ | ✅ |
| ARC-016 | tui.go 职责模糊 → 子包拆分 | tui/ | ✅ |
| ARC-018 | 流处理可能阻塞 | tui/stream.go | ❌ |
| ARC-019 | 状态管理不统一 | tui/state.go | ❌ |
| ARC-020 | RegisterCommands 参数多 | tui/command.go | ❌ |
| ARC-021 | 命令注册紧耦合 | tui/command.go | ❌ |

### 🟢 低 (可暂缓)

| ID | 描述 | 位置 | 状态 |
|----|------|------|------|
| ARC-005 | ask_approve ID 使用 UnixNano | main.go | ❌ |
| ARC-013 | Skill 系统无测试 | skill/*.go | ❌ |
| ARC-022 | SyncCommandSuggestions 可变参数 | tui/command.go | ❌ |

---

## 12. 改进路线图

### Phase 1 — 修复功能性缺陷 ✅

1. ✅ **ARC-010a**: `loadFn` 通过 `eng.AppendHistory()` 注入消息
2. ✅ **ARC-002**: 用 `initChatClient()` 做安全断言

### Phase 2 — 提升可测试性

3. **ARC-014**: 为 Model 依赖引入接口
   - 提取 `EngineInterface`, `ChatClientInterface`, `AgentInterface`, `SessionManagerInterface`, `SkillRegistryInterface`
4. **ARC-006 + ARC-017**: 补充单元测试
   - smoke_test → 工具注册验证
   - tui/model_test.go → Bubble Tea model 单元测试

### Phase 3 — 代码组织优化

5. ✅ **ARC-001**: `main()` 拆分为 `initAgent/Store/Engine/Session/Skill/TUI/Commands/Plugins` 等
6. ✅ **ARC-015 + ARC-016**: Model 拆分 → `tui/approve/ splash/ sugg/ commands/` 子包

### Phase 4 — 代码质量提升

7. ✅ **ARC-004**: switch_mode 返回 error；✅ **ARC-007**: 存储路径 `-store`；✅ **ARC-012**: 技能路径 `-skills`
8. ✅ **ARC-011**: `AddLoader` 错误不忽略
9. **ARC-020, 021**: 简化命令注册

---

> **文档维护者**: Seelex 团队  
> **更新方式**: 每次架构变更后需同步更新此文档  
> **关联文档**: [context-improvement-plan.md](context-improvement-plan.md), [approve-research.md](approve-research.md)
