# Plan

`seelebridge/plan` 承载 Plan 图论与 JSON 适配的纯逻辑（零运行时依赖）：

- `graph.go`：`PlanEdge`、`AdjacencyToEdges`、`DetectCycle`、`TopoSort`。

根包通过 `seelebridge/plan_aliases.go` 重导出以保持公共 API 兼容
（`seelebridge.PlanEdge` 等仍可用）。

## 验证

```text
go test ./seelebridge/plan -count=1
```
