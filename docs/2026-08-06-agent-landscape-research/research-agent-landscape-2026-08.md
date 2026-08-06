# Coding Agent 竞品格局调研（2026 年中）与 Seelex 下一步

> 调研日期：2026-08-06
> 视角：更新既有两份调研（`docs/research/coding-agent-harness-comparison.md`、`docs/research/agent-harness-research-report.md`，均为 2026-08-02 冻结），补充 **2025H2–2026 中期**各产品新进展，并按四个关键方向横向对比：
> ① 记忆/长期上下文；② 工作台可视化；③ 定时/周期任务；④ 自迭代闭环（需求→coding→test→product）。
> 配套规划：本文的落地重点（自迭代试点）见同目录 `plan-iterative-product-pilot.md`。
> 信息口径：全部来自公开网络检索（WebSearch，本环境 WebFetch 被网络策略拦截）；厂商自报数据（SWE-bench 分数、修复率等）标注"未证实"，不作为决策依据。
> Seelex 现状以 2026-08-05 前代码与测试为准。

---

## 一、2026 年行业总体趋势（先说结论）

1. **竞争焦点从"单会话助手"转向"持久化 Agent 工作台"**：Claude Code（Agent View / workflows）、Cursor 3.0（agent-first IDE）、Windsurf 2.0（Agent Command Center）、Devin（Desktop + Cloud）、OpenHands（Agent Canvas）都在做"多 agent 并发 + 可视化管理 + 可恢复"的工作台形态。
2. **记忆成为标配**：Claude Code Auto Memory + Auto Dream（自动写入 + 后台自动整理）、Gemini CLI Auto Memory（自动提炼 + inbox 人工确认）、Windsurf Memories、Devin Knowledge/DeepWiki、Codex 桌面持久记忆。尚未落地的只剩 Cursor（第三方 MCP 补齐）与 seelex。
3. **定时任务从"本机 cron 包装"升级为"云端托管 routine"**：Claude Cloud Routines（三触发器）、Codex Automations GA（scheduled runs + 评审队列）、Copilot 云 agent Automations、Devin scheduled sessions（跨次状态保持）。开源侧 OpenHands Automations 最完整。
4. **自迭代闭环收敛出共识骨架**：需求（编号可测试验收标准）→ 分解（含并行子会话）→ 测试驱动执行（**exit code 为适应度，不用 LLM 当裁判**）→ 独立评审（写者不给自己打分）→ PR/交付 + checkpoint 回滚 → 人工生产门。代表：Devin spec-to-PR、Factory droid 流水线、Claude Code Preview/Review/Merge + Dynamic Workflows、Replit Agent 4。
5. **已知失败模式已有行业共识**：长循环上下文漂移（context rot）、compaction 的"认识论级"假事实、测试空洞→假绿、flaky 误诊导致的投机式修复、无验证闭环时约 12% 回归率、基准分数不可比（DeepSWE 审计出验证器假阳 8.5%/假阴 24%）。**这些直接决定 seelex 自迭代试点的护栏设计**（见规划文档）。

---

## 二、头部产品 2025H2–2026 中期新进展

### 2.1 Claude Code（Anthropic）——记忆与闭环的"形态定义者"

| 方向 | 2025H2–2026 中期进展 |
|---|---|
| ① 记忆 | **Auto Memory**（v2.1.59，2026-02-26）：自动把会话中学到的内容写入 `MEMORY.md`（`~/.claude/projects/<path>/memory/`），与人工维护的 CLAUDE.md 分离；前 200 行每会话注入，主题文件按需加载；可开关。**Auto Dream**（2026-03 起放量）：后台"睡眠巩固"子代理，满足"≥24h 且 ≥5 会话"时自动合并/去重/清理记忆。**Agent Memory**（v2.1.33）：子代理 frontmatter `memory` 字段自带持久 markdown 知识库（user/project/local 三级）。已知问题：token 开销上升、记忆文件在项目外不随 git（"shadow state"）。 |
| ② 工作台 | **Agent View**（研究预览）：多会话管理视图，可 `/bg` 后台化 + git worktree 隔离并行。**Checkpoint/Rewind**：每个用户 prompt 自动快照（保留 100 个 / 30 天），支持"恢复代码+会话/仅代码/仅会话/从这里摘要/摘要到此"五种操作；Agent SDK 侧 `rewind_files()`。**/workflows**：动态工作流运行中可后台执行、查进度。 |
| ③ 定时 | 四层最全：**Cloud Routines**（2026-04，Scheduled/API/GitHub 三触发器，无权限弹窗自主运行，Pro 5 次/日上限）、**Desktop 定时任务**（Manual/Hourly/Daily/Weekdays/Weekly 预设，独立 worktree，睡眠后 7 天内补跑）、会话内 **cron 工具**（/loop，每会话最多 50 个、7 天过期）、系统 cron + `claude -p` headless（官方推荐配 `--max-turns/--max-budget-usd/--allowedTools` 三重护栏）。 |
| ④ 闭环 | **Preview/Review/Merge**（2026-02 桌面闭环）：本地 diff 审阅 → PR 监控 → Auto-fix → Auto-merge。**Dynamic Workflows**（v2.1.154，2026-05-28）：Claude 现场写 JS 编排脚本（`agent()`/`pipeline()`），单会话 fan-out 数十至数百并行子代理，**每个结果被对抗性验证 agent 复核**后才交给用户（Bun 重写 Rust 75 万行 11 天案例）。**Agent Teams**（实验）：lead 会话协调多个完整 Claude 实例。嵌套子代理 5 层后因失控改回默认禁止（v2.1.217）。**Long-Running Agent Harness**（研究）：外部化 checklist（200+ 条 `passes:false` 需求 JSON + 进度文件 + 每会话重新落地协议）强于泛化自检（10/10 vs 5/10）。 |
| 其他 | 1M context GA（承认 context rot：1M 下 MRCR v2 精度 93%→76%）；**Fable 模型**（2026-06-09，`claude-fable-5`，SWE-bench Verified 95% 厂商自报，$10/$50 每 M token）；MCP 冷启动（工具搜索延迟加载，上下文减少约 95%）；Remote Control；Agent Skills 开放标准（~30 工具采纳）。 |

