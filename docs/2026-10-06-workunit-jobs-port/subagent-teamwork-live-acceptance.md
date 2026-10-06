# subagent / teamwork 真机验收（2026-10-06）

- 验收对象：子代理（`fork_subagents` / plan 节点）与团队（leader + teammate）两条链的
  **现场生命周期**：派发 → 建 worktree → 现场上干活 → 收尾 rebase/merge 回主工作区 →
  中断恢复 → 析构销项（两套策略）→ 收尾分类/状态码一致。
- 触发原因：状态机枚举统一（R1–R5 + 三波）改完之后，确认这两条链仍然"跑得起来"。
- 结论：**七条全部可达**；过程中发现两个真问题（一个装配缺口、一个测试基座踩坑），
  装配缺口已修并跑过全量回归。

---

## 0. 结论速览

| # | 验收标志 | 结论 | 证据（关键读数） |
|---|---|---|---|
| 1 | 可以派发 | ✅ | `TestTeamworkWorktreeLifecycleChain` ① `handle=a1 state=done exit=0 归属=exec/wi-file`；`TestLiveSubagentWorktreeChain` `plan_run={"status":"completed","node_count":1}` |
| 2 | 派发出去后有对应 worktree | ✅ | ① 现场在册 `true`，路径 `…\002-seelex-exec-wi-file`（**在主工作区之外**）；真机 `…\001-seelex-impl`，采样 67 |
| 3 | worktree 上有工作 | ✅ | 实测**产出先落在现场**：`现场取证：目录见过=true 现场里有文件=true`；真机同一读数 `=true` |
| 4 | worktree 干完能 rebase/merge 回主工作区 | ✅ | 主工作区 `git log` 出现现场那条提交：`d304714 teammate-acceptance`、`60d1751 live acceptance SEELEX-LIVE-279958`（父 `5f28f66 base`）；文件内容与现场一致 |
| 5 | 中断恢复 | ✅ | `TestRestoreAnchorsAdoptsTeamScenesBeforePrune`（认领**先于** Prune）、`TestSubagentResumeRestartsInterruptedUnit`、`TestTeamUnitRecoverMarksInterruptedAndInjectsTheNoteOnce`、`TestRecoverReportsInterruptedItemsAndKeepsTheirMemory`、`TestRestoredCrashLeftoversMarkedInterrupted` 全绿 |
| 6 | worktree 按层做不同的析构/销项策略 | ✅ | 两档策略 `Immediate`（subagent：收尾即回收）vs `AtTeamClose`（teammate：回收唯一入口 = `team_close`）；端到端实测"收尾保留 → 验收保留 → 整队收口才清" |
| 7 | 报错状态机码一致 | ✅ | 收尾分类只有一份 `workunit.ClassifyFinish` → 四类 `settled/uncommitted/merge_blocked/failed`；两层各自的**落点映射**从同一张表派生；两个哨兵定义在契约里 |

---

## 1. 怎么跑的

### 1.1 确定性层（默认跑，不花钱）

```
go test ./seelebridge            -count=1 -v   → 334 PASS / 0 FAIL / 8 SKIP
go test ./seelebridge/worktree   -count=1 -v   →  32 PASS / 0 FAIL
go test .                        -run "Teamwork|Smoke|Headless" → 全绿（含真 git 的 teamwork 冒烟）
go test ./application/contract/dto ./seelebridge/workunit ./seelebridge/teamwork ./e2e → 99 PASS / 0 FAIL
go test ./...                    -count=1      → 85 行 / 73 ok / 12 no test files / 0 FAIL
go build ./... / go vet ./...                  → 空
```

### 1.2 真机层（真实 API；`config/accounts.yaml` 里是 DeepSeek，探针已确认账号可解析）

```
$env:SEELEX_LIVE_SMOKE='1'
go test ./seelebridge -run TestLiveSubagentWorktreeChain -v -count=1 -timeout 20m
```

真机跑的是**真实模型 + 真实 git**：模型自己决定怎么干活（建文件、提交），框架负责
建现场、收尾、变基、合并、析构。

---

