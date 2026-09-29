# 装配层折叠持 ViewMu 推帧：全进程冻死 + 确定性自锁（2026-09-29 修复）

> 日期：2026-09-29
> 范围：`application/core/context_runtime/{coordinator.go,compaction_index.go}`、
> `application/core/context_compact_{viewmu_hold,selfdeadlock}_repro_test.go`、
> `application/core/context_runtime/compaction_push_lock_test.go`
> 关联：[2026-09-23 message 读路径与写者解耦](2026-09-23-message-read-path-decoupled-from-writer-lock.md)（"读路径持写锁做整段解码"同形）、
> [2026-09-23 迭代边界注入撞会话锁](2026-09-23-iteration-hook-session-lock-reentry.md)（自锁死现场写法）、
> [2026-09-25 压缩：配置化、同会话串行、占用可见](../2026-09-25-compaction-blocking-and-usage-curve/plan.md)（S1 同会话压缩串行门）

## 1. 现场与证据（先复现，再改）

两条用例（修前都是 `Skip + 证据`，证据如下，非推断）：

```text
# TestExplicitCompactFramePushSelfDeadlocksRepro（生产归档接线，main.go:312 同形）
已复现装配层折叠自锁（ViewMu 写锁内再取读锁）：进度条停在 replace(3/7)：index 关永远到不了；
  /compact 提交 3s 未返回；归档 commit 数=0（自锁发生在写盘之前）；
  Snapshot 1s 未返回（会话切换/列表刷新被冻）；新消息 1s 未进队列（Submit 第一步就取 ViewMu.RLock）

# TestExplicitCompactReproViewMuHoldAcrossFramePush（推帧可阻塞的索引面桩）
已复现：折叠在持有 Core.ViewMu 期间推帧，锁被推帧扣住：
  推帧期间 ViewMu 写锁拿不到=true；/compact 未返回=true；Snapshot 被冻=true；
  新消息被冻=true；切会话被冻=true；推帧 ctx 不可取消（context.Background）
```

观测面（用户口径）与代码事实一致：**整块交互面**（压缩命令返回、快照/会话列表/右栏、
消息提交、切会话）同时停摆，而磁盘从最后一次提交起再无写入——一个卡在锁上的进程。

## 2. 根因：一条链，每一跳都是生产接线

```text
prepareExecutionContextFor（装配层折叠）
  └─ c.ViewMu.Lock()                      ← 写锁落在执行这条折叠的 goroutine 上
       ├─ 推帧 c.pushCompactionFrame
       │    └─ CompactionIndexPort.PushCompactionFrame(context.Background(), …)
       │         （compaction_index.go：ctx 是 Background → 不带会话归属）
       │         └─ seelebridge.Runtime.PushCompactionFrame
       │              ├─ MainCompactionDAG().Execute（厚摘要开关打开时 = 前缀重放模型调用）
       │              └─ CompressedTurnArchiver.StoreTurn(ctx, …)（原文写盘）
       │                   ├─ sessionIDFromContext(ctx) == ""        ← Background 不带会话
       │                   └─ a.SessionIDProvider()                  ← 兜底会话归属
       │                        └─ main.go:312 `app.Snapshot().Session.ID`
       │                             └─ view_state.Coordinator.SnapshotView
       │                                  └─ ViewMu.RLock()          ← 同 goroutine 再取读锁
       ├─ 帧正文渲染（纯函数）
       └─ 帧正文进内容存储 + 写压缩记录 + 翻视图修订
```

两个后果，都**不是概率事件**：

- **① 确定性自锁**：`sync.RWMutex` 不可重入，持写锁者再取读锁 = 永久阻塞，没有超时、
  外部取消也进不来。触发条件是"折叠折出非空区间"（走到归档）且装配了轮次归档器——
  生产装配恒为真，因此真实进程里每次软线折叠/`/compact` 都会走到这里。
