# 锁面普查：折叠落点修复之后，别处还有没有自锁/死锁（2026-09-29）

> 范围：全仓（`application/**`、`seelebridge/**`、`sessionstore/**`、`session/**`、
> `seelexctx/**`、`internal/**`、`plugin/**`、`skill/**`、`mcpstack/**`、`gui/**`、
> `workspace/**`）
> 起因：[2026-09-29 装配层折叠持 ViewMu 推帧](2026-09-29-compaction-fold-lock-granularity.md)
> 已确认的确定性自锁（同 goroutine 持写锁再取读锁）修掉后，用户要求"其他地方也都
> 看看有没有自锁或者死锁"。
> 本文是**只读审查**记录：不改代码、不新增测试。所有条目都带 file:line；判定分
> **确认**（我已沿调用链读到可复现路径）与**待证**（缺一环，写明补证方法）。
> 关联：[2026-09-23 message 读路径与写者解耦](2026-09-23-message-read-path-decoupled-from-writer-lock.md)、
> [2026-09-23 迭代边界注入撞会话锁](2026-09-23-iteration-hook-session-lock-reentry.md)。

## 0. 结论（一句话）

**没有再发现"同 goroutine 重入非重入锁"形态的确定性自锁**（这是本次修复那一类：
全仓函数内 `Lock→Lock`/`Lock→RLock` 机械扫描在生产代码里 0 命中）；但找到 **13 处
同族风险面**，按"会不会永久挂死"排序后最要紧的三处是：
`sessionstore/json_layout.go:446` 的**锁前提违例**（会在无锁下发布 message head →
已提交行可被回收）、`seelebridge/runtime_role_turn.go:198` 的**持锁跑整轮宿主实现**
（ABBA/自锁隐患）、`application/core/session_scope.go:464` 的**持 ViewMu 调宿主端口**
（含文件 I/O）。其余为"锁持有时间 = 慢活耗时"与"纪律自相矛盾"两类。

## 1. 方法（可复跑）

1. **机械筛自锁**：`_scratch/lock_scan.py`（本次新增的临时脚本）逐函数跟踪
   接收者表达式的取/放锁深度，报「同一函数内同接收者二次取锁且中间无释放」
   （即 `sync.Mutex`/`RWMutex` 重入）与「持锁区间内出现 channel 接收 / `Wait()` /
   `Acquire()` / `Sleep`」。
   > 生产代码：nested-acquire **0 命中**；hold-while-blocking 唯一命中
   > `sessionstore/sessionstore.go:1036 router.opsCond.Wait()`（`sync.Cond` 语义，
   > 合法，Wait 会释放 opsMu）。命中集中在测试桩（`task_service_test.go:55`、
   > `engine_port_lockfuse_test.go:23` 是注释、`session_runtime/*_test.go`）。
2. **机械筛"持锁调外部"**：`_scratch/port_in_locked.py` 列 `*Locked` 函数体内的
   端口调用；`_scratch/viewmu_scan.py` 列 `Core.ViewMu` 临界区内的跨模块调用，
   再按 `Deps.`（宿主注入实现）过滤——只有 5 行命中，去掉 2 处 nil 判断与 1 处
   测试，真正"ViewMu 内调宿主端口"是 `application/core/plan_tools.go:332` 与
   `application/core/session_scope.go:512`（见 §2.3、§2.4）。
3. **逐锁人工核对**：对每把锁枚举"持锁期间调用了什么"，并机械核对注释里写的前置
   条件（「调用方持有 X」「不得重入」「叶子锁」「锁内不得 I/O」）与真实调用点。
4. **分簇并行初审**（seelebridge / sessionstore+session+workspace / seelexctx+
   adapters+event+plugin+gui 三簇），**本文只收录我本人复核过 file:line 的条目**；
   子代理提出但未复核的降级进 §3。

---

## 2. 确认项（我已读到调用链）

### 2.1 [确认·锁前提违例 → 已提交行可被回收] `json_layout.go:446` 无锁调用 `*Locked` 读入口

