# 步骤③ wi-4 审计：锚点刷新 + U2/U5/U6 正面回答 + 合并分叉现状 + 下一波九条登记

- 角色：`audit-u2u5u6`（work item `wi-4-audit-u2u5u6`，里程碑 `m1-scene-tools`）
- 复核基线：**main 头 `bdbfeba`**（工作区干净；本报告只读代码，不改任何 `*.go`）
- 口径文档：`docs/2026-10-06-workunit-jobs-port/step-3-goal.md`（§3 锚点对照表 / §7 交付判定 A–H）
- 本轮独占：本文件 + `docs/arch/workunit-duplication-inventory.md`（只刷新锚点与现状句）
- 环境：Windows + PowerShell；`bash_read` 拒收含 `|` / `&&` / `>` 的命令，一律改写
  `git grep -n -e A -e B -- 路径` 或 `Select-String`。

---

## 0. 复核时先要说明的一件事（不是判据，是前提）

清单 `workunit-duplication-inventory.md` 的锚点是在 main 头 `746b00e` 一带写的，**之后 ③ 波已经落地**。
按 `git log --oneline`（`bdbfeba` 之前）可核到的相关提交：

| 提交 | 内容 |
|---|---|
| `bfd6c7e` | step-②：把「判据 / 读法 / 容器」各自收成一份（折算 / 打点编解码 / 预览上界 / 恢复说明容器） |
| `e537aed` | step-③E：哨兵错误搬进契约——`workunit` 不再 import `worktree`，门禁例外撤掉 |
| `019c061` | ③U5：同步链补上进程树退化读数 |
| `c20fccc` | ③U2：两个登记来源同一条判据（`Restore` 已在册不覆盖）+ 真 git 用例 |
| `b435540` | ③U6：记录状态词表收成 `dto` 一处 + 零命中门禁 |
| `6b04e49` | ③U2/U5/U6 结论与锚点刷新（落在 `step-3-u-items-delivery.md` + 清单 +9 行） |

因此：**清单 §一 里"现有实现数"这一列有好几行已经过期**（A 现场清理 2→1、B 脏判定 3→1、C 进程树装配 2→1、
D 在册比较 2→1、E 契约依赖 1→0、② 的折算/编解码/恢复说明容器各自收口）。本轮按纪律**只改行号与
"现状一句话"，不删行、不改判据**；"现有实现数"改成现状值并在句末标注 `(③/② 已落地)`，保留它的历史判据
（即"曾经是几份、并到哪"）以便对照。

---

## 1. 锚点对照表（旧 → 新，逐条）

> "旧" = 清单原文的 `文件:行`；"新" = `bdbfeba` 现状。**语义未变的条目也照列**，因为行号漂移本身就是
> 清单失效的原因（`worktree_manager.go` 整体偏小 32–43 行是 ③B/③A/③D 落地造成的）。

### 1.1 `seelebridge/worktree/worktree_manager.go`（清单 §一 与 §四 U2 引用的锚点）

| 符号 | 旧 | 新 | 备注 |
|---|---|---|---|
| `ErrUncommittedChanges` var（引用契约） | `:76`（清单写作时是 `worktree` 自己的定义） | **`:64`** | 现为 `var ErrUncommittedChanges = workunit.ErrUncommittedChanges` |
| `IsUncommittedChanges` | — | **`:70`** | 转调契约 |
| `ErrMergeBlockedByMain` var | `:76` | **`:83`** | 同上（引用） |
| `IsMergeBlockedByMain` | — | **`:86`** | 转调契约 |
| `mergeBlockedError` | — | **`:96`** | |
| `mergeBlockedMarkers` | — | **`:112`** | 判据表（9 条） |
| `isMergeBlockedEvidence` | `:120` | **`:125`** | |
| `finishActor` 字段 | `:162` | **`:172`** | 收尾单写者 |
| `NewWorktreeManager` | — | **`:187`** | actor 在此创建（`:195`） |
| `handleFinish` / `submitFinish` / `Close` | — | **`:200` / `:210` / `:230`** | |
| `worktreeFor` / `RegisteredCount` | — | **`:239` / `:246`** | |
| `Begin`（仅 `RoleSubAgent` 建） | `:246` | **`:256`** | |
| `BeginNamed` | `:256` | **`:266`** | |
| `beginNamed`（四条路径） | `:272` | **`:282`** | |
| `Adopt` | `:340` | **`:350`** | |
| `sceneNameInfix` / `sceneDirName` / `sceneDirPrefix` | （§四 U2 写 `:386/391`） | **`:383` / `:386` / `:391`** | 命名唯一一份（③D 落地） |
| `scenePath` | `:371` | **`:396`** | |
| `worktreeEntryAt`（内走 `worktreePathEqual`） | — | **`:411`（比较在 `:421`）** | |
| `Finish`（有界重试） | `:432` | **`:457`** | 重试循环 `:457–480` |
| `finishExclusive` | `:460` | **`:485`** | |
| `alignMergeTarget` | （清单未给，见本报告 §5） | **`:553`** | 合并分叉的第一道机制 |
| `Release` | `:525`（清单把它当成"合并后补一次 Release"的锚点，**位置本身就是错的**） | **`:593`** | `Release` 定义处；调用点是 `runtime_plan.go:408` / `runtime_teamwork_items.go:124` |
| `Restore` | `:538` / 另处 `:610` | **`:617`** | 已在册不覆盖（③U2） |
| `Info` | — | **`:645`** | |
| `sceneRegistered`（走 `worktreePathEqual`） | `:577` | **`:660`** | |
| `Prune` | `:612` | **`:695`** | "不在册 + 干净"才删 |
| `listWorktrees` / `parseWorktreeList` | `:695` / `:898` | **`:737` / `:747`** | 解析只有这一份 |
| `isManagedPath` | `:682` / `:725` | **`:773`** | 前缀判据走 `sceneDirPrefix` |
| `pathDirty` | `:688` | **`:778`** | 现为转调 |
| `PathDirty`（包级） | — | **`:785`** | 新增（③B 编排面调用点改走它） |
| `pathDirtyWith` = **脏判定唯一实现** | — | **`:798`**（`git status --porcelain` 在 **`:799`**） | **CRLF 幻影脏的唯一修复点 = `:799`** |
| `git status --porcelain` 出现处 | `:689` / `:750` / `runtime_teamwork.go:861`（三处） | **只剩 `:799` 一处** | ③B 收口（全仓读数见 §7） |
| `worktreeDirty`（组件内） | `:749` | **`:860`** | 现为 `return w.pathDirty(wt.Path)` |
| `cleanup`（收尾自动释放入口） | `:779`（非幂等） | **`:888`** | 现为薄包装 |
| `CleanupWorktree`（包级入口） | `:822`（幂等） | **`:918`** | 现为薄包装 |
| `cleanupWorktreeWith` = **释放唯一实现**（幂等） | — | **`:940`** | 缺陷 A（exit 128）的收口点；doc 注释 `:940–963` |
| `worktreeRemoveError` / `deleteWorktreeBranchWith` | — | **`:967` / `:976`** | |
| `gitWorktreeRegistered` / `worktreeRegisteredWith` | — | **`:986` / `:992`** | 登记判定 |
| `worktreePathEqual` | `:921` | **`:1007`** | 分隔符 + 大小写规范化 |
| `ConflictFilesIn` | — | **`:1017`** | |

