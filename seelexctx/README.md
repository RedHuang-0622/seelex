# Seelex Context

## 模块定位

`seelexctx` 是跨 Engine、父子 Agent 和 A2A 分支传递工程上下文的门面。它把原始 history/trace 提炼为稳定 `ContextSnapshot`，支持预算压缩和 child-to-parent merge-back。

## 子模块

| 目录 | 职责 |
|---|---|
| [`snapshot/`](snapshot/README.md) | 上下文 DTO、builder、format 和 validate。 |
| [`provider/`](provider/README.md) | 从 Engine history 或 trace 导出 Snapshot。 |
| [`compactor/`](compactor/README.md) | 基于 token budget 的分级压缩。 |
| [`merger/`](merger/README.md) | 子任务 findings/decisions/progress 合并回父上下文。 |

`bridge.go`/`seele.go` 保留简单 Export/Import API，并复用 Seele `seelectx` 的 token 估算、NeedCompression、TrimHistory 与 context manager。

## 数据流

```text
Engine / Trace -> Provider -> ContextSnapshot -> Compactor -> child agent
parent snapshot <- Merger <----------------------- child result
```

## 设计原则

- copy-on-write：压缩和 merge 不修改调用方原对象。
- 有界上下文：所有跨 Agent 注入都应有 token budget。
- 结构化优先：Goal、Decision、Finding、Constraint、PendingWork 分字段传递。
- 向后兼容：门面 API 保持稳定，复杂能力下沉子包。

## Review 指南

- 是否把完整 secrets/tool raw output 无界注入 child。
- token 估算和压缩层级是否保持确定性。
- merge 是否去重 constraints、保留 parent goal，并正确处理 escape。
- Provider nil/empty trace 是否安全降级。

## 测试

```text
go test ./seelexctx/... -count=1
```
