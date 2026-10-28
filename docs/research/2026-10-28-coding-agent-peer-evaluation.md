# Coding Agent 同级产品对比与当前 Agent 评价

日期：2026-10-28

状态：产品与工作流调研；不是模型能力 benchmark，也不代表任何产品在本仓库的实测得分。

## 问题

评价当前 Codex coding agent，并对比同一能力层级的 coding agent 产品，回答：

- 各产品的核心路线和强项是什么；
- 当前 agent 相对竞品的能力差距在哪里；
- 对 Seelex 这种强架构约束、长会话、多领域拆分项目，哪种工作流更合适。

## 范围

纳入：

- Seelex
- OpenAI Codex
- Anthropic Claude Code
- Cursor Agent / Cloud Agents
- GitHub Copilot coding agent
- Cognition Devin
- Qoder
- OpenHands
- Aider
- Google Antigravity CLI

不纳入：

- 纯代码补全或 chat-only 工具；
- 只提供 Agent SDK、但本身不交付 coding workflow 的框架；
- 仅凭厂商 benchmark 排名判断工程质量。

既有机制矩阵已经记录在
[`2026-09-12-external-harness-comparison.md`](2026-09-12-external-harness-comparison.md)。
本文不重复逐项 harness 源码比较，只补充当前产品能力、适用场景和相对差距。
Seelex 部分以当前仓库 README、模块 README、源码和测试为准；外部产品以官方资料为准。

## 方法与限制

- 以官方产品文档、官方仓库和官方文档站点为主。
- 产品能力检查点：交互形态、自治与并行、上下文/规则、权限/隔离、验证与交接、扩展性。
- 不使用 SWE-bench 等单一分数排名：产品 harness、上下文预算、工具权限和测试环境不同，分数不可直接外推到本仓库。
- OpenAI Developers 页面和 Jules 文档在当前网络环境分别返回 HTTP 403 / 连接失败；
  Codex 部分改用官方 `openai/codex` 仓库和本会话已实际验证的能力，Jules 因此不纳入结论。
- 产品功能变化很快；本报告只反映 2026-10-28 能读取到的官方资料。

## 结论先行

当前 Codex coding agent、Claude Code 和 Cursor 3 属于同一梯队，但路线不同：

- **Codex**：本地/桌面/IDE/云多入口，强在受控执行、仓库指令、skills/plugins、MCP/hooks、多 agent 和工程化审查。
- **Claude Code**：跨 terminal/IDE/desktop/web 的统一引擎，强在自动记忆、subagents、background agents、hooks 和自然的项目规则消费。
- **Cursor 3**：强在 agent-first 工作台、可视化检查点、并行云端 VM、多仓环境和远程桌面/浏览器验证。

如果把“从 issue 到 PR”的 GitHub 流程最短作为目标，**Copilot** 最强；如果目标是
长时间异步跑 backlog，**Devin/Cursor Cloud Agents** 更产品化；如果要求自托管和可控
SDK，**OpenHands** 更强；如果只追求轻量、可解释的终端结对编辑，**Aider** 更直接。

**Seelex** 的差异点不是再做一个通用 IDE 或云代理，而是把 coding agent 做成可自托管、
本地优先的个人工程 harness。它在会话持久化、上下文读回、账号池、子代理/teamwork、
审批和可观测性上已有较深实现，但产品成熟度、OS 级隔离、IDE/云入口和 GitHub 原生
交付链仍落后成熟商业产品。

当前 Codex agent 不是功能数量最激进的产品，但属于**综合控制力、可审计性和复杂仓库
工程纪律最均衡的一档**。在这轮 Seelex store 拆分中，它的主要优势是能按
AGENTS/MEMORY/worktree/测试门禁工作；主要短板是模糊需求下的范围推断仍可能出错，
需要用户及时校准，且方案表达容易过重。

## 同级产品路线