### 1.2 `seelebridge/runtime_teamwork.go`（清单 §一"工作区是否脏"第三份）

| 符号 | 旧 | 新 | 备注 |
|---|---|---|---|
| 包级 `worktreeDirty(root)` 定义 | `:861`（清单记为第三份实现） | **已整函数删除** | 现只剩注释 `:859–861`（"已收口删除"） |
| 调用点改走契约 | 调用点 `:809` 用包级 `worktreeDirty` | **`:809` `dirty, err := worktree.PathDirty(path)`** | ③B 落地 |
| 恢复说明装配点 `consumeTeamResumeNote` | `:698` | **`:698`（未变）** | ② 落地后方法名保留 |
| `ResetSession` | `:836` | **`:836`（未变）** | |
| `jobs.New` | `:88` | **`:72`** | teammate 作业表的唯一事实源 |

### 1.3 `seelebridge/tools/`（清单 §一 3) 与 §三）

| 符号 | 旧 | 新 | 备注 |
|---|---|---|---|
| `startAsync`（后台链装配） | `:244` / `:245` | **`:242`**（`newProcessTreeCommand` 调用 `:245`、`startWithProcessTree` `:256`） | 装配已转调唯一助手 |
| `newScopedCommand`（同步链装配） | `:554` | **函数已不存在** | 被 `newProcessTreeCommand` 取代 |
| `newProcessTreeCommand` = **装配唯一一份** | — | **`router.go:570`** | 同步链（`executeScopedBash` 及 docker 重试）与后台链都转调它 |
| `startWithProcessTree` | `:568 startScopedCommand` | **`router.go:584`** | 两条链共用 |
| `newExecProcessTree`（建树工厂，可注入） | — | **`router.go:551`** | 只为"Job 建不出来"那条用例 |
| 同步链起命令点 / 退化读数 | — | **`router.go:606/610/617`**（docker 重试 `:653/656/658`） | ③U5：`r.noteProcessTreeDegraded` |
| `noteProcessTreeDegraded` = 同步链唯一退化读数落点 | — | **`router.go:710`** | 判据 = `tree.Degraded()` |
| `bashProcessDegradedStage` | — | **`router.go:718`** | |
| `security.NewProcessTree` 调用点 | `async_run.go:245`、`router.go:555` | **只剩 `router.go:571`（经变量 `newExecProcessTree`）**；测试 `process_tree_degraded_bench_test.go:22` | |
| `ProcessTree` / `NewProcessTree` / `Degraded` / `Attach` | `process_tree_windows.go:98/110`（`Degraded:128`） | **`:104`（字段 attachFailed `:106`）/ `:114` / `:134`（判据 `:140`）/ `:153`** | 残②：`Degraded() = job == 0 \|\| attachFailed` |
| `asyncRegistry.beginJob` / 调用 | `:251` | **`:266`**（调用 `:248`） | |
| `jobManager.Kill` | `:301` | **`:301`（未变）** | |
| `JobSpec` | `:44` | **`:45`** | |
| `killSession` | `:436` | **`:451`** | |
| `CloseAsync` / `CloseSessionAsync` | `:107` / `:119` | **`:107` / `:119–120`**（后者体是 `return r.async.killSession(...)`） | |
| `AddSubagentJob` / `CompleteJob` | `:73` / `:108` | **`:75` / `:110`** | |
| `subagentJobsAdapter` | `:233` | **`:238`**（`Add :241` / `Complete :263`） | |
| `ReleaseSessionAsync` | `:179` | **`:179`（未变）** | 调用点 `application/core/workspace_usecase.go:18` |

### 1.4 其他（会话管理 / 作业面 / 4) 组）