### 2.2 Cursor（Anysphere）——工作台可视化最细

| 方向 | 进展 |
|---|---|
| ① 记忆 | **无原生记忆**（截至 2026 年中仅社区 feature request，未证实有内置）。**已移除语义 embedding 索引**（官方确认：模型已擅长 grep/索引搜索；改为客户端稀疏 n-gram 正则索引 + Instant Grep，面向 monorepo）。记忆靠第三方 MCP（claude-mem / PMB / memgrep 等）。 |
| ② 工作台 | **Cursor 2.0**：最多 8 并行 agent、各独立 git worktree、Agents 面板（任务/状态/当前 diff）、Shadow VFS 合并虚拟树为单一 diff。**Cursor 3.0**（2026-04-02）：agent-first 工作台，Composer 面板升级为全屏 **Agents Window**（多 agent 卡片：任务/进度/读写文件/执行命令、grid/平铺布局、本地/worktree/云/SSH 四环境并行）、**Design Mode**（内置浏览器点选 UI 下指令）、Cloud Agent 自动生成 demo 截图。**Checkpoints**（原生，agent 每次编辑前自动快照，独立于 git）——但 2026 年 bug 缠身（discard 后文件仍被改、restore 按钮大面积消失），官方建议 Local History + git 兜底。 |
| ③ 定时 | 原生未证实（第三方 memgrep / cron MCP 提供）。 |
| ④ 闭环 | **Cloud Agent 云接力**：本地会话 `&` 前缀一键迁云端续跑（关机不中断），自动生成演示与截图供验证；自托管云 agent。自迭代验证机制弱于 Claude Code（无内置对抗验证，靠人工审 worktree diff）。 |
| 其他 | 自研模型 Composer（500K 上下文、SWE-bench 51.7% 厂商自报）；Cursor CLI（/plan /ask /agent + 云接力）。 |

### 2.3 Windsurf → Devin Desktop（Cognition）——Kanban 多 agent 管理

| 方向 | 进展 |
|---|---|
| ① 记忆 | **Memories**（原生）：Cascade 自动生成（不耗额度）+ 用户自然语言手动创建；workspace 级、跨会话持久；官方定位"长寿命 prompt 状态而非知识库"，无审计/修剪机制。 |
| ② 工作台 | **Windsurf 2.0**（2026-04-16）：**Agent Command Center**——所有本地+云 agent 的单一 Kanban 看板（针对"几十个 agent 同时跑"的注意力瓶颈）；**Spaces**——按任务聚合 agent 会话/PR/文件/上下文；Cascade 面板提供 plan 预览、分步 diff 审批、工具调用透明、失败步骤恢复。Workflows：`.windsurf/workflows/*.md` 可复用配方，斜杠命令手动调用（**永不自动调用**）。Checkpoints 未证实。 |
| ③ 定时 | 原生未证实（第三方 MCP 提供）。 |
| ④ 闭环 | **内嵌 Devin 云 agent**：本地 Cascade 规划 → 一键委托 Devin（VM/桌面/浏览器）→ 合上电脑继续跑 → 自动提交 PR 回编辑器审阅。 |

### 2.4 GitHub Copilot coding agent——依托 GitHub 平台的闭环

| 方向 | 进展 |
|---|---|
| ① 记忆 | CLI GA（2026-02-25）宣称跨会话 remember；自定义指令（`.agent.md`）体系；**未见自动长期记忆机制（未证实）**。AGENTS.md 支持（2026-06-18）。 |
| ② 工作台 | **Plan Mode**（Shift+Tab / /plan）：提问澄清 → 结构化计划 → 人工审批 → 执行；内置 Plan/Code-review/Init/Explore/Task 五类 agent + 自定义 agent。**Agent Task 面板**：编辑器内 hand off 到 Agent Mode / "Continue in Background" / `&` 前缀委托云端 agent。Checkpoint 未证实（回退靠 git）。 |
| ③ 定时 | **云 agent Automations**（2026-06-02，Microsoft Build）：定时/事件触发（小时/日/周间隔，或 issue/PR 事件），每任务可配名称/prompt/触发器/工具/模型，按量计费。CLI **/every 与 /after**（自然语言/cron/相对时长 + schedule manager）。 |
| ④ 闭环 | **Code Review agent 转 agentic 架构**（2026-03，GitHub Actions 内自主收集仓库上下文）；**自我评审**（2026-02：开 PR 前先用自己的 review agent 审自己的改动）；**Fix with Copilot**（2026-05：一键/批量把评审意见交给云 agent 修复，可直改 PR 或开新 PR）；Copilot App（issue/PR 编排，Interactive/Plan/Autopilot 三模式）。 |

