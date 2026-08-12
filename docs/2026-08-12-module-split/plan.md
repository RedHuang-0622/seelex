# Seelex 模块化拆分与根目录整理方案

日期：2026-08-12
状态：方案（已部分实施——P1 全部完成，P2 完成零耦合子包：security/fs/plan/tools-websearch + internal/model；P2 高耦合与 P3/P4 待后续阶段）
范围：仓库根目录瘦身 + `seelebridge` 子包分层
前置说明：原方案只做盘点与目标设计；2026-08-12 经确认后按阶段执行。执行记录见本文件末尾。

## 1. 背景与目标

架构 review 指出 seelebridge 过于扁平；根目录盘点发现大量本应属于模块包的代码/文档被直接放在仓库根。本方案的目标：

- 根目录只保留主入口、装配、仓库入口文档与配置入口；
- seelebridge 按领域拆成子包，根包退化为组合根 + 公共 facade；
- 所有移动遵循"依赖单向、无环、公共 API 兼容"三条约束；
- 分阶段执行，每阶段可独立验证、可回退。

## 2. 根目录现状盘点

### 2.1 应移入模块包的源码（当前均为 package main）

| 文件 | 现状 | 目标位置 | 动作 |
|---|---|---|---|
| `application_adapters.go` | main 包装配胶水（38KB） | `application/adapters/` | 抽成独立包，main.go 引用 |
| `backend_console.go` | main 包控制台后端（10KB） | `application/console/` | 抽包 |
| `mcpconfig.go` | main 包 MCP 配置加载（3KB） | `mcpstack/config/` | 抽包 |
| `websearch.go` | main 包 web 搜索工具（3KB） | `seelebridge/tools/websearch/` | 抽包（或迁 plugins/） |
| `version.go` | main 包版本信息（构建注入） | `internal/buildinfo/` | 抽包 |

### 2.2 应移入 e2e 的根级测试（均为 package main）

| 文件 | 目标位置 |
|---|---|
| `bootstrap_test.go`、`layout_test.go`、`governance_test.go`、`release_test.go` | `e2e/` |
| `smoke_test.go`、`manual_smoke_test.go`、`context_long_live_test.go` | `e2e/` |
| `tool_full_chain_test.go`、`tool_full_chain_live_test.go` | `e2e/` |
| `docs_contract_test.go` | `e2e/`（或根保留为仓库契约测试） |

`e2e/` 已有 scenario harness（harness/loader/ports/recorder/runner/schema/scripted_engine + fixtures），根级集成测试应迁入并复用。

### 2.3 应移入 docs 的根级文档

| 文件 | 目标位置 | 说明 |
|---|---|---|
| `ARCHITECTURE_REVIEW.md` | `docs/arch/` | 标注"历史/待刷新"，与 H1-H28 假设清单一致 |
| `CODE_EVALUATION_REPORT.md` | `docs/devlog/` | 一次性代码评估 |
| `finish-review.md` | `docs/devlog/` | 项目审核报告 |
| `code-changes.md` | `docs/devlog/` | 变更摘要 |
| `plan.md` | `docs/arch/` | 上下文/Skill/Plugin 实现方案 |
| `test-report.md` | `docs/test/` | 测试报告 |
| `README-dev.md` | `docs/devlog/` | dev GUI 包说明 |

保留在根：`README.md`、`README_EN.md`、`CHANGELOG.md`、`LICENSE`、`SECURITY.md`、`CODE_OF_CONDUCT.md`、`CONTRIBUTING.md`、`AGENTS.md`、`Makefile`。

### 2.4 配置与生成物

| 文件 | 现状 | 建议 |
|---|---|---|
| `seele.yaml`、`seelex.yaml` | 根目录运行配置 | 迁 `config/`，同步修改 main.go 默认加载路径（或保留根目录作为配置入口，二选一，需确认） |
| `coverage.out`（14MB）、`coverage-summary.txt` | 生成物（已 gitignore） | 清理，不进库 |
| `debug.log`、`tmux-client-547.log`、`tmux-server-550.log` | 日志（已 gitignore） | 清理 |
| `opt/`、`tmp/` | 本地杂物（未跟踪） | 清理或 gitignore |
| `local/` | 本地个人目录（已 gitignore） | 保留 |
| `go.work`、`go.work.sum` | 已 gitignore | 保留（不参与提交） |