| 符号 | 旧 | 新 | 备注 |
|---|---|---|---|
| `NoteWorktree` 定义 | `session/subagent_sessions.go:803` | **`:806`** | 唯一调用点 `runtime_plan.go:151`（未变） |
| `buildRecordLocked` | `:474` | **`:475`** | 调用点 `:379` / `:444` |
| `subagent_sessions.go` 状态写点 | `:208`（旧记"自己写字面量"） | **`:208` 现写 `SubAgentDone.String()`** | ③U6 第二批 |
| `ResumeInterruptedSubagents` | `runtime_subagent_resume.go:148` | **`:140`** | |
| `runtime_subagent_resume.go` "在跑"判据 | `:329`（`case "queued","running"`） | **`:328` `case progress.InFlight:`** | 字面量已消失 |
| `subagentResumeState` / 其 notes 容器 | `:49` | **`:50`（结构体）/ `:52`（`notes resumeNotes`）** | 容器唯一一份 = `resume_notes.go:25` |
| `nodeStateOfRecord`（落盘转换点） | — | **`runtime_subagent_resume.go:369`** | 未知词 → `dto.SubAgentUnknown`（非终态） |
| `SubagentTree.Restore` / `restoredSubAgentStatus` | `:422/424`（按字面量判） | **`:369` / `:416`**（内部走 `workunit.InFlight`） | |
| `nodeWorkUnit.Reclaim` / `Recover` | `:93` / `:114` | **`:84` / `:89`** | |
| `teamUnit.Recover` | `:592` | **`:184`** | |
| `lifecycleHost.Reclaim` | `:190` | **`:190`（未变）** | |
| `saveTeamUnitRecord` | `workunit_team.go:134` | **`workunit_team_records.go:68`** | **已搬文件** |
| `teamUnitWorktreeRecord` | `workunit_team.go` 内 | **`workunit_team_records.go:97`** | 只填 Path/Branch（见 §2 U2） |
| `teamUnitStatusFor` | `workunit_team.go:55` | **`:48`** | |
| 记录状态词表三常量 | `workunit_team.go:41` | **`:36–43`** | 全部转调（`workunit.StatusRunning` / `dto.SubAgent*`） |
| `clearTeamUnitRecord` | `workunit_team.go:229` | **`workunit_team_records.go:136`** | |
| `RecoverTeamworkUnits` / `teamUnitSurfaceAlive` | `:331` | **`workunit_team_records.go:223` / `:349`** | |
| `teamUnitScope` | `workunit_team.go:106` | **`workunit_team_records.go:40`** | |
| 恢复说明容器（teammate 侧） | `workunit_team.go:265` `teamResumeState.notes` | **容器已删**；改 `runtime.go:231 teamResume resumeNotes`，读写 `workunit_team_records.go:173–196`，装配点 `runtime_teamwork.go:698` | ②④ 落地（2 → 1） |
| `sessionProjectIDFor` | `runtime_subagent_recovery.go:55` | **`:56`**（读 `sessionWorkspaces` `:62`） | |
| `persistSubagentConclusion` | `runtime_subagent_recovery.go:67` | **`:68`** | |
| `RestoreSubagentAnchors` | `:113` | **`:114`**（`Restore :138`、`adoptTeamworkScenes :144`、`Prune :147`） | 顺序是判据的一部分 |
| `adoptTeamworkScenes` / `teamSceneIndex` | `runtime_teamwork_scene.go:117` | **`:117` / `:42`** | 未变 |
| `applySettleOutcome` | `teamwork/items.go:572` | **`:626`**（调用 `:601`） | |
| `closeStepsLocked` / `Reclaim` / `releaseAllItems` | `coordinator.go:379/407`、`items.go:894` | **`:379` / `:407` / `items.go:948`** | |
| `SessionLedger` / 编译期断言 | `workunit/session.go:31` | **`:32` / `:41`** | |
| `workunit/session.go` 在跑词表 | `:60` | **`:60–63`**（`StatusQueued = dto.SubAgentQueued` 等） | |
| `factory.NewAgent` / `RegisterNodeSession` 调用 | `node/agent_node.go:137` / `:143` | **`:141` / `:144`** | |
| `BeginNodeWorktree` 调用点 / 注入点 | `:104` / `runtime_plan.go:306` | **`node/agent_node.go:126`（字段 `:39`）/ `runtime_plan.go:310`** | |
| `runtime_plan.go` 收尾/释放 | `:208` / `:408` | **`:208` / `:408`（未变）** | |
| `BindWorkspace` / `BeginNamed` 调用 / `MergeWorkspace` / `ReleaseWorkspaceItem` 清理调用 | `:58` / `:66` / `:81` / `:122` | **`:58` / `:66` / `:81` / `:119`** | |
| `gitWorktreeRegistered` 人读格式 → porcelain | `:898` | 已合并到 **`:992 worktreeRegisteredWith`** | |
| `jobs.Scope` 命中 | `coordinator.go:217/426/627`、`items.go:412`、`jobs_events.go:123` 等 | **`coordinator.go:217/426/627`、`items.go:412`（未变）** | |
| `CHANGELOG.md:112` | `:112` | **`:112`（未变）** | M0 作业面迁移被挡住 |

**§三 三条命令读数要点（复核后）**：

- `NoteWorktree` 全仓源文件命中 **2 处**（定义 `subagent_sessions.go:806` + 唯一调用点 `runtime_plan.go:151`），
  其余全是注释/文档/测试 —— 与清单一致（只是定义行号 +3）。
- `security.NewProcessTree` 源文件命中 **1 处**（`router.go:571` 经变量 `newExecProcessTree`）；清单原文的
  "两个调用点"（`async_run.go:245` / `router.go:555`）**已不成立**：两条链现在都经过
  `newProcessTreeCommand`（`:570`）。
- `git status --porcelain` 源文件命中 **1 处**（`worktree_manager.go:799`）；清单原文的"三处"
  （`:689` / `:750` / `runtime_teamwork.go:861`）已收成一处（③B）。

**§四 U2/U5/U6 三段里的锚点**同样按上表刷新；三段"结论（已落地）"的**事实**经本轮独立复核成立，
但两处措辞需要更正（见 §2、§5）。

---

## 2. U2 正面回答：teammate 现场是否被重复登记（记录 `Worktree` 栏 vs 计划/账本认领）

**问**：会不会产生第二个目录 / 会不会互相覆盖登记 / 哪一份先到决定注册表内容？

**答（已确认的两半）**：

1. **不会产生第二个目录。** 注册表的键是 `nodeID`（`worktree_manager.go:401 register`），现场目录名与分支名
   只按 nodeID 拼且只有一处拼法（`sceneNameInfix :383` / `sceneDirName :386` / `scenePath :396` → 分支
   `seelex/<nodeID>`）。两个来源算出的路径**逐字符同源**，`Adopt :350` 与 `beginNamed :282` 进门先判
   "已在册就原样返回"（`Restore :617` 同样在第一句 `if w.worktrees[record.NodeID] != nil { continue }`）。
   用例：`seelebridge/worktree/worktree_registration_sources_test.go:43 TestSceneRegistrationSourcesAgreeInBothOrders`
   两种顺序各跑一遍，断言 `RegisteredCount() == 1` 且路径相等、现场里的未提交产出与分支一件不少。
2. **不会互相覆盖登记。** 两条入口都是"已在册不覆盖"（③U2 落地，`c20fccc`）；
   用例 `worktree_registration_sources_test.go:97 TestRestoreDoesNotDowngradeLiveRegistration`。

**答（第三半 —— 与清单原文措辞不符，此处更正）**：

3. **"哪一份先到决定注册表内容"这件事并没有消失，只是被固定成了"记录投影那一份"。**
   恢复链的调用顺序是**固定的**：`runtime_subagent_recovery.go:138 Restore(records)` 先，
   `:144 adoptTeamworkScenes` 后（→ `Adopt`）。所以对"记录里也有、计划/账本里也有"的 nodeID，
   **先到的那份是记录投影**，`Adopt` 进门就被 `existing != nil` 挡回。
   而 teammate 记录带的现场栏**只有 Path/Branch**：`workunit_team_records.go:97 teamUnitWorktreeRecord`
   返回 `sessionstore.NodeWorktreeRecord{Path, Branch}`（结构体本身有 `MainBranch`/`BaseCommit`
   —— `sessionstore/node_session_store.go:36–37`，是**写方没填**）。
   ⇒ 新进程里 `Restore` 会用"缺栏位"的那一份登记现场，而 `beginNamed`（重派时 `BindWorkspace :66`
   走的那条）**不会刷新**已在册登记。

