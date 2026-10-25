# Application 模块化实施进度

日期：2026-10-25

本文是
[`plan.md`](plan.md)
的执行记录。代码与测试仍是“已实现”能力的最终事实源；本文只记录批次状态、
结构指标和跨会话门禁。

## 批次进度

| 批次 | 状态 | 本轮改动 | 退出证据 |
|---|---|---|---|
| AP-001 | 已完成 | 171 方法冻结、直接内部依赖白名单、兼容层台账 | `application/architecture_budget_test.go` |
| AP-002 | 已完成 | 新增 13 个能力型入站接口；旧四组接口与 171 方法 `Service` 保持兼容 | `application/api_partition_test.go` |
| AP-003 | 阻塞 | 依赖 `contract/dto` 共享模型迁移 | Backend/Middle 会话负责 |
| AP-004 | 已完成 | 抽只读 `SessionQuery`：目录、项目根、元数据、目录刷新等待 | `application/query_faces_test.go`、`application/core/session_query.go` |
| AP-005 | 已完成 | 抽 `ConversationHistoryQuery`；冷读移出共享写锁 | 历史分页回归、慢 I/O 锁行为测试 |
| AP-006+ | 不在本轮 | Conversation 聚合、Session 生命周期、聊天主循环 | 计划中 |

## 基线指标

| 指标 | 2026-10-25 计划值 | 本轮起点 |
|---|---:|---:|
| `Service` 公开方法 | 171 | 171 |
| `application/core` 直接导入 backend/middle/store 包 | 38 | 39 |
| `application/core` 直接导入 backend/middle/store 包（本轮终点） | 38 | 39 |
| `application/core` 生产 Go 文件 | 76 | 以工作树为准 |

本轮起点和终点均为 39，比计划多 1。曾尝试新增
`application/conversation/query` 适配层来把 `middle/switch/history` 移出
`application/core`，但 `e2e/facade_boundary_test.go` 的 I1 门禁禁止门面依赖闭包
新增包；为保持既有边界且不复制历史算法，本轮撤回该适配层并保留直接依赖。
因此 38 目标未完成，预算继续冻结 39。

## 高冲突文件

以下文件在迁移期间单车道修改：

- `application/application.go`
- `application/faces.go`
- `application/core/service_api_faces.go`
- `application/core/service.go`
- `application/core/service_components.go`
- `application/core/service_snapshot.go`

## 兼容层台账

兼容层必须记录旧接口、新接口、当前调用方、删除里程碑、归零验证和回退方式。
兼容实现只允许一行转发；任何状态、分支、写盘或事件发布都表示迁移尚未完成。

| 旧接口 | 新接口 | 当前调用方 | 删除里程碑 | 归零验证 | 回退方式 |
|---|---|---|---|---|---|
| `ViewFace` | 按能力拆分的只读接口 | GUI/TUI/Bridge 待迁 | M8/AP-008 | `rg '\\bViewFace\\b' gui tui wiring e2e` | 调用方继续持 `ViewFace` |
| `CommandFace` | 按能力拆分的命令接口 | GUI/TUI/Bridge 待迁 | M8/AP-008 | `rg '\\bCommandFace\\b' gui tui wiring e2e` | 调用方继续持 `CommandFace` |
| `PolicyFace` | `PolicyAPI` / `AdminAPI` | GUI/TUI/Bridge 待迁 | M8/AP-008 | 旧接口生产调用为零 | 恢复 `PolicyFace` 消费 |
| `QueueFace` | `QueueAPI` | GUI/TUI/Bridge 待迁 | M8/AP-008 | 旧接口生产调用为零 | 恢复 `QueueFace` 消费 |
| 171 方法 `application.Service` | 能力型小接口组合 | 门面与旧宿主 | M8/M9 | `application/testdata/service_methods.txt` 递减 | 恢复完整 `Service` 消费 |
| `Service.ListSessions` / `ProjectRootFor` / `SessionMeta` / `WaitCatalogRefresh` | `SessionQuery` | GUI/TUI/headless 待迁 | M8/AP-008 | 旧方法只一行转发，生产可直接消费 `SessionQuery` | 恢复查询方法体 |
| `Service.LoadMoreHistory` / `LoadLatestHistory` | `ConversationHistoryQuery` | GUI/TUI 待迁 | M8/AP-008 | 旧方法只一行转发，冷读不持写锁 | 恢复分页方法体 |
| `application/core -> middle/switch/history` | 待 Middle 提供已在闭包白名单内的查询端口 | `application/core` | BM6 | 依赖预算为 38 且白名单不含旧边 | 保留当前直接导入与唯一实现 |

## AP-002 实施记录

新增的 13 个接口都由 `Service` 结构满足，单接口方法数 1–15。GUI/TUI 本轮
不迁移，因此旧 `ViewFace`/`CommandFace`/`PolicyFace`/`QueueFace` 继续作为
兼容入口，方法集和行为均未改变。`api_partition_test.go` 钉住两条不变量：

1. 任一能力 API 不得超过 20 个方法；
2. 四个旧兼容接口的每个方法都至少被一个新能力接口覆盖。

## AP-004 / AP-005 实施记录

纯搬运语义：

- `Service.ListSessions` 的组合顺序移入 `sessionQuery.ListSessions`；
- `ProjectRootFor`、`SessionMeta`、`WaitCatalogRefresh` 移入 `sessionQuery`；
- `LoadMoreHistory`、`LoadLatestHistory` 的编排移入
  `conversationHistoryQuery`，既有分页 helper 保持原位置和唯一实现。

删除/合并的重复位置：

- `session_history.go` 原方法体 → `session_conversation_history_query.go`；
- `service_snapshot.go` 原 `ListSessions` 方法体 → `session_query.go`；
- `project_board_read.go`、`session_meta.go`、`session_catalog.go` 的查询方法体
  → `session_query.go`；
- 历史 helper 仍直接调用 `middle/switch/history`；尝试新增 Application 适配包会
  违反 I1 门面闭包，因此没有引入第二份实现或未登记依赖。

门禁：

- 查询对象结构不持有 `*Service`；
- 查询实现文件禁止 `ViewMu.Lock`；
- 慢历史存储读取期间 `Snapshot` 必须可读，证明查询不持共享写锁；
- 171 方法总数不变，旧入口全部保留；
- 直接内部依赖保持 39，38 目标因 I1 闭包约束仍未完成。

## 跨会话阻塞项

- AP-003 需要 Backend/Middle 会话先冻结并迁移共享 DTO。
- AP-004/AP-005 只使用现有 DTO；如发现必须修改 `contract/**` 或
  `backend/**`/`middle/**`，在本轮停止并在交付报告记录所需接口。

## 本轮验证记录

已通过：

- `go test ./application/... -count=1`
- `go test -race ./application/... -count=1`
- Facade/Document 中除依赖闭包外的全部用例
- `go vet ./...`
- `go build ./...`
- `node --test gui/frontend/dist/*.test.mjs`（874 项）
- `git diff --check -- application docs/2026-10-25-application-modularization`

跨会话环境项：

- `go test ./e2e -run 'Facade|Document' -count=1` 当前只有
  `TestFacadeDependencyClosureRatchet` 失败，指出 Concurrent Backend/Middle
  改动新增了未登记的 `contract/protocol`；该路径不属于本轮允许修改范围。
- 全仓 `git diff --check` 仍受既有文档大量行尾空白影响；本轮范围检查为零。
- `python scripts/check_readme_refs.py --strict` 当前指向被 Middle 会话删除的
  `middle/protocol/model/context.go`；本轮没有修改该文件。