- **② 锁的持有时间 = 推帧耗时**：推帧在生产路径上不是纯计算（模型调用 + 归档写盘 +
  压缩栈写盘），而 ViewMu 是**全进程**的视图锁 → 一个会话折叠期间，其它会话的提交、
  快照、切会话全部堵在同一把锁上。

## 3. 修法：临界区切成三段 + 按会话串行推帧

`prepareExecutionContextFor` 的折叠落点从**单个**临界区改成三段：

| 段 | 锁 | 做什么 | 为什么放这里 |
|---|---|---|---|
| A | ViewMu | 会话状态提交（`ContextRetainedFrom`/`TokenAudit`/纪元与版本推进）、`RememberCheckpointLocked`、压缩区间与记录定稿 | 微秒级内存操作；这些字段的唯一保护就是 ViewMu |
| B | **锁外** | 推帧（`CompactionIndexPort`）、帧正文渲染（纯函数） | 模型调用 + 写盘 + 宿主回调；持锁跑它 = ①与② |
| C | ViewMu | 帧正文进内容存储、写压缩记录、`BumpLocked`、决策事实回填 | 只碰内存（pending 登记 + 快照投影），必须与 A 同锁语义 |

边界纪律（写进代码注释与模块 README）：

- **过界的只有值拷贝**：压缩记录、被折区间、帧输入（含 layout/证据/plan 尾部）。锁内对象的
  指针一律不带到锁外，因此 B 段不持有任何"锁内才合法"的引用。
- **C 段自己复核归属**：`RecordContextCompactionLocked(requestID, record)` 按 requestID 反查
  会话并校验 `state.RequestID == requestID`；锁外这段时间里回合换人 → `recorded=false`，
  与"记录门槛不满足"同一结论（进度条终局写 `folded_without_record`），不需要额外的锁内校验。
- **推帧按会话串行**（新增 `Coordinator.compactionPushLock`，`map[sessionID]*sync.Mutex`）：
  压缩栈是链式结构，`sessionstore.SessionContextStore.PushCompact` 校验
  `PrevSegmentID`/`PrevRequestFrom`/`PrevRequestTo` 必须与栈顶逐一相等。两条并发折叠推同一
  会话时，后来者按自己读到的栈顶填锚点，**必然**撞这条校验。这条串行原先由 ViewMu 的宽临界区
  **顺带**提供；推帧移出锁后必须显式补回，否则就是我们在缩小锁粒度时自己弄丢了一条不变量。
  锁只包推帧本身（渲染、落存储、写记录都不在内），跨会话不互等。

**刻意没走"用 channel/异步推帧把命令也解耦"那条路**：推帧回执（`segment_id`、摘要来源、
降级原因）要**原样嵌入帧正文**，帧正文的 `frame_ref` 与压缩记录又要写在同一轮里；改成异步就得
把"渲染帧正文 + 落存储 + 写记录"整段搬到 worker，7 关门禁进度与 `/compact` 回执（逐关耗时）
都会在终局之后才补齐——那是把一条确定的契约换成两条不确定的时序。异步化若要真做，是独立的一
个决定（两阶段记录 + 进度契约重定义），不属于本缺陷。

## 4. 有牙证明（把修复 stash 掉 → 两条用例立刻红）

```text
$ git stash push -- application/core/context_runtime/coordinator.go
$ go test ./application/core/ -run "TestExplicitCompactFramePushSelfDeadlocksRepro|TestExplicitCompactReproViewMuHoldAcrossFramePush"
--- FAIL: TestExplicitCompactFramePushSelfDeadlocksRepro (5.07s)
    装配层折叠自锁（ViewMu 写锁内再取读锁，/compact 永不返回）：… Snapshot 1s 未返回 …
--- FAIL: TestExplicitCompactReproViewMuHoldAcrossFramePush (5.08s)
    折叠在持有 Core.ViewMu 期间推帧，锁被推帧扣住（交互面被冻）：… 写锁拿不到=true …
$ git stash pop        # 装回修复 → 两条均 PASS（0.08s / 1.08s）

$ git stash push -- application/core/context_runtime/compaction_index.go   # 去掉按会话推帧串行
$ go test ./application/core/context_runtime/ -run TestCompactionPush
--- FAIL: TestCompactionPushSerializesWithinSession (0.00s)
    同会话的第二条推帧进入了索引面（"session-s1"）：推帧串行锁没生效
$ git stash pop        # 装回 → 两条 PASS（含跨会话不互等那条）
```

