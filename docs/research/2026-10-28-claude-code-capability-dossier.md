# Claude Code 能力与架构档案（截至 2026-10-28）

> 调研方式：全部结论来自当日抓取的官方一手资料 —— `code.claude.com/docs`（含其 LLM 索引 `llms.txt`）、`CHANGELOG`、npm/PyPI registry、GitHub `anthropics/claude-code` 仓库与 issue 搜索、Anthropic engineering/blog 链接。
> 每条事实后附 URL。**「文档确认」= 官方文档/仓库原文明确写出；「推测」= 无官方文字，由仓库结构、授权条款或第三方分析推断，需二次验证。**
>
> 抓取时间：2026-10-28 23:47（本地）。当前 CLI 最新版 **2.1.312（2026-10-27）**。

---

## 0. 一句话结论

Claude Code 在 2026-10 已经不是「一个终端 CLI」，而是**一套闭源 harness + 一个多入口产品矩阵**：同一个 agent loop 被 CLI / IDE 插件 / 桌面 app / 云端 web 会话 / Agent SDK（Python+TS）/ GitHub Action / Slack 共用；2026 年新增的重心是 **并行与编排**（agent teams、dynamic workflows、background sessions + supervisor 进程）和 **权限自动化**（auto mode 分类器成为默认模式），而用户侧最大痛点集中在**用量额度与时长限制**。

---

## 1. 形态与入口