| 产品 | 核心路线 | 明显强项 | 明显代价/边界 |
|---|---|---|---|
| Seelex | 自托管、本地优先的个人 Agent Harness | ProjectScope + 审批、JSON v8 会话/压缩可读回、账号池、subagent/teamwork/后台作业、Plugin/Skill/MCP、TUI/GUI/headless 共享 Application | Developer Alpha；无 OS 级 shell 隔离、IDE 扩展、托管云和 GitHub 原生 PR 闭环；无标准编码 benchmark |
| Codex | 本地 coding agent + 桌面/IDE/云入口 | 仓库指令、沙箱/审批、skills/plugins、MCP/hooks、多 agent、审查与文档能力均衡 | 产品面较多、配置复杂；复杂任务仍需高质量任务包 |
| Claude Code | 同一引擎覆盖终端、IDE、桌面和 Web | auto memory、CLAUDE.md/AGENTS.md、skills/hooks/MCP、subagents、background agents、CI/Agent SDK | 核心模型与服务生态主要围绕 Anthropic；跨产品工程治理需自己约定 |
| Cursor Agent | Agent-first IDE + Cloud Agents | 可视化 diff/checkpoint、继续工作时可追问/steer、隔离 VM、多仓、artifact、remote desktop、多模型 | 高自治/云能力依赖套餐与环境配置；编辑器平台绑定较强 |
| Copilot coding agent | GitHub 原生后台代理 | issue/PR/security alert 分派、临时云环境、分支与 PR 自动化、组织策略/防火墙、GitHub 治理 | 一任务一分支/PR，单会话最长 59 分钟，深度依赖 GitHub 工作流 |
| Devin | 云端自治软件工程师 | 长任务、并行 backlog、内嵌 shell/IDE/browser、Linear/Jira/GitHub 等集成、AGENTS/knowledge | 更偏托管代理，任务成本和过程可控性需要组织治理 |
| Qoder | 覆盖 IDE、JetBrains、CLI、Cloud Agents、SDK 的平台族 | Repo Wiki、Memory/Rules、Skills/plugins、上下文工程、多 agent、Cloud/API、移动审批 | 产品线较宽，选择与治理成本较高，需按具体入口评估成熟度 |
| OpenHands | 开源 Agent Canvas + SDK/Server + Cloud/Enterprise | 自托管、浏览器控制中心、SDK/API、沙箱、事件与工作区可控 | 需要自己承担部署、升级和运行环境治理 |
| Aider | 终端 AI pair programmer | git 原生、repo map、自动 commit/undo、多 LLM/provider、简单可解释 | 无强自治、云端/多 agent/CI 闭环不是主路线 |
| Antigravity CLI | Google 终端优先 agent | 多 subagent 并行、MCP/skills/hooks、轻量 terminal workflow、Gemini 4 生态 | 正处于 Gemini CLI → Antigravity CLI 迁移期；Gemini CLI 免费/个人入口已退役 |

## 维度对比

评级是面向“真实仓库工程”的定性判断，不是统一 benchmark：

| 维度 | Seelex | Codex | Claude Code | Cursor | Copilot | Devin | Qoder | OpenHands | Aider |
|---|---|---|---|---|---|---|---|---|---|
| 本地深度修改 | 强 | 强 | 强 | 强 | 中 | 中 | 强 | 强 | 强 |
| 长任务/异步自治 | 中-强 | 强 | 强 | 很强 | 强 | 很强 | 强 | 强 | 弱 |
| 并行工作台 | 中 | 强 | 强 | 很强 | 中 | 很强 | 中-强 | 中-强 | 弱 |
| 仓库规则/上下文 | 很强 | 很强 | 很强 | 很强 | 强 | 强 | 很强 | 强 | 中 |
| 上下文档账/读回 | 很强 | 强 | 很强 | 强 | 强 | 强 | 很强 | 强 | 中 |
| 可视 diff/回滚 | 中-强 | 强 | 强 | 很强 | 强 | 中-强 | 强 | 中 | 中 |
| OS 级隔离 | 弱 | 强 | 强 | 云端/VM 强 | 云端隔离强 | 云端强 | 依入口 | 强 | 弱 |
| GitHub/PR 原生闭环 | 弱-中 | 中-强 | 中-强 | 强 | 很强 | 强 | 强 | 中 | 弱 |
| 自托管/开放定制 | 很强 | 中 | 中 | 中-强 | 弱-中 | 中 | 中 | 很强 | 很强 |
| 标准 benchmark 证据 | 未发布 | 有公开生态评测但口径不同 | 有公开生态评测但口径不同 | 有公开生态评测但口径不同 | 有公开生态评测但口径不同 | 厂商/客户案例为主 | 厂商资料为主 | 研究生态较多 | 有自建排行榜 |
| 产品成熟度 | Alpha | 高 | 高 | 高 | 高 | 高 | 中-高 | 中-高 | 高（窄场景） |
| 学习/使用成本 | 中 | 较高 | 中 | 中 | 低-中 | 中 | 中-高 | 高 | 低 |

