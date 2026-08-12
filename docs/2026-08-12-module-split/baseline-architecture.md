# Seelex 目录架构基线（模块拆分前）

日期：2026-08-12
用途：作为之后由 seelex 自行执行模块拆分时的 **before 状态参照**——文件归属、公共 API 面、验证基准均以本快照为准。
前置方案：`docs/2026-08-12-module-split/plan.md`

## 0. 快照元数据

- git head：`b8d2717`（2026-08-12 10:49:47 +0800，main）
- 依赖：`github.com/RedHuang-0622/Seele v0.1.2`（无本地 replace）
- 测试规模：127 个 `_test.go`、845 个 `func Test`、3 个 `func Fuzz`；总语句覆盖率 70.7%（coverage-summary.txt:1652）
- seelebridge：44 个非测试文件 + 41 个测试文件；173 个导出的 `*Runtime` 方法

## 1. 顶层模块清单

| 目录 | Go 文件数 | 定位 |
|---|---:|---|
| `application/` | 85 | 应用层（approval/contract/core/event/model/prompt/search） |
| `seelebridge/` | 85 | 运行时适配层（本次拆分主对象，扁平 44+41） |
| `seelexctx/` | 38 | 上下文生命周期（compactor/lifecycle/memory/merger/provider/search/snapshot） |
| `sessionstore/` | 20 | 会话持久化（event_store/router_storage/durable_history） |
| `tui/` | 11 | TUI 前端 |
| `gui/` | 9 | Wails GUI 桥 |
| `mcpstack/` | 8 | MCP 调用栈 |
| `e2e/` | 8 | 端到端 scenario harness + fixtures |
| `internal/` | 6 | 内部共享（frontmatter/promptassets） |
| `plugin/` | 5 | Plugin 生命周期 |
| `skill/` | 4 | Skill 加载与可见性 |
| `session/` | 2 | 会话用例适配 |
| `workspace/` | 2 | 项目/session binding |
| `config/` | 0 | 账号配置（yaml 非 go） |
| `plugins/` | 0 | 声明式能力包（markdown 契约） |

## 2. 根目录文件清单

### 2.1 保留在根

`main.go`、`main_unix.go`、`main_windows.go`、`Makefile`、`go.mod`、`go.sum`、`AGENTS.md`、`README.md`、`README_EN.md`、`CHANGELOG.md`、`LICENSE`、`SECURITY.md`、`CODE_OF_CONDUCT.md`、`CONTRIBUTING.md`、`DESIGN.md`

### 2.2 抽包（当前均为 package main）

| 文件 | 目标包 |
|---|---|
| `application_adapters.go`（38KB） | `application/adapters/` |
| `backend_console.go`（10KB） | `application/console/` |
| `mcpconfig.go` | `mcpstack/config/` |
| `websearch.go` | `seelebridge/tools/websearch/` |
| `version.go` | `internal/buildinfo/` |

### 2.3 迁 e2e（均为 package main 测试）

`bootstrap_test.go`、`layout_test.go`、`governance_test.go`、`release_test.go`、`smoke_test.go`、`manual_smoke_test.go`、`context_long_live_test.go`、`tool_full_chain_test.go`、`tool_full_chain_live_test.go`、`docs_contract_test.go`

### 2.4 迁 docs

`ARCHITECTURE_REVIEW.md`（→ docs/arch/）、`CODE_EVALUATION_REPORT.md`（→ docs/devlog/）、`finish-review.md`（→ docs/devlog/）、`code-changes.md`（→ docs/devlog/）、`plan.md`（→ docs/arch/）、`test-report.md`（→ docs/test/）、`README-dev.md`（→ docs/devlog/）

### 2.5 配置与生成物

`seele.yaml`、`seelex.yaml`（→ config/，需同步 main.go 加载路径）；`coverage.out`、`coverage-summary.txt`、`debug.log`、`tmux-*.log`（生成物，已 ignore，可清理）；`opt/`、`tmp/`（本地杂物）；`local/`（已 ignore）

## 3. seelebridge 文件归属（拆分对象）

### 3.1 零 `*Runtime` 方法（24 个，可先纯搬移）