## 2. 本波新增的两条用例（覆盖真空）

补它们之前，"派发 → 现场 → 现场上干活 → 合回主工作区 → 析构"这一整条**没有任何一条用例
在一次运行里同时验过**：

- `seelebridge/worktree_test.go` 手动驱动 `beginNodeWorktree`/`Finish`——**不经过真正的计划执行**；
- `seelebridge/worktree/*_test.go` 用脚本化 git——**不碰真 git**；
- `seelebridge/fork_live_smoke_test.go` 走真 API，但只断言**取回的产出文本**，不看现场；
- `TestTeamworkHeadlessSmoke` 走真装配 + 真 git，但 worker 回合**只回正文、不产文件**，
  于是"现场上有工作 + 合回主工作区"这一跳从未被断言过。

新增：

| 文件 | 档位 | 覆盖 |
|---|---|---|
| `seelebridge/worktree_chain_live_test.go` | 真 API（`SEELEX_LIVE_SMOKE=1`） | **subagent 链**：派发 → 现场 → 现场上真干活 → 收尾 rebase/merge → 析构；逐帧取证（注册表 + 现场目录 + 两处文件） |
| `teamwork_worktree_lifecycle_test.go` | 默认跑（真 git + 脚本化 provider） | **teammate 链**：同上 + "析构销项按 teammate 策略走"（收尾/验收都不拆，整队收口才清） |

两条都用**同一套硬断言口径**：现场必须在主工作区之外；产出必须先出现在现场里；收尾必须
问过合并审批（= 现场里提交过东西，diffstat 里带着那个文件）；主工作区必须拿到文件与提交；
成功路径析构后目录 / 分支 / 登记三者都清。

### 2.1 真机读数原文（subagent 链）

```
=== plan_run 耗时 16.8758612s ===
plan_run 输出：{"status":"completed","node_count":1,"final_output":"已在仓库根目录创建
  live-acceptance.txt（内容单行 SEELEX-LIVE-279958）并提交，工作区干净。…"}

现场在册：true  路径=C:\Users\…\TestLiveSubagentWorktreeChain2439456354\001-seelex-impl  采样=67
产出落在现场：true
合并审批：[merge-impl]
    子代理 impl 的改动将合并进主工作区（main）。
    live-acceptance.txt | 1 +
    1 file changed, 1 insertion(+)
主工作区 git log：
    60d1751 live acceptance SEELEX-LIVE-279958
    5f28f66 base
--- PASS: TestLiveSubagentWorktreeChain (17.50s)
```

模型自己还报了一条有价值的观察：**它在现场里 `git log --all` 只看到 `base` 与自己那条提交，
没有主分支引用**，于是没执行 rebase（"合并交给框架"）。与代码一致——建现场用的是
`git worktree add -b seelex/<nodeID>`，主分支在**主工作区**那一侧，现场里看不见它，
变基兜底由收尾段按 `wt.MainBranch` 决定要不要做。

### 2.2 teammate 链读数原文

```
① 派发：handle=a1 state=done exit=0 归属=exec/wi-file
② 现场取证：目录见过=true 现场里有文件=true 采样=27
   场景=…\002-seelex-exec-wi-file
③ 主工作区 git log：
   d304714 teammate-acceptance
   24c668a base
④ 收尾后：单元记录在册、这件事的会话在跑（sess_…-wt-team-exec-wi-wi-file）
④ 析构销项：收尾保留记录与会话 → 验收保留 → 整队收口才清（现场目录/分支/登记全清）
--- PASS: TestTeamworkWorktreeLifecycleChain (2.35s)
```

---

## 3. 逐条证据补充

### 3.1 派发（两条链的入口不同，出口同一个）

- subagent：`plan_load` + `plan_run`（agent 节点）或 `fork_subagents`；
- teammate：`team_plan` → `team_work` → `team_dispatch`（受理回执即返回，不等这一轮跑完）。

派发之后的**归口**是同一份：`workunit.Lifecycle`（`Begin`/`Finish`/`Reclaim`/`Recover`）。
`TestLifecycleOneImplementationAcrossLayers` / `TestNodeWorkUnitSinglePath` 钉着"只有一份实现"。