- 位置：`sessionstore/json_layout.go:431-455`（`retentionAdvisoryWorkspace`，读点在 `:446`）。
- 触发链：`Router.RetentionAdvisoryWorkspace`（`sessionstore/runtime_api.go:74-83`，
  只 `router.mu.RLock` 取 repository 指针，**不持任何模块锁**）
  → `repository.retentionAdvisoryWorkspace`（`json_layout.go:431`）
  → `repository.layout.readMessageHeadLocked(key)`（`:446`）
  → `readModuleHeadFileLocked`（`module_heads.go:346`）→
  `readModuleHeadFileHeal(..., lockHeld=true)`（`:350-372`）→ head 校验两次失败 →
  `repairModuleHeadLocked`（`:462`）→ `publishModuleHead`（`:289`）**在零模块锁下**
  原子替换 `metadata/message.json`。
- 为什么是缺陷（判据来自代码自身）：
  - 同函数上一行 `readCompactHead` / `readRetentionHead` 用的是**无锁变体**
    （它们内部走 `TryLock`，拿不到锁就"有界失败"不发布，见 `module_heads.go:331-345`
    的注释），只有 message 这一行用了 `*Locked` —— 形态上就是笔误。
  - `module_heads.go:331-345` 明文写：重建发布**必须持该模块锁**，否则"会覆盖并发
    writer 刚发布的 head（丢更新），或把 append 完成但 head 未发布的行提升为本已
    提交"。
  - 后果链：并发的 `messageCommitSyncLocked`（`message_rows.go:361` 起，持 messageMu）
    与这次无锁发布交错，若把 head 回退到更早的 `LastSeq`，下一次提交的
    `reapUnpublishedLocked`（`message_rows.go:500`）会按 head 截断尾分片 →
    **已提交行被物理删除**（"append 完成但 head 未发布 = 未提交"这条崩溃恢复语义
    被反向触发）。"行真的被删"是概率窗口，但**前提违例本身是确定的**。
- 修法（二选一，都是一处）：
  (a) 改成无锁入口 `readMessageHead`（与同函数另两个读一致，自愈走 TryLock，绝不发布）；
  (b) 在 advisory 路径显式 `store.mu(key, moduleMessage).Lock()` 后再读（读路径持锁，
      与本仓"读路径不持模块锁"的取向相反，故推荐 (a)）。
- 复现：`module_heads.go:132` 的 `readSelfHealHook` 注释写明"测试用它模拟 writer 在
  两次读取之间完成原子替换"——在 hook 里并发跑一次 `messageCommit`，断言无锁发布
  未发生 / `head.LastSeq` 单调不回退。

### 2.2 [确认·持锁跨整轮宿主实现，有自锁/ABBA 隐患] `runtime_role_turn.go:198-204`

- 位置：`seelebridge/runtime_role_turn.go:198-204`（`runRoleRound`）。
- 形态：`handle.mu.Lock()` 包住 `handle.engine.ChatStream(turnCtx, spec.Input, spec.OnDelta)`
  ——整轮 ReAct（模型调用 + 工具执行 + 审批等待 + 子进程），且 `handle.engine` 是
  **宿主注入实现**（`SetRoleEngineFactory`）或框架 `Session`；`spec.FreshContext` 时
  还在锁内 `handle.engine.ClearHistory()`。
- 上游锁序：`application/core/goal/techleader.go:400-402 RunEval` / `runRoundLocked`
  （注释"调用方持 `s.mu`"）已持 `s.mu` 才走到 `RoleTurn`。因此既有的边是
  `s.mu → handle.mu`。
- 隐患两种：
  1. **同 goroutine 自锁**：角色回合内的任何路径（工具执行/回调）同步再发起同一
     `RoleSessionID` 的一轮 → 卡在 `handle.mu.Lock()`（`sync.Mutex` 不可重入）。
  2. **ABBA**：只要出现一条 `handle.mu → s.mu`（持锁跑回合期间回调治理域），与上游
     `s.mu → handle.mu` 成环。
- 附带契约缺口：`techleader.go:161/166` 的 `noteInFlightLocked` 依赖"与持锁者同
  goroutine 的流式回调"这一前置条件（该函数无锁写 `s.inFlight`），而 seelebridge 侧
  （`runtime_role_turn.go:203`）把 `spec.OnDelta` 原样交给引擎 `onChunk`，既未同步
  封装也未写明约束。
