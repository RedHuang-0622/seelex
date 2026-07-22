# Seelex — 可切换专业形态的工科全栈 Agent

**Seelex** 是一个面向工程研发、设计与交付全过程的全栈 Agent。它以 [Seele](https://github.com/RedHuang-0622/Seele) 作为 Agent 引擎，通过可声明、可组合、可运行时切换的 Plugin，让同一个 Agent 像切换"形态"一样进入不同专业工作域。

Seelex 的目标不是只做一个 TUI，也不是只做 CAD：

- **Seele** 提供 LLM、工具调度、MCP、存储和运行时基础能力；
- **Seelex Application Core** 负责任务状态、命令、Plugin、Skill、会话、审批和事件编排；
- **Plugin** 定义 Agent 当前的专业形态，例如基础读写、CAD、软件开发和后续工程领域；
- **CAD** 是首个重点专业增幅方向，用于验证复杂工程工具链，而不是产品边界；
- **TUI** 是高效率 CLI 工作入口；
- **Electron** 面向毕业设计、课程项目、成果展示和更完整的可视化交付体验。

> 当前阶段重点是稳定 Agent 内核与 Plugin 切换机制。CAD、Dev 和 Electron 将作为建立在同一内核上的产品形态逐步落地。

## 功能特性

### 🧠 多模型 LLM 引擎
- 支持 **OpenAI / Anthropic / DeepSeek** 等主流 API 和定制 Provider
- 多账号 **Round-Robin 轮询**，自动故障切换
- 可配置 `max_tokens`、`temperature`、`timeout` 等模型参数

### 🔌 Plugin 形态切换系统
- 运行时通过 `switch_plugin` 或 `/plugin <name>` 切换 Agent 专业形态
- 每个 Plugin 限定 **工具集 + Skill + System Prompt + MCP Server**
- 内置 7 个基础 Plugin：`default` / `read` / `write` / `git` / `shell` / `plan` / `freecad`
- 事务式激活/停用，失败自动回滚

### 📜 Skill 技能系统
- 目录化加载，Skill 位于对应 Plugin 目录下（`plugins/<name>/<skill>/SKILL.md`）
- 多层 **PromptStack** 叠加（identity → plugin → effort → instructions → skill）
- 运行时通过 `#skillname` 加载、`#end` 退栈
- 支持全局 Skill 和 Plugin 专属 Skill

### ⚡ Effort 等级控制
- **4 档思考深度**：`lite`(只读速答) / `medium`(标准工具) / `high`(完整ReAct) / `max`(深度多Agent)
- 控制 MaxLoops（20/64/512/1024）和 PromptStack 行为指令注入
- 状态栏实时显示 `E:high`，`Alt+E` 快捷循环切换

### 🖥️ TUI 交互终端
- 基于 **Bubble Tea** 构建，流式聊天、命令补全、工具事件面板
- 命令系统：`/help`、`/model`、`/plugins`、`/effort`、`/sessions`、`/new`
- 历史、追踪、账号池管理和 Plugin 列表查看

### 🔧 跨平台可执行
- 预编译 **Windows / Linux / macOS**（amd64 + arm64）二进制
- 静态编译（CGO_ENABLED=0），**零运行时依赖**，即下即用
- 体积 16-17 MB/平台

## 产品模型

```text
                         ┌─────────────────────────────┐
                         │        Seelex Agent         │
                         │  reasoning / state / event  │
                         └──────────────┬──────────────┘
                                        │ switch_plugin
                ┌───────────────────────┼───────────────────────┐
                ▼                       ▼                       ▼
        ┌──────────────┐        ┌──────────────┐        ┌──────────────┐
        │ Base Plugins │        │  CAD Plugin  │        │  Dev Plugin  │
        │ read/write…  │        │ design/CAM…  │        │ code/test…   │
        └──────┬───────┘        └──────┬───────┘        └──────┬───────┘
               └───────────────────────┼───────────────────────┘
                                       ▼
                         Seele Tools / MCP / Skills

 Bubble Tea TUI ── AppController ──┐
                                    ├── application.Service
 Electron UI ───── JSON-RPC/stdio ──┘
```

Plugin 不是皮肤或提示词别名，而是一套专业能力边界：

- 可见工具集合；
- 专属 Skill；
- System Prompt；
- MCP Server；
- 后续可扩展的权限、产物类型和生命周期策略。

## 当前状态

| 能力 | 状态 | 说明 |
|------|:---:|------|
| Headless Application Core | ✅ | TUI 已与业务状态、副作用和异步生命周期分离 |
| TUI 客户端 | ✅ | 支持流式聊天、命令、补全、工具事件、交互面板、Alt+E effort 循环 |
| 文件化 Plugin | ✅ | `plugin.md` 定义工具过滤、Prompt、Skill 和 MCP |
| Plugin 运行时切换 | ✅ | 支持激活、停用、失败回滚和并发串行化 |
| Skill 系统 | ✅ | 支持目录加载、注册、Plugin Skill 和多层 PromptStack 叠加 |
| Effort 等级 | ✅ | lite/medium/high/max 四档，控制 MaxLoops、提示词深度和工具可见性 |
| PromptStack 分层 | ✅ | system prompt 按 identity → plugin → effort → instructions → skill 五层组装 |
| 审批交互 Broker | ✅ | `ask_approve` 和前端决议、TUI 交互面板完整实现；基础 permission gate 已接线 |
| Plan/WorkPlan 可视化 | ✅ | TUI 四级 Effort Plan 面板（单行/打点表/节点树/全框表）+ 进度回调实时更新 |
| 系统诊断 /diag | ✅ | Go 运行时、内存、Plugin、Account、Skill 完整列出 |
| 会话保存与列表 | ✅ | 支持 `/new` 保存和 `/sessions` 查询 |
| 会话恢复 | ⛔ | Seele 暂无历史替换 API，`/resume` 会明确提示不可用 |
| MCP 调用追溯 | ✅ | 含熔断事件通道的完整调用链记录 |
| Plan/WorkPlan 工作流 | ✅ | 支持 plan_load/plan_run/plan_status/plan_export/plan_clear |
| Web 搜索 | ✅ | 支持 Tavily 等搜索 Provider，账号池 YAML 可配置 api_key |
| Electron sidecar | ⬜ | 计划通过 JSON-RPC/stdio 复用 application core |
| CAD Plugin 垂直闭环 | ⬜ | FreeCAD/MCP/命令栈的最小垂直闭环 |
| Dev Plugin | ⬜ | 规划代码、测试、审查和交付能力形态 |

完整进度、度量指标和完成标准见 [`docs/feature-instrumentation.md`](docs/feature-instrumentation.md)。

## 架构

`application` 是无界面的应用核心，持有业务状态、副作用和异步生命周期；`tui` 只负责 Bubble Tea 输入事件、终端尺寸、光标、选中项、滚动和 Lipgloss 渲染。Snapshot、Event 和 Interaction DTO 不依赖 Bubble Tea，可供后续 Electron sidecar 序列化。

### 装配顺序

| 层 | 职责 | 对应函数 |
|----|------|---------|
| L1 | Runtime、Skill、Plugin、Store | `initRuntime()`, `initSkillSystem()`, `initPluginSystem()` |
| L2 | 事件与审批基础设施 | `application.NewEventHub()`, `application.NewApprovalBroker()` |
| L3 | Engine 与 Tool Hooks | `initEngine()` |
| L4 | 产品工具与 Session | `registerProductTools()`, `initSessionManager()` |
| L5 | Headless Application Core | `initApplication()` |
| L6 | TUI Adapter | `initTUI()` |
| L7 | Bubble Tea Program | `startTUI()` |

### 依赖原则

1. interface 定义在使用方，不定义在实现方；
2. TUI 不直接依赖 Engine、Plugin、Skill、Session 或 Seele 深层类型；
3. Seele 已有能力优先通过薄适配器复用，不在 Seelex 重造引擎；
4. Plugin 负责专业能力组合，不把领域逻辑硬编码进 TUI；
5. CLI 与 Electron 使用同一个 application core，不复制业务状态机。

### 架构文档

- [`docs/arch/architecture-and-flaws.md`](docs/arch/architecture-and-flaws.md) — 架构说明书与已知硬伤清单
- [`docs/arch/design-decisions-mcp-storage.md`](docs/arch/design-decisions-mcp-storage.md) — MCP 中间件设计与存储解耦推演
- [`docs/arch/mcp-call-chain-flowchart.md`](docs/arch/mcp-call-chain-flowchart.md) — Agent 调用 MCP 全链路函数流
- [`docs/arch/effort-system-design.md`](docs/arch/effort-system-design.md) — Effort 等级系统完整设计
- [`docs/arch/skill-effort-architecture.md`](docs/arch/skill-effort-architecture.md) — Skill 系统与 PromptStack 实现方案

## 快速开始

### 前提

- Go ≥ 1.25
- LLM API Key（OpenAI / Anthropic / DeepSeek 等）

### 安装与运行

```bash
git clone https://github.com/RedHuang-0622/seelex.git
cd seelex

# 1. 配置账号 — 编辑 config/accounts.yaml 填入 API Key
#    也可复制为 config/accounts.local.yaml 本地使用（已 .gitignore）

# 2. 运行
go run .
```

配置示例（`config/accounts.yaml`）：

```yaml
defaults:
  provider: openai
  max_tokens: 4096
  timeout: 60
  temperature: 0.7

accounts:
  - name: main
    provider: openai
    model: gpt-4o
    base_url: https://api.openai.com/v1
    api_key: sk-...
```

脚本读取 `$HOME/.claude/settings.json` 中的 `ANTHROPIC_AUTH_TOKEN`，生成本地 OpenAI 兼容账号配置。`config/*.local.yaml` 已被 gitignore，不会进入版本库。

### CLI 标志

| 标志 | 默认值 | 说明 |
|------|--------|------|
| `-store` | `.seelex/sessions` | 持久化存储路径 |
| `-plugins` | `plugins` | Plugin 加载路径（逗号分隔） |
| `-permission` | `full_access` | 权限模式：`full_access`(全部放行) / `manual`(白名单外需审批) |

## 使用方式

### Plugin 形态切换

当前仓库包含 7 个 Plugin（1 个通用 + 5 个基础形态 + 1 个专业 Plugin）：

| Plugin | 能力范围 |
|--------|----------|
| `default` | 全部已注册工具和全局 Skill |
| `read` | 阅读、搜索和只读 Git 分析 |
| `write` | 文件编辑、代码修改和必要的 Shell |
| `git` | Git 操作与变更审查 |
| `shell` | Shell 与 DevOps 操作 |
| `plan` | 规划和 WorkPlan 工作流 |
| `freecad` | CAD 设计、建模与工程分析（规划中） |

用户可使用 `/plugin <name>`，Agent 也可调用 `switch_plugin` 或兼容别名 `switch_mode`。

### 命令与补全

| 输入 | 行为 |
|------|------|
| `/` | 展示命令、当前 Plugin 可见工具和 Skill |
| `#` | 展示已加载 Skill |
| `@` | 展示已注册 Plugin |
| `Ctrl+C` | 复制最后一条 AI 回复到系统剪贴板 |
| `Ctrl+V` | 从剪贴板粘贴到输入框 |
| `Alt+E` | 循环切换 Effort 等级（lite→medium→high→max） |
| `Ctrl+Q` | 退出程序 |
| `左键拖选` | 选中对话文本，然后 Ctrl+C 复制 |
| `Tab` | 接受当前建议 |
| `↑` / `↓` | 切换建议或交互选项 |
| `#end` | 退栈最近加载的 Skill |

主要命令：

| 命令 | 说明 |
|------|------|
| `/help` | 显示帮助 |
| `/clear` | 清空对话历史 |
| `/model` | 显示模型和 Provider |
| `/pool` | 查看并切换账号 |
| `/plugins` | 列出 Plugin |
| `/plugin <name>` | 切换 Plugin |
| `/effort` | 查看当前 Effort 等级 |
| `/effort <level>` | 切换 Effort 等级（lite/medium/high/max） |
| `/history` | 显示历史统计 |
| `/trace` | 显示调用追踪 |
| `/diag` | 系统诊断信息（Go运行时、内存、插件、Skill、账号） |
| `/new` | 保存当前会话并清空历史 |
| `/sessions` | 列出持久化会话 |
| `/resume <id>` | 当前受 Seele 历史替换能力限制，会返回明确提示 |
| `/exit` | 退出程序 |

### Effort 等级说明

Effort 控制 Agent 的思考深度和工具使用强度，通过多层 PromptStack 注入行为指令：

| 等级 | MaxLoops | 工具可见性 | 行为特征 |
|------|----------|-----------|---------|
| lite | 20 | 有限只读 | 直接快速回答，不主动规划，不做 loop 限制 |
| medium | 64 | 标准工具集 | 平衡速度与能力，plan 最多 2 节点并行，失败重试 1 次 |
| high | 512 | 全部工具（默认） | 完整 ReAct，复杂任务用 WorkPlan 编排，最多 4 节点并行，失败重试 3 次 |
| max | 1024 | 全部工具 + Fork | 深度推理，无限制并行子代理，失败重试 5 次，交叉验证 |

当前 effort 等级显示在状态栏：`E:lite`(灰) / `E:medium`(金) / `E:high`(蓝) / `E:max`(紫红)。Skill 加载栈也同步显示：如 `E:high  goal|code`。

### 审批与权限现状

当前已经实现：

- 无界面的 `ApprovalBroker`；
- TUI 卡片式交互与 Resolve；
- `ask_approve` 工具；
- 超时、取消和关闭唤醒机制。

当前尚未实现：

- 对所有工具调用进行强制拦截的 Permission Gate；
- 自动执行 `seele.yaml` 中的 `allow / ask / deny` 规则；
- "始终允许"等持久化授权策略。

因此 `seele.yaml` 目前是目标权限策略草案，不能视为已经生效的安全边界。在强制门控接通前，运行高风险工具仍需依赖宿主环境和人工控制。

## 专业形态路线

### CAD Plugin

CAD 是 Seelex 的首个复杂工程增幅场景，计划组合：

- FreeCAD 执行底座；
- MCP 工具桥接；
- 可验证、可重放的 JSON 命令栈；
- 模型、图纸和 STEP 等工程产物；
- 几何约束、事务、撤销和检查点。

CAD 用于证明 Plugin 可以承载真正的工程工作流，但不会把 Seelex 限定为 CAD Agent。

### Dev Plugin

Dev Plugin 将面向软件工程全过程：

- 仓库理解与方案设计；
- 代码实现和重构；
- 单元、集成、性能与安全测试；
- Review、变更报告和交付；
- 与 Git、CI、Issue/Task 系统集成。

### Electron

Electron 不替代 CLI，而是提供另一种产品入口：

- 面向毕业设计和课程项目的可视化操作；
- 工程任务、产物、Plugin 和执行历史展示；
- CAD 模型、报告和演示内容集成；
- 通过 JSON-RPC/stdio 调用与 TUI 相同的 application core。

## 项目结构

```text
seelex/
├── main.go                 # 生命周期与依赖装配（L1-L7 层）
├── main_unix.go            # Unix 信号处理
├── main_windows.go         # Windows 信号处理
├── application_adapters.go # application ports → Seele 适配
├── websearch.go            # Web 搜索工具注册
├── mcpconfig.go            # MCP Server 配置加载与注册
├── version.go              # 版本号
├── seele.yaml              # 目标权限规则草案，当前尚未强制执行
├── application/            # 无界面的应用核心
│   ├── app.go              #   Service（状态持有、生命周期、命令注册、PromptStack）
│   ├── chat.go             #   流式聊天、delta 累积、输入队列
│   ├── command.go          #   Command 接口、CommandRegistry、内置命令
│   ├── completion.go       #   建议补全（Plugin / Skill / 命令 / 历史）
│   ├── diag.go             #   /diag 诊断面板渲染
│   ├── effort.go           #   EffortManager（四档切换、PromptStack 注入、MaxLoops）
│   ├── event.go            #   EventHub（发布/订阅、反压处理）
│   ├── approval.go         #   ApprovalBroker（审批请求/决议/超时/取消）
│   ├── input.go            #   用户输入处理（命令 / Skill / 普通消息）
│   ├── prompt_stack.go     #   多层 system prompt 栈（5 层：identity→plugin→effort→instructions→skill）
│   ├── ports.go            #   Dependencies 接口（ChatEngine / RuntimePort / PluginPort / SkillPort / SessionPort）
│   ├── state.go            #   Snapshot DTO（Session / Message / Chat / Runtime / Plan / Capabilities）
│   └── websearch.go        #   Web 搜索配置加载
├── plugin/                 # Plugin Loader 与事务型 Manager
│   ├── plugin.go           #   Plugin 结构体 + MCPServer 定义
│   ├── loader.go           #   文件系统 Loader（plugin.md 解析）
│   └── manager.go          #   事务式激活/停用/回滚 + 工具过滤
├── plugins/                # 文件化 Plugin 定义（含 Skill）
│   ├── default/            #   通用 Plugin + 9 个全局 Skill
│   │   ├── plugin.md
│   │   ├── cli-design/     #     CLI/TUI 交互设计规范
│   │   ├── code/           #     代码实现
│   │   ├── goal/           #     GOAL 方法论 + A2A 子代理调度
│   │   ├── plan/           #     方案设计入口
│   │   ├── plan-design/    #     启发式方案设计
│   │   ├── plan-efficiency/#     规划式效率方案
│   │   ├── plan-norm/      #     约束式规范方案
│   │   ├── review/         #     代码审查
│   │   └── test/           #     测试编写
│   ├── freecad/            #   CAD Plugin（7 个 Skill）
│   │   ├── cad-batch/      #     批量操作
│   │   ├── cad-boolean/    #     布尔运算
│   │   ├── cad-core/       #     核心建模
│   │   ├── cad-fillet/     #     倒角/倒圆
│   │   ├── cad-inspect/    #     测量/检查
│   │   ├── cad-repair/     #     几何修复
│   │   └── cad-template/   #     模板设计
│   ├── read/               #   只读 Plugin
│   ├── write/              #   写操作 Plugin
│   ├── git/                #   Git Plugin
│   ├── shell/              #   Shell Plugin
│   └── plan/               #   Plan Plugin
├── skill/                  # Skill Loader 与 Registry
│   ├── skill.go            #   Skill 结构体
│   ├── loader.go           #   目录加载器（按 Plugin 路径加载 SKILL.md）
│   └── registry_test.go    #   Registry 测试
├── seelebridge/            # Seele 薄适配层（Anti-Corruption Layer）
│   ├── runtime.go          #   Runtime（Agent 创建、账号池、MCPStack 绑定、Plan 回调）
│   ├── storage.go          #   存储适配（会话持久化/加载/分页）
│   ├── mcp.go              #   MCP Server 注册与管理
│   ├── plugins.go          #   工具可见性切换适配
│   └── trace.go            #   调用追踪导出
├── seelexctx/              # 上下文管理与压缩
│   ├── seele.go            #   Seele ctx 能力 re-export
│   ├── bridge.go           #   桥接层
│   ├── compactor/          #   历史压缩器（LLM 摘要）
│   ├── merger/             #   历史合并器
│   ├── provider/           #   Engine/Trace Provider 导出
│   │   ├── engine.go       #     EngineProvider
│   │   ├── trace.go        #     TraceProvider
│   │   └── provider.go     #     Provider 接口
│   └── snapshot/           #   快照管理
├── mcpstack/               # MCP 调用追溯中间件（双栈架构）
│   ├── stack.go            #   MCPCall 记录 + MCPStack 核心 + Undo/Redo 指针
│   ├── breaker.go          #   熔断事件监听
│   ├── interceptor.go      #   拦截器（wrap MCP 调用）
│   ├── persist.go          #   原子 JSON 持久化
│   ├── prompt.go           #   MCP 历史注入 LLM Prompt
│   ├── provider.go         #   Provider 集成
│   └── snapshot.go         #   快照导出
├── session/                # 会话管理（薄包装 Seele storage.Store）
│   └── manager.go          #   SaveCurrent / Resume / List / LoadHistory / LoadHistoryRange
├── internal/               # 内部工具
│   └── frontmatter/        #   YAML frontmatter 解析
├── tui/                    # Bubble Tea 前端
│   ├── tui.go              #   Model / Update / View
│   ├── view.go             #   视图渲染
│   ├── dialog.go           #   交互卡片（审批等）
│   ├── plan.go             #   Plan 可视化面板
│   ├── stream.go           #   流式输出处理
│   ├── state.go            #   TUI 本地状态
│   ├── styles.go           #   配色方案（初号机配色）
│   ├── suggest_view.go     #   建议补全视图
│   ├── types.go            #   TUI 类型定义
│   └── splash/             #   启动画面
├── config/                 # 账号池配置模板
│   ├── accounts.yaml
│   └── account-pool.yaml
├── scripts/                # 构建与同步脚本
│   ├── sync-claudecode-account.ps1
│   └── Makefile
├── docs/                   # 文档
│   ├── arch/               #   架构设计文档
│   ├── devlog/             #   研发过程记录
│   └── research/           #   调研报告
└── dist/                   # 跨平台构建产物（make build）
```

## 已知问题与局限

当前版本（v0.0.4）存在以下已知问题，详见 [`CODE_EVALUATION_REPORT.md`](CODE_EVALUATION_REPORT.md)（评估日期 2025-07-11，评分 72/100）：

| 类别 | 问题 | 影响 | 当前状态 |
|------|------|------|:---:|
| 代码质量 | `application/command.go` 中使用 `log.Fatalf` 处理注册错误 | 启动即崩溃 | ✅ 已修复 |
| 并发安全 | `EventHub.Publish` 持锁向 channel 发送 | 低概率阻塞 | 🟡 已有反压+resync，channel 发送优化待改 |
| 依赖装配 | `main.go` 超 400 行手工 DI | 可维护性 | 待重构 |
| 安全 | 强制 Permission Gate 未接通 | 权限规则不生效 | 🟡 基础已接线，seele.yaml 规则未强制执行 |
| 测试覆盖 | TUI 包仅 6.2% | TUI 回归保护不足 | 待补充 |

> 自评估报告以来已大幅改善：`application` 包从 42% → 67.4%，`session` 从 0% → 100%，`plugin` 维持 87.6%，`skill` 维持 82.6%。并发安全方面已修复 `PromptStack` 和 `EffortManager` 的 data race，`EventHub` 反压机制已实现。全量 `go test -race` 通过。

## 路线图

### 已完成

- TUI 与 application core 分离（无界面核心 67.4% 覆盖率）；
- Chat、命令、补全、工具事件和审批迁入无界面核心；
- 输入队列：多消息排队合并为一条批量发送；
- Plugin/Skill 文件化与运行时切换（7 个 Plugin、16 个 Skill）；
- Plugin MCP 生命周期和失败回滚；
- 多账号 Runtime、会话保存和基础追踪；
- Effort 等级系统（lite/medium/high/max）：PromptStack 分层 + 行为指令注入 + MaxLoops 控制；
- Skill 多层叠加与退栈（`#goal` → `#code` 压栈，`#end` 退栈）；
- Alt+E 循环切换 Effort + 状态栏实时显示；
- 提示词五层组装：identity → plugin → effort → instructions → skill；
- Plan/WorkPlan 工作流系统（plan_load/plan_run/plan_status/plan_export/plan_clear）；
- MCP 调用追溯中间件（mcpstack）+ 熔断事件通道 + 拦截器；
- Web 搜索集成（Tavily，账号池 YAML 可配置 api_key）；
- /diag 系统诊断面板（Go 运行时、内存、插件、Skill、账号）；
- Plan 可视化（TUI 面板 + 进度回调）；
- 全量 `go test -race` 通过，核心并发路径已加固。

### 下一阶段

1. Effort 注入 API 参数（Anthropic thinking / OpenAI reasoning_effort）；
2. Effort → 模型选型（flash/pro 自动切换）；
3. Effort → Planning 策略（brief/structured/dag）；
4. 接通强制 Permission Gate，消除权限声明与运行行为差异；
5. 为 Snapshot 增加分页、协议版本和稳定错误码；
6. 实现 JSON-RPC/stdio sidecar；
7. 打通 CAD Plugin 最小垂直闭环；
8. 建立 Dev Plugin 的代码—测试—Review 闭环；
9. 基于 sidecar 构建 Electron 毕设与成果展示界面。

## 开发

```bash
# 构建
go build ./...

# 代码检查
go vet ./...

# 运行测试
go test ./... -v -count=1 -timeout=120s

# 竞态检测
go test ./... -race -count=1

# 覆盖率报告
go test ./... -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | tail -1

# 跨平台构建
make build && make package
```

### 当前测试覆盖率

| 包 | 覆盖率 |
|---|:---:|
| `session` | 100.0% |
| `internal/frontmatter` | 100.0% |
| `seelexctx/merger` | 100.0% |
| `seelexctx/compactor` | 98.1% |
| `plugin` | 87.6% |
| `seelexctx/snapshot` | 86.6% |
| `skill` | 82.6% |
| `mcpstack` | 70.4% |
| `seelexctx` | 69.0% |
| `application` | 67.4% |
| `seelebridge` | 58.3% |
| `seelexctx/provider` | 53.3% |
| `(root)` | 11.5% |
| `tui` | 6.2% |
| `tui/splash` | 0.0% |

## 许可证

MIT