### 2.5 OpenHands（All Hands AI）——开源侧闭环与自动化最完整

| 方向 | 进展 |
|---|---|
| ① 记忆 | Condenser 压缩（llm 摘要/recent/amortized 策略）+ EventStream 作为唯一事实源；SDK v1.31.0 对话历史**树状结构（分支/fork）**（可回溯/并行探索会话分支）。 |
| ② 工作台 | GUI **Task List 页签**（v1.5.0，实时显示 agent 任务列表与状态）；**planning agent**（计划/代码模式切换）；cloud 版 **sub-agent task visualizer**（子代理任务可视化）；chat 内实时 agent 活动展示（v1.9.0）；**Agent Canvas + Agent Client Protocol**（2026-06：自托管浏览器工作台，同一界面切换 Claude Code/Codex/Gemini CLI 等 ACP 兼容 agent）。 |
| ③ 定时 | **Automations（beta）**：cron 表达式或事件 webhook 触发整场会话（自然语言描述 + GitHub 凭据 + 每次运行独立沙箱 + 会话可回看）——开源侧最完整；官方仓库自身用 GitHub Actions cron 做 issue 查重、todo 扫描自动开 PR。 |
| ④ 闭环 | Issue Resolver 自动开 PR 并回链对话（`create_pr`/`create_mr` MCP 工具）；预置自动化（PR Review Assistant、Repository Monitor、org 级 QA 按 `pull_request.opened` 触发）；**Agent Control Plane**（2026-05：企业级 agent 舰队编排——并行/重试/状态/最小权限/隔离沙箱/用量追踪）。CodeAct v3 SWE-bench 68.4% 仅见二手综述（未证实）。 |

### 2.6 Devin（Cognition）——定时与并行交付的旗帜

| 方向 | 进展 |
|---|---|
| ① 记忆 | **DeepWiki / Devin Wiki**：自动把仓库变成可对话 wiki（架构图 + 源码链接摘要），"Ask Devin" 直接回答；组织级知识库（会话内合并去重、从代码模式创建知识、MCP 全 CRUD、调度会话做每周知识维护）；**Auto-Triage** 长期记忆（记住既往调查、去重、关联工单）。 |
| ② 工作台 | **Plan 视图**（RL 训练的规划子代理产出分步计划，可批准/调整，后台持续精化）；Todo 列表（对话内跟踪）；**命名 checkpoint 快照**（可导航/回滚，回滚不可逆）；**事件时间线**（任意会话完整时间线可查可检索，"Follow Devin" 逐动作高亮）；Devin Desktop 的 Agent Command Center（Kanban）。 |
| ③ 定时 | **scheduled sessions**（2026-02 起，定时循环会话）+ **recurring scheduled tasks**（成功任务一键转周期工作流，如每周 release notes/QA/feature flag 清理；**运行间保留状态**，如"汇总本周新合并 PR"跨次累积）；Devin 可自己安排自己的循环会话；30% 的 Devin 已通过 API/managed/scheduled 自动启动（CEO 访谈）。 |
| ④ 闭环 | 端到端：分解 → 写码 → 跑测试 → 修错 → 自动开 PR（独立 VM）；**Managed Devins**（协调者分解 → 多工作 Devin 独立 VM 并行 → 冲突消解/结果汇总）；Devin Review 自动响应 review 评论、迭代 CI 失败（GitHub auto-merge 内建，70–90% 修复率厂商自报）；spec-to-PR 流程（第三方报道 80% 代码提交成功率）；Auto-Triage 事件触发（Slack/Linear/GitHub/观测）。Devin v3（2026-02）加动态重规划、并行会话、v3 API（RBAC）。 |

### 2.7 Gemini CLI（Google）——记忆的"人工确认制"路线

| 方向 | 进展 |
|---|---|
| ① 记忆 | **Auto Memory**（v0.42.0，2026-05-12）：后台挖掘历史会话，**提议**记忆更新（统一 diff `.patch`）与可复用 Agent Skills（`SKILL.md` 草稿）；经 `/memory inbox` 人工审阅后应用，**不自动写入**（与 Claude Code 的自动写入形成两条路线）；私有记忆写 `MEMORY.md`、全局写 `~/.gemini/GEMINI.md`，**项目共享 GEMINI.md 禁止自动提取**（防污染）；回滚安全网 + 路径白名单。 |
| ② 工作台 | CLI/TUI 形态，可视化弱；事件驱动调度器带来实时工具调用反馈（TOOL_CALLS_UPDATE）；subagents（2026-03 preview.5）：`@agent` 调用、独立上下文防"上下文腐坏"、工具隔离（私有 ToolRegistry）、远程 A2A 路由。 |
| ③ 定时 | **无原生 /scheduled（已核实不存在）**；官方推荐 `--prompt` 接外部 cron / GitHub Actions 定时工作流（官方仓库自用 cron 做 issue triage）。 |
| ④ 闭环 | 无原生自动交付；**A2A server**（REST/JSON-RPC/gRPC 暴露 agent 能力 + git checkpoint 可恢复工具调用）可被远程调度执行。 |
| 其他 | SandboxManager 全面落地（v0.36.0：Linux bubblewrap/seccomp、macOS Seatbelt、Windows 统一接口 + git worktree 隔离并行会话）。 |

