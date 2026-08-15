# Telemetry

`seelebridge/internal/telemetry` 承载内存遥测追踪器、生命周期钩子、
`Chain` 组合器与 `SummaryHook` 脱敏摘要的构造（薄封装 `Seele/telemetry`）。
属于根 facade 的装配细节，置于 internal/；根包经 `telemetry_aliases.go`
重导出保持公共 API。

## 关键组件

- `NewTracer` / `NewLifecycleHook`：内存遥测追踪器与 llm/tool intent-effect
  钩子（薄封装）。
- `Chain`：telemetry 钩子链组合器（`Wrapper` 装饰器形态）。透传、nil 兜底
  与 `ErrorHook` 传播集中在 Chain，新增观察面不再手抄透传样板；`OnError`
  按最外层→最内层传播给所有实现 `ErrorHook` 的组成钩子（未实现的钩子不
  接收，也不应再为实现透传而实现 `ErrorHook`）。
- `DiagnosticHook` / `StageHook`：bash 停滞诊断 / node 第一视角阶段日志
  （Wrapper 形态，由 Chain 组装）。
- `SummaryHook` / `SummaryEvent`：B 类 llm/tool 脱敏摘要（统一事件库的
  B 类摘要层，见 `seelebridge/events_unified.go`）。只记失败、超时或慢
  调用（默认阈值 30s，`WithSlowThreshold` 可覆盖）；字段仅含
  kind/name/status/duration_ms/at/nodeID，绝不含参数、结果或正文。

## 验证

```text
go test ./seelebridge/internal/telemetry -count=1
```
