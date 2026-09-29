# 锁纪律批次 A/B：纯读锁面、compact 落盘出锁、运行期入口在途登记

- 日期：2026-09-29
- 范围：锁面审计 §2.14、§2.8（批次 A）、§2.13（批次 B）；§2.12 经试做后被架构不变量挡回（见「未决」）
- 不改：§2.9（事件轨 append-only，架构级）、ABBA 另一半（Supervisor 准入/执行分离，设计级）

## 1. §2.14 `seelebridge`：纯读取写锁 → 读锁

- 改前：`Runtime.Session()` 为返回 `bundle.session` 一个纯读取 `bundle.mu.Lock()`；
  而 `sessionBundle.mu` 本身是 `sync.Mutex`，于是装配/切换持写锁期间，所有读面
  （`Session()` / `CurrentSession()`）都被无谓串行。
- 改后：`sessionBundle.mu` → `sync.RWMutex`（`Lock/Unlock` 语义兼容，装配写路径
  `newMainSession` / `AttachSessionContextStore` 不动）；`Session()` 改 `RLock`。
- 判据（`seelebridge/runtime_bundle_read_lock_test.go`）：测试自己持 `bundle.mu.RLock()`
  模拟"另一个纯读"，`Session()`/`CurrentSession()` 必须仍能进入。
  红：读锁面不存在（`sync.Mutex` 上无 `RLock`，编译即红）；绿：通过。

## 2. §2.8 `sessionstore`：compact 桥接落盘移出 `s.mu`

- 改前：`SessionContextStore.update()` 在 `s.mu.Lock()` 内跑变更闭包，而
  `PushCompact` 的闭包内联调 `s.bridgeCompactFrame(frame)` → `CommitCompactFrameWorkspace`
  （取 compactMu→messageMu + 落盘）+ `RetentionAdvisoryWorkspace`。一次压缩写盘期间，
  本会话的全部纯读（`Snapshot`/`SystemPrompt`/`GoalAuditSnapshot`）被按在磁盘上。
- 改后：
  - `update` 拆成 `mutate`（s.mu 内只改内存，不落盘）+ `update`（mutate + Persist）；
  - `PushCompact` 用 `bridgeMu` 串住「压栈 → 桥接 → 持久化」整段，桥接与 Persist 都在
    `s.mu` 之外；
  - `bridgeMu` 存在的原因是**顺序**：帧靠 `PrevSegmentID` 成链、compact 通道会拒绝
    乱序帧，落盘必须与压栈同序，原先靠 `s.mu` 顺带保证，现在由不覆盖任何纯读的
    `bridgeMu` 保证。
  - 锁序：`bridgeMu → s.mu`（mutate 内）/`bridgeMu → 仓库锁`（bridge/Persist）；无反向边。
- 判据（`sessionstore/session_context_lock_test.go`）：压 8 帧链，期间并发纯读；断言
  通道帧序 = 压栈序，且 `-race` 无竞争。该用例是本次改动的回归牙（桥接挪出 `s.mu`
  后若失去顺序保证，直接转红）。

## 3. §2.13 `sessionstore`：运行期「仅 JSON 布局」入口登记在途操作

- 改前：`runtime_api.go` 上的 14 个运行期入口（wire 装配 / compact 帧 / retention 建议 /
  LRU 删除 / lifecycle 恢复 / 草稿尾三件 / queue 五件）统一是
  `router.mu.RLock(); jsonRepositoryLocked(); RUnlock()` 后**放锁执行**：取到 repository
  指针就与后端解绑，`Configure`/`Close`（只等 `activeOps==0`）可以在同一后端上并发关闭，
  而调用方还不知道自己踩在正在拆除的目录上。
- 改后：新增 `Router.withJSONRepositoryAt(projectID, fn(*jsonRepository, string) error) (bool, error)`：
  走 `acquireRepository`（RLock + `activeOps++`）/`releaseRepository`，**保留原口径**——
  「非 JSON 布局」与「Router 已关闭」都返回 `(false, nil)`；14 个入口逐个换成该包装。
- 为什么不用 `withRepositoryAt`：它的关闭分支返回 `error`，而调用方把 `ok=false` 当
  「能力不可用 → 回退旧链路」、把 `error` 当**硬失败**。已核对实例：
  `application/core/session_history.go` 的 wire 装配 `wireErr != nil` 直接中断会话恢复，
  只有 `ok=false` 才保留旧装配结果。因此把关闭顺带变成 error 是口径回归。