### 2.8 OpenAI Codex CLI / Codex Cloud——scheduled runs 与持久会话

| 方向 | 进展 |
|---|---|
| ① 记忆 | macOS 桌面 App（2026-02/04）：**持久记忆**（项目偏好/纠正，云端、锁定 OpenAI 内、不跨工具）、Computer Use（后台操作桌面 + 多并行 agent）；社区批评"无真正记忆"，常用 AGENTS.md + MCP memory server 兜底。**Goal Mode GA**（v0.133.0，2026-05-21）：持久化线程状态机，网络中断/合盖后续跑，服务器端计算 + 自动压缩上下文。 |
| ② 工作台 | 桌面 App：多终端页签、侧栏文件预览、会话历史/恢复（CLI↔Desktop 会话互见仍多处 bug）；TUI 会话选择器重设计。**Checkpoint 明确缺位**：`undo` 只能回退一步（官方哲学反对深层回滚，社区建议手工 git checkpoint）。 |
| ③ 定时 | **Codex Automations GA**（2026）：定时运行（scheduled runs）+ 每任务模型/推理级别 + 执行目标选择（隔离 git worktree 或既有分支）+ 可复用模板；示例：周五 9:00 周度 worktree 清理、每日 repo 简报、issue triage、CI 失败汇总；结果落入**评审队列**供人工处理。**Workspace Agents**（2026-04）：云端常驻 agent，定时/事件触发、离线继续、跨项目保留上下文、组织共享。 |
| ④ 闭环 | GPT-5.3-Codex（2026-02-05）的 **Review → Repair → Validate 自验证循环**；/plan 拆分里程碑 → Cloud 环境实现 → 审 diff → 开 PR 的官方推荐工作流；Automations 产出进评审队列 + PR 评论处理 = "自动执行、人工验收"半闭环。 |

### 2.9 自迭代交付的另一极：Factory 与 Replit（详见闭环调研）

- **Factory AI**（2026-06 定位 org 级"软件工厂"）：`signals → plan → build → test → review → secure → ship → monitor` 全链路；droid 即技能包（`.factory/skills/<name>/SKILL.md`，markdown + YAML frontmatter，人写技能、代理自动装配）；**Test Droid 默认 TDD-first**（先写测试再写实现）；**Review Droid**（STRIDE + OWASP + 供应链，两遍流水线 P0–P3 分级）；Droid Action 直接进 GitHub CI；模型路由控成本。
- **Replit Agent 4**：连续运行数小时、自测自修自推进（配合 >1M context 与 compaction）；**App Testing**（真实浏览器闭环：点击/填数据/校验 UI → 分析修复 → agent 自行决定何时自测，视频回放）；最多 10 并发 agent 自动拆分重组；**Plan-while-building**；Kanban 任务板；平台级自改进（夜间读全部用户 traces → 找问题 → 生成改 prompt 的 PR → A/B 上线）。CEO 自述："一个 prompt 造出一切"的过度承诺导致用户流失，真实世界是迭代与管理。

---

## 三、关键方向横向对比

### 3.1 ① 记忆 / 长期上下文

| 产品 | 记忆形态 | 关键设计选择 |
|---|---|---|
| Claude Code | CLAUDE.md + **Auto Memory**（自动写入 MEMORY.md）+ **Auto Dream**（后台自动整理）+ Agent Memory（子代理级） | 自动写入 + 自动整理；已知 token 开销与"shadow state"问题 |
| Gemini CLI | GEMINI.md + **Auto Memory**（自动提炼 + `/memory inbox` 人工确认） | **人工确认制**；项目共享文件禁止自动提取 |
| Windsurf | Memories（workspace 级自动生成） | 长寿命 prompt 状态；无审计/修剪 |
| Devin | DeepWiki + Agent Memory/Knowledge（决策/结构/错误）+ Auto-Triage 长期记忆 | 组织级知识库 + 定期维护 |
| Cursor | 无原生（第三方 MCP） | **已移除 embedding 索引**，转稀疏 n-gram + Instant Grep |
| Copilot | 未证实（CLI 跨会话上下文 + AGENTS.md） | 平台指令体系 |
| Codex | 桌面持久记忆（云端、锁定厂商内）+ Goal Mode 持久状态 | 记忆不跨工具 |
| OpenHands | Condenser 压缩 + 会话历史树（分支/复刻） | EventStream 为唯一事实源 |
| **Seelex** | ProjectKnowledge（自动构建、hash 版本化、会话前预读）；**无跨会话事实/决策记忆** | memory 向量化 v2 设计已存在（`docs/plan/memory-architecture.md`），未落地 |

> Seelex 结论：ProjectKnowledge 的自动性优于静态 CLAUDE.md，但"跨会话记忆"整体缺席是 **2026 年竞品中最明显的共性差距**——除 Cursor 外所有主要对手今年都发布了原生记忆能力。