- 修法：把串行化从"持锁跑回合"换成**按角色会话键的窄闸门**（`compactionPushLock`
  同款：准入在内、执行在外），`handle.mu` 只护"取/建引擎"的短临界区；`ChatStream`
  与 `ClearHistory` 移出锁外。
- 补证（可确定性复现）：注入一个 `roleEngine` 桩，其 `ChatStream` 内同步调用
  `Runtime.RunRoleTurn(ctx, RoleTurnRequest{RoleSessionID: 同一个 ID})` → 必停在
  `handle.mu.Lock()`。

### 2.3 [确认·ViewMu 写锁内调宿主注入实现] `plan_tools.go:331-335`

- 位置：`application/core/plan_tools.go:331-335`（`handleBackgroundPlanNodeComplete`）：
  `service.ViewMu.Lock()` → `service.Core.Snapshot.Runtime.SubAgentTree = service.Deps.Engine.SubAgentTree()`
  → `bumpLocked()` → `Unlock()`。
- 为什么算同族：这正是本次事故的形状——**持全局视图写锁调宿主任意实现**。宿主实现
  今天是安全的（`seelebridge/ports.go:388-393` → `session/subagent_tree.go:456-520`
  的两阶段投影：持树锁只浅拍、释放后组 DTO、不读运行中会话、不取会话锁），但
  "安全"是宿主侧的实现不变式，application 侧无从强制：当年归档器也只是"读
  `app.Snapshot()`"，契约一漂移就是永久自锁。
- 加固（1 行移动，零语义变化）：把 `service.Deps.Engine.SubAgentTree()` 提到
  `ViewMu.Lock()` 之前取值，锁内只做赋值（纯值拷贝）。

### 2.4 [确认·ViewMu 读锁内调宿主端口 + 文件 I/O] `session_scope.go:463-516`

- 位置：`application/core/session_scope.go:463-466`（`snapshotOfResident`，
  `ViewMu.RLock()` + `defer RUnlock()`），锁内调：
  `service.Approval.PendingBySession`（`:495`）、`service.asyncRunsForTable()`（`:510`
  → `Deps.Runtime.AsyncRunsSnapshot()`）、`service.Deps.Runtime.TaskSnapshot()`（`:512`）。
- 两个问题：
  1. **持读锁等写者**的经典变体：`RWMutex` 下若已有 writer 排队，同 goroutine 的
     第二次 `RLock` 会阻塞；因此只要任一被调方**回调**进 `Core.ViewMu`（例如宿主
     实现回读 `app.Snapshot()`），就是"自己等自己"，永不释放。今天这些实现不回调，
     属"同一类契约依赖"。
  2. 锁内做 I/O（见 §2.5）。

### 2.5 [确认·全局视图写锁内做文件 I/O] 工作表格重投影

- 位置：`application/core/work_table.go:355-363`（`publishTaskDeltas`：`ViewMu.Lock()`
  → `refreshWorkTableLocked(tasks)`）→ `work_table.go:250-259` → `asyncRunsForTable()`
  （`work_table_async.go`）→ `Deps.Runtime.AsyncRunsSnapshot()`（`seelebridge/runtime_async.go`）
  → `Router.AsyncRuns()` → `asyncRegistry.infos()`（`seelebridge/tools/async_probe.go:113-141`）
  → 对表内**每一条**记录 `sampleLog()`（`:154-176`：`os.Stat` + `os.Open` + `ReadAt` 末窗）。
- 判据：`async_probe.go` 的注释自己写"文件 I/O（stat + 读末窗）一律在锁外做：一条命令
  狂写日志时，探针不得把派发/收尾路径挡住"——那是对 `g.mu` 说的，而**调用方持有的是
  `Core.ViewMu` 写锁**（全进程视图锁）。于是"每次 task 增量刷新"的代价 = 后台作业条数
  × 3 次系统调用，且持写锁。与本次修复的后果②同形（锁的持有时间 = 慢活耗时）。