### 3.2 现场（worktree）与"现场上的工作"

- 命名只有一份：`sceneDirName` = `filepath.Base(root) + "-seelex-" + nodeID`，分支 `seelex/<nodeID>`；
  teammate 的 Work Item 级 nodeID = `<role>-<itemID>`，teammate 级 = `<role>`；
- 绑根只有一处换算（`workItemNodeID`，去 `seelex/` 前缀）：`TestWorkItemWorktreeIsBoundAsTeammateRoot`
  钉的正是"工具真的落在现场里，不是回退主工作区"（2026-10-05 的缺陷形状）；
- 现场降级是**显式**的：不是 git 仓库 → 不建现场、落到主工作区（`TestWorktreeManagerBeginDegradesOutsideGit`）。

### 3.3 收尾：变基 → 提交判定 → 审批 → 合并 → 清理

`WorktreeManager.Finish`（单写者 actor 串行）逐条都有用例：

| 分支 | 语义 | 用例 |
|---|---|---|
| 落后于主分支 | **在现场里**变基（主工作区一动不动） | `TestWorktreeMergeApproved` |
| 0 提交 + 干净 | 无可合并 → 清理现场 | `TestWorktreeCleanWithNoChanges` / `TestWorktreeLifecycleCreateAndClean` |
| 0 提交 + 脏 | `ErrUncommittedChanges`：现场保留、**不判死** | `TestWorktreeDirtyUncommittedPreserved` / `TestPlanNodeUncommittedWorktreeKeepsResult` |
| 有提交 + 审批拒 | 硬失败、现场保留 | `TestWorktreeMergeRejected` |
| 有提交 + 合并成功 | merge 回主工作区 + 清理 | `TestWorktreeMergeApproved` |
| 主工作区挡路 | **可重试**，预算内有界重试后才报 `ErrMergeBlockedByMain` | `TestFinishRetriesWhileMainWorkspaceDirtyThenMerges` / `TestFinishReportsMergeBlockedByMainWithoutFailingScene` |
| 真冲突 | 确定性失败，列出冲突文件 | `TestFinishMergeConflictIsDefinitive` |
| 并发收尾 | 串行化（同一时刻只有一个在改主工作区） | `TestFinishSerializesConcurrentMergesIntoMain` / `TestFinishWithoutActorOverlapsMerges` |
| 主分支漂走 | 合并目标 = 现场记录的那条分支（先切回去再合） | `worktree_merge_kickback_test.go` |

### 3.4 中断恢复

三条都指向同一组不变式（`seelebridge/workunit/README.md`）：**现场是人的资产**、
**认领先于 Prune**、**收尾分类只有一份**、**恢复说明只有一族**。

- 认领必须在 `Prune` 之前，否则"干净但还没合并"的现场会被当孤儿连分支删掉：
  `TestRestoreAnchorsAdoptsTeamScenesBeforePrune`（F4 的宿主级回归守卫）；
- 重启后从记录重建登记，且**不覆盖活登记**、**不把 `MainBranch` 降级成空**：
  `TestRestoreDoesNotDowngradeLiveRegistration` / `TestRestoreStillRebuildsRegistrationAfterRestart`；
- 记录不存在 / 目录不在的防幽灵语义仍在：`TestRestoreSkipsWorktreesThatNoLongerExist`；
- 中断判定与恢复说明：`TestSubagentResumeRestartsInterruptedUnit`（七步模板）、
  `TestTeamUnitRecoverMarksInterruptedAndInjectsTheNoteOnce`（说明注入**恰好一次**）、
  `TestRecoverReportsInterruptedItemsAndKeepsTheirMemory`（恢复**不去动**现场与会话）。

### 3.5 析构/销项的两档策略（你要的那一条）

契约里这**唯一允许出现的层间差异**被显式化成 `FinishPolicy`（`seelebridge/workunit/contract.go`）：

| 层 | 策略 | 什么时候回收 | 调的是哪个函数 |
|---|---|---|---|
| subagent | `Immediate` | `Finish` 落定**当场** | `Lifecycle.Reclaim` |
| teammate | `AtTeamClose` | 留到**整队收口**（`team_close`） | 同一个 `Lifecycle.Reclaim` |

