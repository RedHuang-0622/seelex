# End-to-End Testing

## 生态位

`e2e/` 承载跨 Application、Engine script、Tool lifecycle 和 Interaction 的确定性验收能力。它验证用户旅程，不依赖真实 LLM 或外部网络。

主要调用方：CI（`go test ./e2e/... -count=1`）与本地回归；它同时是 README 覆盖门禁
（`layout_test.go`）的所在地。

## 架构图

```mermaid
flowchart TB
    FIX["e2e/fixtures/*.json<br/>受 docs/gui/schemas/agent-scenario-v1.schema.json 约束"] --> LOADER["scenario loader（严格：拒绝未知字段）"]
    LOADER --> ENG["ScriptedEngine<br/>严格消费 turn script"]
    LOADER --> RUNNER["Runner：submit / resolve / cancel 步骤"]
    ENG --> SVC["真实 application.Service"]
    FAKE["fake Runtime / Plugin / Skill / Session ports"] --> SVC
    RUNNER --> SVC
    SVC --> REC["eventRecorder：记录 Application events"]
    REC --> ASSERT["按顺序与 payload 断言最终可观察结果"]
    ASSERT --> RESULT["Result"]
```

## 用例图

```mermaid
flowchart LR
    DEV(("维护者 / CI"))

    UC1(["跑通一条用户旅程（提交 → 工具 → 审批 → 终态）"])
    UC2(["断言 Snapshot 与 Event 的可观察结果"])
    UC3(["校验文档契约与 fixture schema"])
    UC4(["门禁：模块必须有 README 且链接可解析"])

    E2E["e2e harness"]

    DEV --> UC1
    DEV --> UC2
    DEV --> UC3
    DEV --> UC4
    UC1 --> E2E
    UC2 --> E2E
    UC3 --> E2E
    UC4 --> E2E
```

## 子模块

- [`scenario/`](scenario/README.md)：scenario v1 schema、loader、scripted engine、runner、event recorder 和 harness。
- fixture 由 `docs/gui/schemas/agent-scenario-v1.schema.json` 约束；新增 fixture 应放在 `e2e/fixtures/`。

## 边界

E2E harness 可以实现 Application ports，但不复制生产实现。真实浏览器/Wails smoke 属于 GUI 测试层，不应塞进纯 Go scenario runner。

## Review 指南

- scenario 必须确定性、无网络、无真实凭据。
- 断言应面向可观察结果和事件顺序，不依赖私有字段。
- 新协议字段需同步 schema、loader validation 和有效/无效 fixture。

## 测试

```text
go test ./e2e/... -count=1
go test . -run '^TestGUI' -count=1
```
