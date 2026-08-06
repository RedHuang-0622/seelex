# Seelex Context

## 模块定位

`seelexctx` 是 Seele v2 会话上下文契约（`seelectx`）的 Seelex 适配层与跨
父子 Agent 的上下文承袭门面。它把原始 history/遥测事件提炼为稳定
`ContextSnapshot`，支持预算压缩和 child-to-parent merge-back；同时把主会话
的装配/压缩/控制决策实现为 `seelectx` 的原子策略（Assembler/Processor/
Compressor/Controller），供 `session.ContextComponents` 注入。

## 子模块

| 目录 | 职责 |
|---|---|
| [`snapshot/`](snapshot/README.md) | 上下文 DTO、builder、format 和 validate。 |
| [`provider/`](provider/README.md) | 从会话（SessionSource/DurableHistory）或 telemetry 导出 Snapshot。 |
| [`compactor/`](compactor/README.md) | 基于 token budget 的分级压缩。 |
| [`merger/`](merger/README.md) | 子任务 findings/decisions/progress 合并回父上下文。 |
| [`memory/`](memory/README.md) | 超长上下文的历史记忆选取：按当前查询从压缩帧选 top-K，渲染有界「相关记忆」块。 |
| [`lifecycle/`](lifecycle/) | 泛型 Context actor 与有界批处理管道；提供 cold-load/windowed/pipelined 策略和关闭竞态测试。 |

根包文件：

| 文件 | 职责 |
|---|---|
| `assembler.go` | `RequestAssembler`：system prompt（effort/skill）+ PromptBlocks + working history 拼装。 |
| `processor.go` | `ToolResultProcessor`：超大工具结果 → result_ref/省略警告。 |
| `compressor.go` | `Compressor` 适配：短历史免压缩 + QuickChat 隔离摘要。 |
| `controller.go` | `ContextController`：软/硬阈值、窗口外压缩、checkpoint 决策。 |
| `window.go` | 滑动窗口轮数策略（配置 + provider 推导）。 |
| `gap.go` | 真空区覆盖：滑动窗口与压缩内容之间的未压缩轮次，Load 时检测并压入合并帧。 |
| `history_safety.go` | Provider 历史安全配对规则（assistant/tool 配对、恢复信封）。 |
| `bridge.go` | Export/ExportWithGoal/Import 兼容 API（委托子包）。 |
| `seele.go` | re-export 仍被使用的 Seele `seelectx` token 估算/压缩函数。 |

## 数据流

```text
Session history / DurableHistory -> Provider -> ContextSnapshot -> Compactor -> child agent
parent snapshot <- Merger <------------------------------ child result
ContextComponents（Assembler/Processor/Compressor/Controller）-> session.Session
Load（尾窗）─ 真空区 GapCoverer → CoverHistoryGap → CompactStack 合并帧
Assembler ─ 查询 → memory.Select（压缩帧 top-K）→ 相关记忆块
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
go test -race ./seelexctx/lifecycle -count=1
```
