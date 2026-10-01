# seelebridge/teamwork — leader/worker 团队编排域

## 生态位

把 teamwork 从「一排轮流发言的座位」重做成「**一个 leader + 一组可被派发 / 观察 /
终止的 worker 作业**」。分工（`docs/arch/teamwork-leader-worker-architecture.md` §2 / D1）：

- **作业面**归 Seele 的 `jobs` 根能力（契约 + `Manager` + `jobs_manage`）；
- **硬编排**归本包：计划（谁、什么顺序）+ 派发 + 汇合 + 里程碑 + 退场；
- **执行体**（在角色会话里真跑一轮）与**工作区**（git worktree）是**端口**，由装配层
  （`seelebridge/runtime_teamwork.go`）注入——本包因此能在没有引擎、没有 git 的测试里把
  编排语义（顺序、超员拒绝、作用域回收、退场四步）全部跑完。

顺序的**唯一事实**是计划的 `stages[].depends_on`，不是 leader 的调用姿势，也不是任何
「上一轮是谁」的隐式状态。

## 与其它域的关系

```mermaid
flowchart LR
    LEADER["leader 会话<br/>team_plan / dispatch / join / milestone / retire"] --> CO["teamwork.Coordinator"]
    CO --> JOBS["Seele jobs.Manager（作业表 / 状态机 / jobs_manage）"]
    CO --> STORE["PlanStore → sessionstore.moduleTeamwork<br/>plan 头 + events.jsonl 审计"]
    JOBS --> WE["WorkerExecutor（Kind=worker）"]
    JOBS --> SE["SeatExecutor（Kind=seat）"]
    WE --> WR["WorkerRunner 端口 → runtime_role_turn"]
    SE --> SR["SeatRunner / SeatRoundRunner 端口 → goal 域"]
    CO --> WT["WorkspaceReleaser 端口 → worktree 域"]
    CO --> SS["SessionResetter 端口 → 角色会话内容"]
```

## 生命周期状态

```mermaid
stateDiagram-v2
    [*] --> Enrolled: team_plan（stages + members + milestones）
    Enrolled --> Dispatched: team_dispatch（role ∈ 在编，且未超上限）
    Dispatched --> Observed: jobs_manage observe / fetch（消费式）
    Dispatched --> Joined: team_join（有界等待，只观察不取回）
    Joined --> Milestoned: team_milestone（依赖边判据）
    Milestoned --> Retired: team_retire（回收作业 → 释放工作区 → 清会话内容 → 保在线）
    Retired --> [*]
```

## 职责与非职责

职责：

- 计划校验与持久化（`SetPlan`）：一角色一成员、无内置角色（`main` / `user`）、人数上限、
  stage id 唯一、`depends_on` 无环、里程碑引用存在——**在持久化点再校验一次**（计划可被
  leader 重写，第二道闸必须存在）；
- 派发（含去重与超员拒绝）、有界汇合、里程碑、退场四步；
- 追加 `plan / dispatch / join / milestone / retire` 审计行（append-only）。

非职责：

- 不跑 teammate（跑是 `jobs.Manager` + `WorkerRunner` 的事）；
- 不做 git 操作、不碰角色会话内容、不 import `application` 或根包——全部走端口。

## 目录或文件结构

| 文件 | 职责 |
|---|---|
| `teamwork.go` | 端口契约（`PlanStore` / `WorkerRunner` / `SeatRunner` / `SeatRoundRunner` / `WorkspaceReleaser` / `SessionResetter`）、`Options`、`Coordinator` 装配、计划校验、成员权限格子折叠 |
| `coordinator.go` | 五个动作：`SetPlan` / `Dispatch` / `Join` / `Milestone` / `Retire`，以及 `audit` |
| `executor.go` | 两个作业执行体：`WorkerExecutor`（`Kind=worker`）、`SeatExecutor`（`Kind=seat`） |
| `store.go` | `sessionstore` 持久面的适配与计划读写 |
| `teamwork_test.go` | 端口桩驱动的编排语义测试 |

## 核心实现

`Coordinator` 只掌控顺序、作用域与收口，它**自己不跑**任何 teammate——这正是
「leader 阻塞与否与 worker 是否推进正交」的落点。

- `Dispatch` 拒绝未在编的角色、拒绝超过产品级上限的成员（**显式拒绝，绝不静默排队**：
  排队会把「队伍满了」伪装成「它在跑」），并对同一角色的重复派发用 `jobs.Spec.Dedup`
  收敛到已运行的那条（两条作业共享一个 worktree 会让「谁在改这里」失去答案）。
- `Join` 是**有界**等待且**只观察不取回**：取回（`Fetch`）是消费式的，会把 teammate 的
  输出从工作表格上拿走。等待用 `jobs.Manager.Events()` 的变更信号口 + 短定时器，不轮询。
- `Retire` 四步固定顺序：回收该 teammate 的作业（`Reclaim(Scope{Session, Subject})`，
  不动兄弟 teammate）→ 释放工作区 → 清会话内容 → 保留在编。**端口缺失是显式错误**，
  不是静默跳过——worktree 数量只有在退场真的释放时才被封顶。
- `audit` 失败**不吞**：审计与计划是同一份事实的两个面。

## 依赖方向

`teamwork` → `Seele jobs`、`sessionstore`（计划/审计持久面）。**禁止** import
`application/*` 或 `seelebridge` 根包；执行体与工作区经端口反向注入。

## 并发、存储、安全或错误语义

- 执行体的 ctx 是 `jobs.Manager` 从 `Background` 派生的：**会话归属与工作正文一律走
  载荷**（`WorkerRequest` / `SeatRequest`），不能指望执行体 ctx 里还有原调用；
- 作业作用域 = `{Session: 主会话, Subject: emp_<role>}`；`Subject` 同时是权限主体；
- 成员权限格子（组 → 位）越界（非 0..255）是**计划写错了**，显式报错而不是截断成
  「分配成功」；
- 缺端口的动作一律显式报错，不静默降级（缺 `Worktrees` ⇒ `team_retire` 第二步报错）。

## 扩展方式

- 换持久面：实现 `PlanStore`（生产实现是 `sessionstore.moduleTeamwork`）；
- 换执行体：实现 `WorkerRunner` / `SeatRunner`；
- 换工作区策略：实现 `WorkspaceReleaser`（须遵守 `ErrUncommittedChanges` 语义）；
- 换顺序来源：目前唯一事实是 `stages[].depends_on`——要改顺序语义就改计划校验，而不是
  在执行体里加隐式状态。

## Review 指南

- 超员是否**显式拒绝**（不得排队）；同角色重复派发是否收敛到同一条作业；
- `Join` 是否只观察不取回、是否**有界**（预算到点如实返回「仍在跑」）；
- `Retire` 四步顺序是否固定、缺端口是否显式报错；
- 计划校验是否在**持久化点**也跑（不只在装配期）；
- 执行体是否只从载荷读会话归属与正文（不从 ctx 猜）。

## 测试与验证

端口桩驱动，不需要引擎/git（见 `teamwork_test.go`）。

```text
go test ./seelebridge/teamwork/ -count=1
go test ./seelebridge/... -count=1
```
