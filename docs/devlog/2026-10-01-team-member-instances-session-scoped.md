# 团队会话粒度：成员实例按会话派发（用例 2 / 任务 C，2026-10-01）

> **口径**：本文是用例 2「多个会话召唤同一支团队 → 团队是会话粒度的」的实现记录。
> 每条给**修前事实**（可核对）、**改法**与**回归证据**。结论分 **Confirmed** / **Hypothesis**。

---

## 1. 症状与根因（修前，Confirmed）

用户口径：**一个团队被多个会话召唤时，每个会话持有工厂派发的自己的成员实例**，
彼此的运行态（作业、会话内容、工作区）互不串；母本只提供"配方"。

修前事实：

- `agentteam.RoleSessionID(teamID, roleName)` 只由 `(team_id, role_name)` 决定
  （`application/core/agentteam/spec.go`）。两个主会话召唤同一支团队 ⇒ **同一个角色会话号**。
- **存储面没串**（侥幸）：角色会话子树挂在各自主会话下
  （`sessionstore/role_session.go: roleSessionRoot(mainKey, ...)` = `<主会话根>/role_<hash(roleSessionID)>`）。
- **运行面全串**（真症状）：凡按角色会话号做键的地方都把两条会话的同名员工当成同一个人：
  - 角色引擎槽 `roleTurnState.sessions[roleSessionID]`（`seelebridge/runtime_role_turn.go`）
    ⇒ 第二个会话的员工回合被扣进第一个会话的引擎，**历史串味**；
  - 项目根绑定 `ProjectScope.BindFor(roleSessionID, root)`
    （`bindRoleProjectRoot`/`bindWorkerProjectRoot`）⇒ **后一个会话覆盖前一个的项目根**；
  - 权责反查 `agentteam_role_index.go` ⇒ 代码里原本就写着这条已知缺陷：
    「角色会话号是 `teamID-roleName`，**不含主会话身份**……要从根上消除歧义得把主会话
    身份编进角色会话号（存储格式迁移），不在本次改动范围内」。

后者是**设计层已经记账、明确推迟**的那件事；用例 2 就是把它做掉。

## 2. 改法（成员实例 = (主会话, team, 角色)）

把主会话身份编进角色会话号，隔离由**标识**保证，而不是靠下游各自记得再拼一次主会话：

- `agentteam.RoleSessionID(mainSessionID, teamID, roleName)`
  （`application/core/agentteam/spec.go`；空 `mainSessionID` = 无会话归属的退化形态，仅桩/测试）。
- 工厂两处派生（`Materialize` / `InstantiateRole`）与成员表投影 `buildMember`
  带上 `mainSessionID`（`factory.go`）。
- 同口径跟着改：goal 的 TL 会话号（`goal_service.go`、`goal_team_recorder.go`）、
  teamwork 的 `DefaultRoleSessionID`（`seelebridge/teamwork/teamwork.go`）
  与 `Coordinator.SetPlan` 的补齐口径（`coordinator.go`）。
- `agentteam_role_index.go` 的"歧义口径"注释从「待消除」改为「已消除」：`byRole`
  保留切片 + 最严口径仍是对的（它还承担"同一 (主会话, 角色) 改名/改权责后以最新为准"
  的覆盖语义），但跨会话重号这条来源已不存在。
- 文档同口径：`docs/arch/teamwork-leader-worker-architecture.md` §4.2、
  `application/core/agentteam/README.md`。

**迁移影响（诚实标注）**：角色子树目录名是 `role_<hash(roleSessionID)>`，所以升级后
**已存在的角色会话子树不再被新号指向**（旧目录成为孤儿；角色会话是"进程内执行面 +
派生草稿"，不是长期事实来源）。这不是静默数据损坏，但确实是一次存储格式迁移——
与注释里当年记的那句"存储格式迁移"一致。

## 3. 回归（红 → 绿）

| 用例 | 位置 | 修前 | 修后 |
|---|---|---|---|
| 同一团队装配进两个会话，各自 `Created=true`、角色会话号不同、同会话重复装配幂等 | `application/core/agentteam/agentteam_test.go` `TestMaterializeSameTeamIntoTwoSessionsDispatchesOwnMembers` | 两次装配落到同一个号，第二次 `Created=false`（RED） | GREEN |
| 两个主会话召唤同一团队的同一角色 → 各自一个角色引擎、回合不与他会话合流 | `seelebridge/runtime_role_turn_test.go` `TestTeamMemberInstancesAreSessionScoped` | 只造出 1 个引擎（RED） | GREEN |

既有幂等/身份用例按新口径改写期望：`TestGoalPresetMaterializeIsIdempotent`
（`main-1-goal-a2a-tl`）、`TestSetPlanDerivesRoleSessionIDsAndAudits`（`s-v-model-pm`）、
`TestRecordTLRoundAppendsThenSyncsInTheSameRound`、`TestDispatchJoinMilestoneLifecycle`
（退场步 3 的 `session:s-v-model-exec`）。

命令：

```text
go test -p 1 -count=1 ./application/core/agentteam/... ./seelebridge/teamwork/... ./application/core/ ./seelebridge/
```

## 4. 已知残余（本用例范围外的相邻缺陷，未在本次修复）

- **teammate 的工作区名仍只按角色名**：`Coordinator.Retire` 调
  `WorkspaceReleaser.ReleaseWorkspace(ctx, role)`，而 `Runtime` 侧按 `role` 查
  `worktreeMgr.Info(role)`。若两个会话在同一个项目里派发同名角色，`worktree_manager`
  的 `nodeID → worktree` 表（路径 `<repoBase>-seelex-<nodeID>`）会指向**同一个目录**。
  （**Confirmed**，属 worktree 域的命名问题，与角色会话号正交；改法是把 teammate 的
  工作区指派名一并做成会话粒度，涉及 `WorkspaceReleaser` 端口签名。）
- **`TestDispatchRefusesWhenTeamIsFull` 的竞态 flaky —— 已修**：它靠"占位作业处于
  Running"顶满人数，而占位作业此前**载荷为空**，worker 执行体会立刻判失败终态（`sink.Exit(1)`
  + `Complete(StateFailed)`），于是"派发 → 终态"的窗口决定它是否偶发假绿；机器负载高时
  （全量 `-p 1` 跑）复现红。修法是给占位作业一份**可解码**的载荷，让它们真的走到
  `fakeRunner.RunWorker` 并阻塞在 `runner.block` 上——这正是该用例设置 `block` 的本意，
  修后是**更强**的判据（不再依赖调度时序）。`go test -count=10 ./seelebridge/teamwork/` 全绿。