关键点（写进契约注释、也有用例）：**策略不是实现，只是调用点**——两档调的是同一个析构
函数，所以不会长成"两份拆现场"。`AtTeamClose.AfterFinish` 是**空实现**，而且
`lifecycleHost.Finish` 对团队托管那一支**早返回**、根本不调它：回收唯一入口就是 `team_close`。

物理现场的清理则两条链共用一份（`cleanupWorktreeWith`，**幂等**）：
- 合并成功那一条 → `Finish` 的 `cleanup` 已经在磁盘上删掉现场（teammate 侧 `MergeWorkspace`
  还配对一次 `Release` 清注册表，避免"幽灵绑定"→ `team_accept` 再动手一次报 128，缺陷 A）；
- 未提交 / 被挡 / 冲突 / 被拒那几条 → 现场一律保留（人的资产）。

端到端实测（本波新增）：**收尾保留记录与会话 → `team_accept` 保留 → `team_close` 才清**
（目录 / 分支 / 记录全清）。对应既有用例：`TestTeamUnitFinishPolicyKeepsTheScene`、
`TestWorktreeAndSessionSurviveUntilTeamClose`、`TestTeamCloseClearsTheUnitRecords`、
`TestTeamCloseGateKeepsUnmergedScenes`、`TestReleaseWorkspaceItemIdempotentWhenBindingAlreadyReclaimed`。

### 3.6 收尾分类 / 状态码一致（你说的"报错的状态机的码"）

**唯一一份分类器**：`workunit.ClassifyFinish(result, mergeErr)` → 四类：

| `OutcomeKind` | 词 | 判死？ | 处置 |
|---|---|---|---|
| `OutcomeSettled` | `settled` | 否 | 落定，待验收 |
| `OutcomeUncommitted` | `uncommitted` | **否** | 现场保留 + 产出照常交付；提示"补提交并合并" |
| `OutcomeMergeBlocked` | `merge_blocked` | **否** | 现场保留；提示"先让主工作区干净，再重试合并" |
| `OutcomeFailed` | `failed` | 是 | 现场与记忆都留着，可重派 / 待人工 |

两个哨兵是这一格自己的词表，**定义在契约里**（实现包只引用不定义，依赖方向
`worktree → workunit`）：`workunit.ErrUncommittedChanges` / `workunit.ErrMergeBlockedByMain`。

同一份结论落到**两个载体**（各一张映射表，都从上面这一个分类器派生）：
- 计划侧 `applySettleOutcome`（`teamwork/items.go`）：`Settled→review`；
  `Uncommitted/MergeBlocked→review + Unmerged 标记`；`Failed→failed`；
- 记录侧 `teamUnitStatusFor`（`workunit_team.go`）：`Failed→failed`，其余 `→done`
  （"没合进去"由计划承载，两处各记一份就会漂）。

守卫用例：`TestClassifyFinishIsTheOneClassification`、`TestFinishPolicyIsTheOnlyDifference`、
`TestChainOneFinishScriptForBothLayers`、`TestChainEveryLayerCoversEveryOutcome`、
`TestNodeFinishClassificationIsOneTable`、`TestTeamSettleClassifiesWithTheSharedClassifier`、
`TestLifecycleOneImplementationAcrossLayers`。

状态枚举那一格（第三波刚收的口）：`dto.ToolEventStatus` / `dto.SubAgentNodeStatus` 等 13 格
由 `application/contract/dto/state_machines_test.go` + 两张 `e2e` 门禁逐格钉住（wire 词、
Parse 回读、认不得的词必报错、不许 `string(枚举值)`）。

---

## 4. 发现的两个真问题

### 4.1 装配缺口（**已修**）：测试基座没有装"单元记录"端口

`newFullChainHarnessWithLimits`（`tool_full_chain_test.go`）装了 `AttachHistoryRouter`，
却**没有**装 `AttachSubSessionStore`——而组合根在同一个位置装了它（`main.go:380`）。

