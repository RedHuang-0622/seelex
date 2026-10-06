# 同一件事在仓库里有几份实现 —— 只读盘点

> 口径：**只报事实，不改任何现有代码**。每条给 `文件:行` 锚点 + 一句话现状。
> 盘点对象：worktree 生命周期 / 会话生命周期 / 后台作业与进程树 / 其他「同一判断或同一动作两份」。
> 本文件为新增文档，盘点期间未编辑、未移动任何既有文件。
>
> 相关契约包（作为"应该归到哪个父实现"的落点）：`seelebridge/workunit`（`contract.go` /
> `classify.go` / `session.go`）——它已经存在，但**只有部分调用链完成接线**，本次盘点记录的
> 正是"契约已定义、链上仍有第二份"的现状。

---

## 一、盘点表（同一件事 × 现有实现数 × 锚点）

### 1) worktree 管理

| 同一件事 | 现有实现数 | 锚点 | 现状一句话 |
|---|---|---|---|
| 建现场（`git worktree add` + 命名 `seelex/<nodeID>`） | **1 份实现 / 2 条调用链** | 实现 `seelebridge/worktree/worktree_manager.go:272`（`beginNamed`，四条路径：认领/不碰/复用分支/新建） | 两条链最终都落到 `beginNamed`，现场创建本身**没有**复制。 |
| ├ 子代理链调用点 | | `seelebridge/node/agent_node.go:104`（`BeginNodeWorktree`）→ `seelebridge/runtime_plan.go:145`（`beginNodeWorktree`）→ `worktree_manager.go:246`（`Begin`，仅 `RoleSubAgent` 才建） | 子代理经 `node.Deps.BeginNodeWorktree` 建现场。 |
| └ teammate 链调用点 | | `seelebridge/workunit_team.go:533`（`teamUnit.Begin`）→ `seelebridge/runtime_teamwork_items.go:58`（`BindWorkspace`）→ `worktree_manager.go:256`（`BeginNamed`） | teammate 经 `BindWorkspace` 建现场，与子代理同实现、同命名（用例 `worktree_merge_serial_test.go:374` 钉住）。 |
| 收尾合并（rebase → 提交判定 → 审批 → merge → cleanup） | **1 份实现 / 2 条调用链** | `worktree_manager.go:432`（`Finish`）→ `:460`（`finishExclusive`，收尾单写者 actor） | 合并是唯一一份实现；子代理走 `runtime_plan.go:208`（`finishNodeWorktree`），teammate 走 `runtime_teamwork_items.go:81`（`MergeWorkspace`），二者都在合并后补一次 `Release`（`worktree_manager.go:525`）。 |
| 主工作区在途改动保护 | **1 份实现** | `worktree_manager.go:76`（`ErrMergeBlockedByMain`）/ `:120`（`isMergeBlockedEvidence`）/ `:432`（预算内有界重试，阶段 `awaiting_merge`）/ `:162`（`finishActor` 串行化） | 两条链共享同一份保护；没有第二份。 |
| **恢复名单登记（`NoteWorktree`）** | **2 份机制，且子代理那 1 份全仓只有 1 个调用点** | 子代理：`seelebridge/session/subagent_sessions.go:803`（`NoteWorktree`）唯一调用点 = `runtime_plan.go:151`（在 `beginNodeWorktree` 内） | 子代理「建现场 → 立刻落持久节点记录」是一条完整链。 |
| └ teammate 的登记 | | teammate **不调** `NoteWorktree`；改由 `workunit_team.go:134`（`saveTeamUnitRecord`，直接写 `NodeSessionStore`）+ 计划/账本认领 `runtime_teamwork_scene.go:117`（`adoptTeamworkScenes`） | **teammate 链少了「进入子代理恢复名单」这一步**：既不写 `SubagentSessions` 的 worktree 槽，也不在 `NoteWorktree` 的调用面上。现状靠 `RestoreSubagentAnchors`（`runtime_subagent_recovery.go:113`）里后接的"从团队计划 + 绑定账本认领"补回来（F4 的修法）。 |
| 恢复登记的两条来源（同一份现场） | **2 份** | ① 记录投影：`worktree_manager.go:538`（`Restore(records)`）读 `NodeSessionStore`（teammate 的 `saveTeamUnitRecord` 也往同一张表写 `Worktree` 一栏）② 计划+账本：`runtime_teamwork_scene.go:117`（`adoptTeamworkScenes` → `WorktreeManager.Adopt` `:340`） | teammate 现场现在**有两个登记来源**（记录里的 `Worktree` 栏目 + 计划/账本），哪一份先到决定注册表内容；两者都在 `Prune`（`:612`）之前跑。是否重复登记待验证（见第四节 U2）。 |
| 回收/清理现场（删目录 + 删分支） | **2 份** | ① `worktree_manager.go:779`（`cleanup`，成功路径，**非幂等**）② `worktree_manager.go:822`（`CleanupWorktree`，导出，**幂等**：目录/分支/登记任一不在 = 已释放） | 同一动作两份实现且语义不同（幂等 vs 非幂等）；两处都出现就是"缺陷 A：exit status 128 / not a working tree"的来源（注释在 `:806`）。 |
| 残留兜底回收 | 1 份 | `worktree_manager.go:612`（`Prune`：不在册 **且** 干净才删） | 只有一份，无重复。 |
| 「工作区是否脏」判定 | **3 份** | ① `worktree_manager.go:749`（`worktreeDirty(wt)`）② `worktree_manager.go:688`（`pathDirty(path)`）③ `seelebridge/runtime_teamwork.go:861`（`worktreeDirty(root)`，包级） | 三处都跑裸 `git status --porcelain` 判非空；CRLF 幻影脏风险因此有三处暴露面。 |

