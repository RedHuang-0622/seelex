# Seelex 文档索引

> 文档按长期维护设计、研发过程和研究资料组织。GUI 的当前事实以 `gui/` 目录为准。

---

文档创建与放置规则以根目录 [`AGENTS.md`](../AGENTS.md) 为准；分类目录的维护约定见 [`arch/README.md`](arch/README.md)、[`product/README.md`](product/README.md)、[`research/README.md`](research/README.md)、[`test/README.md`](test/README.md) 和 [`devlog/README.md`](devlog/README.md)。

## 📐 arch/ — 架构与设计

| 文档 | 说明 |
|------|------|
| [`seele-v2-runtime-architecture.md`](arch/seele-v2-runtime-architecture.md) | Seelex 使用 Seele v0.1.1 远程模块边界的稳定架构；迁移历史见 [`2026-08-01-seele-v2-underlying-refactor`](2026-08-01-seele-v2-underlying-refactor/plan.md) |
| [`architecture-and-flaws.md`](arch/architecture-and-flaws.md) | 架构说明书与已知硬伤清单（初稿） |
| [`design-decisions-mcp-storage.md`](arch/design-decisions-mcp-storage.md) | MCP 中间件从 CAD 专属→通用→存储解耦的设计推演 |
| [`mcp-call-chain-flowchart.md`](arch/mcp-call-chain-flowchart.md) | Agent 调用 MCP 全链路函数流 + 熔断事件通道 |
| [`agent-permission-subjects.md`](arch/agent-permission-subjects.md) | 工具权责模型：主体（root/sub/emp_ro/emp_rw）× 路由组（ro/rw/rw_session/rw_desktop/ctl/adm）× 位（rwx）+ sudo；求值顺序、错误语义、当前实现与未落地部分 |
| [`context-improvement-plan.md`](arch/context-improvement-plan.md) | Context 包拆分为 snapshot/provider/compactor/merger 方案 |
| [`skill-effort-architecture.md`](arch/skill-effort-architecture.md) | Effort system prompt 与 Skill 用户上下文的当前实现设计 |
| [`agent-workbench-architecture.md`](arch/agent-workbench-architecture.md) | DSL 对话卡片、Agent E2E、Workspace 沙盒与多会话并行总体架构 |
| [`agent-team-work-vs-market.md`](arch/agent-team-work-vs-market.md) | Agent Team「team work」与市场常见多代理方案的机制差异、收益与数据依据（含本次实测/静态度量/既有报告/外部未复核口径的分层标注与诚实局限清单） |
| [`agent-team-seat-vs-claim.md`](arch/agent-team-seat-vs-claim.md) | 席位制 team work 与认领式 teamwork（任务池/认领/并行扇出）的优势、代价与扬长避短；含作品集叙事骨架、演示脚本、主张↔证据映射与未验证清单 |
| [`agent-team-phase2-and-goal-vs-vmodel.md`](arch/agent-team-phase2-and-goal-vs-vmodel.md) | Agent Team 第二阶段反思：市面对照、现状诊断（带代码锚点）、改动清单，与 goal / V 模型的范畴澄清；含待商榷 fork（讨论输入，未改代码） |
| [`teamwork-leader-worker-architecture.md`](arch/teamwork-leader-worker-architecture.md) | Teamwork 目标架构与里程碑：leader + 异步 worker（子进程工具调用范式）；Seele 新增 `jobs` 契约+Manager+`jobs_manage`、Seelex 提供 Executor/worktree(git)、`moduleTeamwork` 硬编排存储、teammate 约束与「回收作业+释放 worktree+删内容保在线」、job 作用域 `Scope{Session,Subject}`；含 M0–M4 详表与死代码清单（目标设计，未改代码） |

## 🧭 product/ — 产品规划

| 文档 | 说明 |
|------|------|
| [`agent-workbench/prd.json`](product/agent-workbench/prd.json) | Agent Workbench 机器可读 PRD、里程碑、验收标准与指标 |
| [`pmstory.md`](product/pmstory.md) | 基于历史文档的决策叙事：各阶段产品/工程决策的权衡、代价与复盘 |

## GUI — Wails 客户端设计与审查