### 3.2 ② 工作台可视化

| 产品 | 工作台形态 | 关键要素 |
|---|---|---|
| Claude Code | Agent View + /rewind 菜单 + /workflows 进度 | 多会话总览、checkpoint 快照与五种回滚、workflow 进度 |
| Cursor | **Agents Window**（3.0 全屏卡片） | 任务/进度/读写文件/命令/完整轨迹、4 环境并行、原生 checkpoint（bug 多） |
| Windsurf/Devin | **Agent Command Center（Kanban）** + Spaces + Cascade/Devin 面板 | 多 agent 看板、Plan 视图、Todo 列表、命名 checkpoint、事件时间线 |
| Copilot | Plan Mode + Agent Task 面板 | 计划审批流；无 checkpoint |
| OpenHands | Task List 页签 + sub-agent 可视化 + Agent Canvas | 实时任务列表、子代理任务可视化、浏览器工作台（ACP） |
| Codex | 桌面 App 页签/预览/会话历史 | 无 checkpoint 回滚 |
| Gemini CLI | TUI（弱） | 实时工具调用反馈 |
| **Seelex** | TUI + GUI(Alpha)：Plan 面板（点击节点看会话/打点/事件/工具活动）、tasklist 勾选；Agent Workbench PRD（卡片/多会话/右栏工作台，proposed 未落地） | 缺 SubAgentTree 树形视图、缺 checkpoint 回滚、缺 Plan 流程图 |

> Seelex 结论：Snapshot+Event 协议与 Plan 节点详情面板是很好的底座；**缺口集中在子代理树可见性（已有设计未上线）与 checkpoint 回滚（竞品普遍标配）**。

### 3.3 ③ 定时 / 周期任务

| 产品 | 定时能力 | 形态 |
|---|---|---|
| Claude Code | Cloud Routines（Scheduled/API/GitHub 三触发）+ Desktop 定时任务 + 会话内 cron + Actions schedule | 四层最全；云托管 |
| Codex | Automations GA（scheduled runs）+ Workspace Agents（常驻） | worktree 隔离 + 评审队列 |
| Copilot | 云 agent Automations + CLI /every /after | 定时/事件双触发 |
| OpenHands | Automations（cron + webhook，beta） | 开源最完整；每次独立沙箱 |
| Devin | scheduled sessions + recurring scheduled tasks（**跨次状态保持**） | 成功任务一键转周期工作流 |
| Gemini CLI | **无原生**（外部 cron + GitHub Actions） | — |
| Cursor / Windsurf | 未证实（第三方） | — |
| **Seelex** | **无**（仓库代码无 cron/scheduled 痕迹） | — |

> Seelex 结论：定时任务是一片完全空白且竞争已白热化的领域；seelex 作为本地 harness，**先补"headless 非交互执行 + 会话级 cron"两层**（对齐 Claude Code 的 /loop 与系统 cron + `-p` 组合）成本最低、收益直接（自迭代试点转周期任务的前提）。

### 3.4 ④ 自迭代闭环（需求→coding→test→product）

| 产品 | 闭环形态 | 关键机制 |
|---|---|---|
| Claude Code | Preview/Review/Merge 桌面闭环 + Dynamic Workflows + Auto-fix PR + Routines | **对抗性验证 agent**（写者不给自己打分）；bot 自触发 guard；hooks 强制门；外部化 checklist |
| Devin | spec-to-PR + Managed Devins 并行 + 自动 PR + auto-merge | 编号验收标准；独立 VM；checkpoint 回滚；人工检查点；CI 失败自动迭代 |
| Factory | droid 流水线（signals→…→ship→monitor） | **TDD-first**；独立 Review/Security droid；`automatic_review: true` 进 CI |
| Replit | Agent 4 长跑 + App Testing（真实浏览器闭环） | 自测自修；10 并行；Kanban；平台自改进 |
| Copilot | 自我评审 + Fix with Copilot + Review agent | 开 PR 前自审；评审意见自动修复 |
| Codex | Review→Repair→Validate + 评审队列 | 自验证循环；人工验收 |
| OpenHands | 自动开 PR + PR Review 自动化 + org QA + Control Plane | 事件触发；重试/状态/审计 |
| **Seelex** | Plan DAG（agent/approve/verify/deliver 节点）+ 任务终态协议 + merge-back | **骨架已有**：验证节点、审批节点、交付节点、打点协议；**缺**：失败回环、独立评审、交付物协议、定时化 |

> Seelex 结论：**闭环骨架是竞品中最接近的**——Plan DAG 的 verify/approve/deliver 节点类型 + task_check_node/task_complete 终态协议 + GUI 打点，已覆盖共识闭环的大部分环节；缺的是"失败自动回环"、"独立评审上下文"与"交付物/PR 协议"，这正是自迭代试点要补的（见规划文档）。

---

## 四、Seelex 现状对照：本次新增缺口（叠加既有两份报告）

既有报告已列：执行沙箱（P0）、检查点/回滚（P0）、代码库检索（P1）、跨会话记忆（P1）、hooks、completion buffer、子代理可见性（P0）、A2A（长期）。本次调研**新增或强化**的差距：

