# 会话分叉回归：子会话序号基线与恢复覆盖（2026-09-11）

> 状态：**已修复**。三个根包复现用例全部转绿；根因 #1/#2/#3 均有探针证据。

## 1. 现象与复现

三个根包用例在干净 HEAD 上稳定失败（与本文件所在分支的其它改动无关，
已用 `git worktree` 在无改动的 HEAD 上复现确认）：

```text
go test . -run 'TestBackgroundCompletionWhileSwitchingToC|TestTwoRunningViewThirdThenSwitchedFinishesHot|TestTwoRunningViewThirdThenSwitchedFinishesCold' -count=1
```

场景（TC-A2-01）：A 会话后台运行中切到 B → 从 B 分叉出 C → C 跑完第一轮
（"hello C"）、再跑第二轮（阻塞）→ 此刻释放 A，让 A 的收尾与 C 的运行并发 →
释放 C → `ResumeSession(C)` 后断言 C 的可见内容含 "hello C" 与 "hello C2"。

实际：C 的第一轮内容丢失，或磁盘记录与视图互相矛盾（详见下）。

用例规格与验收口径见
[../2026-08-30-session-resource-refactor/test-cases.md](../2026-08-30-session-resource-refactor/test-cases.md)
（TC-A2-01 标注为「已绿」，因此本回归是**后来引入的**）。

## 2. 回归引入点

`git bisect run`（good=`ebc0bda` 2026-08-31，bad=HEAD）定位到：

```text
e901a512 fix(core): fork 子引擎登记后一律卸载，resume 从 fork 快照装载正文
```

该提交把 `forkSessionLocked` 末尾的 `UnloadSession(childID)` 从「仅逐会话宿主」
改为**一律执行**（为修 F-4：legacy 宿主 resume 时把空引擎判为已加载 →
热挂载空视图）。对照实验（改回仅逐会话宿主）显示：TC-A2-01 与 `...Hot`
转绿，但 F-4 的 `TestForkChildVisibleWithRealStore`、
`TestForkSessionChildContentVisibleAfterResume` 转红——**两者直接冲突，
不能靠回退解决**，必须修「子会话从磁盘重新装载」这条链。

## 3. 根因 #1（已修复）

**子会话的内存 transcript 序号与继承基线重叠。**

`forkSessionLocked` 会把父会话的事件物理复制进子会话快照，这些事件带着父的
seq 区间（1..N）。子会话的内存 transcript 可能没有任何基线（子引擎登记后被
卸载、冷启动为空、没有可导入的引擎历史），于是子会话自己产生的事件从 seq 1
起算，**与继承区间重叠**。落盘时 `mergeTranscriptEventsBySeq`（按 seq 覆盖）
会让自己的消息被旧行顶掉。

探针证据（修复前，C 的第一轮落盘）：

```text
persisted=4  基线: seq1 first A / seq2 ok / seq3 hello B / seq4 ok
memory=2     seq=1 hello C / seq=2 ok
merged       seq=1 hello C(内存覆盖) / seq=2 ok / seq=3 hello B / seq=4 ok
```

修复后同一位置：

```text
persisted=4  memory=2(seq=5 hello C / seq=6 ok)  merged=6
PROBE[after-C1] C texts=[first A, ok, hello B, ok, hello C, ok]
```

实现：

- `application/core/session_fork.go`：fork 时调用
  `SeedTranscriptSeqFor(childID, maxTranscriptEventSeq(forkContext.Events))`；
- `application/core/task_context/task_context_state.go`：新增
  `SeedTranscriptSeqFor`（只抬序号基线、只增不减、不复制内容）。

注意：这里**不做任何合并**——`mergeTranscriptEventsBySeq` 是既有函数
（`ebc0bda` 引入，位于 `application/core/session_runtime/archive.go`），
用于**所有会话**落盘时拼接「磁盘已提交事件 + 内存未提交事件」；子代理的
merge（`seelexctx/merger`、`SubagentContextActor.MergeBackIntoParent`）
是另一条链，本次未触碰。

## 4. 根因 #2（已修复）

**会话恢复会用快照整段替换内存 transcript 并回退 `transcriptSeq`。**

探针证据（在 C 的第二轮之前）：

```text
PROBE restoreTask sid=C restoredTranscript=4 restoredSeq=4 currentTranscript=3 currentSeq=7
```