`plan_executor.go`、`plan_policy.go`、`plan_preflight.go`、`plan_input_adapter.go`、`plan_tool_provider.go`、`plan_events.go`、`plan_authority.go`、`replan_guard.go`、`worktree_manager.go`、`subagent_context.go`、`subagent_sessions.go`、`task_registry.go`、`task_terminal.go`、`filesystem_actor.go`、`project_scope.go`、`pathgate.go`、`sandbox.go`、`config.go`、`storage.go`、`stream_completer.go`、`trace.go`、`node_scope.go`、`telemetry_diagnostic.go`、`command_other.go`、`command_windows.go`

### 3.2 高耦合 `*Runtime` 方法（需先组件化再搬）

| 文件 | 导出方法数 | 目标域 |
|---|---:|---|
| `runtime.go` | 46 | 根 facade |
| `context_components.go` | 17 | 根装配 |
| `agent_node.go` | 14 | `node/` |
| `mcp.go` | 13 | `mcp/` |
| `scoped_tools.go` | 11 | `tools/` |
| `todo_tool.go` | 10 | `task/` |
| `task_registry.go` | 8 | `task/` |
| `scheduler.go` | 7 | `mcp/` 或 `scheduler/` |
| `fork_tool.go` | 6 | `fork/` |
| `actor.go` | 6 | 根 facade |
| `branch.go` | 5 | `node/` |
| `plugins.go` | 5 | `plugin/` |
| `worktree.go` | 4 | `worktree/` |
| `accounts.go` | 3 | 根 facade |
| `subagent_tree.go` | 3 | `session/` |
| `subagent_events.go` | 3 | `session/` |
| `node_tool_result.go` | 3 | `node/` |
| `plan_factory.go` | 2 | `plan/` |
| `plan_preflight.go` | 1 | `plan/` |
| `docker.go` | 1 | `mcp/` |
| `history_search.go` | 1 | `tools/` |
| `task_terminal.go` | 1 | `task/` |

合计 173 个导出 `*Runtime` 方法——这是根 facade 必须保留的公共 API 面。

### 3.3 测试文件（41 个，随源码迁移）

`agent_node_test.go`、`command_windows_test.go`、`docker_test.go`、`event_sink_test.go`、`filesystem_actor_test.go`、`fork_concurrency_repro_test.go`、`fork_concurrency_test.go`、`fork_live_smoke_test.go`、`fork_smoke_test.go`、`fork_tool_test.go`、`glob_guard_test.go`、`mcp_integration_test.go`、`mcp_provider_test.go`、`merge_back_concurrency_test.go`、`node_scope_test.go`、`node_tool_result_test.go`、`permission_state_test.go`、`plan_executor_test.go`、`plan_input_fuzz_test.go`、`plan_kernel_test.go`、`plan_test.go`、`project_scope_test.go`、`replan_guard_test.go`、`runtime_test.go`、`sandbox_test.go`、`scheduler_test.go`、`scoped_tools_diagnostic_test.go`、`storage_test.go`、`stream_completer_test.go`、`subagent_audit_test.go`、`subagent_events_test.go`、`subagent_sessions_test.go`、`subagent_tree_test.go`、`task_registry_test.go`、`telemetry_diagnostic_test.go`、`todo_tool_test.go`、`trace_test.go`、`visibility_test.go`、`worktree_failure_smoke_test.go`、`worktree_manager_test.go`、`worktree_test.go`

## 4. 验证基准（拆分后回归对照）

```text
# 本地关键测试组
go test ./seelebridge/ -run 'TestForkManySubagents|TestMergeBack|TestTaskRegistry|TestWorktreeScene' -count=1

# 真实 API 冒烟（需账号配置）
$env:SEELEX_LIVE_SMOKE='1'
$env:SEELEX_ACCOUNTS_PATH='G:\Program\go\seelex\config\accounts.yaml'
go test ./seelebridge/ -run TestForkSubagentsLiveSmoke -count=1 -v -timeout 20m

# 构建
go build ./...
make rebuild-gui VERSION=dev LOCAL_CONFIG='G:/Program/go/seelex/config/accounts.yaml'
```

拆分完成标准：以上命令全绿、导出符号不减少（173 个 `*Runtime` 方法仍可调用）、根目录文件数按 §2 收敛。

## 5. 快照生成命令（可复现）

```powershell
# 目录树
Get-ChildItem -Recurse -Force | Where-Object { $_.Name -notmatch '^(\.git|dist|local|opt|tmp|node_modules)$' }
# 耦合度
rg -c 'func \(r \*Runtime\)' seelebridge/*.go
# 导出方法
rg -c '^func \(r \*Runtime\) [A-Z]' seelebridge/*.go
```