| 文档 | 说明 |
|------|------|
| [`gui/README.md`](gui/README.md) | GUI 架构总览、模块边界和维护规则 |
| [`gui/architecture.md`](gui/architecture.md) | Agent Workbench 权威总体架构、并发与 generation 发布模型 |
| [`gui/module_dotting.json`](gui/module_dotting.json) | 模块职责、状态、接口、输入输出和依赖 DAG |
| [`gui/schemas/`](gui/schemas/) | 对外及跨模块 JSON Schema 契约与维护规则 |
| [`gui/api/`](gui/api/) | 规划 HTTP API、安全、分页、错误和快照语义 |
| [`gui/recipes/`](gui/recipes/) | generation 提交、回滚、重建与故障恢复 |
| [`gui/architecture-review.md`](gui/architecture-review.md) | 并发、循环依赖、模块边界与解耦审查 |
| [`gui/decisions.md`](gui/decisions.md) | Wails、协议、reducer、keyed DOM、Markdown 和 CI 决策记录 |
| [`gui/ci-and-testing.md`](gui/ci-and-testing.md) | GUI 分支 CI、测试分层和本地等价命令 |
| [`gui/code-review.md`](gui/code-review.md) | 功能打点到详设、源码位置、测试证据的审查追溯矩阵 |
| [`gui/modules/shell-and-interactions.md`](gui/modules/shell-and-interactions.md) | 外壳布局、左右栏收起、焦点/按压态、共享提示气泡与快捷键 |
| [`gui/modules/right-sidebar.md`](gui/modules/right-sidebar.md) | 右栏 Files/Changes/Artifacts/索引/状态子页；提交记录与工作树走 tree-fork 树轨与泳道 |
| [`gui/modules/dsl-card-runtime.md`](gui/modules/dsl-card-runtime.md) | JSON DSL 卡片在 Conversation 中的协议、渲染与安全设计 |
| [`gui/modules/agent-e2e-interaction.md`](gui/modules/agent-e2e-interaction.md) | 确定性 Core scenario、Playwright 与 Wails smoke 设计 |
| [`gui/modules/workspace-sandbox.md`](gui/modules/workspace-sandbox.md) | 右栏 Files/Changes/Artifacts 与后端路径沙盒设计 |
| [`gui/modules/multi-session-pages.md`](gui/modules/multi-session-pages.md) | 多会话页签、独立 SessionActor、有界并发与后台状态设计 |

## 📓 devlog/ — 研发过程

| 文档 | 说明 |
|------|------|
| [`test-report.md`](devlog/test-report.md) | 测试报告（已更新至 7ed72fb） |
| [`finish-review.md`](devlog/finish-review.md) | 机械设计方向最终审查 + 后续重构更新 |
| [`code-changes.md`](devlog/code-changes.md) | 代码变更摘要（2026-07-17） |
| [`CODE_EVALUATION_REPORT.md`](devlog/CODE_EVALUATION_REPORT.md) | 一次性代码评估报告 |
| [`2026-07-28-finish-review.md`](devlog/2026-07-28-finish-review.md) | 整体项目审查报告（不通过生产发布；Alpha 内测前置条件） |
| [`2026-08-05-code-changes.md`](devlog/2026-08-05-code-changes.md) | Session 存储层模块化变更摘要 |
| [`README-dev.md`](devlog/README-dev.md) | 开发态 GUI 包说明 |
| [`2026-07-17-seelex-runtime-plugin-refactor-front-review.md`](devlog/2026-07-17-seelex-runtime-plugin-refactor-front-review.md) | Plugin 重构前置审查 |
| [`2026-07-17-tui-application-core-separation-front-review.md`](devlog/2026-07-17-tui-application-core-separation-front-review.md) | TUI/Application 分离前置审查 |
| [`2026-07-17-tui-application-core-separation-plan.md`](devlog/2026-07-17-tui-application-core-separation-plan.md) | TUI 分离实施方案 |
| [`2026-09-15-team-work-computer-use.md`](devlog/2026-09-15-team-work-computer-use.md) | team work 接入 computer use 证据面（截图句柄/尺寸/前台窗口进 ADVISOR 输入）+ 真实 API 真机探针 |
| [`2026-09-15-agentteam-instantiation-and-speak-schedule.md`](devlog/2026-09-15-agentteam-instantiation-and-speak-schedule.md) | Agent Team 员工一步实例化 + 发言调度运行态（含治理轮次上限与前端两栏） |
| [`2026-09-15-agentteam-panel-optimization.md`](devlog/2026-09-15-agentteam-panel-optimization.md) | Agent Team 面板与相邻面板的可优化之处：只读审计（带证据）+ 行业对照 + 分批建议；含员工面板去小字备注的落地记录 |
| [`2026-09-15-frontend-render-and-memory.md`](devlog/2026-09-15-frontend-render-and-memory.md) | 前端渲染与内存减负（事件委托 / 共享提示气泡 / 拓扑单传 / tree-fork 统一树轨与泳道） |
| [`2026-09-16-frontend-tables-and-chip-dropdown.md`](devlog/2026-09-16-frontend-tables-and-chip-dropdown.md) | Goal 活动栈与角色记录改专用表格（一行一帧 / 一行一车道一列一回合）、权限档 chip 就地下拉切换、Dev GUI 包归档根目录修正 |

## 🔬 research/ — 调研报告

