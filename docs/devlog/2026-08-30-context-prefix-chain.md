# 2026-08-30 上下文前缀链路（Context Prefix Chain）A/B/C 实现记录

## 范围

按 [docs/2026-08-30-context-prefix-chain/task-dispatch.md](../2026-08-30-context-prefix-chain/task-dispatch.md)
完成任务 A（装配顺序对齐 + checkpoint 正常路径移除）、B（累积 context 前缀 +
达峰才压缩）、C（plan/task 后置 + stacks 拆分），并同步设计文档与模块 README。
任务 D（fork todolist 过滤）为工作区既有实现，本批回归通过；E（全量回归）与
F（文档）由后续批次负责/已部分执行。

## 变更要点

### A：装配顺序 + checkpoint 移出

- `seelexctx/assembler.go`：`AssemblerOptions.StackBlocks` 拆分为
  `PrefixStacks`（skill/compact 稳定前缀）与 `TailStacks`（plan/task 尾部）；
  `Assemble` 投影改为 system → project → memory → 稳定前缀栈 → 调用方块 →
  WorkingHistory（累积 context）→ 尾部栈；`RenderStackBlocks` 拆分为
  `RenderStablePrefixBlocks` / `RenderTailBlocks`。
- `seelebridge/runtime_context.go`：主会话装配器接线 `PrefixStacks` /
  `TailStacks`；`stackBlocks` 保留为节点子代理继承兼容入口。
- `application/core/context_runtime/coordinator.go`：正常路径不再组装
  `checkpointMessage`（删除 `checkpointContextMessage`）；`fitExecutionHistory`
  顺序改为 system → 累积 context → plan 尾部。
- 恢复路径保留：`history_safety.go` 改用 `RetainedSystemOnly`（仅 system
  指令），checkpoint 续接信封不变。

### B：累积 context + 达峰才压缩

- `TranscriptTailHistory`：`maxUnits<=0` = 全量累积（append-only 已定稿轮次，
  字节稳定）；`>0` = 有界窗口（压缩后新鲜窗口）。
- `RetainedSystemHistory`：从「仅首条 system」扩展为「稳定前缀 + 已定稿轮次
  累积段」，剔除动态尾部（plan/checkpoint/压缩帧/恢复信封）；下一轮只追加
  保留段之后的新事件。
- `PrepareExecutionContextFor`：达峰判定以全量累积 context 为准（与引擎缓存
  估算取峰值）；软阈值触发压缩（发布 `ContextCompactions`）并切换有界新鲜
  窗口。

### C：plan/task 后置且不参与压缩

- plan 尾部内容克制：`RenderTailBlocks` 的 plan 帧只带 plan_id/title/status，
  整计划 nodes 不入尾部（节点详情由 plan 尾部消息与 `read_plan` 提供）。
- `ActivePlanContextMarker` 作为控制标记：plan 尾部消息不参与轮次单元切分
  （`isStackContextMarker` / `chatUnits`），即不参与压缩；也不作为记忆查询源。

## 验证

- `go build ./...` 通过。
- `go test ./application/core ./seelexctx ./seelebridge -count=1` 全绿。
- `go test ./application/core ./seelexctx -race -count=1` 全绿。
- 新增/更新契约测试：投影顺序、正常路径无 checkpoint、累积字节稳定、
  TranscriptTailHistory 全量累积、保留段语义、plan 尾部克制、plan 消息不参与
  压缩单元。
- `git diff --check` 无空白错误；`go vet`（context_runtime / task_context /
  seelexctx / seelebridge）通过。

## 相关 commit

见上下文前缀链路 A/B/C + 文档同步提交（本批次）。
