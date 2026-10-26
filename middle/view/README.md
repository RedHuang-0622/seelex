# middle/view

## 生态位

`middle/view` 是中间层的只读性能读数落点。当前公开能力只剩
`PerfStats`：把宿主在锁内采样出的 Snapshot、历史窗口和工具引用数量，计算成不含
正文的 GUI 性能指标。主要调用方是 `application/observability`。

EP-018 后，诊断文本归 `application/observability`，RuntimeVisibility /
ParentEvidence / LatestVisibleUserGoal 归 `application/admin`。这些能力不再通过
Middle 反向依赖 Application 做兼容 wrapper；旧调用方已迁到最终归属包。

## 职责与非职责

- 做：性能指标的数量、体积和截断计数口径。
- 不做：诊断文本、Runtime 投影、取锁、取数、持久化、事件发布或会话状态机。

## 文件结构

| 文件 | 职责 |
|---|---|
| `metrics.go` | `PerfStats`：GUI 性能追踪的数据面 |
| `view_test.go` | 指标口径（字符数、最大条、截断数、归档读数） |

## 核心实现

`PerfStats(snapshot, historyWindow, toolRefs)` 的三个输入都来自宿主，因为单一来源
不在 Middle：锁与 Snapshot 在 Application，历史窗口来自 limits，工具归档读数来自
任务协调器。`SnapshotBytes` 是当前快照的 JSON 估算，返回给前端对照 DOM/JS heap。

## 数据流

```text
application/observability 在 Core.ViewMu 读锁内采样
  → middle/view.PerfStats
  → 无正文 PerfStats DTO
  → GUI 性能追踪
```

## 依赖方向

允许依赖 `contract/protocol` 与标准库。禁止依赖 `application/*`、`backend/*`、
`store`，也禁止自取锁或持有宿主字段。

## 并发与错误语义

本包无状态、无锁。调用方必须保证传入的 Snapshot 与引用计数来自同一采样边界。
派生过程无 I/O；失败时由调用方按既有空读数或错误策略处理。

## 扩展方式

新增指标前先确认它是“从宿主采样输入派生的纯计算”。只新增字段和纯函数，不在这里
读取 limits、Runtime 或存储。诊断或 Runtime 投影应分别扩展
`application/observability` 与 `application/admin`。

## Review 指南

- 是否重新引入 `diagnostics.go` / `projection.go`？它们已经归 Application。
- 是否出现 `sync`、`Mutex`、`RLock`、`backend/*` 或 `application/*`？
- `PerfStats` 是否仍只返回数量/体积，不泄露 prompt、参数或工具输出正文？
- `SnapshotBytes` 是否仍在调用方既有锁边界内计算？

## 测试与验证

```text
go test ./middle/view -count=1
go test ./application/observability -count=1
go test ./application/core -run 'PerfStats' -count=1
```

## 文件与函数索引

### `metrics.go`

- `func PerfStats(snapshot model.Snapshot, historyWindow int, toolRefs []model.ToolResultRef) model.PerfStats` — 计算 GUI 性能追踪所需的无正文指标。