**结论：`已确认`**（登记不重复、不覆盖）**+ `仍开放`**（"弱登记先到"这一段没有用例覆盖、
本轮不允许改代码所以不能确证行为后果）。

- 需要什么实验（**不改代码**也能做一遍判定）：真 git + 新管理器，构造 teammate 形态记录
  （`{Path, Branch}` 两栏，`MainBranch/BaseCommit` 空，形态与 `saveTeamUnitRecord` 落盘一致），
  顺序 `Restore` → `BeginNamed(nodeID)` → 观察：
  ① `WorktreeForNode(nodeID).MainBranch` 是否为空；
  ② 随后 `Finish` 的读数 —— `branchBehindBase :835` 拼的是 `"HEAD.."+wt.MainBranch`（空 → `HEAD..`），
     `commitCountSince :847` 拼 `wt.BaseCommit+"..HEAD"`（空 → `..HEAD`）；`alignMergeTarget :553` 在
     `target == ""` 时**直接 `return nil`**，即静默降级成"合进当前 HEAD"。

  > **2026-10-06 更正（实验已跑完：wi-6 / 提交 `d689aac`）**：上面原推测的「两者都会拿到 git 的
  > `ambiguous-argument` 错误」**与实测不符** —— 本机 git 2.51 对空的一侧**不报错**：
  > `git rev-list --count "HEAD.."` → `0`（exit 0）、`git rev-list --count "..HEAD"` → `0`（exit 0）。
  > 于是缺栏现场被读成"没落后、没提交"，`Finish` **报成功**并一路走到 `cleanup`：现场目录与
  > `seelex/<nodeID>` 分支被删、已提交的产出一个字节都没合回 main。**实测后果比"M2 静默失效"更重：
  > 静默丢产出**（红灯原文已进 `d689aac` 提交正文；`_logs/wi6_red.txt` 只存在于该现场内，`_logs/` 被 gitignore）。
  既有三条用例**都绕开了这一段**：`worktree_registration_sources_test.go:43/:132` 喂的记录
  **填满了** `MainBranch/BaseCommit`（`:82–85`、`:157–160`），`:97` 那条只在"本进程已有活登记"时验证不降级。
- 相邻事实（同一张表被两条链读，**本条不在 U2 判据内，只报读数**）：
  `RestoreSubagentAnchors :114` 把 `nodeSessionStore.List` 的**全部**记录同时喂给
  `subagentSessions.Restore :131`、`subagentTree.Restore :134` 与 `worktreeMgr.Restore :138`；
  `SubagentTree.Restore`（`session/subagent_tree.go:367`）只按 `record.NodeID != ""` 过滤，**不区分这一格是谁的**，
  于是 teammate 单元记录（NodeID = `<role>-<itemID>`）会以 `interrupted` 落到子代理树/工作表格上。
  是否可见、是否要过滤 —— **仍开放**（需要一条"重启后 teammate 记录不许长成子代理树节点"的用例）。

---

## 3. U5 正面回答：两条链在进程树退化时的对外主张是否一致

**装配面：一致，且只有一份（已确认）。** 唯一助手 `newProcessTreeCommand`
（`seelebridge/tools/router.go:570`：`NewProcessTree` → `CommandContext` → `winhide` → `Dir` →
`ConfigureHiddenCommand` → `ConfigureProcessTree` → `cmd.Cancel = tree.Terminate()` → `WaitDelay`）
+ `startWithProcessTree :584`（起命令 + `Attach`，挂不上**不放弃执行**）。
调用点：后台链 `async_run.go:245/256`；同步链 `router.go:606/610` 与 docker 重试 `router.go:653/656`。
**超时/取消策略有意不并**（后台 `WithoutCancel + asyncHardCap` vs 同步 `scopedToolTimeout`），
理由写在 `router.go:562–569` —— 与 step-3-goal §3-C 的口径一致。

**读数面：过去不一致，现在"都说得出口"，但**对外形状**仍不同（`仍开放`）。**
- 后台链：判据折进探针 `seelebridge/tools/async_probe.go:64`（观察行）与 `:127`
  （`AsyncRunInfo.Degraded`，即工作表格那一栏）。
- 同步链（bash / bash_read，含 docker 重试）：`router.go:617` / `:658` 调 `noteProcessTreeDegraded`
  （`:710`，判据 `tree.Degraded()`），产出 `bash.process.degraded` 诊断 → `application/console` 的
  `LogBashEvent`。**不退化时一个字都不发**（既有"诊断阶段逐个相等"用例是守卫）。
- 判据本身一处：`seelebridge/security/process_tree_windows.go:134 Degraded()`
  = `t.job == 0 || t.attachFailed`（`:140`），残②（`Attach` 失败也算退化）已落地
  （`process_tree_attach_test.go`）；POSIX 侧恒 false（注释写明平台差异）。

**与 wi-2 结论是否一致**：wi-2 报"装配口径一致、读数过去不一致"，本轮**独立核对一致**；
本轮补充一句它没说死的现状：**同步链的退化事实只到后端诊断口，不进工具结果**（要不要进是独立一批的决定，
涉及对外形状/dto）→ `仍开放`。

---

## 4. U6 正面回答：在跑状态字面量残留点清单

判据词表 = `seelebridge/workunit/session.go:60–63`（`StatusQueued/StatusRunning` 直接引
`dto.SubAgentQueued/SubAgentRunning`）+ `InFlight`。命令：
`git grep -n -e '"running"' -e '"queued"' -e '"done"' -e '"failed"' -e '"interrupted"' -e '"completed"' -e '"killed"' -- "*.go" ":(exclude)*_test.go"`。

