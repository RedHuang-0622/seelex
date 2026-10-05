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
- **U3｜`roleTurnState` 之外，`sessionstore/role_session.go`（群聊 role draft / role wire）是否仍在生产链上？** 它是一套**第三份**角色会话持久化（`sessionstore/role_session.go` + `role_session_router.go`），来源是 AgentTeam/群聊模型。
  - 验证：`git grep -n "RoleSnapshotWorkspace\|SyncRoleDraft" -- application/ internal/` 看当前装配是否调用；若已被 Work Item 口径取代，可作为"同一件事三份"的补记，否则需要单独判定它是否与 teammate 链重叠。
- **U4｜两条链的"中断判定"是否会给出两个答案？** `Coordinator.Recover` 的 `report.Interrupted`（按句柄）与 `RecoverTeamworkUnits` 的记录侧判定（按记录 status + 执行面）是两份判据，代码用并集合并，但未验证"记录说在跑、句柄仍在册"这类冲突态下的预期口径。
  - 验证：构造"句柄在册但记录 status=running 且角色会话槽已空"的用例，断言 `Resume.Interrupted` 与 `Report.Interrupted` 的差集符合预期。
- **U5｜进程树退化路径的覆盖面**：`ProcessTree.Degraded()`（`process_tree_windows.go:128`）的 fallback 只在单测覆盖（`docs/2026-09-24-async-tool-deferred-ack/README.md:619` 记载"本机从未真实触发"）；两条调用链（`async_run` / `router`）在退化时的对外主张是否一致未验证。
  - 验证：在 Job 创建被拒的环境/假树注入下，分别跑 `bash_bg` 与同步 `bash`，比对其错误文案与 `Degraded` 读数。
- **U6｜`InFlight` 未收敛处是否有行为差异**：`runtime_subagent_resume.go:329` 只认 `queued|running`，与契约 `InFlight` 目前一致；但若契约词表扩展（例如新增 `paused`），此处不会跟随。
  - 验证：`git grep -n '"queued"\|"running"' -- seelebridge/ | grep -v _test` 列出全部字面量点，逐个判定是否应转调 `workunit.InFlight`。
