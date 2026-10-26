# application/observability

## 生态位

`application/observability` 承载前端性能读取等只读应用适配。指标计算仍由
`middle/view` 唯一实现，本包只负责锁边界和窄输入装配。

## 职责

- 在共享视图读锁内采样 Snapshot、历史窗口和工具结果引用数量。
- 调用 `middle/view.PerfStats` 生成无内容的性能指标。

## 非职责

- 不计算指标、不拥有 Snapshot 或限额。
- 不读取操作内容、不执行 I/O、不缓存采样历史。
- 不导入 `application/core`，不持有完整 `Service`。

## 文件结构

- `perf.go`：性能指标应用组件。
- `perf_test.go`：锁边界测试。

## 核心实现

`PerfDeps` 以函数提供锁和三个输入。`PerfStats` 在锁内采样一次并立即释放，
随后由 `middle/view` 计算指标。

## 数据流

`application/core` 一行兼容转发 → `PerfApplication` → `middle/view.PerfStats`。

## 依赖方向

允许依赖 `middle/protocol/model` 与 `middle/view`。禁止依赖 `application/core`、
backend 具体实现或前端包。

## 并发与错误

只获取共享读锁；不得在锁外读取需要一致性的输入，也不得把锁持有到调用方。

## 扩展方式

新增无内容指标时优先扩展 `middle/view` 的计算和本包输入，不在 core 复制公式。

## Review 指南

- 锁是否只覆盖输入采样和计算。
- 指标是否仍不包含 prompt、参数或工具输出正文。

## 测试与验证

```text
go test ./application/observability -count=1
go test ./application/... -count=1
```