### 2) 会话管理（新建 / 重启 / 结束）

| 同一件事 | 现有实现数 | 锚点 | 现状一句话 |
|---|---|---|---|
| 新建会话 | **2 份** | 子代理：`seelebridge/node/agent_node.go:137`（`factory.NewAgent`）+ `:143`（`RegisterNodeSession`），另有一条公开构造 `seelebridge/runtime_subagent_session.go:20`（`NewSubagentSessionWithID`）；teammate：`seelebridge/runtime_role_turn.go:324`（`roleSessionFor`）开 `roleTurnState.sessions` 引擎槽 | 子代理的会话是**框架 Session** 并登记进 `SubagentSessions`；teammate 的会话是进程内引擎槽。两条链各开各的会话（`workunit_node.go:60` 明确"Begin 不越权替它开会话"）。 |
| 运行期落盘（"跑到哪、现场在哪"） | **2 份写入 / 1 份记录形状** | 子代理：`seelebridge/session/subagent_sessions.go:474`（`buildRecordLocked`，经 actor 写 `NodeSessionStore`）；teammate：`workunit_team.go:134`（`saveTeamUnitRecord`，**直写** `NodeSessionStore`） | 记录形状已统一（`sessionstore.NodeSessionRecord`，`workunit/session.go:31` 的 `SessionLedger` 编译期断言），但**写入口有两条**（一条经 actor、一条直写）。 |
| 会话状态词表 | **3 处** | ① 子代理字面量 `subagent_sessions.go:515`（`"running"`/`"queued"`）与 `seelebridge/runtime_subagent_resume.go:329`（`case "queued","running"`）② teammate 常量 `workunit_team.go:41`（`running`/`done`/`failed`）③ 契约 `seelebridge/workunit/session.go:60`（`StatusQueued`/`StatusRunning` + `InFlight`） | 判"是否在跑"的**字面量仍有两处**没走契约：`runtime_subagent_resume.go:329` 自己 `switch`，`subagent_sessions.go:515` 自己写字面量；teammate 侧 `workunit_team.go:50` 已改成转调 `workunit.InFlight`。 |
| 重启回灌（认领 + 回灌 + 判中断） | **2 条链** | 子代理：`seelebridge/runtime_subagent_recovery.go:113`（`RestoreSubagentAnchors`）+ `seelebridge/runtime_subagent_resume.go:148`（`ResumeInterruptedSubagents`，七步模板）；teammate：`workunit_team.go:331`（`RecoverTeamworkUnits`）+ `seelebridge/runtime_teamwork_scene.go:117`（认领） | 两条链各有一套"重启后读回"；契约的 `Recover` 只在 `workunit_node.go:114` / `workunit_team.go:592` 做适配转发。 |
| **恢复续跑（重派同一件事）** | **1 份，只存在于子代理链** | 子代理：`runtime_subagent_resume.go` 的 `Reexecute`（`fork_subagents` 同键重跑） | teammate 链只有"认领现场 + 注入恢复说明"，**没有重派/续跑动作**；重派交给 leader 的 `team_dispatch`。同一件事（恢复续跑）在 teammate 链是缺的。 |
| 恢复说明（resume note）的状态容器 | **2 份同形状实现** | 子代理：`runtime_subagent_resume.go:49`（`subagentResumeState.notes`，键 = 节点 id，`SubagentResumeNote` 读取）；teammate：`workunit_team.go:265`（`teamResumeState.notes`，键 = 角色会话号，`consumeTeamResumeNote` 读取，装配点在 `runtime_teamwork.go:698`） | 两个 map 同形状、同语义（system 注入、读完即消），只是键与读点不同；注释自己也承认"同一形状、同一语义"（`workunit_team.go:262`）。 |
| 结束/清会话 | **2 份** | 子代理：`UnregisterNodeSession` + `NoteOutcome` + `persistSubagentConclusion`（`runtime_subagent_recovery.go:67`）+ 记录删除（`workunit_node.go:93` `Reclaim`）；teammate：`runtime_teamwork.go:836`（`ResetSession`：清内存历史 + 落引擎槽）+ `workunit_team.go:229`（`clearTeamUnitRecord`） | 两条链各写各的"结束"；`ResetSession` 只认角色会话号，记录清点分散在 `workunit_team.go` 两处。 |
| 「这件事是否已中断」判据 | **2 份** | ① 团队账本侧 `seelebridge/teamwork/coordinator.go`（`Recover` 用 `handleAlive(item.Handle)` 判 `report.Interrupted`）② 记录侧 `workunit_team.go:331`（`teamUnitSurfaceAlive`：角色会话在册 **或** 作业句柄在册） | 同一件事两个判据，`RecoverTeamworkUnits` 用**并集**把两者合起来（账本侧 + 记录侧各一条）。 |
| 会话的存储作用域解析 | **2 份** | ① `runtime_subagent_recovery.go:55`（`sessionProjectIDFor`，读 `sessionWorkspaces` 绑定表）② `workunit_team.go:106`（`teamUnitScope`，读 `backend.KeyFor`） | 同一件事（"这条会话的记录落哪个 project/session 目录"）两处各算一次；代码注释已把这个风险写成事实（`workunit_team.go:106` 之上）。 |

