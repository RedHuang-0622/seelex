# 锁面普查的修复批次：模块锁身份、回合闸门、ViewMu 内的宿主调用（2026-09-29）

> 日期：2026-09-29
> 范围：`sessionstore/{json_layout.go,module_heads.go}`、`seelebridge/{runtime_role_turn.go,plan/executor.go,tools/async_exec.go}`、
> `application/core/{plan_tools.go,session_scope.go,work_table.go,service_assembler.go,view_state/coordinator.go,goal/techleader.go}`
> 用例：`sessionstore/{retention_advisory_lock_test.go,module_lock_identity_test.go}`、
> `seelebridge/runtime_role_turn_test.go`、`seelebridge/plan/slot_race_test.go`
> 关联：[锁面普查：别处还有没有自锁/死锁](2026-09-29-lock-audit-other-loci.md)（本批的输入）、
> [装配层折叠持 ViewMu 推帧](2026-09-29-compaction-fold-lock-granularity.md)（同一纪律的母本）、
> [迭代边界注入撞会话锁](2026-09-23-iteration-hook-session-lock-reentry.md)（"持锁跨整轮"的真实挂死现场）、
> [2026-09-08 session 子系统锁序修复](../2026-09-08-session-storage-architecture/conformance-checklist.md)（ViewMu → SubAgentTree 那条环）

## 1. 本批修了什么

审计是只读的（13 处风险面），本批只落地"能不能永久挂死"这一列的前四项 + 两处同族慢活，
每条都配了**红→绿**用例（§2）。逐条对照：

| 审计条目 | 改动 | 用例 |
|---|---|---|
| §2.1 `json_layout.go:446` 无锁调 `*Locked` 入口 | `readMessageHeadLocked` → `readMessageHead`（无锁入口：TryLock + 有界失败，绝不发布） | `retention_advisory_lock_test.go` ×2 |
| §2.10 `media` 被静默别名成 `messageMu` | 新增 `mediaMu` + `case moduleMedia`；`mutexFor` 的 `default` 改成显式 `panic` | `module_lock_identity_test.go` ×3 |
| §2.2 `runtime_role_turn.go:198` 持 `handle.mu` 跨整轮 | 闸门改名并写成叶子 `roundGate`（不护任何字段、持它不取上层锁）；引擎读移到闸门外；ctx 在飞标记把同会话重入拦成 `ErrRoleRoundReentrant`（副作用之前） | `runtime_role_turn_test.go` ×2 |
| §2.2 附带：`inFlight` 依赖"同 goroutine 回调"跨包契约 | `noteInFlight`/`clearInFlight` 自带叶子锁 `inFlightMu`（在 `s.mu` 之内取、从不反向） | 既有 `tl_stream_test.go` 回放 |
| §2.3/§2.4/§2.5 ViewMu 内调宿主端口与 I/O | `plan_tools.go` 三处树投影提到锁外；`snapshotOfResident` 的审批/任务/后台作业采样移到 `ViewMu.RLock()` 之前；`refreshWorkTableLocked` 改收**按值**的后台作业快照（端口签名 + `view_state.RuntimeStateProjection` 一并带值） | 既有 plan/子代理/工作表格用例 + `-race` |
| §2.11 `plan/executor.go` 锁外解引用槽指针 | `PolicyFor`/`BindingFor`/`CurrentRunIDFor` 改为锁内取值；`readSlot` 注释写死"解引用必须在锁内" | `plan/slot_race_test.go`（`-race`） |
| §2.6 `async_exec.go` 锁内文件 I/O | 目录创建改双检 `MkdirTemp`（锁外）；`evictLocked` 只摘表并**返回**待删日志路径，由调用方在锁外删；`close`/关停补删点改 `removeDir`（锁外 `RemoveAll`，成功才忘掉） | `seelebridge/tools` 既有用例 |

## 2. 有牙证明（把修复挡回去 → 用例立刻红）