| # | 文件:行 | 现状字面量 | 判定 | 理由 |
|---|---|---|---|---|
| 1 | `seelebridge/fork/tool.go:212` | `"handle": …, "state": "running"`（受理回执 `jobs[].state`） | **应转调** `dto.AsyncStateRunning.String()` | 这一栏就是**后台作业状态那一格**（`dto.AsyncState`），而同文件 `:243` / `:255` 已经在用 `dto.AsyncStateFailed` / `dto.AsyncStateDone` 调 `deps.Jobs.Complete` —— 同一文件同一格两种写法；且 `fork/tool.go` **不在**门禁"后台作业状态"格的写方清单里（`e2e/subagent_status_vocabulary_gate_test.go:66–84`），也不是"计划批次结果状态"格的写方 |
| 2 | `seelebridge/fork/tool.go:257` | `"state": "done"` | **应转调** `dto.AsyncStateDone.String()` | 同上 |
| 3 | `seelebridge/runtime_role_turn.go:490` | `Status: "running"`（`dto.RoleToolActivity`） | **应转调** `dto.ToolEventRunning.String()` | 该字段注释自己写"取 running \| success \| error（与 `SubagentToolEvent` 同词表）"（`application/contract/dto/role_tool_activity.go:36`）= **工具事件状态那一格**；写点写裸字面量，且 `runtime_role_turn.go` 不在门禁"工具事件状态"格写方清单（gate `:104–114`） |
| 4 | `seelebridge/runtime_role_turn.go:502/504` | `status, message := "success", ""` / `status, message = "error", …` | **应转调** `dto.ToolEventSuccess` / `dto.ToolEventError` | 同上（第二帧写点） |
| 5 | `application/core/service.go:125` | `if strings.TrimSpace(event.Status) == "running" { kind = EventTeammateToolStarted }` | **应转调**（读方） | 读的正是 #3/#4 写的那个字段；用字面量判"在跑"就是把同一格再抄一份（本项目对"状态比较"形态的既有口径） |
| 6 | `seelebridge/runtime_teamwork_board.go:305–306` | `teamworkMemberRunning = "running"` / `teamworkMemberFree = "free"` | **待定格的"家"，不是就地转调** | 这是**另一格**：取值面 `running\|free`（"这个人此刻在不在干活"），`dto` 里没有对应枚举（`dto.TeamworkMemberView.Status`）。处置顺序应是：先把这个格登记进 `docs/arch/state-machine-inventory.md` §2/§3，再决定它归哪一格的父枚举 |
| 7 | `seelebridge/plan/tool_provider.go:517/521` | `case "failed":` / `case "completed":`（`nr.Status`） | **不是我们的格，保留** | 读的是**框架** `workplanTypes.WorkPlanResult.NodeResults[].Status`（Seele 的词）；同文件写 `plan_run` 结果的 status 已走 `dto.PlanRunStatus`。登记为边界 |
| 8 | `seelebridge/events_unified.go:89` | `if event.Status == "failed"` | **保留（边界）** | 框架 `SummaryEvent.Status`（U6 ④ 已登记"不归我们的契约"） |
| 9 | `seelebridge/internal/telemetry/summary.go:27/193` | `Status: "failed"` / `status = "failed"` | **保留（边界）** | 框架遥测的状态词，与 `StatusError` 同族 |
| 10 | `seelebridge/tools/async_exec.go:1032` | `Status: "killed"`（回执状态词） | **仍开放（第五批）** | 取值面 `observed\|killed\|already_finished\|finished`，目前只在门禁白名单里登记了一条理由；U6 ⑤ 的口径是"连 `job_manage` 的四种 op 一起收" |
| 11 | `seelebridge/runtime_teamwork_jobs.go:79` | 注释里的 `running\|done\|failed\|killed` | **不动** | 注释，说明"框架 `jobs.State*` 与我们那张表同形" |
| 12 | `seelebridge/session/subagent_sessions.go:515` | `else if _, running := s.sessions[nodeID]; running` | **已不是字面量** | 清单原文把它记成"自己写字面量"，现状是**局部变量名**；同文件状态写点 `:208` 已是 `SubAgentDone.String()` |
| 13 | `seelebridge/workunit/contract.go:117` | `OutcomeFailed OutcomeKind = "failed"` + `classify.go` 哨兵 | **就是家** | 契约自己的词表；§一 4) "收尾分类→落点映射"两处载体（`teamwork/items.go:626` / `workunit_team.go:48`）都读它 |
| 14 | `-`（旁证，非本波范围） | `sessionstore/lifecycle.go:34`、`sessionstore/session_granular.go:38`、`application/event/hub.go:135`、`application/core/task_context/turn_status_record.go:29` | **不动** | 分别是 sessionstore 队列/会话生命周期格、压缩进度格、回合存档边界读法（各自有口径文件），都不是本次"在跑词表"那一格 |

**摘要读数**：清单 §一4) 与 §四 U6 记的三处（`runtime_subagent_resume.go:329`、
`subagent_sessions.go:515`、`subagent_tree.go:422/424`）**全部已收口**；清单 §四 U6 ⑥
（记录状态那一格的枚举化）**也已落地**——`application/contract/dto/subagent.go:12`
是 `type SubAgentNodeStatus uint8` + iota（words 表 `:27`），落盘转换点
`runtime_subagent_resume.go:369 nodeStateOfRecord`。

本轮**新发现 5 个残留写/读点**（#1–#5），其中 #1–#2 是"同一文件两种写法"，
#3–#5 是"同一格第三个写点与一个读点"——**都未被门禁覆盖**。

**顺带一条口径读数（`仍开放`，需要人来定，不是缺陷）**：门禁"记录状态"格声明的取值面是
`{queued, running, done, failed}`（`e2e/subagent_status_vocabulary_gate_test.go:71`），
而 `dto.SubAgentNodeStatus` 现在有 6 个值（多 `unknown` 与 `interrupted`）。
`unknown` 只在 `nodeStateOfRecord:369` 的"认不得"分支出现；`interrupted`
是树投影/恢复侧的词（`session/subagent_tree.go:416 restoredSubAgentStatus` —— 该文件在门禁
这一格的清单里）。**要么把这两个词补进门禁取值面，要么在门禁里写清"它们是读侧产物、不是写侧词"**——
本轮只报读数，不做判定。

---

## 5. 合并分叉现状核查（用户最关心的一条）

**问**：多个子代理/teammate 并发把各自现场合回主分支时，"提交树分叉 / 先合回来的被踢成另一个分支"
现在被哪几道机制挡住？还剩下哪些可能分叉的路径？

**已有机制（4 道，全在 `seelebridge/worktree/worktree_manager.go`）**

