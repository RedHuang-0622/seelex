# 会话分叉回归：子会话序号基线与恢复覆盖（2026-09-11）

> 状态：**部分修复**。根因 #1 已修复并有探针证据；根因 #2 已定位、未修复，
> 交接给会话生命周期工作线。三个根包复现用例仍红。

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

## 4. 根因 #2（已定位，未修复 — 交接点）

**会话恢复会用快照整段替换内存 transcript 并回退 `transcriptSeq`。**

探针证据（在 C 的第二轮之前）：

```text
PROBE restoreTask sid=C restoredTranscript=4 restoredSeq=4 currentTranscript=3 currentSeq=7
```

`_RestoreSessionTaskLockedFor`（`application/core/task_context/coordinator.go`）
执行 `st.transcript = restored.Transcript; st.transcriptSeq = restored.TranscriptSeq`
——C 内存里 3 条活事件（seq 已到 7）被替换成快照的 4 条（基线），序号回退到 4；
随后 C2 的事件重新拿到 5,6，落盘时覆盖掉 C1 已提交的行。

**试过但未转绿的候选修法（已回退，勿直接照抄）**：在该函数里改为
「内存已有事件则不采用恢复快照，只把序号基线取两者最大」。改完后三个用例仍红，
说明**这条恢复不是唯一写入点，或覆盖发生在别处**——继续排查请从「恢复之后
谁又写了 C 的 transcript」入手。

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

## 5. 未覆盖

- `TestTwoRunningViewThirdThenSwitchedFinishesCold` 在**改与不改**两种状态下都红
  → 至少还有一个独立原因，本轮未排查。
- 失败输出逐轮不同（一轮是「基线 + C2」，另一轮是「基线 + 重复的 ok」）
  → 根因 #2 表现出并发窗口特征，建议配合 `-race` 与多轮复现。

## 6. 验证现状

```text
go build ./...                     通过
go test ./application/core/...     全绿（含 F-4 的两个回归用例）
go test . -run '<三个 repro 用例>'  仍红（根因 #2 未修）
```