“上下文档账/读回”不是通用模型能力，而是产品是否把压缩事实、工具结果和原始证据
保留为可审计、可恢复的数据面。Seelex 在这项上有明确工程投入。

## Seelex 的相对优劣

### 强项

1. **本地优先和数据控制**

   Seelex 不依赖 IDE 云索引或托管代理环境。项目、会话、压缩、工具结果、审批和
   运行状态在本机组织，适合不希望把完整仓库上下文交给单一 IDE/云平台的使用者。

2. **持久化与恢复不是附带功能**

   当前 JSON v8 布局把会话顺序日志、项目/Session 隔离、message/EVENT head、
   分片、context/stack 和 tool result 引用分开。相比多数“会话 JSON + 重新喂模型”
   的实现，恢复边界更显式。

3. **Context Engineering 深度**

   稳定前缀、滑动窗口、主动压缩、压缩帧读回、超大工具结果 `result_ref`、
   `read_tool_result`、`read_compressed_turn`、`search_history` 构成了从“压缩”
   到“证据按需取回”的闭环。这是 Seelex 相对通用 IDE agent 的明显差异化。

4. **多 Agent 与后台作业**

   `fork_subagents` 具有独立 Session、作用域、账号路由和 token budget；
   teamwork 负责 leader/worker 作业；`bash_bg`、`read_batch`、`bg_manage`
   把分钟级任务从主回合中解耦。组合深度高于 Aider，也接近 Claude/Cursor 的
   subagent/background 路线。

5. **模型与账号层**

   OpenAI-compatible provider、按 agent/subagent/goalplan 角色的账号池、流式租约
   和可切换路由，使 Seelex 不绑定单一模型厂商。Aider 也支持多 provider，但
   Seelex 的账号池和角色路由更产品化。

6. **扩展和可观测性**

   Plugin 的事务式切换同时覆盖工具、System Prompt、Skill、MCP；Snapshot/Event、
   worktable/traceboard 和工具轨迹使运行过程可观察。与 Codex/Claude/Cursor
   的 skills/hooks/MCP 生态方向一致。

### 短板

1. **执行隔离是最高优先级安全差距**

   Seelex 的 `ProjectScope` 能把文件和 shell 的 cwd 限制在绑定的项目根内，
   `PermissionGate` 能 allow/ask/deny，但 shell 仍在宿主用户权限下执行。
   `CommandSandbox` 当前明确标注为 `cwd-gate`，不是 OS sandbox。
   Codex/Claude/Cursor/OpenHands 已提供 OS/容器/VM 级边界，这是 Seelex 与成熟
   产品之间最实质的安全差距。

2. **交互入口仍偏开发者自用**

   Seelex 有 TUI、Wails GUI、headless 和 backend console，但没有 VS Code/JetBrains
   扩展；GUI 仍是 Alpha。Cursor/Claude/Copilot 在 diff、编辑器上下文、移动端审批
   和 IDE 工作流上更成熟。

3. **云端/团队交付链较弱**

   Seelex 的 teamwork 仍是同一进程内、由 leader 提示词驱动的编排，不是完整 A2A；
   没有 Copilot 式 issue → 临时环境 → PR，也没有 Cursor/Devin 式隔离 VM 和运行
   artifact 工作台。

4. **产品成熟度和验证证据**

   Developer Alpha、JSON schema 仍可演进；SQLite/PostgreSQL/Redis 后端当前退役；
   未发布 SWE-bench/Terminal-Bench 结果；覆盖率仍是短板。商业竞品并不因此必然
   更正确，但它们在发布稳定性、兼容面和用户验证上更成熟。

5. **自动项目知识的产品面较早期**

   `project_refresh` + `seelex.project.md` + 模块文档索引已经能形成项目知识，
   但相比 Qoder Repo Wiki、Claude auto memory、Cursor rules/projects，仍更依赖
   仓库自身是否维护了高质量文档和元数据。

### Seelex 最适合的位置

- 本地、隐私敏感、需要自托管的 coding agent；
- 需要控制多模型、多账号和角色路由的个人/小团队；
- 需要保留完整会话事实、压缩证据和工具结果回读的长期工程任务；
- 需要并行 subagent/teamwork，但不想把仓库交给托管 IDE 云环境的场景。

不适合直接宣称已经替代：

- Copilot 的 GitHub 原生 issue/PR 自动化；
- Cursor/Devin 的成熟云端并行工作台；
- Claude/Codex 的开箱即用 IDE/桌面体验；
- OpenHands 级别的开源运行时生态和自托管 SDK。

## 当前 Codex Agent 的优势