## 3. seelebridge 现状盘点（44 个非测试文件）

### 3.1 零 `*Runtime` 方法——可先纯搬移

`plan_executor.go`、`plan_policy.go`、`plan_preflight.go`、`plan_input_adapter.go`、`plan_tool_provider.go`、`plan_events.go`、`plan_authority.go`、`replan_guard.go`、`worktree_manager.go`、`subagent_context.go`、`subagent_sessions.go`、`task_registry.go`、`task_terminal.go`、`filesystem_actor.go`、`project_scope.go`、`pathgate.go`、`sandbox.go`、`config.go`、`storage.go`、`stream_completer.go`、`trace.go`、`node_scope.go`、`telemetry_diagnostic.go`、`command_*.go`

### 3.2 高耦合 `*Runtime` 方法——需先组件化再搬

| 文件 | 方法数 | 目标域 |
|---|---|---|
| `runtime.go` | 46 | 根 facade（保留） |
| `context_components.go` | 17 | 根装配（保留或拆 runtime/internal） |
| `agent_node.go` | 14 | `node/` |
| `mcp.go` | 13 | `mcp/` |
| `scoped_tools.go` | 11 | `tools/` |
| `todo_tool.go` | 10 | `task/` |
| `task_registry.go`（facade 方法） | 8 | `task/` |
| `scheduler.go` | 7 | `mcp/` 或 `scheduler/` |
| `fork_tool.go` | 6 | `fork/` |
| `actor.go` | 6 | 根 facade（mailbox 委托） |
| `branch.go` | 5 | `node/` |
| `plugins.go` | 5 | `plugin/` |
| `worktree.go` | 4 | `worktree/` |

## 4. 目标结构

### 4.1 根目录目标布局

```text
seelex/
  main.go / main_unix.go / main_windows.go   # 主入口（唯一 package main）
  Makefile / go.mod / go.sum / AGENTS.md / README.md / ...
  application/        # 应用层（含 adapters/、console/）
  seelebridge/        # 组合根 + 公共 facade（瘦身）
  seelexctx/  session/  sessionstore/  workspace/  skill/  plugin/  plugins/
  mcpstack/  internal/  gui/  tui/  e2e/
  config/             # accounts*.yaml + seele.yaml + seelex.yaml
  docs/               # arch/ product/ gui/ research/ test/ devlog/ swebench/ YYYY-MM-DD-*/
  scripts/  dist/  local/
```

### 4.2 seelebridge 子包树

```text
seelebridge/
  runtime.go                 # 组合根：装配 + 公共 facade + 工具注册（瘦身）
  accounts.go  actor.go      # 根级薄壳（委托到子包）
  node/                      # 节点执行域：agent_node、node_scope、node_tool_result、branch
  plan/                      # plan 域：executor/ provider/ policy/ preflight/ input/ events/ replan
  fork/                      # fork_subagents 工具 + fork DAG 构造 + 并发编排
  worktree/                  # worktreeManager（已组件化，直接搬）
  task/                      # taskRegistry、todo_tool、task_terminal
  session/                   # subagentSessions、subagentContext、subagentTree、subagentEvents
  tools/                     # registry、scoped_tools、permission、websearch
  security/                  # project_scope、pathgate、sandbox
  fs/                        # filesystem_actor
  mcp/                       # mcp.go、plugins、scheduler
```

### 4.3 依赖规则

- 方向：根 facade → 各域；域之间只经接口/只依赖下层域；
- **域组件禁止 import seelebridge 根包**（否则成环）；
- 共享类型（TaskRecord、PlanBranchBinding、NodeScope 等）下沉到所属域，或建 `seelebridge/internal/model/` 类型层；
- 高耦合 `*Runtime` 方法一律转成组件方法（deps 闭包/接口注入），复用 planExecutor/worktreeManager 的既有模式；
- 外部调用面（main.go、application）只依赖根 facade，拆包期间用 `type Alias = internal.X` 保持兼容。

