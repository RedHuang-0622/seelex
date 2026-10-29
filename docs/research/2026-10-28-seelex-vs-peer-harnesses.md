# Seelex 与 peer agent harness 对比（2026-10-28 一手调研）

> **本轮调研口径**：完全重新取证，不引用本仓库既有的任何调研文档。所有外部事实来自 2026-10-28 当天抓取的官方文档 / GitHub / release notes / npm registry；所有 Seelex 事实来自本仓库源码与 `docs/arch/`。
>
> **证据标注**：
> - 【文档】官方文档或官方 release note 原文
> - 【代码】本仓库或开源仓库源码/文件结构可查
> - 【第三方】非官方逆向、评测、社区
> - 【推测】无一手文字，仅有结构性线索
>
> **对照基准**：arXiv:2609.00006《Harness Engineering: Anatomy, Architecture, and Evolution of Coding Agents》给出的七子系统模型（agent loop / LLM integration / tools & actions / memory & context / safety & permissions / orchestration / extensibility + 接口层 + 会话底座）。该文对 11 个生产 harness 做源码解剖，是本领域目前唯一有实证基础的公共坐标系。

---

## 0. 三句话结论

1. **2026 年上半年整个领域完成了「工具 → 平台」的转向**：harness 变成可 import 的 SDK，扩展面长出 marketplace 与信任分级，行为规矩从 prompt 散文迁进配置文件，agent 本身变成 OpenAI 兼容端点。Seelex 站在这个转向的**产品语义端**（看板 + 目标治理 + 并行 worker），但**没有建平台端的三件基础设施**：OS 沙箱、编辑器/协议互操作、生态分发。

2. **在七个子系统里，Seelex 有两个位置比所有 peer 更细**：权限判定（主体 × 路由组 × 位，Linux rwx 语义）和作业契约（派发只回受理回执、取回是消费式、句柄自带来源）。这两处不是「大厂还没做」，而是**大厂选了更简单的模型**——它们用沙箱兜底，所以不需要那么细的位。

3. **Seelex 最大的单点差距是执行隔离**：`bash` 目前只有项目目录 cwd 约束，**没有 OS 级沙箱**【代码/文档承认】。Codex 有三平台 OS 沙箱 + Starlark execpolicy + Guardian 复核 agent；Claude Code 有 Seatbelt/bubblewrap + 凭据 deny/mask + MDM 强制。在一个「模型会自己跑命令」的系统里，这一条比任何编排花活都重要。

---

## 1. 六家快照（2026-10-28）

| | **Seelex** | **Claude Code** | **Codex CLI** | **Pi** | **Qoder** | **ZCode** |
|---|---|---|---|---|---|---|
| 厂商/归属 | 个人开源（RedHuang-0622） | Anthropic | OpenAI | Earendil（原 badlogic/Mario Zechner） | 阿里（新加坡团队） | 智谱 / Z.ai |
| 版本（当日） | v0.1.2（tag），1567 commits，Developer Alpha【代码】 | CLI **2.1.312**（2026-10-27）【文档】 | **0.162.0**（2026-10-08）【文档】 | npm **1.4.0**（2026-10-21）【文档】 | 产品族多线（Quest 窗口 2026-10-15 下线）【文档】 | **3.14.x**（3.14.3/3.14.4 快照不一）【文档/第三方】 |
| 实现语言 | **Go**（1.26+）【代码】 | 闭源 native binary（仓库 language 标 TS，非源码）【文档/推测】 | **Rust**，100+ crate，~1.12M LOC【文档】 | TypeScript / Node ≥22【文档】 | IDE=Code-OSS fork；新桌面=自研 Electron 43；CLI 自有运行时【第三方】 | TypeScript monorepo（pnpm），Electron + Web + CLI 共用运行时【文档】 |
| 许可 | MIT【代码】 | 专有，`All rights reserved`【文档】 | **Apache-2.0**（2026-08 把整套 harness 开源）【文档】 | MIT【文档】 | 闭源 | **Apache-2.0**（2026-09-20/21 开源整仓）【文档】 |
| 入口 | TUI（Bubble Tea）+ Wails GUI + headless RPC + backend 诊断台【代码】 | CLI / VS Code / JetBrains / 桌面 app / Web / Slack / Chrome / Agent SDK(Py+TS) / GitHub Action【文档】 | CLI/TUI / IDE 插件 / 桌面 app / Web / App Server(JSON-RPC) / Python SDK / GitHub Action【文档】 | TUI / `-p` / `--mode json` / `--mode rpc` / SDK【文档】 | 桌面 Qoder / Qoder IDE / JetBrains 插件 / CLI / Cloud Agents / Mobile·Web / QoderWork / QoderWake / Enterprise（**九条线**）【文档】 | 桌面（Electron）/ Web（`zcode --web`）/ TUI·CLI，同一个 `zcode` 命令分流【文档】 |
| 模型面 | OpenAI 兼容 endpoint；按 agent/subagent/goalplan 角色分账号池【代码】 | **仅 Anthropic 模型**，官方明说不支持任何 gateway 路由到非 Claude 模型；多云售卖（Bedrock / Google Agent Platform / Foundry）【文档】 | 默认 GPT-6.1 Sol；`[model_providers]` BYO；`--oss` 接 Ollama / LM Studio【文档】 | 15–20+ provider，OAuth 一等公民【文档】 | Qwen 系 + 多模型（官方不点名各档位模型）；CN 版走 GLM/DeepSeek/Kimi/Qwen【文档/第三方】 | 深度绑 GLM-5.3（1M context）+ GLM-5.3-Flash 看截图；可接其他后端【文档/第三方】 |

