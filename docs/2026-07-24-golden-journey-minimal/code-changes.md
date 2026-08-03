# 代码变更摘要

## 新增/修改文件

| 文件 | 类型 | 说明 | 设计模式 |
|---|---|---|---|
| `e2e/scenario/schema.go`、`loader.go` | 新增 | 定义并严格加载最小 `seelex.scenario/v1`，拒绝未知字段和未实现步骤 | Schema-first、Fail fast |
| `e2e/scenario/ports.go`、`runner.go`、`recorder.go` | 新增 | 通过调用方窄接口执行 action/expect，并以事件通知作为 barrier | Ports and Adapters、Observer |
| `e2e/scenario/scripted_engine.go`、`harness.go` | 新增 | 用 scripted Engine 驱动真实 Application、ToolHookBridge 和 ApprovalBroker | Adapter、Dependency Injection |
| `e2e/fixtures/approval-chat.json` | 新增 | E2E-J01/J02 allow 主链路共享 fixture | Contract fixture |
| `e2e/scenario/*_test.go` | 新增 | Loader 正反例与黄金旅程回归测试 | Table/Scenario testing |
| `docs/gui/schemas/agent-scenario-v1.schema.json` | 新增 | 最小 Scenario v1 JSON Schema | Schema-first |
| `docs_contract_test.go` | 修改 | 将黄金旅程 fixture 纳入文档契约校验 | Contract test |
| `docs/gui/module_dotting.json`、`modules/agent-e2e-interaction.md`、`schemas/README.md` | 修改 | 模块状态改为 partial，并记录已实现边界 | Documentation governance |

## API 变更

| API | 变更 | 兼容性 |
|---|---|---|
| `scenario.Load` / `LoadFile` | 新增严格 Scenario loader | 新增测试基础设施，不影响生产 API |
| `scenario.NewRunner` / `NewHarnessRunner` | 新增 Core scenario runner 与默认真实 Application harness | 新增测试基础设施，不影响生产 API |
| `scenario.Result.WriteJSON` | 新增可序列化场景报告 | 新增能力 |

## 接口抽象

| 接口 | 实现方 | 使用方 |
|---|---|---|
| `Application` | `application.Service` | `Runner` |
| `ToolLifecycle` | `applicationToolLifecycle` | `ScriptedEngine` |
| `ApprovalRequester` | `application.ApprovalBroker` | `ScriptedEngine` |
| `ScriptState` | `ScriptedEngine` | `Runner` |

## 循环依赖检查

- [x] `e2e/scenario -> application` 单向依赖。
- [x] 生产包未反向依赖 `e2e/scenario`。
- [x] 未向 `application.Service` 增加测试专用入口。

## 验证

- `go build ./...`：通过。
- `go vet ./...`：通过。
- `go test ./... -count=1 -timeout=180s`：通过。
- `go test ./e2e/scenario -run '^TestGoldenJourneyChatToolApproval$' -count=100`：通过。
- `go test . -run '^TestGUI' -count=1`：通过。
- `git diff --check`：通过。

## Commit 建议

```text
test(e2e): add minimal golden journey runner

- define and validate the executable scenario v1 subset
- drive real chat, tool and approval application state
- add deterministic J01/J02 fixture and stability coverage
- mark Agent E2E architecture as partially implemented

Refs: E2E-J01, E2E-J02, CAP-E2E
```

本次未执行 commit。

## 2026-07-27 审查后修复

| 文件 | 变更 | 说明 |
|---|---|---|
| `e2e/scenario/ports.go`、`harness.go`、`scripted_engine.go` | 新增真实 Tool executor 注入点 | scenario 仍以 fixture 驱动，但可将指定 tool.call 交给真实框架 handler。 |
| `e2e/fixtures/manual-plan.json`、`e2e/scenario/runner_test.go` | 新增 WorkPlan manual 黄金旅程 | 覆盖真实 `plan_load → plan_run → ApprovalBroker → NodeResult → PlanState`。 |
| `docs_contract_test.go` | 新 fixture 纳入 Schema 契约检查 | 防止 fixture 与 `scenario-v1` Schema 漂移。 |
