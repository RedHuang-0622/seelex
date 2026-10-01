# 团队/作业面六用例勘察定稿（2026-10-01）

本文是「团队 + 作业面」6 条验收用例的**现状矩阵**：每条给出**判据、证据、归属任务**。
它是勘察结论，不是设计稿；已落地的部分指回代码与用例，未落地的部分给出改法边界。

口径：结论分 **Confirmed**（有代码/用例证据）与 **Hypothesis**（待验证，写明验证方式）。
未标注者均为 Confirmed。

---

## 1. 现状矩阵

| # | 用例 | 现状 | 证据 | 归属 |
|---|---|---|---|---|
| 0 | jobs 事件流从 Seele 回退到 Seelex | **已落地** | `seelebridge/jobs_events.go`、`seelebridge/jobs_events_test.go` | 完成 |
| 1 | 402/额度中断 → 语义驱动的中断恢复 | **部分**（见 §2） | `seelebridge/account/failure.go`、`seelebridge/internal/stream/stream.go` | B（完成） |
| 2 | 多会话共用一个团队 → 团队会话粒度（会话持有工厂派发的成员） | **已落地（2026-10-01）** | `application/core/agentteam/spec.go` 的 `RoleSessionID(主会话, team, 角色)`；用例 `TestMaterializeSameTeamIntoTwoSessionsDispatchesOwnMembers` / `TestTeamMemberInstancesAreSessionScoped` | 完成（见 `2026-10-01-team-member-instances-session-scoped.md`） |
| 3 | worktree 生命周期（残留清理、不占磁盘） | **已落地** | `seelebridge/worktree/worktree_manager.go` 的 `Prune`/`Restore`/`Begin` | D（完成） |
| 4 | 前端：查看会话粒度下不同员工的运行详情 | **已落地（2026-10-01）** | `gui/frontend/dist/agent-team-view.js`（员工行 k→v 表格化 + 面板/详情刷新键）、`app.js`（`team.changed` 热更新）；见 `2026-10-01-e-frontend-runtime-detail-and-kv.md` | 完成（E） |
| 5 | 前端：切换对话视图到员工 | **已落地（2026-10-01，边界见 §5）** | `renderRoleSessionSwitcher`（切员工切换条）+ `app.js` 的 `roleSessionDetail` | 完成（E） |
| 6 | 前端：加供应商到 accounts.yaml | **不做**（用户已定） | `AGENTS.md` 铁律禁读写 accounts.yaml | — |

---

## 2. 用例 1：402 / 额度中断

**改前的现象**：会话循环报 `session loop 0: seelebridge: stream with account "X": ChatClient stream: HTTP 402 …`
后整轮判死，正文原样透出。

**根因**（两条叠加）：

1. **账号池不做重试**（Confirmed）：Seele `accountpool/README.md` 明写「模块不负责账号文件
   格式、Provider 协议、**重试**…这些策略由调用方或上层模块组合」。所以「第一志愿没额度 →
   第二志愿」这条回退**只能由 Seelex 实现**，而改前 Seelex 没有任何实现。
2. **错误分类里没有额度语义**（Confirmed）：`application/core/history_safety.go` 的
   `classifyProviderFailure` 只认 `context_exhausted` / `invalid_history` / `timeout` /
   5xx；402 落 `providerFailureNone` → 无恢复、无重放、无换号。

**注意**：改前也不是完全没有"回退"，但那是**另一种**东西——`FallbackRoles` /
`ResolveAccountSpec` / `ForRole` 解决的是「该角色**没有配置**账号」，与「配置了但**额度
耗尽**」是两回事，不能互相顶替。

**已落地**：分类放在能起作用的地方（`seelebridge/account/failure.go`），换号在流式适配器
的**租约释放之后**做（`seelebridge/internal/stream/stream.go`），并明确两条边界：

- pin = 「优先用它」而不是「只准用它」：第一志愿被拒时取第二志愿；
- 传输/上游故障**不换号**（换号不改变结果），原样上抛，避免一次抖动把所有账号撞一遍。

**仍缺**（下一步）：额度耗尽且**所有账号都试过**之后，会话循环看到的仍是原始错误文本；
若要把它呈现成一句人话（"所有账号都无可用额度"），需要在 `application/core` 的错误呈现面
按语义分类加一条。这是呈现层的事，不影响上面的控制流。

---

## 3. 用例 3：worktree 生命周期

**改前的风险**（Confirmed）：

- **无界累积**：worktree 只在成功收尾时 `git worktree remove`；失败/中断的现场按设计保留
  （`Release` 只解除注册、不删磁盘），而**全仓没有第二处清理器**。每个残留 = 一份完整项目
  检出 + 一个 `seelex/<id>` 分支。
- 真正建 worktree 的是 `fork_subagents` 的每个非 entry 节点（`seelebridge/node/agent_node.go`），
  不是 teammate——文档里「worktree 数量被 teammate 人数封顶」在实现上不成立。
- **幽灵条目**：`Restore` 不检查目录是否还在，把已删除的路径重新登记成现场 → `Info` 报一个
  不存在的路径，`team_retire` 第二步对它跑 `git status` 而失败。
- **同名 force-remove**：路径/分支只按 `nodeID` 命名，跨会话/跨批次的第二个 `Begin` 会
  `worktree remove --force` 一个正在使用的目录。