- 加固：把 `AsyncRunsSnapshot()`（与 `TaskSnapshot()`）在**锁外**采样一次、作为值参数
  传入 `refreshWorkTableLocked`（三个建表入口 + `view_state.RefreshWorkTableLocked`
  端口共用同一签名）。

### 2.6 [确认·锁纪律自相矛盾：锁内做文件 I/O] `seelebridge/tools/async_exec.go`

- 位置：`beginJob`（`:230` `g.mu.Lock()` + `defer Unlock`）锁内调
  `g.evictLocked()`（`:249`）与 `g.directory()`（`:250`）；`evictLocked` 内含
  `_ = os.Remove(oldest.logPath)`（`:331`），`directory()` 内含 `os.MkdirTemp`；
  `close()`（`:799-804`）锁内调 `removeDirLocked()` → `os.RemoveAll`。
- 同文件的反面范式就在旁边：`killTarget` 的注释（`:341-345`）"终止动作一律在锁外做
  ——`finish` 也要这把锁，等 `taskkill` 返回不能把整张表按住"；`noteOutput/notifyLocked`
  （`:770-797`）把信号做成非阻塞。即**自家纪律写清楚了，这三处没照做**。
- 后果（非永久自锁）：慢盘/大目录下，持 `g.mu` 的 `RemoveAll/MkdirTemp/Remove` 会把
  子进程日志写入（`cappedLogWriter.Write` → `noteOutput` 需要 `g.mu`）、派发、探针一起
  按住。修法：`retire` 已是正确范式（锁内摘表 → 锁外删文件），照它改三处。

### 2.7 [确认·`port.mu` 内跑宿主注入回调] `internal/adapters/engine_port.go`

- 位置：`replaceRawHistoryFor`（`:492-501`）、`installSessionEngineLocked`（`:590-623`）、
  `ResumeRawSession`（`:739-745`）在 `port.mu` 内调 `port.prepareHistory(sessionID, desired)`
  与引擎历史方法（`installHistoryInPlace` 的 Clear/Append、Resume 的整段重建）。
- `prepareHistory` 是宿主注入闭包（`main.go` 装配 → `Runtime.PrepareMainSessionHistory`
  → `sessionBindings.mu` → `sessionstore.DurableHistory`），即"持进程级锁调宿主实现 +
  落盘"；`internal/adapters/README.md` 自称"`port.mu` 内不做跨越等待的操作"，与实现不符；
  `engine_port.go:89-91` 对 `PrepareHistory` 的注释（"启动期配置，必须在并发开始前完成"）
  也与"每次历史替换都调用"不符。
- 现状：四条折叠路径已由 `engine_port_lockfuse_test.go` 钉住，但
  `ResumeRawSession` / `ReleaseWorkingHistoryFor` / `installPendingLocked` 不在该批判据里。
  修法：锁内只做注册表改写与收集，`port.mu.Unlock()` 之后再执行 `prepareHistory`。

### 2.8 [确认·锁内跨模块落盘] `sessionstore/session_context.go:811-819`

- `update` 持 `s.mu.Lock()` 执行闭包 → `PushCompact`（`:743`）→ `bridgeCompactFrame`
  （`:776/792/801`）→ `Router.CommitCompactFrameWorkspace`（`sessionstore/runtime_api.go:64`）
  → `compactCommit`（取 `compactMu`，再取 `messageMu`）→ 末尾还调
  `RetentionAdvisoryWorkspace`（即 §2.1 那条无锁入口）。锁序 `s.mu → compactMu → messageMu`
  今天无反向边（不是死锁），但把 UI 侧读（`Snapshot`/`SystemPrompt`）按在两次落盘之间。
  修法：照同文件 `AppendGoalAudit`（`:717-731`）口径，锁内只改内存、写盘移到 `update` 之后。

### 2.9 [确认·持锁做整段重建 + 整文件重写] `sessionstore/event_store.go:86-91`

