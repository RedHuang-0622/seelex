# Application Approval

## 定位

本包提供与界面无关的审批生命周期。`core.Service` 把高风险动作转换为 `ApprovalRequest`，TUI/GUI 只展示 `model.Interaction` 并提交决议。

## 核心实现

- `ApprovalRequest`：问题、风险、工具预览、选项和超时。
- `ApprovalDecision`：前端选择的 option ID。
- `ApprovalBroker`：维护待决请求表，以 request ID 关联等待方和 UI 决议。
- `SetObserver`：把当前 Interaction 投影给 Application Snapshot。

`Request` 注册 pending request、发布打开事件并等待 context、timeout、Resolve 或 Shutdown；`Resolve` 保证请求只完成一次；`ResolveAll` 用同一显式决议原子摘取并释放全部当前 pending request，供用户在工具等待期间开启 Full Access；`Shutdown` 唤醒全部等待者。

## 生态位与边界

本包决定“如何等待和完成审批”，不决定哪些工具需要审批，也不直接渲染界面。权限规则属于 Seele permission/runtime 适配层，展示属于前端。

## 并发与 Review

- pending map 必须在锁内修改，向等待 channel 发送时避免重复完成。
- context cancel、timeout、Resolve、ResolveAll、Shutdown 可能竞争，任何路径都必须清理 pending，且不能重复完成等待方。
- Observer 不应在持锁状态执行不可控逻辑。
- 新增决议类型时同步更新 `model.Interaction` 和两个前端。

## 测试

审批行为由 `broker_test.go`、`application/core/service_test.go` 和 `race_test.go` 覆盖：

```text
go test ./application/approval -count=10
go test ./application/core -run Approval -count=1
```
