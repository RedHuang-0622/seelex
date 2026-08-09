# Work Table A/B 对比测试方法与基线

## 目的

工作表格落地同时解决了长任务期间前端内存/CPU 的主要热点。本文记录新旧方案
的对比方法与回归门槛，防止后续改动静默回退到“整份 runtime + 整树重建”：

| 对比项 | A（基线/旧） | B（新） |
|---|---|---|
| 事件 payload | 整份 `runtime.changed`（含 plan 深拷贝 + 全量列表） | `worktable.changed` 扁平有界表格（行 ≤ `work_table_rows`，trace ≤ 10/行，evidence 截断） |
| 前端增量 | 每事件整树 innerHTML 重建（plan DSL + 子代理树） | keyed reconciliation + html 缓存，只重建变化行 |
| reducer | 每次事件整棵 plan 深拷贝 | `runtime.changed` 不预克隆 plan；`subagent.*` 路径级结构共享 |

## 自动断言

- `application/core/work_table_ab_test.go`（`TestWorkTablePayloadSmallerThanFullRuntime`）：
  构造 20 节点 × 30 事件/30 工具、20 todo、10 子代理的密集状态，断言
  `worktable.changed` payload 字节 ≤ 整份 `runtime.changed` 的 30%。
- `gui/frontend/dist/work-table.test.mjs` + `protocol.test.mjs`：断言表格行
  html 只随变化行重算（html 缓存语义）、`worktable.changed` 不克隆 plan
  （plan 对象引用不变）。
- 分配基线：`go test -run TestWorkTablePayloadSmallerThanFullRuntime -benchmem` 输出
  记录在 CI 日志；回归门槛以上述字节比为准，AllocsPerRun 只作观测。

## 手动冒烟

1. 长任务运行中观察右侧工作表格：工具事件密集时表格只更新变化行，不整表闪烁。
2. 子代理 fork 运行中：`subagent` 阶段行状态/打点实时刷新；点「详情」可看
   会话记录、上下文快照与工具活动。
3. todo 三态：行内按钮 未做/进行中/完成 往返切换，刷新快照后状态保持。

## 基线记录

2026-08-09（Windows，Go 1.25）：密集状态（20 节点 × 60 trace/节点 + 20 todo +
10 subagent）下 `worktable.changed` ≈ 34KB，整份 `runtime.changed` ≈ 300KB，
比值 ≈ 0.12，显著低于 0.30 门槛。