## 5. 执行阶段

| 阶段 | 内容 | 风险 | 验证 |
|---|---|---|---|
| P0 | 本方案确认 + 存档（提交远程） | 无 | 评审 |
| P1 | 根目录整理：docs/config/e2e 纯搬移 + 杂物清理 + gitignore 补漏 | 低 | `git mv` 保留历史；`go test ./...` |
| P2 | seelebridge 零耦合文件搬入子包 + 根包 alias 兼容 | 低-中 | 每包 `go test`；`go build ./...` |
| P3 | 高耦合组件化：agent_node → fork_tool → scoped_tools → todo_tool → mcp → scheduler，逐个闭环 | 高 | 每转一个跑全绿 + 对应冒烟 |
| P4 | 根 facade 瘦身、main.go 装配瘦身、README/契约测试同步、全量测试 + live smoke + rebuild | 中 | `make rebuild-gui` + `SEELEX_LIVE_SMOKE=1` |

## 6. 风险与对策

- **`*Runtime` 方法不能跨包**：物理搬移前必须先组件化，否则编译失败——P2/P3 顺序不可调换；
- **import 环**：域组件禁引根包是硬规则，发现即拆接口；
- **公共 API 兼容**：根包 alias 保证 main.go/application 尽量零改动；
- **测试搬迁**：`_test.go` 随源码走；根级集成测试迁 e2e 并复用 scenario harness；fixture 路径同步；
- **git 历史**：一律 `git mv`，不做 copy+delete；
- **配置路径**：seelex.yaml 迁 config/ 需同步 main.go 加载路径，或用"根目录保留 + config/ 软链"过渡。

## 7. 明确不做（本方案之外）

- 不实施假账号 fallback 移除、`application/contract` 依赖反转、main.go 领域逻辑外迁（列为独立任务）；
- 不改 Go module 名、不拆 application 内部结构（另立方案）；
- 不调整文档放置规范与模块 README 要求（AGENTS.md 继续生效）。


---

## 执行记录（2026-08-12）

### P1 根目录整理 —— 完成 ✅

- 文档搬移：`ARCHITECTURE_REVIEW.md` → `docs/arch/`（标注历史待刷新）、`CODE_EVALUATION_REPORT.md` → `docs/devlog/`、`finish-review.md` → `docs/devlog/2026-07-28-finish-review.md`、`code-changes.md` → `docs/devlog/2026-08-05-code-changes.md`、`plan.md` → `docs/arch/plan.md`、`test-report.md` → `docs/test/2026-07-27-test-report.md`、`README-dev.md` → `docs/devlog/`。
- 配置迁移：`seele.yaml`/`seelex.yaml` → `config/`；`main.go` 增加 `firstExisting`（优先 config/ 回退根目录）；Makefile 与 release.yml 打包路径同步。
- 测试迁移：`bootstrap_test.go`/`governance_test.go`/`layout_test.go`/`docs_contract_test.go` → `e2e/`（package e2e + repoRoot 相对路径修正）；后续（2026-08-12 第二轮）`release_test.go` 中不依赖 main 符号的发布/构建契约测试 → `e2e/release_contract_test.go`。
- 源码抽包：`version.go` → `internal/buildinfo/`；`websearch.go` → `seelebridge/tools/websearch/`（窄接口 `ToolRegistrar`）；`mcpconfig.go` → `mcpstack/config/`（只留加载，注册留 main）；`backend_console.go` → `application/console/`；`application_adapters.go` → `application/adapters/`（类型导出 EnginePort/RuntimePort 等）。
- 新增模块 README：adapters、console、buildinfo、mcpstack/config、websearch。

### P2 seelebridge 零耦合子包 —— 部分完成 ✅/⏳