---

## 2. 逐子系统对比

### 2.1 Agent loop

| | 现状 |
|---|---|
| **Seelex** | ReAct 主循环 + Effort 四档（lite/medium/high/max）约束循环数、工具调用数、计划规模；工具结果超预算归档为 `result_ref`【代码】 |
| **Claude Code** | 三阶段 `gather context → take action → verify results`，可随时被打断；官方给这层壳起名 agentic harness【文档】 |
| **Codex** | Loop 内建 prompt-cache 友好；上下文压缩走 encrypted content【文档】 |
| **Pi** | 特意做到最小：`packages/agent/src/agent-loop.ts` 5 个文件，其余全交给 `AgentTool` / `StreamFn` / 事件 sink【文档】 |
| **Qoder** | Chat（短任务）/ Quest（规格驱动、长任务）/ Ask·Plan·Auto 三模式【文档】 |
| **ZCode** | Goal 模式：`/goal` 定义目标 → 拆计划 → 执行 → 自评 → 迭代，多个 Goal 可并发【文档】 |

**判断**：loop 本体已经商品化，没有差异化空间。真正的分水岭在 loop 之外——停条件、失败恢复、预算硬顶。Claude Code 的 `maxTurns` / `maxBudgetUsd` 是硬顶；见 §2.6 关于 Seelex 缺预算硬顶的观察。

### 2.2 LLM 集成

- **Codex** 是这一维的天花板：server 下发 model catalog、五套自有 transport、29 个 provider profile【第三方/论文】。代价是把 OpenAI 生态焊死。
- **Claude Code** 是反面极端：单 provider、零可替换性，换来调校深度（auto mode 的分类器只对自家模型档位开放）。
- **Pi** 是开放端：17–20+ provider + OAuth + 并发刷新锁 + chmod-600 凭据。
- **Seelex** 走 OpenAI 兼容一条路 + **账号池按角色路由**（agent / subagent / goalplan 三组，分支确定性选路、流式租借）【代码】。角色化账号池这个设计在六家里**只有 Seelex 有**——它解决的问题是「子代理烧的不是主对话那个号」，这在多 agent 下是真实的成本隔离需求。

### 2.3 Tools & actions

| | 工具面特征 |
|---|---|
| **Seelex** | 内置工具族按域拆包（`backend/tool/{computer,fork,io,jobs,node,reference,registry,review,search,ssh,task,websearch,worktree}`）【代码】；关键差异见 §3.1 双执行面 |
| **Claude Code** | 43 个 typed 工具；MCP 工具定义**延迟加载**靠 `ToolSearch` 按需拉【文档】；`Glob`/`Grep` 在 macOS/Linux/WSL **默认不存在**（清单明写 "Absent by default"）【文档】 |
| **Codex** | 工具体系 + **tool calls as V8-executed code**（code mode）；`execpolicy` 用 Starlark DSL 写命令 allow/prompt/forbid【文档】 |
| **Pi** | 默认只有 4 个：read / write / edit / bash；`BashOperations` 是可插拔点（SSH、容器都在这一层接）【文档】 |
| **Qoder** | IDE 侧补全/Ask/Agent + MCP + `.qoder/rules`；CLI 支持 ACP【文档】 |
| **ZCode** | 插件贡献 skills / 自定义命令 / MCP server；MCP stdio·http·sse；7 个 hook 事件（SessionStart / UserPromptSubmit / PreToolUse / PermissionRequest / PostToolUse / PostToolUseFailure / Stop）【文档】 |

### 2.4 Memory & context（Seelex 的相对强项）