`_RestoreSessionTaskLockedFor`（`application/core/task_context/coordinator.go`）
执行 `st.transcript = restored.Transcript; st.transcriptSeq = restored.TranscriptSeq`
——C 内存里 3 条活事件（seq 已到 7）被替换成快照的 4 条（基线），序号回退到 4；
随后 C2 的事件重新拿到 5,6，落盘时覆盖掉 C1 已提交的行。

修法：`_RestoreSessionTaskLockedFor` 改为「内存已有事件则不采用恢复快照，只把
序号基线取两者最大（只增不减）」。

## 4.1 根因 #3（已修复）

**后台冷恢复迟到完成时，无条件覆盖目标会话的可见会话。**

`resumeSession` 在「有会话在跑 + 目标未驻留」时走异步分支
（`resumeSessionColdInBackground`），其 epoch 守卫只防止「抢占视图」，但仍会
发布目标会话的**可见投影**。若目标会话在这期间已经跑过自己的回合，恢复快照
（更旧的基线）就会把这一轮的可视内容顶掉。探针证据（调用栈 + 长度）：

```text
PROBE viewSet sid=C fromLen=5 toLenBefore=2     ← 用「标记+基线」5 条替换掉 C 的 2 条活消息
  view_state.SetSessionViewLocked ← resumeSessionCold ← resumeSessionColdInBackground(epoch=4)
```

修法：`session_history.go` 的可见投影发布加守卫——`activateEpoch == 0`
（同步装载，调用方持视图过渡锁）保持原语义；后台装载只在目标可见会话**仍为空**
时安装（`sessionViewEmptyLocked`）。

### 关于「恢复之后谁又写了 C 的 transcript」

排查确认：**没有第三方写入**。此前的候选修法未转绿，是因为它只堵住了任务状态
（transcript/seq）这一侧，而**可见会话**仍被同一次迟到恢复覆盖——两处必须一起修。

### 探针手册（一轮可复现）

在以下位置加临时 `println`（跑完务必全部撤掉）：

| 打点 | 位置 | 输出 |
|---|---|---|
| 事件追加 | `task_context.appendTranscriptEventLocked` | 会话 ID、`seqBefore`、`len(transcript)`、role/content |
| 落盘合并 | `session_runtime.allTranscriptEventsForSession` | `persisted`/`memory`/`merged` 计数与 memory 各条 seq |
| 恢复覆盖 | `task_context._RestoreSessionTaskLockedFor` | `restoredTranscript`/`restoredSeq` vs `currentTranscript`/`currentSeq` |
| 引擎装载 | `sessionstore.DurableHistory.Load` | 来源（prepared/store）、事件数 |
| 引擎清理/替换 | `internal/adapters.EnginePort.ReleaseWorkingHistoryFor` / `replaceRawHistoryFor` | 会话 ID、动作、条数 |

已知清白（本轮排除）：`PrepareNextLoad` 全程未被武装；`ImportEngineHistory`
只对 A 跑过一次且 history 为空、从未触碰 C。

## 5. 附带修正：一条过时的污染断言

`TestTwoRunningViewThirdThenSwitchedFinishesHot/Cold` 原先断言「切到 C 后视图
不得出现 seed A / seed B」。但 C 是从 B fork 出来的，`seed A/seed B` 是它**合法
继承**的父会话正文——F-4 契约明确要求 fork 子会话可见继承内容
（[../../application/core/session_fork_test.go](../../application/core/session_fork_test.go)
的 `TestForkSessionChildContentVisibleAfterResume`：子会话 `TotalMessages=2`、
最后一条是父的正文）。这两条用例在 `2ef91cc` 能过，只是因为当时 F-4 的 bug 让
子会话视图恒为空。因此把污染判据收敛为「其它会话的**在途**内容」
（`long A` / `long B`），保留 `seed C` 必须可见的断言。

## 5.1 仍未覆盖

- 全周期偶发：`seelexctx/lifecycle` 的 `TestPipelineIntervalFlush`
  在全量并行时偶发失败、单跑 3/3 通过（时间敏感，与本回归无关）。

## 6. 验证现状

```text
go build ./...                      通过
go test . -count=1                  全绿
go test ./application/core/...      全绿（含 F-4 的两个回归用例）
go test . -run '<三个 repro 用例>'   全部 PASS
go test ./... -count=1              仅剩 seelexctx/lifecycle 的偶发（单跑通过）
```
