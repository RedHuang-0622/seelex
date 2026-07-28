# Plan Plugin

`plan` 将可见工具收窄到 `plan_*` WorkPlan 操作，用于加载、执行、观察、导出和清理 DAG 工作流。

## JSON 契约

`plan_load` 的 `nodes` 必须是以 node ID 为 key 的对象，`edges` 是 source 到 successor IDs 的邻接表；不能把 nodes 发送成数组。Seelex 在 `seelebridge/plan_tool_provider.go` 补充 LLM-facing schema，执行器仍由 Seele WorkPlan 提供。

## 边界

Plan Plugin 负责编排，不直接授予分支工具权限。并行 branch 的 runtime、account、workspace 和结果归属由 `seelebridge`/Application binding 管理。

## 依赖与生命周期

manifest 选择 `plan_*` tools，Seele WorkPlan 保存 DAG/执行器，`seelebridge` 负责 schema 和 branch runtime，Application 把 node/branch callback 投影给 TUI/GUI。

## Review

- DAG schema、cycle/topological semantics 和 GUI Plan DSL 是否一致。
- 默认 fail-fast；best-effort 只能由调用方显式启用。
- node 状态和失败 interaction 是否及时反映到 Snapshot/Event。

## 验证

```text
go test ./seelebridge ./application/core ./tui ./gui -run Plan -count=1
node --test gui/frontend/dist/plan-dsl.test.mjs
```