### 1. 复杂仓库纪律较强

Codex 会读取仓库级 `AGENTS.md`、局部规则、Memory 和现有文档，再开始修改。
在 Seelex 这轮工作中，它能够主动：

- 隔离到专用 worktree；
- 避免覆盖主工作区未提交改动；
- 追踪领域依赖、方法 ratchet、结构门禁和删除清单；
- 把“运行态判据”“耐久持久化事实”“event 投影”分开讨论。

这类能力在 DDD、大型 Go 仓库和迁移类任务中比单纯生成代码更重要。

### 2. 控制面完整

同级产品里，Codex 的沙箱、审批、skills/plugins、MCP、hooks、非交互执行和多
agent 组合比较均衡。它不是单纯聊天窗口，也不是完全不可干预的云端黑盒。

### 3. 可审计

当前 agent 会保留：

- 读取了哪些文件；
- 执行了哪些命令；
- 哪些测试通过、哪些未执行；
- 哪些是事实、哪些是推断；
- 哪些生产代码被修改、哪些只是方案。

这对高风险重构比“最后给一个总结”更有价值。

### 4. 方案与实施可以分层

Claude/Cursor/Devin 也能做计划，但 Codex 在“front-review → 方案 → 结构门禁 →
分批实施”的工程化流程上比较自然。当前会话中，用户要求“只给方案”时能保持生产
代码不变，这是明显优点。

## 当前 Codex Agent 的差距

### 1. 模糊范围仍需用户纠偏

最初把“Session 在其他包里的重复实现”理解成根 `store` 与 `store/session` 的比较，
属于典型的范围推断错误。Claude/Cursor 的强项之一是更积极地在计划阶段提出边界
确认；Codex 当前更倾向于先形成完整方案，再等待用户指出偏差。

对 Seelex 这类概念密集仓库，差距不在代码能力，而在**前置语义澄清**。

### 2. 方案容易过重

当前 agent 倾向输出大矩阵、完整迁移顺序、风险清单和验收断言。优点是可供审计，
缺点是可能把简单边界讨论扩展成新的“文档上帝模块”。Claude Code 和 Aider 在
小任务上通常更短、更直接；Cursor 则更强调可视化 diff 和即时回滚。

### 3. 产品化并行/验证体验仍可加强

Cursor 的并行 Agents Window、隔离 VM、artifact、remote desktop 和可共享运行现场
更像“多个工程师的工作台”；Copilot 的 issue → 临时环境 → PR 是更短的 GitHub
交付链；Devin 更强调把并行 backlog 当作核心产品。

Codex 已经支持多 agent 和 worktree，但在“多个任务统一看板、运行产物可视化、
从 issue 直接委派到 PR”的产品体验上，仍更多依赖任务编排和底层工具。

### 4. 自动长期记忆的显式产品感稍弱

Claude Code 把 auto memory 和跨会话规则消费放在显著位置；Qoder 将 Repo Wiki、
Memory/Rules 作为上下文工程核心；Cursor 有 rules、memories、projects 和侧聊记录。
Codex 的 AGENTS、skills、plugins 和 Memory 组合很强，但更依赖用户主动维护高质量
仓库知识。

对本仓库而言，这会把一部分负担压回 `AGENTS.md`、Memory 和开发文档质量。

### 5. GitHub 原生闭环不是最短路径

Copilot 可以直接把 issue、PR、code scanning alert 分派给 agent，并在 GitHub
托管的临时环境执行。Codex 可以完成同样的工程工作，但产品链路不像 Copilot
那样直接嵌入 GitHub 事件模型。

## 同级产品各自最适合什么

| 使用场景 | 首选 | 理由 |
|---|---|---|
| 强约束复杂仓库、需要严谨计划和审查 | Codex / Claude Code | 两者都重视项目指令、工具证据和多阶段工作流 |
| 大量并行云端编码任务 | Cursor Cloud Agents / Devin | 隔离 VM、多任务工作台、artifact 和交接更产品化 |
| GitHub issue/PR 自动修复 | GitHub Copilot coding agent | GitHub 原生、权限/组织策略/PR 闭环最短 |
| 需要 Repo Wiki、持久项目知识与多入口 | Qoder | 上下文工程和平台族覆盖最完整 |
| 自托管、SDK、可修改运行时 | OpenHands | 开放程度和部署自由度最高 |
| 轻量、git 原生、可控的终端结对编程 | Aider | 简单、可解释、模型 provider 灵活 |
| Google 终端多 subagent 工作流 | Antigravity CLI | 当前 Google 推荐入口；需接受迁移期风险 |