| # | 机制 | 锚点 | 挡住什么 |
|---|---|---|---|
| M1 | **收尾单写者 `finishActor`** | 字段 `:172`、创建 `:195`、消费者 `handleFinish :200`、投递 `submitFinish :210`；`Finish :457` 经它执行 `finishExclusive :485` | 同一时刻只有一个收尾在动主工作区（rebase / merge / cleanup 都要动 `.git`），杜绝 `.git/index.lock` 相撞与**交错合并** |
| M2 | **`alignMergeTarget` 先把主工作区切回"现场记录的那条分支"** | `:553`（判据：目标 = `wt.MainBranch`；不一致就 `git checkout` 回去；挡路 → `mergeBlockedError`，切不动 → 硬失败带 git 原文） | **"主工作区当前分支漂走 → 先合回来的产出被踢成另一分支的孤儿"**（2026-10-06 那条红灯的形状） |
| M3 | **`Finish` 的有界重试** | `:457–480`（`awaiting_merge` 阶段，超预算 → `ErrMergeBlockedByMain`，现场保留、结论照常交付） | "主工作区被在途改动挡住"这种**可重试**失败被误判成确定性失败 |
| M4 | **"主工作区挡路"判据只有一份** | `mergeBlockedMarkers :112` + `isMergeBlockedEvidence :125`，被 `alignMergeTarget :571` 与 `finishExclusive :528` 两处共用 | 挡路与**真冲突**分开：真冲突（MERGE_HEAD + 冲突索引留在主工作区）是确定性失败，必须人或主代理收拾（`finishExclusive :531–534`） |

**红灯用例（覆盖到的场景）**

- `seelebridge/worktree/worktree_merge_kickback_test.go:98 TestFinishSecondUnitKeepsFirstMerge`（真 git 仓 +
  真 worktree）：A、B 各自提交 → 收尾 A 前把主工作区切到 `side` → 收尾 A 之后断言
  ① `main` 含 A 的提交（`isAncestor`）② A 的分支指针不被改写 ③ A 的清理不碰 B 的现场与 `seelex/B` 分支指针。
  **这条正是"踢回另一个分支"的正面回归**。
- `worktree_merge_serial_test.go:136 TestFinishSerializesConcurrentMergesIntoMain`：两条收尾同时起跑，
  判据取"**同时进入 merge 的子进程数**必须恒为 1"（`scriptedGit.maxInMerge`）；
  `:183 TestFinishWithoutActorOverlapsMerges` 是**反证对照**（摘掉 actor → 立刻看到 2，红线用例能区分两者）。
- `:217 TestFinishRetriesWhileMainWorkspaceDirtyThenMerges`（挡路 → 等干净 → 合上，且**重试不再重复问审批**）、
  `:253 TestFinishReportsMergeBlockedByMainWithoutFailingScene`（超预算 → 报 `ErrMergeBlockedByMain` 且现场不判死）、
  `:287 TestFinishMergeConflictIsDefinitive`（真冲突 = 确定性失败）、
  `:317 TestFinishBlockedByMainReleasesActorForOtherNodes`（等主工作区干净时**不占 actor**，其他节点照常收尾）、
  `:374 TestBeginNamedReusedByTeammateAndSubagent`。
- 现场释放那一侧不是本项判据，但同属"重复收口"：`worktree_release_judgment_test.go:42`、
  `worktree_vanished_scene_repro_test.go:41`（"现场被对端收走时 `Finish` 必须报错"——**不许被幂等口径抹掉**）。

**仍可能分叉的路径（只报事实，不改代码）**

1. **`MainBranch` 为空的现场（登记缺栏）会让 M2 静默失效。** `alignMergeTarget :553` 第一句
   `if target == "" || target == "HEAD" { return nil }`，即"没有可切的目标 → 维持原语义（合进当前 HEAD）"。
   而 §2 U2 已核到一条**能造出空 `MainBranch` 的路径**（teammate 记录只带 Path/Branch + `Restore` 先到）。
   ⇒ **M2 在这条路上等于没装**；此时若主工作区分支在两次收尾之间漂走，分叉会回来。
   **已收口**（wi-6 / 提交 `d689aac`）**+ 仍开放（能力面）**：实验（2026-10-06）实测出比"没装"更重的后果 ——
   git 2.51 对空 ref **不报错**，缺栏现场被读成"没落后、没提交"，`Finish` 报成功并把现场与分支删掉
   （静默丢产出；细节与更正见 §2 末）。收口走判据侧（选 (b)）：新增本格唯一判据 `classifySceneMergeTarget`
   + 哨兵 `errSceneFactsIncomplete`，登记缺栏**显式硬失败**且点名叫缺哪一栏，现场目录 / 现场分支指针 /
   主分支 / 主工作区四处一律原样，空 ref（`HEAD..` / `..HEAD`）一个都不许拼；游离 HEAD 维持"合进当前 HEAD"
   原语义；形状变更面仅"登记缺栏"这一种现场，栏位齐全的路径一字不变。用例
   `seelebridge/worktree/worktree_weak_registration_merge_test.go`（真 git + 真 worktree，含反证对照）。
   **仍开放（真正的能力修复在记录侧）**：`workunit_team_records.go:97 teamUnitWorktreeRecord` 只写
   Path/Branch，且 `dto.NodeWorktreeInfo` 不带 `BaseCommit` ⇒「重启后恢复并重派」的 teammate 工作项收尾
   仍然合不回来（现在至少不再静默丢产出，改成显式失败 + 保留现场）。
2. **跨进程并发仍无锁。** M1 是**进程内** actor：两个 seelex 进程（或一次人工 git 操作）同时收尾同一个
   主工作区不在保护范围内。真冲突/M2 的 `checkout` 失败会把它降级成硬失败（不是静默分叉），
   但**没有任何机制阻止**两个进程各自把不同分支合进各自认为的"主分支"。
3. **"主工作区是否脏 / 是否在册 / 是否在工作树清单里"三处口径**（③ 已把脏判定收成一份 `pathDirtyWith :798`；
   "是否在册"是 `sceneRegistered :660` 的规范化比较；"清单解析"只有 `parseWorktreeList :747`）
   现在**同源**，本轮未发现第二份——这一项从"已知缺陷候选"降级为**已确认无重复**。

---

## 6. 下一波九条测试登记（只报读数，不做判定）

> 方法：`git grep -n "func Test" -- <文件>` 与关键字 `git grep`（`AdjustItem` / `milestone` / `elevat`…），
> 不臆断。**"无"= 本次 grep 未命中**。