- **Seelex**：把上下文装配当成**缓存前缀工程**来做——system 段（identity → plugin → effort → instructions → skill 目录）**一旦定稿永不追加**；中段 project → memory → compact → context（append-only 累积）；尾部 plan/task/当前输入每轮重建【代码 `docs/arch/context-prefix-chain.md`】。压缩是四区模型 + 压缩 DAG + 分片压缩链；超大工具结果归档成 `result_ref`，用 `read_tool_result` / `read_compressed_turn` / `search_history` 按需读回；装配逼近 98% 预算时**探测即主动压缩**，且压缩失败只丢无损性、不改写上下文、不中断会话【代码】。
- **Claude Code** 的强项是**把「压缩后什么必须活」写成一张表**：项目根 CLAUDE.md / 无 `paths:` 的 rules / auto memory / plan / skill 描述 / 最近 5 个文件，压缩后从磁盘重注入；`paths:` 作用域的 rules 会被摘要掉【文档】。另有 auto-compact **thrashing 保护**：反复压缩仍爆窗就报错停摆而不是死循环【文档】。
- **Pi** 把**压缩策略本身做成扩展点**：可用 topic-based、code-aware，甚至换另一个模型来摘要；默认保留最近 ~20k token + 层层 context handoff【文档/第三方】。
- **Qoder** 的记忆是产品卖点（越用越顺手），但机制不公开。
- **ZCode** 1M context 撑着，但 2026-09 的 **Repo Wiki 上传仓库数据风波**（见 §5）说明它的代码索引链路曾经越界。

**判断**：Seelex 与 Pi 是同一哲学的两端——都把「压缩」当可替换组件；但 Seelex 多押了一条 **prefix-cache 优先**（append-only context 区），这在长会话成本上是真金白银。Claude Code 的「重注入清单」是 Seelex 最该补的一张表：现在 Seelex 有机制，但**没有把「压缩后什么保证活着」写成对外契约**。

### 2.5 Safety & permissions（Seelex 差异最大、差距也最大的一维）

**这一维必须拆成两层看，否则会得出错误结论：判定层 vs 隔离层。**

**判定层（谁能不能调什么）——Seelex 领先**

Seelex 的模型是「**主体 × 路由组 × 位**」，直接借 Linux 的 user / group / mode / sudo 语义【代码 `docs/arch/agent-permission-subjects.md`】：

- **主体**（谁在调）：`root`（uid 0 语义）/ `sub`（普通用户）/ `emp_ro` / `emp_rw`（受限用户）——挂在调度 ctx 上，不落共享状态，并行子代理同时调工具不互相污染
- **路由组**（哪一族工具，按工具名 glob，LMRW 最后匹配胜出）：`ro`(4) / `rw`(6) / `rw_session`(6) / `rw_desktop`(6) / `ctl`(1) / `adm`(7) / `bash`(1)
- **位**：缺位即拒绝
- **档位梯子**：`manual` / `edit` / `auto` / `full`，按会话解析；**档位只剪掉 `ask`，从不覆盖危险 `deny`**；`full` 短路只作用于 root，员工越权仍走审批
- **审批归属折算**（这条很值得说）：角色会话的越权审批会被折到**宿主主会话**上呈现——否则「员工提权」对用户完全不可见，只能等超时被拒【代码】

对比：
- **Claude Code** 是 `allow / ask / deny` 三态 + 六种权限模式（`default`/Manual、`acceptEdits`、`plan`、`auto`、`dontAsk`、`bypassPermissions`）。**v2.1.283 起交互式默认改成 `auto`**（分类器复核，默认 Claude Sonnet 5）；v2.1.302 起 `-p`/SDK 退回 Manual 且需审批的直接拒绝。deny 在所有模式生效（含 bypass），allow 在 bypass 下无效；**项目级 settings 里的 `auto`/`bypassPermissions` 不生效**【文档】。
- **Codex** 是**双轴**：沙箱模式（`read-only` / `workspace-write` / `danger-full-access`）× 审批模式（`untrusted` / `on-request` / `never`），加 built-in permission profile（`:read-only` / `:workspace` / `:danger-no-sandbox`）与 granular 类别；`--full-auto` 已于 0.128 废弃；另有 **Guardian** 复核 agent（v0.160 起可向 Guardian 开对话历史查询，能识别「跨多轮逐步提权」这类模式）【文档】。
- **Pi** 的立场是**没有权限弹窗**：用容器，或自己写确认扩展【文档】。这是有意的极简主义，不是疏漏。
- **ZCode / Qoder**：GUI 侧审批，机制未公开细化。

**隔离层（真跑起来能伤到什么）——Seelex 落后一代**