- `EventStore.Append` 持 `store.mu` 跨越"全量读事件库 → merge → 原子整文件重写"
  （`sessionstore.go:1230-1348`、`writeAtomic` `:1643`，含 rename 退避 sleep）。
  `store.mu` 是 EventStore 实例级、**跨会话共用** → 所有会话的事实落库在一把锁上排队，
  且临界区长度随事件库体积线性增长。不是死锁（无反向边），属"(读路径/写路径)持锁做
  整段解码/写盘"这一族（与 2026-09-23 那条同形）。

### 2.10 [确认·潜在自锁埋点 + 读路径持独占锁] `moduleMedia` 被静默别名到 `messageMu`

- 位置：`sessionstore/module_heads.go:183-215`（`mutexFor` 的 `default: return &locks.messageMu`）
  ——`sessionModuleLocks`（`:105-121`）**没有 `mediaMu` 字段**；消费点
  `sessionstore/media.go:201-241`（`repository.layout.mu(key, moduleMedia)`）。
- 后果一（现状）：`ReadMedia`/`ListMedia` 拿的是**独占锁**，与 `messageCommit`
  （`message_rows.go:361`）同锁 → 截图/媒体落盘与消息提交互相串行（与
  `sessionstore/README.md` 的"media 与 message 各自加锁，不要跨模块持锁"相反）。
- 后果二（埋点）：`sync.Mutex` 不可重入 → 将来任何"在 message 临界区内读写媒体"的
  调用即永久自锁。
- 修法：补 `mediaMu` 字段 + `case moduleMedia`；`default` 改成显式失败（禁止静默别名）。

### 2.11 [确认·数据竞争（非死锁）] `seelebridge/plan/executor.go` 锁外解引用锁内指针

- 位置：`PolicyFor`（`:172-176`）、同形 `BindingFor`（`:212-216`）、`CurrentRunIDFor`
  （`:441-445`）：
  `slotMu.RLock(); slot := readSlot(sessionID); slotMu.RUnlock(); return slot.policy`。
  `readSlot`（`:90-104`）命中时**返回 map 里的指针**；写侧 `SetPolicyFor`（`:162-164`）
  在 `slotMu.Lock()` 内写 `slot.policy`。注释 `:90` 写的是"readSlot 返回指定会话槽
  （**读锁内调用**）"，实现把解引用放到了锁外。
- 修法：锁内取值拷贝返回（`policy := slot.policy`），或恢复注释契约；`-race` 可复现。

### 2.12 [确认·持全局发布锁等另一组件（不永久死锁，吞吞吐）] 事件谓词里的 actor 往返

- 位置：`application/event/hub.go:320-336`（`publish` 全程持 `publishMu`）→
  `deliver`（`:381-421`，持 `subscriber.mu`）→ `subscriber.filter(event)`；
  生产谓词 `application/core/session_scope.go:549-570`（`sessionEventFilter`）在**空 sid
  （跟随视图）**分支每次求值调 `service.sessions.ActiveID()` →
  `session/domain_actor.go:174` `domain.call(...)`（**同步 actor 往返**，投递命令后等回包）。
- 判据冲突：`hub.go:225-227` 明文要求谓词"必须无阻塞、无副作用（由发布 goroutine 执行）"。
- 实际影响面：GUI 正常期用显式 sid 订阅（`gui/bridge.go:400-420`，谓词退化为字符串比较），
  只有草稿/占位期（sid 为空）会走空分支——那时每次事件发布被一次 actor RPC 串行化。
  彻底成环需要"actor 自身参与发布"，当前 actor loop 只做查表与回包。
- 修法：把谓词求值移出 `publishMu`（`hub.mu` 已经这样做了），或把视图指针缓存进
  `atomic.Value`，让谓词无阻塞。

### 2.13 [确认·绕过存储切换栅栏] `sessionstore/runtime_api.go` 的直连入口不登记 `activeOps`

- 位置：`sessionstore/runtime_api.go:29-83`（`AssembleWireWorkspace` /
  `CommitCompactFrameWorkspace` / `RetentionAdvisoryWorkspace` / `LRUDeleteWorkspace` …）：
  只 `router.mu.RLock()` 取 repository 指针后**在锁外**执行，从不 `activeOps++`；
  而 `Router.Configure`（`sessionstore/sessionstore.go:941-1000`）在写锁内只等
  `waitOpsIdleLocked`（`:1032-1039`，`opsCond.Wait()`）即 swap，随后 `old.Close()`
  并释放 data root lock。
