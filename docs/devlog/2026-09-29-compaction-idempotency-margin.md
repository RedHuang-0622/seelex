# 上下文压缩「一轮一条记录」：折叠的幂等/有效性校验 + 出厂档余量上调

日期：2026-09-29
范围：`application/core/context_runtime/coordinator.go`、`config/seelex.yaml`（+ `internal/bootseed/assets/config/seelex.yaml` 逐字节副本）

## 0. 一句话

折叠只裁 transcript 一侧，而装配又把**整条请求**收口到保留窗口决策的落点上；当落点仍在软线
之上时，这次折叠换不来任何余量——下一轮达峰判据会用同一个数字再越线，于是同一个会话**每轮都
压一次**。装配层现在先做**幂等/有效性校验**：落点够不到软线就不折（把额度让给硬线/自主压缩），
同时把出厂档的余量从 15% 提到 30%，让「折一次能撑一阵」这件事在配置上也成立。

## 1. 现场

用户报告「折叠执行之后的下一轮对话压缩已经不见所踪」「一发消息一条压缩记录」。会话
`session-48c05322bb9e6f1a` 的 `compact.jsonl` 里 3.5 分钟内落了 3 条记录：

| 记录 | 区间 | 判据量 | 时间 |
| --- | --- | --- | --- |
| 帧 1 | `message-1..message-522`（事件 1..604） | 205,309 token | 21:19:52 |
| 帧 2 | `message_from="" / message_to=""`（事件 1..474） | — | 21:21:42 |
| 帧 3 | `message-1..message-830`（事件 1..951） | 162,530 token | 21:23:19 |

三帧同 scope、`origin=auto`、判据都贴着软线；面板上全是「上下文预算达峰 · 自动阈值」。

机制：`budget.SoftThreshold = budget×95%`、`TargetAfterCompaction = budget×80%`（旧档），
余量 `soft − target = 15%` 预算 ≈ 25k token；而该会话**单轮增长可达 +29k** ⇒ 折完下一轮就
再越线。更关键的是判据量含**固定开销**（system 稳定层 + plan + 工具 + 当轮输入，不参与折叠），
而 `retain.Retained` 是落点上限：`retained + overhead ≥ soft` 时，折叠在数学上就不可能把请求
降回软线以下——这类折叠是**无效折叠**，做了也不会改变下一轮结论。

## 2. 改法（`coordinator.go`）

1. 保留窗口决策**提前**到判据之前并复用（`retain := retainWindowDecision(...)`），下方折叠分支
   不再重算第二遍；同时算出 `requestOverhead = rawTokens − allContextTokens`（固定开销）。
2. 新增有效性校验：

   ```go
   ineffectiveFold := fold && !options.forceCompact && !options.maintenanceFold && !hardCompact &&
       retain.Retained+requestOverhead >= budget.SoftThreshold
   ```

   `newCheckpoint` 与 `compacting` 都 `&& !ineffectiveFold`；跳过时终局 Detail 落
   `skipped=ineffective_fold landing=… soft=… overhead=… retained=… all=…`，判据关 Detail 加
   `overhead=` / `ineffective=`，读进度的人能自答「为什么这次没压」。
3. **维护入口豁免**（`prepareOptions.maintenanceFold`）：`CompactTaskContextFor` 是引擎迭代
   hook / 控制器驱动的**维护入口**——调用目的本身就是要折出有界 checkpoint，跳过即违约。第一次
   落盘时把它排除后，两条既有用例由红转绿：

   ```
   --- FAIL: TestContextControllerCompactsAndCleansInternalCheckpoint
       task_execution_test.go:153: history = [{Role:system …}], want system and recent complete units
   --- FAIL: TestContextControllerRepeatedCompactionDoesNotAccumulateCheckpoints
       task_execution_test.go:231: visible compactions = []model.ContextCompaction(nil)
   ```

## 3. 出厂档（`config/seelex.yaml`）

| 键 | 旧 | 新 | 理由 |
| --- | --- | --- | --- |
| `context_safety_reserve_divisor` | 8 | 10 | 安全预留 = 窗口 ÷ 10，预算略微收紧，靠近上限前先让出空间 |
| `context_target_percent` | 80 | 65 | 余量 `soft − target` 15% → 30%：一轮增长（现场 +29k）不再必然重新越线 |
| `context_frame_carry_tokens` | 1024 | 4096 | 现场两条帧的 Chapter 2 正文是 4159 / 3087 token，原上限让**每条**帧在并入那一步都退化成锚点 |

代码内置兜底档（配置缺失时）保持 8/95/98/80/50 不动：`seelexctx/limits_test.go` 与
`TestLoadLimitsCompactionBudgetRatios` 钉的是那一份，出厂档只动 YAML 两份（`config/`
与 `internal/bootseed/assets/config/`，`boot_seed_test.go:63` 要求逐字节相同，已核对）。

## 4. 有牙证明

新增 `application/core/context_budget_margin_idempotency_test.go`
（`pinIneffectiveFoldRatios` 8/60/90/80/50 + `applyWindowConfig(WindowConfig{Ratio: 1})` +
16 轮 × 32k 字符）：首个装配与随后两回合都不得落记录、`compactionRecords` 恒 0。

把 `coordinator.go` 单独 `git stash` 回旧实现（只留新用例）后跑：

```
--- FAIL: TestContextBudgetSkipsFoldWithoutMargin
    无余量的折叠应被跳过，却落了 1 条记录 … EstimatedTokens:132779（软线 100,084）
```

`git stash pop` 后同一用例 ok。

## 5. 验收

- `go build ./...` ok；
- `go test ./application/core/... -count=1` 全绿（含 `TestContextController*`、新用例、
  `context_compact_*`、`session_history_*`）；`go test ./seelexctx/... ./sessionstore/... ./seelebridge/ -count=1` 全绿；
- 出厂 YAML 与 bootseed 副本 `Get-FileHash` 一致。

## 6. 边界

- 校验只挡**软线自动路径**：显式（`/compact`、`compact_context`）、硬阈值与维护入口照旧折叠。
- 帧很小/折叠内容偏少是另一条链（`context_frame_carry_tokens` 与厚摘要开关），本文件只上调了
  并入上限；「该不该用被折区间原文当素材」属于另一个判断，本轮不猜。
- 出厂档改动要**重启/重建**才对运行实例生效（`dist/seelex-gui-dev/config/seelex.yaml` 由 dev
  构建脚本同步）。
