# M8 代码变更摘要

## 新增/修改文件

| 文件 | 类型 | 说明 | 设计模式 |
| --- | --- | --- | --- |
| `seelebridge/branch.go` | 新增 | Seelex branch DTO、计划级 binding、稳定账号选择、私有 client/factory。 | Adapter、Factory、Strategy |
| `seelebridge/runtime.go`、`config.go` | 修改 | 注册 Seele branch hook/resolver，记录显式账号选择。 | Adapter |
| `application/ports.go`、`application_adapters.go` | 修改 | 将计划级 binding 作为 application-owned 窄接口传入 runtime。 | Port/Adapter |
| `application/chat.go`、`state.go` | 修改 | 冻结 session/workspace/account，映射 branch 生命周期到 PlanState。 | State machine |
| `main.go` | 修改 | 装配 branch event callback。 | Composition root |
| `tui/plan.go`、`gui/frontend/dist/styles.css` | 修改 | 展示 queued、canceled、panicked。 | Presentation mapping |
| `e2e/scenario/*` | 修改 | 支持将 WorkPlan branch event 注入 application，并新增双自动 fork E2E。 | Test harness adapter |
| `go.mod`、`go.sum` | 修改 | Seele 依赖声明对齐并直接解析远端 `v0.0.8`。 | Dependency management |

## API 变更

| API | 变更 | 兼容性 |
| --- | --- | --- |
| `seelebridge.PlanBranchEvent` | 新增 Seelex DTO | 新增，无破坏性变更 |
| `seelebridge.PlanBranchBinding` | 新增 request-scoped branch binding | 新增，无破坏性变更 |
| `Runtime.SetPlanBranchCallback` | 新增 branch event bridge | 新增，无破坏性变更 |
| `Runtime.SetPlanBranchBinding` | 新增 immutable binding 设置入口 | 新增，无破坏性变更 |
| `application.RuntimePort.SetPlanBranchBinding` | 新增 port 方法 | 仅适配器和测试实现需补齐 |
| `application.NodeStatus` | 新增 queued/canceled/panicked | JSON 枚举扩展，旧客户端可忽略 |

## 关键约束

- 每个分支创建自己的 `ChatClient` 和单账号 `AccountPool`；不修改共享 `r.client`。
- 私有 pool 复用已选账号对象，保留 Seele 内建的线程安全 RPM 限流；不复制含 mutex 的 `api.Account`。
- 账号选择以 `planID:branchID` 稳定 hash 决定；同角色仅有一个账号时合法退化为同账号。
- 未设置 binding 时 resolver 返回空 runtime，沿用旧节点 factory 路径。
- application 不导入 Seele `forkexec`，只消费 `seelebridge.PlanBranchEvent`。

## 验证

- `go test -p 1 ./...`：通过。
- `go vet ./...`：通过。
- 移除本地 `replace` 后，`go list -m` 确认解析 `github.com/RedHuang-0622/Seele v0.0.8`；
  冷测试 `./seelebridge ./application ./e2e/scenario`：通过。
- `node --test gui/frontend/dist/*.test.mjs`：26 项通过。
- `go test -race`：本机 `CGO_ENABLED=0`，保留给启用 CGO 的 CI。

## 循环依赖检查

- [x] `application -> seelebridge` 为单向 DTO/port 依赖。
- [x] `seelebridge` 不依赖 application。
- [x] TUI/GUI 仅消费 application snapshot。

## 建议提交信息

```text
feat(workplan): bridge isolated parallel plan branches

- bind role/account/session/workspace per plan branch
- publish branch lifecycle into PlanState and both frontends
- cover isolated automatic fork factories in the E2E harness

Refs: M8
```