**已落地**：`Prune`（不在册 **且** 干净才回收；有改动一律保留）、`Restore` 跳过不存在的目录、
`Begin` 按 `nodeID` 幂等且不再强删在册路径；调用顺序 = 先 `Restore` 后 `Prune`
（`seelebridge/runtime_subagent_recovery.go`）。

**已知未决**：`worktreeDirty` 用裸 `git status --porcelain`，Windows/WSL 下 CRLF 转换可能造成
「幻影脏」（见 `seelebridge/worktree/README.md` 的已知风险）。它会让 `Prune` **多保留**（安全
方向），但也会让节点收尾误判为未提交失败——待单独修。

---

## 4. 用例 2（C）：团队会话粒度

**现状**：团队母本（员工库 / 团队库）是**全局**的，已装配会话持有的是副本；改员工库档案
**不会热传播**到已装配会话（要再"入职"一次，或走"普及到全局"）。

**要做的语义**：一个团队被多个会话召唤时，**每个会话持有工厂派发的自己的成员实例**，
彼此的运行态（作业、会话内容、工作区）互不串；母本只提供"配方"。

**边界**：这与 `Coordinator` 的会话作用域键（`project_id + session_id`）一致——作业表与计划
已经是会话粒度的，缺的是**成员实例的派发**这一步。改法是 `application/core/agentteam/factory.go`
的母本 → 会话副本路径，不是作业面。

---

## 5. 用例 4/5（E）：前端

**已有**：状态区（右栏）四块（员工库 / 团队库 / 员工栏 / Team 栏）、拖拽装配、入职面板
（系统提示词 / tools_policy / 逐格权限 / LLM 提示词优化候选）、删除、顺序调整、`team.changed`
失效。

**缺口（2026-10-01 已逐条收口，见 `2026-10-01-e-frontend-runtime-detail-and-kv.md`）**：

- 员工运行详情（`RoleSnapshot` 弹窗）是**一次性拉取**，无刷新键、无事件驱动
  → **已落地**：详情自带刷新键（身份在键上，原样重放），`team.changed` 到达且视图开着时重取；
- **没有手动刷新键**（只有展开即取 + `team.changed` + 切会话重取）；母本 CRUD 不发事件
  → **已落地**：面板常驻手动刷新键（`data-team-refresh`，强制重取）；母本六条写
  （员工库 / 团队库 / 默认顺序 / 普及）补发会话级 `team.changed`；
- 「单个员工的 k→v 全字段」只在懒加载的编辑面板里，行内只有 chip 回显（表格化不彻底）
  → **已落地**：员工行可展开一张 k→v 全字段表（`employeeFieldRows` / `.team-kv`），空栏也照列；
- 对话视图切到员工（用例 5）**取决于 ClaudeTeamwork 的运行方式调研**（见 §6）
  → **已落地（有边界）**：运行详情顶部是「切员工」切换条，点谁就把**已存在的会话视图**
  目标换成谁的角色会话。边界（Confirmed）：角色会话是主会话根下的子树
  （`role_<hash>/`、`goal_<hash>/`），GUI 主视图会话切换（`ResumeSession`）作用于顶层会话，
  指不到子树——把主对话区指向角色会话需要新造"视图目标 = 嵌套会话"的后端通道，不在本次范围。

---

## 6. 用例 5 的调研口径（A4）

`github.com/dperegolise/claude-teamwork` 的可借鉴点：Sentinel 只编排不写码 → Orchestrator 拆
里程碑 → Explorer/Worker/Reviewer/Critic/Auditor/Integrator；**每个里程碑一个
`.claude/worktrees/<id>`**（lead 建目录、cd 进去再 spawn），隔离与并行是同一件事；`/teamwork:status`
看进度/存活/审计态；`.teamwork/` 只装**持久证据 + resume 底料**；Stop hook 以 `audit-PASS`
强制审计门；SessionStart hook 负责崩溃恢复。

**与 Seelex 的对应**：`.claude/worktrees/<id>` ↔ `seelebridge/worktree`；`/teamwork:status` ↔
状态区 Team 栏 + `jobs_manage`；Stop hook 审计门 ↔ 合并审批门（`WorktreeManagerDeps.Gate`）。
**"切到员工视图"在它那里是"看这个人正在干什么"**：既要有**运行态读数**（作业记录），也要有
**它的工作区现场**（worktree 路径 + 未提交改动）。这条是 E 的前置结论。

---

## 7. 验证

```text
go test ./seelebridge/... -count=1
go test ./... -count=1
```

新增回归：`TestJobsEventStreamProjectsLifecycleIntoSessionLog`、
`TestStreamingCompleterFailsOverOnQuotaRejection`、
`TestStreamingCompleterDoesNotFailOverOnTransportError`、
`TestPruneRemovesOrphanWorktreesAndKeepsDirtyOnes`、`TestPruneRunsGitWorktreePrune`、
`TestRestoreSkipsWorktreesThatNoLongerExist`。

前端与母本事件（E，2026-10-01；见 `2026-10-01-e-frontend-runtime-detail-and-kv.md`）：

```text
node --test gui/frontend/dist/*.test.mjs
go test ./application/core/ -run "TestMasterCRUDsPublishTeamChanged" -count=1
```

新增回归：`agent-team-view.test.mjs`（`employeeFieldRows` 的 k→v、面板刷新键、详情刷新键、
「切员工」切换条）、`agent-team-refresh.test.mjs`（app.js 的接线口径）、
`TestMasterCRUDsPublishTeamChanged`（母本六条 CRUD 各发一条会话级 `team.changed`）。