### 3) 后台子进程树 / 作业面

| 同一件事 | 现有实现数 | 锚点 | 现状一句话 |
|---|---|---|---|
| 作业登记表 / 状态机 / 取回 | **2 张表** | ① `seelebridge/tools`：`async_exec.go:251`（`asyncRegistry.beginJob`）、状态机 `async_exec.go` 全文、`jobManager`（`job_contract.go:301` `Kill` 等）② Seele `jobs.Manager`：装配点 `runtime_teamwork.go:88`（`jobs.New(...)`），执行体 `seelebridge/teamwork/executor.go:26` | bash_bg / read_batch / fork_subagents 走 tools 表；teammate worker 作业走 `jobs.Manager`。**同一件事（作业从创建到回收）两套表**。`CHANGELOG.md:112` 记载"M0 最后一步——把后台作业面搬到 `jobs.Manager`——被挡住"。 |
| 作业 scope 模型 | **2 份** | tools 侧按 `JobSpec.SessionID`（`job_contract.go:44`）；jobs 侧按 `jobs.Scope{Session, Subject}`（`teamwork/coordinator.go:217`、`items.go:412`） | 隔离键不同：一个只有会话，一个是会话 + 主体（`emp_<role>`）。 |
| 作业回收入口 | **2 份、时机与粒度不同** | tools：`seelebridge/runtime_tools.go:179`（`ReleaseSessionAsync`，会话销毁即杀）→ `tools/async_run.go:119`（`CloseSessionAsync`）→ `async_exec.go:436`（`killSession`）；全局 `async_run.go:107`（`CloseAsync`，注册进 `r.lifecycle`，`runtime_tools.go:169`）。jobs：`teamwork/coordinator.go:407`（`Reclaim`）→ `jobs.Manager.Reclaim`，时机 = `team_retire`/`team_close`（`coordinator.go:379` `closeStepsLocked` + `items.go:894` `releaseAllItems`） | **两条链没有走到同一处**：tools 表按会话销毁回收，jobs 表按"角色/团队收口"回收；`team_retire` 已明确**不回收作业**（"谁还在跑留到整队收口"）。 |
| 子代理作业的完成（被动终态） | 1 份 | `tools/job_subagent.go:73`（`AddSubagentJob`）/ `:108`（`CompleteJob`，执行体收尾合成终态）；调用点 `fork/tool.go:164/204/242/249` | 只有一份；`subagentJobsAdapter`（`runtime_plan.go:233`）是薄转发。 |
| 进程树终止（Windows Job Object） | **1 份实现 / 2 处装配序列** | 实现 `seelebridge/security/process_tree_windows.go:98`（`ProcessTree`）、`:110`（`NewProcessTree`，KILL_ON_JOB_CLOSE）；调用点 ① `tools/async_run.go:245`（后台命令 `startAsync`）② `tools/router.go:555`（同步命令 `newScopedCommand`） | 进程树/作业对象**只有 tools 链用到**（`NewProcessTree` 全仓就这两个调用点）；teammate 作业是进程内 goroutine（`teamwork/executor.go`），本身没有进程树，它内部起的 bash 仍落 tools 链。两处装配序列（`ConfigureProcessTree` → `cmd.Cancel = tree.Terminate()` → `Attach` → `Close`）是同一动作写了两遍。 |
| 作业生命周期事件流 | 1 份 | `seelebridge/jobs_events.go`（消费 `jobs.Manager.Events()` 扇出信号 → 投影 `frameworkevent.Event`） | 只有一份，作用于 jobs 表；tools 表另有一条读面（`AsyncRunsSnapshot`）。 |