- 判据：
  - `TestRuntimeEntriesKeepOKFalseOnClosedRouter`（口径锁）：关闭后 14 个入口一律
    `(false, nil)`（advisory 为 legacy 零值 + nil）。**已用探针实测有牙**：临时把包装的
    关闭分支改成 `errors.New("...closed")`（= `withRepositoryAt` 口径）→ 该用例转红；
    复原后转绿。
  - `TestJSONRuntimeEntryRegistersInFlightOp`：在途期间 `activeOps > 0`，且 `Close()`
    在此期间不返回、放行后返回；结束后计数归零。
- 顺带修掉一处隐式丢错：`if !ok` 分支原先一律返回 `nil` error，会吞掉包装层可能给出的
  错误；现在改为透传 `err`（在最终语义下 `!ok` 时 `err` 恒为 nil，口径不变）。

## 4. 验证

```
go build ./...                                        → ok
go vet ./sessionstore/... ./session/... ./seelebridge/... ./application/core/...  → ok
go test ./session ./sessionstore ./seelebridge -count=1        → ok（1.8s / 49.4s / 23.9s）
go test ./application/core -count=1                            → ok（12.0s）
go test -race ./session -count=1                                → ok
go test -race ./application/core -count=1                       → ok（40.5s）
go test -race ./sessionstore -run 'TestPushCompactBridgesOutsideStoreLock|TestJSONRuntimeEntryRegistersInFlightOp|TestRuntimeEntriesKeepOKFalseOnClosedRouter|TestRetentionAdvisoryDoesNotPublishMessageHeadOutsideModuleLock' -count=1 → ok
go test -race ./seelebridge -run TestSessionReadPathDoesNotTakeWriteLock -count=1 → ok
```

未跑：`./sessionstore` 全包 `-race`（单包 49s，race 下超串行 5 分钟预算），
以及全仓 `go test ./...`。二者留给提交前的一次全量跑测。

## 5. 未决与风险

1. **§2.12（hub 谓词内 actor 往返）——被架构不变量挡回，维持原状。**
   试做过的改法是给 `session.Domain` 加 V 的无锁镜像（actor 在 `SetActive` 回包前
   `Store`，谓词零往返读）。被 `session/shared_face_test.go::TestGlobalSharedFaceBounded`
   （T5.5 静态断言：Domain 只允许 `cmds`/`stopCh`/`done` 三个 channel 字段，G 之外不得有
   共享可变状态）判红。剩下的路都有代价，需要先定设计：
   - 用 `application/core.currentViewSessionID()`（ViewMu.RLock + `Snapshot.Session.ID`）：
     零新共享状态、且 `view_singleton_test` 已钉住 `ActiveID == Snapshot.Session.ID`，
     但谓词在 hub `publishMu` 内求值，只要存在"持 ViewMu 时发布事件"的路径就是自锁；
   - 把谓词求值整体挪出 `publishMu`：会动摇「seq 序 = 投递序」这条既有保证
     （`publish` 特意持 `publishMu` 覆盖到投递结束），风险大于收益。
   现状：空 sid（草稿/占位视图）订阅每次发布多一次 actor 往返（actor 只做 map/字段操作，
   无回调，因此不会死锁、不是 ABBA，只是热路径上的一次无谓往返）。已在
   `application/core/session_scope.go` 该分支留注释说明原因与试做结论，避免后人盲目"优化"。

2. **§2.9（事件轨整文件重写）**：`EventStore.Append` 在 `store.mu`（全进程唯一 sink）内
   调 `jsonRepository.AppendFrameworkEvent`，后者持 `repository.mu`（跨会话共用）**读整文件
   → merge → 全量 Marshal → `writeAtomic` 整文件重写**。是写放大，不是死锁；属架构项。
3. **ABBA 另一半**：`Supervisor.RunEval` 仍以 `s.mu` 圈住整个 b 回合（`runRoundLocked`）。
   既有边 `s.mu → roundGate`；上批把 roundGate 做成叶子是**纪律**不是**强制**。需先定设计。
4. **flaky 用例**：`repro_session_background_test.go::TestBackgroundCompletionWhileSwitchingToC`
   本批未动（本批只碰 `sessionstore`/`seelebridge`/`application/core`/`session`）。历史记录
   显示其为负载敏感的已知边缘用例（2026-09-08 存储重构的 checklist 已把它连同两个
   `TestTwoRunningViewThirdThenSwitchedFinishes*` 列为 HEAD 预存在红灯）。
5. **环境观察**：本回合 9 个后台作业（`bash_bg`）全部 `exit=1`、0 输出，`bash_bg` 现报
   `无法创建后台输出文件 …\seelex-async-1778845832\a14.log: 系统找不到指定的路径`——
   后台作业的输出目录已被删除，属宿主环境问题，**非**本批或前批代码所致；本轮验收全部
   改用串行执行完成。
