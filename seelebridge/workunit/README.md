# workunit：异步作业的「工作单元」契约

一个**只装生命周期**的契约包：现场、会话、作业。它不装执行面（`Run` / 回合外壳 /
装配 / 调度 / 统一事件流一律不进这里），只回答一个问题——

> 一件异步的活，**怎么开始、怎么收尾、怎么回收、重启后怎么接着做**，三层是不是同一套说法。

## 一、层链：只增不减

| 层 | 相对下层**增**了什么 | 现场 | 会话 | 收尾策略 |
|---|---|---|---|---|
| `job` | ——（基线） | 无 | 无（后台运行 + 结果回传） | 不适用：**不实现 `Unit`** |
| `subagent` | worktree 现场 + 一件活自己的异步会话 + 存储与合并纪律 | **临时**（派出即建、收尾即清） | 一件活一条，落 `sessionstore.NodeSessionStore` | `Immediate` |
| `teammate` | 听 leader 调度 + 装配（plugin / 系统提示词 / skill 前缀复用） | 归 team | 归 team | `AtTeamClose` |

两条关键口径：

- **`job` 层不实现 `Unit`**：它不是"少一份现场的工作单元"，它根本不是工作单元（没有现场也没有
  持久会话）。让 job 去实现 `Unit` 只会得到四个空实现 + 一个永远为 nil 的 `mergeErr`——那正是
  「为接口而接口」。作业面只在 teammate 的 `Reclaim` 里被需要，端口是 `Jobs`。
- **一个 `Unit` = 一件事 = 一份现场 + 一条会话**。粒度差异不靠分支表达：teammate 的角色级现场
  就是 teammate 单元自己那一份，Work Item 级的现场属于该 Work Item 的 subagent 单元。于是
  `Reclaim` 永远只拆"我自己这一份"。

## 二、文件

| 文件 | 内容 |
|---|---|
| `contract.go` | `Kind` / `Scene` / `Result` / `Outcome(Kind)` / `Unit`（`Begin` `Finish` `Reclaim` `Recover`）/ `FinishPolicy`（`Immediate`、`AtTeamClose`）/ `Jobs` 端口 |
| `classify.go` | `ClassifyFinish(result, mergeErr)`：三层**唯一一份**收尾分类（跑失败判死 → 未提交（不判死）→ 主工作区挡路（不判死）→ 其他合并错误判死 → 落定）+ `bounded` |
| `session.go` | `SessionLedger`（结构上就是 `*sessionstore.NodeSessionStore`）/ `Resume` / `RecoveryNoteRole` / `RecoveryNotePrefix` / `RecoveryNote(kind, record)` |

## 三、四条不变式

1. **现场是人的资产**：未提交产出的现场**不许**被框架悄悄丢；只有"回收"这一条路能拆现场，
   而回收只有一个入口（teammate 侧 = `team_close`）。
2. **认领先于 `Prune`**：重启回灌必须先认领现场（`Recover` 跑完再 `worktree.Prune`），否则
   "干净但还没合并"的现场会被当孤儿连分支一起删。
3. **收尾分类只有一份**：`ClassifyFinish`。同一件"跑完了但没合进去"，不许在一条路上是警告、
   在另一条路上是判死（旧实现就是这样漂移的）。
4. **恢复说明只有一族**：`RecoveryNote` 的稳定前缀 + `system` 注入 role；三层在审计里因此是
   同一种东西，而不是三份自造说明。

## 四、会话恢复（重启回灌）

1. **形状一致**：一个工作单元 = 一条 `sessionstore.NodeSessionRecord`（`NodeID` 定位：
   subagent 是节点 id，teammate 是 `<role>-<itemID>`）；记录里的 `History` / `StagesJSON` /
   `ContextJSON` / `Worktree` 就是"跑到哪、现场在哪"。
2. **运行期落盘**：跑着就写（进程中断可从最近一次记录恢复），落定再写终态。
3. **认领先于 `Prune`**（不变式 2）。
4. **回灌 + 恢复说明**：历史灌回执行面并注入 `RecoveryNote`；记录说 `running/queued` 而本进程
   没有它的执行面 ⇒ **中断**，记进 `Resume.Interrupted`，交上层重跑或人工处置。

## 五、测试与验证

```powershell
go vet ./seelebridge/workunit/
go test ./seelebridge/workunit/ -count=1
```

用例守着的是**语义**而不是实现细节：分类表（含"两种收尾失败同时具备 → 未提交优先"）、
`AtTeamClose` 不得自己拆现场、`SessionLedger` 与既有存储的同名同签名（编译期断言）、
恢复说明的前缀族与有界性。