### 4) 其他「同一判断 / 同一动作出现两份」

| 同一件事 | 现有实现数 | 锚点 | 现状一句话 |
|---|---|---|---|
| 「工作区脏」判定 | 3 份 | 见 §1 末行（`worktree_manager.go:749` / `:688` / `runtime_teamwork.go:861`） | 三处裸 `git status --porcelain`。 |
| 现场清理（删目录+删分支） | 2 份 | `worktree_manager.go:779` vs `:822` | 幂等语义漂移。 |
| 进程树装配序列 | 2 份 | `tools/async_run.go:244`（`startAsync`）vs `tools/router.go:554`（`newScopedCommand` + `:568` `startScopedCommand`） | 同一套 `NewProcessTree`/`ConfigureProcessTree`/`Cancel`/`Attach` 写了两遍。 |
| 恢复说明状态容器 | 2 份 | `runtime_subagent_resume.go:49` vs `workunit_team.go:265` | 同形状 map 两份。 |
| 「在跑」状态字面量 | 2 处未收（+1 处已收） | `runtime_subagent_resume.go:329`、`session/subagent_sessions.go:515` vs 契约 `workunit/session.go:60` | 契约有一份，子代理侧两处仍写死字面量。 |
| 收尾分类 → 落点状态映射 | 2 份（载体不同，同一判据） | 计划侧 `teamwork/items.go:572`（`applySettleOutcome`：`OutcomeKind` → item status `review/failed` + `Unmerged` 标记）；记录侧 `workunit_team.go:55`（`teamUnitStatusFor`：`OutcomeKind` → 记录 status `done/failed`） | 分类本身已统一（`workunit.ClassifyFinish`），但**同一结论映射到两个载体**各写一遍（`settleWorkItem` 步 3 与 `settleTeamUnitRecord` 各一次）。 |
| 「是否在册现场」路径比较 | 2 份 | `worktree_manager.go:538`（`Restore` 逐字符 `os.Stat`）与 `:577`（`sceneRegistered` 走 `worktreePathEqual` 规范化比较） | 同一件事（路径是否在册）两种比较口径；注释把逐字符比较写成"恢复现场被 `Prune` 误删的第二个成因"。 |
| 「同名现场路径」构造 | 1 份 | `worktree_manager.go:371`（`scenePath`）与 `:682`（`isManagedPath` 前缀判定） | 都是 `filepath.Base(root)+"-seelex-"` 口径，**同一命名两处拼**（一处拼路径、一处判前缀），未抽成一个常量/函数。 |

---

## 二、归并建议清单（按收益排序）

> 收益排序的判据：① 是否已经在真实事故里伤过人（F2/F3/F4/F5、缺陷 A）② 是否会随"再加一条链"继续漂移 ③ 改动面是否已经收敛到一个小函数。

1. **恢复名单登记（`NoteWorktree`）收进 `workunit.Unit.Recover/Begin` 的父实现**
   - 现状 **2 份**（子代理 `NoteWorktree`；teammate 计划+账本认领）。
   - 应归到 **`seelebridge/workunit` 的现场登记唯一实现**（`Unit.Begin` 里统一落 `NodeSessionRecord.Worktree`；恢复走 `Unit.Recover`）。
   - 代价/风险：teammate 的现场登记目前夹在 `RestoreSubagentAnchors` 的 Restore/Adopt/Prune 顺序判据里，改动必须保住"认领先于 Prune"这一条，否则 F4 复现。

2. **现场清理（`cleanup` vs `CleanupWorktree`）并成一份幂等实现**
   - 现状 **2 份**且语义漂移（`:779` 非幂等、`:822` 幂等）。
   - 应归到 **`WorktreeManager.cleanup` 一个唯一实现**（`CleanupWorktree` 变成它的薄包装，或反之）。
   - 代价/风险：低；但两条路径的调用方（`finishExclusive` 的干净成功分支 vs `ReleaseWorkspace*`）对"目录已不在"的容忍度不同，合并时必须按幂等口径统一，否则缺陷 A 从前门回来。

