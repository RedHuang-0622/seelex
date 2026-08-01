# Seelex — 可切换专业形态的工科全栈 Agent

> 当前发布通道：`v0.1.0-alpha.1` Developer Alpha。TUI 为默认入口，桌面 GUI 为显式启用的 Alpha 功能。

**Seelex** 是面向工程研发、设计与交付全过程的全栈 Agent。它以本地重构后的 [Seele](https://github.com/RedHuang-0622/Seele)（无产品语义的 Agent runtime）作为执行内核，在其上实现产品语义层：任务/计划编排、上下文成本治理、账号池、存储模型与双前端。

**能力边界（与 Seele 的分工）：**

> Seele 提供执行能力（agent/session/tools/workplan/seelectx/accountpool/event/telemetry）；Seelex 决定何时调用、调用什么、上下文放什么、费用如何归集。

| 能力 | 所有者 |
|---|---|
| LLM 装配、会话 ReAct 循环、WorkPlan DAG 内核、账号租约、工具分发、事件/遥测原语 | Seele |
| Task 生命周期、Plan 产品 DSL（`plan_load`/`plan_run`）、节点 kind 解释、工具实现、插件可见性、上下文压缩策略、Token 账本、EventStore | Seelex |

## 功能特性

### 🧠 多模型账号池
- 支持 **OpenAI / Anthropic ** 等主流 API 与定制 Provider（`base_url`）
- **P2C 账号池 + 租约**：多账号按角色（`agent`/`subagent`/`goalplan`）注册，按需获取、防超售；流式响应 lease-until-EOF
- 计划 DAG 分支按账号**确定性 hash 分配**，可多账号并行执行
- 运行时 `/account` 切换选中账号/provider，provider 过滤经 selector 闭包生效

### 📋 Plan 编排（WorkPlan 产品 DSL）
- `plan_load`/`plan_run`/`plan_clear` 是普通产品工具：`codec.Import` 导入 `nodes+edges` 规范 JSON，拓扑校验 + 策略校验后加载
- **Plan→subagent**：`plan_run` 用 WorkPlan 内核执行 DAG，agent 节点是独立 Session（`SeelexAgentNode` 注入节点作用域、父证据与预算），可真并行
- 节点事件经 `event.Sink` 投影到 sessionstore 事件库，GUI/TUI 的 PlanState/SubAgentTree 由投影驱动
- 规划/重规划走**隔离规划会话**（独立 Completer、强制 `tool_choice=plan_load`、不消耗主账号租约）

### ✅ Task 生命周期（与 Plan 分离）
- `TaskService` 只消费会话结果、工具事件与 **WorkPlan 事件投影**，不直连执行器
- 终态工具 `task_complete`/`task_failed`/`task_needs_user_decision` 校验投影收敛后接受
- `taskExecutionState` 是功能打点快照（`docs/feature-instrumentation.md`），终态即亡不持久化；恢复只依赖 TaskFrame 与事件投影

### 🧠 上下文成本治理（核心设计）
- **两级存储作用域**：项目粒度只有 ProjectKnowledge（跨会话共享的模块语义，hash 版本化，会话前预读）；会话粒度完全独立
- 会话上下文 = **SystemPrompt（永不压缩）+ 4 个使用栈（plan/task/skill/compact，now using = 栈顶）+ ChatQueue（append-only）**
- **滑动窗口**：最新 N 轮原样进入请求（`ReadEventTail` 按轮/预算读）；**只压缩窗口外轮次**，产物 push CompactStack（合并式摘要，栈顶自足，`From/To` 可审计）
- 窗口 N 由 `WindowPolicy` 推导（配置 `window.rounds/ratio/min_rounds/max_rounds` 显式覆盖，默认 ratio=0.7/4/40），代码无魔法数字
- 超大工具输出先归档 `result_ref`（`read_tool_result` 按需读回），SystemPrompt 与栈帧永不进 ProviderHistory
- seelectx 五件套（DurableHistory/Assembler/ToolResultProcessor/Compressor/ContextController）注入主会话与节点会话

### 🔌 Plugin 形态切换系统
- 运行时通过 `switch_plugin` 或 `/plugin <name>` 切换 Agent 专业形态
- 每个 Plugin 限定 **工具集 + Skill + System Prompt + MCP Server**
- 插件 Manager 维护 include/exclude 快照；`VisibilityPolicy` 每请求过滤可见工具集，隐藏工具 Dispatch 拒绝（`ErrToolNotVisible`）
- 事务式激活/停用，失败自动回滚

### 📜 Skill 技能系统
- 目录化加载（`plugins/<name>/<skill>/SKILL.md`），`#skillname 需求` 激活并发送，`#end` 退栈
- 活动 Skill 以条目化用户上下文发送，不写入 system prompt

### ⚡ Effort 等级控制
- 4 档思考深度：`lite` / `medium` / `high` / `max`，控制 MaxLoops 与 PlanPolicy 约束
- 状态栏实时显示 `E:high`，`Alt+E` 快捷循环切换

### 🖥️ TUI 交互终端
- 基于 **Bubble Tea**，流式聊天、命令补全、工具事件面板
- 命令系统：`/help`、`/model`、`/plugins`、`/effort`、`/sessions`、`/new`、`/account`

### 🪟 桌面 GUI（Alpha）
- 与 `tui/` 同级的 `gui/`，复用同一 `application.Service`
- 使用 `-tags "gui,desktop,production"` 构建（`make rebuild-gui VERSION=<tag>`），默认 TUI 构建不依赖 WebView

### 🔧 跨平台可执行
- 预编译 Windows amd64、Linux amd64、macOS amd64/arm64 CLI 二进制（`make release`）
- 静态编译（CGO_ENABLED=0），零运行时依赖

## 快速开始

### 前置

- Go 1.25+；本仓库通过 `go.work` 引用本地 `../Seele`（重构后的执行内核），`go.mod` 带 `replace github.com/RedHuang-0622/Seele => ../Seele`

### 配置账号

按 [`config/accounts.example.yaml`](config/accounts.example.yaml) 的 `roles` 分组格式创建 `config/accounts.yaml`（已 gitignore，**切勿提交**）：

```yaml
roles:
  agent:     # 默认交互助理
    - model: your-agent-model
      base_url: https://api.openai.com/v1
      api_key: your-api-key
  subagent:  # 计划子代理（可选，快任务）
    - model: your-subagent-model
      base_url: https://api.openai.com/v1
      api_key: your-api-key
```

未配置时回退到 `OPENAI_API_KEY` 环境变量（agent 角色）。

### 运行 TUI

```bash
go build -o seelex.exe .
./seelex.exe
```

### 构建 GUI（Windows Dev）

```bash
make rebuild-gui VERSION=<tag>    # 需 LOCAL_CONFIG 指向本地真实配置
```

## 模块导航

| 模块 | 职责 |
|---|---|
| [`application/`](application/) | 服务层：Chat、TaskService、Effort/PlanPolicy、审批、事件编排；前端唯一消费入口（`application/model` DTO 稳定） |
| [`seelebridge/`](seelebridge/) | Seele 适配层：Runtime composition root（账号池/工具注册表/主会话/事件 Sink）、Plan 产品工具与节点作用域、插件可见性、MCP provider |
| [`seelexctx/`](seelexctx/) | seelectx 契约适配：Assembler/ToolResultProcessor/Compressor/ContextController、跨会话承袭子包（snapshot/provider/compactor/merger） |
| [`sessionstore/`](sessionstore/) | 原子、项目作用域的持久化（JSON/SQLite/PG/Redis 四后端）：DurableHistory、SessionContextRecord（5 栈）、ProjectRecord、事件库 |
| [`session/`](session/) | 会话用例适配器 |
| [`workspace/`](workspace/) | 项目与 session binding、路径沙盒（ProjectScope/PathGate） |
| [`plugin/`](plugin/) + [`plugins/`](plugins/) | Plugin 生命周期与声明式能力包 |
| [`skill/`](skill/) | Skill 加载与可见性 |
| [`tui/`](tui/) | Bubble Tea 终端前端 |
| [`gui/`](gui/) | Wails 桌面前端（Alpha），权威设计见 `docs/gui/` |
| [`internal/promptassets/`](internal/promptassets/) | 产品 prompt 模板（plan preflight/replan 等） |

## 存储模型

```text
项目粒度（跨会话共享，只读）
└── ProjectKnowledge：项目通用模块语义（hash 版本化，变更才重建）

会话粒度（互相独立）
└── SessionContextRecord
      ├── SystemPrompt   —— 永不压缩，始终完整进入请求
      ├── PlanStack      —— now using plan
      ├── TaskStack      —— now using task
      ├── SkillStack     —— now using skill
      ├── CompactStack   —— now using compact context（窗口外压缩摘要）
      └── ChatQueue      —— append-only 轮次队列
            ├── 滑动窗口：最新 N 轮原样保留
            └── 窗口外：唯一可压缩部分
```

## 文档索引

- 架构与设计：[`docs/README.md`](docs/README.md)（含 Seele v0.0.8 迁移架构、上下文改进、MCP 存储解耦、功能打点）
- 底层重构详设：[`docs/2026-08-01-seele-v2-underlying-refactor/plan.md`](docs/2026-08-01-seele-v2-underlying-refactor/plan.md)

## 构建与发布

```bash
make build              # 仅构建跨平台二进制
make release            # clean -> build -> package（tar.gz + 配置 + 插件）
make rebuild-gui        # Windows Dev GUI（需 LOCAL_CONFIG）
make publish-rebuild-gui  # Windows Publish GUI（仅 example 配置）
```
