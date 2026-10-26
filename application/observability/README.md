# application/observability

## 生态位

`application/observability` 承载诊断文本、性能指标计算与前端只读采样适配。

## 职责

- 在共享视图读锁内采样 Snapshot、历史窗口和工具结果引用数量。
- 由 `metrics.go` 的唯一 `PerfStats` 实现生成无内容的性能指标。
- 从权威 Snapshot 生成 `/diag` 稳定文本，不读宿主私有状态。

## 非职责

- 不拥有 Snapshot、限额或前端缓存；只计算传入快照的派生指标。
- 不读取操作内容、不执行 I/O、不缓存采样历史。
- 不导入 `application/core`，不持有完整 `Service`。

## 文件结构

- `perf.go`：性能指标应用组件。
- `metrics.go`：无内容 PerfStats 计算。
- `diagnostics.go`：`RenderDiag` 诊断文本纯函数（EP-018 从 `middle/view` 归位）。
- `perf_test.go`：锁边界测试。
- `diagnostics_test.go`：诊断段、Plan 完成计数与空运行态投影测试。

## 核心实现

`PerfDeps` 以函数提供锁和三个输入。`PerfApplication.PerfStats` 在锁内采样一次并
立即释放，随后调用本包唯一 `PerfStats` 计算指标。`RenderDiag` 只消费传入的 Snapshot。

## 数据流

`application/core` → `PerfApplication` → `observability.PerfStats`；
`application.RenderDiag` / `/diag` → `observability.RenderDiag`。

## 依赖方向

允许依赖 `contract/protocol` 与 `contract/dto`。禁止依赖
`application/core`、backend 具体实现或前端包。

## 并发与错误

只获取共享读锁；不得在锁外读取需要一致性的输入，也不得把锁持有到调用方。

## 扩展方式

新增无内容指标时优先扩展 `metrics.go` 的计算和本包输入；诊断格式只改
`diagnostics.go`，不得复制到 core。

## Review 指南

- 锁是否只覆盖输入采样和计算。
- 指标是否仍不包含 prompt、参数或工具输出正文。

## 测试与验证

```text
go test ./application/observability -count=1
go test ./application/... -count=1
```