后果：基座里 `Runtime.teamUnitLedger()` 返回 nil，`saveTeamUnitRecord` 直接 return，
**teammate / subagent 的单元记录一律不落盘**，恢复链在基座上读的是一个空集。
`TestTeamworkHeadlessSmoke` 的"落盘事实"探针读的是**计划**（另一条持久化路径），
所以这个缺口一直没被冒烟发现。

发现方式：本波新增用例里"收尾之后单元记录还在"这条断言首跑就红——`实际记录：[]`。

修法：在 harness 里按组合根同一句补上（紧跟 `AttachHistoryRouter`），随后全量回归
`85 行 / 73 ok / 0 FAIL`，无副作用。

> 同一类缺口还有一处**证据级较弱的观察**：`SetPlanCheckpointStore`（`main.go:381`）在
> harness 里也没装。它的影响面（plan 断点续跑在基座上是否可验）本次没有测，登记为待确认，
> 未擅自补装。

### 4.2 测试基座踩坑（记录，非产品问题）：Windows 上 `bash` 落到 PowerShell，`>` 写出 UTF-16

本机 `scopedBashCommand` 的探测顺序（Git Bash 固定路径 → PATH 里的 bash（排除 WSL）→
PowerShell → cmd）在本机落到 **PowerShell**，于是 `echo X > file` 写出的是
**UTF-16LE + BOM**：

```
"\xff\xfeT\x00E\x00A\x00M\x00M\x00A\x00T\x00E\x00…"
```

首跑的红是"主工作区内容不对"——而其实**文件确实合回了主工作区**，卡住的是编码。
产品侧无影响（模型读文件内容时按同一编码往返）；但任何按"内容相等"断言的用例都会被它卡住。
修法：让 worker 用 `write_file` 工具（确定的 UTF-8）建文件，`git add/commit` 仍走 bash。

---

## 5. 未覆盖 / 仍开放

1. **真机跑 teammate 链**：本波的真机验收只覆盖 subagent 链（`TestLiveSubagentWorktreeChain`）；
   teammate 链是"真 git + 真装配 + 脚本化 provider"那一档。要不要再加一条
   `SMOKE_TEAM_WORK_LIVE=1` 档的真机 teammate 现场验收，是独立一批的决定
   （quota 与 flakiness 都要算）。
2. **`gui/*_live_probe_test.go` 那族**（`SMOKE_FORK_LIVE` / `SMOKE_TEAM_LIVE` /
   `SMOKE_SUBAGENT_LIVE`）本次**没跑**：它们要一个 `tmp/bin/seelex-headless.exe` 起真进程，
   且 `SMOKE_FORK_LIVE_BIND=1` 会把**本地仓库根**绑成项目根——子代理可能在实际仓库里建
   worktree 并合并提交。按桌面纪律与"不动用户资产"，本次没有开这一档（要跑建议先把仓库
   复制到临时目录再把探针指过去）。
3. **`bash` 的 shell 选择本身**（§4.2）：`scopedBashCommand` 在 Windows 上优先找 Git Bash，
   找不到就退 PowerShell——两者的语法不兼容（`&&`、`|`、重定向编码）。这不在本次验收范围，
   但模型在真机上已经因此改了写法（把 `&&` 拆成两步），登记为观察。
4. 既有缺口照旧：真 API 冒烟需要 `config/accounts.yaml` 与额度；GUI 手工点按冒烟未做
   （桌面纪律：只读检查、不合成输入）。

---

## 6. 复跑

```powershell
# 确定性层（默认，不花钱）
go test ./seelebridge ./seelebridge/worktree -count=1 -v
go test . -run "Teamwork|Smoke|Headless" -count=1 -v
go test ./... -count=1

# teammate 现场生命周期端到端（真 git，脚本化 provider）
go test . -run TestTeamworkWorktreeLifecycleChain -count=1 -v

# 真机（真实 API + 真 git；消耗额度）
$env:SEELEX_LIVE_SMOKE='1'
go test ./seelebridge -run TestLiveSubagentWorktreeChain -count=1 -v -timeout 20m
```
