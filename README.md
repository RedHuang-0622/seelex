# Seelex — 可切换专业形态的工科全栈 Agent

**Seelex** 是一个面向工程研发、设计与交付全过程的全栈 Agent。它以 [Seele](https://github.com/RedHuang-0622/Seele) 作为 Agent 引擎，通过可声明、可组合、可运行时切换的 Plugin，让同一个 Agent 像切换“形态”一样进入不同专业工作域。

Seelex 的目标不是只做一个 TUI，也不是只做 CAD：

- **Seele** 提供 LLM、工具调度、MCP、存储和运行时基础能力；
- **Seelex Application Core** 负责任务状态、命令、Plugin、Skill、会话、审批和事件编排；
- **Plugin** 定义 Agent 当前的专业形态，例如基础读写、CAD、软件开发和后续工程领域；
- **CAD** 是首个重点专业增幅方向，用于验证复杂工程工具链，而不是产品边界；
- **TUI** 是高效率 CLI 工作入口；
- **Electron** 面向毕业设计、课程项目、成果展示和更完整的可视化交付体验。

> 当前阶段重点是稳定 Agent 内核与 Plugin 切换机制。CAD、Dev 和 Electron 将作为建立在同一内核上的产品形态逐步落地。

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
| Effort 等级 | ✅ | low/medium/high/max 四档，控制 MaxLoops、提示词深度和工具可见性 |
| PromptStack 分层 | ✅ | system prompt 按 identity + effort + plugins + instructions + skill 五层组装 |
| 审批交互 Broker | 🟡 | `ask_approve` 和前端决议已实现；强制 Permission Gate 尚未接通 |
| 会话保存与列表 | ✅ | 支持 `/new` 保存和 `/sessions` 查询 |
| 会话恢复 | ⛔ | Seele 暂无历史替换 API，当前会明确提示不可用 |
| Electron sidecar | ⬜ | 计划通过 JSON-RPC/stdio 复用 application core |
| CAD Plugin | ⬜ | 规划 FreeCAD/MCP/命令栈的最小垂直闭环 |
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

### MCP 调用链路

Agent 调用 MCP 工具的完整函数链、数据转换和熔断事件通道详见：
[`docs/arch/mcp-call-chain-flowchart.md`](docs/arch/mcp-call-chain-flowchart.md)

包含：
- Setup 阶段（AttachMCP 装配）
- Call 阶段（Agent.Dispatch → MCP Server）
- 熔断事件异步通道（breaker → channel → mcpstack）
- 每个函数的包/文件/行号索引

### 依赖原则

1. interface 定义在使用方，不定义在实现方；
2. TUI 不直接依赖 Engine、Plugin、Skill、Session 或 Seele 深层类型；
3. Seele 已有能力优先通过薄适配器复用，不在 Seelex 重造引擎；
4. Plugin 负责专业能力组合，不把领域逻辑硬编码进 TUI；
5. CLI 与 Electron 使用同一个 application core，不复制业务状态机。

### 设计决策记录

MCP 中间件从 CAD 专属到通用、熔断器事件通道、框架-应用存储解耦的完整推演过程：
[`docs/arch/design-decisions-mcp-storage.md`](docs/arch/design-decisions-mcp-storage.md)

### 架构文档

- [`docs/arch/effort-system-design.md`](docs/arch/effort-system-design.md) — Effort 等级系统完整设计（配置 → API → 提示词 → TUI）
- [`docs/arch/skill-effort-architecture.md`](docs/arch/skill-effort-architecture.md) — 当前 Skill 系统与 PromptStack 实现方案
- [`docs/arch/mcp-call-chain-flowchart.md`](docs/arch/mcp-call-chain-flowchart.md) — MCP 工具调用链路

## 快速开始

### 前提

- Go ≥ 1.25

### 安装与运行

```bash
git clone https://github.com/RedHuang-0622/seelex.git
cd seelex
cp config/account-openai.yaml config/account-openai.local.yaml
# 编辑本地配置并填入 API Key
go run . -c config/account-openai.local.yaml
```

配置示例：

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

支持多账号 round-robin 轮询，参见 `config/account-pool.yaml`。

### Claude Code MiniMax 账号同步

```powershell
.\scripts\sync-claudecode-account.ps1
go run . -c config/account-claudecode.local.yaml
```

脚本读取 `$HOME/.claude/settings.json` 中的 `ANTHROPIC_AUTH_TOKEN`，生成本地 OpenAI 兼容账号配置。`config/*.local.yaml` 已被忽略，不会进入版本库。

### CLI 标志

| 标志 | 默认值 | 说明 |
|------|--------|------|
| `-c` | `config/account-openai.yaml` | LLM 配置路径 |
| `-store` | `""` | 持久化存储路径，空值表示当前目录 |
| `-skills` | `skills,cmd/repl/skills` | Skill 加载路径，逗号分隔 |
| `-plugins` | `plugins` | Plugin 加载路径，逗号分隔 |

## 使用方式

### Plugin 形态切换

当前仓库包含用于验证 Plugin 基础设施的基础形态：

| Plugin | 能力范围 |
|--------|----------|
| `default` | 全部已注册工具和全局 Skill |
| `read` | 阅读、搜索和只读 Git 分析 |
| `write` | 文件编辑、代码修改和必要的 Shell |
| `git` | Git 操作与变更审查 |
| `shell` | Shell 与 DevOps 操作 |
| `plan` | 规划和 WorkPlan 工作流 |

用户可使用 `/plugin <name>`，Agent 也可调用 `switch_plugin` 或兼容别名 `switch_mode`。后续 CAD、Dev 等专业 Plugin 将沿用相同机制。

### 命令与补全

| 输入 | 行为 |
|------|------|
| `/` | 展示命令、当前 Plugin 可见工具和 Skill |
| `#` | 展示已加载 Skill |
| `@` | 展示已注册 Plugin |
| `Ctrl+C` | 复制最后一条 AI 回复到系统剪贴板 |
| `Ctrl+V` | 从剪贴板粘贴到输入框 |
| `Alt+E` | 循环切换 Effort 等级（low→medium→high→max） |
| `Ctrl+Q` | 退出程序 |
| `Tab` | 接受当前建议 |
| `↑` / `↓` | 切换建议或交互选项 |
| `#end` | 退栈最近加载的 Skill |

主要命令：

| 命令 | 说明 |
|------|------|
| `/help` | 显示帮助 |
| `/model` | 显示模型和 Provider |
| `/pool` | 查看并切换账号 |
| `/plugins` | 列出 Plugin |
| `/plugin <name>` | 切换 Plugin |
| `/effort` | 查看当前 Effort 等级 |
| `/effort <level>` | 切换 Effort 等级（low/medium/high/max） |
| `/history` | 显示历史统计 |
| `/trace` | 显示调用追踪 |
| `/new` | 保存当前会话并清空历史 |
| `/sessions` | 列出持久化会话 |
| `/resume <id>` | 当前受 Seele 历史替换能力限制，会返回明确提示 |
| `/exit` | 退出程序 |

### Effort 等级说明

Effort 控制 Agent 的思考深度和工具使用强度，通过多层 PromptStack 注入行为指令：

| 等级 | MaxLoops | 工具可见性 | 行为特征 |
|------|----------|-----------|---------|
| low | 0 | 有限只读 | 直接快速回答，不调工具 |
| medium | 8 | 标准工具集 | 平衡速度与能力，必要时简述规划 |
| high | 25 | 全部工具（默认） | 完整 ReAct，复杂任务用 WorkPlan 编排行 |
| max | 50 | 全部工具 + Fork | 深度推理，多 Agent 并行，交叉验证 |

当前 effort 等级显示在状态栏：`E:low`(灰) / `E:medium`(金) / `E:high`(蓝) / `E:max`(紫红)。Skill 加载栈也同步显示：如 `E:high  goal|code`。

### 审批与权限现状

当前已经实现：

- 无界面的 `ApprovalBroker`；
- TUI 卡片式交互与 Resolve；
- `ask_approve` 工具；
- 超时、取消和关闭唤醒机制。

当前尚未实现：

- 对所有工具调用进行强制拦截的 Permission Gate；
- 自动执行 `seele.yaml` 中的 `allow / ask / deny` 规则；
- “始终允许”等持久化授权策略。

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
├── main.go                 # 生命周期与依赖装配
├── application_adapters.go # application ports 适配
├── application/            # 无界面的应用核心
├── plugin/                 # Plugin Loader 与事务型 Manager
├── plugins/                # 文件化 Plugin 定义
├── skill/                  # Skill Loader 与 Registry
├── skills/                 # 全局 Skill
├── seelebridge/            # Seele 薄适配层
├── seelexctx/              # 上下文、压缩、合并和快照
├── session/                # 会话存储薄包装
├── tui/                    # Bubble Tea 前端适配器
├── docs/                   # 文档：架构(arch/)、CAD(cad/)、研发(devlog/)、调研(research/)
└── seele.yaml              # 目标权限规则草案，当前尚未强制执行
```

## 路线图

### 已完成

- TUI 与 application core 分离；
- Chat、命令、补全、工具事件和审批迁入无界面核心；
- Plugin/Skill 文件化与运行时切换；
- Plugin MCP 生命周期和失败回滚；
- 多账号 Runtime、会话保存和基础追踪；
- Effort 等级系统（low/medium/high/max）：PromptStack 分层 + 行为指令注入 + MaxLoops 控制；
- Skill 多层叠加与退栈（`#goal` → `#code` 压栈，`#end` 退栈）；
- Alt+E 循环切换 Effort + 状态栏实时显示；
- 提示词五层组装：identity + effort + plugins + instructions + skill；
- application、plugin、skill、bridge、effort 和 TUI adapter 单元测试。

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
go build ./...
go vet ./...
go test ./... -v -count=1 -timeout=120s
```

## 许可证

MIT