- 后果：存储切换/关闭可与旧 backend 上的长读写在飞行中重叠（根目录被删、数据根锁被
  释放）。不是死锁，但同属锁面缺陷（`sessionstore.go:293-299` 注释宣称的"Close 只在
  活跃操作归零后关闭旧后端"对这些入口不成立）。
- 修法：这 15 个入口统一改走 `withRepositoryAt`（或在 `RLock` 内完成 `activeOps++`
  再放指针）。

### 2.14 [nit·确认] 纯读也取写锁

- `seelebridge/runtime.go:516-522` `Runtime.Session()` 用 `bundle.mu.Lock()` 只为返回
  `bundle.session`（纯读）；同包 `CurrentSession()` 同形。→ 装配/切换期间读会话要排写锁。
  修法：改 `RLock`。

---

## 3. 待证项（缺一环，写明补证）

| # | 位置 | 缺的那一环 | 补证方法 |
|---|---|---|---|
| H1 | `session/manager.go:100-107/109-116`（`SaveCurrent/Resume`）持 `m.mu` 调注入回调（`router.Save` + `EnginePort.RawHistory/ReplaceRawHistory`） | 需要一条"持 `EnginePort.mu`/router 写锁时回调 `Manager`"的反向边（全仓 `Sessions.SaveCurrent` 只有 `application/core/session_runtime/archive.go:92`、`workspace_usecase.go:121` 两处，均在 `ViewMu`/`port.mu` 之外） | 给 `m.mu` 加持有者断言 + 内部 sleep，并发归档/切会话压测；判据复用 `engine_port_lockfuse_test.go` |
| H2 | `seelebridge/runtime.go:519-521`（`newMainSession` 持 `bundle.mu` 调 `tasks.SetDefaultIdentity` → `TaskRegistry` 无缓冲 actor mailbox 同步往返） | 需要 actor `apply` 侧出现"回调上层再取 `bundle.mu`"的边（当前 `apply` 只做状态迁移 + 非阻塞 `emitChange`） | 在 `apply` 插 sleep，量 `NewMainSessionWithID`/`Session()` 阻塞时长 |
| H3 | `seelexctx/lifecycle/pipeline.go:186-205`（`FlushContext` 持 `gate.RLock` 等 `<-request.reply`）vs `CloseContext` 写锁 | 需要 `Storage.Append` 无视 ctx 且不返回（当前内存/分片实现有界，`Close()` 传 `Background()`） | 注入"`Append` 阻塞且不理会 ctx"的假 Storage，goroutine A `FlushContext` + B `Close()` |
| H4 | `seelexctx/lifecycle/actor.go:333-341`（`SnapshotContext` 在 `closedFlag` 置位后读 actor 私有 `resident`） | 需要"`a.closed` 未关而 `closedFlag` 已置位"的窗口内并发调用 | `-race` + 并发 `Close`/`SnapshotContext` 探针 |
| H5 | `session/domain.go:103-121`（`SetChatState/UpdateChat` 在 `view.mu` 之外写 `view.Chat`） | 需要一条非 nil view 的调用点（现有 8+ 处全传 nil，现不可触发） | 代码审阅即可，属"等着被踩"的一致性问题 |

---

## 4. 已核清（重点摘录，避免把"没查到"读成"已证清")

- `application/core`：`ViewMu` 的 `*Locked` 契约整体成立（`session_runtime/archive.go:46-107`
  甚至把磁盘 I/O 明确放在 `ViewMu` 之外）；上一批修复的三段式临界区
  （`context_runtime/coordinator.go:714-846`）我逐行复核：A 段只做内存提交、B 段
  （推帧 + 帧正文渲染）确在锁外、C 段只碰内存，`compactionPushLock` 只包推帧且**不**在
  持它时取 `ViewMu`；`content_lru.go`/`resident_lru.go` 的"调用方不得持有 `ViewMu`"
  前置条件与全部调用点一致（`session_lifecycle.go:111-113`、`session_history.go:552-554`
  /`823`、`session_draft.go:228` 均在 `ViewMu.Unlock()` 之后）。