3. **「工作区脏」判定收成一份**
   - 现状 **3 份**（`:749` / `:688` / `runtime_teamwork.go:861`）。
   - 应归到 **`WorktreeManager` 的一个私有判定**（`pathDirty`），其余两处转调。
   - 代价/风险：低；但 CRLF 幻影脏的修复点因此只需改一处（当前任一改法都要改三处，正好是漂移温床）。

4. **会话状态词表收成契约一份**
   - 现状 **3 处**（`runtime_subagent_resume.go:329`、`subagent_sessions.go:515`，契约 `session.go:60`）。
   - 应归到 **`workunit.InFlight` / `workunit.StatusQueued|StatusRunning` 唯一一份**（teammate 侧已做到，子代理侧两处照抄）。
   - 代价/风险：低；风险是子代理终态（`done`/`failed`）刻意不进契约，归并时不要顺手把终态也写死。

5. **进程树装配序列收成一份**
   - 现状 **2 份**（`async_run.go:244` / `router.go:554`）。
   - 应归到 **`security` 或 `tools` 的一个 `newProcessTreeCommand(...)` 助手**（唯一的 `NewProcessTree`+`ConfigureProcessTree`+`Cancel`+`Attach` 序列）。
   - 代价/风险：低-中；两处的 ctx 语义不同（后台用 `WithoutCancel + HardCap`、同步用 `scopedToolTimeout`），助手只能收"树的装配"，不能把超时策略也一起收进去。

6. **恢复说明状态容器收成一份**
   - 现状 **2 份**同形状 map（`subagentResumeState` / `teamResumeState`）。
   - 应归到 **一个 `resumeNotes` 类型**（键 → 说明；`set/consume/clear`）。
   - 代价/风险：低；`workunit.RecoveryNote` 构建器已经是唯一一份，剩下仅容器。

7. **「恢复续跑」补到 teammate 链（对齐 subagent 的 `Reexecute`）**
   - 现状 **1 份，只有子代理链有**（`runtime_subagent_resume.go` 七步 + `Reexecute`）。
   - 应归到 **`workunit.Unit.Recover` 之后的统一续跑动作**（两地都调同一工具，例如 `fork_subagents` 同键重跑 / `team_dispatch` 复用会话号）。
   - 代价/风险：中；teammate 的"重派"当前由 leader 决策（`team_recover` + `team_dispatch`），补统一动作时**不能**绕过 leader 的闸门（屏障/依赖/在编校验），否则会造出第二条调度真相。

8. **作业面（tools 表 vs `jobs.Manager`）收成一处的长期项**
   - 现状 **2 张表**（`async_exec.go` / `jobs.Manager`）。
   - 应归到 **`jobs.Manager` 一处**（`workunit.Jobs` 端口已是这个形状的编译期断言）。
   - 代价/风险：**高**；`CHANGELOG.md:112` 已明确登记为被挡住的一步——两套 scope 模型（`JobSpec.SessionID` vs `jobs.Scope{Session,Subject}`）、两套回收时机（会话销毁 vs 团队收口）都要同时迁移，属于跨层改动，不建议与本次归并一起做。

9. **「是否在册」路径比较口径收成一份**
   - 现状 **2 份**（`Restore` 的 `os.Stat` / `sceneRegistered` 的 `worktreePathEqual`）。
   - 应归到 **`worktreePathEqual` 唯一口径**（含 `scenePath`/`isManagedPath` 的命名常量）。
   - 代价/风险：低；纯口径统一，但要注意 `Restore` 的"目录不存在不登记"语义是防幽灵条目（`:534`），保留它、只统一"如何比较"。

---

## 三、确认过的命令与输出要点

> 环境说明：本机默认 shell 是 PowerShell，`grep`/`head` 不可用；`bash_read` 会拒收带
> `|`、`&&`、`2>/dev/null` 的命令。以下结论全部来自项目内的 grep/读取工具（等价于
> `git grep -n` 的逐条命中），逐行给出命中原文要点。

- `NoteWorktree` 全仓命中（源文件仅两处：定义 + 一个调用点）：
  - `seelebridge/session/subagent_sessions.go:803` — `func (s *SubagentSessions) NoteWorktree(...)`（定义）
  - `seelebridge/runtime_plan.go:151` — `r.subagentSessions.NoteWorktree(nodeID, sessionstore.NodeWorktreeRecord{`（**唯一调用点**，在 `beginNodeWorktree` 内）
  - 其余命中均为注释/文档/测试（`runtime_subagent_recovery.go:139`、`runtime_teamwork_scene.go:9/12`、`workunit_node.go:20/55/60`、`runtime_teamwork_scene_test.go:6` 等）。