两条用例的判据是**交互面**（写锁本身 / 快照 / 提交 / 切会话）——推帧被桩按住时它们任一项为
真就是缺陷还在。`/compact` 是否仍等自己这一次推帧单列为**契约断言**（回执要嵌进记录与帧正文，
不许提前返回）：它等的是自己的同步调用，不是锁，因此不作冻结判据（修前修后都为 true）。

## 5. 验收（本机 2026-09-29）

| 命令 | 结果 |
|---|---|
| `go build ./...` / `go vet ./application/core/context_runtime/` | exit 0 / 无输出 |
| `gofmt -l application/core/context_runtime …repro_test.go` | 空 |
| `go test ./application/core/... -count=1` | 17 包全 ok（core 13.2s） |
| `go test . -count=1` | ok 13.5s（根包全链路） |
| `go test ./seelebridge/... ./internal/... -count=1` | 全 ok（未改动的树，回归确认） |
| `go test -race ./application/core/ -run "TestExplicitCompact\|TestCompaction\|TestContext\|TestSubmitParks" -count=1` | ok 19.8s，无竞争报告 |
| `go test -race ./application/core/context_runtime/ -count=1` | ok 2.2s，无竞争报告 |
| `TestExplicitCompactGateTimeline`（既有门禁形状用例） | 9 帧顺序不变：judge→assemble→replace→index→frame→store→record，逐关耗时之和 616ms ≈ 整轮 618ms |

注：本机 `CGO_ENABLED=1`，因此 `-race` 是**实跑**的（与若干旧 devlog 里"CGO_ENABLED=0 未跑"
的前提不同）；`-race` 只覆盖上述包与用例，未对全仓跑。

## 6. 未做 / 已知边界

1. **推帧 ctx 仍不可取消**（`compaction_index.go` 的 `context.Background()`）：点停止/切会话不会
   中断在飞的厚摘要模型调用（`limits.context_compaction_summary.enabled` 打开时才存在）。修前它
   是"解不开冻死"的一部分，修后交互面不再被扣住，只剩"这一轮要等推帧自己回来"这条已知代价。
   要真做需要把回合 ctx 一路传进 `prepareExecutionContextFor`（根包三个调用点 + 装配路径），
   属独立批次。
2. **归档器的兜底会话归属读的是"视图"会话**（`main.go:312` + `compressed_turn.go` 的
   `SessionIDProvider` 注释 + `ReadCompressedTurnHandler` 读 `Snapshot.Session.ID`）：装配层推帧
   走的是无会话 ctx，因此**后台会话**折叠的原文归档会落到视图会话的 `ToolResults` 通道，而读面
   也按视图会话读——同一视图内自洽，切走视图后同一个 `segment_id` 就读不回来了。**这是代码
   阅读所得，未跑多会话 live 复现**；正确的做法是让推帧带上会话归属
   （`task_context.WithSessionID(ctx, sessionID)`，见 `compressed_turn.go` 的"运行中会话归属一律
   以 ctx 为准"），并把读面也改成按 ctx 会话路由。属独立批次（改动跨 seelebridge/根包读面）。
3. **第一段临界区仍持锁做 O(n) token 估算**（`CountRequestTokens` 多次采样）：它是纯计算
   （`CalibratedTokenCounter`，无 I/O），因此不在本次范围内；若将来计数器改成走 provider
   tokenizer API，这段必须一并拆出去。
4. 本次未跑真 API 冒烟（`compactlive`）：改动是锁纪律与串行化，离线用例已覆盖判定面；真 API
   证据断档问题（见 `docs/2026-09-29-context-compaction-fold-review.md` P1-C）照旧归发布门禁。
