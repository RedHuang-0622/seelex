# Telemetry

`seelebridge/internal/telemetry` 承载内存遥测追踪器与生命周期钩子的构造
（`NewTracer`/`NewLifecycleHook`，薄封装 `Seele/telemetry`）。属于根 facade
的装配细节，置于 internal/；根包经 `telemetry_aliases.go` 重导出保持公共 API。

## 验证

```text
go test ./seelebridge -count=1
```