| # | 新增/强化差距 | 竞品证据 | 关联既有项 |
|---|---|---|---|
| N1 | **无失败回环**（verify 失败 → 修复 → 重验的自动循环） | Claude Auto-fix、Devin CI 迭代、ruflo 修复循环 | 复用 verify 节点 + Effort 预算（新增） |
| N2 | **无独立评审上下文**（写者不给自己打分） | Claude security-guidance / Dynamic Workflows 对抗验证、Factory Review droid、Copilot 自我评审 | 子代理机制现成，新增 review 节点语义 |
| N3 | **无交付物协议**（deliver 节点产出 PR 描述/变更报告，非裸文本） | Devin/OpenHands 自动 PR、Copilot Fix with Copilot | deliver 节点已存在，补输出契约 |
| N4 | **无定时/周期能力**（代码无 cron） | 见 3.3 | 新增方向 |
| N5 | **headless 非交互执行缺位**（无 `-p` 式单次执行模式与护栏参数） | Claude `-p --max-turns`、Codex automations、Gemini `--prompt` | 试点与定时共同前置 |
| N6 | **跨会话记忆仅剩设计**（memory 向量化 v2 未落地；竞品全部 2026 年内发布） | 见 3.1 | docs/plan/memory-architecture.md |
| N7 | **Checkpoint 回滚仍缺位**（竞品 2026 标配，Cursor 因 bug 反证其必要性） | Claude /rewind、Devin 命名快照、Cursor 原生 checkpoint | 既有 P0，本次确认竞争压力上升 |
| N8 | **记忆/教训的"自动提炼"形态未定**（Claude 自动写入 vs Gemini inbox 人工确认制，路线二选一） | Claude Auto Memory、Gemini Auto Memory | 与 N6 配套决策 |

---

## 五、可落地的 Seelex 下一步（按方向，标注优先级与工作量）

> 工作量口径：S=1–3 天，M=1–2 周，L=1 个月以上。优先级综合"竞争差距 + 用户偏好 + 与试点关系"。

### 方向 A：记忆 / 长期上下文
| # | 下一步 | 优先级 | 工作量 | 依据 |
|---|---|---|---|---|
| A1 | 跨会话事实记忆落库：decisions/findings/constraints 按 session 持久化，新会话按相关性注入（复用 ContextSnapshot 字段 + sessionstore，无需向量化即可起步） | 中 | M | 竞品 2026 标配；既有报告 P1-5；与试点跨会话需求直接相关 |
| A2 | 记忆自动提炼试点：会话结束自动提炼 MEMORY.md 条目，**先走 Gemini 式 inbox 人工确认制**（防自动污染，与 Claude 自动写入路线对比后再定） | 中 | M | Gemini Auto Memory 设计原则；已知 Claude Auto Memory 的 token 开销与 shadow state 问题 |
| A3 | memory 向量化 v2 落地（`docs/plan/memory-architecture.md`：graph role + 私有 Retriever） | 中 | L | 设计已冻结；依赖 graph role 账号配置 |
| A4 | AGENTS.md/CLAUDE.md 惯例文件消费（生态兼容，读入 ProjectKnowledge） | 低 | S | 既有报告 P2-11 |

### 方向 B：工作台可视化
| # | 下一步 | 优先级 | 工作量 | 依据 |
|---|---|---|---|---|
| B1 | **SubAgentTree 可见性接通**（forkexec 生命周期事件已有；TUI/GUI 树形渲染 + 节点 token/耗时/工具列表） | **高** | M | 既有报告 P0-3；有设计文档未落地；试点多节点运行的可视化前提 |
| B2 | 试点阶段视图：tasklist 打点数据聚合为「需求/设计/编码/测试/验证/交付」里程碑徽章（对齐 Devin todo/OpenHands Task List） | 中 | S–M | task_check_node 打点已存在，纯前端聚合 |
| B3 | 会话级 checkpoint（快照点 + 回滚入口，先会话/上下文级、再 git 级） | **高** | M–L | 竞品 2026 标配；既有 P0；试点失败回环的"撤销"需求 |
| B4 | Plan 节点依赖图可视化（渲染 DAG 边，对齐 Devin plan 视图 / Cursor 卡片） | 低 | L | GUI 已有 Plan 面板，增量演化 |

### 方向 C：定时 / 周期任务
| # | 下一步 | 优先级 | 工作量 | 依据 |
|---|---|---|---|---|
| C1 | **headless 非交互执行模式**（`seelex -p` 式单次执行 + `--max-loops/--budget-usd/--allowed-tools` 护栏参数） | **高** | M | 试点的"无人值守执行"前置；Claude/Codex/Gemini 全部具备；已有 Effort 预算机制可复用 |
| C2 | 会话级 cron 工具（/loop 式：会话内注册周期提醒，复用现有 task 机制，7 天过期） | 中 | M | Claude Code 会话内 cron；成本低、见效快 |
| C3 | 系统级 scheduled runs（cron 表达式 + 独立会话 + 结果评审队列，对齐 Codex Automations / OpenHands Automations） | 中 | L | 依赖 C1；试点成功后转周期任务的承接形态 |
| C4 | 事件触发（GitHub webhook / PR 事件触发会话，对齐 Copilot/Devin Automations） | 低 | L | 依赖 C3；需对外端口 |

