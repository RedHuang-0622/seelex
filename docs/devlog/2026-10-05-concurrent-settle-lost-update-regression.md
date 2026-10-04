# 2026-10-05 回归：并发 settle 不丢更新

> 里程碑 `m-fix` / 工作项：**回归：并发 settle 不丢更新**。
> 目标：给 `docs/devlog/2026-10-05-teamwork-self-survey.md` §2 那条「5 个并发 settle，
> 2 个被覆盖回 running」一条**有判别力**的回归测试，并给出「修复前会失败」的实测证据。
>
> 结论：**测试有判别力**（修复前 3/3 次失败、修复后 20 次全绿、`-race` 通过）；
> 修复：`seelebridge/teamwork/planlock.go` + `items.go` 的 settle 两段式。

---

## 1. 交付物

| 项 | 落点 |
|---|---|
| 测试文件 | `seelebridge/teamwork/items_concurrent_test.go`（新增） |
| 用例 1 | `TestConcurrentSettleDoesNotLoseUpdates`（N=5，全并发 settle） |
| 用例 2 | `TestConcurrentSettleAndAcceptDoNotResurrectDone`（并发 settle 混并发 accept） |
| 夹具改动 | `items_test.go`：`fakeSpaces.mergeGate`（把「合并」钉成会合点）；`teamwork_test.go`：内存替身按生产语义 `clonePlan` 深拷贝 |
| 修复 | `planlock.go`（按 `sessionstore.Key` 分片的计划锁）+ `items.go` 的 `SettleWorkItem` 两段式（合并移到临界区外） |

两条用例都以 `n` 个**同一里程碑、彼此无依赖**的工作项起步：先 `PlanMilestone` 排活、
`DispatchItem` 全部派发，执行体用端口桩的 `block` 通道停在半路 ⇒ 全部停在 `running`，
settle 由用例手动并发驱动。

**判别力不靠 sleep 也不靠调度骰子**，靠夹具里的 `rendezvous`（会合点）：

- 用例 1：`newRendezvous(n, auto=true)` —— 所有 settle **先读过了计划**（快照=全 running）、
  都到达「合并」再一起放行。旧实现是 `ReadPlan →(合并) → WritePlan` 的单次读-改-写，
  于是这道会合之后各家必然拿着同一份陈旧快照各写各的。
- 用例 2：`newRendezvous(2, auto=false)` —— 只把 `wi-0/wi-1` 的合并钉住（它们是「早读」的两家），
  等 `arrivedCount()==2` **确认它们已读且已停住**之后，让窗口里另一件事走完整条链
  （`settle` → `review`，再两条**并发** `AcceptItem` → `done`），最后才 `open()` 放行。
  被放行的两家快照里 `wi-2/wi-3` 还是 `running`。

断言的是一句**不变量**，不是实现细节：收口风暴之后不得有任何工作项停在 `running`，
已 `done` 的不得被谁的陈旧快照写回去，且每件恰好尾插一次。

## 2. 修复前会失败（证据）

副本 `tmp/prefix-baseline/teamwork`：生产代码换成 **HEAD 版**（`items.go` / `coordinator.go`，
即没有计划锁、也没有两段式），用例文件与夹具**与仓库内完全一致**。复现（PowerShell；`git show`
的重定向必须走 `cmd /c` 才是原始字节）：

```powershell
New-Item -ItemType Directory -Force tmp\prefix-baseline\teamwork | Out-Null
Copy-Item seelebridge\teamwork\*.go tmp\prefix-baseline\teamwork\
cmd /c "git show HEAD:seelebridge/teamwork/items.go > tmp\prefix-baseline\teamwork\items.go"
cmd /c "git show HEAD:seelebridge/teamwork/coordinator.go > tmp\prefix-baseline\teamwork\coordinator.go"
Remove-Item tmp\prefix-baseline\teamwork\planlock.go
go test ./tmp/prefix-baseline/teamwork/ -run TestConcurrentSettle -count=1 -v
```

失败输出（原文，`-count=3` 时 **3/3 次都失败**，每次失败点相同）：

