# 一个会话冷加载，另一个会话「断掉」：视图过渡 key 不再覆盖整段装载

日期：2026-09-29
范围：`application/core/session_history.go`、`application/core/session_switch_probe_test.go`（改契约断言）

## 0. 一句话

冷加载的**装载**（磁盘三读 / wire 装配 / 引擎恢复 / 队列回填）此前整段跑在**视图过渡 key**
里，而这把 key 在生产宿主上是**全进程唯一**的一把（`seelebridge/runtime.go:718`
`PerSessionExecution()==false` ⇒ `transitionForSession` 一律回退 `transitionForKey("")`，
见 `session_scope.go:37`、`session_runtime/transition_manager.go:92`）。于是「A 冷加载」= 「别的会话
的 resume/submit 全部排队」，用户看到的就是「一个会话激发冷加载，另一个会话就容易断掉」。
另外，不激活视图的后台装载仍会无条件改写**进程级执行面**（实时 task 注册表归属、全进程子代理树），
把运行中会话的执行面抢走。

## 1. 红灯（两条，均已实测）

`application/core/session_cross_session_cold_load_repro_test.go`：

```
--- FAIL: TestReproColdLoadBlocksAnotherSessionSubmit
    （goroutine 现场：SubmitToSession → ActivateSession → (*Service).resumeSession
      在 transition_manager.go:53 的 lock 上排队，整段冷加载期间发不出消息）
--- FAIL: TestReproColdLoadStealsSharedRuntimeScopeFromRunningSession
    红灯：A 的后台冷加载（视图已切回运行中的 B，mayActivate=false）抢走了进程级执行面：
    实时 task 注册表归属变成 "sess-cold"（want "sess-b"）、ClearSubagentTree 调用=1 次（want 0）
```

## 2. 改法

1. `resumeSession` 拆成「判定意图 / 激活空壳 / 装载 / 按意图发布」四段，**只在判定视图意图与
   按 epoch 发布基线两段持 key**：
   - 同目标装载在途 → 幂等返回；
   - 已驻留（含运行中）→ 热挂载（bumpViewEpoch 后立即返回）；
   - 未驻留 → 先 `beginAsyncRestore` 激活目标 restoring 空壳并推进 epoch，**随即让出 key**，
     再在键外装载：空闲时前台同步等装载收口（保持「ResumeSession 返回 ⇒ 目标已装载，
     失败则视图回退到切换前会话」的契约，失败走 `handleColdRestoreFailure`），
     运行中则 `go resumeSessionColdInBackground(...)` 立即返回。
   - 随之删除只为同步分支存在的 `rollbackSyncResumeFailure`（其回退语义并入
     `handleColdRestoreFailure`，失败路径单一实现）。
2. `resumeSessionCold` 的**进程级执行面改写**（`SwitchSessionTasks` / `ClearSubagentTree` /
   `RestoreSubagentAnchors`）移到 `if mayActivate` 内：不激活视图的装载（目标已被更新的切换
   取代、视图不在目标上）只完成目标会话自身的状态装载（引擎驻留、可见投影、任务槽），
   不再抢运行中会话的执行面。位置仍在发布投影之前，紧随其后的 `publishRuntimeProjections`
   因此带上目标会话的工作表格/子代理树。
3. `mayActivate` 判定统一为「仍是最新视图意图（`service.viewEpoch == activateEpoch`）且视图仍
   指向目标会话」。两个调用点都传 `beginAsyncRestore` 给的 epoch，**没有**再传 0 的路径
   （旧语义里 `activateEpoch==0` 表示「无条件激活」的分支随之成为死代码）。

## 3. 契约翻转：`TestSwitchDuringColdLoadSerializes`

旧断言把「A 冷加载期间切 B 会一直等 A」记为**瓶颈证据**（用例注释原文：
「预期：视图 key 串行，B 等待 A 冷加载完成——记录该等待为瓶颈证据」）。这正是本次要修的行为，
因此用例随修复一起改判：**切到 B 不再等 A**（2s 超时视为失败 = 装载仍在占 key），
A 释放门闩后自身仍须正常收口，防死锁那半句保留。

## 4. 验收

- 两条红灯转绿；`TestSwitchDuringColdLoadSerializes` 在新契约下绿；
- `go test ./application/core/... ./seelebridge/ ./gui/ -count=1` 全绿（同族回归
  `session_switch_*`、`hot_attach_running_test.go`、`session_parallel_test.go`、
  `view_switch_isolation_test.go`、`session_history_hot_tail_test.go` 均未受影响）；
- 回归用例保留：`session_cross_session_cold_load_repro_test.go`。

## 5. 边界

- 本文件只改「锁面 + 发布判据」，不改冷加载要读什么、装什么；引擎恢复与队列回填顺序照旧。
- 冷加载与目标会话自身回合并发时的覆盖风险由既有判据承担（「目标可见会话非空则不安装恢复
  快照」），本次未放宽该判据。
- 空闲分支仍是**同步**语义（调用方等装载收口）：切会话的前端体验不变，变的是**别的会话**
  不再被它挡住。
