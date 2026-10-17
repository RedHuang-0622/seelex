# 任务书：修复 `application/core` 三个基线红

> **状态：三条已全绿（`4388524c` 的压缩有效性修复 + 复跑验证）。请勿重复认领。**
> 本文件保留为现象与复现记录，不再作为开工指令。

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

---

## 进度打点（2026-10-17，已完成）

### 状态

- 已认领并完成；未改任何测试期望、fixture 或断言。
- 修复提交：`4388524c`（`fix(context): 压缩有效性按软线判定，自主压缩不再误伤完整前缀`），
  仅改 `backend/engine/context/coordinator.go`。
- 三条基线红全部转绿；`./application/core/`、`./backend/engine/context/...`、
  `./middle/switch/...`、`./e2e/` 均通过。

### 三条用例的根因与修复点

三条红共用同一个决策分叉：**这一轮压缩值不值得做**。此前有两处判断失真：

1. **无余量压缩不幂等**（`TestContextBudgetSkipsCompactionWithoutMargin`）
   - 现场数字：回合 3 `raw=150137` ≥ 硬线 `150127`，保留区 `133446`，固定开销 `5810`，
     压缩落点 `139256`。
   - 根因：`ineffectiveCompact` 原先按 `budget.HardThreshold` 比较。2026-09-30
     `budget.SoftThreshold` 被改成“报表用的硬线同值”后，这条校验实际拿硬线比较，
     `139256 < 150127` 被判成“有余量”，于是真正落了一条压缩记录；下一轮以同一个数字
     再越线。无效压缩还会写一条 `Failed` 失败痕，进一步提升记录条数。
   - 修复：新增 `compactionSoftLine`，复用
     `seelectx.ContextWindowPolicy.SoftThreshold()`（`limits.context_soft_percent × 预算`）
     作为有效性判据线；`139256 ≥ 100084`（测试注入 soft=60%）→ 跳过。无效压缩是
     判据前置的主动跳过，不再写失败痕/压缩记录，只留瞬态进度。

2. **运行中会话只保留最后一轮**（`TestRunningSessionAccumulatesAllRoundsInOrderControl`）
   - 现场数字：`raw=6038` ≥ 硬线 `6021`，但 transcript 只有 `all=231`，
     固定开销 `5807`；`retain=161`（231 × 0.7），落点 `5968`。
   - 根因：旧的硬线比较认为 `5968 < 6021` 有余量 → `compacting=true`，
     装配目标被设成 `retain=161`，把 24 条消息裁到最后一轮；随后自主压缩又把
     历史换成 checkpoint 帧。
   - 修复：改用软线 `5836` 判定，`5968 ≥ 5836` → 无效压缩跳过，目标回到全量预算
     `6144`，24 条消息完整保留。自主压缩只在“装不进全量预算”或“真的折过有界窗口
     且仍贴硬线”时触发，不再因固定开销贴线而误伤 transcript。

3. **冷恢复后只剩最后一轮**（`TestColdRestoreTailFirstRequestKeepsTranscriptOrder`）
   - 现场数字与第 2 条相同；冷恢复只装载尾部 8 条消息，但尾部不是 transcript 前缀。
   - 根因：`retainedMatchesTranscriptPrefix` 已能识别“尾窗不是前缀”并回退全量重建；
     真正把它裁成最后一轮的是同一条错误压缩判据与 `target=161`。
   - 修复：同第 2 条；修复后首个请求按原序重建 `user-00 answer-00 … user-11 answer-11`。

### 红灯证据

在 HEAD `3d2a9ab7` 的干净副本（不动共享脏工作区）复现到与任务书完全一致的失败：

```text
--- FAIL: TestContextBudgetSkipsCompactionWithoutMargin
    context_budget_margin_idempotency_test.go:74: 回合 3 又落记录（无余量的压缩未做幂等校验）: 1 条
--- FAIL: TestRunningSessionAccumulatesAllRoundsInOrderControl
    context_restore_prefix_order_test.go:34: running-mode context order = [user-11 answer-11], want original
--- FAIL: TestColdRestoreTailFirstRequestKeepsTranscriptOrder
    context_restore_prefix_order_test.go:65: after cold restore context order = [user-11 answer-11], want original [...]
```

### 修复后验证

```text
go test ./application/core/ -run "TestContextBudgetSkipsCompactionWithoutMargin|TestRunningSessionAccumulatesAllRoundsInOrderControl|TestColdRestoreTailFirstRequestKeepsTranscriptOrder" -count=1 -timeout 300s
ok

go test ./application/core/ -count=1 -timeout 900s
ok   (52.9s)

go test ./backend/engine/context/... ./middle/switch/... -count=1 -timeout 900s
ok   （全部子包）

go test ./... -count=1 -timeout 2400s
全部包 ok；唯一一次 e2e 依赖闭包 ratchet 失败在并发改动落定后单独复跑
`go test ./e2e/ -count=1` 已全绿（33.3s），不属于本修复引入的回归。
```

此外 `gofmt -l backend/engine/context/coordinator.go` 无输出，`git diff --check` 通过。

### 阻塞点与说明

- **共享脏工作区**：本任务期间另一条流正在做 NodeScope/SceneScope 改名，曾短暂导致
  `model.SceneScopeFromContext` 未定义、`application/core` 无法编译；未触碰该改名工作，
  待其落定后在本工作区完成验证。全量测试结果因此会受并发改动影响。
- **e2e 瞬态失败**：全量运行中 `TestFacadeDependencyClosureRatchet` 曾报“门面依赖闭包
  新增了未登记的 Seelex 包”，是并发工作区包集合变化导致的瞬态；单独复跑该用例与
  `./e2e/` 均通过。
- **沙箱限制**：沙箱内有一批 `project scope: resolve root links: Access is denied`
  的环境假失败，已在沙箱外复跑确认通过；本修复不涉及路径/权限代码。
- **提交权限**：`.git` 在沙箱内只读，提交需一次授权；提交后的仓库 hook 自动重建了
  `dist/dev/seelex.exe` 与 `dist/seelex-gui-dev/seelex-gui.exe`，未执行任何 clean/删除。