| 文档 | 说明 |
|------|------|
| [`agent-frontend-design-research.md`](research/agent-frontend-design-research.md) | AI Agent 前端界面 + DSL 卡片渲染设计调研 |
| [`codex-session-resume.md`](research/codex-session-resume.md) | Codex 会话恢复与 rollout 有序存储调研（append-only JSONL、单 writer、前缀重放） |
| [`approve-research.md`](research/approve-research.md) | Approve 节点选型（OpenCode vs Claude Code vs Seele） |
| [`context-management-review.md`](research/context-management-review.md) | 上下文管理（继承/合并/压缩）实现审查与理论依据调研 |
| [`2026-09-14-seelex-thesis-proposal-v3.md`](research/2026-09-14-seelex-thesis-proposal-v3.md) | 开题报告 v3：面向软件工程任务的 Coding Agent Harness（v2 基线 + 增量章） |

## 📊 根目录

| 文档 | 说明 |
|------|------|
| [`feature-instrumentation.md`](feature-instrumentation.md) | 功能打点表与北极星指标 |

## 📦 一次性工作包（YYYY-MM-DD-topic）

| 文档 | 说明 |
|------|------|
| [`2026-08-22-full-code-review/README.md`](2026-08-22-full-code-review/README.md) | 全仓库分层代码审阅：架构字符画、数据流图、设计决策解读、Tech Leader 质询与解答 |
| [`2026-08-22-application-split/design.md`](2026-08-22-application-split/design.md) | application 容器化重构设计（合约净化、adapters 归位、core 域包化） |
| [`2026-08-23-worktable-sharding-filetree/README.md`](2026-08-23-worktable-sharding-filetree/README.md) | worktable 多维分片 + 工作台文件树工作包（plan + 打点表） |
| [`2026-08-14-decoupling/00-index.md`](2026-08-14-decoupling/00-index.md) | 解耦重构系列文档索引 |
| [`2026-09-07-session-order-log/README.md`](2026-09-07-session-order-log/README.md) | 会话全序日志（Session Order Log）设计与实施路线：现状审查、Codex 差距、按顺序存储、前缀 + 追加发送治理、恢复重放 |
| [`2026-09-08-context-restore-review/README.md`](2026-09-08-context-restore-review/README.md) | 会话上下文管理/存储/恢复现状描述与复现：恢复后首轮上下文乱序/重复/丢前缀、超预算收缩把最新轮整段裁掉、工具轮 LLM 输出/草稿持久化缺口（均已修复转绿；pprof 无死锁/泄漏/热点；请求顺序↔存储匹配通过，记录 hash 恢复前后一致；最新修复未提交） |
| [`2026-09-08-session-rollout-p2/README.md`](2026-09-08-session-rollout-p2/README.md) | Session Rollout 全序模型（历史设计）：rollout.jsonl 契约/双写/崩溃续写、生命周期 kind、resume 重放切换、P3 发送治理契约；JSON 旧链路已删除并切到 message 事件行 + 模块 head 布局，rollout 实现不再存在（文档仅作历史归档）；SQLite/Redis 等其它后端为后续项 |
| [`2026-09-08-session-storage-architecture/README.md`](2026-09-08-session-storage-architecture/README.md) | 会话存储新架构总览：message 事件行 + metadata 模块化 head、EVENT/compact、历史分页/wire 装配/续跑读取器、lifecycle（draft/queue）、fork session/subagent、LRU/retention/检索/blob GC（M1–M4 已实现并接线为唯一 JSON 链路；my_design.md 为权威明细；impact-map.md 为波及面盘点；implementation-M1-M4.md 为实施记录；benchmarks/ 含新链路优越性、冷热切换 pprof 与性能量化成果专档） |
| [`2026-09-08-interrupted-round-truncation/README.md`](2026-09-08-interrupted-round-truncation/README.md) | 中断轮截断策略更改：残缺工具链轮保留为开放单元、断点续扫不跳 next user；装配层补齐缺失 tool 结果（合成占位、provider-only、幂等）；重启 continue 续跑端到端（三处同构切分 + 装配 seam；字符画表达） |
| [`2026-09-15-agent-permission-routing-groups/README.md`](2026-09-15-agent-permission-routing-groups/README.md) | Agent 权限分封：路由组式 Linux 权限模型（主体×组×位+sudo）设计与对 Seele 框架的需求单 R1–R8；落地差异见同目录 `seele-integration-status.md`，长期口径见 [`arch/agent-permission-subjects.md`](arch/agent-permission-subjects.md) |

## 🔬 调研报告

| 文档 | 说明 |
|------|------|
| [`research/2026-08-23-file-content-preview.md`](research/2026-08-23-file-content-preview.md) | 文件内容详情查看（File Preview）方案调研 |
| [`research/codex-session-resume.md`](research/codex-session-resume.md) | Codex 会话恢复与 rollout 有序存储调研（append-only JSONL、单 writer、前缀重放） |
