# Seelex — Open-Source Coding Agent Harness

> 面向软件工程任务的 Agent Harness 与 Multi-Agent Runtime：把 LLM、Tool Calling、Agentic Workflow、Context Engineering、权限策略和持久化装配成可运行、可观察、可恢复的本地 Coding Agent。

[![CI](https://img.shields.io/github/actions/workflow/status/RedHuang-0622/seelex/ci.yml?style=flat-square&label=CI&logo=github)](https://github.com/RedHuang-0622/seelex/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.25%2B-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-2ea44f?style=flat-square)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-Windows%20%7C%20Linux%20%7C%20macOS-6f42c1?style=flat-square)](https://github.com/RedHuang-0622/seelex/releases)
[![Status](https://img.shields.io/badge/Status-Developer%20Alpha-f59e0b?style=flat-square)](#当前状态与边界)

[English README](README_EN.md)

Seelex 不是一个只负责转发聊天请求的 AI Chat Client，也不是把 Prompt、Shell 和模型 API 粘在一起的薄封装。作为面向软件工程的 AI Agent Framework，它在 [Seele](https://github.com/RedHuang-0622/Seele) Agent Runtime 之上提供 Coding Agent 的产品语义：项目作用域 Tool Calling / Function Calling、Task/Plan 生命周期、Multi-Agent Orchestration、并行 Subagent、Context Engineering、模型与账号路由、Human-in-the-loop 审批、Plugin/Agent Skills/MCP、Session Persistence，以及共享同一 Application Core 的 TUI 和桌面 GUI。

当前项目处于 **Developer Alpha**。默认入口是 TUI；GUI 已可构建和使用，但仍属于 Alpha 功能。

## 项目概览

Seelex 由两个公开层次组成：[Seele](https://github.com/RedHuang-0622/Seele) 提供 Agent、Session、Tool Registry、ReAct、WorkPlan 和 Account Pool 等运行时原语；本仓库在其上实现面向软件工程的 Application Core、Workspace Sandbox、Context Pipeline、Plugin/Skill/MCP、持久化和交互前端。两层之间通过 [Seele Bridge](seelebridge/README.md) 隔离，使 Runtime 能力与产品语义可以分别演进。

主会话默认使用 ReAct 和项目作用域 Tool Calling 完成任务。面对需要拆分的长任务，模型可以按需加载 WorkPlan DAG：每个 Subagent 节点拥有独立 Session、NodeScope、PromptBlocks、账号 binding 和 token budget，并行执行后再把 findings、decisions 与 progress 合并回父会话。Plan 不是所有请求的强制前置步骤，因此简单任务不会额外承担规划延迟和 token 成本。

上下文处理采用预算驱动的 Context Engineering 流程。Seelex 会为输出预留 token、保留最近对话窗口、压缩窗口外历史，并把超大 Tool Result 归档为可读回的引用。文件和 Shell 工具则同时受 ProjectScope 与 Permission Policy 约束：前者负责 workspace root 的路径 containment，后者在合法范围内继续执行 allow、ask 或 deny，并通过 Human-in-the-loop Interaction 完成审批。

运行时可以使用 OpenAI-compatible endpoint，包括满足流式响应和 Tool Calling 契约的 DeepSeek 服务。模型账号按 agent、subagent、goalplan 等角色进入 Account Pool；Plugin 可以事务式切换工具、Agent Skills 和 MCP Server。会话数据可保存到 JSON、SQLite、PostgreSQL 或 Redis，并保持统一的 project/session 隔离和 immutable generation 语义。

项目通过 Go 单元测试、集成测试、确定性 E2E scenario、GUI 协议测试和 Windows/Linux/macOS CI 验证。完整的设计依据和代码入口见 [关键技术决策](#关键技术决策)，当前已知限制见 [项目状态与边界](#当前状态与边界)。

不连接真实模型也可以完成基础验证：

~~~bash
git clone https://github.com/RedHuang-0622/seelex.git
cd seelex
go test ./... -count=1 -timeout=120s
go build .
~~~

上述验证不需要真实模型账号；连接模型并执行 Tool Calling、ReAct 或 Multi-Agent Plan 时才需要本地 <code>config/accounts.yaml</code>。

## 为什么是 Harness

一个可用的 Agent 不只有模型调用。它还需要回答以下问题：

- 模型在什么时候可以调用哪些工具？
- 文件和 Shell 操作如何限制在当前项目内？
- 长任务如何拆分、并行执行并把结果合并回主会话？
- 上下文超出预算时，哪些内容保留、压缩或按需读回？
- 会话、计划和工具结果如何可靠恢复？
- 不同模型、账号、Plugin、Skill 和 MCP Server 如何在运行时切换？

Seelex 把这些能力组织成可替换、可测试的模块，而不是把它们写进某个前端或单一 Agent 循环。

## 已实现能力

| 能力 | 当前实现 |
|---|---|
| Agent 执行 | 流式对话、工具调用、取消、审批交互和任务终态 |
| Plan 与子 Agent | 可选 WorkPlan DAG、拓扑校验、并行分支、独立节点 Session、事件投影和结果 merge-back |
| 上下文治理 | Prompt Stack、滑动窗口、预算控制、压缩、超大工具结果归档与按需读回 |
| 项目安全 | ProjectScope 路径约束、PathGate 规则、manual/full_access 权限模式 |
| 扩展系统 | 声明式 Plugin、目录化 Skill、MCP Server 动态挂载与工具可见性过滤 |
| 模型与账号 | OpenAI-compatible endpoint、按角色分组的账号池、分支确定性选路和流式租约 |
| 持久化 | JSON、SQLite、PostgreSQL、Redis 后端；项目与 Session 隔离、分片和原子 generation 切换 |
| 前端 | Bubble Tea TUI；Wails/WebView GUI（Alpha） |
| 可观测性 | Snapshot/Event 协议、Plan 节点事件、MCP 调用轨迹和运行时状态 |
| 测试 | Go 单元/集成/E2E、GUI 协议测试、跨平台 CI、race/coverage 和发布安全检查 |

Plan 是可选能力。普通请求可以直接进入主 ReAct 流程；只有在任务需要结构化拆分时才加载和执行 DAG。

## 架构

~~~text
┌────────────────────── Clients ──────────────────────┐
│  TUI (Bubble Tea)              GUI (Wails/WebView)  │
└──────────────────────────┬───────────────────────────┘
                           │ Snapshot / Event / Action
┌──────────────────────────▼───────────────────────────┐
│ application/                                         │
│ Chat · Task · Plan · Approval · Session · Workspace │
└───────────────┬──────────────────────┬───────────────┘
                │                      │
┌───────────────▼────────────┐  ┌──────▼──────────────┐
│ seelebridge/               │  │ seelexctx/          │
│ Runtime · Tools · Plan     │  │ Assemble · Compact  │
│ Account · MCP · PathGate   │  │ Snapshot · Merge    │
└───────────────┬────────────┘  └──────┬──────────────┘
                │                      │
┌───────────────▼──────────────────────▼───────────────┐
│ Seele runtime                                       │
│ agent · session · tools · workplan · accountpool    │
└──────────────────────────────────────────────────────┘

Supporting modules:
plugin/ · skill/ · sessionstore/ · workspace/ · mcpstack/
~~~

### Seelex 与 Seele 的边界

| 模块 | 负责什么 |
|---|---|
| Seele | Agent/Session 原语、ReAct 执行、工具注册与分发、WorkPlan 内核、账号租约、事件和遥测 |
| Seelex | 工程任务语义、Plan 产品 DSL、项目作用域工具、上下文策略、Plugin/Skill/MCP 编排、持久化和前端 |

Seelex 当前依赖 <code>github.com/RedHuang-0622/Seele v0.1.1</code>。上游能力通过 <code>seelebridge/</code> 集中适配，Application 和前端不直接依赖 Seele 的内部类型。

## 关键技术决策

这一节描述当前代码中已经落地的设计选择，以及这些选择试图解决的工程问题。它们也是 Seelex 与普通 AI Chat Client、Prompt Wrapper 或单文件 Agent Demo 的主要区别。

### 1. 用 Ports and Adapters 隔离 Agent Runtime 与产品语义

Seelex 采用接近 **Hexagonal Architecture / Ports and Adapters** 的依赖方向：

- <code>application/contract</code> 由消费方定义 Runtime、Session、Plugin、Workspace 等端口。
- 根目录 adapter、<code>seelebridge/</code> 和存储模块实现这些端口。
- TUI 与 GUI 只依赖 Application DTO、Snapshot 和 Event，不直接调用 Seele Engine、数据库或 Plugin Manager。

这个决策把“模型如何执行”和“产品如何解释执行结果”分开。Seele 可以继续演进 Agent、Session、Tool Registry 和 WorkPlan 原语；Seelex 则保持 Task、Plan、审批、Workspace 和前端协议稳定。上游类型变化被限制在 **Anti-Corruption Layer** <code>seelebridge/</code> 内，而不是扩散到整个代码库。

### 2. Application Snapshot 是权威状态，Event 只负责增量同步

聊天、Plan、Session、Interaction 和 Runtime State 的权威事实保存在 Application，而不是某个前端。客户端启动时先读取完整 Snapshot，再消费带 sequence/revision 的有序 Event：

- Event 连续时，TUI/GUI 只应用增量更新。
- Event 出现缺口、未知版本或协议不兼容时，客户端重新加载 Snapshot。
- 前端本地状态只保存 viewport、光标、输入框和布局等纯 UI 信息。

这是一个面向 Agent 长任务的 **Snapshot + Event Delta Protocol**。它避免 TUI 和 GUI 各自维护一套业务状态机，也避免流式 token、工具事件和 Plan 节点状态因丢包而永久错位。

### 3. ReAct 是默认执行路径，DAG Plan 是按需加载的编排能力

Seelex 没有强迫所有请求先生成 Plan。简单任务直接进入主 **ReAct Loop**，减少额外模型请求、首 token 延迟和计划 token 消耗；只有复杂任务需要结构化拆分时，模型才使用 <code>plan_load</code>/<code>plan_run</code> 加载 **Agentic Workflow DAG**。

Plan 在执行前完成：

- JSON Schema 与字段归一化。
- 节点引用、边和拓扑校验。
- cycle detection 与 topological order。
- Effort 对节点数、串并行和最大并发的策略约束。

每个 <code>kind: agent</code> 节点获得独立 Session、NodeScope、PromptBlocks、账号 binding 和 token budget。并行分支不共享不可控的会话状态；父任务证据在执行前注入，子节点的 findings、decisions 和 progress 在完成后结构化 merge-back。

这个设计属于 **Multi-Agent Orchestration / Subagent Orchestration**，但当前仍是单进程内编排，不宣称已经实现跨组织 A2A Protocol。

#### 子代理的进度与结果

`fork_subagents` 会在运行时构造 `start → subagent(s) → summary` 的 DAG，并同步等待该 DAG 到达终态。因此，外层工具在子代理仍运行时显示 `Waiting for output…` 是预期行为，不能仅据此判定为死锁。执行中的权威状态来自 Plan 事件：在 GUI 右侧 Plan 中点击子代理节点，即可查看会话记录、功能打点、事件时间线、工具活动和最终输出。

当前 summary 节点会拼接各子代理输出；长审查或大量工具输出可能使外层工具结果超过单条 provider context 的预算。出现“结果过大、无法读取完整内容”时，不能据此转述或推断审查结论，应以节点详情中的会话与工具证据为准。完整结果的可靠交付需要有界摘要和可分页的结果引用；在该交付契约落地前，不应把外层 `final_output` 当作大结果的唯一读取通道。

### 4. 上下文不是无限聊天记录，而是一条有预算的 Context Pipeline

Seelex 把 **Context Engineering** 实现为可组合的 Session Components：

<code>Assembler → ToolResultProcessor → Compressor → ContextController</code>

核心决策包括：

- 从模型总 context window 中扣除输出预留和 12.5% 安全余量，再计算输入预算。
- System Prompt 与当前 Plan/Task/Skill 等工作栈不参与历史压缩。
- 最新 N 轮保留原文，只压缩滑动窗口之外的历史。
- 超大 Tool Result 保存为 immutable <code>result_ref</code>，模型先看到有界摘要，需要时再调用 read-back 工具。
- Provider History 必须保持 assistant/tool call 配对，不能把孤立 Tool Result 重新注入模型。
- 父子 Agent 之间传递结构化 Goal、Constraint、Decision、Finding 和 PendingWork，而不是无界复制完整 transcript。

这套 **Token Budgeting + Context Compression + Selective Retrieval** 策略的目标不是制造“无限上下文”错觉，而是在成本、可审计性和任务连续性之间保持确定边界。

### 5. Workspace Sandbox 与 Permission Policy 是两层独立安全边界

Seelex 没有只依赖 Prompt 告诉模型“不要访问项目外文件”。所有文件和 Shell 工具先经过：

1. **ProjectScope**：把目标解析为 canonical absolute path，并验证它仍位于绑定 workspace root 内。
2. **PathGate / Permission Gate**：在合法项目范围内进一步计算 allow、ask 或 deny。

ProjectScope 解决“能否逃出项目目录”的物理边界；PathGate 解决“项目内哪些操作仍需要审批”的策略边界。两者不能互相替代。

默认权限模式是 <code>manual</code>。Plugin tool visibility、Human-in-the-loop approval 和 scoped tool dispatch 在请求时共同生效，隐藏工具即使被模型构造出调用也会被拒绝。Windows Shell 使用显式系统 PowerShell、<code>-NoProfile</code> 和 <code>-NonInteractive</code>，降低 profile 注入、WSL shim 命中和交互阻塞风险。

### 6. 会话持久化采用 Immutable Generation，而不是原地覆盖大 JSON

所有存储后端先按 <code>project_id</code> 分区，再按 <code>session_id</code> 隔离。History 被拆成固定大小的 immutable shards，manifest 只在新 generation 完整写入后原子切换：

- 读者只能看到旧的完整 generation 或新的完整 generation。
- 中途失败的 shard 不会被发布为当前会话。
- JSON、SQLite、PostgreSQL 和 Redis 保持相同的逻辑 snapshot 语义。
- Redis 使用 project hash tag，让 manifest、shard、state 和 index 位于同一 Cluster slot。

Provider History、append-only Transcript Event、Application State 和 immutable Tool Result 分开保存。这个决策避免“为了恢复模型上下文而覆盖用户可见事实”，也让 Plan、标题、工具来源和压缩 checkpoint 可以独立演进。

### 7. Plugin 切换是事务，而不是修改一个 current 字段

一个 Plugin 同时影响 Tool include/exclude、System Prompt、Skill visibility 和 MCP Server。Seelex 的 Plugin Manager 采用 prepare/switch/cleanup 顺序：

1. 先准备目标 Plugin 需要的新 MCP 连接。
2. 再切换 Tool visibility 与 Skill scope。
3. 成功后拆除旧 MCP。
4. 任一步失败都按逆序恢复先前状态。

工具可见性以请求级 snapshot 传入 Runtime，避免正在执行的请求观察到一半新、一半旧的能力集合。这使 Plugin 可以作为 Agent 的专业形态切换机制，而不必为 read、write、git、shell 或 CAD 工作流分别维护多套二进制。

### 8. 账号池按角色和分支路由，并把租约保持到流结束

模型账号按 <code>agent</code>、<code>subagent</code>、<code>goalplan</code> 等 role 注册到 Account Pool。同步请求和流式请求共享路由规则，但流式请求会把 lease 保持到 EOF、错误或显式 Close，避免响应过程中被其他请求抢占或切换账号。

Plan branch 使用 role + branch ID 的确定性 hash 选择账号；显式 AccountID binding 可以直接 pin。这个决策让相同 DAG 的账号路由可复现，同时降低多个并发 Subagent 争用同一模型额度和可变状态的概率。

### 9. 测试 Harness 与生产 Harness 共用公开契约

<code>e2e/scenario</code> 使用 scripted engine、fixture、event recorder 和 Application ports 构造确定性用户旅程，不依赖真实 LLM、外部网络或秘密配置。测试验证的是 Submit、Tool lifecycle、Interaction、Snapshot/Event 和最终可观察结果，而不是私有字段。

生产运行时与测试 Harness 共享 Application contract，使 Tool Calling、Plan projection、审批和前端协议可以在离线 CI 中重复验证；真实模型 smoke test 则保持显式启用，避免普通测试把 API 可用性误当成代码正确性。

## 快速开始

### 1. 准备环境

- Go 1.25+
- 一个支持 OpenAI-compatible Chat Completions 的模型 endpoint
- Git

~~~bash
git clone https://github.com/RedHuang-0622/seelex.git
cd seelex
~~~

### 2. 创建本地账号配置

Linux/macOS：

~~~bash
cp config/accounts.example.yaml config/accounts.yaml
~~~

PowerShell：

~~~powershell
Copy-Item config/accounts.example.yaml config/accounts.yaml
~~~

编辑 <code>config/accounts.yaml</code>。最小配置如下：

~~~yaml
defaults:
  provider: openai
  context_window: 128000
  max_tokens: 8192
  timeout: 120s
  temperature: 0

roles:
  agent:
    - model: your-model
      base_url: https://your-openai-compatible-endpoint/v1
      api_key: replace-with-your-api-key

  # 可选：Plan 节点使用的独立账号或快速模型
  subagent:
    - model: your-subagent-model
      base_url: https://your-openai-compatible-endpoint/v1
      api_key: replace-with-your-api-key
~~~

<code>config/accounts.yaml</code> 已被 Git 忽略。不要提交真实 API key、token、DSN 或本机私有配置。

### DeepSeek 示例

DeepSeek API 提供 OpenAI-compatible 接口时，可以继续使用 <code>provider: openai</code>，并替换模型与 endpoint：

~~~yaml
defaults:
  provider: openai
  context_window: 128000
  max_tokens: 8192

roles:
  agent:
    - model: deepseek-chat
      base_url: https://api.deepseek.com
      api_key: replace-with-your-deepseek-api-key

  subagent:
    - model: deepseek-chat
      base_url: https://api.deepseek.com
      api_key: replace-with-your-deepseek-api-key
~~~

具体兼容性取决于 endpoint 是否支持项目所需的流式响应、工具调用和对应模型参数。Seelex 不对所有 OpenAI-compatible 服务作统一兼容承诺。

### 3. 运行 TUI

直接运行：

~~~bash
go run .
~~~

或构建本地二进制：

~~~bash
go build -o seelex .
./seelex
~~~

Windows：

~~~powershell
go build -o seelex.exe .
.\seelex.exe
~~~

### 4. 运行 GUI

GUI 需要显式 build tags：

~~~bash
go run -tags "gui,desktop,production" . -frontend gui
~~~

Windows 开发构建也可以使用：

~~~powershell
make rebuild-gui VERSION=dev
~~~

GUI 使用系统 WebView，当前仍处于 Alpha 阶段。日常开发和问题排查建议优先使用 TUI。

## 常用启动参数

| 参数 | 默认值 | 说明 |
|---|---|---|
| <code>-frontend</code> | <code>tui</code> | 选择 <code>tui</code> 或 <code>gui</code> |
| <code>-store</code> | <code>.seelex/sessions</code> | 会话持久化路径 |
| <code>-plugins</code> | <code>plugins</code> | Plugin 搜索路径，多个路径用逗号分隔 |
| <code>-permission</code> | <code>manual</code> | <code>manual</code> 为白名单外审批；<code>full_access</code> 全部放行 |
| <code>-version</code> | — | 输出版本并退出 |

<code>full_access</code> 会放宽工具审批，仅应在明确受控的工作区内使用。

## Plugin、Skill 与 MCP

内置 Plugin 位于 <code>plugins/</code>：

| Plugin | 用途 |
|---|---|
| <code>default</code> | 默认完整能力 |
| <code>read</code> | 只读检索与分析 |
| <code>write</code> | 文件和代码修改 |
| <code>git</code> | Git 工作流 |
| <code>shell</code> | Shell/DevOps |
| <code>freecad</code> | CAD 垂直能力验证 |

每个 Plugin 通过 <code>plugin.md</code> 声明工具 include/exclude、System Prompt、Skill 和可选 MCP Server。激活失败时，Manager 会回滚工具、Skill、MCP 和当前 Plugin 状态，避免留下半激活运行时。

Skill 使用 <code>&lt;skill&gt;/SKILL.md</code> 目录结构；相关脚本和资源保存在同一 Skill root 内。资源路径会经过 canonicalization 和逃逸检查。

## 会话、项目与存储

- Workspace 保存项目目录和 Session binding。
- ProjectScope 把文件、Shell 和工作目录限制在绑定的项目 root 内。
- SessionStore 以 <code>(project_id, session_id)</code> 为隔离键。
- JSON/SQLite 适合本地使用；PostgreSQL/Redis 需要外部服务。
- Provider history、可见 transcript、Plan 状态和工具结果使用不同的数据边界，避免模型历史覆盖应用事实。

默认存储路径是 <code>.seelex/sessions</code>。

## 项目结构

| 目录 | 职责 |
|---|---|
| [<code>application/</code>](application/README.md) | 稳定应用层：Chat、Task、Plan、审批、会话、项目和 Snapshot/Event |
| [<code>seelebridge/</code>](seelebridge/README.md) | Seele 防腐层、工具、账号池、Plan、MCP、ProjectScope 与 PathGate |
| [<code>seelexctx/</code>](seelexctx/README.md) | 上下文装配、预算、压缩、快照和父子 Agent merge-back |
| [<code>sessionstore/</code>](sessionstore/README.md) | JSON/SQLite/PostgreSQL/Redis 持久化 |
| [<code>plugin/</code>](plugin/README.md) | Plugin loader、生命周期和事务式切换 |
| [<code>skill/</code>](skill/README.md) | Skill 加载、资源安全和可见性 |
| [<code>workspace/</code>](workspace/README.md) | Workspace 与 Session binding |
| [<code>mcpstack/</code>](mcpstack/README.md) | MCP 调用轨迹、持久化和上下文摘要 |
| [<code>tui/</code>](tui/README.md) | Bubble Tea 终端前端 |
| [<code>gui/</code>](gui/README.md) | Wails GUI 适配层与前端 |
| [<code>e2e/</code>](e2e/README.md) | 无真实 LLM 的确定性端到端场景 |
| [<code>docs/</code>](docs/README.md) | 架构、产品、研究、测试和研发记录 |

## 构建与验证

仓库 CI 在 Windows、Linux 和 macOS 上执行构建与测试。主要本地检查：

~~~bash
gofmt -l .
go build ./...
go vet ./...
go test ./... -count=1 -timeout=120s
node --test gui/frontend/dist/*.test.mjs
~~~

GUI 构建检查：

~~~bash
go build -tags "gui,desktop,production" ./...
~~~

Linux CI 还会执行 race detector、覆盖率和发布包安全检查。

2026-08-03 在当前工作树执行 `go test ./... -covermode=count -coverprofile=coverage.out` 的可复现结果为全仓 **62.8%**；关键包分布为 `application/core` **75.6%**、`seelebridge` **66.7%**、`tui` **26.1%**、`workspace` **68.4%**。这个分布也暴露了剩余风险：TUI 虽已补到 26.1%，仍低于核心编排层，不应只用全仓平均值掩盖前端交互测试不足。CI 使用同类命令，并上传 `coverage.out` 与 `coverage-summary.txt` 供复核。

## 当前状态与边界

以下限制是当前项目状态的一部分：

- 项目仍处于 Developer Alpha，CLI、配置字段和持久化 schema 可能继续调整。
- TUI 是默认入口；GUI 功能较完整，但仍依赖平台 WebView，属于 Alpha。
- 当前 Plan 是同一进程内由主 Agent 编排多个独立节点 Session，不是跨进程或跨组织的完整 A2A Protocol 实现。
- OpenAI-compatible 不等于完全行为一致；工具调用、流式协议和模型参数仍需按 provider 验证。
- 项目尚未发布 SWE-bench、Terminal-Bench 等标准化编码基准结果。
- PostgreSQL、Redis、MCP 和外部 Web Search 的真实部署需要各自的服务与配置。

如果你正在寻找稳定 API 或无人值守生产服务，请先审查对应模块 README、测试和变更记录，再决定是否采用。

## 文档

- [文档索引](docs/README.md)
- [架构索引](docs/arch/README.md)
- [功能打点与指标](docs/feature-instrumentation.md)
- [内置 Plugin](plugins/README.md)
- [GUI 设计与协议](docs/gui/README.md)

模块 README 描述当前实现；<code>docs/YYYY-MM-DD-topic/</code> 下的文件主要是阶段性方案与研发记录，不应被视为所有功能均已完成的证明。

## 参与开发

项目当前仍主要由原作者维护；欢迎提交可复现 Issue、设计审查或 Pull Request，但仓库不会把自动化提交包装成外部贡献。开始修改前，请阅读 [贡献指南](CONTRIBUTING.md)、[安全策略](SECURITY.md)、[行为准则](CODE_OF_CONDUCT.md)、[AGENTS.md](AGENTS.md) 和目标模块的 README，并保持以下约束：

- 前端只消费 Application DTO/Event，不复制业务状态机。
- 上游 Seele 能力优先通过 <code>seelebridge/</code> 适配。
- 新增工具必须经过 ProjectScope、权限和可见性边界。
- 行为变化应同时更新测试和对应模块文档。
- 不提交账号文件、API key、DSN、token 或本机绝对路径。

## License

[MIT](LICENSE)