| | OS 级沙箱 |
|---|---|
| **Seelex** | ❌ **无**。`bash` 只有「workdir 必须在项目内」的路径约束；`docs/2026-07-28-project-session-scope/sandbox-research.md` 明确写「将 shell 明确标记为『非 OS 级隔离』」，`CommandSandbox` 端口只是**规划**【代码】 |
| **Claude Code** | ✅ macOS Seatbelt；Linux/WSL2 bubblewrap + socat（可选 seccomp）；**原生 Windows 不沙箱，要在 WSL2 跑**；两层（文件系统 / 网络域名）独立可关；`sandbox.credentials` 支持凭据文件 deny + 环境变量 deny/**mask**（给哨兵值）；managed settings 可 `failIfUnavailable: true` + `allowUnsandboxedCommands: false` 做成**强制**；沙箱运行时开源为 `@anthropic-ai/sandbox-runtime`【文档】 |
| **Codex** | ✅ 三平台 OS 沙箱（macOS Seatbelt / Linux Landlock+seccomp / Windows sandbox），默认断网 + 域名白名单；`deny read` 规则在 safe-command 与审批绕过路径上仍生效（0.16x 专门修过）；Starlark `execpolicy` 做命令级 allow/prompt/forbid【文档】 |
| **Pi** | ❌ 无内建沙箱、无权限弹窗：工具与扩展以启动用户的 OS 权限运行，官方建议用容器/受限用户【文档】 |
| **Qoder / ZCode** | GUI 桌面产品，沙箱细节未公开；ZCode Web 模式默认只听 `127.0.0.1`，监听非本机地址时自动生成访问令牌【文档】 |

**结论**：Seelex 在**判定层**比六家里任何一家都细（没人做主体×组×位，也没人做审批归属折算）；在**隔离层**是最弱的一档（和 Pi 同级）。这不是「设计取舍」，因为在 Seelex 里判定层细密的理由恰恰是**没有沙箱兜底**——它必须靠判定把危险挡在门外。而 Codex/Claude Code 敢把判定做粗，是因为即使判定漏了，沙箱还在。**Seelex 补沙箱之后，很可能可以顺手简化判定层。**

### 2.6 Orchestration（2026 的主战场，也是 Seelex 最值得看的地方）

**2026 年四家全在打同一张牌：并行。** 但打法各不相同：

| | 机制 | 隔离单位 | 失败恢复 |
|---|---|---|---|
| **Seelex** | `fork_subagents`（后台作业派发，独立 Session/SceneScope/worktree，结构化 merge-back）+ **`teamwork`**：leader + 异步 worker，**里程碑是屏障（`milestones[].depends_on`）、里程碑内是 Work Item DAG（`work_items[].depends_on`）**；一个 Work Item = 一个 Session + 一个 git worktree | **worktree + session 双重绑定**，写 `teamwork/worktrees.jsonl` 追加日志；worktree 创建失败时 `workspace_mode=main` **必须用户逐次批准显式重试，不静默降级** | 结算链固定：尾插自动「先合并 worktree → 有界回执进消息队列 → 状态 → leader 验收 → `team_accept`」；`team_fail` **保留现场与记忆**可重派；`team_recover` 只把「句柄已作废、可重派」显式化，重派**复用原会话号**；`team_close` 是唯一收口点（幂等）【代码】 |
| **Claude Code** | subagents（`.claude/agents` Markdown+frontmatter，`isolation: worktree`）+ **agent teams**（lead 监督多个 peer session，共享任务列表，`SendMessage` / `TeammateIdle` hook）+ **dynamic workflows**（Claude 自己写 **JavaScript 脚本**，后台编排**几十到几百个** subagent，中间结果存脚本变量而非上下文）+ background sessions 由独立 **supervisor 进程**托管（`claude daemon`，跨重启存活） | 默认 20 个并发 subagent（可调），有嵌套深度限制；`--worktree` / `EnterWorktree` | subagent 可 resume（按 agent ID 用 `SendMessage` 续跑）、可 fork（继承父对话全文）；**TaskOutput 已于 v2.1.277 移除**，改读后台输出文件【文档】 |
| **Codex** | `multi_agent` 自 runtime 0.130.0 起默认稳定；`spawn_agent` / `send_input` / `followup_task`；自定义 agent 是 `.codex/agents/*.toml`；上限 `agents.max_concurrent_threads_per_session`；**agent command center**（`/agents` 视图）统一管所有线程；worktree 会话 0.156.0 起默认开 | ⚠️ **`spawn_agent` 没有 cwd/worktree 参数——子代理共享父的 workspace 与 sandbox**；真正独立 checkout 要靠 worktree 会话或 `codex exec --cd`【文档】 | Guardian 复核带 handoff-aware 上下文切片（按 spawn/send/followup 调用还原每个 worker 该看哪段）；`--sandbox workspace-write` 是无人值守推荐档【文档】 |
| **Pi** | ❌ 明确不做 subagent / plan mode / to-dos / background bash，理由写在 README：spawn pi 实例用 tmux，或自己写扩展 | — | — |
| **Qoder** | Quest Mode 规格驱动（Spec → Action Flow → Task Report 落 `.qoder/quest`）+ **Cloud Agents**（Agent / Environment / Session / Event 四概念，每 Session 一个隔离容器、互相不可达、环境销毁即清数据） | 云端容器隔离 | 未公开 |
| **ZCode** | 多 agent 协作（v3.0 起）+ 通用 sub-agent 系统（v3.2.0 起支持**每 agent 自定义读写权限**）；Goal 可并发 | 未公开（第三方明确提醒：冲突编辑与重复劳动是已知失败模式） | 未公开 |

**三条判断：**

1. **Seelex 的 teamwork 是六家里唯一把「并行」当事务来做的。** 别人做的是「尽力而为的并行」：Claude Code 的 dynamic workflows 能扩到几百个，但中间结果活在脚本变量里；Codex 的子代理连 worktree 都不隔离。Seelex 的选择相反——**上限只到 6 个 teammate**（`max_teammates`，超限**显式拒绝而非静默排队**），换来每个 Work Item 都有 worktree、会话号、追加式绑定账本和可审计的结算链。这是「小规模但可追溯」对「大规模但尽力而为」。

2. **Seelex 的规模天花板是硬伤**。6 个 teammate vs Claude Code 的 20 并发 / 几百 subagent，在「大重构并行」这种场景上差一个数量级。而且它的 worktree+session 双绑定的成本在几十个并发时会直接压垮机器。

3. **我本轮实测到一个此前没被记录的护栏缺口（本会话观察，非架构断言）**：五个调研子代理里两个跑到 ReAct 第 26 / 31 轮仍未收敛，最后是我手动 `kill` 才停的（`accountpool: acquire: context canceled` / `read SSE: context canceled`）。Claude Code 对同类问题有 `maxTurns` + `maxBudgetUsd` 硬顶和 auto-compact thrashing 熔断；Seelex 有 Effort 档位约束循环数，但**没有看到「按美元/按 token 的会话级硬预算」**。对一个会自动派发子代理的系统，这条护栏的缺位比缺沙箱更早咬人。

### 2.7 Extensibility

| | 扩展面 |
|---|---|
| **Seelex** | 声明式 Plugin（include/exclude 工具可见性快照）+ 目录化 Skill（`SKILL.md`）+ MCP Server 冷启动登记/按需加载/**工具可见性过滤** + `tool_notes`（把本机实测的调用纪律折进 MCP 工具描述）+ plugin/skill/mcp 自管理工具；切换插件是**事务式**的【代码】 |
| **Claude Code** | skill = `.claude/skills/<name>/SKILL.md`，**slash command 已合并进 skills**，遵循 `agentskills.io` 开放标准；plugin = skills+agents+hooks+MCP+commands 的打包单元，manifest 带依赖版本区间；三个官方 marketplace；**mods**（2026 新增 = plugin 里的 JavaScript 进程内 hook，能画 pane/按钮/输入框、能改工具调用与渲染）；`claude plugin eval` + 成本计量 + 组织 allowlist；33 个 hook 事件 × 5 种 handler，且 **deny 规则压过 PreToolUse 的 allow**【文档】 |
| **Codex** | 2026-08 起整套 harness Apache-2.0 开源；plugin marketplace + 信任分级 + agent authors；skills 已跟随 SKILL.md（旧 prompts deprecated）；**MCP client / server 双角色**；App Server 把客户端与运行时解耦（IDE 插件可直接指向更新的 app-server 二进制，客户端与服务端发版解耦）【文档】 |
| **Pi** | 四层递进：Skills（Markdown+frontmatter，progressive disclosure）→ Prompt Templates → **Extensions（TypeScript `ExtensionAPI`，注册工具/命令/生命周期 hook/自定义 UI）** → Packages（npm 打包，`pi install npm:<pkg>` / `git:<url>`）；项目 trust 机制控制项目级扩展是否加载【文档】 |
| **Qoder** | Skills / Plugins / Connectors / Hooks + `plugin.json`；JetBrains 插件支持 MCP 与 `.qoder/rules`【文档】 |
| **ZCode** | 插件可贡献 skills、自定义命令、MCP server；内置官方插件 `browser-use` / `document-skills` / `skill-creator` / `zcode-guide` 默认启用；`.agents/skills/` 里内置 architecture-governance、react-best-practices 等约 10 个技能，**还有一个 `dogfood`（官方在用自己产品开发自己）**【文档】 |

**关键趋势（论文的实证 + 我的数据一致）**：**SKILL.md 在采用率上已经压过 MCP（9/11 vs 8/11）**，而且连当初公开反对 MCP 的 Pi 都在 **0.99.0（2026-09-29）把 MCP 收进内建**——官方还写了一篇自我打脸的《"You Said No MCP!"》。所以「MCP vs Skills」这个老争论已经结束：**两个都留，Skills 当主路，MCP 当接外部世界的口**。Seelex 当前的 plugin + skill + MCP 三件套正好在这个收敛点上。

### 2.8 接口层（Seelex 最明显的短板）

- **Claude Code** 覆盖最广：CLI / VS Code / JetBrains / 桌面 app / Web / 手机 / Slack / Chrome / **Agent SDK（Py+TS，定位是「跑 Claude Code 二进制的库」）** / GitHub Action；本地↔云互通（`--cloud` / `--teleport` / Remote Control）；三种执行环境（Local / Cloud / Remote Control）【文档】。
- **Codex** 的杀手设计是 **App Server**：Thread → Turn → Item 三原语 + `item/started → delta* → item/completed` 生命周期 + 审批 = 同一信道上的**反向 JSON-RPC**、turn 暂停等回答。官方明确说评估过用 MCP 承担这个角色并**否决**了——MCP 的请求-响应模型表达不了流式 diff 与持久会话状态。分工是：MCP 接外部工具，App Server 接客户端【文档】。
- **ACP（Agent Client Protocol，Zed 发起，Linux 基金会，v1 stable）** 已在 6 个系统落地，并长出**第三个角色：harness 托管**（OpenHands 可把 Claude Code / Codex / Gemini CLI 当可互换后端跑）。跨厂商标准已经成型。
- **Seelex**：TUI / Wails GUI / headless 回环 RPC / backend 诊断台，**没有 IDE 集成，没有 ACP，没有 SDK，没有 App Server 级协议**。它的 Application 层已经做到了「四个前端共享同一份装配 + Snapshot/Event 协议」【代码】，也就是说**协议骨架是有的，只是没对外**。这是低成本可补的一块——接 ACP 比自研 IDE 插件便宜得多。

### 2.9 会话底座

| | 存储与恢复 |
|---|---|
| **Seelex** | JSON v8（order log / 项目与 Session 隔离 / 消息分片 / 媒体分区 / plan·task·goal 三栈通道）；SQLite / PG / Redis 枚举**保留用于显式退役错误**【代码】；通用恢复七步模板 + 子代理冷恢复同键续跑 + 中断轮残缺工具链截断 |
| **Claude Code** | `~/.claude/projects/<project>/<session-id>.jsonl`，**官方明说格式是内部的、跨版本会变，别写脚本直读**；默认保留 30 天；resume 时还原历史/模型/权限模式/活跃 goal/未过期定时任务，**不还原** `--mcp-config` / `--settings` / `--plugin-dir`；checkpoint/rewind 每 turn 快照文件、保留最近 100 个、与 git 无关【文档】 |
| **Codex** | thread-store（SQLite 性能优化在 0.160 发布里）；thread resume 可带 turns page；permission profile 存进 thread store【文档】 |
| **Pi** | **JSONL 树**：每条 entry 有 `id` + `parentId`，`/tree` 就地切任何历史点继续、全部历史存一个文件；`/fork` / `/clone` / `--fork`；session 按工作目录组织在 `~/.pi/agent/sessions/`【文档】 |
| **ZCode / Qoder** | 未公开细粒度格式；Qoder 有 `.qoder/quest`（Task Report）与 `.qoder/repowiki`（Wiki）落盘产物 |

**Pi 的 session tree 是六家里最优雅的一处设计**：分支不是「复制一份历史」，而是同一文件里的 parentId 链，所以「回退到某个点再试另一条路」不产生文件爆炸。Seelex 的「有对话历史快照 + 增量刷新」做的是同类事，但落在两套结构里（JSON 会话 + workunit 单元记录），比 Pi 的单一事实源更容易不一致。

---

## 3. Seelex 真正与众不同的四处

以下四点不是「大厂还没做」，而是**在六家里只有 Seelex 这么做**，且有代码支撑。这是它值得被认真看的部分。

### 3.1 同一工具名的双执行面（本地 / SSH）

`read_file` / `write_file` / `edit_file` / `bash` / `bash_bg` / `bg_manage` 六个名字在**本机和远端同名可用**，回执结构相同，只是多两格 `execution_surface` 与 `env` 标明这次到底在哪跑【代码 `backend/tool/ssh/README.md` + `backend/tool/registry/transport*.go`】。细节值得记：

- **库内直连**（`x/crypto/ssh`），不 shell out 到 `ssh.exe`
- **四段独立超时**：库自己的 `ClientConfig.Timeout` 只覆盖 TCP 建连，握手与认证段无上限，`ssh.Session` 也拿不到 `ctx`——所以它自己拨号拿取消权，两个计时器分别卡 banner 段与 auth 段
- **命令来源纪律**：只允许 `CommandFile`（`bash -s` + stdin，首选）/ `Stdin` / 内联（**仅单行 ASCII**，含非 ASCII 直接拒）
- **远端文件面 base64 承载路径**，内容走内容通道——命令正文里不出现一个字节的用户数据，避免远端 shell 重新解释路径里的 `$()` 与引号
- **远端作业 = 一组文件而非一条通道**（`cmd.sh` / `run.sh` / `pid` / 退出码落盘），所以**本端预算用尽之后仍能指认远端进程**
- 「仍在跑」这种状态**单独一种返回形态**，不允许被说成成功；被截断的输出要如实报

对比：Claude Code 没有远端执行面（要远端只能自己在 `Bash` 里跑 ssh，也就没有句柄、没有真值上报）；Codex 有 `--remote` / exec-server，但那是 app-server 层的事；ZCode 有 SSH/WSL 远程开发（资源经 SFTP 上传、不依赖 CDN），最接近，但它是**产品形态**而非**工具契约**。**「同一套工具名 + 同一套回执 + 显式的执行面上报」这件事，我没在另外五家里找到对等物。**

### 3.2 权限判定：主体 × 路由组 × 位

见 §2.5。核心在两点别家没有的东西：

1. **主体是一等公民**。`root` / `sub` / `emp_ro` / `emp_rw` 写在调度 ctx 上而非共享状态里，所以并行子代理同时调工具不会互相污染权限。别家的权限主体只有「这个 session」。
2. **审批归属折算**：角色会话的越权请求折到宿主主会话呈现。这条解决的是一个**真实的可用性黑洞**——多 agent 系统里，「员工代理想越权」如果只归到那个隐形的角色会话上，用户在 UI 上什么都看不到，只能等审批超时被静默拒绝。

### 3.3 作业契约：派发只回执，取回是消费式

`bash_bg` / `read_batch` / `fork_subagents` 共用一套作业面：

- 派发只返回**受理回执**（`handle` + `log_path`），不含输出
- `op=fetch` 是**消费式**的（取过的字节不会再给第二次）；`op=observe` 只读看进展、**永不消费**
- `op=kill` 终止整棵进程树，**已产出内容不丢**
- **句柄自带来源**（本机 `bt-` / 子代理 `bs-` / 团队表 `bteam-`），跨表调用**显式报错并点名对偶工具**
- 串行 `bash` 拒绝超过 5 分钟的 `timeout` 并指路 `bash_bg`
- 作业落终态且所属会话空闲时，**自动起一个回合让模型取回结果**

Claude Code 有 `run_in_background` + `/tasks` + `Monitor`（逐行把输出喂回模型），Codex 有 background server，但**「取回是消费式 / observe 不消费 / 句柄带来源」这套区分**是 Seelex 独有的。它解决的是「长任务产出被反复读消费掉」这类真实损耗。

### 3.4 沙箱 HTML 可视答案

会话里 `seelex-html` 围栏 → `sandbox="allow-scripts"` 的 iframe、**无 `allow-same-origin`**、内联 CSP 断网、只允许 `data:` 图片、高度钳制 120–640px；块内交互默认留在沙箱内，跨帧只走白名单动作（`ask-agent` / `fill-composer` / `copy-text` / `open-source`），且驱动会话要求围栏 `interactive=1` **且块内刚发生过真实手势**【代码】。

六家里没有对等物。Claude Code 有 `Artifact` 工具但未见同等沙箱约束的公开描述；Codex 是终端里渲染 Mermaid/公式。这个能力的价值在于：**它让「回答」可以是一张可交互的图，而不只是一段字**，同时不把执行权交出去。

---

## 4. Seelex 的四个硬差距（按严重程度排序）

| # | 差距 | 证据 | 严重性 |
|---|---|---|---|
| 1 | **无 OS 级沙箱**：`bash` 只有 workdir 在项目内的路径约束，`CommandSandbox` 端口仍是规划 | `docs/2026-07-28-project-session-scope/sandbox-research.md` 自述「将 shell 明确标记为『非 OS 级隔离』」【代码】 | **最高**。判定层再细，一次绕过的判定就是全盘；且没有沙箱就没法做「沙箱内免确认」这一档体验，用户要么被弹窗烦死要么放权 |
| 2 | **无编辑器 / 协议互操作**：没有 IDE 集成、没有 ACP、没有对外 SDK / App Server | 入口只有 TUI / GUI / headless RPC / 诊断台【代码】 | **高**。ACP 已 v1 stable 且有 6 个系统落地；不接 ACP 等于自愿放弃整块编辑器生态。而 Seelex 的 Snapshot/Event 协议骨架已经在了，接 ACP 是低成本高回报 |
| 3 | **无会话级硬预算**：有 Effort 档位约束循环数，未见按 token/美元的硬顶 | 本会话实测：两个子代理跑到 ReAct 第 26 / 31 轮仍未收敛，需人工 kill | **高**。会自动派子代理的系统最容易烧穿预算；Claude Code 的 `maxTurns` / `maxBudgetUsd` 与 Codex 的 usage 视图都是标配 |
| 4 | **无生态分发**：plugins 目录 + skills 目录都是本地的，没有 registry / marketplace / 信任分级 / 迁移导入器 | `plugins/` 目录 + `skill_create` / `plugin_create` 都是写本地磁盘 | **中**。2026 的竞争单位已经不是 agent loop 而是生态面。Codex 提供 Claude Code 配置导入器，Claude Code 有 3 个官方 marketplaces，Pi 有 npm 分发，ZCode 内置 10 个技能 |

另有一条不算差距但要说清的：**模型面只有 OpenAI 兼容**。它比 Claude Code 的「只准 Anthropic」开放，但比 Codex 的 BYO + `--oss` 与 Pi 的 15–20 provider 窄。角色化账号池是加分项，不解决 provider 广度。

---

## 5. 该抄什么 / 不该抄什么

### 值得抄（都有官方文档背书，可直接落到 Seelex）

1. **压缩后的「必须活着」清单**（Claude Code）：把「压缩后什么从磁盘重注入」写成一张对外契约表——项目根规则 / auto memory / plan / skill 描述 / 最近 N 个文件。Seelex 有机制没契约，补这张表成本极低。
2. **crash 熔断而非无限重试**（Claude Code 的 auto-compact thrashing 保护）：反复压缩仍爆窗就报错停摆，而不是继续循环。直接对上 §4 的第 3 条。
3. **MCP 工具定义延迟加载 + `ToolSearch`**（Claude Code）：工具面变大后的必然选择，直接决定常驻上下文成本。Seelex 已有工具可见性过滤，但工具 schema 应否常驻是另一回事。
4. **App Server 式的协议分层**（Codex，Thread/Turn/Item + 反向 JSON-RPC 审批）：Seelex 的 Snapshot/Event 已经同构，把它固化成公开协议，等于免费拿到 IDE 与第三方客户端。
5. **`failIfUnavailable` 式沙箱强制**（Claude Code）：沙箱缺失时**拒绝启动**而不是静默降级为无沙箱——这正好回应 Seelex 沙箱文档里自己写的「生产模式必须 fail-fast，而不是悄悄降级成普通 `exec.Command`」。
6. **ACP 接入**（Zed / Linux 基金会）：比自研 IDE 插件便宜一个数量级，且立刻进 6 个系统的生态。

### 不该抄

1. **`-p` 默认 Manual + 静默拒绝**（Claude Code v2.1.302）：会把 CI 里的权限问题变成「什么都不发生」。Seelex 的「显式拒绝并点名下一步」纪律是对的，别退回去。
2. **内部会变的会话格式**（Claude Code 自述「跨版本会变，别写脚本直读」）：Seelex 的 JSON v8 是自有格式，不要走「内部格式 + 随时破坏」这条路，要么版本化要么给稳定导出面。
3. **单 provider 锁死**（Claude Code 明说不支持非 Claude 模型）：Seelex 的角色化账号池已经比它好，不要为了调校便利退到单家。
4. **「不做沙箱、不做权限弹窗、用容器解决」的极简主义**（Pi）：Pi 敢这么赌是因为它把整件事交给用户，而且它没有 GUI 看板要保证可观测。Seelex 是「personal agent + 看板治理」，这条哲学与产品语义冲突。

---

## 6. 一句话定位

> **Seelex 是一个「把并行当事务、把权限当 UNIX、把远端当本机」的 personal agent harness。它的设计密度在六家里排前二，工程成熟度排最后。**

它不是 Claude Code 的替代品——Claude Code 赢在入口矩阵、沙箱、生态和模型调校；也不是 Codex 的替代品——Codex 赢在协议分层与三平台隔离。它的位置更接近「**给一个人的、可审计的多 agent 工作台**」：goal 栈 + 看板投影 + 里程碑屏障 + worktree/session 双绑定 + 消费式作业面，这套东西是「一个人把一件事拆开并行，还能回头查清每一步」的完整答案。

要往前走，三件事的优先级是：**沙箱 → 硬预算 → ACP**。沙箱决定它能不能安全地被信任，硬预算决定它能不能被长时间放手，ACP 决定它能不能被别人的编辑器看见。

---

## 附：本轮的「未确认」清单（不要当事实用）

| 断言 | 状态 | 说明 |
|---|---|---|
| Pi 的默认工具集是 4 个还是 7–13 个 | **冲突** | 官方 philosophy 段与第三方指南都说 4 个（read/write/edit/bash）；另一份 2026-04 的源码 review 数到 13 个 builtin。1.4.0 的实际集合未逐一验证 |
| Codex 的 crate 数与 LOC | **快照冲突** | 论文（2026-07）记 89→126 crates / 621K→1.12M LOC；2026-08 的中文解读说 100+ crate。只能当量级参考 |
| ZCode 是否为「Z Code」桌面 GUI 的同一产品线 | **未坐实** | 2026 早期「Z Code」被描述为把 claude code/codex/gemini 包成图形化的多 agent 协作平台；2026-09-20 开源的 `zai-org/ZCode` 是自研 runtime。二者大概率是同一产品线的重写，但**没有官方文字把两条线明确连起来** |
| Qoder 的 JetBrains 插件是否仍有 Quest / Repo Wiki | **文档确认无** | 插件文档目录只有 ai-chat / next / inline-chat / database；但用户社群仍有「插件里用 Quest」的旧贴，属过期信息 |
| ZCode 多 agent 的冲突解决机制 | **未公开** | 第三方明确点出「冲突编辑与重复劳动是已知失败模式」，官方无对应文档 |
| Qoder 云端的沙箱强度 | **未公开** | 只知「每 Session 一个隔离容器、互相不可达」 |
| Seelex 的 `plan_*` 工具面当前可见性 | **文档冲突** | `README.md` 说 WorkPlan DAG 工具面「已退役」，但运行时工具表里 `plan_load` / `plan_run` / `task_check_node` 等在 goal 技能门控下可见。以运行时为准，README 该处需更新 |