### 方向 D：自迭代闭环（本次重点）
| # | 下一步 | 优先级 | 工作量 | 依据 |
|---|---|---|---|---|
| D1 | **「需求→coding→test→product」自迭代试点**（详见 `plan-iterative-product-pilot.md`：需求节点产编号验收标准 → 设计 → 人工门 → 编码 → 测试 → verify 真实命令验证 → 有界失败回环 → 独立评审 → 交付物） | **高（用户首选）** | M | 竞品共识闭环骨架；seelex Plan DAG 已具备大部分环节；失败模式有行业共识护栏可抄 |
| D2 | **verify 验证门强化**：verify 节点接真实测试命令 + 失败输出结构化回传 + 有界重试（Effort 预算内），测试证据（测试文件 + exit code）作为完成唯一权威 | **高** | S–M | "exit code 为适应度、不用 LLM 当裁判"是全部竞品共识；D1 的核心机制 |
| D3 | 独立评审节点：review agent（独立上下文/独立账号，审代码与测试质量，输出结构化 verdict） | 中 | M | "写者不给自己打分"；子代理机制现成，定义 review 节点输入输出契约即可 |
| D4 | 交付物协议：deliver 节点输出契约（变更摘要/测试结果/PR 描述 markdown），git commit 封装（不自动 push） | 中 | S | Devin/OpenHands 自动 PR 的本地化第一步；人工生产门保留 |
| D5 | 失败分类护栏：verify 失败先重跑确认，区分真 bug 与 flaky（对齐 Claude AutoFix 教训：flaky 误诊导致 cargo-cult 修复与无限重跑） | 中 | S | 行业已知失败模式；D2 的回环必须带分类 |

### 推荐 Top3
1. **D1 自迭代试点（高 / M）**——用户明确倾向；seelex 骨架最接近竞品共识闭环；试点是"多方向能力的自然牵引器"（拉动 C1、B2、D2–D5）。
2. **D2 verify 验证门 + 有界失败回环（高 / S–M）**——试点核心机制，也是任何闭环的信任基础；改动面小（复用 verify 节点与 Effort 预算）。
3. **B1 SubAgentTree 可见性 + B3 会话级 checkpoint（高 / M–L）**——试点运行的可视化与安全网；既有 P0 项，2026 年竞品全部标配。

---

## 六、来源索引（精选，按产品）

