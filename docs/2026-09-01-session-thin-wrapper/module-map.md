# 模块改造前置与验收清单（Module Map）

> 依据: thin-wrapper-session-design / mbd-*（模型/用例/活动图/测试）
> 阅读顺序：先看「前置文件」，改完看「验收文件」是否达标。

## 1. session/（薄封装层：SessionUnit + 端口）

| 项 | 文件 |
|---|---|
| 前置 | [session/domain.go](../../session/domain.go)（现 Unit/View/ChatRuntime）、
  [session/manager.go](../../session/manager.go)（legacy）、
  [thin-wrapper-session-design.md](./thin-wrapper-session-design.md)、
  [mbd-models.md](./mbd-models.md) §2 |
| 改造 | 新增 `ports.go`（EnginePort/StorePort）、SessionUnit 骨架、
  薄生命周期（status 由 HasSession 驱动，删自造状态机） |
| 验收 | [session/domain_test.go](../../session/domain_test.go)（扩展）+ 新增契约断言
  `var _ session.EnginePort = …`；测试用例 T2.1/T2.3/T2.4、B1/B2；`go test ./session ./... -p 1` |

## 2. seelebridge/（能力层：bundle / loop / trace / task）

| 项 | 文件 |
|---|---|
| 前置 | [runtime.go](../../seelebridge/runtime.go)（bundleFor / NewMainSession / Telemetry hook）、
  [runtime_bundle.go](../../seelebridge/runtime_bundle.go)、
  [runtime_session.go](../../seelebridge/runtime_session.go)、
  [ports.go](../../seelebridge/ports.go)（task/switch）、
  [internal/telemetry](../../seelebridge/internal/telemetry)（chain/summary/stage/diagnostic） |
| 改造 | 实现 EnginePort（HasSession/NewMainSessionWithID/ChatStreamFor/Unload）；
  trace 会话过滤；task 会话路由 |
| 验收 | `go test ./seelebridge -count=1`、[task_partition_test.go](../../seelebridge/task_partition_test.go)、
  新增 T1.1/T1.2/T5.4；`-race ./seelebridge` |

## 3. application/core（facade：runChat 委托 + 投影 + 编排）

| 项 | 文件 |
|---|---|
| 前置 | [chat.go](../../application/core/chat.go)（runChat/appendDelta）、
  [session_scope.go](../../application/core/session_scope.go)、
  [session_history.go](../../application/core/session_history.go)、
  [session_lifecycle.go](../../application/core/session_lifecycle.go)、
  [work_table.go](../../application/core/work_table.go)、
  [service_assembler.go](../../application/core/service_assembler.go)、
  [contract/ports.go](../../application/contract/ports.go) |
| 改造 | runChat 委托 Seele loop（hooks 投影）；生命周期改经 session 端口；
  移除平行循环/预算/transcript；worktable 经会话 scope 同步 |
| 验收 | `go test ./application/core -count=1`、事件指纹回归、
  T2.4 编译断言（core 无容器字段）、UC6 结构断言、
  [session_stress_test.go](../../application/core/session_stress_test.go) /
  [session_decoupling_test.go](../../application/core/session_decoupling_test.go) |

## 4. sessionstore + workspace（存储：会话粒度）

| 项 | 文件 |
|---|---|
| 前置 | [sessionstore/router.go](../../sessionstore/router.go)、
  [durable_history.go](../../sessionstore/durable_history.go)、
  [sessionstore.go](../../sessionstore/sessionstore.go)、
  [workspace/](../../workspace)（binding）、
  [session_runtime/archive.go](../../application/core/session_runtime/archive.go)（三读一写） |
| 改造 | 实现 StorePort（`session:<id>` 五片 + 项目索引）；旧 workspace 粒度口废弃 |
| 验收 | `go test ./sessionstore ./session`、T2.6/B6 会话粒度持久化 + 幂等、迁移测试 |

## 5. view_state + task_context（投影 / 上下文栈）

| 项 | 文件 |
|---|---|
| 前置 | [view_state/coordinator.go](../../application/core/view_state/coordinator.go)、
  [task_context/coordinator.go](../../application/core/task_context/coordinator.go)、
  [task_context_state.go](../../application/core/task_context/task_context_state.go)、
  [session/domain.go](../../session/domain.go)（View） |