- ✅ `security/`：`project_scope.go`、`pathgate.go`（+ 测试）；`ProjectScope.Relative`/`ResolveInside` 随包导出。
- ✅ `security/`（2026-08-12 第二轮）：`sandbox.go` + `command_{windows,other}.go`（+ 测试）迁入；
  `CommandSandbox`/`SandboxCapabilities` 保留，`ScrubEnvironment`/`FileExists`/`ConfigureHiddenCommand`/
  `NewNativeProjectCWD` 导出；根包 runtime/scoped_tools/scheduler/docker/worktree_manager 改经 security 引用；
  根包 `security_aliases.go` 重导出。
- ✅ `fs/`：`filesystem_actor.go`（+ 测试）。
- ✅ `plan/`：`graph.go`（PlanEdge/AdjacencyToEdges/DetectCycle/TopoSort）；根包 `plan_aliases.go` alias 重导出保持 API 兼容。
- ✅ `task/`（2026-08-12 第二轮）：`task_registry.go`/`task_terminal.go`（+ 测试）迁入；
  `TaskRegistry` actor、`TaskRecord`/`TaskSpec`/`TaskStatus`/`TaskTracePoint`、`TodoItem` 兼容契约、
  `TaskTerminalProvider` 随包；Runtime task 门面拆到根包 `task_facade.go`；根包 `task_aliases.go` 重导出。
- ✅ `plan/`（2026-08-12 第三轮）：plan 域整体迁入——`executor`（Executor/ExecutorDeps）、
  `tool_provider`（ToolProvider/LoadedPlanDoc/RunPlan）、`preflight`（PlanPreflight/ReplanRequest）、
  `policy`（PlanPolicy）、`events`（PlanNodeEvent/EventSink）、`replan_guard`（ReplanGuard/ReplanMetrics）、
  `input_adapter`（NormalizePlanLoadArguments）、`factory_types`（SeelexNodeInput/NodeBudgetInput/
  CanonicalPlanDocument/product/approval 节点）、`branch_types`（PlanBranchBinding/PlanBranchEvent）、
  `authority`；根包 plan_aliases.go 全量重导出 + 构造薄壳；Executor 增加 CurrentRunID/EventSink/
  LoadedPlan/MaxForkConcurrency 读取面供根包测试；replan_guard_test 迁 plan/。
- ✅ `fork/`（2026-08-12 第三轮）：fork_subagents 纯类型与 summary 节点迁入
  （Input/SubagentSpec/PlanCanonical/SummaryNode/ResultSummaryLines）；fork_tool.go 保留
  Runtime 编排门面；根包 fork_aliases.go 重导出。
- ✅ `internal/model/`：`account.go`（AccountSpec/AccountRole/Role*/ResolveAccountSpec/FallbackRoles/
  AccountRoleFromName）下沉；根包 model_aliases.go 重导出；task/plan 直接 import。
- ✅ `internal/config|storage|stream|telemetry/`（2026-08-12 第四轮）：补齐 §3.1 遗漏的 4 个
  纯零耦合文件——`config.go`→internal/config（Config/AccountLimits/Load，accountRole 移 model 域）、
  `storage.go`→internal/storage（SessionStore/NestedSessionStore）、
  `stream_completer.go`→internal/stream（NewStreamingCompleter，测试随包，runtime 集成测试留根包）、
  `trace.go`→internal/telemetry；根包 config_aliases/storage_aliases/telemetry_aliases 重导出保持 API 兼容。
- ⏳ 高耦合文件（subagent_sessions/subagent_context/subagent_tree/subagent_events、worktree_manager、
  agent_node、mcp、scoped_tools、todo_tool、scheduler、node_scope/node_tool_result 等）
  依赖根包类型（NodeScope/forkSubagentSpec/ParentEvidenceProjection/Runtime 装配面），
  物理搬移前必须先组件化（P3），本轮未做。

### P3 组件化 / P4 facade 瘦身 —— 待后续 ⏳

高耦合 `*Runtime` 方法需先转为组件方法（deps 闭包/接口注入），再物理搬移；顺序不可调换（规划 §6 风险与对策）。