- `beginNodeWorktree`：定义 `seelebridge/runtime_plan.go:145`；注入点 `runtime_plan.go:306`（`nodeDeps` 的 `BeginNodeWorktree`）；测试 6 处（`worktree_test.go:111/158/196/234/255/290`）。
- `BeginNamed`：定义 `seelebridge/worktree/worktree_manager.go:256`；唯一生产调用点 `seelebridge/runtime_teamwork_items.go:66`（`BindWorkspace` 内 `wt := r.worktreeMgr.BeginNamed(nodeID)`）。
- `security.NewProcessTree` 全仓仅两个调用点：`seelebridge/tools/async_run.go:245`、`seelebridge/tools/router.go:555`。
- `git status --porcelain` 命中三处：`seelebridge/worktree/worktree_manager.go:689`、`:750`、`seelebridge/runtime_teamwork.go:861`。
- `jobs.Scope` 命中：`seelebridge/teamwork/coordinator.go:217/426/627`、`seelebridge/teamwork/items.go:412`、`seelebridge/jobs_events.go:123`、`seelebridge/runtime_teamwork_board.go:187`、`runtime_teamwork_board_archive.go:75`、`runtime_teamwork_board_close.go:76`、`runtime_teamwork_context.go:86`、`runtime_teamwork_jobs.go:26`（teammate 作业只有 `jobs.Manager` 这一个事实源）。
- `ResetSession` → 定义 `seelebridge/runtime_teamwork.go:836`；调用点 `seelebridge/teamwork/coordinator.go:441`（`retireSteps` 步 3）、`seelebridge/teamwork/items.go:793`（`releaseBinding` 清会话内容）。
- `retireSteps` / 收口：`seelebridge/teamwork/coordinator.go:379`（`closeStepsLocked`）、`:407`（`Reclaim`）、`:517`（`Close`）、`items.go:894`（`releaseAllItems`）；文档 `docs/arch/teamwork-leader-worker-architecture.md:471/473` 明确 `team_retire` **不回收作业**，回收统一到 `team_close`。
- `ReleaseSessionAsync`：定义 `seelebridge/runtime_tools.go:179`；调用点 `application/core/workspace_usecase.go:18`（会话删除/归档）。
- `CloseSessionAsync` / `CloseAsync`：`seelebridge/tools/async_run.go:119` / `:107`；`CloseAsync` 注册进关停链 `seelebridge/runtime_tools.go:169`。
- `CHANGELOG.md:112` 原文要点：M0 最后一步（把后台作业面搬到 Seele `jobs.Manager`）**被挡住**——异步面与框架 `jobs.Manager` 在……（不一致）。
- 契约包现状：`seelebridge/workunit/contract.go`（`Unit`/`FinishPolicy`/`Jobs`）、`classify.go`（`ClassifyFinish`）、`session.go`（`InFlight`/`RecoveryNote`/`SessionLedger`）；`seelebridge/workunit/README.md` 已把四条不变式（现场是人的资产 / 认领先于 Prune / 收尾分类只有一份 / 恢复说明只有一族）写成文档。
- 最近提交（`git log --oneline -3`）：`746b00e`（三层共用一份在跑词表 + 跨层一致性用例）、`db9e7f4`（subagent 侧接线统一契约：`ClassifyFinish`/`RecoveryNote`，新增 `workunit_node.go` 第一份 Unit 实现）、`09643d5`（合并挡路分类 `ErrMergeBlockedByMain` + 收尾串行化）——说明本盘点记录的正是"契约已落地一半"的中间状态。

---

## 四、不确定项（需要怎么验证）

- **U1｜`workunit.Unit.Recover` 是否有生产调用者？** 目前只看到 `nodeWorkUnit.Recover`（`workunit_node.go:114`）与 `teamUnit.Recover`（`workunit_team.go:592`）两个实现；未确认真实装配里谁按 `Unit` 调 `Begin/Finish/Reclaim/Recover`（还是各链仍走 `bindWorkerProjectRoot`/`RestoreSubagentAnchors`/`RecoverTeamworkUnits` 原函数）。
  - 验证：`git grep -n "workunit.Unit\|\.Recover(" -- seelebridge/application/`，并追 `newNodeWorkUnit` / `newTeamUnit` 的调用点是否有生产装配（非测试）。
