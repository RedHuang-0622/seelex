# Fork

`seelebridge/fork` 承载 `fork_subagents` 的纯类型与 DAG 构造：

- `types.go`：`Input`/`SubagentSpec` 输入契约、`SubagentsContractDescription`
  工具契约描述、`PlanCanonical` 规范 JSON（审计/展示）。
- `summary.go`：`SummaryNode` 汇总节点（把前驱输出压缩为每子代理一行的
  有界摘要，rune 计数截断）与 `ResultSummaryLines`/`SummaryLineLimit`/
  `SummaryMaxLines` 截断参数。

执行编排（账号/任务绑定/结果复用/worktree 生命周期）留在根包 Runtime 门面
（`fork_tool.go`）；本包不反向依赖 `seelebridge` 根包，仅依赖 `plan` 子包
（`SeelexNodeInput`）。根包 `fork_aliases.go` 重导出符号保持 API 兼容。

## 验证

```text
go test ./seelebridge -count=1
```