| 改造 | View 投影收敛到 SessionUnit；上下文栈绑定单元；窗口/队列上限 |
| 验收 | `go test ./application/core ./session`、T3.* 投影、T2.8/B3 上限、B4 深拷贝 |

## 6. gui/（前端：双投影 / 事件过滤 / bridge）

| 项 | 文件 |
|---|---|
| 前置 | [components.js](../../gui/frontend/dist/components.js)、
  [trajectory.js](../../gui/frontend/dist/trajectory.js)、
  [protocol.js](../../gui/frontend/dist/protocol.js)、
  [client-state.js](../../gui/frontend/dist/client-state.js)、
  [app.js](../../gui/frontend/dist/app.js)、
  [bridge.go](../../gui/bridge.go) |
| 改造 | 对话/轨迹一致性（INV-M3-3）；切换 resync；trace 视图（按会话） |
| 验收 | `node --test gui/frontend/dist/*.test.mjs`（165+）、T3.7 一致性、T3.8 空/超限、
  `go test ./gui -count=1`、GUI tag 构建 |

## 7. seelebridge/internal/telemetry（trace）

| 项 | 文件 |
|---|---|
| 前置 | [chain.go](../../seelebridge/internal/telemetry/chain.go)、
  [summary.go](../../seelebridge/internal/telemetry/summary.go)、
  [stage_hook.go](../../seelebridge/internal/telemetry/stage_hook.go)、
  [diagnostic.go](../../seelebridge/internal/telemetry/diagnostic.go)、
  Seele telemetry（模块缓存 `github.com/RedHuang-0622/Seele/telemetry`） |
| 改造 | trace 会话过滤；链序断言（Before/After 依序透传） |
| 验收 | T1.1/T1.2/T1.4、T5.4 并行隔离、`-race` 相关包 |

## 8. fork / 子代理（会话粒度）

| 项 | 文件 |
|---|---|
| 前置 | [session_fork.go](../../application/core/session_fork.go)、
  [plan/executor.go](../../seelebridge/plan/executor.go)、
  seelebridge subagent sessions（bundle）、[work_table.go](../../application/core/work_table.go) 同步 |
| 改造 | fork = 会话粒度深拷贝；subagent = 独立 SessionUnit（own loop/视图/历史） |
| 验收 | T2.7/B4 深拷贝隔离、UC7/UC8、既有 fork 测试全绿 |

## 9. 工具 / 沙箱 / FC（管线不动，补顺序断言）

| 项 | 文件 |
|---|---|
| 前置 | [main.go](../../main.go)（权限装配）、Seele `tools/permission`（FC）、
  seelebridge/security（PathGate）、seelebridge/worktree、seele.yaml 权限段 |
| 改造 | 仅补「FC 许可 → 沙箱中间件」顺序断言；授权即 allowance；工具注册/执行不变 |
| 验收 | 既有 permission/pathgate 测试 + UC9 顺序断言、B3 限额 |

## 10. 构建 / 冒烟（跨阶段）

| 项 | 文件 |
|---|---|
| 前置 | [scripts/seelex-flow.ps1](../../scripts/seelex-flow.ps1)、[Makefile](../../Makefile)、
  [internal/buildinfo/version.go](../../internal/buildinfo/version.go) |
| 验收 | `seelex-flow` stage→smoke→deploy→smoke、
  `go build -tags "gui,desktop,production" ./...`、前端 `node --test` 全量 |

## 汇总（模块 × 阶段 × 验收命令）

| 阶段 | 模块 | 验收命令 |
|---|---|---|
| 9.1 | session/ + application/core（契约） | `go test ./... -p 1`、`go vet ./...`（已实施 2026-09-01） |
| 9.2 | application/core + seelebridge + gui（loop/trace/投影） | `go test ./application/core ./seelebridge`、`node --test`（已实施 2026-09-01） |
| 9.3 | sessionstore + workspace + session | `go test ./sessionstore ./session`（已实施 2026-09-01） |
| 9.4 | fork + subagent（seelebridge/plan + core） | `go test ./application/core ./seelebridge -race`（已实施 2026-09-01） |
| 9.5 | 全仓清理 | `go test ./... -p 1`、`-race` 关键包、冒烟（已实施 2026-09-01；Deploy 基线二进制需用户确认） |