```text
$ go test ./sessionstore/ -run TestRetentionAdvisory -count=1 -v          # 修后
--- PASS: TestRetentionAdvisoryDoesNotPublishMessageHeadOutsideModuleLock (0.08s)
--- PASS: TestRetentionAdvisoryHealsMessageHeadWhenLockIsFree (0.05s)

$ git stash push -- sessionstore/json_layout.go && go test ./sessionstore/ -run TestRetentionAdvisory -count=1
    retention_advisory_lock_test.go:63: advisory 在他人持 messageMu 期间改写了 metadata/message.json
      —— 无锁发布逃出了模块锁
--- FAIL: TestRetentionAdvisoryDoesNotPublishMessageHeadOutsideModuleLock (0.07s)   # 反向护栏仍 PASS

$ go test ./sessionstore/ -run TestModuleLocksAreDistinct -count=1 -v     # 修后 PASS
$ git stash push -- sessionstore/module_heads.go && go test ./sessionstore/ -run TestModuleLocksAreDistinct -count=1
    module_lock_identity_test.go:40: 模块 "media" 与 "message" 共用同一把锁（0xc0000b4120）——模块锁必须一对一
--- FAIL
    TestUnmappedModuleLockPanicsInsteadOfAliasing → 未映射的模块没有显式失败           # FAIL
    TestMediaReadNotBlockedByHeldMessageLock       → 他人持 messageMu 期间媒体读被挡住  # FAIL (3.03s)

$ go test ./seelebridge/ -run TestRoleRound -count=1 -v                   # 修后全绿
$ # 把重入守卫的判据临时短路（false && inFlight == roleSessionID）：
    runtime_role_turn_test.go:348: 外层回合没有返回——它被本轮之内的重入请求扣在闸门上了
--- FAIL: TestRoleRoundRejectsReentrantDriveOnSameSession (5.02s)

$ go test -race ./seelebridge/plan/ -run TestSlotReadsCopyValueInsideLock -count=1   # 修后 PASS 3.6s
$ # 把 PolicyFor 改回"锁外解引用槽指针"：
==================
WARNING: DATA RACE
Write at 0x00c000076000 by goroutine 10:  plan.(*Executor).SetPolicyFor()  executor.go:167
Previous read at 0x00c000076000 by goroutine 11:  plan.(*Executor).PolicyFor()  executor.go:179
==================
--- FAIL: TestSlotReadsCopyValueInsideLock (0.10s)
```

四条判据分别是**磁盘**（head 文件有没有被无锁改写）、**锁身份**（两个模块的锁指针是否同一个）、
**返回/超时**（重入是显式错误还是永久等待）、**竞争检测器**（`-race` 报不报）——都不是"读代码看着对"。

## 3. 验收（本机 2026-09-29）

| 命令 | 结果 |
|---|---|
| `go build ./...` | exit 0 |
| `go vet ./application/... ./seelebridge/... ./sessionstore/ ./internal/... .` | 无输出 |
| `go test ./application/... -count=1` | 全 ok（core 12.6s，17 包） |
| `go test ./seelebridge/... ./internal/... -count=1` | 全 ok（seelebridge 22.6s、tools 15.0s、plan 3.1s） |
| `go test ./sessionstore/ -count=1` | ok 32.2s |
| `go test . -count=1` | ok 13.6s（根包全链路） |
| `go test -race ./sessionstore/ -count=1` | ok 41.6s |
| `go test -race ./seelebridge/ -count=1` | ok 16.6s（含新用例） |
| `go test -race ./application/core/ ./seelebridge/tools/ -count=1` | ok 61.0s / 15.3s |
| `go test -race ./seelebridge/plan/ -count=1` | ok（含新用例） |
| `gofmt -l`（本批改动的 17 个文件） | 空（`application/core/history_safety.go` 的 gofmt 差异是**改动前就有**的，本批未触碰） |

## 4. 未做 / 已知边界

1. **ABBA 的另一半仍在**：`Supervisor.RunEval`/`runRoundLocked` 依旧持 `s.mu` 走 `evaluator.Evaluate`
   → `runRoleRound` → 闸门。把 b 回合的准入从 `s.mu` 里拆出来（"准入在内、执行在外"的同款三段式）
   会动到 peer 状态机、corr 信封与 gate 的互斥语义，属独立批次——本批只把闸门写成叶子并
   把同 goroutine 重入变成本地可判定的错误。
2. **在飞标记的绕过面**：回合内再驱动同一角色会话时若丢掉 ctx（例如自己造 `context.Background()`），
   标记看不到，会退回"排在闸门上等待"。约定是"回合内驱动下一轮必须传本轮 ctx"；要彻底关掉
   需要闸门带持有者身份（Go 没有可移植的 goroutine id），未做。
3. **本批未动**（审计 §5 的其余条目）：`internal/adapters/engine_port.go` 在 `port.mu` 内调
   `prepareHistory`；`sessionstore/session_context.go:811` 的 `s.mu → PushCompact` 落盘；
   `sessionstore/event_store.go` 持 `store.mu` 整文件重写；`application/event/hub.go` 的谓词内 actor
   往返；`sessionstore/runtime_api.go` 的 15 个直连入口不登记 `activeOps`；`seelebridge/runtime.go`
   纯读取写锁。
4. **ctx 仍不可取消**（母本那篇 §6.1 的边界不变）：`compaction_index.go` 的 `context.Background()`
   没动。
5. **未跑真 API / 长时压测**：本批判据全在确定性用例与 `-race` 上；`-race` 覆盖面是上述包与
   用例，不是全仓。