- **U2｜teammate 现场是否被重复登记（记录 `Worktree` 栏 + 计划/账本各一次）？** `saveTeamUnitRecord`（`workunit_team.go:134`）会往同一张 `NodeSessionStore` 写带 `Worktree` 的记录，而 `RestoreSubagentAnchors` 的 `worktreeMgr.Restore(records)`（`runtime_subagent_recovery.go:113` 内）读的是**未按现场名单过滤**的全部记录。
  - 验证：写一条"teammate 单元记录 → 重启 → 观察 `RegisteredCount()` 与 `adoptTeamworkScenes` 返回值"的用例（或在测试里断言两次登记的 nodeID 相同不产生第二个目录）。
  - **结论（已落地）**：**不会重复登记**——注册表的键是 `nodeID`，现场目录名/分支名也只按 nodeID 拼（`sceneDirName`/`sceneDirPrefix`，`worktree_manager.go:386/391`），两个来源指向**同一个目录**；`Adopt`（`:354`）与 `beginNamed`（`:287`）进门先判"已在册就原样返回"，重建前那句 `worktree remove --force` 因此走不到。
  - **落地时发现并修掉的一处分叉**：`Restore`（`:610`）过去是**无条件覆盖**，而 teammate 记录只写 Path/Branch（`teamUnitWorktreeRecord`，`workunit_team_records.go:97-107`），于是"会话恢复链在本次进程里对已在册的 nodeID 跑一次"会把活登记的 `MainBranch`/`BaseCommit` 覆盖成空栏位——收尾的 `alignMergeTarget` 随即静默降级成"合进当前 HEAD"（正是 2026-10-06 那条红灯的形状）。现在 `Restore` 与 `Adopt`/`beginNamed` 是同一条判据：**已在册不覆盖，谁先到都一样**。
  - 用例：`seelebridge/worktree/worktree_registration_sources_test.go`（两种顺序各一条 + 活登记不被降级 + 重启仍能重建 + "目录不在不登记"的防幽灵语义仍在）。
- **U3｜`roleTurnState` 之外，`sessionstore/role_session.go`（群聊 role draft / role wire）是否仍在生产链上？** 它是一套**第三份**角色会话持久化（`sessionstore/role_session.go` + `role_session_router.go`），来源是 AgentTeam/群聊模型。
  - 验证：`git grep -n "RoleSnapshotWorkspace\|SyncRoleDraft" -- application/ internal/` 看当前装配是否调用；若已被 Work Item 口径取代，可作为"同一件事三份"的补记，否则需要单独判定它是否与 teammate 链重叠。
- **U4｜两条链的"中断判定"是否会给出两个答案？** `Coordinator.Recover` 的 `report.Interrupted`（按句柄）与 `RecoverTeamworkUnits` 的记录侧判定（按记录 status + 执行面）是两份判据，代码用并集合并，但未验证"记录说在跑、句柄仍在册"这类冲突态下的预期口径。
  - 验证：构造"句柄在册但记录 status=running 且角色会话槽已空"的用例，断言 `Resume.Interrupted` 与 `Report.Interrupted` 的差集符合预期。
- **U5｜进程树退化路径的覆盖面**：`ProcessTree.Degraded()`（`process_tree_windows.go:128`）的 fallback 只在单测覆盖（`docs/2026-09-24-async-tool-deferred-ack/README.md:619` 记载"本机从未真实触发"）；两条调用链（`async_run` / `router`）在退化时的对外主张是否一致未验证。
  - 验证：在 Job 创建被拒的环境/假树注入下，分别跑 `bash_bg` 与同步 `bash`，比对其错误文案与 `Degraded` 读数。
  - **结论（已落地）**：**装配口径一致，读数过去不一致**。装配只有一份（`newProcessTreeCommand` / `startWithProcessTree`，同步链与后台链同源、挂不上都不放弃执行）；读数只有后台链有——探针把 `Degraded` 折进 `AsyncRunInfo.Degraded`（工作表格那一栏）与观察行"· 进程树挂不上，终止只及直接子进程"（`async_probe.go:62/:125`），同步链（bash / bash_read，含 docker 重试）**一个都没有**。现在同步链在起命令后读**同一处判据**（`tree.Degraded()`）并产出 `bash.process.degraded` 诊断读数：`seelebridge/tools/router.go` 的 `noteProcessTreeDegraded`，两处起命令点都调它；不退化时一个字都不发（既有"诊断阶段逐个相等"用例是这条的守卫）。
  - **仍开放**：① 同步链这条读数目前只到**后端诊断口**（`application/console` 的 `LogBashEvent`），要不要把退化事实带进**工具结果**（改对外形状 / 进不进 dto）是独立一批的决定。
  - **残②已落地**：`Degraded()` 过去判的是"Job 建没建成"（`job == 0`），**"Job 建成了但这个进程没挂进去"这一形态不进判据**——`Attach` 失败只是 `return err`，树照样报 `Degraded() == false`，调用方于是可以拿"整棵树已终止"去主张一个没保证的事实（Job 终止打不到不在 Job 里的进程，漏掉的是孙进程）。现在 `Attach` 的两条失败路径都置位，`Degraded() = job == 0 || attachFailed`；POSIX 侧仍是恒 false（Setpgid + kill(-pgid) 覆盖整组，没有"挂不上"这一形态，注释里写清了这处平台差异）。用例：`seelebridge/security/process_tree_attach_test.go`（失败必报退化 + 活进程挂上必不报；真红先跑，见落地记录）。
  - 用例：`seelebridge/tools/process_tree_degraded_reading_test.go`（退化必报 + 健康必静默）。建树工厂 `newExecProcessTree` 只在用例里被换掉：`&security.ProcessTree{}` 就是"Job 没建成"的真实形态，而"Job 建不出来"没法在真机上按需复现。
