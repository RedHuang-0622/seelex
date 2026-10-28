# Store M11 收尾记录

日期：2026-10-29
分支：`main`

## 结论

根 `store` 的生产 import 从 M10 的 **19 个文件**收紧到 **8 个文件**。剩余文件都在
组合根或显式持久化适配边界，不再承载领域判定、第二份投影或运行期能力发现。

当前结构指标：

| 指标 | 数值 |
|---|---:|
| 根生产 Go 文件 | 37 |
| 直接 import 根 `store` 的生产文件 | 8 |
| `Router` 方法（公开） | 81（71） |
| `jsonRepository` 方法（公开） | 62（21） |
| `storeEngine` 方法（公开） | 142（0） |
| `SessionGranularStore` 方法（公开） | 25（19） |
| 根公开函数 | 21 |

## 本批变化

1. `kernel/session/fork.go` 改用 `contract/protocol` 实际身份与段落纯函数，
   `store.Event`、`store.CompactFrame`、`store.ToolResult` 不再泄漏进 Kernel。
2. `store/session` 成为事件会话归属、会话不存在判据和 Commit payload 的规范 owner：
   - `SessionIDFromEvent` / `SessionIDFromLocations`
   - `IsSessionNotFound`
   - `Commit`
   根 `store` 只保留同身份 alias / 函数值 alias。
3. `backend/engine/obs` 只接收 `Load` / `LoadRange` 函数端口，不再持根 Router
   或自行构造 EventStore。
4. `backend/engine/assemble` 的 SessionContext/History/子代理恢复改用消费方窄接口；
   具体 DurableHistory、EventStore 和持久化构造收进 `runtime_history.go`。
5. `internal/host` 的 compressed-turn 适配器使用 `store/session.Commit`；SSH、
   session manager 与综合 workspace 端口仍是具体存储适配边界。
6. 删除只用于编译期断言的
   `backend/engine/context/seelectx/router_storage_conformance.go`；同一断言已由
   `router_storage_conformance_test.go` 的真实消费用例覆盖。
7. `TestStoreDDDM10FacadeGate` 增加陈旧 allowlist 与重复 count 检查；未来条目只能删除，
   不能靠保留旧名字制造“预算未超”的假象。
8. `SessionGranularStore` 删除 19 个无生产价值的转发方法：历史/事件/上下文/工具结果/
   压缩读取改由 Host 直连 Router；queue 与 pending-tail 使用既有 Host Router 端口；
   仅保留 record/session/目录解析与内部编排所需面。

## 剩余 8 个根 import

| 文件 | 保留原因 |
|---|---|
| `boot/app.go` | 组合根必须构造 EventStore、注入 workspace resolver 与持久化钩子 |
| `boot/application.go` | 组合根构造 SessionMetaStore、RoleSessionAdapter，并接线 SSH resolver |
| `boot/runtime.go` | 组合根打开 typed `Stores`，选择具体实现 |
| `boot/tools.go` | 组合根把 project record / Router / workspace 注入工具域 |
| `internal/host/session_manager.go` | 具体 Router 的进程内会话生命周期适配壳 |
| `internal/host/session_ssh_state_port.go` | SSH 状态文件依赖 Router 私有布局与项目目录，属于持久化适配 |
| `internal/host/session_workspace_ports.go` | Host 把具体 Store owner 收窄成 Application/Kernel 端口的唯一综合适配器 |
| `backend/engine/assemble/runtime_history.go` | Engine 历史适配器构造 DurableHistory、EventStore 与 search 事件源 |

这些文件不能直接换成“零 import”而不新增一层只做转发的 facade；那会违反
AGENTS §10.2「禁止包一层算拆分」。要让根 import 归零，下一步必须把
Commit/DurableHistory/EventStore 的构造工厂继续下沉成真正的领域构造器，
而不是继续移动包装。

## 验证

```text
go test ./kernel/session ./store/... ./backend/engine/obs ./backend/engine/assemble ./internal/host ./e2e -count=1
python scripts/store-ddd/audit-root-shrink-coverage.py --check
```

本批次验收：

- `go test -p 1 ./... -count=1 -timeout=600s`：通过；
- `go test ./e2e -count=1`：通过；
- `go build ./...`：通过；
- `go build -tags "gui,desktop,production" ./...`：通过；
- `go vet ./...`：通过；
- `node --test gui/frontend/dist/*.test.mjs`：952/952 通过；
- `python scripts/store-ddd/audit-root-shrink-coverage.py --check`：通过；
- `python scripts/gen_core_readme_index.py --check`：通过。