- 压缩轮门（`context_compact_gate.go`）：领轮/收口/复判只在 `ViewMu` 内做，等待一律睡在
  信号通道、**锁已释放**，无"持锁等待"。
- `application/event/hub.go`：`hub.mu` 只在注册/注销时短暂持有；`deliver` 的先排空再发
  resync 因 `buffer ≥ 1` 必然成功（与 `close` 不互锁）。
- `application/approval/broker.go`、`application/console`、`application/prompt`：均"锁内取
  引用、锁外回调"。
- `session/domain_actor.go`：actor loop 不给自己投递、reply 带缓冲；`session/ports.go`
  的单元锁与队列锁都是短临界区。
- `sessionstore`：模块锁全序 `compact→message`、`lifecycle→message`、`retention→message`、
  `模块→guide` 已建立，无 `message→{compact,lifecycle,retention,event}` 反向边；
  `Router.mu → opsMu` 单向；路径键锁（`roleDraftLocks`/`teamConfigLocks`/`teamRegistryLocks`）
  每处只取一把；`data_root_lock` 在锁外等 `done`。
- `seelebridge`：Runtime 的一批单值注入锁（`turnArchiverMu/historyRouterMu/sessionWorkspacesMu/
  eventPersisterMu/...`）全部"锁内取引用、锁外调用"；`session/subagent_tree.go` 两阶段投影；
  `session/tool_events.go` 锁内只拷观察者；`scheduler` 任务在锁外跑；`mcp.Manager.Close`
  先放锁再 `wg.Wait`；`fs` 路径锁锁内只 read-modify-write。
- `seelexctx`：`controller.go` 的三处临界区都是"读→解锁→调宿主→再锁内复核"；
  `lifecycle/actor.go` 的 gate 关闭**不**持写锁等 mailbox 排空（无该死锁形态）；
  `merger/memory/snapshot/search/tokens/...` 无锁面。

## 5. 建议顺序（按"会不会永久挂死"）

1. **`json_layout.go:446`**（§2.1）：一处笔误，改 1 个标识符；加 `readSelfHealHook` 并发用例。
2. **`runtime_role_turn.go:198`**（§2.2）：把整轮从 `handle.mu` 里搬出来（改用按角色会话键的
   窄闸门），并给 `OnDelta` 的同 goroutine 契约加断言/同步封装。
3. **`plan_tools.go:332` + `session_scope.go:464/510/512`**（§2.3/§2.4/§2.5）：把宿主端口与
   文件 I/O 采样移到 `ViewMu` 之外——这就是上一批修复的同一纪律，属"补完"。
4. `engine_port.go` 的 `prepareHistory` 与 `async_exec.go` 的三处锁内 I/O（§2.6/§2.7）：
   同族"锁内慢活"，都是把动作移到解锁之后的小改。
5. `module_heads.go:mutexFor` 的 `default` 与 `plan/executor.go` 的锁外解引用（§2.10/§2.11）：
   防埋点 + 修竞争。
6. 其余（§2.8/§2.9/§2.12/§2.13/§2.14）：按吞吐与一致性需求排期。

## 6. 未做 / 边界

- **本次不改任何代码、不加测试**（纯审查）；§5 里每条修法都给了最小改动位置，可直接开批次。
- 机械扫描只覆盖"同一函数内重入"与"持锁遇阻塞原语"两类**形态**；跨函数/回调型自锁靠人工
  调用链核对（§2 里每条都写了链）。脚本在 `_scratch/`（临时件，未纳入 `scripts/`）。
- 框架 `github.com/RedHuang-0622/Seele` 经 `replace` 指向工作树外目录，本次**不可读**：
  `ChatStream` 内工具执行与 `onChunk` 回调是否与调用方同 goroutine、`Session.ReplaceHistory`
  的锁语义，这几处只能按仓库注释（`techleader.go:161`、`engine_port.go:597-607`）推断，
  是 §2.2/§3-H1/H2 的"缺的那一环"。
- 未跑真实 API / 长时压测；§2.1/§2.2 的复现配方都没执行（属下一批次的验证工作）。