| # | 用户提出的测试要求 | 仓库现状（读数） |
|---|---|---|
| 1 | 越权提权 | **已有**：`seelebridge/tools/permission_teammate_test.go:24`（域内直跑 / 域外走升级页）、`:79`（升级被拒 = 调用被拒）、`:96`（满档下能力面仍闭合）；`permission_inherit_test.go:35/58/96/150`；派发携带属主权限域 `seelebridge/teamwork/items_test.go:718`。真机档 `real_api_permission_smoke_test.go`（需额度，**未跑**） |
| 2 | worktree 与 Session 生命周期 | **已有**：`seelebridge/teamwork/items_test.go:424 TestWorktreeAndSessionSurviveUntilTeamClose`、`:476 TestTeamCloseEndsEveryLiveBinding`；根包 `teamwork_worktree_lifecycle_test.go`（真 git + 脚本化 provider）；`seelebridge/workunit_team_test.go:581`（收尾策略保留现场）、`:173`（收口闸门保留未合并现场） |
| 3 | 后台执行 | **已有**：`seelebridge/tools/async_exec_test.go`、`async_kill_test.go`、`async_progress_test.go`、`job_contract_test.go`、`job_manage_batch_test.go`、`async_exec_raceproof_test.go`；真机档 `async_tool_wire_live_probe_test.go`、`async_exec_ab_live_test.go`（**未跑**） |
| 4 | 里程碑内先安排完再下一个 | **已有**：`seelebridge/teamwork/items_test.go:248 TestNextMilestoneOpensOnlyAfterEveryItemIsDone`、`:237 TestPlanMilestoneRefusesMilestoneWhoseDependencyIsNotDone`、`:279 TestDispatchRefusesItemInMilestoneBehindTheBarrier`；`seelebridge/teamwork/teamwork_test.go:307 TestMilestoneRefusesMilestoneBehindTheBarrier` |
| 5 | 里程碑内依赖 DAG | **已有**：`items_test.go:299 TestDispatchRefusesItemWhoseDependencyIsNotDone`、`:308 TestDependencyChainReleasesStepByStep`；校验层 `sessionstore/teamwork_items_test.go:40`（跨里程碑依赖被拒）、`:63`（环被拒） |
| 6 | 一 Milestone 内多 Session | **已有**：`items_test.go:331 TestTeammateRunsMultipleItemsEachWithItsOwnSessionAndWorktree`；账本层 `seelebridge/workunit_team_test.go:275–276`（同一里程碑两行不同 SessionID）；烟测 `teamwork_headless_smoke_test.go:678`（同一里程碑两个工作项） |
| 7 | 每 Session 进度详情 UI | **部分有**：子代理侧 `application/core/subagent_detail_test.go:27/59`、`gui/subagent_route_smoke_test.go:23`、`seelebridge/session/subagent_sessions_stream_test.go`；teammate 侧只有工具活动投影 `seelebridge/runtime_role_tool_test.go` 与真机档 `gui/team_workcontent_live_probe_test.go:52`（需额度）⇒ 原判「teammate「每 Session 进度详情」的非真机档端到端 UI 用例：无（仍开放）」。**2026-10-06 补一半（wi-8 / 提交 `cf8d827`）**：勘定 teammate 侧两条读面各带一半 —— 会话详情面 `dto.TeammateSessionLiveView`（`session_id/role/live/running/messages/truncated`，**不带 worktree、不带状态词/阶段**）与看板投影面 `dto.TeamworkWorkItemView`（session id + worktree + 状态词 + 阶段齐全，搬运点 `seelebridge/runtime_teamwork_board.go:354`）；新增 `seelebridge/runtime_teamwork_work_item_progress_test.go` 按"这件事自己的会话号"定位该工作项并钉住后者（含反向：角色会话号不该命中）。**另一半仍开放**：让**一条**读面同时给出会话正文 + worktree + 阶段（需动 wire 契约或前端；锚点 `docs/arch/workunit-progress-read-surface.md` §1.5/§3.1） |
| 8 | 未开始工作的调整 | **只有负向**：`items_test.go:593 TestAdjustItemRefusesStartedAndFinishedWork`（已开始/待验收/已结束都拒绝）。`git grep -n AdjustItem -- "*_test.go"` 只命中这一条 ⇒ 原判「正向路径（未开始的工作可调整成功）：无（仍开放）」。**2026-10-06 已补齐（wi-8 / 提交 `cf8d827`）**：新增 `seelebridge/teamwork/items_adjust_test.go` 3 条 —— ①pending 五项一次改齐（role/name/description/goal/depends_on）+ `team_items` 读回逐字段一致、未提到的格一个不动；②新依赖成硬闸门（改成另一件未完成项 → 派发**显式拒收**且不留会话/现场；依赖 done 后放行）；③running/review/done 负向不回归（补上既有漏掉的 **review**）。实现未改；实现入口 `seelebridge/teamwork/items.go:250`、口径文案 `runtime_teamwork_schema.go:123` |
| 9 | 中断恢复含 UI 与上下文记忆 | **已有**：`items_test.go:519 TestRecoverReportsInterruptedItemsAndKeepsTheirMemory`；`seelebridge/workunit_team_test.go:301`（标中断 + 恢复说明只注入一次）；`seelebridge/runtime_subagent_resume_test.go:68/169/205`；`application/core/interrupted_continue_test.go:22`；`seelebridge/session/subagent_persist_test.go`；UI 侧 `application/core/subagent_detail_test.go:59 TestSubagentSessionDetailCarriesWorktree` |

---

## 7. 复核用的命令原文与原始读数