## 对 Seelex 的具体建议

### 继续使用 Codex 的位置

适合：

- Store DDD 拆分、契约重排、依赖方向治理；
- 需要读大量文档并保持非 store 业务语义；
- 需要 worktree、结构门禁、测试和多批提交；
- 需要把“为什么这样拆”沉淀成可审计文档。

### 不应只依赖一个 agent 的位置

建议把以下工作交给第二 agent 或人工评审：

- 架构边界是否真正唯一；
- “事实源/状态机/投影”分类是否正确；
- 大规模删除清单是否遗漏调用方；
- 跨数周的长期记忆是否仍准确。

Claude Code 适合做第二视角的架构 review；Cursor 适合并行分支和可视化回滚；
Copilot 适合 GitHub issue/PR 收尾；OpenHands 适合需要自托管验证的敏感仓库。

### 本仓库最值得补的 agent 支撑

1. 在 `AGENTS.md` 增加“先回答语义类别再找落点”的前置门禁：
   `运行态 / 耐久事实 / 投影 / 装配 / 测试`。
2. 把领域唯一 owner 和禁用依赖做成可执行结构门禁，减少对 agent 记忆的依赖。
3. 对高风险任务固定使用专用 worktree，并要求 `git status + baseline commit` 证据。
4. 为跨会话架构决策维护短索引，避免每次从大 plan 文档重新建立上下文。
5. 对同一架构任务使用第二 agent 做只读 review，不共享修改权限。

## 推荐组合

当前最现实的组合不是替换 Codex，而是分工：

```text
Codex       主实施 + 结构门禁 + 验证证据
Claude Code 第二视角架构 review / 规则与记忆交叉检查
Cursor      并行实验、可视化回滚、云端环境验证
Copilot     GitHub issue → PR / 组织级自动化
```

对个人开发，保留 Codex + 一个第二 review agent 已足够；对团队，再增加 Copilot
或 Cursor Cloud Agents 承担 issue/PR 和并行环境。

## 对当前 agent 的最终评价

**定位：强工程执行者，不是完全自主的架构裁决者。**

优势：

- 复杂仓库导航和约束遵守；
- 方案、代码、测试、证据一体化；
- 对危险操作和工作区污染有明确意识；
- 能进行多 agent、MCP、skills、hooks 和自动化组合。

主要差距：

- 模糊需求下会先推断而不是先收敛；
- 容易把方案做得比问题本身更重；
- 自动持久化项目知识不如内存/Repo Wiki 型产品显眼；
- GitHub backlog → PR 的原生闭环不如 Copilot；
- 云端并行工作台和 artifact 呈现不如 Cursor/Devin 直接。

综合评价：**同级第一梯队，适合高风险复杂仓库；当前约 8/10。**
若把“一次就正确理解架构范围”和“减少方案冗余”补上，可进入 8.5–9/10。

## 来源

- Seelex README（当前能力、Developer Alpha、已知边界）：
  [`../../README.md`](../../README.md)
- Seelex 项目作用域、权限与 CommandSandbox：
  [`../../backend/limit/gate/README.md`](../../backend/limit/gate/README.md)
- OpenAI Codex 官方仓库：
  <https://github.com/openai/codex>
- Codex 文档入口：
  <https://developers.openai.com/codex>
- Claude Code Overview：
  <https://code.claude.com/docs/en/overview>
- Cursor Agent：
  <https://cursor.com/docs/agent/overview>
- Cursor Cloud Agents：
  <https://cursor.com/docs/cloud-agent>
- Cursor Agents Window：
  <https://cursor.com/docs/agent/agents-window>
- GitHub Copilot coding agent：
  <https://docs.github.com/en/copilot/concepts/agents/coding-agent/about-coding-agent>
- Devin Introduction：
  <https://docs.devin.ai/get-started/devin-intro>
- Qoder Product Overview：
  <https://docs.qoder.com/product-series/what-is-qoder>
- OpenHands Introduction：
  <https://docs.openhands.dev/overview/introduction>
- Aider Documentation：
  <https://aider.chat/docs/>
- Aider Git Integration：
  <https://aider.chat/docs/git.html>
- Aider Repository Map：
  <https://aider.chat/docs/repomap.html>
- Google Antigravity CLI：
  <https://antigravity.google/product/antigravity-cli>
- Gemini CLI：
  <https://github.com/google-gemini/gemini-cli>