```
=== RUN   TestConcurrentSettleDoesNotLoseUpdates
    items_concurrent_test.go:176: 并发 settle 丢失更新：wi-0 状态 = running，want review
--- FAIL: TestConcurrentSettleDoesNotLoseUpdates (0.00s)
=== RUN   TestConcurrentSettleAndAcceptDoNotResurrectDone
    items_concurrent_test.go:246: 已 done 被并发 settle 的陈旧快照覆盖：wi-2 状态 = running，want done
--- FAIL: TestConcurrentSettleAndAcceptDoNotResurrectDone (0.01s)
FAIL
FAIL	github.com/RedHuang-0622/seelex/tmp/prefix-baseline/teamwork	0.831s
FAIL
```

即：**用例 2 复现的正是最初那条形态**——`wi-2` 已经走完 `settle → accept → done`，
被早读的那两家 settle 用陈旧快照覆盖回 `running`。

旁证（弱一点、只作补充）：副本 `tmp/lockoff-probe` 保留**当前两段式代码**、仅把
`lockPlan/unlockPlan` 改成空实现（`planlock.go` 两行），同一用例**间歇**失败：

```
    items_concurrent_test.go:176: 并发 settle 丢失更新：wi-0 状态 = running，want review
    items_concurrent_test.go:246: 已 done 被并发 settle 的陈旧快照覆盖：wi-2 状态 = review，want done
```

（`-count=3` 里 2 次失败 1 次通过。）这说明锁在「两段式已经缩小窗口之后」**仍是承重的**；
但它不是主证据——间歇失败正是要靠会合点消除的东西。

## 3. 修复后通过

```
go test ./seelebridge/teamwork/ -count=1
ok  	github.com/RedHuang-0622/seelex/seelebridge/teamwork	0.901s

go test ./seelebridge/teamwork/ -run TestConcurrentSettle -count=20
ok  	github.com/RedHuang-0622/seelex/seelebridge/teamwork	1.040s
```

（`-v` 全量 42 条用例全绿，含两条并发用例：
`--- PASS: TestConcurrentSettleDoesNotLoseUpdates (0.00s)` /
`--- PASS: TestConcurrentSettleAndAcceptDoNotResurrectDone (0.01s)`。）

## 4. `-race` 的真实结果

**本机 `-race` 可用**（`go1.25.8 windows/amd64`，`CGO_ENABLED=1`，`CC=gcc` 存在），不是"缺 C 工具链"
的降级场景：

```
go test -race ./seelebridge/teamwork/ -count=1
ok  	github.com/RedHuang-0622/seelex/seelebridge/teamwork	2.026s

go test -race ./seelebridge/teamwork/ -run TestConcurrentSettle -count=10
ok  	github.com/RedHuang-0622/seelex/seelebridge/teamwork	2.082s
```

**一条要说清的口径**：`-race` 在**修复前的副本上也不报竞态**（`go test -race ./tmp/prefix-baseline/teamwork/
-run TestConcurrentSettle` 只报那条丢失更新的失败，没有 `WARNING: DATA RACE`）。原因是
缺失的原子性**跨三次调用**（ReadPlan / MergeWorkspace / WritePlan），并不落在某一个被并发
读写的内存单元上——探针索引不到它。所以「丢失更新」这类缺陷**不能拿 `-race` 当判据**，
必须靠会合点排定的交错用例：这正是本条测试存在的理由。

## 5. 残差与边界

- 用例只覆盖**进程内**并发（计划锁就是进程内的）。多进程/多 Runtime 同时写同一
  `sessionstore.Key` 的计划头仍是开放的（需要 store 层 CAS 或跨进程锁，属另案）。
- `tmp/prefix-baseline`、`tmp/lockoff-probe` 是**一次性证据副本**（`tmp/` 已被 .gitignore），
  取完证据后删除——避免 `go test ./...` 把"故意失败的包"卷进来（复现命令见 §2）。
- 内存替身的 `clonePlan` 是**生产语义的照抄**，不是测试取巧：`sessionstore` 计划头本来
  就是 JSON 落盘/读回。少了它，调用方的原地修改会绕过 `WritePlan`，用例会假绿且引入真竞态。