### 1.1 终端 CLI（主形态）
- 推荐安装是 **native installer**：`curl -fsSL https://claude.ai/install.sh | bash`（macOS/Linux/WSL）、`irm https://claude.ai/install.ps1 | iex`（Windows PowerShell）、`curl claude.ai/install.cmd`（CMD）；另有 Homebrew cask（`claude-code` 稳定通道 / `claude-code@latest`）、WinGet、apt/dnf/apk。[来源](https://code.claude.com/docs/en/overview)
- Native 安装会**后台自动更新**；Homebrew/WinGet 不会自动更新。[来源](https://code.claude.com/docs/en/overview)
- 非交互（headless）入口是 `-p/--print`，可配 `--output-format json|stream-json`、`--allowedTools`、`--max-turns`、`--max-budget-usd`；另有 `--bare` 精简启动（跳过 hooks/skills/subagents/plugins/MCP/auto memory/CLAUDE.md 的自动发现），官方明确「`--bare` 是脚本与 SDK 调用的推荐模式，未来会成为 `-p` 的默认」。[来源](https://code.claude.com/docs/en/headless)

### 1.2 Agent SDK（Python + TypeScript）
- SDK 的定位被写死为「**a library that runs the Claude Code binary**」，提供与 CLI 同一套 tools / agent loop / 权限 / sessions / hooks / subagents / MCP / skills / plugins。[来源](https://code.claude.com/docs/en/agent-sdk/overview)
- 只支持 **Python 与 TypeScript**；其它语言要求「把 CLI 当子进程跑」（`-p --output-format json`）。[来源](https://code.claude.com/docs/en/agent-sdk/overview)
- 版本（registry 实测）：TS `@anthropic-ai/claude-agent-sdk` **0.3.312**，Python `claude-agent-sdk` **0.2.174**。[来源](https://registry.npmjs.org/@anthropic-ai/claude-agent-sdk/latest) / [来源](https://pypi.org/pypi/claude-agent-sdk/json)
- 官方限制：**不允许第三方开发者在其产品中提供 claude.ai 登录或限额**，必须用 API key 认证。[来源](https://code.claude.com/docs/en/agent-sdk/overview)

### 1.3 GitHub Actions / GitLab CI
- `claude-code-action`：在 workflow 里 `@claude` 触发，或用 prompt 在任何 GitHub event 上跑；用 `/install-github-app` 一键装 App + secret（`ANTHROPIC_API_KEY` 或 `CLAUDE_CODE_OAUTH_TOKEN`）。[来源](https://code.claude.com/docs/en/github-actions)
- 该 Action **构建在 SDK 之上**；GitLab 走独立 CI/CD 集成；GHES 有专门页面支持（含插件市场）。[来源](https://code.claude.com/docs/en/github-actions)
- 支持用 Bedrock / Google Cloud Agent Platform / Microsoft Foundry 代替 Claude API 跑 Action。[来源](https://code.claude.com/docs/en/github-actions-cloud-providers)

### 1.4 桌面 app
- 独立 app（不再是 IDE 插件）：可视化 diff 评审、并排多 session、**定时任务**、启动 cloud session、集成终端与编辑器、side chats、computer use、Dispatch（手机派活）、app preview、PR 监控、企业配置。[来源](https://code.claude.com/docs/en/overview) / [来源](https://code.claude.com/docs/en/desktop)
- 平台：macOS（Intel+AS）、Windows x64/ARM64、Ubuntu/Debian（**beta**）；**app 内已含 Claude Code，无需单独装 CLI**。[来源](https://code.claude.com/docs/en/overview)

### 1.5 Web / 云会话 / 手机
- `claude.ai/code`：云端跑，无需本地环境，支持并行任务、`Projects`（让 Claude 协调多个并行 session）、iOS/Android app。[来源](https://code.claude.com/docs/en/overview) / [来源](https://code.claude.com/docs/en/claude-code-on-the-web)
- 本地↔云互通：`claude --cloud` 推到云、`claude --teleport` 拉回终端、`/desktop` 把终端会话交给桌面 app、Remote Control 从手机/浏览器接管**本地**执行。[来源](https://code.claude.com/docs/en/overview)
- 执行环境被官方分为三类：**Local**（本机）、**Cloud**（Anthropic 托管 VM 或组织自托管的 self-hosted environments）、**Remote Control**（本机执行、浏览器控 UI）。[来源](https://code.claude.com/docs/en/how-claude-code-works)

### 1.6 其它入口（容易漏掉）
VS Code / Cursor 扩展、JetBrains 插件（需另装 CLI）、Slack / Claude Tag、Chrome 扩展、CLI 内 computer use（仅 macOS）、`claude-cli://` deep links、channels（把 CI/IM/webhook 事件推进运行中的 session）。[来源](https://code.claude.com/docs/en/overview) / [来源](https://code.claude.com/docs/en/channels)

> **注意**：`Managed Agents`（platform.claude.com）是**另一个产品**——Anthropic 托管 agent harness + 托管/自托管 sandbox；与 Agent SDK（你自己进程里跑）不是一回事。[来源](https://code.claude.com/docs/en/agent-sdk/overview)

---

## 2. 实现与分发：闭源 + 原生二进制

### 2.1 闭源（文档确认 · 仓库证据）
- 仓库 `anthropics/claude-code` 的 `LICENSE.md` 全文：`© Anthropic PBC. All rights reserved. Use is subject to Anthropic's Commercial Terms of Service.`[来源](https://github.com/anthropics/claude-code/blob/main/LICENSE.md)
- 仓库根目录只有 `CHANGELOG.md`、`README.md`、`plugins/`、`mods/`、`examples/`、`scripts/`、`demo.gif` 等，**没有任何 `src/`**；它是 issue tracker + 变更日志 + 示例插件仓库，不是源码仓库。[来源](https://github.com/anthropics/claude-code)
- 仓库元数据：`language: TypeScript`，star ≈153.7k，created 2025-02-22，pushed 2026-10-27，open issues ≈15,885。[来源](https://api.github.com/repos/anthropics/claude-code)

### 2.2 分发形态 = 逐平台原生二进制（文档确认）
- 「**npm 包安装的是与 standalone installer 相同的 native binary**」，npm 通过 per-platform optional dependency（`@anthropic-ai/claude-code-darwin-arm64` 等）拉二进制，postinstall 链接；支持矩阵 `darwin-arm64 / darwin-x64 / linux-x64 / linux-arm64 / linux-x64-musl / linux-arm64-musl / win32-x64 / win32-arm64`。[来源](https://code.claude.com/docs/en/setup)
- npm 路径**只要求 Node ≥22** 作为包装器运行时（registry `engines.node: >=22.0.0`），不是「Node 应用」。[来源](https://registry.npmjs.org/@anthropic-ai/claude-code/latest)
- **npm 安装已被官方标记为 deprecated**，README 把 native installer 列为首选。[来源](https://github.com/anthropics/claude-code/blob/main/README.md)
- 二进制完整性：每次发布带 `manifest.json`（各平台 SHA256），用 Anthropic GPG key 签名；Linux 走签名 apt/dnf/apk 仓库。[来源](https://code.claude.com/docs/en/setup)
- 双发布通道：stable（约落后一周、跳过重大回归版本）与 latest。[来源](https://code.claude.com/docs/en/overview)

> **推测（未获文档确认）**：二进制内部实现语言/运行时（是否 Rust 重写、是否 Bun 打包）官方从未写明；仓库 `language: TypeScript` 只是 GitHub 对仓库内容的猜测。第三方「源码逆向」文档（如 `claude-code-best/claude-code`、`openedclaude/claude-reviews-claude`）声称读到 `src/services/compact/microCompact.ts` 等路径，**这些不是一手资料，只能当线索**。[线索来源](https://github.com/openedclaude/claude-reviews-claude/blob/main/architecture/11-compact-system.md)

---

## 3. Agent 循环与编排

### 3.1 循环本体
- 三阶段：**gather context → take action → verify results**，可循环、可被用户随时打断；官方给这个「模型 + 工具 + 上下文管理」的外壳起了名字：**agentic harness**。[来源](https://code.claude.com/docs/en/how-claude-code-works)
- 内置工具分五类：文件操作 / 搜索 / 执行 / Web / 代码智能（LSP，需 code intelligence 插件）。[来源](https://code.claude.com/docs/en/how-claude-code-works)

### 3.2 subagents（`.claude/agents`）
- 定义是 **Markdown + YAML frontmatter**，作用域与优先级：`--agents` CLI JSON（session）> `.claude/agents/`（项目，会从 cwd 向上递归扫描）> `~/.claude/agents/`（用户级）> plugin agents；目录可递归分子目录。[来源](https://code.claude.com/docs/en/sub-agents)
- frontmatter 字段（实测清单）：`name`、`description`、`tools`、`disallowedTools`、`model`、`permissionMode`、`maxTurns`、`autoCompactWindow`、`skills`、`mcpServers`、`hooks`、`memory`(`user|project|local`)、`background`、`omitClaudeMd`、`effort`、`isolation: worktree`、`color`、`initialPrompt`、`experimental.cacheTtl`。[来源](https://code.claude.com/docs/en/sub-agents)
- 内置 subagent：**Explore**（只读、按 quick/medium/very thorough 分档）、**Plan**（plan mode 里的研究代理）、**general-purpose**、`claude`（兜底/后台 session 默认 agent）、`statusline-setup`、`claude-code-guide`。Explore/Plan 默认**不加载 CLAUDE.md 与 git status** 以降本。[来源](https://code.claude.com/docs/en/sub-agents)
- **默认在后台跑**（除非特定前台场景），后台 subagent 的权限询问会浮到主 session 并标注来源。[来源](https://code.claude.com/docs/en/tools-reference)
- 并发上限：默认 **20 个同时在跑**（`CLAUDE_CODE_MAX_CONCURRENT_SUBAGENTS` 可调，v2.1.217+），另有嵌套深度限制。[来源](https://code.claude.com/docs/en/sub-agents)
- 可 `resume`（按 agent ID/name 用 `SendMessage` 续跑）、可 fork（fork 继承父对话全文而非空上下文）。[来源](https://code.claude.com/docs/en/sub-agents)
- subagent `description` 合计超 15,000 token 会在启动时告警（它们常驻主上下文）。[来源](https://code.claude.com/docs/en/sub-agents)

### 3.3 「Task 工具」的现状（重要更正）
- **派生 subagent 的工具现在叫 `Agent`**（`Agent(subagent_type=...)`），旧名 Task tool 只存在于历史 changelog 语境；权限规则写法也是 `Agent(Explore)`。[来源](https://code.claude.com/docs/en/tools-reference)
- 与之并存的是**任务清单工具**：`TaskCreate` / `TaskGet` / `TaskList` / `TaskUpdate` / `TaskStop`；`TodoWrite` 默认被禁用（`CLAUDE_CODE_ENABLE_TASKS=0` 可回退）。
- `TaskOutput` **已于 v2.1.277 移除**，改由 `Read` 读后台任务输出文件。[来源](https://code.claude.com/docs/en/tools-reference) / [来源](https://code.claude.com/docs/en/changelog)
- **推测**：`Task*` 系列工具只在「列在 Task tool availability 的模型」上默认提供，官方未在该表外解释筛选逻辑。

### 3.4 plan mode
- 是**权限模式之一**（`plan`）：读文件、跑探索命令、写计划，**不改源码**；计划通过 `ExitPlanMode` 提交审批，批准后按你选的选项切到 auto 或逐条审阅。[来源](https://code.claude.com/docs/en/permission-modes)
- 探索期命令的处理分三档：bypass 可用的交互终端 → 不审不问；auto 可用且 `useAutoModeDuringPlan` 开（默认）→ 分类器审；否则只读集外的命令要问。[来源](https://code.claude.com/docs/en/permission-modes)
- 计划文本**在 compaction 后从磁盘重注入**，不会随摘要丢失。[来源](https://code.claude.com/docs/en/context-window)

### 3.5 后台任务与并行（2026 年的重头）
- Bash 工具 `run_in_background: true` + `/tasks` 管理；`Monitor` 工具可跑后台命令并**逐行把输出喂回 Claude**，也能开 WebSocket 把每条消息当事件。[来源](https://code.claude.com/docs/en/tools-reference)
- 无人值守场景（`-p`、SDK、CI、云 session）后台命令有**时限**：默认 30 分钟、最大 2 小时（v2.1.285+）。[来源](https://code.claude.com/docs/en/tools-reference)
- **background sessions + agent view**：由独立 **supervisor 进程**托管（`claude daemon status` 可看状态/版本/socket/worker 数），关掉终端也继续跑，状态落盘、跨自动更新与 supervisor 重启存活。[来源](https://code.claude.com/docs/en/agent-view)
- **agent teams**：lead agent 监督多个 peer session，有共享任务列表与 agent 间消息（`SendMessage` / `ListAgents` / `TeammateIdle` hook）。[来源](https://code.claude.com/docs/en/agent-teams)
- **dynamic workflows**：Claude 写一段 **JavaScript 脚本**，运行时在后台执行，编排「几十到几百个」subagent；中间结果存脚本变量而非上下文，可在同一 session 内 resume；`Workflow` 工具需要权限。[来源](https://code.claude.com/docs/en/workflows) / [来源](https://code.claude.com/docs/en/tools-reference)
- **worktrees**：`--worktree`、`isolation: worktree`、`EnterWorktree`/`ExitWorktree` 给并行 session 做 git 隔离。[来源](https://code.claude.com/docs/en/tools-reference) / [来源](https://code.claude.com/docs/en/worktrees)
- 四者的分工官方给了对比表（谁持有计划 / 中间结果在哪 / 规模）：subagents、skills、agent teams、workflows。[来源](https://code.claude.com/docs/en/workflows)

### 3.6 hooks 生命周期
- 触发点极多（实测事件表）：`SessionStart`、`Setup`、`UserPromptSubmit`、`UserPromptExpansion`、`PreToolUse`、`PermissionRequest`、`PermissionDenied`、`PostToolUse`、`PostToolUseFailure`、`PostToolBatch`、`Notification`、`MessageDisplay`、`SubagentStart`、`SubagentStop`、`TaskCreated`、`TaskCompleted`、`Stop`、`StopFailure`、`TeammateIdle`、`InstructionsLoaded`、`ConfigChange`、`CwdChanged`、`DirectoryAdded`、`FileChanged`、`WorktreeCreate`、`WorktreeRemove`、`PreCompact`、`PostCompact`、`PreModelSwitch`、`PostModelSwitch`、`Elicitation`、`ElicitationResult`、`SessionEnd`。[来源](https://code.claude.com/docs/en/hooks)
- 5 种 handler：`command`（stdin 收 JSON）、`http`、`mcp_tool`、`prompt`（送模型）、`agent`；默认超时 600s/30s/60s 分级；支持 `async: true` 后台跑。[来源](https://code.claude.com/docs/en/hooks)
- 优先级硬规则：**deny 规则压过 PreToolUse 的 `"allow"`**；hook 不能绕过 permission rules。[来源](https://code.claude.com/docs/en/permissions)
- 并发：prompt/agent 类 hook 每 session 最多 24 个并行（含 subagent），另加最多 12 个已超时等待的。[来源](https://code.claude.com/docs/en/changelog)

---

## 4. 工具面

### 4.1 内置工具清单（文档表格，节选并注明边界）
`Agent`、`Artifact`、`AskUserQuestion`、`Bash`、`CronCreate`、`CronDelete`、`CronList`、`Edit`、`EndConversation`、`EnterPlanMode`、`EnterWorktree`、`ExitPlanMode`、`ExitWorktree`、`Glob`、`Grep`、`ListAgents`、`ListMcpResourcesTool`、`LSP`、`Monitor`、`NotebookEdit`、`PowerShell`、`PushNotification`、`Read`、`ReadMcpResourceTool`、`RemoteTrigger`、`ReportFindings`、`ScheduleWakeup`、`SendFeedback`、`SendMessage`、`SendUserFile`、`ShareOnboardingGuide`、`Skill`、`SubagentHandback`、`TaskCreate`、`TaskGet`、`TaskList`、`TaskStop`、`TaskUpdate`、`TodoWrite`、`ToolSearch`、`WaitForMcpServers`、`WebFetch`、`WebSearch`、`Workflow`、`Write`。[来源](https://code.claude.com/docs/en/tools-reference)
- 两个反直觉点：`Glob` / `Grep` **在 macOS/Linux/WSL 上默认不存在**（清单里明写 "Absent by default"）；`SubagentHandback` 只在 auto mode 下发给非 fork 的本地 subagent。[来源](https://code.claude.com/docs/en/tools-reference)
- 权限规则语法统一为 `ToolName(specifier)`：`Bash(npm run *)`、`Read(~/secrets/**)`、`Edit(/src/**)`、`Skill(deploy *)`、`Agent(Explore)`、`WebFetch(domain:example.com)`；`Edit(...)` allow 同时给读权限，`Read(...)` deny 同时挡 Write/Edit。[来源](https://code.claude.com/docs/en/tools-reference)

### 4.2 MCP
- 传输：**stdio**（本地命令）、**http**（`streamable-http` 是其别名）、**sse**（**官方标注 deprecated**，仍兼容旧端点）。[来源](https://code.claude.com/docs/en/mcp)
- 配置位置：`.mcp.json`、`~/.claude.json`、`claude mcp add[-json]`；有 `url` 但没 `type` 会被当 stdio 并报错。[来源](https://code.claude.com/docs/en/mcp)
- 规模化的关键设计：**MCP 工具定义默认延迟加载**，靠 `ToolSearch` 按需拉，只有工具名与 server 说明常驻上下文；配套 `WaitForMcpServers`、`ListMcpResourcesTool`/`ReadMcpResourceTool`、MCP elicitation（`Elicitation`/`ElicitationResult` hook）。[来源](https://code.claude.com/docs/en/mcp) / [来源](https://code.claude.com/docs/en/tools-reference)
- 还支持 MCP Apps UI 资源、按 server 的 `requiresUserInteraction` 审批标记、组织级 connector 工具策略（可强设 `ask`）。[来源](https://code.claude.com/docs/en/mcp)

### 4.3 skills
- 形态：`.claude/skills/<name>/SKILL.md`（+ 支持文件目录），**自定义 slash command 已合并进 skills**（`.claude/commands/x.md` 仍可用）。[来源](https://code.claude.com/docs/en/skills)
- 遵循 **Agent Skills 开放标准** `agentskills.io`，Claude Code 在其上扩展了 invocation 控制、subagent 执行、动态上下文注入。[来源](https://code.claude.com/docs/en/skills)
- 内置 bundled skills：`/doctor`、`/code-review`、`/batch`、`/debug`、`/loop`、`/claude-api`、`/run`、`/verify`、`/run-skill-generator` 等；`disableBundledSkills` 可关。[来源](https://code.claude.com/docs/en/skills)
- 上下文成本策略：skill **描述**常驻、正文按需加载；`disable-model-invocation: true` 或 `skillOverrides` 可把描述也移出上下文。[来源](https://code.claude.com/docs/en/how-claude-code-works)

### 4.4 plugins / marketplace
- 一个 plugin = skills + agents + hooks + MCP servers + commands 的打包单元，manifest 为 `plugin.json`，有依赖版本区间（`^1.2`）。[来源](https://code.claude.com/docs/en/plugins/overview) / [来源](https://code.claude.com/docs/en/plugins/manifest-reference)
- 官方维护三个 marketplace：`claude-plugins-official`（官方+合作方，首个交互式 session 自动添加）、`claude-community`、demo `claude-code-plugins`；另有 `anthropics/skills`、`anthropics/knowledge-work-plugins` 等专题市场。安装语法 `/plugin install commit-commands@claude-plugins-official`。[来源](https://code.claude.com/docs/en/plugins/anthropic-marketplaces)
- **mods**（2026 新增，值得注意）：plugin 里注册的 **JavaScript 进程内 hook**，能画 pane / 按钮 / 输入框，能改工具调用与渲染；有 `mods API`、`mod api`、测试 harness 与组织级管控。[来源](https://code.claude.com/docs/en/plugins/mods/overview)
- 有 plugin eval 命令 `claude plugin eval`、plugin 成本/使用计量、组织 allowlist。[来源](https://code.claude.com/docs/en/plugin-evals)

---

## 5. 上下文管理

### 5.1 自动压缩 / `/compact`
- 接近上限时**自动压缩成摘要**（保留请求与关键片段，早期细节可能丢），可用 `/compact focus on ...` 指定保重点。[来源](https://code.claude.com/docs/en/how-claude-code-works)
- 压缩节流保护：若单个文件/工具输出大到「摘要后立刻又满」，**几次之后停止自动压缩并报 thrashing 错误**（有专门 troubleshooting 条目）。[来源](https://code.claude.com/docs/en/how-claude-code-works) / [来源](https://code.claude.com/docs/en/troubleshooting)
- 阈值可调：`/autocompact 500k`、`autoCompactWindow`（subagent 只能往下调）、`CLAUDE_CODE_AUTO_COMPACT_WINDOW`；每个模型的默认阈值有表。[来源](https://code.claude.com/docs/en/model-config) / [来源](https://code.claude.com/docs/en/context-window)
- 局部压缩：`/rewind` 里可选 **Summarize from here / up to here**，只摘一段。[来源](https://code.claude.com/docs/en/context-window)

### 5.2 压缩后什么活下来（官方表格，很有价值）
- system prompt + output style 继续生效；项目根 CLAUDE.md、无 `paths:` 的 rules、auto memory **从磁盘重注入**；skill 描述重建（v2.1.304+）；git status 重新取；plan mode 的计划重注入；已调用的 skill 正文重注入（单 skill ≤5,000 token、总 ≤25,000 token，最旧先丢）；最近读过/改过的文件**重读最多 5 个**；后台命令与后台 subagent 继续跑并提醒 Claude。
- **丢掉的**：`paths:` 作用域的 rules 与子目录 CLAUDE.md（按需重载，实际等于被摘要掉）、hooks 早先注入的上下文。[来源](https://code.claude.com/docs/en/context-window)

### 5.3 CLAUDE.md 层级与 rules
- 加载顺序：**从当前目录一路向上到根**，全部**拼接**（不覆盖），根在上、cwd 在下；同目录内 `CLAUDE.local.md` 追加在 `CLAUDE.md` 之后。子目录的 CLAUDE.md **懒加载**（当 Claude 读/写/编辑该子目录文件时）。[来源](https://code.claude.com/docs/en/memory)
- 支持 `AGENTS.md`（仓库已有其它 agent 的约定时可直接读）；`--add-dir` 目录默认不加载其 CLAUDE.md，需 `CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD=1`。[来源](https://code.claude.com/docs/en/memory)
- `.claude/rules/*.md` 做模块化；`paths:` frontmatter 做路径作用域规则；提供 `claudeMdExcludes` 排除他队文件；HTML 注释在注入前被剥离（省 token）。[来源](https://code.claude.com/docs/en/memory)
- `--setting-sources` 可裁成 `managed-only`（只留组织 managed CLAUDE.md + auto memory）。[来源](https://code.claude.com/docs/en/memory)

### 5.4 auto memory
- Claude 自己写笔记：`MEMORY.md` 的**前 200 行或 25KB（先到者）**在每次 session 启动注入；subagent 也能有独立 memory（`~/.claude/agent-memory/<name>/`、`.claude/agent-memory/`、`.claude/agent-memory-local/`）。[来源](https://code.claude.com/docs/en/memory) / [来源](https://code.claude.com/docs/en/sub-agents)

### 5.5 microcompact
- **在官方文档与 CHANGELOG 中检索不到 "microcompact"/"micro-compact"，也没有 `Old tool result content cleared` 字样**（本次对全部抓取页面与 1.1MB CHANGELOG 做了全文检索）。→ **不是文档确认能力。**
- **第三方逆向分析（非一手，仅作线索）**：称存在三层压缩 —— `microCompact`（就地清空旧工具输出、图片/PDF 超 2000 token 即清、时间衰减配置）、`sessionMemoryCompact`（用持续维护的 session memory 免 API 摘要，保留窗口 10k–40k token）、以及传统 LLM 摘要；并且 microcompact 复用 `cache_edits` 以免破坏 prompt cache。[线索](https://github.com/openedclaude/claude-reviews-claude/blob/main/architecture/11-compact-system.md) / [线索](https://github.com/claude-code-best/claude-code/blob/main/docs/context/compaction.mdx)
- 结论：**microcompact 归入「推测/未确认」**。可确认的近似行为只有官方描述的「MCP 工具定义延迟加载」「skill 正文按需加载」「subagent 独立上下文」「压缩后重读最多 5 个文件」，以及 prompt caching 文档里的 cache 命中机制。[来源](https://code.claude.com/docs/en/prompt-caching)

---

## 6. 会话：resume / fork / 存储

| 入口 | 行为 |
|---|---|
| `claude --continue` | 重开当前目录最近一个 session |
| `claude --resume` | 打开 session picker（`Ctrl+W` 扩到同仓库所有 worktree，`Ctrl+A` 扩到本机所有项目） |
| `claude --resume <name>` / `<transcript-path>` | 按名字或 `.jsonl` 绝对路径恢复 |
| `claude --from-pr <n>` | 过滤到与某 PR 关联的 session |
| `/resume`（会话内） | 切到另一个 session；目标若是**仍在后台运行**的 session，则改为 attach 上去 |
| `--fork-session` / `/branch` | 复制历史到**新 session ID**，原 session 不变 |
| `/fork`（agent view 语境） | 复制当前 session 到后台再继续 |

[来源](https://code.claude.com/docs/en/sessions) / [来源](https://code.claude.com/docs/en/how-claude-code-works) / [来源](https://code.claude.com/docs/en/agent-view)

- 存储：`~/.claude/projects/<project>/<session-id>.jsonl`，`<project>` = 工作目录路径把非字母数字换成 `-`（超 200 字符截断并附加全路径 hash）。每行一个 JSON 对象（消息 / 工具调用与结果 / 请求记录 / 元数据）。**官方明说该格式是内部的、跨版本会变，别写脚本直读**，要用 `/export` 或脚本接口。[来源](https://code.claude.com/docs/en/sessions)
- 保留：默认 30 天（`cleanupPeriodDays`）；`CLAUDE_CONFIG_DIR` 改根目录、`CLAUDE_CODE_PROJECT_DIR_NAME` 自定义目录名、`CLAUDE_CODE_SKIP_PROMPT_HISTORY` 完全不写、`-p --no-session-persistence` 单次不写；`claude purge` 清。`-p`/SDK/`/loop` 起的 session **不进 picker、`--continue` 也跳过**。[来源](https://code.claude.com/docs/en/sessions)
- 恢复时还原：对话历史（压缩过则从 `Conversation compacted` 行开始，不重载被摘要掉的消息）、模型、`--agent`、权限模式（含 plan mode 的特殊规则表）、活跃 goal、未过期的 session 级定时任务；**不还原** `--mcp-config` / `--settings` / `--plugin-dir` / `--fallback-model` / `--add-dir`。中断的工具调用不会重跑。[来源](https://code.claude.com/docs/en/sessions)
- checkpoint / rewind：每次发起 turn 前快照文件，保留最近 100 个；`Esc Esc` 或 `/rewind` 回退；与 git 无关、跨 resume 有效；只覆盖文件改动。[来源](https://code.claude.com/docs/en/checkpointing)

---

## 7. 权限模型与沙箱

### 7.1 六种权限模式（**用户的前提已过时，这里是 2026-10 的真实集合**）
| 模式（config value） | 不询问即可执行 |
|---|---|
| `default`（UI 名 **Manual**） | 只读 |
| `acceptEdits` | 读 + 文件编辑 + 常见文件系统命令（mkdir/touch/mv/cp…） |
| `plan` | 读 + （auto 可用时）分类器批准的命令；**源码编辑被挡** |
| `auto` | 几乎全部，背后有**分类器模型**复核 |
| `dontAsk` | 只放行工作目录内读 + 预批准工具；**其它一律拒绝**（CI 用） |
| `bypassPermissions` | 全部（仅限容器/VM） |
[来源](https://code.claude.com/docs/en/permission-modes)

- **默认模式已改为 `auto`**：v2.1.283 起，交互式终端与 VS Code 新 session 默认 auto（更早版本仅 Pro/Max/Team 默认）。[来源](https://code.claude.com/docs/en/permission-modes)
- **`-p` 与 Agent SDK 例外**：v2.1.302 起，未指定模式的 `-p`/SDK 运行从 **Manual** 起步，需要审批的调用**直接拒绝**，除非你有 allow 规则 / hook / `canUseTool` / `--permission-prompt-tool`。[来源](https://code.claude.com/docs/en/permission-modes)
- auto mode 的分类器：默认走 **Claude Sonnet 5**；可用 `CLAUDE_CODE_AUTO_MODE_MODEL` 指定；对模型有硬门槛（Anthropic API 上 Opus 4.6+/Sonnet 4.6+/Haiku 5.5/Fable；Bedrock/Agent Platform/Foundry 上仅 Sonnet 5+/Opus 4.7+/Haiku 5.5/Fable），不满足就退回 Manual。[来源](https://code.claude.com/docs/en/permission-modes)
- **任何模式都不会自动放行**的东西：显式 ask 规则、组织强设 `ask` 的 connector 工具、`AskUserQuestion`、标了 `requiresUserInteraction` 的 MCP 工具、被识别为 critical path 的 `rm`/`rmdir`、跨 session 消息保护；`blockReadsOutsideWorkingDirectories` 开启时工作目录外读取在 auto/bypass 下也要问。[来源](https://code.claude.com/docs/en/permission-modes)
- **deny 规则在所有模式生效，包括 `bypassPermissions`**；allow 规则在 bypass 下无效。[来源](https://code.claude.com/docs/en/permission-modes)
- 模式来源优先级：`--permission-mode`/`--dangerously-skip-permissions` > `permissions.defaultMode`（settings）> 内置默认；**项目级 `.claude/settings.json` 里的 `auto`/`bypassPermissions` 不生效**。[来源](https://code.claude.com/docs/en/permission-modes)

### 7.2 settings.json 层级
| 作用域 | 文件 |
|---|---|
| User | `~/.claude/settings.json` |
| Shared project（入 git） | `.claude/settings.json` |
| Project local（不入 git） | `.claude/settings.local.json` |
| Managed（组织） | `managed-settings.json` / MDM / claude.ai console 的 server-managed settings |
- 另有 `~/.claude.json`（账号、MCP server 配置、项目 trust 决策等，Claude Code 自写）。[来源](https://code.claude.com/docs/en/settings)
- 权限规则三态：`allow` / `ask` / `deny`，写在 `permissions` 下，也可用 `--allowedTools`/`--disallowedTools`、SDK `allowedTools`、skill 的 `allowed-tools` frontmatter、hook 的 `if` 条件。[来源](https://code.claude.com/docs/en/tools-reference)
- 工作区 trust：项目级 allow 规则需要先 trust 该目录才生效；未 track 的 `.claude/settings.local.json` 例外。[来源](https://code.claude.com/docs/en/settings)

### 7.3 沙箱（OS 级，不是容器）
- macOS 用系统 **Seatbelt**；Linux/WSL2 用 **bubblewrap + socat**（可选 seccomp 过滤）；**原生 Windows 不沙箱**，要在 WSL2 里跑。[来源](https://code.claude.com/docs/en/sandboxing)
- 两种沙箱模式：**auto-allow**（沙箱内命令免询问）与常规（仍走权限流）；两种模式的**文件系统与网络限制完全相同**，差别只在是否自动批准。[来源](https://code.claude.com/docs/en/sandboxing)
- 有「**unsandboxed retry**」逃生舱（沙箱内失败可申请到沙箱外重试），管理员可在 managed settings 里禁掉它，从而把沙箱变成**强制**（admin-required，仓库文件里的放松设置一律忽略）。[来源](https://code.claude.com/docs/en/sandboxing)
- 沙箱与 auto mode 独立可叠加；`sandbox.enabled`、`excludedCommands`、允许域名清单等可在 settings 配置；`/sandbox` 面板落盘到 `.claude/settings.local.json`。[来源](https://code.claude.com/docs/en/sandboxing)
- 外层隔离另有 sandbox runtime / container / VM（`sandbox-environments`），`--dangerously-skip-permissions` 官方要求必须跑在容器/VM 里且非 root。[来源](https://code.claude.com/docs/en/permission-modes)

### 7.4 企业策略面
`managed-settings.json` / MDM / server-managed settings 可下发：`permissions.defaultMode`、`disableAutoMode`、`disableBypassPermissionsMode`、`allowedProviders`（例如锁死只能用某个 `ANTHROPIC_BASE_URL` 网关）、模型限制/组织默认模型、每角色 effort 上限、插件与 mod 的 allowlist、`allowedHttpHookUrls`、CLAUDE.md 的 managed-only 模式、HIPAA 配置。[来源](https://code.claude.com/docs/en/permission-modes) / [来源](https://code.claude.com/docs/en/permissions) / [来源](https://code.claude.com/docs/en/llm-gateway) / [来源](https://code.claude.com/docs/en/plugins/org)

---

## 8. 模型与账号：**只能 Anthropic 模型**，但多云可部署

- 官方一句话钉死第三方模型：「**Anthropic 不支持通过任何 gateway 把 Claude Code 路由到非 Claude 模型**」。[来源](https://code.claude.com/docs/en/llm-gateway)
- 可用的推理后端（都是 Anthropic 模型的不同售卖渠道）：Anthropic API、**Claude Platform on AWS**、**Amazon Bedrock**、**Google Cloud's Agent Platform（原 Vertex AI）**、**Microsoft Foundry**、自建 LLM gateway / Claude apps gateway。[来源](https://code.claude.com/docs/en/third-party-integrations) / [来源](https://code.claude.com/docs/en/llm-gateway)
- 别名 → 实际模型按 provider 不同（文档表，Anthropic API）：`opus`→Opus 5.5、`sonnet`→Sonnet 5.5、`haiku`→Haiku 5.5；Bedrock/Agent Platform 的 `sonnet` 仍是 Sonnet 4.5、Foundry 的 `opus` 是 Opus 4.6。另有 `fable`/`best`（Fable 5.1 / Fable 5）、`opusplan`（plan 用 opus、执行用 sonnet）、`sonnet[1m]`/`opus[1m]` 百万上下文。[来源](https://code.claude.com/docs/en/model-config)
- 版本门槛被写进文档：Sonnet 5.5 需 CLI ≥2.1.284、Opus 5.5 ≥2.1.280、Haiku 5.5 ≥2.1.293、Fable 5.1 ≥2.1.257。[来源](https://code.claude.com/docs/en/model-config)
- 账号/认证：claude.ai 订阅登录（Pro/Max/Team/Enterprise）、Anthropic Console API key、`apiKeyHelper`、各云 provider 自有凭据；`--bare` 模式**不读 OAuth/keychain**，必须给 `ANTHROPIC_API_KEY`。[来源](https://code.claude.com/docs/en/headless)
- 计费口径：订阅是按 5 小时窗口 + 周限额共享同一配额池，超额可开 usage credits（按 API 价）；Fable 在某些 plan 上直接走 usage credits 并弹同意框。[来源](https://code.claude.com/docs/en/costs) / [来源](https://code.claude.com/docs/en/model-config)

---

## 9. 版本、定价、成熟度、已知痛点

### 9.1 版本
- CLI/文档最新：**2.1.312（2026-10-27）**；npm `@anthropic-ai/claude-code` 同为 2.1.312；TS SDK 0.3.312；PyPI SDK 0.2.174。[来源](https://code.claude.com/docs/en/changelog) / [来源](https://registry.npmjs.org/@anthropic-ai/claude-code/latest)
- 发布节奏极高：CHANGELOG 单文件已 1.1 MB，版本号在 2.1.x 上密集递增（近两周内出现 2.1.283/285/288/293/302/304/305/312 等）。[来源](https://code.claude.com/docs/en/changelog)

### 9.2 定价
- 订阅档位（**第三方汇总，彼此有出入**，权威以官方 `claude.com/pricing` 为准）：Pro **$20/月**（年付 ≈$17）、Max 5x **$100/月**、Max 20x **$200/月**、Team 席位（标准 vs 高级，价格与是否含 Claude Code 各家说法不一致）、Enterprise；Free 不含 Claude Code；另有按 token 的 API 计费。[来源](https://www.cloudzero.com/blog/claude-code-pricing/) / [来源](https://www.faros.ai/blog/claude-code-pricing) / [来源](https://claude.com/pricing)
- **官方给出的企业实测成本口径**：平均约 **$13/开发者/活跃日**、**$150–250/开发者/月**，90% 用户低于 $30/活跃日。[来源](https://code.claude.com/docs/en/costs)
- 后台进程本身也消耗少量 token（典型 <$0.04/session）。[来源](https://code.claude.com/docs/en/costs)

### 9.3 成熟度信号
- 生态规模：仓库 153.7k star、2025-02 建仓、至今仍在日更；三套官方 marketplace；SDK 双语言；IDE/桌面/Web/手机/Slack/CI 全入口。[来源](https://api.github.com/repos/anthropics/claude-code) / [来源](https://code.claude.com/docs/en/plugins/anthropic-marketplaces)
- 工程化程度高：GPG 签名 manifest、多发行版仓库、双 release channel、OpenTelemetry 指标、`/doctor` 体检、plugin eval、成本归因（`/usage` 按 skill/subagent/plugin/MCP 归因）。[来源](https://code.claude.com/docs/en/setup) / [来源](https://code.claude.com/docs/en/costs)

### 9.4 已知痛点（GitHub issue 实测排序，2026-10-28 抓取，按 👍 排序）
| 反应数/评论 | issue |
|---|---|
| 2116👍 / 282 评论 | #45596 **Bring Back Buddy** —— 社区联署请愿（标题原文 "A Consolidated Plea from the Community"） |
| 1222👍 / 226 | #18435 桌面 app 多账号切换 |
| 1070👍 / 188 | #36151 手机端多账号 |
| 891👍 / 742 | #34229 **Phone verification**（验证流程） |
| 822👍 / 354 | #826 Console 输出时历史滚到顶部（TUI 滚动 bug） |
| 726👍 / **1499 评论** | #16157 **Max 订阅「瞬间打满用量」** |
| 577👍 / 37 | #31005 请求支持 `AGENTS.md` 与 `.agents/skills/` |
| 546👍 / 879 | #38335 **Max 计划 session 限额异常快速耗尽（2026-03-23 起 CLI）** |
| 462👍 / 71 | #1455 不遵守 XDG Base Directory |
| 275👍 / 81 | #22543 Cowork 功能生成 10GB VM bundle 拖慢性能 |
| 252👍 / 41 | #65961 模型默认写冗长注释、不听指令 |
[来源](https://github.com/anthropics/claude-code/issues?q=is%3Aissue+is%3Aopen+sort%3Areactions-desc)

- 结构性痛点（文档自己承认）：**auto-compact thrashing**（反复压缩仍爆窗则报错停摆）、**压缩后早期指令丢失**（官方建议把规则写进 CLAUDE.md 而不是靠对话）、**`-p` 默认 Manual 导致 CI 里调用被静默拒绝**（v2.1.302 行为变更）、**`--bare` 之前行为不一致直到 2.1.286**。[来源](https://code.claude.com/docs/en/troubleshooting) / [来源](https://code.claude.com/docs/en/permission-modes) / [来源](https://code.claude.com/docs/en/headless)
- 供应侧波动：auto mode 可能被 Anthropic **服务端临时关闭**；Fable 的安全分类器命中会触发自动回退模型。[来源](https://code.claude.com/docs/en/permission-modes) / [来源](https://code.claude.com/docs/en/model-config)

---

## 10. 与本仓库（Seelex）对比时值得抄 / 不值得抄的判断

**值得参考的机制（都有官方文档背书）**
1. **权限模式 + 分类器双轨**：`auto`（分类器复核）与 `bypassPermissions`（仅容器）分开，且 `-p`/SDK 明确退回 `default` 并要求显式 allow —— 这是「无人值守不静默放权」的正确姿势。[来源](https://code.claude.com/docs/en/permission-modes)
2. **压缩后的显式重注入清单**：项目根 CLAUDE.md / rules / auto memory / plan / skill 描述 / 最近 5 个文件 —— 把「压缩后什么必须活」写成一张表，是工程化的关键。[来源](https://code.claude.com/docs/en/context-window)
3. **MCP 工具定义延迟加载（ToolSearch）**：工具面变大后的必然选择，直接决定常驻上下文成本。[来源](https://code.claude.com/docs/en/mcp)
4. **subagent 后台化 + supervisor 进程**：session 与终端解耦，`claude daemon` 托管，跨重启存活。[来源](https://code.claude.com/docs/en/agent-view)
5. **hook 事件粒度**：33 个事件 + 5 种 handler + deny 压过 hook allow 的硬优先级，是「可编排但不失控」的样本。[来源](https://code.claude.com/docs/en/hooks)
6. **worktree 作为并行隔离原语**（`--worktree` / `isolation: worktree`）。[来源](https://code.claude.com/docs/en/worktrees)

**不建议照抄**
1. `-p` 默认 Manual + 静默拒绝（会把 CI 里的 permission 问题变成「什么都不发生」）。[来源](https://code.claude.com/docs/en/permission-modes)
2. session transcript 格式「内部、跨版本会变」——详见文档自述，意味着任何依赖它的外部工具都会碎。[来源](https://code.claude.com/docs/en/sessions)
3. 单 provider 锁定（多入口很强，但模型层零可替换性，且官方明说不支持非 Claude 模型）。[来源](https://code.claude.com/docs/en/llm-gateway)
4. 05 小时窗口 + 周限额的额度模型，是社区最大负面来源（#16157 / #38335 合计 2378 条评论）。[来源](https://github.com/anthropics/claude-code/issues/16157)

---

## 附：本文的「未确认」清单（不要当事实用）

| 断言 | 状态 | 说明 |
|---|---|---|
| microcompact / session-memory compact 三层压缩 | **未确认** | 官方文档与 CHANGELOG 全文无此词；仅第三方逆向仓库描述，路径与常量无法在官方渠道验证 |
| 二进制内部语言/运行时（Rust? Bun?） | **推测** | 官方只说是「native binary」；仓库 `language: TypeScript` 只是 GitHub 猜测 |
| 桌面 app / Web 前端技术栈 | **推测** | 无官方文字 |
| `Task*` 工具为何只对部分模型默认开放 | **推测** | 文档只给「Task tool availability」表，未解释筛选逻辑 |
| prompt cache 命中率的具体数值表现 | **未验证** | 官方只给机制说明（`/context`、cache 命中率查看），无 benchmark |
| 各 plan 的具体 token 配额 | **未验证** | 官方明确不公布固定消息数，实际按对话长度/模型浮动 |
