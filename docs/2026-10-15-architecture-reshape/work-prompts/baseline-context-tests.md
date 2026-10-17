# 任务书：修复 `application/core` 三个基线红

## 现象

这三例在架构重排前就红，迁移后逐条同名；它们不是搬迁引入的，但属于用户可见的上下文
恢复/压缩行为问题。不要改测试期望来“变绿”。

### 1. 无余量压缩不幂等

```text
--- FAIL: TestContextBudgetSkipsCompactionWithoutMargin
    context_budget_margin_idempotency_test.go:74: 回合 3 又落记录（无余量的压缩未做幂等校验）: 1 条
```

预期：同一份上下文在没有余量时，压缩记录不应在后续回合重复落账。

### 2. 运行中会话只保留最后一轮

```text
--- FAIL: TestRunningSessionAccumulatesAllRoundsInOrderControl
    context_restore_prefix_order_test.go:34: running-mode context order = [user-11 answer-11], want original
```

预期：运行中会话的上下文顺序应包含全部轮次（`user-00 answer-00 … user-11 answer-11`），
而不是只剩最后一轮。

### 3. 冷恢复后只剩最后一轮

```text
--- FAIL: TestColdRestoreTailFirstRequestKeepsTranscriptOrder
    context_restore_prefix_order_test.go:65: after cold restore context order = [user-11 answer-11], want original [user-00 answer-00 … user-11 answer-11]
```

预期：冷恢复后的第一次请求，上下文仍按原会话顺序重建全部轮次；不能因为“tail-first”
只带最后一轮。

## 复现命令

```text
go test ./application/core/ -run "TestContextBudgetSkipsCompactionWithoutMargin|TestRunningSessionAccumulatesAllRoundsInOrderControl|TestColdRestoreTailFirstRequestKeepsTranscriptOrder" -count=1 -timeout 300s
```

## 你的任务

1. 先读三条用例与它们调用的恢复/压缩路径，写出根因假设（是预算裁剪、tail-first 选择、
   还是写入点重复）。
2. 红灯先行：如果现有用例没有覆盖真正的分叉点，补一条会红的更小用例；不要改原有期望。
3. 修实现，不动测试语义。重点检查：
   - `application/core/context_*` / `context_restore_*`；
   - `backend/engine/context/**` 的 compactor / snapshot 恢复；
   - `middle/switch/sessionusecase` 的历史重建与 `TaskService` 的上下文裁剪。
4. 修完后跑：

```text
go test ./application/core/ -count=1 -timeout 900s
go test ./backend/engine/context/... ./middle/switch/... -count=1 -timeout 900s
go test ./... -count=1 -timeout 2400s
```

## 约束

- 不要动 NodeScope/SceneScope 的改名工作。
- 不要用“跳过测试 / 放宽断言 / 改 fixture”收口。
- 一个根因一个 commit；提交到 `refactor/architecture-reshape`。

交付时给出：三条用例各自根因、修复点、红灯证据、修复后全量结果。