- Claude Code：Auto Memory/Auto Dream（[InfoQ](https://www.infoq.cn/article/QTPQmPZ1DsSBjTTymgK3)、[SFEIR](https://institute.sfeir.com/en/articles/claude-code-dream-auto-dream-memory-consolidation/)）、1M context（[官方 blog](https://claude.com/blog/1m-context-ga)）、Agent View（[官方 blog](https://claude.com/blog/agent-view-in-claude-code)）、checkpointing（[官方文档](https://code.claude.com/docs/en/checkpointing)、[SDK file-checkpointing](https://code.claude.com/docs/en/agent-sdk/file-checkpointing)）、Routines（[官方 blog](https://claude.com/blog/introducing-routines-in-claude-code)、[文档](https://code.claude.com/docs/en/routines)）、Desktop 定时任务（[文档](https://code.claude.com/docs/en/desktop-scheduled-tasks)）、Dynamic Workflows（[官方 blog](https://claude.com/blog/introducing-dynamic-workflows-in-claude-code)、[文档](https://code.claude.com/docs/en/workflows)）、Fable 5（[The Hacker News](https://thehackernews.com/2026/06/anthropic-releases-claude-fable-5-its.html?m=1)）、Long-Running Agent Harness（[ZenML](https://www.zenml.io/llmops-database/long-running-agent-harness-for-multi-context-software-development)）、Preview/Review/Merge（[官方 blog](https://claude.com/blog/preview-review-and-merge-with-claude-code)）
- Cursor：2.0/3.0（[dev.to 8-agent](https://dev.to/jangwook_kim_e31e7291ad98/cursor-20-8-parallel-ai-agents-and-visual-editor-bridge-50nk)、[3.0 changelog](https://cursor.ac.cn/changelog/3-0)、[AgentMarketCap](https://agentmarketcap.ai/blog/2026/04/08/cursor-3-agent-first-ide-parallel-ai-fleets-anysphere)）、checkpoints 回归（[官方帮助](https://cursor.com/help/troubleshooting/agent-issues.md)、[论坛](https://forum.cursor.com/t/cursor-3-where-are-the-checkpoints/157319/3)）、索引移除（[Bito](https://bito.ai/blog/how-cursors-codebase-indexing-works-2026-guide/)、[ZenML](https://www.zenml.io/llmops-database/fast-regex-search-indexing-for-ai-agent-tool-performance)）
- Windsurf/Devin：Windsurf 2.0（[devin.ai blog](https://devin.ai/blog/windsurf-2-0)）、Devin Desktop（[devin.ai blog](https://devin.ai/blog/windsurf-is-now-devin-desktop)）、recurring scheduled tasks（[ai-primer](https://www.ai-primer.com/engineer/stories/devin-recurring-scheduled-tasks)）、Managed Devins（[ai-primer](https://www.ai-primer.com/engineer/stories/devin-managed-devins)）、Auto-Triage（[ai-primer](https://www.ai-primer.com/engineer/stories/devin-auto-triage-long-term-memory)）、Workflows 定义（[Devin 文档](https://docs.devinenterprise.com/desktop/cascade/workflows)）
- Copilot：CLI GA（[GitHub changelog](https://github.blog/changelog/2026-02-25-github-copilot-cli-is-now-generally-available/)）、Code Review agentic（[changelog](https://github.blog/changelog/2026-03-05-copilot-code-review-now-runs-on-an-agentic-architecture/)）、Fix with Copilot（[changelog](https://github.blog/changelog/2026-05-19-easily-apply-copilot-code-review-feedback-with-copilot-cloud-agent/)）、Automations（[changelog](https://github.blog/changelog/2026-06-02-schedule-and-automate-tasks-with-copilot-cloud-agent/)）
- OpenHands：v1.5.0（[newreleases](https://newreleases.io/project/github/OpenHands/OpenHands/release/1.5.0)）、Agent Canvas/ACP（[官方博客](https://www.openhands.dev/blog/use-any-coding-agent-in-openhands-with-acp)）、Control Plane（[TMCnet](https://www.tmcnet.com/usubmit/2026/05/06/10378085.htm)）、Automations（[官方文档](https://docs.openhands.dev/openhands/usage/automations/overview)）、PR 创建（[DeepWiki](https://deepwiki.com/All-Hands-AI/OpenHands/14.2-pull-request-creation-and-patch-management)）
- Gemini CLI：Auto Memory（[官方文档](https://geminicli.com/docs/cli/auto-memory/)、[PR #25601](https://github.com/google-gemini/gemini-cli/pull/25601)）、SandboxManager（[v0.36.0](https://github.com/google-gemini/gemini-cli/pull/24558)）、subagents（[PR #17567](https://github.com/google-gemini/gemini-cli/pull/17567)）、A2A server（[DeepWiki](https://deepwiki.com/google-gemini/gemini-cli/5.8-a2a-server-and-agent-protocol)）、官方 cron 示例（[workflow](https://github.com/google-gemini/gemini-cli/blob/ea870843ec4dc41a456fd3569cc2ee0e17fbfab1/.github/workflows/gemini-scheduled-issue-triage.yml)）
- Codex：Plan mode（[PR #10195](https://github.com/openai/codex/pull/10195)）、Automations GA（[ai-primer](https://www.ai-primer.com/engineer/stories/openai-codex-automations-ga)）、Workspace Agents（[SiliconANGLE](https://siliconangle.com/2026/04/22/openai-subscribers-get-new-workspace-agents-automate-complex-tasks-across-teams/)）、Goal Mode（[evezone](https://evezone.evetech.co.za/daily-drop/codex-cli-goal-mode-goes-ga-how-the-rust-rewritten-agent-s-persistent-goal-sessions-change)）、macOS 桌面（[MacRumors](https://www.macrumors.com/2026/04/16/openai-codex-mac-update/)）
- Factory/Replit：Factory droid 评测（[dev.to](https://dev.to/pickuma/factory-ai-droids-review-how-far-autonomous-coding-agents-have-come-in-2026-4m00)、[Security Review](https://docs.factory.ai/enterprise/security-review)）、Replit Agent 4（[官方 blog](https://replit.com/blog/introducing-agent-4-built-for-creativity)、[App Testing](https://docs.replit.com/features/agent/app-testing)、[SaaStr 访谈](https://www.saastr.com/amjad-masad-and-me-at-saastr-ai-2026-the-agents-we-actually-built-and-what-replits-founder-thinks-comes-next/)）
- 失败模式/基准：Compaction epistemic failure（[arXiv:2607.13071](https://arxiv-org.ezproxy.obspm.fr/html/2607.13071v1)）、AutoFix flaky（[dev.to](https://dev.to/byteframe/why-claude-code-autofix-cant-fix-flaky-tests-e6d)）、context rot（[LOCA-bench 汇总](https://github.com/edwinidrus/Context-lens)）、DeepSWE 审计（[zencoder](https://zencoder.ai/blog/20k-bug-that-changed-evals)）、SWE-bench 2026 更新（[Simon Willison](https://simonwillison.net/2026/Feb/19/swe-bench/)）、12% 回归率（[TestSprite](https://siliconangle.com/2026/06/11/testsprite-launches-open-source-command-line-tool-help-ai-agents-check-work/)）
- 行业：Nylas 2026 State of Agentic AI（[BusinessWire](https://www.businesswire.com/news/home/20260218088523/en/94-of-Developers-Would-Switch-Vendors-as-Agentic-AI-Triggers-Infrastructure-Race)）、Gartner 2026 云原生 MQ（[Virtualization Review](https://virtualizationreview.com/articles/2026/08/05/ai-agents-move-to-the-center-of-gartners-2026-cloud-native-magic-quadrant.aspx)）、HFS Horizons 2026（[HFS](https://www.hfsresearch.com/research/hfs-horizons-agentic-technology-2026/)）

> 说明：以上为三个并行调研批次（2026-08-06）的检索结果精选；Seelex 现状证据见 `docs/research/coding-agent-harness-comparison.md` 第四节与 `docs/feature-instrumentation.md`。