- **U6｜`InFlight` 未收敛处是否有行为差异**：`runtime_subagent_resume.go:329` 只认 `queued|running`，与契约 `InFlight` 目前一致；但若契约词表扩展（例如新增 `paused`），此处不会跟随。
  - 验证：`git grep -n '"queued"\|"running"' -- seelebridge/ | grep -v _test` 列出全部字面量点，逐个判定是否应转调 `workunit.InFlight`。
  - **结论（已落地）**：判"还在不在跑"的那半份只有契约一处（`workunit.StatusQueued/StatusRunning` + `InFlight`）；两个**终态**过去有**两份定义**（`workunit_team.go:36-37` 与 `runtime_subagent_resume.go:353-354` 各写一遍字面量，互相没有约束）+ 两处直接写字面量的写点（`node/coordinator.go:132/135`、`session/subagent_sessions.go:208`）+ 一处按字面量判（`session/subagent_tree.go:422/424`）。现在四个取值面的**字面量只有 `dto.SubAgent*` 一份**，其余全部转调（`string(dto.SubAgentDone)`），跨包互锁在 `stage_preview_judgment_test.go` 的 `TestSubagentStatusVocabularyAgreesWithTheWire`（teammate 侧与 `session` 包再导出都进了表）。
  - 机械门禁：`e2e/subagent_status_vocabulary_gate_test.go`——按"格子"声明范围（每格给出写方/读方清单与取值面），按五种形态查（状态比较 / 状态赋值 / 状态字段 / 状态常量声明 / switch 状态分支），阴性对照与"白名单过期也红"一起生效。第一版只查四种形态，**抓不到第二份定义本人**（`asyncStateDone = "done"` 这种常量声明）——第二批补上第五种。
  - **第二批已落地**：① 后台作业状态词收成 `dto.AsyncState*`（`tools/async_exec.go`、`application/core/async_completion.go`、`application/core/work_table_async.go` 全部转调）；② 计划批次结果状态词收成 `dto.PlanRunStatus*`（写侧 `plan/tool_provider.go`、读侧 `application/core/plan_tools.go` 的四个点全部转调——plan 包因此第一次引用契约，因为 plan_run 的结果 JSON 本来就是两层的契约面）。
  - **第三批（枚举化；用户口径"状态用枚举和 iota 来规范，不要用零散的字符串做硬编码比较"）已落地**：`dto.AsyncState` 与 `dto.PlanRunStatus` 从"契约里的一处**无类型字符串**"升级成 **int + iota 枚举**——`String()` 给对外词（JSON / 工具结果 / 看板只在 `words` 表里出现一次）、`ParseX` 读回、`MarshalJSON`/`UnmarshalJSON` 保住 wire 形状；字段类型跟着换：`AsyncRunRecord.State`、`tools` 的 `asyncRun.state` / `asyncPayload.State` / 探针 `State`、`fork` 的子代理作业终态端口 `Complete(handle, state dto.AsyncState)`、`TeamworkJobCompletionRecord.State`（框架 `jobs.State*` 在 seelebridge 投影处**折一次**）。效果：**字符串比较编译不过**——写错一个词是编译错误，不再是"记录说已完成、看板说还在跑"（读数与迁移面见 `docs/2026-10-06-workunit-jobs-port/step-3-u-items-delivery-3.md`）。
  - **仍开放（第四批）**：③ 子代理工具事件状态词与工具调用状态词在 `session/tool_events.go`、`application/core/tool_hooks.go`、`application/core/subagent_view/coordinator.go` 三处交织（running|success|error 与 running|completed|failed 两张表叠在一起），**收口前先把读方点清点**；④ 统一事件摘要状态词（`events_unified.go:89`）是 Seele 框架 `SummaryEvent.Status` 的词，不归我们的契约；⑤ 回执状态词（`observed|killed|already_finished|finished`）目前只在白名单里登记了一处理由（`async_exec.go` 的 `Status: "killed"`），要连 `job_manage` 的四种 op 一起收；⑥ **记录状态那一格**（`dto.SubAgentNodeStatus`，现为 `type X string` + 四个常量，字段仍写 `string`）面更大：既落盘（会话记录）又上 GUI，枚举化要连恢复路径一起过。