```powershell
# 基线
git rev-parse HEAD                     # bdbfeba9852d1b22823327f76ff42a58366629cd
git status --porcelain                 # 空

# 1. 函数锚点（清单逐条复核的主来源）
git grep -n "^func " -- seelebridge/worktree/worktree_manager.go
git grep -n -e "cleanup" -e "CleanupWorktree" -e "cleanupWorktreeWith" -e "worktreeRemoveError" -e "deleteWorktreeBranchWith" -- seelebridge/worktree/worktree_manager.go
git grep -n -e "alignMergeTarget" -e "finishActor" -e "finishExclusive" -e "submitFinish" -e "handleFinish" -e "sceneRegistered" -e "worktreePathEqual" -e "isManagedPath" -e "scenePath" -- seelebridge/worktree/worktree_manager.go

# 2. 脏判定收口读数（③B）
git grep -n -e worktreeDirty -e pathDirty -e "git status --porcelain" -- seelebridge/
#   → 实现只剩 worktree_manager.go:798 pathDirtyWith；runtime_teamwork.go:859-861 已改成注释；
#     "status --porcelain" 只剩 :789/:791 注释与 :799 实现那一行

# 3. U2：登记来源与顺序
git grep -n -e "RestoreSubagentAnchors" -e "adoptTeamworkScenes" -e "worktreeMgr.Restore" -e "func " -- seelebridge/runtime_subagent_recovery.go
#   → :114 定义；:138 Restore(records)；:144 adoptTeamworkScenes；:147 Prune
git grep -n -e saveTeamUnitRecord -e teamUnitWorktreeRecord -e func -- seelebridge/workunit_team_records.go
#   → :68 saveTeamUnitRecord（Worktree 栏在 :86）；:97 teamUnitWorktreeRecord（只填 Path/Branch）
git grep -n -e "NodeWorktreeRecord" -A 12 -- sessionstore/
#   → node_session_store.go:33 结构体含 MainBranch:36 / BaseCommit:37（是写方没填，不是没有这两栏）

# 4. U5：进程树装配与退化读数
git grep -n -e startAsync -e newScopedCommand -e NewProcessTree -e ConfigureProcessTree -e newProcessTreeCommand -e startWithProcessTree -e noteProcessTreeDegraded -- seelebridge/tools/
git grep -n Degraded -- seelebridge/tools/async_probe.go
git grep -n -e "func NewProcessTree" -e Attach -e Degraded -e attachFailed -- seelebridge/security/process_tree_windows.go

# 5. U6：字面量残留
git grep -n -e '"running"' -e '"queued"' -e '"done"' -e '"failed"' -e '"interrupted"' -e '"completed"' -e '"killed"' -- seelebridge/ ":(exclude)*_test.go"
git grep -n -e '"running"' -e '"queued"' -- "*.go" ":(exclude)*_test.go" ":(exclude)vendor/**"   # 需要再按目录切分看全

# 6. 合并分叉：机制与红灯
git grep -n "func Test" -- seelebridge/worktree/worktree_merge_kickback_test.go seelebridge/worktree/worktree_merge_serial_test.go
#   → kickback:98；serial:136/183/217/253/287/317/374

# 7. 九条登记
git grep -n "func Test" -- seelebridge/teamwork/items_test.go
git grep -n -e AdjustItem -- "*_test.go"
git grep -n -i milestone -- "*_test.go"        # 量较大，按里程碑屏障/DAG 关键字筛出上表条目
git grep -ln -i -e elevat -e 越权 -- "*_test.go"
```

**未做的复核（明说，避免误读）**：`e2e/workunit_ports_test.go` 的门禁例外是否真的撤掉（③E 判据）只看
了 `git grep "worktree\." -- seelebridge/workunit/` 的命中（只剩注释/README），没有跑门禁；
`git grep` 的字面量扫描在 `application/` + `internal/` + `sessionstore/` 三个目录上因输出预算被截断过一次，
U6 表里的第 14 行是那次扫描的**部分**读数（已注明"非本波范围"）。

---

## 8. 红线与未做

- **未改任何 `*.go`**：本轮 `git diff --stat` 只应含本文件与 `docs/arch/workunit-duplication-inventory.md`。
- **不改判据**：清单的 §二 归并建议、§四 U1/U3/U4 全部原样保留；只改锚点与与现状不符的现状句
  （"现有实现数"列按现状改写并标 `(③/② 已落地)`）。
- **需要改代码才能确证的结论一律标"仍开放"**，共 4 条：
  ① U2 "弱登记先到"（§2）；② U6 #1–#5 五个残留写/读点（§4）；③ U5 同步链退化事实不进工具结果（§3）；
  ④ 合并分叉路径 1（`MainBranch == ""` 时 M2 静默失效，§5）与路径 2（跨进程无锁）。
  每条都给了"需要什么实验"。
- 本轮**未**读取 `config/accounts.yaml` / `*.local.yaml`；未在仓库内留临时文件。

---

## 9. 落地记录（leader 复核 · 2026-10-06）

§8 那四条「仍开放」里，① 与其相邻的合并分叉路径 1、以及 §6 的 #7/#8 已被 m2 里程碑处理；leader 逐件独立复核后验收（不走 teammate 自述）。main 头：`0adef97` →（wi-7 `f0bed6a`）→（wi-8 `cf8d827`）→（wi-6 `d689aac`）。

| 工作项 | 工件 | leader 独立读数（亲跑，非自述） |
|---|---|---|
| wi-6「弱登记」收口 | `d689aac`，3 文件 = `worktree_manager.go` +122/−15、新用例 `worktree_weak_registration_merge_test.go` 224 行、README +13 | `go test ./seelebridge/worktree/ ./seelebridge/teamwork/ -count=1` → **ok** 27.5s / 1.3s，exit 0；通读新用例：断言非空转（`errors.Is(errSceneFactsIncomplete)` + 诊断点名 `MainBranch` + `gitCallLog` 里空 ref 零容忍 + 现场/分支/main/主工作区四处原样，另有反证对照证明根在"弱登记先到"） |
| wi-8 两条空白用例 | `cf8d827`，3 个新文件（2 个用例文件 + 线记录），**零生产代码**、既有断言一字未改 | `go test ./seelebridge/teamwork/ -count=1` → **ok** 1.337s；`go test ./seelebridge/ -run TestTeamworkBoardReadsWorkItemProgressDetail -v -count=1` → **PASS** 0.03s |

**「合并不分叉」的复核读数（与用户看的提交树一致）**

- `git log --graph --oneline -25` → main 是**单亲线性**链；`git log --merges --oneline -20` → 本轮 team run **零合并提交**（只有 4 条历史老 merge）。⇒ 验收/收口期的 rebase 语义成立，未出现 merge-back 分叉。
- teammate 自报的 `f3341b3` 仍可寻址且提交正文与 `d689aac` 同源 ⇒ **rebase 改写了哈希**（`f3341b3` → `d689aac`），这正是"没有分叉"的成因。
- **注意**：提交树**看不见**缺栏登记那类失败 —— 现场与分支被删、产出一个字节没合回 main，而 `Finish` 报成功；**丢产出不产生分叉**，所以"提交树没有分叉"不能当作这条路的证据（该路已由 `d689aac` 换成显式硬失败 + 保留现场）。

**仍开放（下一波输入，均为能力面/产品面，不是缺陷回归）**

1. 记录侧补栏：`workunit_team_records.go:97 teamUnitWorktreeRecord` 只写 Path/Branch、`dto.NodeWorktreeInfo` 不带 `BaseCommit` ⇒「重启后恢复并重派」的 teammate 工作项收尾合不回来；备选路（重派时 `BeginNamed` 按 git 现值补空栏）需先定"已在册不刷新"这条口径。
2. §6 #7 的另一半：让**一条**读面同时给出会话正文 + worktree + 阶段。
3. §5 路径 2：跨进程无锁（两个 seelex 进程同收一个主工作区）。
4. §3 U5 读数面：同步链退化事实是否进工具结果（产品判断）。
5. §4 U6 剩余点：`#6`（`running|free` 是另一格，归格待定）、`#10`（`"killed"` 与 `job_manage` 四 op 同批）。
